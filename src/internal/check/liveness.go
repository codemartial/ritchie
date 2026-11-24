// This file is the liveness pass of the ownership analysis: places as paths from a
// local or from self, and a backward pass over the statements of one routine or closure
// that finds, for each operand that can be consumed, whether a later read can reach
// the value that the operand reads.
package check

import (
	"fmt"
	"math/big"
	"slices"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// root is the local or the receiver that a place starts at, where a local is its declaration and its name.
type root struct {
	decl syntax.Node
	name string
	self bool
}

// step is one selection of a place: a field, an index or key that is a literal or a local, an index or key that is a computation over literals, locals, and counts of places, which has the text of the computation, or any other index or key, which has no field, literal, local, or text.
type step struct {
	field   string
	literal string
	local   root
	text    string
}

// place is a path of field and index selections from a root, where fixed reports that every index selects an element of an array or an entry of a dictionary by a literal, by a local, or by a computation, that no selection reads the value of a nullable, and that every value that the path selects from is of the deep copy class, so that two reads of the path select one place while no statement stores to those locals or changes a root that a computation reads, which reads lists, and the owner of the root owns the storage of the place, which a value of the flat copy class shares with its copies.
type place struct {
	root  root
	steps []step
	fixed bool
	reads []root
}

// rootOf returns the root of the local that a name reads or stores to.
func (p *Program) rootOf(x *syntax.Name) (root, bool) {
	b := p.bindings[x]
	if b == nil {
		b = p.stored[x]
	}
	if b == nil {
		return root{}, false
	}
	return root{decl: b.decl, name: b.name}, true
}

// placeOf returns the place that an expression reads or that a target stores to, or false for an expression that is no path from a local or from self.
func (p *Program) placeOf(x syntax.Expr) (place, bool) {
	at := place{fixed: true}
	for {
		at.fixed = at.fixed && !p.narrowed[x]
		switch y := x.(type) {
		case *syntax.Name:
			r, ok := p.rootOf(y)
			at.root = r
			slices.Reverse(at.steps)
			return at, ok
		case *syntax.Self:
			at.root = root{self: true}
			slices.Reverse(at.steps)
			return at, true
		case *syntax.FieldRead:
			at.fixed = at.fixed && p.Copy(p.types[y.X]) == builtin.Deep
			at.steps = append(at.steps, step{field: y.Name})
			x = y.X
		case *syntax.IndexRead:
			_, array := p.types[y.X].(*syntax.Array)
			s, reads := p.selection(y.Index, array)
			at.fixed = at.fixed && s != step{}
			at.steps = append(at.steps, s)
			at.reads = append(at.reads, reads...)
			x = y.X
		default:
			return place{}, false
		}
	}
}

// selection returns the step of an index of an array or of a key of a dictionary and the roots that its computation reads: a literal of an integer or of a string, a local, which as a key has a type without a part that a statement stores to, or a computation, which a local of any other type is.
func (p *Program) selection(x syntax.Expr, array bool) (step, []root) {
	switch x := x.(type) {
	case *syntax.Lit:
		switch x.Kind {
		case syntax.IntLit, syntax.StringLit, syntax.RawStringLit:
			return step{literal: x.Canonical()}, nil
		}
		return step{}, nil
	case *syntax.Name:
		if r, ok := p.rootOf(x); ok && (array || p.atom(p.types[x])) {
			return step{local: r}, nil
		}
	}
	text, reads, ok := p.computation(x)
	if !ok {
		return step{}, nil
	}
	return step{text: text}, reads
}

// atom reports whether a type is a built-in type of the flat copy class or an enum, whose values have no part that a statement stores to.
func (p *Program) atom(t syntax.Type) bool {
	if c, ok := builtin.Of(t); ok {
		return c.Copy == builtin.Flat
	}
	n, ok := t.(*syntax.Named)
	if !ok {
		return false
	}
	e := p.Table.Lookup(n.Module, n.Name)
	return e != nil && e.Kind == resolve.EnumKind
}

// computation returns a text that two computations share only when they yield one value while no statement changes a root that they read, and those roots, for an expression of integer literals, locals and their fields that hold no future, primitive operators, and counts of places.
func (p *Program) computation(x syntax.Expr) (string, []root, bool) {
	switch x := x.(type) {
	case *syntax.Lit:
		return x.Canonical(), nil, x.Kind == syntax.IntLit
	case *syntax.Name:
		r, ok := p.rootOf(x)
		return fmt.Sprintf("%s@%p", r.name, r.decl), []root{r}, ok && !holdsFuture(p.types[x])
	case *syntax.FieldRead:
		text := ""
		for y := syntax.Expr(x); ; {
			f, ok := y.(*syntax.FieldRead)
			if !ok || p.narrowed[y] {
				at, ok := p.placeOf(y)
				return fmt.Sprintf("%s@%p,%t%s", at.root.name, at.root.decl, at.root.self, text), []root{at.root}, ok && len(at.steps) == 0 && !p.narrowed[y] && !holdsFuture(p.types[x])
			}
			text, y = "."+f.Name+text, f.X
		}
	case *syntax.Prim:
		text, reads := x.Op+"(", []root(nil)
		for _, a := range x.Args {
			t, r, ok := p.computation(a)
			if !ok {
				return "", nil, false
			}
			text, reads = text+t+",", append(reads, r...)
		}
		return text + ")", reads, true
	case *syntax.Call:
		k := p.callees[x]
		if k == nil || k.op == nil || k.op.Row.Name != "count" || len(x.Args) != 1 {
			return "", nil, false
		}
		at, ok := p.placeOf(x.Args[0].Value)
		if !ok || !at.fixed {
			return "", nil, false
		}
		text, reads := fmt.Sprintf("count(%s@%p,%t", at.root.name, at.root.decl, at.root.self), append(at.reads, at.root)
		for _, s := range at.steps {
			text += fmt.Sprintf(".%s[%s|%s@%p|%s]", s.field, s.literal, s.local.name, s.local.decl, s.text)
			if s.local != (root{}) {
				reads = append(reads, s.local)
			}
		}
		return text + ")", reads, true
	}
	return "", nil, false
}

// indexes returns the index and key operands of a path of field and index selections, outermost first.
func indexes(x syntax.Expr) []syntax.Expr {
	var out []syntax.Expr
	for {
		switch y := x.(type) {
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			out = append(out, y.Index)
			x = y.X
		default:
			return out
		}
	}
}

// same reports whether two selections select one part when both are evaluated while no statement stores to their locals.
func (s step) same(t step) bool {
	return s == t && s != step{}
}

// meets reports whether two selections can select one part.
func (s step) meets(t step) bool {
	if s.field != "" || t.field != "" {
		return s.field == t.field
	}
	return s.literal == "" || t.literal == "" || s.literal == t.literal
}

// covers reports whether a store to the place replaces the whole value of another place.
func (at place) covers(other place) bool {
	if at.root != other.root || len(at.steps) > len(other.steps) {
		return false
	}
	for i, s := range at.steps {
		if !s.same(other.steps[i]) {
			return false
		}
	}
	return true
}

// within reports whether the place can be another place or a part of it.
func (at place) within(other place) bool {
	if at.root != other.root || len(at.steps) < len(other.steps) {
		return false
	}
	for i, s := range other.steps {
		if !s.meets(at.steps[i]) {
			return false
		}
	}
	return true
}

// overlaps reports whether a read of one place can read storage of the other.
func (at place) overlaps(other place) bool {
	return at.within(other) || other.within(at)
}

// operands returns the expressions that the evaluation of an expression evaluates, in written order, where a closure has none, because its body runs when it is called.
func operands(x syntax.Expr) []syntax.Expr {
	values := func(first []syntax.Expr, args []*syntax.Arg) []syntax.Expr {
		for _, a := range args {
			first = append(first, a.Value)
		}
		return first
	}
	switch x := x.(type) {
	case *syntax.Prim:
		return x.Args
	case *syntax.Logical:
		return []syntax.Expr{x.Left, x.Right}
	case *syntax.Format:
		var out []syntax.Expr
		for _, part := range x.Parts {
			if part.Slot != nil {
				out = append(out, part.Slot)
			}
		}
		return out
	case *syntax.Call:
		return values([]syntax.Expr{x.Callee}, x.Args)
	case *syntax.MethodCall:
		return values([]syntax.Expr{x.Receiver}, x.Args)
	case *syntax.ServiceCall:
		return values(nil, x.Args)
	case *syntax.QueryCall:
		return values(nil, x.Args)
	case *syntax.Create:
		return values(nil, x.Args)
	case *syntax.Lookup:
		return []syntax.Expr{x.Value}
	case *syntax.Construct:
		return values(nil, x.Args)
	case *syntax.ArrayLit:
		return x.Elems
	case *syntax.DictLit:
		var out []syntax.Expr
		for _, en := range x.Entries {
			out = append(out, en.Key, en.Value)
		}
		return out
	case *syntax.FieldRead:
		return []syntax.Expr{x.X}
	case *syntax.IndexRead:
		return []syntax.Expr{x.X, x.Index}
	case *syntax.Slice:
		return []syntax.Expr{x.X, x.Lo, x.Hi}
	case *syntax.Send:
		out := []syntax.Expr{x.X}
		for _, arm := range x.Arms {
			out = append(out, arm.Values...)
		}
		return out
	case *syntax.Wait:
		return []syntax.Expr{x.X}
	}
	return nil
}

// effect is what the evaluation of one statement does to the sites of its unit: the sites whose place it reads, the sites whose place it stores as a whole, the sites whose storage one of its stores writes into after its values are evaluated, the sites whose place holds an index local that it stores, the sites that the candidates of a match read after its subject, and whether an operation of the statement can fail.
type effect struct {
	reads  *big.Int
	stores *big.Int
	into   *big.Int
	stale  *big.Int
	later  *big.Int
	fails  bool
}

// span is one loop of the pass with the sites that are live where a break and where a continue of the loop arrives.
type span struct {
	label string
	exit  *big.Int
	next  *big.Int
}

// liveness is the liveness pass of one unit: the sites that are live when the unit ends, the effect of each statement, and the sites that are live after each statement evaluates its values and after its evaluation fails.
type liveness struct {
	u       *unit
	end     *big.Int
	effects map[syntax.Node]*effect
	after   map[syntax.Node]*big.Int
	failed  map[syntax.Node]*big.Int
	loops   []*span
}

func union(sets ...*big.Int) *big.Int {
	out := new(big.Int)
	for _, s := range sets {
		out.Or(out, s)
	}
	return out
}

func minus(a, b *big.Int) *big.Int { return new(big.Int).AndNot(a, b) }

// run computes the sets of the pass, where a site is live when the unit ends if its root is self or a local that a closure captured, because the caller observes the receiver and the closure keeps its locals for its next call.
func (u *unit) run() *liveness {
	lv := &liveness{u: u, end: new(big.Int), effects: map[syntax.Node]*effect{}, after: map[syntax.Node]*big.Int{}, failed: map[syntax.Node]*big.Int{}}
	for i, s := range u.sites {
		if s.at.root.self || u.captured(s.at.root) {
			lv.end.SetBit(lv.end, i, 1)
		}
	}
	lv.list(u.body, lv.end)
	return lv
}

// list returns the sites that are live before a statement list from those that are live after it.
func (lv *liveness) list(stmts []syntax.Stmt, out *big.Int) *big.Int {
	for _, s := range slices.Backward(stmts) {
		out = lv.stmt(s, out)
	}
	return out
}

// stmt returns the sites that are live before a statement from those that are live after it, and records the sets of the statement, where an iterator loop over a place reads the place at the start of each visit.
func (lv *liveness) stmt(s syntax.Stmt, out *big.Int) *big.Int {
	switch s := s.(type) {
	case *syntax.Block:
		return lv.list(s.Stmts, out)
	case *syntax.Break:
		return lv.target(s.Label).exit
	case *syntax.Continue:
		return lv.target(s.Label).next
	case *syntax.Join:
		return union(out, lv.end)
	case *syntax.For:
		l := &span{label: s.Label, exit: out, next: new(big.Int)}
		lv.loops = append(lv.loops, l)
		for {
			in := lv.list(s.Body.Stmts, l.next)
			if minus(in, l.next).Sign() == 0 {
				break
			}
			l.next = union(l.next, in)
		}
		lv.loops = lv.loops[:len(lv.loops)-1]
		return l.next
	case *syntax.IterFor:
		e := lv.effect(s)
		l := &span{label: s.Label, exit: out, next: out}
		if at, ok := lv.u.o.p.placeOf(s.Source); ok {
			visit := &effect{reads: new(big.Int)}
			lv.read(at, visit)
			l.next = union(out, visit.reads)
		}
		lv.loops = append(lv.loops, l)
		for {
			head := union(minus(lv.list(s.Body.Stmts, l.next), e.stores), e.stale)
			if minus(head, l.next).Sign() == 0 {
				break
			}
			l.next = union(l.next, head)
		}
		lv.loops = lv.loops[:len(lv.loops)-1]
		lv.after[s], lv.failed[s] = l.next, lv.end
		return union(e.reads, l.next, lv.failure(e))
	case *syntax.Match:
		e := lv.effect(s)
		ok, bad := e.later, lv.end
		if !lv.u.o.exhaustive(s) {
			ok = union(ok, out)
		}
		for _, arm := range s.Arms {
			a := lv.effect(arm)
			in := union(minus(lv.list(stmtsOf(arm.Body), out), a.stores), a.stale)
			if lv.u.o.catches(arm) {
				bad = union(bad, in)
			} else {
				ok = union(ok, in)
			}
		}
		lv.after[s], lv.failed[s] = ok, bad
		if !e.fails {
			bad = minus(bad, lv.end)
		}
		return union(e.reads, ok, bad)
	case *syntax.Return:
		out = lv.end
	}
	e := lv.effect(s)
	rest := minus(out, e.stores)
	lv.after[s], lv.failed[s] = union(rest, e.into), lv.failure(e)
	return union(e.reads, e.stale, rest, lv.failed[s])
}

// failure returns the sites that are live when a statement of an effect fails, which leaves the unit.
func (lv *liveness) failure(e *effect) *big.Int {
	if e.fails {
		return lv.end
	}
	return new(big.Int)
}

// target returns the loop that a break or a continue with a label leaves or continues, which is the innermost loop for an empty label.
func (lv *liveness) target(label string) *span {
	for _, l := range slices.Backward(lv.loops) {
		if label == "" || l.label == label {
			return l
		}
	}
	panic("check: a break or a continue stands outside every loop")
}

// effect returns the effect of a statement, of the subject and candidates of a match, of the source and variables of an iterator loop, or of the bindings of an arm.
func (lv *liveness) effect(n syntax.Node) *effect {
	if e := lv.effects[n]; e != nil {
		return e
	}
	e := &effect{reads: new(big.Int), stores: new(big.Int), into: new(big.Int), stale: new(big.Int), later: new(big.Int)}
	lv.effects[n] = e
	switch n := n.(type) {
	case *syntax.VarDecl:
		lv.exprs(n.Values, e)
		for _, t := range n.Targets {
			lv.store(place{root: root{decl: t, name: t.Name}}, e)
		}
	case *syntax.ConstDecl:
		lv.expr(n.Value, e)
	case *syntax.Store:
		for _, t := range n.Targets {
			if at, ok := lv.u.o.p.placeOf(t); ok {
				lv.store(at, e)
			}
			lv.exprs(indexes(t), e)
		}
		lv.exprs(n.Values, e)
	case *syntax.ExprStmt:
		lv.expr(n.X, e)
	case *syntax.Return:
		lv.exprs(n.Values, e)
	case *syntax.Match:
		lv.expr(n.Subject, e)
		later := &effect{reads: e.later}
		for _, arm := range n.Arms {
			lv.exprs(arm.Patterns, later)
		}
		e.fails = e.fails || later.fails
	case *syntax.IterFor:
		lv.expr(n.Source, e)
		for _, v := range n.Vars {
			lv.store(place{root: root{decl: n, name: v}}, e)
		}
	case *syntax.Arm:
		if n.Binding != "" {
			lv.store(place{root: root{decl: n, name: n.Binding}}, e)
		}
		for _, t := range n.Targets {
			lv.store(place{root: root{decl: t, name: t.Name}}, e)
		}
	}
	return e
}

// store records a store to a place: the store replaces each site that the place covers, it writes into the storage of each site that holds the array or dictionary of the last index of the place, which is a read of that site after the values of the statement, it can fail when the place has an index, and it makes each site stale whose place selects by the local that it stores as a whole or by a computation that reads its root.
func (lv *liveness) store(at place, e *effect) {
	last := -1
	for i, s := range at.steps {
		if s.field == "" {
			last = i
		}
	}
	e.fails = e.fails || last >= 0
	for i, s := range lv.u.sites {
		if at.covers(s.at) {
			e.stores.SetBit(e.stores, i, 1)
		}
		if last >= 0 && (place{root: at.root, steps: at.steps[:last]}).within(s.at) {
			e.into.SetBit(e.into, i, 1)
			e.reads.SetBit(e.reads, i, 1)
		}
		if len(at.steps) == 0 && slices.ContainsFunc(s.at.steps, func(t step) bool { return t.local == at.root }) {
			e.stale.SetBit(e.stale, i, 1)
		}
	}
	lv.change(at.root, e)
}

// change records that a statement changes a root: each site is stale whose place selects by a computation that reads the root.
func (lv *liveness) change(r root, e *effect) {
	for i, s := range lv.u.sites {
		if slices.Contains(s.at.reads, r) {
			e.stale.SetBit(e.stale, i, 1)
		}
	}
}

// exprs records the reads of each expression of a list.
func (lv *liveness) exprs(xs []syntax.Expr, e *effect) {
	for _, x := range xs {
		lv.expr(x, e)
	}
}

// expr records the reads of an expression: each site that a place of the expression overlaps, and each site at a local or at self that a closure of the expression captures.
func (lv *liveness) expr(x syntax.Expr, e *effect) {
	p := lv.u.o.p
	if x == nil {
		return
	}
	if at, ok := p.placeOf(x); ok {
		lv.read(at, e)
		index := indexes(x)
		e.fails = e.fails || len(index) > 0
		lv.exprs(index, e)
		return
	}
	switch x := x.(type) {
	case *syntax.Closure:
		for _, r := range p.captures(x) {
			lv.read(place{root: r}, e)
		}
		return
	case *syntax.Lit, *syntax.Name, *syntax.Logical, *syntax.Construct, *syntax.ArrayLit, *syntax.DictLit, *syntax.FieldRead:
	case *syntax.MethodCall:
		if r, ok := lv.u.o.start(x.Receiver); ok && lv.u.o.mutates(x) {
			lv.change(r, e)
		}
		e.fails = true
	default:
		e.fails = true
	}
	lv.exprs(operands(x), e)
}

// read records a read of a place.
func (lv *liveness) read(at place, e *effect) {
	for i, s := range lv.u.sites {
		if at.overlaps(s.at) {
			e.reads.SetBit(e.reads, i, 1)
		}
	}
}

// captures returns the roots that the body of a closure names, which are the locals and the receiver that its creation copies and the locals that the closure declares.
func (p *Program) captures(x *syntax.Closure) []root {
	var out []root
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Name:
			if r, ok := p.rootOf(n); ok {
				out = append(out, r)
			}
		case *syntax.Self:
			out = append(out, root{self: true})
		}
		return true
	})
	return out
}
