// This file is the store family of the Rewrites table: the compound assignment, whose
// index and key operands are evaluated once into temporaries.
package lower

import (
	"strings"

	"ritchie.dev/internal/syntax"
)

// compoundStore lowers m op= e to T t = e followed by m = m op t, so that m is read after the right side, with e in place of t when e is a literal or a name, and m ??= e by coalescingStore.
func (l *lowerer) compoundStore(s *syntax.CompoundStore) []syntax.Stmt {
	if s.Op == "??=" {
		return l.coalescingStore(s)
	}
	o := s.Origin
	target := l.onceTarget(s.Target, true)
	vt := l.p.TypeOf(s.Value)
	value := l.expr(s.Value, vt)
	if !plain(s.Value) {
		tmp := l.fresh("t")
		l.hoist(&syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(vt, o), Name: tmp}}, Values: []syntax.Expr{value}})
		l.declare(tmp, vt)
		value = name(o, tmp)
	}
	rhs := &syntax.Prim{Origin: o, Op: strings.TrimSuffix(s.Op, "="), Args: []syntax.Expr{copyPath(target), value}}
	return []syntax.Stmt{store(o, target, rhs)}
}

// coalescingStore lowers m ??= e to if m == none { m = e }: the index and key operands of m are in temporaries before the test, and a read of m through an index step is stored in a temporary that the test reads, so that the place is read once and stored at most once, and e is evaluated only in the arm.
func (l *lowerer) coalescingStore(s *syntax.CompoundStore) []syntax.Stmt {
	o := s.Origin
	t := l.p.TypeOf(s.Target)
	target := l.onceTarget(s.Target, true)
	test := copyPath(target)
	if indexed(target) {
		tmp := l.fresh("t")
		l.hoist(&syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(t, o), Name: tmp}}, Values: []syntax.Expr{test}})
		l.declare(tmp, t)
		test = name(o, tmp)
	}
	l.push()
	pre, value := l.sub(func() syntax.Expr { return l.expr(s.Value, t.(*syntax.Nullable).Elem) })
	l.pop()
	return []syntax.Stmt{&syntax.Match{Origin: o, Arms: []*syntax.Arm{
		caseArm(o, presenceTest(test, "=="), block(o, append(pre, store(o, target, value)))),
	}}}
}

// indexed reports whether a lowered target path has an index step.
func indexed(x syntax.Expr) bool {
	for {
		switch y := x.(type) {
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			return true
		default:
			return false
		}
	}
}

// plain reports whether an expression is a name or a literal, where a negated literal is a literal.
func plain(x syntax.Expr) bool {
	if p, ok := x.(*syntax.Prim); ok && p.Op == "-" && len(p.Args) == 1 {
		x = p.Args[0]
	}
	switch x.(type) {
	case *syntax.Lit, *syntax.Name:
		return true
	}
	return false
}

// onceTarget lowers the target of a compound assignment with every index and key operand in a temporary, so that it is evaluated once, and validates each index selector inside the path after its own operand.
func (l *lowerer) onceTarget(x syntax.Expr, last bool) syntax.Expr {
	switch x := x.(type) {
	case *syntax.FieldRead:
		return &syntax.FieldRead{Origin: x.Origin, X: l.onceTarget(x.X, false), Name: x.Name}
	case *syntax.IndexRead:
		y := l.onceTarget(x.X, false)
		index := l.expr(x.Index, nil)
		t := l.p.TypeOf(x.Index)
		tmp := l.fresh("t")
		l.hoist(&syntax.VarDecl{Origin: x.Origin, Targets: []*syntax.VarTarget{{Origin: x.Origin, Type: l.written(t, x.Origin), Name: tmp}}, Values: []syntax.Expr{index}})
		l.declare(tmp, t)
		out := &syntax.IndexRead{Origin: x.Origin, X: y, Index: name(x.Origin, tmp)}
		if !last {
			l.hoist(&syntax.Match{Origin: x.Origin, Subject: copyPath(out), Arms: []*syntax.Arm{anyArm(x.Origin, block(x.Origin, nil))}})
		}
		return out
	}
	return established(x, nil)
}

// copyPath returns an independent copy of a lowered target path, whose indices are names.
func copyPath(x syntax.Expr) syntax.Expr {
	switch x := x.(type) {
	case *syntax.Name:
		n := *x
		return &n
	case *syntax.Self:
		n := *x
		return &n
	case *syntax.FieldRead:
		return &syntax.FieldRead{Origin: x.Origin, X: copyPath(x.X), Name: x.Name}
	case *syntax.IndexRead:
		return &syntax.IndexRead{Origin: x.Origin, X: copyPath(x.X), Index: copyPath(x.Index)}
	case *syntax.Lit:
		n := *x
		return &n
	}
	return x
}
