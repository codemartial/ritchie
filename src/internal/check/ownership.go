// This file is the ownership analysis: it decides, over a kernel program, how each
// operand in a value position reaches the receiver of its value, and records the
// modes in a table beside the program.
package check

import (
	"slices"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// Mode is how the value of an operand in a value position reaches its receiver: as an independent copy, as the operand itself while both holders stay live and no statement writes either, as the operand itself while the receiver does not write it for the extent of a call or a loop, or as the operand itself because no later read reaches the value.
type Mode uint8

const (
	Copy Mode = iota
	Share
	Borrow
	Consume
)

// Modes is the table of operand modes beside a kernel program: the mode of each operand that reads a place, the iterator loops whose copied variable refers to its element, and the iterator loops that walk an independent copy of their source, because the body, or a method that the body calls on a part of the source path, changes the source in place.
type Modes struct {
	operands  map[syntax.Expr]Mode
	refers    map[*syntax.IterFor]bool
	snapshots map[*syntax.IterFor]bool
}

// Of returns the mode of an operand, which is Copy for an operand that the table does not hold.
func (m *Modes) Of(x syntax.Expr) Mode {
	if m == nil {
		return Copy
	}
	return m.operands[x]
}

// Refers reports whether the copied variable of an iterator loop refers to its element.
func (m *Modes) Refers(s *syntax.IterFor) bool { return m != nil && m.refers[s] }

// Snapshot reports whether an iterator loop walks an independent copy of its source.
func (m *Modes) Snapshot(s *syntax.IterFor) bool { return m != nil && m.snapshots[s] }

// owner is the state of the ownership analysis of one kernel program: the table, the parameters of each routine that its body leaves unmutated, the units, the unit of each routine, and the iterator loops over a place.
type owner struct {
	p       *Program
	modes   *Modes
	lent    map[*syntax.Func][]bool
	units   []*unit
	routine map[*syntax.Func]*unit
	loops   []*hold
}

// unit is one routine or closure under analysis: the routine, which encloses the closure of a closure unit, its statements, whether self is a struct that the routine can change in place, the declarations of a closure, the operands that can be consumed, the results that can be, the declarations that can share their value, and the iterator loops that enclose the statement being recorded.
type unit struct {
	o        *owner
	fn       *syntax.Func
	closure  *syntax.Closure
	body     []syntax.Stmt
	self     bool
	declared map[syntax.Node]bool
	sites    []*site
	results  []result
	shares   []*syntax.VarDecl
	held     []*hold
}

// result is a call of a built-in with an in-place function whose result is the first operand of another such call, where last reports that the other call is the one value of its statement and cannot fail, so that no operation of the statement fails after it.
type result struct {
	x    syntax.Expr
	last bool
}

// statement is the evaluation that holds a site: a statement, the subject of a match, or the source of an iterator loop, with the values that it evaluates and the index operands of its targets, which it evaluates before them.
type statement struct {
	node   syntax.Node
	values []syntax.Expr
	before []syntax.Expr
}

// site is one operand that reads a fixed place of a root that its unit owns: the operation that takes it, or nil when its statement does, whether it is the first operand of a built-in with an in-place function, whether that built-in call is the one value of its statement, so that its failure leaves the storage as it was and no later operation of the statement can fail, whether the in-place function leaves every element of the operand as it was, whether a read reaches the operand when its statement fails, the enclosing iterator loops whose source the place can hold, which walk an independent copy when the operand is consumed, whether an enclosing iterator loop bars the consumption, because it walks futures or its variable refers to an element that the place lies in, and whether the analysis found it consumed.
type site struct {
	x       syntax.Expr
	at      place
	in      *statement
	call    syntax.Expr
	inPlace bool
	whole   bool
	keeps   bool
	failing bool
	held    []*hold
	bound   bool
	taken   bool
}

// hold is an iterator loop whose source is a place, which the loop holds for its extent, with whether the loop walks a collection of futures, and, once settled, whether its body can change a part of the collection before it replaces the source.
type hold struct {
	s       *syntax.IterFor
	at      place
	futures bool
	settled bool
	early   bool
}

// Ownership returns the table of operand modes of a kernel program.
func (p *Program) Ownership() *Modes {
	o := &owner{p: p, lent: map[*syntax.Func][]bool{}, routine: map[*syntax.Func]*unit{},
		modes: &Modes{operands: map[syntax.Expr]Mode{}, refers: map[*syntax.IterFor]bool{}, snapshots: map[*syntax.IterFor]bool{}}}
	for _, f := range p.Files {
		for _, d := range f.Decls {
			if fn, ok := d.(*syntax.Func); ok && fn.Body != nil {
				o.add(fn, nil, fn.Body.Stmts)
				o.closures(fn, fn.Body)
			}
		}
	}
	for _, u := range o.units {
		u.stmts(u.body)
		lv := u.run()
		for i, s := range u.sites {
			s.failing = lv.failed[s.in.node].Bit(i) == 1
			live := lv.after[s.in.node].Bit(i) == 1 || s.failing && !(s.inPlace && (s.whole || s.keeps))
			s.taken = !live && !s.bound && u.clean(s) && !slices.ContainsFunc(s.held, o.early)
		}
		u.share()
	}
	o.conflicts()
	for _, u := range o.units {
		for _, s := range u.sites {
			if !s.taken {
				continue
			}
			o.modes.operands[s.x] = Consume
			for _, l := range s.held {
				o.modes.snapshots[l.s] = true
			}
		}
		for _, r := range slices.Backward(u.results) {
			if s := u.source(r.x); s == nil || !s.failing || r.last {
				o.modes.operands[r.x] = Consume
			}
		}
	}
	return o.modes
}

// source returns the consumed site whose storage the result of a call of a built-in with an in-place function holds, or nil when the result is independent storage.
func (u *unit) source(x syntax.Expr) *site {
	for {
		first := u.first(x)
		if i := slices.IndexFunc(u.sites, func(s *site) bool { return s.x == first }); first != nil && i >= 0 && u.sites[i].taken {
			return u.sites[i]
		}
		if first == nil || u.takes(first) || u.o.modes.operands[first] != Consume {
			return nil
		}
		x = first
	}
}

// first returns the first operand of a call of a built-in with an in-place function or of a slice, or nil for any other expression.
func (u *unit) first(x syntax.Expr) syntax.Expr {
	k := u.o.p.callees[x]
	if k == nil || k.op == nil || k.op.Row.InPlace == nil {
		return nil
	}
	switch x := x.(type) {
	case *syntax.Call:
		return x.Args[0].Value
	case *syntax.Slice:
		return x.X
	}
	return nil
}

// add records a unit, where self is a struct that the unit can change in place in an instance method of a struct and in a closure of one.
func (o *owner) add(fn *syntax.Func, closure *syntax.Closure, body []syntax.Stmt) {
	en := o.p.Table.Of(fn)
	u := &unit{o: o, fn: fn, closure: closure, body: body}
	u.self = en.Owner != nil && en.Owner.Kind == resolve.StructKind && en.Level != resolve.TypeLevel
	o.units = append(o.units, u)
	if closure == nil {
		o.routine[fn] = u
		return
	}
	u.declared = map[syntax.Node]bool{}
	syntax.Inspect(closure, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.Param, *syntax.VarTarget, *syntax.ConstDecl, *syntax.IterFor, *syntax.Arm:
			u.declared[n] = true
		}
		return true
	})
}

// closures records a unit for each closure of a routine, where a closure with an expression body returns that expression.
func (o *owner) closures(fn *syntax.Func, body syntax.Node) {
	syntax.Inspect(body, func(n syntax.Node) bool {
		c, ok := n.(*syntax.Closure)
		switch {
		case !ok:
		case c.Body != nil:
			o.add(fn, c, c.Body.Stmts)
		default:
			o.add(fn, c, []syntax.Stmt{&syntax.Return{Origin: c.Origin, Values: []syntax.Expr{c.Expr}}})
		}
		return true
	})
}

// captured reports whether a root is a local that a closure unit copied at its creation.
func (u *unit) captured(r root) bool {
	return u.closure != nil && !r.self && !u.declared[r.decl]
}

// constant reports whether a root is a local constant, which no statement writes.
func constant(r root) bool {
	switch r.decl.(type) {
	case *syntax.ConstDecl:
		return true
	case *syntax.VarTarget:
		return syntax.ClassifyBare(r.name) == syntax.Constant
	}
	return false
}

// owned reports whether a unit owns the storage of a root, so that a read of a place at the root can be consumed: self in a unit that can change it in place, a local that a closure captured, a parameter that the routine mutates, the copied variable of an iterator loop that does not refer to its element, and every other local that is no constant.
func (u *unit) owned(r root) bool {
	switch {
	case r.self:
		return u.self
	case constant(r):
		return false
	case u.captured(r):
		return true
	}
	switch d := r.decl.(type) {
	case *syntax.Param:
		i := slices.Index(u.fn.Params, d)
		return u.closure != nil || i < 0 || !u.o.lends(u.fn)[i]
	case *syntax.IterFor:
		return r.name == copied(u.o.p, d) && !u.o.modes.refers[d]
	}
	return true
}

// copied returns the variable of an iterator loop that receives a copy of an element at each visit: the variable of a loop over an array, the last variable of a loop over a dictionary, and none for any other source.
func copied(p *Program, s *syntax.IterFor) string {
	switch p.types[s.Source].(type) {
	case *syntax.Array:
		return s.Vars[0]
	case *syntax.Dict:
		return s.Vars[len(s.Vars)-1]
	}
	return ""
}

// deep reports whether a transfer of a value of a type copies storage and the type holds no future, which is moved.
func (p *Program) deep(t syntax.Type) bool {
	return t != nil && p.Copy(t) == builtin.Deep && !holdsFuture(t)
}

// mutates reports whether a method call runs a method that can change the place of its receiver: a call of an instance method, or a call whose callee is not known.
func (o *owner) mutates(x *syntax.MethodCall) bool {
	k := o.p.callees[x]
	switch {
	case k == nil:
		return true
	case k.op != nil:
		return false
	}
	return k.fn == nil || o.p.Table.Of(k.fn).Level != resolve.TypeLevel
}

// start returns the root of a path of field and index selections.
func (o *owner) start(x syntax.Expr) (root, bool) {
	at, ok := o.p.placeOf(x)
	return at.root, ok
}

// writes reports whether a node holds a store to a place of a root or a call of a method that can change one.
func (o *owner) writes(n syntax.Node, r root) bool {
	found := false
	syntax.Inspect(n, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Store:
			for _, t := range n.Targets {
				if tr, ok := o.start(t); ok && tr == r {
					found = true
				}
			}
		case *syntax.MethodCall:
			if tr, ok := o.start(n.Receiver); ok && tr == r && o.mutates(n) {
				found = true
			}
		}
		return !found
	})
	return found
}

// lends returns for each parameter of a routine whether its body leaves the parameter unmutated, so that a caller passes its argument without a copy and the routine does not own the storage of the parameter.
func (o *owner) lends(fn *syntax.Func) []bool {
	if out, ok := o.lent[fn]; ok {
		return out
	}
	out := make([]bool, len(fn.Params))
	routine := o.p.Table.Of(fn).Routine
	if fn.Body != nil && (routine == resolve.FreeRoutine || routine == resolve.StructMethod) {
		for i, param := range fn.Params {
			out[i] = o.p.deep(o.p.Table.Type(param.Type)) && !o.writes(fn.Body, root{decl: param, name: param.Name})
		}
	}
	o.lent[fn] = out
	return out
}

// borrows reports whether a call of a routine passes the argument at a position without a copy: the routine leaves the parameter unmutated, the argument is a place at a local, at a constant whose value is a collection, or at self in a unit that can change it in place, the receiver of the call does not start at the root of the argument, and no other operand of the call writes that root.
func (u *unit) borrows(i int, xs []syntax.Expr, fn *syntax.Func, recv syntax.Expr) bool {
	o := u.o
	if i >= len(fn.Params) || !o.lends(fn)[i] {
		return false
	}
	r, ok := o.start(xs[i])
	switch {
	case !ok:
		return o.p.ReadsConstant(xs[i])
	case r.self && !u.self:
		return false
	}
	if recv != nil {
		if rr, ok := o.start(recv); ok && rr == r || o.writes(recv, r) {
			return false
		}
	}
	for j, x := range xs {
		if j != i && o.writes(x, r) {
			return false
		}
	}
	return true
}

// stmts records the sites, the borrows, and the loops of a statement list.
func (u *unit) stmts(list []syntax.Stmt) {
	for _, s := range list {
		u.stmt(s)
	}
}

// stmt records the sites, the borrows, and the loops of one statement.
func (u *unit) stmt(s syntax.Stmt) {
	p := u.o.p
	switch s := s.(type) {
	case *syntax.VarDecl:
		u.values(&statement{node: s, values: s.Values})
		if len(s.Targets) == 1 && len(s.Values) == 1 {
			u.shares = append(u.shares, s)
		}
	case *syntax.ConstDecl:
		u.values(&statement{node: s, values: []syntax.Expr{s.Value}})
	case *syntax.Store:
		in := &statement{node: s, values: s.Values}
		for _, t := range s.Targets {
			in.before = append(in.before, indexes(t)...)
		}
		u.values(in)
	case *syntax.ExprStmt:
		u.walk(s.X, &statement{node: s, values: []syntax.Expr{s.X}}, true)
	case *syntax.Return:
		u.values(&statement{node: s, values: s.Values})
	case *syntax.Block:
		u.stmts(s.Stmts)
	case *syntax.For:
		u.stmts(s.Body.Stmts)
	case *syntax.Match:
		if s.Subject != nil {
			in := &statement{node: s, values: []syntax.Expr{s.Subject}}
			_, reads := p.placeOf(s.Subject)
			switch {
			case !u.o.binds(s):
			case reads:
				u.transfer(s.Subject, in, nil)
			default:
				u.o.modes.operands[s.Subject] = Consume
			}
			u.walk(s.Subject, in, true)
		}
		for _, arm := range s.Arms {
			for _, c := range arm.Patterns {
				u.walk(c, nil, false)
			}
			u.stmts(stmtsOf(arm.Body))
		}
	case *syntax.IterFor:
		u.walk(s.Source, &statement{node: s, values: []syntax.Expr{s.Source}}, true)
		at, ok := p.placeOf(s.Source)
		if v := copied(p, s); v != "" && !Futures(p.types[s.Source]) {
			r := root{decl: s, name: v}
			u.o.modes.refers[s] = !u.o.writes(s.Body, r) && (!ok || (!at.root.self || u.self) && !u.o.writes(s.Body, at.root))
		}
		if ok {
			l := &hold{s: s, at: at, futures: Futures(p.types[s.Source])}
			u.o.loops = append(u.o.loops, l)
			u.held = append(u.held, l)
		}
		u.stmts(s.Body.Stmts)
		if ok {
			u.held = u.held[:len(u.held)-1]
		}
	}
}

// values records the sites of the values of a statement, where the statement takes every value, and the sites of the operations of those values.
func (u *unit) values(in *statement) {
	for _, v := range in.values {
		u.transfer(v, in, nil)
		u.walk(v, in, len(in.values) == 1)
	}
	for _, x := range in.before {
		u.walk(x, in, false)
	}
}

// walk records the sites and the borrows of the operations of an expression, where whole reports that the expression is the one value of its statement, or the one operand of such a value that is a call of a built-in with an in-place function that cannot fail: the first operand of a built-in with an in-place function is a site, every other operand of a built-in that reads a place is borrowed, an argument of a routine is borrowed or is a site, and an argument of a function value, of a construction, and of a collection literal is a site, while a candidate of a match and an expression without a statement hold no site, a send holds none and copies each argument of its call, because its read can run ahead of the statements in front of it, and a closure is a unit of its own.
func (u *unit) walk(x syntax.Expr, in *statement, whole bool) {
	args := func(args []*syntax.Arg) []syntax.Expr {
		out := make([]syntax.Expr, len(args))
		for i, a := range args {
			out[i] = a.Value
		}
		return out
	}
	switch x := x.(type) {
	case nil, *syntax.Closure:
		return
	case *syntax.Send:
		for _, y := range append(operands(x.X), operands(x)[1:]...) {
			u.walk(y, nil, false)
		}
		return
	case *syntax.Call:
		u.call(x, nil, args(x.Args), in, whole)
	case *syntax.MethodCall:
		u.call(x, x.Receiver, args(x.Args), in, whole)
	case *syntax.Slice:
		u.call(x, nil, []syntax.Expr{x.X, x.Lo, x.Hi}, in, whole)
	case *syntax.Construct:
		for _, a := range x.Args {
			u.transfer(a.Value, in, x)
		}
	case *syntax.ArrayLit, *syntax.DictLit:
		for _, v := range operands(x) {
			u.transfer(v, in, x)
		}
	}
	for _, y := range operands(x) {
		u.walk(y, in, whole && y == u.sole(x))
	}
}

// sole returns the one operand of a call of a built-in with an in-place function that cannot fail, after which no operation of the call can fail, or nil for any other expression.
func (u *unit) sole(x syntax.Expr) syntax.Expr {
	c, ok := x.(*syntax.Call)
	if !ok || u.first(x) == nil || len(c.Args) != 1 || len(u.o.p.callees[x].op.Row.Faults) > 0 {
		return nil
	}
	return c.Args[0].Value
}

// call records the sites, the results, and the borrows of the operands of a call of a built-in, of a slice, of a routine, or of a function value, where recv is the receiver of a method call.
func (u *unit) call(x, recv syntax.Expr, args []syntax.Expr, in *statement, whole bool) {
	p := u.o.p
	k := p.callees[x]
	switch {
	case k == nil:
	case k.op != nil:
		if recv != nil {
			args = append([]syntax.Expr{recv}, args...)
		}
		row := k.op.Row
		for i, a := range args {
			_, ok := p.placeOf(a)
			switch {
			case a == nil || holdsFuture(p.types[args[0]]) && i > 0:
			case !ok:
				if i == 0 && in != nil && u.first(x) != nil && u.first(a) != nil {
					u.results = append(u.results, result{a, whole && u.sole(x) != nil})
				}
			case i > 0 || row.InPlace == nil || !u.site(&site{x: a, in: in, call: x, inPlace: true, whole: whole, keeps: row.Keeps && !holdsFuture(p.types[a])}):
				u.o.modes.operands[a] = Borrow
			}
		}
	case k.fn == nil:
		for _, a := range args {
			u.transfer(a, in, x)
		}
	default:
		en := p.Table.Of(k.fn)
		if en.Level == resolve.TypeLevel {
			recv = nil
		}
		for i, a := range args {
			switch {
			case u.borrows(i, args, k.fn, recv):
				u.o.modes.operands[a] = Borrow
			case en.Routine == resolve.FreeRoutine || en.Routine == resolve.StructMethod:
				u.transfer(a, in, x)
			}
		}
	}
}

// transfer records an operand that its receiver keeps as a site when a transfer of its type copies storage.
func (u *unit) transfer(x syntax.Expr, in *statement, call syntax.Expr) {
	if u.o.p.deep(u.o.p.types[x]) {
		u.site(&site{x: x, in: in, call: call})
	}
}

// site records a site when its operand stands in a statement and reads a fixed place of a root that the unit owns, with the enclosing iterator loops that hold its place, and reports whether it did.
func (u *unit) site(s *site) bool {
	at, ok := u.o.p.placeOf(s.x)
	if s.in == nil || !ok || !at.fixed || !u.owned(at.root) {
		return false
	}
	s.at = at
	for _, l := range u.held {
		switch {
		case !l.at.within(at):
			s.bound = s.bound || at.within(l.at) && (l.futures || u.o.modes.refers[l.s])
		case l.futures:
			s.bound = true
		default:
			s.held = append(s.held, l)
		}
	}
	u.sites = append(u.sites, s)
	return true
}

// binds reports whether an arm of a match binds the value of its subject.
func (o *owner) binds(s *syntax.Match) bool {
	return slices.ContainsFunc(s.Arms, func(arm *syntax.Arm) bool {
		return arm.Kind == syntax.TypedArm && arm.Binding != "" && !o.catches(arm)
	})
}

// catches reports whether an arm selects a failure of the subject of its match.
func (o *owner) catches(arm *syntax.Arm) bool {
	return arm.Kind == syntax.AnyErrorArm || arm.Kind == syntax.TypedArm && isError(o.p.Table.Type(arm.Type))
}

// exhaustive reports whether one arm of a match runs for every value of its subject, or for every evaluation of a match without a subject.
func (o *owner) exhaustive(s *syntax.Match) bool {
	none := slices.ContainsFunc(s.Arms, func(arm *syntax.Arm) bool { return arm.Kind == syntax.NoneArm })
	return slices.ContainsFunc(s.Arms, func(arm *syntax.Arm) bool {
		switch arm.Kind {
		case syntax.AnyArm, syntax.ResultsArm:
			return true
		case syntax.TypedArm:
			return !o.catches(arm) && (none || Same(o.p.Table.Type(arm.Type), o.p.types[s.Subject]))
		}
		return false
	})
}

// clean reports whether no other operand of the statement of a site reads storage of the site after the operation of the site takes it: an operand that overlaps the place of the site stands inside another operand of that operation or inside an index operand of a target, which the statement evaluates before its values, or is another operand of that operation whose type is of the flat copy class, because the operation takes its operands after it has evaluated them all, or the in-place function of the site leaves every element of its operand as it was and no receiver takes the storage of the other operand, and no method call of the statement can change the place or a root that a computation of the place reads.
func (u *unit) clean(s *site) bool {
	p := u.o.p
	ok := true
	var visit func(x syntax.Expr, inside, direct bool)
	visit = func(x syntax.Expr, inside, direct bool) {
		if x == nil || x == s.x || !ok {
			return
		}
		if at, reads := p.placeOf(x); reads {
			if at.overlaps(s.at) && (!inside || direct && p.Copy(p.types[x]) != builtin.Flat) && !(s.keeps && !u.takes(x)) {
				ok = false
			}
			for _, i := range indexes(x) {
				visit(i, inside, false)
			}
			return
		}
		switch x := x.(type) {
		case *syntax.Closure:
			ok = ok && (inside || !slices.Contains(p.captures(x), s.at.root))
			return
		case *syntax.MethodCall:
			if at, reads := p.placeOf(x.Receiver); reads && (at.overlaps(s.at) || slices.Contains(s.at.reads, at.root)) && u.o.mutates(x) {
				ok = false
			}
		}
		for _, y := range operands(x) {
			visit(y, inside || x == s.call, x == s.call)
		}
	}
	for _, v := range s.in.values {
		visit(v, s.call == nil, s.call == nil)
	}
	for _, x := range s.in.before {
		visit(x, true, false)
	}
	return ok
}

// takes reports whether an operand is a site of the unit, so that its receiver can take its storage.
func (u *unit) takes(x syntax.Expr) bool {
	return slices.ContainsFunc(u.sites, func(s *site) bool { return s.x == x })
}

// consumed reports whether the analysis found a site at a root consumed.
func (u *unit) consumed(r root) bool {
	return slices.ContainsFunc(u.sites, func(s *site) bool { return s.taken && s.at.root == r })
}

// share records the mode Share for the value of each declaration of one local that reads a place or a constant without consuming it, when no statement of the unit writes or consumes the local and none writes or consumes the root of the place, so that both holders stay live and neither is written.
func (u *unit) share() {
	o, p := u.o, u.o.p
	var body syntax.Node = u.closure
	if u.closure == nil {
		body = u.fn.Body
	}
	kept := func(r root) bool { return !o.writes(body, r) && !u.consumed(r) }
	for _, d := range u.shares {
		x, t := d.Values[0], d.Targets[0]
		if !p.deep(p.types[x]) || !kept(root{decl: t, name: t.Name}) || constant(root{decl: t, name: t.Name}) {
			continue
		}
		at, ok := p.placeOf(x)
		switch {
		case !ok && !p.ReadsConstant(x):
			continue
		case ok && (at.root.self && !u.self || !kept(at.root)):
			continue
		}
		o.modes.operands[x] = Share
	}
}

// receiver is a call in a body of an instance method of a struct on a receiver that is a place.
type receiver struct {
	fn *syntax.Func
	at place
}

// reach returns, for a body outside its closures and a root, the calls of instance methods of structs on a receiver at the root.
func (o *owner) reach(body syntax.Node, r root) []receiver {
	var calls []receiver
	syntax.Inspect(body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.MethodCall:
			at, ok := o.p.placeOf(n.Receiver)
			if !ok || at.root != r || !o.mutates(n) {
				break
			}
			if k := o.p.callees[n]; k != nil && k.fn != nil && o.routine[k.fn] != nil && o.routine[k.fn].self {
				calls = append(calls, receiver{k.fn, at})
			}
		}
		return true
	})
	return calls
}

// early reports whether an iterator loop walks a collection of futures, which has no independent copy, or its body can change a part of the collection that the loop holds before it replaces the source, so that the loop observes the change and cannot walk an independent copy.
func (o *owner) early(l *hold) bool {
	if !l.settled {
		l.settled = true
		l.early = l.futures
		if !l.early {
			l.early, _ = o.alters(l.s.Body.Stmts, l.at, map[*syntax.Func]bool{})
		}
	}
	return l.early
}

// alters reports whether a statement list can change a part of the value of a place before one of its statements replaces the place as a whole, and whether a statement of the list replaces it, where only a store to a place whose indexes are literals counts as a replacement, because a local of an index can change, and open holds the methods whose bodies are under inspection.
func (o *owner) alters(stmts []syntax.Stmt, at place, open map[*syntax.Func]bool) (altered, replaced bool) {
	stable := !slices.ContainsFunc(at.steps, func(s step) bool { return s.field == "" && s.literal == "" })
	for _, s := range stmts {
		if b, ok := s.(*syntax.Block); ok {
			if altered, replaced = o.alters(b.Stmts, at, open); altered || replaced {
				return altered, replaced
			}
			continue
		}
		if o.changes(s, at, open) {
			return true, false
		}
		st, ok := s.(*syntax.Store)
		if ok && stable && slices.ContainsFunc(st.Targets, func(t syntax.Expr) bool {
			to, ok := o.p.placeOf(t)
			return ok && to.covers(at)
		}) {
			return false, true
		}
	}
	return false, false
}

// changes reports whether a statement, outside its closures, can change a part of the value of a place: a store to a part, a call of a method that can change its receiver on a part, or such a call on a receiver that holds the place, when the callee is not known or its body changes a part of that place of its receiver before it replaces it.
func (o *owner) changes(s syntax.Stmt, at place, open map[*syntax.Func]bool) bool {
	part := func(to place) bool { return len(to.steps) > len(at.steps) && to.within(at) }
	found := false
	syntax.Inspect(s, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.Store:
			for _, t := range n.Targets {
				to, ok := o.p.placeOf(t)
				found = found || ok && part(to)
			}
		case *syntax.MethodCall:
			recv, ok := o.p.placeOf(n.Receiver)
			switch {
			case !ok || recv.root != at.root || !o.mutates(n):
			case part(recv):
				found = true
			case at.within(recv):
				k := o.p.callees[n]
				switch {
				case k == nil || k.fn == nil || open[k.fn]:
					found = true
				case o.routine[k.fn] != nil && o.routine[k.fn].self:
					open[k.fn] = true
					found, _ = o.alters(k.fn.Body.Stmts, place{root: root{self: true}, steps: at.steps[len(recv.steps):]}, open)
					delete(open, k.fn)
				}
			}
		}
		return !found
	})
	return found
}

// taking returns the consumed sites whose storage can hold a place of the receiver of a method: the sites of the method itself, and those of each method that it calls on a part of its receiver that holds the place, where open holds the methods under inspection with the count of the selections of the place.
func (o *owner) taking(fn *syntax.Func, at place, open map[*syntax.Func]int) []*site {
	if n, ok := open[fn]; ok && n == len(at.steps) {
		return nil
	}
	open[fn] = len(at.steps)
	var out []*site
	for _, s := range o.routine[fn].sites {
		if s.taken && at.within(s.at) {
			out = append(out, s)
		}
	}
	for _, c := range o.reach(fn.Body, root{self: true}) {
		if at.within(c.at) {
			out = append(out, o.taking(c.fn, place{root: root{self: true}, steps: at.steps[len(c.at.steps):]}, open)...)
		}
	}
	return out
}

// conflicts settles each iterator loop whose body calls, on a part of its source path, a method that can consume, in place, storage of its receiver that holds the source, because the loop holds the collection that its source selected: when the loop can walk an independent copy of its source, it does, and otherwise the methods that the body reaches consume no such storage, so that a store to an element reaches the collection that the loop holds until a statement replaces it.
func (o *owner) conflicts() {
	for _, l := range o.loops {
		var sites []*site
		for _, c := range o.reach(l.s.Body, l.at.root) {
			if l.at.within(c.at) {
				sites = append(sites, o.taking(c.fn, place{root: root{self: true}, steps: l.at.steps[len(c.at.steps):]}, map[*syntax.Func]int{})...)
			}
		}
		switch {
		case len(sites) == 0:
		case !o.early(l):
			o.modes.snapshots[l.s] = true
		default:
			for _, s := range sites {
				s.taken = false
			}
		}
	}
}
