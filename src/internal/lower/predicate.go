// This file is the match family of the Rewrites table: a match with a predicate arm
// becomes a match without a subject over a temporary, and a candidate whose prelude
// cannot stand in front of the match begins a nested match inside the arm that
// selection reaches.
package lower

import (
	"ritchie.dev/internal/syntax"
)

// loweredArm is an arm after lowering with the prelude of its candidates.
type loweredArm struct {
	arm *syntax.Arm
	pre []syntax.Stmt
}

// match lowers a match statement, where a subject that no candidate compares is evaluated without a temporary.
func (l *lowerer) match(s *syntax.Match) []syntax.Stmt {
	if l.nullableSubject(s) {
		return l.nullableMatch(s)
	}
	o := s.Origin
	var subject syntax.Expr
	var subjectType syntax.Type
	if s.Subject != nil {
		subject = l.expr(s.Subject, nil)
		subjectType = l.p.TypeOf(s.Subject)
	}
	predicate := false
	if s.Subject != nil && !isBool(subjectType) {
		for _, arm := range s.Arms {
			for _, p := range arm.Patterns {
				if arm.Kind == syntax.CaseArm && isBool(l.p.TypeOf(p)) {
					predicate = true
				}
			}
		}
	}
	var head []syntax.Stmt
	convert := func(p syntax.Expr) syntax.Expr { return l.expr(p, subjectType) }
	needName := false
	for _, arm := range s.Arms {
		for _, p := range arm.Patterns {
			if arm.Kind == syntax.CaseArm && hasPrelude(p) {
				needName = true
			}
		}
	}
	if predicate || needName && subject != nil && !isName(subject) {
		t := l.fresh("t")
		head = append(head, &syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(subjectType, o), Name: t}}, Values: []syntax.Expr{subject}})
		l.declare(t, subjectType)
		subject = name(o, t)
	}
	if predicate {
		t := subject.(*syntax.Name).Name
		convert = func(p syntax.Expr) syntax.Expr {
			if isBool(l.p.TypeOf(p)) {
				return l.expr(p, nil)
			}
			return &syntax.Prim{Origin: p.Where(), Op: "==", Args: []syntax.Expr{name(o, t), l.expr(p, subjectType)}}
		}
		subject = nil
	}
	var arms []loweredArm
	for _, arm := range s.Arms {
		arms = append(arms, l.arm(arm, convert))
	}
	again := func() syntax.Expr {
		if n, ok := subject.(*syntax.Name); ok {
			return name(o, n.Name)
		}
		return subject
	}
	out := &syntax.Match{Origin: o, Subject: subject, Arms: nestArms(o, again, arms)}
	if predicate && !reads(out, head[0].(*syntax.VarDecl).Targets[0].Name) {
		value := head[0].(*syntax.VarDecl).Values[0]
		switch {
		case isName(value) || isLit(value):
			head = nil
		case isCall(value):
			head = []syntax.Stmt{&syntax.ExprStmt{Origin: o, X: value}}
		default:
			head = []syntax.Stmt{&syntax.Match{Origin: o, Subject: value, Arms: []*syntax.Arm{anyArm(o, block(o, nil))}}}
		}
	}
	return append(head, out)
}

// reads reports whether a node reads a local name.
func reads(n syntax.Node, local string) bool {
	found := false
	syntax.Inspect(n, func(n syntax.Node) bool {
		if x, ok := n.(*syntax.Name); ok && x.Name == local && x.Module == "" && x.Owner == nil {
			found = true
		}
		return !found
	})
	return found
}

// nestArms lays out lowered arms, and the first arm whose candidates have a prelude, and every arm after it, move into a nested match inside an any arm, after that prelude.
func nestArms(o syntax.Origin, subject func() syntax.Expr, arms []loweredArm) []*syntax.Arm {
	var out []*syntax.Arm
	for i, a := range arms {
		if len(a.pre) == 0 {
			out = append(out, a.arm)
			continue
		}
		inner := &syntax.Match{Origin: a.arm.Origin, Subject: subject(), Arms: nestArms(o, subject, append([]loweredArm{{arm: a.arm}}, arms[i+1:]...))}
		out = append(out, anyArm(a.arm.Origin, block(a.arm.Origin, append(a.pre, inner))))
		return out
	}
	return out
}

// arm lowers one arm of a match in a scope of its own, converting each candidate.
func (l *lowerer) arm(arm *syntax.Arm, convert func(syntax.Expr) syntax.Expr) loweredArm {
	l.push()
	defer l.pop()
	if arm.Binding != "" {
		l.declare(arm.Binding, l.p.LocalType(arm))
	}
	var targets []*syntax.VarTarget
	for _, t := range arm.Targets {
		l.declare(t.Name, l.p.LocalType(t))
		targets = append(targets, &syntax.VarTarget{Origin: t.Origin, Type: l.erase(t.Type), Name: t.Name})
	}
	var pre []syntax.Stmt
	var patterns []syntax.Expr
	for _, p := range arm.Patterns {
		ps, v := l.sub(func() syntax.Expr { return convert(p) })
		pre = append(pre, ps...)
		patterns = append(patterns, v)
	}
	out := &syntax.Arm{Origin: arm.Origin, Kind: arm.Kind, Patterns: patterns, Type: l.erase(arm.Type), Binding: arm.Binding, Targets: targets}
	if arm.Body != nil {
		out.Body = l.body(arm.Body)
	}
	return loweredArm{arm: out, pre: pre}
}

// hasPrelude reports whether lowering an expression hoists statements: it contains a form that a rewrite turns into a match.
func hasPrelude(x syntax.Expr) bool {
	found := false
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.Ternary, *syntax.Coalesce, *syntax.Chain:
			found = true
		case *syntax.Closure:
			return false
		}
		return !found
	})
	return found
}

// isBool reports whether a type is Bool.
func isBool(t syntax.Type) bool {
	n, ok := t.(*syntax.Named)
	return ok && n.Module == "" && n.Name == "Bool"
}
