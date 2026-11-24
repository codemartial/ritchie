// This file is the dependency analysis of the loop pipeline: it decides, over a kernel
// program, which reads of an iterator loop run ahead of the iterations in front of
// them, and records them in a table beside the program.
package check

import (
	"slices"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/syntax"
)

// Ahead is the entry of an iterator loop in the table of loops whose reads run ahead: the sends that are dispatched ahead, the declarations and stores of the body that compute what those sends need, where a declaration without a value has the number zero, and the matches whose arm the walk ahead selects, each with its number in the order of the body, and whether the walk ahead reads the loop variables.
type Ahead struct {
	Sends   map[*syntax.Send]int
	Values  map[syntax.Stmt]int
	Matches map[*syntax.Match]int
	Vars    bool
}

// RunAhead returns the table of the iterator loops whose reads run ahead, which holds a loop when its body holds no effect and at least one send whose arguments, arm values, and enclosing branch conditions are fixed early.
func (p *Program) RunAhead() map[*syntax.IterFor]*Ahead {
	table := map[*syntax.IterFor]*Ahead{}
	pure := p.pure()
	for _, n := range p.Graph.Nodes {
		body := n.body()
		if body == nil {
			continue
		}
		syntax.Inspect(body, func(x syntax.Node) bool {
			switch x := x.(type) {
			case *syntax.Closure:
				return false
			case *syntax.IterFor:
				l := &lead{p: p, loop: x, pure: pure}
				if a := l.analyze(); a != nil {
					table[x] = a
				}
			}
			return true
		})
	}
	return table
}

// pure returns the routines whose call yields a value that depends on its arguments alone and always ends: a routine whose body holds no loop without an iterator and calls only built-ins that yield a value and are neither an effect nor an impurity root, and routines that are pure, so that no routine of a call cycle is pure.
func (p *Program) pure() map[Node]bool {
	out := map[Node]bool{}
	for changed := true; changed; {
		changed = false
		for _, n := range p.Graph.Nodes {
			if out[n] || n.Func == nil || n.Func.Body == nil || unbounded(n.Func.Body) {
				continue
			}
			ok := true
			for _, e := range p.Graph.Edges[n] {
				switch k := e.Callee; {
				case k.op != nil:
					ok = ok && computes(k.op.Row)
				case k.remote || k.fn == nil:
					ok = false
				default:
					ok = ok && out[Node{Func: k.fn}]
				}
			}
			if ok {
				out[n], changed = true, true
			}
		}
	}
	return out
}

// computes reports whether a built-in yields a value that depends on its operands alone and changes none of them.
func computes(r *builtin.Row) bool {
	return !r.Effect && !r.Standalone && !r.ResultNone && !r.Ref
}

// lead is the analysis of one iterator loop: the declarations of its body, the variables that the body stores, whether it stores self, the locals of the body that are fixed early, whether the loop variables are, and what the walk of the body met under branch conditions that are fixed early and outside every nested loop, in the order of the body.
type lead struct {
	p        *Program
	loop     *syntax.IterFor
	pure     map[Node]bool
	declared map[syntax.Node]bool
	stored   map[syntax.Node]bool
	self     bool
	fixed    map[syntax.Node]bool
	vars     bool
	met      []met
}

// met is one statement, send, or exit that the walk of a loop body met, with the matches that enclose it, where a statement that stores a value to a local has the declaration of the local and the value.
type met struct {
	node  syntax.Node
	decl  syntax.Node
	value syntax.Expr
	under []*syntax.Match
}

// root returns the declaration of the local that a path of field and index selections starts at, or nil, and whether the path starts at self.
func (p *Program) root(x syntax.Expr) (syntax.Node, bool) {
	for {
		switch y := x.(type) {
		case *syntax.Name:
			return p.Local(y), false
		case *syntax.Self:
			return nil, true
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			x = y.X
		default:
			return nil, false
		}
	}
}

// analyze returns the entry of the loop, or nil when no read of its body runs ahead.
func (l *lead) analyze() *Ahead {
	p := l.p
	if Futures(p.types[l.loop.Source]) {
		return nil
	}
	l.declared, l.stored, l.fixed = map[syntax.Node]bool{}, map[syntax.Node]bool{}, map[syntax.Node]bool{}
	effect := false
	changed := map[syntax.Node]bool{}
	store := func(x syntax.Expr, whole bool) {
		d, self := p.root(x)
		l.self = l.self || self
		if d != nil {
			l.stored[d] = true
			changed[d] = changed[d] || !whole
		}
	}
	syntax.Inspect(l.loop.Body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.VarDecl:
			for _, t := range n.Targets {
				l.declared[t] = true
				l.fixed[t] = len(n.Targets) == 1 && !holdsFuture(p.locals[t])
			}
		case *syntax.ConstDecl, *syntax.IterFor, *syntax.Param:
			l.declared[n] = true
		case *syntax.Arm:
			l.declared[n] = true
			for _, t := range n.Targets {
				l.declared[t] = true
			}
		case *syntax.Store:
			d, _ := l.simple(n)
			for _, t := range n.Targets {
				store(t, d != nil)
			}
		case *syntax.MethodCall:
			if p.ChangesReceiver(n) {
				store(n.Receiver, false)
			}
		}
		if x, ok := n.(syntax.Expr); ok {
			effect = effect || p.Marks.Effect(x)
		}
		return true
	})
	if effect {
		return nil
	}
	source, self := p.root(l.loop.Source)
	l.vars = !l.stored[l.loop] && !(self && l.self) && !(source != nil && l.stored[source])
	for d, part := range changed {
		if part {
			delete(l.fixed, d)
		}
	}
	for l.settle(l.loop.Body.Stmts, true) {
	}
	l.walk(l.loop.Body.Stmts, nil)
	return l.table()
}

// simple returns the declaration of the local that a statement stores one value to as a whole, and the value, which is nil for a declaration without one, or no declaration for any other statement.
func (l *lead) simple(s syntax.Stmt) (syntax.Node, syntax.Expr) {
	switch s := s.(type) {
	case *syntax.VarDecl:
		switch {
		case len(s.Targets) != 1:
		case len(s.Values) == 0:
			return s.Targets[0], nil
		case len(s.Values) == 1:
			return s.Targets[0], s.Values[0]
		}
	case *syntax.Store:
		if name, ok := s.Targets[0].(*syntax.Name); ok && len(s.Targets) == 1 && len(s.Values) == 1 {
			return l.p.Local(name), s.Values[0]
		}
	}
	return nil, nil
}

// settle removes from the locals that are fixed early each local that a statement list stores a value to that is not fixed early, or stores under a branch condition that is not fixed early or in a nested loop, which fixed reports for the list, and reports whether it removed one.
func (l *lead) settle(stmts []syntax.Stmt, fixed bool) bool {
	changed := false
	drop := func(d syntax.Node) {
		if l.fixed[d] {
			delete(l.fixed, d)
			changed = true
		}
	}
	for _, s := range stmts {
		switch s := s.(type) {
		case *syntax.Block:
			changed = l.settle(s.Stmts, fixed) || changed
			continue
		case *syntax.Match:
			inside := fixed && l.selects(s)
			for _, arm := range s.Arms {
				changed = l.settle(stmtsOf(arm.Body), inside) || changed
			}
			continue
		}
		d, value := l.simple(s)
		if fixed && d != nil && (value == nil || l.fixedValue(value)) {
			continue
		}
		syntax.Inspect(s, func(n syntax.Node) bool {
			switch n := n.(type) {
			case *syntax.VarDecl:
				for _, t := range n.Targets {
					drop(t)
				}
			case *syntax.Store:
				for _, t := range n.Targets {
					if d, _ := l.p.root(t); d != nil {
						drop(d)
					}
				}
			}
			return true
		})
	}
	return changed
}

// stmtsOf returns the statements of an arm body.
func stmtsOf(s syntax.Stmt) []syntax.Stmt {
	switch s := s.(type) {
	case nil:
		return nil
	case *syntax.Block:
		return s.Stmts
	}
	return []syntax.Stmt{s}
}

// selects reports whether a match selects its arm by conditions that are fixed early: a match without a subject or over a value, whose arms are case arms and one any arm, and whose subject and candidates are fixed early.
func (l *lead) selects(s *syntax.Match) bool {
	if s.Subject != nil && (len(s.Arms) == 1 && s.Arms[0].Kind == syntax.AnyArm || !l.fixedValue(s.Subject)) {
		return false
	}
	for _, arm := range s.Arms {
		if arm.Kind != syntax.CaseArm && arm.Kind != syntax.AnyArm || arm.Body == nil {
			return false
		}
		if slices.ContainsFunc(arm.Patterns, func(x syntax.Expr) bool { return !l.fixedValue(x) }) {
			return false
		}
	}
	return true
}

// fixedValue reports whether a value is fixed early: a literal, a variable that the body does not store, a loop variable or a local of the body that is fixed early, or the result of an operation without an effect, without a wait, and without an impurity root over values that are fixed early, where no operand is a place that its operation consumes, because the operation would change, ahead of the iterations in front of it, storage that those iterations read.
func (l *lead) fixedValue(x syntax.Expr) bool {
	p := l.p
	if _, ok := p.placeOf(x); ok && p.Modes.Of(x) == Consume {
		return false
	}
	all := func(xs ...syntax.Expr) bool {
		return !slices.ContainsFunc(xs, func(x syntax.Expr) bool { return x != nil && !l.fixedValue(x) })
	}
	args := func(args []*syntax.Arg) bool {
		return !slices.ContainsFunc(args, func(a *syntax.Arg) bool { return !l.fixedValue(a.Value) })
	}
	switch x := x.(type) {
	case *syntax.Lit:
		return true
	case *syntax.Name:
		d := p.Local(x)
		switch {
		case holdsFuture(p.types[x]):
			return false
		case d == nil:
			return true
		case d == l.loop:
			return l.vars
		case l.declared[d]:
			return l.fixed[d]
		}
		return !l.stored[d]
	case *syntax.Self:
		return !l.self
	case *syntax.Prim:
		return all(x.Args...)
	case *syntax.Logical:
		return all(x.Left, x.Right)
	case *syntax.Format:
		return !slices.ContainsFunc(x.Parts, func(part syntax.FormatPart) bool { return part.Slot != nil && !l.fixedValue(part.Slot) })
	case *syntax.FieldRead:
		return all(x.X)
	case *syntax.IndexRead:
		return all(x.X, x.Index)
	case *syntax.Slice:
		return all(x.X, x.Lo, x.Hi)
	case *syntax.ArrayLit:
		return all(x.Elems...)
	case *syntax.DictLit:
		return !slices.ContainsFunc(x.Entries, func(e *syntax.DictEntry) bool { return !all(e.Key, e.Value) })
	case *syntax.Construct:
		return args(x.Args)
	case *syntax.Call:
		return l.computed(x) && args(x.Args)
	case *syntax.MethodCall:
		return l.computed(x) && all(x.Receiver) && args(x.Args)
	}
	return false
}

// computed reports whether a call is an operation whose result depends on its operands alone: a call of a built-in that computes, or of a pure routine that is no method on a receiver.
func (l *lead) computed(x syntax.Expr) bool {
	k := l.p.callees[x]
	switch {
	case k == nil || k.remote:
		return false
	case k.op != nil:
		return computes(k.op.Row)
	}
	return k.fn != nil && k.kind != syntax.StructMethod && l.pure[Node{Func: k.fn}]
}

// walk records what a statement list holds under branch conditions that are fixed early and outside every nested loop: each statement that stores a value to a local that is fixed early, each match that selects, each send whose arguments and arm values are fixed early, and each exit, with the matches that enclose it.
func (l *lead) walk(stmts []syntax.Stmt, under []*syntax.Match) {
	for _, s := range stmts {
		var own []syntax.Expr
		switch s := s.(type) {
		case *syntax.Block:
			l.walk(s.Stmts, under)
		case *syntax.Break, *syntax.Continue, *syntax.Return:
			l.met = append(l.met, met{node: s, under: under})
		case *syntax.Match:
			if !l.selects(s) {
				own = []syntax.Expr{s.Subject}
				break
			}
			l.met = append(l.met, met{node: s, under: under})
			inside := append(slices.Clone(under), s)
			for _, arm := range s.Arms {
				l.walk(stmtsOf(arm.Body), inside)
			}
		case *syntax.VarDecl:
			own = s.Values
		case *syntax.ConstDecl:
			own = []syntax.Expr{s.Value}
		case *syntax.Store:
			own = slices.Concat(s.Targets, s.Values)
		case *syntax.ExprStmt:
			own = []syntax.Expr{s.X}
		}
		if d, value := l.simple(s); d != nil && l.fixed[d] {
			l.met = append(l.met, met{node: s, decl: d, value: value, under: under})
			continue
		}
		for _, x := range own {
			if x == nil {
				continue
			}
			syntax.Inspect(x, func(n syntax.Node) bool {
				switch n := n.(type) {
				case *syntax.Closure:
					return false
				case *syntax.Send:
					if l.sent(n) {
						l.met = append(l.met, met{node: n, under: under})
					}
				}
				return true
			})
		}
	}
}

// sent reports whether the arguments and the arm values of a send are fixed early.
func (l *lead) sent(x *syntax.Send) bool {
	var args []*syntax.Arg
	switch c := x.X.(type) {
	case *syntax.Call:
		args = c.Args
	case *syntax.ServiceCall:
		args = c.Args
	default:
		return false
	}
	if slices.ContainsFunc(args, func(a *syntax.Arg) bool { return !l.fixedValue(a.Value) }) {
		return false
	}
	return !slices.ContainsFunc(x.Arms, func(arm *syntax.SendArm) bool {
		return slices.ContainsFunc(arm.Values, func(v syntax.Expr) bool { return !l.fixedValue(v) })
	})
}

// table returns the entry of the loop from what the walk met: the sends, the statements that store to a local of the body that a send, a match of the entry, or another such statement reads, and the matches that enclose a send, an exit, or such a statement, or nil when the walk met no send.
func (l *lead) table() *Ahead {
	a := &Ahead{Sends: map[*syntax.Send]int{}, Values: map[syntax.Stmt]int{}, Matches: map[*syntax.Match]int{}, Vars: l.vars}
	need := map[syntax.Node]bool{}
	reads := func(x syntax.Node) {
		if x == nil {
			return
		}
		syntax.Inspect(x, func(n syntax.Node) bool {
			if name, ok := n.(*syntax.Name); ok {
				if d := l.p.Local(name); d != nil && l.declared[d] {
					need[d] = true
				}
			}
			return true
		})
	}
	enclose := func(under []*syntax.Match) {
		for _, m := range under {
			if _, ok := a.Matches[m]; !ok {
				a.Matches[m] = 0
				reads(m.Subject)
				for _, arm := range m.Arms {
					for _, c := range arm.Patterns {
						reads(c)
					}
				}
			}
		}
	}
	for _, m := range l.met {
		switch n := m.node.(type) {
		case *syntax.Send:
			a.Sends[n] = len(a.Sends) + 1
			reads(n)
			enclose(m.under)
		case *syntax.Break, *syntax.Continue, *syntax.Return:
			enclose(m.under)
		}
	}
	if len(a.Sends) == 0 {
		return nil
	}
	for changed := true; changed; {
		changed = false
		for _, m := range l.met {
			s, ok := m.node.(syntax.Stmt)
			if _, kept := a.Values[s]; !ok || kept || m.decl == nil || !need[m.decl] {
				continue
			}
			a.Values[s], changed = 0, true
			if m.value != nil {
				reads(m.value)
			}
			enclose(m.under)
		}
	}
	values, matches := 0, 0
	for _, m := range l.met {
		switch n := m.node.(type) {
		case *syntax.Match:
			if _, ok := a.Matches[n]; ok {
				matches++
				a.Matches[n] = matches
			}
		case syntax.Stmt:
			if _, ok := a.Values[n]; ok && m.value != nil {
				values++
				a.Values[n] = values
			}
		}
	}
	return a
}
