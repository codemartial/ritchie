// This file is the control family of the Rewrites table: the if chain and the
// condition loop, beside the iterator loop, which is a kernel form whose source and
// body are lowered in place.
package lower

import (
	"ritchie.dev/internal/syntax"
)

// ifChain lowers an if chain into one match without a subject, with one arm per condition of the chain.
func (l *lowerer) ifChain(s *syntax.If) []syntax.Stmt {
	pre, cond := l.sub(func() syntax.Expr { return l.condition(s.Cond) })
	m := &syntax.Match{Origin: s.Origin, Arms: []*syntax.Arm{caseArm(s.Origin, cond, l.block(s.Then))}}
	l.elseArms(m, s.Else)
	return append(pre, m)
}

// elseArms appends the arms of the rest of an if chain to a match, and a condition whose prelude or test cannot stand in front of the match begins a nested match inside the any arm, because a candidate is evaluated only when selection reaches it.
func (l *lowerer) elseArms(m *syntax.Match, e syntax.Stmt) {
	switch e := e.(type) {
	case nil:
	case *syntax.Block:
		m.Arms = append(m.Arms, anyArm(e.Origin, l.block(e)))
	case *syntax.If:
		pre, cond := l.sub(func() syntax.Expr { return l.condition(e.Cond) })
		if len(pre) > 0 {
			inner := &syntax.Match{Origin: e.Origin, Arms: []*syntax.Arm{caseArm(e.Origin, cond, l.block(e.Then))}}
			l.elseArms(inner, e.Else)
			m.Arms = append(m.Arms, anyArm(e.Origin, block(e.Origin, append(pre, inner))))
			return
		}
		m.Arms = append(m.Arms, caseArm(e.Origin, cond, l.block(e.Then)))
		l.elseArms(m, e.Else)
	}
}

// condFor lowers a condition loop: the condition is tested at the top of the body by a match whose any arm breaks.
func (l *lowerer) condFor(s *syntax.CondFor, body *syntax.Block) []syntax.Stmt {
	o := s.Origin
	l.push()
	defer l.pop()
	pre, cond := l.sub(func() syntax.Expr { return l.condition(s.Cond) })
	head := append(pre, &syntax.Match{Origin: o, Arms: []*syntax.Arm{
		caseArm(o, cond, block(o, nil)),
		anyArm(o, block(o, []syntax.Stmt{&syntax.Break{Origin: o}})),
	}})
	stmts := l.stmts(body.Stmts)
	return []syntax.Stmt{&syntax.For{Origin: o, Label: s.Label, Body: block(body.Origin, append(head, stmts...))}}
}

// iterFor lowers an iterator loop, which is a kernel form: its source expression and its body are lowered, and the loop variables take the types that the checker derived from the source.
func (l *lowerer) iterFor(s *syntax.IterFor, body *syntax.Block) []syntax.Stmt {
	src := l.expr(s.Source, nil)
	l.push()
	defer l.pop()
	for i, v := range s.Vars {
		l.declare(v, l.p.LoopVarTypes(s)[i])
	}
	return []syntax.Stmt{&syntax.IterFor{Origin: s.Origin, Label: s.Label, Vars: s.Vars, Source: src, Body: l.block(body)}}
}
