// This file is the rewrite of futures of phase B: it decides which reads are in
// flight, which locals hold a future, and which uses wait, and writes the kernel
// forms send, wait, and join with the future types of the locals.
package lower

import (
	"maps"
	"slices"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/syntax"
)

// local identifies a local of a body by its declaration and its name, because the variables of one iterator loop share a declaration.
type local struct {
	decl syntax.Node
	name string
}

// holding is what a local holds: a value, a future of its value, or a collection whose elements are futures.
type holding uint8

const (
	value holding = iota
	whole
	elements
)

// flights is what the rewrite of futures decides for the bodies of a program after phase A: the reads that a send hands to the runtime, the expressions of a future type that stay futures because their use is a move, the reads of an element under a handler, which read the values of their collection, because the handler lowering makes the read the subject of an outcome match, the arms of the reads whose handler belongs to their future, what each local holds, the locals that hold no future because a closure reads them or a statement writes a place inside them, the local collections that hold no future of their whole value because a statement stores to an element of them, the declarations that a store with such a handler takes the place of, the calls that are effects among the expressions that phase B built, and for each parameter that holds a future the local that holds it in its place, because no parameter has a future type.
type flights struct {
	p          *check.Program
	sequential bool
	inline     map[*syntax.Handled]bool
	sent       map[syntax.Expr]bool
	kept       map[syntax.Expr]bool
	valued     map[syntax.Expr]bool
	arms       map[syntax.Expr][]*syntax.SendArm
	held       map[local]holding
	fixed      map[local]bool
	indexed    map[local]bool
	merged     map[*syntax.Store][]*syntax.VarTarget
	dropped    map[syntax.Stmt]bool
	effects    map[syntax.Expr]bool
	shadows    map[local]string
}

func newFlights(p *check.Program, sequential bool, inline map[*syntax.Handled]bool) *flights {
	return &flights{p: p, sequential: sequential, inline: inline, sent: map[syntax.Expr]bool{}, kept: map[syntax.Expr]bool{}, valued: map[syntax.Expr]bool{},
		arms: map[syntax.Expr][]*syntax.SendArm{}, held: map[local]holding{}, fixed: map[local]bool{}, indexed: map[local]bool{},
		merged: map[*syntax.Store][]*syntax.VarTarget{}, dropped: map[syntax.Stmt]bool{}, effects: map[syntax.Expr]bool{}, shadows: map[local]string{}}
}

// pass is one walk of a body under the holdings found so far: the handlers that enclose the statement being walked, whether the statement stands in a handler arm, the result types of the routine, the reads that an effect of their own statement follows, and whether the walk found a new holding.
type pass struct {
	*flights
	b       *outcomes
	hs      []handler
	orig    *original
	results []syntax.Type
	blocked map[syntax.Expr]bool
	grew    bool
}

// analyse decides the futures of one routine or closure body with its parameters and the result types of its routine, walking the body until no local gains a holding, because a move into a local makes the stores of that local moves, and returns the declarations of the locals that hold the futures of the parameters.
func (fl *flights) analyse(b *outcomes, body *syntax.Block, params []*syntax.Param, results []syntax.Type) []syntax.Stmt {
	fl.places(body)
	for {
		w := &pass{flights: &flights{p: fl.p, sequential: fl.sequential, inline: fl.inline, sent: map[syntax.Expr]bool{}, kept: map[syntax.Expr]bool{}, valued: map[syntax.Expr]bool{},
			arms: map[syntax.Expr][]*syntax.SendArm{}, held: fl.held, fixed: fl.fixed, indexed: fl.indexed, merged: map[*syntax.Store][]*syntax.VarTarget{}, dropped: map[syntax.Stmt]bool{}},
			b: b, results: results, blocked: map[syntax.Expr]bool{}}
		w.stmts(body.Stmts)
		if w.grew {
			continue
		}
		maps.Copy(fl.sent, w.sent)
		maps.Copy(fl.kept, w.kept)
		maps.Copy(fl.valued, w.valued)
		maps.Copy(fl.arms, w.arms)
		maps.Copy(fl.merged, w.merged)
		maps.Copy(fl.dropped, w.dropped)
		break
	}
	var shadows []syntax.Stmt
	for _, p := range params {
		l := local{p, p.Name}
		if fl.held[l] == value {
			continue
		}
		fl.shadows[l] = b.fresh(p.Name)
		t := &syntax.VarTarget{Origin: p.Origin, Type: fl.futureType(p.Type, fl.held[l]), Name: fl.shadows[l]}
		shadows = append(shadows, &syntax.VarDecl{Origin: p.Origin, Targets: []*syntax.VarTarget{t}, Values: []syntax.Expr{name(p.Origin, p.Name)}})
	}
	return shadows
}

// places records the locals of a body that hold no future: each local that a closure of the body reads, because no capture has a future type, and each local at the root of a place that a store or a call of a method writes through a field or an index, because a future has no place inside it, where the index store of a collection local is the store of an element, which a collection of futures admits and a future of the whole collection does not.
func (fl *flights) places(body *syntax.Block) {
	root := func(x syntax.Expr, selected bool) {
		indexed := false
		for {
			switch y := x.(type) {
			case *syntax.FieldRead:
				x, selected = y.X, true
				continue
			case *syntax.IndexRead:
				_, plain := y.X.(*syntax.Name)
				x, selected, indexed = y.X, selected || !plain, true
				continue
			case *syntax.Name:
				if l, ok := fl.local(y); ok {
					fl.fixed[l] = fl.fixed[l] || selected
					fl.indexed[l] = fl.indexed[l] || indexed
				}
			}
			return
		}
	}
	syntax.Inspect(body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			inside := map[syntax.Node]bool{}
			syntax.Inspect(n, func(m syntax.Node) bool {
				inside[m] = true
				return true
			})
			syntax.Inspect(n, func(m syntax.Node) bool {
				if x, ok := m.(*syntax.Name); ok {
					if l, ok := fl.local(x); ok && !inside[l.decl] {
						fl.fixed[l] = true
					}
				}
				return true
			})
			return false
		case *syntax.Store:
			for _, t := range n.Targets {
				root(t, false)
			}
		case *syntax.MethodCall:
			if fl.p.ChangesReceiver(n) {
				root(n.Receiver, true)
			}
		}
		return true
	})
}

// local returns the local that a plain name refers to.
func (fl *flights) local(x syntax.Expr) (local, bool) {
	n, ok := x.(*syntax.Name)
	if !ok || n.Module != "" || n.Owner != nil {
		return local{}, false
	}
	d := fl.p.Local(n)
	return local{d, n.Name}, d != nil
}

// typeOf returns the type of a local without its holding.
func (fl *flights) typeOf(l local) syntax.Type {
	if loop, ok := l.decl.(*syntax.IterFor); ok {
		return fl.p.LoopVarTypes(loop)[slices.Index(loop.Vars, l.name)]
	}
	return fl.p.LocalType(l.decl)
}

// element returns the type of the elements of a collection type that can hold futures, which are the elements of an array and the values of a dictionary, or nil for any other type.
func element(t syntax.Type) syntax.Type {
	switch t := t.(type) {
	case *syntax.Array:
		return t.Elem
	case *syntax.Dict:
		return t.Value
	}
	return nil
}

// holds reports whether a local can hold a future: a declared variable or a parameter, which a local stands for, that no closure reads and no statement writes a place inside.
func (w *pass) holds(l local) bool {
	switch l.decl.(type) {
	case *syntax.VarTarget, *syntax.Param:
		return !w.fixed[l]
	}
	return false
}

// hold records that a local holds a future or futures as elements, where a holding grows from a future of the whole value to futures as elements.
func (w *pass) hold(l local, h holding) bool {
	if !w.holds(l) || h == elements && element(w.typeOf(l)) == nil || h == whole && w.indexed[l] {
		return false
	}
	if w.held[l] < h {
		w.held[l], w.grew = h, true
	}
	return w.held[l] == h
}

// read reports whether a call is a remote read or a call of a reader whose future can stay in flight: the schedule is not sequential, no enclosing handler selects a failure of it, and no effect follows it in its own statement.
func (w *pass) read(x syntax.Expr) bool {
	if !w.p.Read(x) && !w.p.Reader(x) || w.sequential || w.blocked[x] {
		return false
	}
	return !w.b.selects(x, ctx{hs: w.hs, orig: w.orig}, nil, false)
}

// future returns the type of the value of an expression of a future type: a local that holds a future of its whole value, or an element of a local collection of futures, and nil for any other expression.
func (fl *flights) future(x syntax.Expr) syntax.Type {
	switch x := x.(type) {
	case *syntax.Name:
		if l, ok := fl.local(x); ok && fl.held[l] == whole {
			return fl.typeOf(l)
		}
	case *syntax.IndexRead:
		if l, ok := fl.local(x.X); ok && fl.held[l] == elements {
			return element(fl.typeOf(l))
		}
	}
	return nil
}

// moves reports whether a value put into a place of a type is a move of a future: a read whose one result has the type, or an expression of a future type whose value has the type, where the read of an element under a handler is waited for, because the handler lowering gives the operation a value.
func (w *pass) moves(x syntax.Expr, t syntax.Type) bool {
	switch x.(type) {
	case *syntax.ServiceCall, *syntax.Call:
		rs := w.p.Results(x)
		return w.read(x) && len(rs) == 1 && check.Same(rs[0], t)
	}
	f := w.future(x)
	if _, element := x.(*syntax.IndexRead); element && (len(w.hs) > 0 || w.orig != nil) {
		return false
	}
	return f != nil && check.Same(f, t)
}

// futures reports whether a value put into a collection place of a type holds a future as an element: a local collection of futures of the type, or a collection literal one of whose elements is a move, and marks the elements that are moves.
func (w *pass) futures(x syntax.Expr, t syntax.Type, mark bool) bool {
	elem := element(t)
	if elem == nil {
		return false
	}
	found := false
	each := func(el syntax.Expr) {
		moved := w.moves(el, elem)
		found = found || moved
		if moved && mark {
			w.keep(el)
		}
	}
	switch x := x.(type) {
	case *syntax.Name:
		l, ok := w.local(x)
		return ok && w.held[l] == elements && check.Same(w.typeOf(l), t)
	case *syntax.ArrayLit:
		for _, el := range x.Elems {
			each(el)
		}
	case *syntax.DictLit:
		for _, en := range x.Entries {
			each(en.Value)
		}
	}
	return found
}

// keep records that an expression of a future type stays a future, and that the collection of an element that stays a future is not waited for.
func (w *pass) keep(x syntax.Expr) {
	w.kept[x] = true
	if ix, ok := x.(*syntax.IndexRead); ok {
		w.kept[ix.X] = true
	}
}

// put records the store of a value into a local of a type: a move of a future into a local that then holds one, or the store of futures as elements into a local collection that then holds them.
func (w *pass) put(l local, x syntax.Expr, t syntax.Type) {
	switch {
	case w.held[l] != elements && w.moves(x, t) && w.hold(l, whole):
		w.keep(x)
	case w.futures(x, t, false) && w.hold(l, elements):
		w.futures(x, t, true)
		w.kept[x] = true
	}
}

// appended returns the appended value of a store of append to its own first argument, which stores the value as an element of the local, or nil.
func (w *pass) appended(target, x syntax.Expr) syntax.Expr {
	c, ok := x.(*syntax.Call)
	if !ok || len(c.Args) != 2 {
		return nil
	}
	op := w.p.Operation(c)
	l, plain := w.local(target)
	first, same := w.local(c.Args[0].Value)
	if op == nil || op.Row.Name != "append" || !op.Row.Futures || !plain || !same || first != l {
		return nil
	}
	return c.Args[1].Value
}

// store records the stores of a declaration or a store into its targets: the results of one read into several locals, an element of a local collection, or one value for each target.
func (w *pass) store(targets, values []syntax.Expr, at func(i int) (local, syntax.Type, bool)) {
	if len(values) == 1 && len(targets) > 1 {
		rs := w.p.Results(values[0])
		moved := w.read(values[0]) && len(rs) == len(targets)
		locals := make([]local, len(targets))
		for i := 0; moved && i < len(targets); i++ {
			l, t, ok := at(i)
			locals[i] = l
			moved = ok && w.holds(l) && check.Same(rs[i], t) && w.held[l] != elements && !w.indexed[l]
		}
		if moved {
			w.keep(values[0])
			for _, l := range locals {
				w.hold(l, whole)
			}
		}
		return
	}
	for i, x := range values {
		if l, t, ok := at(i); ok {
			w.put(l, x, t)
			continue
		}
		ix, ok := targets[i].(*syntax.IndexRead)
		if !ok {
			continue
		}
		if l, ok := w.local(ix.X); ok {
			if elem := element(w.typeOf(l)); elem != nil && w.moves(x, elem) && w.hold(l, elements) {
				w.keep(x)
			}
		}
	}
}

// stmts walks a statement list, where a call of a reader whose result one local receives and the next statement computes on at once is waited for at its statement, and an inline handler that belongs to the future of its read takes the place of the declarations of its targets that stand in front of it.
func (w *pass) stmts(list []syntax.Stmt) {
	for i, s := range list {
		if d, ok := s.(*syntax.VarDecl); ok && i+1 < len(list) && len(d.Targets) == 1 && len(d.Values) == 1 {
			if c, ok := d.Values[0].(*syntax.Call); ok && w.p.Reader(c) && computesOn(first(list[i+1]), d.Targets[0].Name) {
				w.blocked[c] = true
			}
		}
		w.stmt(s)
		h, ok := s.(*syntax.Handled)
		if !ok {
			continue
		}
		st := w.supplied(h)
		if st == nil || i < len(st.Targets) {
			continue
		}
		decls := list[i-len(st.Targets) : i]
		var targets []*syntax.VarTarget
		for j, d := range decls {
			v, ok := d.(*syntax.VarDecl)
			t, plain := st.Targets[j].(*syntax.Name)
			if !ok || !plain || len(v.Values) != 0 || len(v.Targets) != 1 || v.Targets[0].Name != t.Name {
				targets = nil
				break
			}
			targets = append(targets, v.Targets[0])
		}
		if targets != nil {
			w.merged[st] = targets
			for _, d := range decls {
				w.dropped[d] = true
			}
		}
	}
}

// first returns the expression that a statement evaluates first when the statement has one value, or nil.
func first(s syntax.Stmt) syntax.Expr {
	switch s := s.(type) {
	case *syntax.VarDecl:
		if len(s.Values) == 1 {
			return s.Values[0]
		}
	case *syntax.Store:
		if len(s.Values) == 1 {
			return s.Values[0]
		}
	case *syntax.Return:
		if len(s.Values) == 1 {
			return s.Values[0]
		}
	}
	return nil
}

// computesOn reports whether an expression is a primitive operation over literals and plain names that include a local, so that its evaluation waits for the local before anything else happens.
func computesOn(x syntax.Expr, name string) bool {
	if _, ok := x.(*syntax.Prim); !ok {
		return false
	}
	uses, simple := false, true
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Prim, *syntax.Lit:
		case *syntax.Name:
			uses = uses || n.Name == name && n.Module == "" && n.Owner == nil
		default:
			simple = false
		}
		return simple
	})
	return uses && simple
}

// supplied returns the store of an inline handler whose read carries the arms of the handler, or nil.
func (w *pass) supplied(h *syntax.Handled) *syntax.Store {
	last := protectedStatement(h)
	st, ok := last.(*syntax.Store)
	if !ok || len(st.Values) != 1 || w.arms[st.Values[0]] == nil {
		return nil
	}
	return st
}

// protectedStatement returns the last statement of the one region that an inline handler protects, which is the statement that the handler was written on.
func protectedStatement(h *syntax.Handled) syntax.Stmt {
	blk, ok := h.Stmt.(*syntax.Block)
	if !ok || len(blk.Stmts) != 1 {
		return nil
	}
	region, ok := blk.Stmts[0].(*syntax.Block)
	if !ok || !region.Region || len(region.Stmts) == 0 {
		return nil
	}
	return region.Stmts[len(region.Stmts)-1]
}

// block marks the reads of the expressions of one statement that an effect of the same statement follows in the order of evaluation, because the routine waits for such a read before the effect.
func (w *pass) block(xs ...syntax.Expr) {
	var order []syntax.Node
	for _, x := range xs {
		if x != nil {
			syntax.Inspect(x, func(n syntax.Node) bool {
				if _, closure := n.(*syntax.Closure); closure {
					return false
				}
				order = append(order, n)
				return true
			})
		}
	}
	last := -1
	for i, n := range order {
		if x, ok := n.(syntax.Expr); ok && w.p.Marks.Effect(x) {
			last = i
		}
	}
	for i, n := range order {
		x, ok := n.(syntax.Expr)
		if !ok || i >= last || !w.p.Read(x) && !w.p.Reader(x) {
			continue
		}
		size := 0
		syntax.Inspect(n, func(syntax.Node) bool { size++; return true })
		if i+size <= last {
			w.blocked[x] = true
		}
	}
}

// stmt walks one statement of the program after phase A.
func (w *pass) stmt(s syntax.Stmt) {
	switch s := s.(type) {
	case *syntax.VarDecl:
		w.block(s.Values...)
		targets := make([]syntax.Expr, len(s.Targets))
		w.store(targets, s.Values, func(i int) (local, syntax.Type, bool) {
			t := s.Targets[i]
			return local{t, t.Name}, w.p.LocalType(t), true
		})
		w.exprs(s.Values, true)
	case *syntax.Store:
		w.block(append(slices.Clone(s.Targets), s.Values...)...)
		if len(s.Targets) == 1 && len(s.Values) == 1 {
			if y := w.appended(s.Targets[0], s.Values[0]); y != nil {
				l, _ := w.local(s.Targets[0])
				if elem := element(w.typeOf(l)); w.moves(y, elem) && w.hold(l, elements) {
					w.keep(y)
				}
				if w.held[l] == elements {
					w.kept[s.Values[0]], w.kept[s.Values[0].(*syntax.Call).Args[0].Value] = true, true
				}
				w.target(s.Targets[0])
				w.exprs(s.Values, false)
				return
			}
		}
		locals := true
		w.store(s.Targets, s.Values, func(i int) (local, syntax.Type, bool) {
			l, ok := w.local(s.Targets[i])
			locals = locals && ok
			if !ok {
				return l, nil, false
			}
			return l, w.typeOf(l), true
		})
		for _, t := range s.Targets {
			w.target(t)
		}
		w.exprs(s.Values, locals && len(s.Values) == 1)
	case *syntax.ExprStmt:
		w.block(s.X)
		if w.read(s.X) {
			w.keep(s.X)
		}
		w.exprs([]syntax.Expr{s.X}, true)
	case *syntax.Return:
		w.block(s.Values...)
		w.exprs(s.Values, false)
	case *syntax.Resume:
		w.exprs(s.Values, false)
	case *syntax.Block:
		w.stmts(s.Stmts)
	case *syntax.For:
		w.stmts(s.Body.Stmts)
	case *syntax.IterFor:
		w.block(s.Source)
		v := local{s, s.Vars[len(s.Vars)-1]}
		if l, ok := w.local(s.Source); ok && w.held[l] == elements && !w.fixed[v] {
			w.kept[s.Source] = true
			if w.held[v] != whole {
				w.held[v], w.grew = whole, true
			}
		}
		w.exprs([]syntax.Expr{s.Source}, false)
		w.stmts(s.Body.Stmts)
	case *syntax.Match:
		if s.Subject != nil {
			w.block(s.Subject)
			w.exprs([]syntax.Expr{s.Subject}, false)
		}
		for _, arm := range s.Arms {
			w.exprs(arm.Patterns, false)
			if arm.Body != nil {
				w.stmt(arm.Body)
			}
		}
	case *syntax.Handled:
		w.handled(s)
	}
}

// target marks the names of an assignment target that are no uses of a value: the local of a plain target, and the local collection of an element target.
func (w *pass) target(t syntax.Expr) {
	switch t := t.(type) {
	case *syntax.Name:
		w.kept[t] = true
	case *syntax.IndexRead:
		if l, ok := w.local(t.X); ok && w.held[l] == elements {
			w.kept[t], w.kept[t.X] = true, true
		}
		w.exprs([]syntax.Expr{t.X, t.Index}, false)
	case *syntax.FieldRead:
		w.exprs([]syntax.Expr{t.X}, false)
	}
}

// handled walks a handler on a block: the protected statements under the handler, and the arm bodies as code of an arm under the handlers outside, where an inline handler whose arms only supply or return known values belongs to the future of the read of its statement.
func (w *pass) handled(s *syntax.Handled) {
	blk, ok := s.Stmt.(*syntax.Block)
	if !ok {
		return
	}
	outer, orig := w.hs, w.orig
	w.belongs(s)
	w.hs = append([]handler{{arms: s.Arms}}, outer...)
	w.stmts(blk.Stmts)
	w.hs, w.orig = outer, &original{}
	for _, arm := range s.Arms {
		if arm.Body != nil {
			w.stmt(arm.Body)
		}
	}
	w.orig = orig
}

// belongs records the arms of an inline handler on the read of its statement when the handler belongs to the future of the read: the statement is a store of the results of one read into locals, or the read alone, each arm only supplies known values or only returns known values, and no handler outside selects a failure that the arms leave.
func (w *pass) belongs(s *syntax.Handled) {
	var read syntax.Expr
	var targets []syntax.Expr
	switch st := protectedStatement(s).(type) {
	case *syntax.Store:
		if len(st.Values) == 1 {
			read, targets = st.Values[0], st.Targets
		}
	case *syntax.ExprStmt:
		read = st.X
	}
	if read == nil || !w.inline[s] || w.sequential || !w.p.Read(read) && !w.p.Reader(read) {
		return
	}
	w.block(read)
	rs := w.p.Results(read)
	if w.blocked[read] || len(rs) != len(targets) {
		return
	}
	locals := make([]local, len(targets))
	for i, t := range targets {
		l, ok := w.local(t)
		if !ok || !w.holds(l) || w.indexed[l] || w.held[l] == elements || !check.Same(rs[i], w.typeOf(l)) {
			return
		}
		locals[i] = l
	}
	exclude := map[string]bool{}
	all := false
	var arms []*syntax.SendArm
	for _, arm := range s.Arms {
		out := &syntax.SendArm{Origin: arm.Origin, Type: arm.Type}
		out.Construct = syntax.SendArmNode
		switch arm.Kind {
		case syntax.TypedArm:
			exclude[w.b.errorKey(arm.Type)] = true
			all = all || w.b.errorKey(arm.Type) == "Error!"
		case syntax.AnyArm:
			all, out.Type = true, nil
		default:
			return
		}
		body, ok := arm.Body.(*syntax.Block)
		if !ok || len(body.Stmts) > 1 {
			return
		}
		want := len(rs)
		if len(body.Stmts) == 1 {
			switch st := body.Stmts[0].(type) {
			case *syntax.Resume:
				out.Values = st.Values
			case *syntax.Return:
				out.Values, out.Return, want = st.Values, true, len(w.results)
			default:
				return
			}
		}
		if len(out.Values) != want || len(body.Stmts) == 0 && want > 0 {
			return
		}
		for _, v := range out.Values {
			if !w.known(v) {
				return
			}
		}
		out.Values = copies(out.Values)
		arms = append(arms, out)
	}
	if w.b.selects(read, ctx{hs: w.hs, orig: w.orig}, exclude, all) {
		return
	}
	for _, l := range locals {
		if !w.hold(l, whole) {
			return
		}
	}
	w.arms[read] = arms
	w.keep(read)
}

// known reports whether a handler value is a known value: a literal, a negated literal, a collection literal of known values, a constant, or a local that holds no future, and no error.
func (w *pass) known(x syntax.Expr) bool {
	if t, ok := w.p.TypeOf(x).(*syntax.Named); ok && len(t.Name) > 1 && t.Name[len(t.Name)-1] == '!' {
		return false
	}
	switch x := x.(type) {
	case *syntax.Lit:
		return true
	case *syntax.Prim:
		return plain(x) && isLit(x.Args[0])
	case *syntax.ArrayLit:
		return !slices.ContainsFunc(x.Elems, func(e syntax.Expr) bool { return !w.known(e) })
	case *syntax.DictLit:
		return !slices.ContainsFunc(x.Entries, func(e *syntax.DictEntry) bool { return !w.known(e.Key) || !w.known(e.Value) })
	case *syntax.Name:
		if l, ok := w.local(x); ok {
			return w.held[l] == value
		}
		return w.p.Collection(x)
	}
	return false
}

// exprs walks the values of a statement, where whole reports that one call of a reader that is the whole value of the statement is handed to the runtime as a remote read is.
func (w *pass) exprs(xs []syntax.Expr, whole bool) {
	for _, x := range xs {
		if c, ok := x.(*syntax.Call); ok && (whole && len(xs) == 1 || w.kept[x]) && w.p.Reader(c) {
			w.sent[x] = true
		}
		w.expr(x)
	}
}

// expr walks an expression outside its closures and records each remote read as sent and each collection of futures that an operation reads without an element value as kept.
func (w *pass) expr(x syntax.Expr) {
	if x == nil {
		return
	}
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.Send:
			if c, ok := n.X.(*syntax.Call); ok {
				for _, a := range c.Args {
					w.expr(a.Value)
				}
			}
			if c, ok := n.X.(*syntax.ServiceCall); ok {
				for _, a := range c.Args {
					w.expr(a.Value)
				}
			}
			return false
		case *syntax.ServiceCall:
			w.sent[n] = w.sent[n] || w.p.Read(n)
		case *syntax.Call:
			if w.kept[n] && w.p.Reader(n) {
				w.sent[n] = true
			}
			if op := w.p.Operation(n); op != nil && op.Row.Futures && op.Row.Name != "append" && len(n.Args) == 1 {
				if l, ok := w.local(n.Args[0].Value); ok && w.held[l] == elements {
					w.kept[n.Args[0].Value] = true
				}
			}
		case *syntax.IndexRead:
			if l, ok := w.local(n.X); ok && w.held[l] == elements {
				if w.kept[n] || len(w.hs) == 0 && w.orig == nil {
					w.kept[n.X] = true
				} else {
					w.valued[n] = true
				}
			}
		}
		return true
	})
}

// futureType returns a written type under a holding: the type itself, the future of the type, or the collection type with futures as its elements.
func (fl *flights) futureType(t syntax.Type, h holding) syntax.Type {
	o := t.Where()
	o.Construct = syntax.FutureType
	switch h {
	case whole:
		return &syntax.Future{Origin: o, Elem: t}
	case elements:
		switch c := t.(type) {
		case *syntax.Array:
			return &syntax.Array{Origin: c.Origin, Elem: &syntax.Future{Origin: o, Elem: c.Elem}, Length: c.Length}
		case *syntax.Dict:
			return &syntax.Dict{Origin: c.Origin, Key: c.Key, Value: &syntax.Future{Origin: o, Elem: c.Value}}
		}
	}
	return t
}

// targets returns the targets of a declaration with the type of each under its holding.
func (fl *flights) targets(ts []*syntax.VarTarget) []*syntax.VarTarget {
	out := make([]*syntax.VarTarget, len(ts))
	for i, t := range ts {
		out[i] = t
		if w := fl.futureType(t.Type, fl.held[local{t, t.Name}]); w != t.Type {
			out[i] = &syntax.VarTarget{Origin: t.Origin, Type: w, Name: t.Name}
		}
	}
	return out
}

// rewrite returns the kernel form of a sub-expression that the rewrite of futures changes, with its operands rebuilt by inner: a read as a send, alone when its future stays in flight and under a wait otherwise, any other expression of a future type that does not stay a future under a wait, a parameter that holds a future as the local that stands for it, and a call that is an effect recorded as one.
func (fl *flights) rewrite(y syntax.Expr, inner func(syntax.Expr) syntax.Expr) (syntax.Expr, bool) {
	o := y.Where()
	if l, ok := fl.local(y); ok && fl.shadows[l] != "" {
		var out syntax.Expr = name(o, fl.shadows[l])
		if !fl.kept[y] {
			o.Construct = syntax.WaitExpr
			out = &syntax.Wait{Origin: o, X: out}
		}
		return out, true
	}
	switch {
	case fl.sent[y]:
		o.Construct = syntax.SendExpr
		var out syntax.Expr = &syntax.Send{Origin: o, X: inner(y), Arms: fl.arms[y]}
		if !fl.kept[y] {
			o.Construct = syntax.WaitExpr
			out = &syntax.Wait{Origin: o, X: out}
		}
		return out, true
	case !fl.kept[y] && !fl.valued[y] && (fl.future(y) != nil || fl.collection(y)):
		o.Construct = syntax.WaitExpr
		return &syntax.Wait{Origin: o, X: inner(y)}, true
	case fl.p.Marks.Effect(y):
		out := inner(y)
		fl.effects[out] = true
		return out, true
	}
	return nil, false
}

// collection reports whether an expression is a local collection of futures.
func (fl *flights) collection(x syntax.Expr) bool {
	l, ok := fl.local(x)
	return ok && fl.held[l] == elements
}

// effect reports whether a call of the program that phase B built is an effect.
func (fl *flights) effect(x syntax.Expr) bool { return fl.effects[x] || fl.p.Marks.Effect(x) }

// joined returns a body with a join in front of each statement that holds an effect or returns, and at its end, where a send reaches that point with no join between.
func (fl *flights) joined(body *syntax.Block) *syntax.Block {
	need := map[syntax.Stmt]bool{}
	end := false
	check.Reached(body, fl.effect, func(s syntax.Stmt) {
		if s == nil {
			end = true
		}
		need[s] = true
	})
	if len(need) == 0 {
		return body
	}
	out := block(body.Origin, joins(body.Stmts, need))
	if end {
		o := body.Origin
		o.Start = max(o.Start, o.End-1)
		out.Stmts = append(out.Stmts, join(o))
	}
	return out
}

// join returns a join at an origin.
func join(o syntax.Origin) syntax.Stmt {
	o.Construct = syntax.JoinStmt
	return &syntax.Join{Origin: o}
}

// joins returns a statement list with a join in front of each statement that needs one, in the list and in the statements that it holds.
func joins(list []syntax.Stmt, need map[syntax.Stmt]bool) []syntax.Stmt {
	out := make([]syntax.Stmt, 0, len(list))
	for _, s := range list {
		if need[s] {
			out = append(out, join(s.Where()))
		}
		out = append(out, within(s, need))
	}
	return out
}

// within returns a statement with the joins that the statements inside it need, where a loop keeps its node, because the transfers that phase B placed refer to it.
func within(s syntax.Stmt, need map[syntax.Stmt]bool) syntax.Stmt {
	switch s := s.(type) {
	case *syntax.Block:
		return &syntax.Block{Origin: s.Origin, Stmts: joins(s.Stmts, need), Region: s.Region}
	case *syntax.For:
		s.Body = within(s.Body, need).(*syntax.Block)
	case *syntax.IterFor:
		s.Body = within(s.Body, need).(*syntax.Block)
	case *syntax.Match:
		out := &syntax.Match{Origin: s.Origin, Subject: s.Subject, Arms: make([]*syntax.Arm, len(s.Arms))}
		for i, arm := range s.Arms {
			a := *arm
			switch {
			case arm.Body == nil:
			case need[arm.Body]:
				a.Body = block(arm.Body.Where(), joins([]syntax.Stmt{arm.Body}, need))
			default:
				a.Body = within(arm.Body, need)
			}
			out.Arms[i] = &a
		}
		return out
	}
	return s
}
