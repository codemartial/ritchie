// This file is the handler family of phase A: every handler form becomes a handler on a
// block, which phase B lowers to outcome matches.
package lower

import (
	"ritchie.dev/internal/syntax"
)

// handled lowers a handled statement to the block handler intermediate.
func (l *lowerer) handled(s *syntax.Handled) []syntax.Stmt {
	o := s.Origin
	l.protected++
	defer func() { l.protected-- }()
	switch st := s.Stmt.(type) {
	case *syntax.VarDecl:
		return l.inlineDeclaration(s, st)
	case *syntax.Block:
		arms := l.handlerArms(s.Arms, nil)
		l.push()
		var inner []syntax.Stmt
		for _, x := range st.Stmts {
			if l.stopped {
				break
			}
			inner = append(inner, l.region(x)...)
		}
		l.pop()
		return []syntax.Stmt{&syntax.Handled{Origin: o, Stmt: rebuilt(st, inner), Arms: arms}}
	case *syntax.For:
		return l.loop(st, l.protectBody(s, st.Body))
	case *syntax.CondFor:
		return l.loop(st, l.protectBody(s, st.Body))
	case *syntax.IterFor:
		return l.loop(st, l.protectBody(s, st.Body))
	case *syntax.If:
		return l.stmt(l.protectBranches(s, st))
	case *syntax.Match:
		arms := make([]*syntax.Arm, len(st.Arms))
		for i, arm := range st.Arms {
			a := *arm
			if arm.Body != nil {
				body, ok := arm.Body.(*syntax.Block)
				if !ok {
					body = block(arm.Body.Where(), []syntax.Stmt{arm.Body})
				}
				a.Body = &syntax.Handled{Origin: o, Stmt: &syntax.Block{Origin: introduced(body.Origin), Stmts: []syntax.Stmt{body}}, Arms: s.Arms}
			}
			arms[i] = &a
		}
		return l.stmt(&syntax.Match{Origin: st.Origin, Subject: st.Subject, Arms: arms})
	}
	arms := l.handlerArms(s.Arms, l.p.Recovered(s))
	return []syntax.Stmt{l.protect(o, l.stmt(s.Stmt), arms)}
}

// inlineDeclaration lowers an inline handler on a declaration: the declaration of each target, then the store of the values under the handler.
func (l *lowerer) inlineDeclaration(s *syntax.Handled, d *syntax.VarDecl) []syntax.Stmt {
	types := make([]syntax.Type, len(d.Targets))
	for i, t := range d.Targets {
		types[i] = l.p.LocalType(t)
	}
	arms := l.handlerArms(s.Arms, types)
	var out []syntax.Stmt
	var targets []syntax.Expr
	for i, t := range d.Targets {
		out = append(out, &syntax.VarDecl{Origin: d.Origin, Targets: []*syntax.VarTarget{{Origin: t.Origin, Type: l.erase(t.Type), Name: t.Name}}})
		targets = append(targets, name(t.Origin, t.Name))
		l.declare(t.Name, types[i])
	}
	pre, values := l.subValues(d.Values, types)
	body := append(pre, &syntax.Store{Origin: d.Origin, Targets: targets, Values: values})
	return append(out, l.protect(s.Origin, body, arms))
}

// region lowers one source statement of a protected block, where several statements go into one introduced block so that the handler recovers the source statement as a whole, and the variables the statement declares are declared before that block so that later statements read them.
func (l *lowerer) region(s syntax.Stmt) []syntax.Stmt {
	out := l.stmt(s)
	if len(out) <= 1 {
		return out
	}
	declared := map[string]bool{}
	d, ok := s.(*syntax.VarDecl)
	if h, inline := s.(*syntax.Handled); inline {
		d, ok = h.Stmt.(*syntax.VarDecl)
	}
	if ok {
		for _, t := range d.Targets {
			declared[t.Name] = true
		}
	}
	var decls, body []syntax.Stmt
	for _, x := range out {
		d, ok := x.(*syntax.VarDecl)
		if !ok || !declared[d.Targets[0].Name] {
			body = append(body, x)
			continue
		}
		var targets []syntax.Expr
		for _, t := range d.Targets {
			decls = append(decls, &syntax.VarDecl{Origin: d.Origin, Targets: []*syntax.VarTarget{t}})
			targets = append(targets, name(t.Origin, t.Name))
		}
		if len(d.Values) > 0 {
			body = append(body, &syntax.Store{Origin: d.Origin, Targets: targets, Values: d.Values})
		}
	}
	if len(body) == 1 {
		return append(decls, body...)
	}
	return append(decls, &syntax.Block{Origin: introduced(s.Where()), Stmts: body, Region: true})
}

// subValues lowers a value list with its own prelude.
func (l *lowerer) subValues(values []syntax.Expr, wants []syntax.Type) ([]syntax.Stmt, []syntax.Expr) {
	saved := l.pre
	l.pre = nil
	out := l.values(values, wants)
	pre := l.pre
	l.pre = saved
	return pre, out
}

// protect wraps the lowered statements of one protected statement in the block handler intermediate, where they form one region that the handler recovers as a whole, and records the handler as an inline handler.
func (l *lowerer) protect(o syntax.Origin, body []syntax.Stmt, arms []*syntax.Arm) syntax.Stmt {
	if len(body) != 1 || !isRegion(body[0]) {
		body = []syntax.Stmt{&syntax.Block{Origin: introduced(o), Stmts: body, Region: true}}
	}
	h := &syntax.Handled{Origin: o, Stmt: &syntax.Block{Origin: introduced(o), Stmts: body}, Arms: arms}
	l.inline[h] = true
	return h
}

// isRegion reports whether a statement is a region block.
func isRegion(s syntax.Stmt) bool {
	b, ok := s.(*syntax.Block)
	return ok && b.Region
}

// protectBody moves a handler from a loop onto a block that contains the loop body.
func (l *lowerer) protectBody(s *syntax.Handled, body *syntax.Block) *syntax.Block {
	h := &syntax.Handled{Origin: s.Origin, Stmt: &syntax.Block{Origin: introduced(body.Origin), Stmts: body.Stmts}, Arms: s.Arms}
	return block(body.Origin, []syntax.Stmt{h})
}

// protectBranches places the handler of an if on each branch block.
func (l *lowerer) protectBranches(s *syntax.Handled, st *syntax.If) *syntax.If {
	out := &syntax.If{Origin: st.Origin, Cond: st.Cond, Then: l.protectBody(s, st.Then)}
	switch e := st.Else.(type) {
	case *syntax.If:
		out.Else = l.protectBranches(s, e)
	case *syntax.Block:
		out.Else = l.protectBody(s, e)
	}
	return out
}

// introduced marks the origin of a block that a rewrite introduces, which phase B splices into its context.
func introduced(o syntax.Origin) syntax.Origin {
	o.Construct = syntax.HandledStmt
	return o
}

// handlerArms lowers the arms of a handler, where a concise arm becomes a brace arm that resumes its values lowered under wants, the results of the one statement that the handler recovers when it recovers one, a concise arm of one call without results becomes a brace arm that makes the call and resumes nothing, and exhaustiveness leaves no trace.
func (l *lowerer) handlerArms(arms []*syntax.Arm, wants []syntax.Type) []*syntax.Arm {
	out := make([]*syntax.Arm, len(arms))
	for i, arm := range arms {
		l.push()
		if arm.Binding != "" {
			l.declare(arm.Binding, l.p.LocalType(arm))
		}
		var body syntax.Stmt
		switch {
		case len(arm.Values) == 1 && isCall(arm.Values[0]) && len(l.p.Results(arm.Values[0])) == 0:
			call := &syntax.ExprStmt{Origin: arm.Values[0].Where(), X: arm.Values[0]}
			body = block(arm.Origin, append(l.stmt(call), &syntax.Resume{Origin: arm.Origin}))
		case arm.Values != nil:
			pre, values := l.subValues(arm.Values, wants)
			body = block(arm.Origin, append(pre, &syntax.Resume{Origin: arm.Origin, Values: values}))
		default:
			b := l.body(arm.Body)
			if _, ok := b.(*syntax.Block); !ok {
				b = block(arm.Origin, []syntax.Stmt{b})
			}
			body = b
		}
		l.pop()
		out[i] = &syntax.Arm{Origin: arm.Origin, Kind: arm.Kind, Type: l.erase(arm.Type), Binding: arm.Binding, Body: body}
	}
	return out
}
