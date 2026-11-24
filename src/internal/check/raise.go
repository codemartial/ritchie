// This file infers raise sets as a fixpoint over the call graph and performs the kernel
// checks that read them: raise sets against declared sets, dead arms, and fallible
// operands of assignment targets.
package check

import (
	"cmp"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
	"ritchie.dev/rlr"
)

// RaiseSet is the set of nominal error types an operation can propagate, where Opaque marks incomplete knowledge.
type RaiseSet struct {
	Types  map[string]bool
	Opaque bool
}

// Names returns the error types of the set in order.
func (s RaiseSet) Names() []string { return slices.Sorted(maps.Keys(s.Types)) }

// Empty reports whether no failure is known or possible.
func (s RaiseSet) Empty() bool { return len(s.Types) == 0 && !s.Opaque }

func (s RaiseSet) union(o RaiseSet) RaiseSet {
	out := RaiseSet{Types: map[string]bool{}, Opaque: s.Opaque || o.Opaque}
	for n := range s.Types {
		out.Types[n] = true
	}
	for n := range o.Types {
		out.Types[n] = true
	}
	return out
}

func (s RaiseSet) without(names map[string]bool) RaiseSet {
	out := RaiseSet{Types: map[string]bool{}, Opaque: s.Opaque}
	for n := range s.Types {
		if !names[n] {
			out.Types[n] = true
		}
	}
	return out
}

func (s RaiseSet) equal(o RaiseSet) bool {
	if s.Opaque != o.Opaque || len(s.Types) != len(o.Types) {
		return false
	}
	for n := range s.Types {
		if !o.Types[n] {
			return false
		}
	}
	return true
}

func setOf(names ...string) RaiseSet {
	out := RaiseSet{Types: map[string]bool{}}
	for _, n := range names {
		out.Types[n] = true
	}
	return out
}

var opaque = RaiseSet{Types: map[string]bool{}, Opaque: true}

// errorName is the canonical name of an error type: its declaring module and name.
func errorName(t syntax.Type) string {
	n, ok := t.(*syntax.Named)
	if !ok {
		return ""
	}
	if n.Module == "" {
		return n.Name
	}
	return n.Module + "::" + n.Name
}

var runtimeTier = func() RaiseSet {
	set := setOf()
	for _, r := range builtin.Rows {
		if r.Form == builtin.ErrorRow && r.Tier == builtin.Runtime {
			set.Types[r.Name] = true
		}
	}
	return set
}()

// domainErrors returns the canonical names of the error types that a program declares, which are the only errors a declared error set lists.
func domainErrors(t *resolve.Table) map[string]bool {
	out := map[string]bool{}
	for _, k := range t.Errors() {
		out[errorName(&syntax.Named{Module: k.Module, Name: k.Name})] = true
	}
	return out
}

// outcome is the raise summary of a statement: what reaches an enclosing handler, what escapes the routine, and the explicit error returns among them.
type outcome struct {
	avail RaiseSet
	esc   RaiseSet
	rets  RaiseSet
	live  bool
}

func plain(set RaiseSet, live bool) outcome {
	return outcome{avail: set, esc: set, rets: setOf(), live: live}
}

func (o outcome) then(p outcome) outcome {
	return outcome{avail: o.avail.union(p.avail), esc: o.esc.union(p.esc), rets: o.rets.union(p.rets), live: p.live}
}

// join is the summary of two paths of which one runs: the union of their sets, live when either path is live.
func (o outcome) join(p outcome) outcome {
	return outcome{avail: o.avail.union(p.avail), esc: o.esc.union(p.esc), rets: o.rets.union(p.rets), live: o.live || p.live}
}

// raiser walks one routine for its raise sets, reporting the checks that read them when report is set.
type raiser struct {
	c      *checker
	r      Node
	report bool
}

// inferRaises computes the raise set of every routine and closure as a fixpoint over the call graph.
func (c *checker) inferRaises() {
	for _, n := range c.nodes {
		c.p.raises[n] = setOf()
	}
	for changed := true; changed; {
		changed = false
		for _, n := range c.nodes {
			rs := &raiser{c: c, r: n}
			set := rs.node(n)
			if !set.equal(c.p.raises[n]) {
				c.p.raises[n] = set
				changed = true
			}
		}
	}
}

// reportRaises walks every routine once more at the fixpoint, reporting dead arms, undeclared escaping errors, and fallible target operands.
func (c *checker) reportRaises() {
	for _, n := range c.nodes {
		rs := &raiser{c: c, r: n, report: true}
		set := rs.node(n)
		if n.Func != nil {
			c.declared(n.Func, set)
		}
	}
	var keys []origin
	for k := range c.seen {
		if !c.live[k] {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b origin) int {
		return cmp.Or(cmp.Compare(a.index, b.index), cmp.Compare(a.start, b.start))
	})
	for _, k := range keys {
		arm := c.deadAt[k]
		reason := diagnostics.HandleDeadArm
		if !c.handlerArm[arm] {
			reason = diagnostics.MatchDeadArm
		}
		o := arm.Origin
		end := o.End
		if i := strings.Index(o.File.Text[o.Start:o.End], "=>"); i >= 0 {
			end = o.Start + i
			for end > o.Start && o.File.Text[end-1] == ' ' {
				end--
			}
		}
		c.report(o, o.Start, end, reason, "no failure of the protected operations can select this arm")
	}
}

// declared checks the declared error set of a routine, where one is written, against its escaping set, which it lists exactly in the error types the program declares while catalog errors escape unlisted, and which has no other effect because callers see the inferred set.
func (c *checker) declared(fn *syntax.Func, set RaiseSet) {
	if len(fn.Raises) == 0 {
		return
	}
	e := c.p.Table.Of(fn)
	if e == nil {
		return
	}
	name := diagnostics.Code(routineName(e))
	listed := map[string]bool{}
	for _, t := range fn.Raises {
		r := c.p.Table.Type(t)
		if r == nil {
			continue
		}
		n := errorName(r)
		if !c.domain[n] {
			c.report(t.Where(), t.Where().Start, t.Where().End, diagnostics.ContractInvalidError, fmt.Sprintf("%s is a catalog error, which a declared error set does not list", n))
			continue
		}
		listed[n] = true
		if !set.Types[n] {
			c.report(t.Where(), t.Where().Start, t.Where().End, diagnostics.ContractUnraised, fmt.Sprintf("%s lists %s, which cannot escape it", name, n))
		}
	}
	var missing []string
	for _, n := range set.Names() {
		if c.domain[n] && !listed[n] {
			missing = append(missing, n)
		}
	}
	from := fn.Start
	if fn.Receiver != nil {
		from = fn.Receiver.Where().End
	}
	switch {
	case len(missing) > 0:
		c.reportName(fn, from, fn.Name, diagnostics.ContractUndeclared, fmt.Sprintf("%s can raise %s, which its declared error set does not list", name, strings.Join(missing, ", ")))
	case set.Opaque:
		c.reportName(fn, from, fn.Name, diagnostics.ContractUndeclared, fmt.Sprintf("%s can raise an error of a type its declared error set does not list", name))
	}
}

// routineName is the name of a routine as the source writes it, which the runtime report of an escaping error also shows.
func routineName(e *resolve.Entry) string {
	owner := ""
	if e.Owner != nil {
		owner = e.Owner.Name
	}
	return rlr.RoutineName(e.Module, owner, e.Name)
}

// node computes the escaping set of a routine or closure.
func (rs *raiser) node(n Node) RaiseSet {
	switch {
	case n.Func != nil && n.Func.Body != nil:
		return rs.stmts(n.Func.Body.Stmts, true).esc
	case n.Closure != nil && n.Closure.Body != nil:
		return rs.stmts(n.Closure.Body.Stmts, true).esc
	case n.Closure != nil && n.Closure.Expr != nil:
		return rs.expr(n.Closure.Expr)
	}
	return setOf()
}

// stmts walks a statement list, where a transfer ends the path and later statements contribute nothing.
func (rs *raiser) stmts(list []syntax.Stmt, live bool) outcome {
	out := plain(setOf(), live)
	for _, s := range list {
		if !out.live {
			break
		}
		out = out.then(rs.stmt(s))
	}
	return out
}

// stmt returns the raise summary of one statement and records the set that escapes it.
func (rs *raiser) stmt(s syntax.Stmt) outcome {
	o := rs.summary(s)
	rs.c.p.escapes[s] = o.esc
	return o
}

// summary computes the raise summary of one statement.
func (rs *raiser) summary(s syntax.Stmt) outcome {
	c := rs.c
	switch s := s.(type) {
	case *syntax.Bad, *syntax.Join:
		return plain(setOf(), true)
	case *syntax.Block:
		return rs.stmts(s.Stmts, true)
	case *syntax.VarDecl:
		return plain(rs.exprs(s.Values), true)
	case *syntax.ConstDecl:
		return plain(setOf(), true)
	case *syntax.Store:
		set := rs.exprs(s.Values)
		for _, t := range s.Targets {
			set = set.union(rs.target(t, false))
		}
		return plain(set, true)
	case *syntax.CompoundStore:
		set := rs.expr(s.Value).union(rs.target(s.Target, true))
		if k := c.p.callees[s.Target]; k != nil && k.op != nil && c.p.constant[s.Value] == nil && c.p.collections[s.Value] == nil {
			set = set.union(setOf(k.op.Row.Faults...))
		}
		return plain(set, true)
	case *syntax.ExprStmt:
		return plain(rs.expr(s.X), true)
	case *syntax.For:
		body := rs.stmts(s.Body.Stmts, true)
		body.live = hasBreak(s.Body)
		return body
	case *syntax.CondFor:
		cond := rs.expr(s.Cond)
		known, value := rs.constBool(s.Cond)
		body := rs.stmts(s.Body.Stmts, true)
		out := plain(cond, true).then(body)
		out.live = !known || !value || hasBreak(s.Body)
		if known && !value {
			out = plain(cond, true)
		}
		return out
	case *syntax.IterFor:
		out := plain(rs.expr(s.Source), true).then(rs.stmts(s.Body.Stmts, true))
		out.live = true
		return out
	case *syntax.Break, *syntax.Continue:
		return plain(setOf(), false)
	case *syntax.Return:
		set := rs.exprs(s.Values)
		out := plain(set, false)
		if len(s.Values) == 1 {
			if t := c.p.types[s.Values[0]]; t != nil && isError(t) {
				raised := rs.errorValue(s.Values[0])
				out.avail = out.avail.union(raised)
				out.esc = out.esc.union(raised)
				out.rets = raised
			}
		}
		return out
	case *syntax.Resume:
		return plain(rs.exprs(s.Values), false)
	case *syntax.If:
		cond := rs.expr(s.Cond)
		known, value := rs.constBool(s.Cond)
		then := rs.stmts(s.Then.Stmts, true)
		other := plain(setOf(), true)
		if s.Else != nil {
			other = rs.stmt(s.Else)
		}
		switch {
		case known && value:
			return plain(cond, true).then(then)
		case known:
			return plain(cond, true).then(other)
		}
		return plain(cond, true).then(then.join(other))
	case *syntax.Match:
		return rs.match(s)
	case *syntax.Handled:
		return rs.handled(s)
	}
	panic(fmt.Sprintf("check: unexpected statement %T", s))
}

// hasBreak reports whether a loop body contains a break of the loop itself.
func hasBreak(b *syntax.Block) bool {
	found := false
	var visit func(n syntax.Node) bool
	visit = func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Break:
			found = true
		case *syntax.For, *syntax.CondFor, *syntax.IterFor, *syntax.Closure:
			return false
		case *syntax.Handled:
			for _, arm := range n.Arms {
				if arm.Body != nil {
					if _, ok := arm.Body.(*syntax.Break); ok {
						found = true
					}
					syntax.Inspect(arm.Body, visit)
				}
			}
		}
		return !found
	}
	syntax.Inspect(b, visit)
	return found
}

// constBool reads the compile-time value of a condition that the typer evaluated.
func (rs *raiser) constBool(x syntax.Expr) (bool, bool) {
	lit, ok := rs.c.p.constant[x].(*syntax.Lit)
	if !ok {
		return false, false
	}
	return lit.Kind == syntax.KwTrue || lit.Kind == syntax.KwFalse, lit.Kind == syntax.KwTrue
}

// target returns the failures of an assignment target: those of its index and key operands, each fallible one reported, the read of every index selector inside its path, and the store through its last selector, which a compound assignment also reads, and records the failures of each selector as its site.
func (rs *raiser) target(x syntax.Expr, compound bool) RaiseSet {
	set := rs.path(x)
	if ix, ok := x.(*syntax.IndexRead); ok {
		own := rs.selector(ix, "[]=")
		if compound {
			own = own.union(rs.selector(ix, "[]"))
		}
		rs.c.p.sites[ix] = own
		set = set.union(own)
	}
	return set
}

// path returns the failures of the selectors of a target path that lead to its last selector: the index and key operands, and the read of each index selector.
func (rs *raiser) path(x syntax.Expr) RaiseSet {
	switch x := x.(type) {
	case *syntax.FieldRead:
		return rs.path(x.X).union(rs.read(x.X))
	case *syntax.IndexRead:
		set := rs.expr(x.Index)
		if rs.report && !set.Empty() {
			rs.c.reportAt(x.Index, diagnostics.AssignmentFallibleOperand, "an index or key operand of an assignment target cannot fail")
		}
		return rs.path(x.X).union(rs.read(x.X)).union(set)
	}
	return setOf()
}

// read returns the failures of reading an index selector inside a target path and records them as its site.
func (rs *raiser) read(x syntax.Expr) RaiseSet {
	ix, ok := x.(*syntax.IndexRead)
	if !ok {
		return setOf()
	}
	set := rs.selector(ix, "[]")
	rs.c.p.sites[ix] = set
	return set
}

// selector returns the fault set of the row of an index operator that reads or stores through a selector, by the types of its container, key, and element.
func (rs *raiser) selector(x *syntax.IndexRead, op string) RaiseSet {
	types := []syntax.Type{rs.c.p.types[x.X], rs.c.p.types[x.Index]}
	if op == "[]=" {
		types = append(types, rs.c.p.types[x])
	}
	row, _, _ := rs.c.selectRow(builtin.Operator, op, types)
	if row == nil {
		return setOf()
	}
	return setOf(row.Faults...)
}

// errorValue returns the possible nominal types of an error-valued expression that a return raises: its declared type, so the binding of an any or any! arm raises the opaque set.
func (rs *raiser) errorValue(x syntax.Expr) RaiseSet {
	t := rs.c.p.types[x]
	if builtinName(t) == "Error!" {
		return opaque
	}
	return setOf(errorName(t))
}

// exprs returns the union of the failures of a list of expressions.
func (rs *raiser) exprs(list []syntax.Expr) RaiseSet {
	set := setOf()
	for _, x := range list {
		set = set.union(rs.expr(x))
	}
	return set
}

// expr returns the failures an expression can propagate: those of its operands and its own operation, where a constant expression contributes nothing.
func (rs *raiser) expr(x syntax.Expr) RaiseSet {
	c := rs.c
	if x == nil || c.p.constant[x] != nil || c.p.collections[x] != nil {
		return setOf()
	}
	switch x := x.(type) {
	case *syntax.Lit, *syntax.Name, *syntax.Self, *syntax.Closure, *syntax.Bad:
		return setOf()
	case *syntax.Prim:
		return rs.exprs(x.Args).union(rs.own(x))
	case *syntax.Format:
		var slots []syntax.Expr
		for _, p := range x.Parts {
			if p.Slot != nil {
				slots = append(slots, p.Slot)
			}
		}
		return rs.exprs(slots).union(rs.own(x))
	case *syntax.Call:
		return rs.args(x.Args).union(rs.expr(x.Callee)).union(rs.own(x))
	case *syntax.MethodCall:
		return rs.expr(x.Receiver).union(rs.args(x.Args)).union(rs.own(x))
	case *syntax.ServiceCall:
		return rs.args(x.Args).union(rs.own(x))
	case *syntax.DotMember:
		return rs.args(x.Args).union(rs.own(x))
	case *syntax.Construct:
		return rs.args(x.Args)
	case *syntax.Create:
		return rs.args(x.Args).union(rs.own(x))
	case *syntax.Lookup:
		return rs.expr(x.Value).union(rs.own(x))
	case *syntax.ArrayLit:
		return rs.exprs(x.Elems)
	case *syntax.DictLit:
		set := setOf()
		for _, e := range x.Entries {
			set = set.union(rs.expr(e.Key)).union(rs.expr(e.Value))
		}
		return set
	case *syntax.FieldRead:
		return rs.expr(x.X)
	case *syntax.IndexRead:
		return rs.expr(x.X).union(rs.expr(x.Index)).union(rs.own(x))
	case *syntax.Slice:
		return rs.expr(x.X).union(rs.expr(x.Lo)).union(rs.expr(x.Hi)).union(rs.own(x))
	case *syntax.Ternary:
		return rs.expr(x.Cond).union(rs.expr(x.Then)).union(rs.expr(x.Else))
	case *syntax.Coalesce:
		return rs.expr(x.Left).union(rs.expr(x.Right))
	case *syntax.Logical:
		return rs.expr(x.Left).union(rs.expr(x.Right))
	case *syntax.Chain:
		return rs.exprs(x.Operands)
	case *syntax.QueryCall, *syntax.GetOrCreate:
		return opaque
	case *syntax.Send:
		return rs.subjectArguments(x.X).union(rs.unsupplied(x))
	case *syntax.Wait:
		if s, ok := x.X.(*syntax.Send); ok {
			return rs.subjectArguments(s.X).union(rs.own(x))
		}
		return rs.expr(x.X)
	}
	panic(fmt.Sprintf("check: unexpected expression %T", x))
}

// unsupplied returns the failures of the read of a send that no arm of the send selects, which count at the send, where an arm without a type and an arm of the type Error! select every failure.
func (rs *raiser) unsupplied(x *syntax.Send) RaiseSet {
	selected := map[string]bool{}
	for _, arm := range x.Arms {
		t := rs.c.p.Table.Type(arm.Type)
		if arm.Type == nil || builtinName(t) == "Error!" {
			return setOf()
		}
		selected[errorName(t)] = true
	}
	return rs.own(x.X).without(selected)
}

func (rs *raiser) args(args []*syntax.Arg) RaiseSet {
	set := setOf()
	for _, a := range args {
		set = set.union(rs.expr(a.Value))
	}
	return set
}

// own returns the failures of the operation of an expression itself: the fixed set of a built-in row, the escaping set of a callee, or the outcomes of a remote operation, with the resolution of an element or entry receiver of a method call.
func (rs *raiser) own(x syntax.Expr) RaiseSet {
	c := rs.c
	if w, ok := x.(*syntax.Wait); ok {
		set := setOf()
		if s, ok := w.X.(*syntax.Send); ok {
			set = rs.own(s.X)
		}
		c.p.sites[x] = set
		return set
	}
	k := c.p.callees[x]
	if k == nil {
		return setOf()
	}
	set := setOf()
	switch {
	case k.op != nil:
		set = setOf(k.op.Row.Faults...)
	case k.fn != nil:
		set = c.p.raises[Node{Func: k.fn}]
	case k.typ != nil:
		set = opaque
	case k.lookup:
		set = setOf("NotFound!")
	case k.create:
		set = setOf("Duplicate!")
	}
	if k.remote {
		set = set.union(runtimeTier)
	}
	if m, ok := x.(*syntax.MethodCall); ok {
		for _, s := range c.p.PlaceSelectors(m) {
			set = set.union(rs.own(s))
		}
	}
	c.p.sites[x] = set
	return set
}

// handled returns the summary of a handled statement: its arms select from what each protected statement makes available, a failure inside an arm is reported and reaches only the enclosing handlers while the original error continues outward, and an explicit error return from an arm escapes.
func (rs *raiser) handled(s *syntax.Handled) outcome {
	c := rs.c
	var protected []syntax.Stmt
	header := setOf()
	compound := true
	var heads []RaiseSet
	switch st := s.Stmt.(type) {
	case *syntax.Block:
		protected = st.Stmts
		compound = isCompound(s)
	case *syntax.For:
		protected = st.Body.Stmts
	case *syntax.CondFor:
		protected = st.Body.Stmts
		header = rs.expr(st.Cond)
	case *syntax.IterFor:
		protected = st.Body.Stmts
		header = rs.expr(st.Source)
	case *syntax.Match:
		header = rs.subject(st)
		for _, arm := range st.Arms {
			if arm.Body != nil {
				protected = append(protected, arm.Body)
				heads = append(heads, rs.exprs(arm.Patterns))
			}
		}
	case *syntax.If:
		return rs.stmt(branchHandlers(s, st))
	default:
		protected = []syntax.Stmt{s.Stmt}
		compound = false
	}
	inner := plain(header, true)
	var per []outcome
	for i, p := range protected {
		if !inner.live {
			per = append(per, plain(setOf(), false))
			continue
		}
		o := rs.stmt(p)
		if heads != nil {
			o = plain(heads[i], true).then(o)
		}
		per = append(per, o)
		inner = inner.then(o)
	}
	switch st := s.Stmt.(type) {
	case *syntax.For:
		inner.live = hasBreak(st.Body)
	case *syntax.CondFor, *syntax.IterFor:
		inner.live = true
	case *syntax.Match:
		inner.live = !covered(st.Arms)
		for _, o := range per {
			inner.live = inner.live || o.live
		}
	}
	available := setOf()
	for _, o := range per {
		available = available.union(o.avail)
	}
	selected := map[string]bool{}
	remaining := available
	hasAny := false
	out := outcome{avail: header, esc: header, rets: setOf(), live: inner.live}
	for _, arm := range s.Arms {
		live := false
		var selects RaiseSet
		switch arm.Kind {
		case syntax.TypedArm:
			t := c.p.locals[arm]
			if t == nil {
				t = c.p.Table.Type(arm.Type)
			}
			name := errorName(t)
			live = name != "" && !selected[name] && (available.Types[name] || available.Opaque)
			selects = setOf(name)
			if name != "" && !selected[name] {
				selected[name] = true
				remaining = remaining.without(map[string]bool{name: true})
			}
		case syntax.AnyArm:
			live = !remaining.Empty()
			selects = remaining
			hasAny = true
		}
		rs.mark(arm, live, true)
		if rs.report && arm.Values != nil {
			rs.placeValue(s.Stmt, armValues(arm), selects)
		}
		if rs.report && compound {
			if arm.Values != nil {
				rs.replacement(arm.Values, armValues(arm), protected, per, selects)
			}
			for _, res := range resumes(arm.Body) {
				rs.replacement(res.Values, res, protected, per, selects)
			}
			if assigned, ok := c.p.noop[arm]; ok {
				rs.noopReads(protected, per, selects, assigned)
			}
		}
		var a outcome
		switch {
		case arm.Values != nil:
			a = plain(rs.exprs(arm.Values), true)
		case arm.Body != nil:
			a = rs.stmt(arm.Body)
		}
		out.avail = out.avail.union(a.avail)
		if !a.avail.Empty() {
			out.avail = out.avail.union(selects)
		}
		out.esc = out.esc.union(a.rets)
		out.rets = out.rets.union(a.rets)
		out.live = out.live || a.live
	}
	if hasAny {
		remaining = setOf()
	}
	out.avail = out.avail.union(remaining)
	out.esc = out.esc.union(remaining)
	out.rets = out.rets.union(inner.rets.without(selected))
	return out
}

// branchHandlers places the handler of an if on each branch block, because the handler protects the statements of the branches and not the conditions.
func branchHandlers(s *syntax.Handled, st *syntax.If) *syntax.If {
	guarded := func(b *syntax.Block) *syntax.Block {
		h := &syntax.Handled{Origin: s.Origin, Stmt: &syntax.Block{Origin: b.Origin, Stmts: b.Stmts}, Arms: s.Arms}
		return &syntax.Block{Origin: b.Origin, Stmts: []syntax.Stmt{h}}
	}
	out := &syntax.If{Origin: st.Origin, Cond: st.Cond, Then: guarded(st.Then)}
	switch e := st.Else.(type) {
	case *syntax.If:
		out.Else = branchHandlers(s, e)
	case *syntax.Block:
		out.Else = guarded(e)
	}
	return out
}

// noopReads reports a read, after a protected statement that a no-op arm can recover, of a variable that statement stores and that neither the arm nor a later statement stored first.
func (rs *raiser) noopReads(protected []syntax.Stmt, per []outcome, selects RaiseSet, assigned map[string]bool) {
	c := rs.c
	for i, p := range protected {
		o := per[i]
		selectable := o.avail.Opaque && !selects.Empty()
		for n := range selects.Types {
			selectable = selectable || o.avail.Types[n]
		}
		if !selectable {
			continue
		}
		names := map[string]bool{}
		for _, v := range c.storedPlaces(p).vars {
			if !assigned[v.name] {
				names[v.name] = true
			}
		}
		for _, later := range protected[i+1:] {
			if len(names) == 0 {
				break
			}
			syntax.Inspect(later, func(n syntax.Node) bool {
				switch n := n.(type) {
				case *syntax.Name:
					if names[n.Name] && n.Module == "" && n.Owner == nil && c.p.bindings[n] != nil {
						c.reportAt(n, diagnostics.VariableUseBeforeAssign, diagnostics.Code(n.Name)+" cannot be read while unassigned, which it is after the no-op arm that recovers the statement storing it")
						delete(names, n.Name)
					}
				case *syntax.Closure:
					return false
				}
				return true
			})
			if st, ok := later.(*syntax.Store); ok {
				for _, t := range st.Targets {
					if n, ok := t.(*syntax.Name); ok {
						delete(names, n.Name)
					}
				}
			}
			if d, ok := later.(*syntax.VarDecl); ok {
				for _, t := range d.Targets {
					delete(names, t.Name)
				}
			}
		}
	}
}

// resumes returns the resume statements with values of an arm body that target the arm itself.
func resumes(body syntax.Stmt) []*syntax.Resume {
	var out []*syntax.Resume
	if body == nil {
		return nil
	}
	var visit func(n syntax.Node) bool
	visit = func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Resume:
			if len(n.Values) > 0 {
				out = append(out, n)
			}
		case *syntax.Closure:
			return false
		case *syntax.Handled:
			syntax.Inspect(n.Stmt, visit)
			return false
		}
		return true
	}
	syntax.Inspect(body, visit)
	return out
}

// placeValue rejects an arm that supplies values where it selects a failure of the validation of an array or dictionary place that an assignment targets, which permits only diversion or return.
func (rs *raiser) placeValue(st syntax.Stmt, at syntax.Node, selects RaiseSet) {
	var targets []syntax.Expr
	switch st := st.(type) {
	case *syntax.Store:
		targets = st.Targets
	case *syntax.CompoundStore:
		targets = []syntax.Expr{st.Target}
	}
	for _, t := range targets {
		for x := t; x != nil; {
			switch y := x.(type) {
			case *syntax.FieldRead:
				x = y.X
			case *syntax.IndexRead:
				for n := range selects.Types {
					if rs.c.p.sites[y].Types[n] {
						rs.c.reportAt(at, diagnostics.HandleIncompatibleReplacement, "an arm that recovers the validation of a mutable place diverts or returns and supplies no values")
						return
					}
				}
				x = y.X
			default:
				x = nil
			}
		}
	}
}

// replacement checks the values an arm of a block or loop handler supplies against the results of every protected statement the arm can recover.
func (rs *raiser) replacement(values []syntax.Expr, at syntax.Node, protected []syntax.Stmt, per []outcome, selects RaiseSet) {
	c := rs.c
	for i, p := range protected {
		o := per[i]
		selectable := o.avail.Opaque && !selects.Empty()
		for n := range selects.Types {
			selectable = selectable || o.avail.Types[n]
		}
		if !selectable {
			continue
		}
		results, has := c.statementResults(p)
		supplied := len(values)
		if supplied == 1 && isCall(values[0]) {
			supplied = len(c.p.results[values[0]])
			if supplied == 0 {
				continue
			}
		}
		if !has {
			c.reportAt(at, diagnostics.HandleIncompatibleReplacement, "the arm supplies values for a recovered statement without results")
			continue
		}
		if supplied != len(results) {
			c.reportAt(at, diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("the recovered statement has %d results and the replacement supplies %d", len(results), supplied))
			continue
		}
		if len(values) == 1 && isCall(values[0]) {
			for j, t := range c.p.results[values[0]] {
				if !Assignable(t, results[j]) {
					c.reportAt(values[0], diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("result %d has type %s where %s is required", j+1, show(t), show(results[j])))
				}
			}
			continue
		}
		for j, v := range values {
			if d, ok := c.deferred[v]; ok && !c.typeAgainst(v, d, results[j]) {
				c.reportAt(v, diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("the replacement cannot stand where %s is required", show(results[j])))
				continue
			}
			if !c.fits(v, results[j]) {
				c.reportAt(v, diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("a replacement of type %s cannot stand where %s is required", show(c.p.types[v]), show(results[j])))
			}
		}
	}
}

// typeAgainst types a deferred arm value in check mode against the type one recovered statement requires, and reports whether it types there without a fault, dropping any fault it would raise because the caller reports the incompatible replacement.
func (c *checker) typeAgainst(v syntax.Expr, d deferredValue, want syntax.Type) bool {
	faults, diags := len(c.faults), len(c.diags)
	c.TypeOf(v, d.sc, d.r, want)
	if len(c.faults) > faults || len(c.diags) > diags {
		c.faults, c.diags = c.faults[:faults], c.diags[:diags]
		return false
	}
	return true
}

// fits reports whether a typed value stands where a type is required, where an integer literal fits any integer type whose range holds it.
func (c *checker) fits(v syntax.Expr, want syntax.Type) bool {
	t := c.p.types[v]
	if contextual(v) {
		if lit, ok := v.(*syntax.Lit); ok && lit.Kind == syntax.IntLit {
			return integerBounds[builtinName(unwrap(want))] != [2]*big.Int{} && c.inRange(lit, false, want)
		}
		if p, ok := v.(*syntax.Prim); ok && len(p.Args) == 1 {
			if lit, ok := p.Args[0].(*syntax.Lit); ok && lit.Kind == syntax.IntLit {
				return integerBounds[builtinName(unwrap(want))] != [2]*big.Int{} && c.inRange(lit, true, want)
			}
		}
	}
	return t != nil && !isNone(t) && Assignable(t, want)
}

// inRange reports whether an integer literal, negated or not, lies within the range of an integer type.
func (c *checker) inRange(lit *syntax.Lit, negative bool, want syntax.Type) bool {
	bounds, ok := integerBounds[builtinName(unwrap(want))]
	if !ok {
		return false
	}
	n, _ := new(big.Int).SetString(lit.Canonical(), 10)
	if negative {
		n.Neg(n)
	}
	return n.Cmp(bounds[0]) >= 0 && n.Cmp(bounds[1]) <= 0
}

// mark records whether an arm is live at this site, judged by its origin across every site.
func (rs *raiser) mark(arm *syntax.Arm, live bool, handler bool) {
	if !rs.report {
		return
	}
	c := rs.c
	k := keyOf(arm.Origin)
	c.seen[k] = true
	c.live[k] = c.live[k] || live
	c.deadAt[k] = arm
	c.handlerArm[arm] = handler
}

// isOutcome reports whether a match selects among the outcomes of its subject: the subject can fail with an outcome of its own and an arm has an outcome form.
func (rs *raiser) isOutcome(s *syntax.Match) bool {
	if s.Subject == nil || !outcomeArms(s.Arms) {
		return false
	}
	if isCall(s.Subject) || sentOf(s.Subject) != nil {
		return true
	}
	k := rs.c.p.callees[s.Subject]
	return k != nil && k.op != nil && len(k.op.Row.Faults) > 0
}

// covered reports whether an arm list selects every value of its subject by an any arm.
func covered(arms []*syntax.Arm) bool {
	for _, arm := range arms {
		if arm.Kind == syntax.AnyArm {
			return true
		}
	}
	return false
}

// subject returns the summary of the header of a match: the failures of a value subject, or the arguments and unselected outcomes of an outcome subject.
func (rs *raiser) subject(s *syntax.Match) RaiseSet {
	if !rs.isOutcome(s) {
		return rs.expr(s.Subject)
	}
	out, _ := rs.outcomes(s)
	return out.esc
}

// outcomes returns the summary of the subject of an outcome match, marking which arms its outcomes can select: the failures of the arguments and the outcomes that no arm selects, and whether every success outcome is selected.
func (rs *raiser) outcomes(s *syntax.Match) (outcome, bool) {
	c := rs.c
	all := rs.own(s.Subject)
	remaining := all
	selected := map[string]bool{}
	successCovered, present, absent := false, false, false
	success := c.p.types[s.Subject]
	for _, arm := range s.Arms {
		live := true
		switch arm.Kind {
		case syntax.TypedArm:
			t := c.p.locals[arm]
			if t == nil {
				t = c.p.Table.Type(arm.Type)
			}
			switch {
			case isError(t):
				name := errorName(t)
				live = !selected[name] && (all.Types[name] || all.Opaque)
				selected[name] = true
				remaining = remaining.without(map[string]bool{name: true})
			case success != nil && presentMember(success, t):
				live = !successCovered && !present
				present = true
				successCovered = present && absent
			default:
				live = !successCovered
				successCovered = true
			}
		case syntax.NoneArm:
			if success != nil && nullable(success) == success {
				live = !successCovered && !absent
				absent = true
				successCovered = present && absent
			}
		case syntax.AnyArm, syntax.ResultsArm:
			live = !successCovered
			successCovered = true
		case syntax.AnyErrorArm:
			live = !remaining.Empty()
			remaining = setOf()
		}
		rs.mark(arm, live, false)
	}
	out := plain(rs.subjectArguments(s.Subject), false)
	out.avail = out.avail.union(remaining)
	out.esc = out.esc.union(remaining)
	return out, successCovered
}

// match returns the summary of a match: a value match unions its parts, and an outcome match selects among the outcomes of its subject.
func (rs *raiser) match(s *syntax.Match) outcome {
	if !rs.isOutcome(s) {
		out := plain(rs.expr(s.Subject), true)
		joined := plain(setOf(), false)
		cover := covered(s.Arms)
		for _, arm := range s.Arms {
			always, never := false, false
			if s.Subject == nil && arm.Kind == syntax.CaseArm {
				always, never = constantArm(arm.Patterns, rs.constBool)
			}
			if never {
				continue
			}
			a := plain(rs.exprs(arm.Patterns), true)
			if arm.Body != nil {
				a = a.then(rs.stmt(arm.Body))
			}
			joined = joined.join(a)
			if always {
				cover = true
				break
			}
		}
		joined.live = joined.live || !cover
		return out.then(joined)
	}
	out, successCovered := rs.outcomes(s)
	for _, arm := range s.Arms {
		if arm.Body != nil {
			out = out.join(rs.stmt(arm.Body))
		} else {
			out.live = true
		}
	}
	out.live = out.live || !successCovered
	return out
}

// subjectArguments returns the failures of evaluating the operands of a subject operation, which are failures of the match statement and not outcomes of the operation.
func (rs *raiser) subjectArguments(x syntax.Expr) RaiseSet {
	switch x := x.(type) {
	case *syntax.Wait:
		if s, ok := x.X.(*syntax.Send); ok {
			return rs.subjectArguments(s.X)
		}
	case *syntax.Call:
		return rs.args(x.Args).union(rs.expr(x.Callee))
	case *syntax.MethodCall:
		if selectors := rs.c.p.PlaceSelectors(x); selectors != nil {
			set := rs.args(x.Args)
			for _, s := range selectors {
				set = set.union(rs.expr(s.Index))
			}
			return set
		}
		return rs.expr(x.Receiver).union(rs.args(x.Args))
	case *syntax.ServiceCall:
		return rs.args(x.Args)
	case *syntax.DotMember:
		return rs.args(x.Args)
	case *syntax.Prim:
		return rs.exprs(x.Args)
	case *syntax.IndexRead:
		return rs.expr(x.X).union(rs.expr(x.Index))
	case *syntax.Slice:
		return rs.expr(x.X).union(rs.expr(x.Lo)).union(rs.expr(x.Hi))
	case *syntax.Format:
		var slots []syntax.Expr
		for _, p := range x.Parts {
			if p.Slot != nil {
				slots = append(slots, p.Slot)
			}
		}
		return rs.exprs(slots)
	}
	return setOf()
}
