// This file is the conditional-expression family of the Rewrites table: the ternary,
// coalescing, the chained comparison, and a short-circuit operator whose right operand
// needs a prelude.
package lower

import (
	"ritchie.dev/internal/syntax"
)

// ternary lowers a conditional expression to a temporary stored by the arms of a match.
func (l *lowerer) ternary(x *syntax.Ternary, want syntax.Type) syntax.Expr {
	o := x.Origin
	t := l.p.TypeOf(x)
	if t == nil {
		t = want
	}
	tmp := l.fresh("t")
	l.hoist(decl(o, l.written(t, o), tmp))
	l.declare(tmp, t)
	for _, s := range l.ternaryMatch(x, t, func(v syntax.Expr) syntax.Stmt { return store(o, name(o, tmp), v) }) {
		l.hoist(s)
	}
	return name(o, tmp)
}

// ternaryMatch builds the match of a ternary, where each arm evaluates its branch, with the branch's prelude, and stores it through into.
func (l *lowerer) ternaryMatch(x *syntax.Ternary, t syntax.Type, into func(syntax.Expr) syntax.Stmt) []syntax.Stmt {
	o := x.Origin
	branch := func(e syntax.Expr) func() []syntax.Stmt {
		return func() []syntax.Stmt {
			pre, v := l.sub(func() syntax.Expr { return l.expr(e, t) })
			return append(pre, into(v))
		}
	}
	cond := l.condition(x.Cond)
	l.push()
	then := branch(x.Then)()
	l.pop()
	l.push()
	other := branch(x.Else)()
	l.pop()
	return []syntax.Stmt{&syntax.Match{Origin: o, Arms: []*syntax.Arm{
		caseArm(o, cond, block(o, then)),
		anyArm(o, block(o, other)),
	}}}
}

// coalesce lowers a ?? b to a result variable stored by the match of coalesceMatch.
func (l *lowerer) coalesce(x *syntax.Coalesce, want syntax.Type) syntax.Expr {
	o := x.Origin
	result := l.p.TypeOf(x)
	if result == nil {
		result = want
	}
	r := l.fresh("r")
	m := l.coalesceMatch(x, func(v syntax.Expr) syntax.Stmt { return store(o, name(o, r), v) })
	l.hoist(decl(o, l.written(result, o), r))
	l.declare(r, result)
	l.hoist(m)
	return name(o, r)
}

// coalesceMatch builds the match of a ?? b: the outcome match on a when a is a call, whose member arm stores the present value through into, and otherwise a match without a subject over the presence test of a, or of a temporary holding a when a is not a tracked path, whose arm stores the tested value through into, where the any arm evaluates b and stores it through into.
func (l *lowerer) coalesceMatch(x *syntax.Coalesce, into func(syntax.Expr) syntax.Stmt) syntax.Stmt {
	o := x.Origin
	left := l.p.TypeOf(x.Left)
	elem := left.(*syntax.Nullable).Elem
	subject := l.expr(x.Left, left)
	var member *syntax.Arm
	switch {
	case isCall(x.Left):
		w := l.fresh("v")
		member = memberArm(o, l.written(elem, o), w, block(o, []syntax.Stmt{into(name(o, w))}))
	default:
		if !l.tracked(x.Left) {
			t := l.fresh("t")
			l.hoist(&syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(left, o), Name: t}}, Values: []syntax.Expr{subject}})
			l.declare(t, left)
			subject = name(o, t)
		}
		member = caseArm(o, presenceTest(subject, "!="), block(o, []syntax.Stmt{into(copyPath(subject))}))
		subject = nil
	}
	l.push()
	pre, other := l.sub(func() syntax.Expr { return l.expr(x.Right, elem) })
	l.pop()
	return &syntax.Match{Origin: o, Subject: subject, Arms: []*syntax.Arm{member, anyArm(o, block(o, append(pre, into(other))))}}
}

// chain lowers a chained comparison to a conjunction, with a temporary for each operand that is not a name or a literal so that every operand is evaluated once.
func (l *lowerer) chain(x *syntax.Chain) syntax.Expr {
	o := x.Origin
	operands := l.temporaries(x.Operands, l.operands(x.Operands, nil), func(v syntax.Expr) bool { return isName(v) || isLit(v) })
	var out syntax.Expr
	for i, op := range x.Ops {
		cmp := &syntax.Prim{Origin: o, Op: op, Args: []syntax.Expr{operands[i], operands[i+1]}}
		if out == nil {
			out = cmp
			continue
		}
		out = &syntax.Logical{Origin: o, Op: "and", Left: out, Right: cmp}
	}
	return out
}

// logical lowers and and or, which are kernel forms, and when the right operand needs a prelude, or holds an operation that can fail under a handler, which makes it the subject of an outcome match, it runs inside the arm that selection reaches.
func (l *lowerer) logical(x *syntax.Logical) syntax.Expr {
	o := x.Origin
	bool := named(o, "Bool")
	left := l.condition(x.Left)
	l.push()
	pre, right := l.sub(func() syntax.Expr { return l.condition(x.Right) })
	l.pop()
	if len(pre) == 0 && (l.protected == 0 || !l.fallible(x.Right)) {
		return &syntax.Logical{Origin: o, Op: x.Op, Left: left, Right: right}
	}
	t := l.fresh("t")
	l.hoist(decl(o, bool, t))
	l.declare(t, bool)
	evaluated := block(o, append(pre, store(o, name(o, t), right)))
	decided := func(value string) *syntax.Block {
		return block(o, []syntax.Stmt{setBool(o, t, value)})
	}
	m := &syntax.Match{Origin: o}
	if x.Op == "and" {
		m.Arms = []*syntax.Arm{caseArm(o, left, evaluated), anyArm(o, decided("false"))}
	} else {
		m.Arms = []*syntax.Arm{caseArm(o, left, decided("true")), anyArm(o, evaluated)}
	}
	l.hoist(m)
	return name(o, t)
}

// fallible reports whether a source expression holds an operation that can fail outside its closures.
func (l *lowerer) fallible(x syntax.Expr) bool {
	found := false
	syntax.Inspect(x, func(n syntax.Node) bool {
		if _, ok := n.(*syntax.Closure); ok || found {
			return false
		}
		e, ok := n.(syntax.Expr)
		found = ok && !l.p.Site(e).Empty()
		return !found
	})
	return found
}
