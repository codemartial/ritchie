// This file is the presence rule of the lowering: a bare nullable path in a condition
// becomes its presence test, a source match over a nullable becomes a match without a
// subject over the presence test, and a narrowed use stays as written, because the
// kernel checker types it by the presence facts.
package lower

import (
	"ritchie.dev/internal/syntax"
)

// tracked reports whether a source expression is a path that the presence rule of the kernel tracks, which is a path that starts at no constant whose value is a collection, because the kernel keeps such a constant by its name and a constant is no variable.
func (l *lowerer) tracked(x syntax.Expr) bool {
	return pathOf(x) != "" && !l.p.ReadsConstant(x)
}

// condition lowers a condition, where a bare path of nullable type is the presence test p != none.
func (l *lowerer) condition(x syntax.Expr) syntax.Expr {
	if pathOf(x) != "" {
		if _, ok := l.p.TypeOf(x).(*syntax.Nullable); ok {
			return presenceTest(l.expr(x, nil), "!=")
		}
	}
	return l.expr(x, named(x.Where(), "Bool"))
}

// presenceTest returns the comparison of a lowered nullable with none, the presence test of the kernel.
func presenceTest(x syntax.Expr, op string) *syntax.Prim {
	o := x.Where()
	return &syntax.Prim{Origin: o, Op: op, Args: []syntax.Expr{x, &syntax.Lit{Origin: o, Kind: syntax.KwNone, Text: "none"}}}
}

// nullableSubject reports whether a match selects among the members of a nullable value: its subject is a nullable that is not the subject of an outcome match, and an arm is a member arm or a none arm.
func (l *lowerer) nullableSubject(s *syntax.Match) bool {
	if s.Subject == nil {
		return false
	}
	if _, ok := l.p.TypeOf(s.Subject).(*syntax.Nullable); !ok {
		return false
	}
	member := false
	for _, a := range s.Arms {
		member = member || a.Kind == syntax.TypedArm || a.Kind == syntax.NoneArm
	}
	switch s.Subject.(type) {
	case *syntax.Prim, *syntax.Format, *syntax.IndexRead, *syntax.Slice:
		return false
	}
	return member && !isCall(s.Subject)
}

// nullableMatch lowers a source match over a nullable to a match without a subject, where p is the subject path or a temporary that holds a subject that is not a path: a member arm case T v => A becomes the arm of p != none whose body declares T v = p before A, case none => B becomes the arm of p == none, an equality candidate v becomes p == v, and any stays.
func (l *lowerer) nullableMatch(s *syntax.Match) []syntax.Stmt {
	o := s.Origin
	t := l.p.TypeOf(s.Subject)
	subject := l.expr(s.Subject, nil)
	var head []syntax.Stmt
	if !l.tracked(s.Subject) {
		tmp := l.fresh("t")
		head = append(head, &syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(t, o), Name: tmp}}, Values: []syntax.Expr{subject}})
		l.declare(tmp, t)
		subject = name(o, tmp)
	}
	equal := func(p syntax.Expr) syntax.Expr {
		return &syntax.Prim{Origin: p.Where(), Op: "==", Args: []syntax.Expr{copyPath(subject), l.expr(p, t)}}
	}
	var arms []loweredArm
	for _, arm := range s.Arms {
		switch arm.Kind {
		case syntax.TypedArm:
			arms = append(arms, loweredArm{arm: l.memberArm(arm, subject)})
		case syntax.NoneArm:
			var body syntax.Stmt = block(arm.Origin, nil)
			if arm.Body != nil {
				body = l.body(arm.Body)
			}
			arms = append(arms, loweredArm{arm: caseArm(arm.Origin, presenceTest(copyPath(subject), "=="), body)})
		default:
			arms = append(arms, l.arm(arm, equal))
		}
	}
	return append(head, &syntax.Match{Origin: o, Arms: nestArms(o, func() syntax.Expr { return nil }, arms)})
}

// memberArm lowers a member arm of a source match over a nullable to the arm of its presence test, whose body first declares the arm's binding from the subject, where the subject has its value type.
func (l *lowerer) memberArm(arm *syntax.Arm, subject syntax.Expr) *syntax.Arm {
	l.push()
	defer l.pop()
	var stmts []syntax.Stmt
	if arm.Binding != "" {
		l.declare(arm.Binding, l.p.LocalType(arm))
		stmts = append(stmts, &syntax.VarDecl{Origin: arm.Origin, Targets: []*syntax.VarTarget{{Origin: arm.Origin, Type: l.erase(arm.Type), Name: arm.Binding}}, Values: []syntax.Expr{copyPath(subject)}})
	}
	if arm.Body != nil {
		switch b := l.body(arm.Body).(type) {
		case *syntax.Block:
			stmts = append(stmts, b.Stmts...)
		default:
			stmts = append(stmts, b)
		}
	}
	return caseArm(arm.Origin, presenceTest(copyPath(subject), "!="), block(arm.Origin, stmts))
}
