// This file types the kernel forms of a read in flight and performs the kernel checks
// on them: where a future type, a send, and a wait stand, and that a join stands
// between a send and the next effect or exit that the send reaches.
package check

import (
	"fmt"
	"slices"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
)

// sentOf returns the send of a wait that waits for a read at the place of its send, or nil for any other expression.
func sentOf(x syntax.Expr) *syntax.Send {
	if w, ok := x.(*syntax.Wait); ok {
		s, _ := w.X.(*syntax.Send)
		return s
	}
	return nil
}

// holdsFuture reports whether a type is a future type or holds one.
func holdsFuture(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.Future:
		return true
	case *syntax.Array:
		return holdsFuture(t.Elem)
	case *syntax.Dict:
		return holdsFuture(t.Key) || holdsFuture(t.Value)
	case *syntax.Nullable:
		return holdsFuture(t.Elem)
	case *syntax.Named:
		return slices.ContainsFunc(t.Args, holdsFuture)
	case *syntax.FuncType:
		return slices.ContainsFunc(t.Params, holdsFuture) || slices.ContainsFunc(t.Results, holdsFuture)
	}
	return false
}

// send types a send: its operand is a call, each result of which the send yields as a future, and each arm supplies one value for each result of the call or returns one value for each result of the routine.
func (c *checker) send(x *syntax.Send, sc *scope, r *routine) ([]syntax.Type, bool) {
	results, ok := c.results(x.X, sc, r, nil)
	if !ok {
		return nil, false
	}
	for _, arm := range x.Arms {
		if arm.Type != nil {
			if t := c.resolveType(arm.Type, r.module); t != nil && !isError(t) {
				c.faultAt(arm.Type, diagnostics.TypeMismatch, fmt.Sprintf("an arm of a send selects an error type and not %s", show(t)))
				ok = false
			}
		}
		wants := results
		if arm.Return {
			wants = r.results
		}
		if len(arm.Values) != len(wants) {
			c.faultAt(arm, diagnostics.VariableArity, fmt.Sprintf("%d values are supplied for %d results", len(arm.Values), len(wants)))
			ok = false
			continue
		}
		for i, v := range arm.Values {
			ok = c.assign(v, sc, r, wants[i]) != nil && ok
		}
	}
	futures := make([]syntax.Type, len(results))
	for i, t := range results {
		futures[i] = &syntax.Future{Elem: t}
	}
	c.p.results[x] = futures
	if len(futures) == 1 {
		c.attach(x, futures[0])
	}
	return futures, ok
}

// wait types a wait: the results of the read of a send at its place, the value of a future, or the collection of the values of a collection of futures.
func (c *checker) wait(x *syntax.Wait, sc *scope, r *routine) ([]syntax.Type, bool) {
	var results []syntax.Type
	if s, ok := x.X.(*syntax.Send); ok {
		futures, ok := c.send(s, sc, r)
		if !ok {
			return nil, false
		}
		for _, f := range futures {
			results = append(results, f.(*syntax.Future).Elem)
		}
	} else {
		t := c.TypeOf(x.X, sc, r, nil)
		if t == nil {
			return nil, false
		}
		value := Waited(t)
		if value == nil {
			c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("a wait takes a future or a collection of futures and not %s", show(t)))
			return nil, false
		}
		results = []syntax.Type{c.narrowed(x, value, r)}
	}
	c.p.results[x] = results
	if len(results) == 1 {
		c.attach(x, results[0])
	}
	return results, true
}

// Flights performs the kernel checks on the forms of a read in flight over a kernel program and returns their diagnostics: where a future type, a send, and a wait stand, and that a join stands between a send and the next effect or exit that the send reaches.
func (p *Program) Flights() []diagnostics.Record {
	var diags []diagnostics.Record
	report := func(n syntax.Node, start int, message string) {
		o := n.Where()
		diags = append(diags, o.File.Record(diagnostics.FutureMisplaced, max(start, o.Start), o.End, message))
	}
	for _, f := range p.Files {
		for _, d := range f.Decls {
			p.placed(d, report)
		}
	}
	for _, n := range p.Graph.Nodes {
		body := n.body()
		if body == nil {
			continue
		}
		Reached(body, p.Marks.Effect, func(s syntax.Stmt) {
			if s == nil {
				report(body, body.End-1, "a join stands between a send and the end of the routine")
				return
			}
			report(s, 0, "a join stands between a send and this statement")
		})
	}
	diagnostics.Sort(diags)
	return diags
}

// body returns the block body of a routine or closure, or nil.
func (n Node) body() *syntax.Block {
	switch {
	case n.Func != nil:
		return n.Func.Body
	case n.Closure != nil:
		return n.Closure.Body
	}
	return nil
}

// placed reports each future type, send, and wait of a declaration that stands where the kernel does not admit it: a future type outside the type of a local variable or of the elements of a local collection, arms on a send that is no whole value and no statement, an arm value that is no known value, a wait for a future as the subject of an outcome match, and a send whose operand is no remote read and no call of a reader.
func (p *Program) placed(d syntax.Decl, report func(n syntax.Node, start int, message string)) {
	admitted := map[syntax.Node]bool{}
	armed := map[syntax.Node]bool{}
	syntax.Inspect(d, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.VarDecl:
			for _, t := range n.Targets {
				if localFuture(t.Type) {
					admitted[t.Type] = true
				}
			}
			if len(n.Values) == 1 {
				armed[n.Values[0]] = true
			}
		case *syntax.Store:
			if len(n.Values) == 1 {
				armed[n.Values[0]] = true
			}
		case *syntax.ExprStmt:
			armed[n.X] = true
		case *syntax.Match:
			if w, ok := n.Subject.(*syntax.Wait); ok && sentOf(w) == nil && outcomeArms(n.Arms) {
				report(w, 0, "the subject of an outcome match waits for a read at the place of its send")
			}
		case *syntax.Future, *syntax.Array, *syntax.Dict:
			if admitted[n] {
				return false
			}
			if _, future := n.(*syntax.Future); future {
				report(n, 0, "a future type is the type of a local variable or of the elements of a local collection")
				return false
			}
		case *syntax.Send:
			if len(n.Arms) > 0 && !armed[n] {
				report(n, 0, "a send carries arms where it is the whole value of a declaration or a store, or a statement")
			}
			if p.callees[n.X] != nil && !p.Read(n.X) && !p.Reader(n.X) {
				report(n.X, 0, "a send takes a remote read or a call of a reader")
			}
			for _, arm := range n.Arms {
				for _, v := range arm.Values {
					if !knownValue(v) {
						report(v, 0, "an arm of a send supplies a literal or a variable")
					}
				}
			}
		}
		return true
	})
}

// localFuture reports whether a written type is one that a local variable can have when it holds a future: a future, an array of futures, or a dictionary whose values are futures, where the type inside each future holds none.
func localFuture(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.Future:
		return !holdsFuture(t.Elem)
	case *syntax.Array:
		f, ok := t.Elem.(*syntax.Future)
		return ok && !holdsFuture(f.Elem)
	case *syntax.Dict:
		f, ok := t.Value.(*syntax.Future)
		return ok && !holdsFuture(f.Elem) && !holdsFuture(t.Key)
	}
	return false
}

// knownValue reports whether an expression has the form of a known value: a literal, a negated literal, a collection literal of known values, or a name.
func knownValue(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Lit, *syntax.Name:
		return true
	case *syntax.Prim:
		_, lit := x.Args[0].(*syntax.Lit)
		return x.Op == "-" && len(x.Args) == 1 && lit
	case *syntax.ArrayLit:
		return !slices.ContainsFunc(x.Elems, func(e syntax.Expr) bool { return !knownValue(e) })
	case *syntax.DictLit:
		return !slices.ContainsFunc(x.Entries, func(e *syntax.DictEntry) bool { return !knownValue(e.Key) || !knownValue(e.Value) })
	}
	return false
}

// reach is the state of the walk of Reached: whether a send reaches the current point with no join between, whether the point can be reached, the same facts at the breaks and the continues of each enclosing loop, and whether the walk reports.
type reach struct {
	effect func(syntax.Expr) bool
	at     func(syntax.Stmt)
	quiet  bool
	loops  []*reachLoop
}

// reachLoop holds whether a send reaches the statement after a loop through one of its breaks, and the head of the loop through one of its continues.
type reachLoop struct {
	label             string
	breaks, continues bool
}

// Reached calls at for each statement of a routine body that holds an effect or returns, and with nil for the end of the body, where a send reaches that point on some path with no join between, and takes the join that the kernel requires there as present in what follows, so that each missing join is reported once.
func Reached(body *syntax.Block, effect func(syntax.Expr) bool, at func(syntax.Stmt)) {
	w := &reach{effect: effect, at: at}
	if flying, live := w.list(body.Stmts, false); flying && live {
		at(nil)
	}
}

// list walks a statement list from the fact before it and returns the fact after it and whether its end can be reached.
func (w *reach) list(stmts []syntax.Stmt, flying bool) (bool, bool) {
	for _, s := range stmts {
		var live bool
		if flying, live = w.stmt(s, flying); !live {
			return false, false
		}
	}
	return flying, true
}

// holds reports whether the expressions of a statement, outside its closures and the statements that it holds, hold a send and hold an effect.
func (w *reach) holds(xs ...syntax.Expr) (send, effect bool) {
	for _, x := range xs {
		if x == nil {
			continue
		}
		syntax.Inspect(x, func(n syntax.Node) bool {
			switch n := n.(type) {
			case *syntax.Closure:
				return false
			case *syntax.Send:
				send = true
			case syntax.Expr:
				effect = effect || w.effect(n)
			}
			return true
		})
	}
	return send, effect
}

// simple applies the expressions of one statement to the fact before it: an effect, or an exit, that a send reaches is reported and takes the join, and a send of the statement reaches what follows.
func (w *reach) simple(s syntax.Stmt, exit, flying bool, xs ...syntax.Expr) bool {
	send, effect := w.holds(xs...)
	if (effect || exit) && flying {
		if !w.quiet {
			w.at(s)
		}
		flying = false
	}
	return flying || send
}

// stmt walks one statement from the fact before it and returns the fact after it and whether the statement after it can be reached.
func (w *reach) stmt(s syntax.Stmt, flying bool) (bool, bool) {
	switch s := s.(type) {
	case *syntax.Join:
		return false, true
	case *syntax.VarDecl:
		return w.simple(s, false, flying, s.Values...), true
	case *syntax.ConstDecl:
		return w.simple(s, false, flying, s.Value), true
	case *syntax.Store:
		return w.simple(s, false, flying, append(slices.Clone(s.Targets), s.Values...)...), true
	case *syntax.CompoundStore:
		return w.simple(s, false, flying, s.Target, s.Value), true
	case *syntax.ExprStmt:
		return w.simple(s, false, flying, s.X), true
	case *syntax.Return:
		w.simple(s, true, flying, s.Values...)
		return false, false
	case *syntax.Resume:
		return w.simple(s, false, flying, s.Values...), false
	case *syntax.Break:
		if l := w.target(s.Label); l != nil {
			l.breaks = l.breaks || flying
		}
		return false, false
	case *syntax.Continue:
		if l := w.target(s.Label); l != nil {
			l.continues = l.continues || flying
		}
		return false, false
	case *syntax.Block:
		return w.list(s.Stmts, flying)
	case *syntax.For:
		l := w.loop(s.Label, s.Body, flying)
		return l.breaks, true
	case *syntax.CondFor:
		flying = w.simple(s, false, flying, s.Cond)
		l := w.loop(s.Label, s.Body, flying)
		return flying || l.breaks || l.continues, true
	case *syntax.IterFor:
		flying = w.simple(s, false, flying, s.Source)
		l := w.loop(s.Label, s.Body, flying)
		return flying || l.breaks || l.continues, true
	case *syntax.If:
		flying = w.simple(s, false, flying, s.Cond)
		yes, live := w.list(s.Then.Stmts, flying)
		no, other := flying, true
		if s.Else != nil {
			no, other = w.stmt(s.Else, flying)
		}
		return yes && live || no && other, live || other
	case *syntax.Match:
		flying = w.simple(s, false, flying, s.Subject)
		out, reached := flying, !selective(s)
		for _, arm := range s.Arms {
			in := w.simple(s, false, flying, arm.Patterns...)
			if arm.Body == nil {
				out, reached = out || in, true
				continue
			}
			after, live := w.stmt(arm.Body, in)
			out, reached = out || after && live, reached || live
		}
		return out && reached, reached
	case *syntax.Handled:
		after, _ := w.stmt(s.Stmt, flying)
		for _, arm := range s.Arms {
			if arm.Body != nil {
				a, live := w.stmt(arm.Body, after)
				after = after || a && live
			}
		}
		return after, true
	case *syntax.Bad:
		return flying, true
	}
	panic(fmt.Sprintf("check: unexpected statement %T", s))
}

// selective reports whether a match completes only through its arms: it has an any arm, or it selects the success outcome of a subject that has outcomes, because a failure that no arm selects leaves the routine.
func selective(s *syntax.Match) bool {
	outcome := s.Subject != nil && fallibleForm(s.Subject) && outcomeArms(s.Arms)
	for _, arm := range s.Arms {
		switch arm.Kind {
		case syntax.AnyArm:
			return true
		case syntax.NoneArm, syntax.ResultsArm:
			if outcome {
				return true
			}
		case syntax.TypedArm:
			if outcome && !isError(arm.Type) {
				return true
			}
		}
	}
	return false
}

// target returns the enclosing loop that a break or a continue names, or the innermost one.
func (w *reach) target(label string) *reachLoop {
	for _, l := range slices.Backward(w.loops) {
		if label == "" || l.label == label {
			return l
		}
	}
	return nil
}

// loop walks a loop body from the fact at its head, which holds when a send reaches the head on entry, at the end of the body, or at a continue, walking the body without reports until that fact is known, and returns the facts of the loop, where continues holds the fact at the end of the body as well.
func (w *reach) loop(label string, body *syntax.Block, flying bool) *reachLoop {
	walk := func(head, quiet bool) *reachLoop {
		l := &reachLoop{label: label}
		w.loops = append(w.loops, l)
		saved := w.quiet
		w.quiet = saved || quiet
		end, live := w.list(body.Stmts, head)
		w.quiet = saved
		w.loops = w.loops[:len(w.loops)-1]
		l.continues = l.continues || end && live
		return l
	}
	if !flying && !walk(false, true).continues {
		return walk(false, false)
	}
	return walk(true, false)
}
