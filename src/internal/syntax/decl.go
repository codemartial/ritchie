// This file parses top-level declarations.
package syntax

import (
	"strings"

	"ritchie.dev/internal/diagnostics"
)

// declaration decides the form from the first one to three tokens and parses it.
func (p *parser) declaration() Decl {
	start := p.pos
	t := p.tok()
	switch t.Kind {
	case KwContract:
		return p.contract()
	case Variable:
		if p.peek(1).Kind == LParen {
			return p.function(start)
		}
		if p.methodAhead() {
			return p.method(start)
		}
	case QueryName:
		if p.peek(1).Kind == LParen {
			return p.freeQuery(start)
		}
	case TypeName, TypeParam, EntityType, ServiceType, ProjectionType, ErrorType:
		if p.methodAhead() {
			return p.method(start)
		}
		if d := p.typeDeclaration(start); d != nil {
			return d
		}
	}
	if ty, ok := p.tryType(); ok && p.at(Constant) {
		name := p.next()
		p.initialized(name)
		value := p.initializer()
		return &Const{Origin: p.org(start, ConstDeclNode), Type: ty, Name: name.Text, Value: value}
	}
	p.pos = start
	p.misdeclared()
	p.fail("expected a declaration")
	return nil
}

// misdeclared reports a module-level line that is a constant declaration except for one part: a constant name without its type, or a typed name that is not in constant case.
func (p *parser) misdeclared() {
	if t := p.tok(); t.Kind == Constant && p.peek(1).Kind == Assign {
		example := ""
		switch p.peek(2).Kind {
		case StringLit, RawStringLit, FStringLit:
			example = "String"
		case IntLit:
			example = "Int"
		case FloatLit:
			example = "Float"
		case KwTrue, KwFalse:
			example = "Bool"
		}
		message := "expected the type of the constant before its name"
		if example != "" {
			message += ", as in " + diagnostics.Code(example+" "+t.Text)
		}
		p.failAt(t.Start, t.End, diagnostics.UnexpectedToken, message)
	}
	t, ok := p.typedName()
	switch {
	case !ok:
	case isKeyword(t):
		p.failAt(t.Start, t.End, diagnostics.UnexpectedToken, diagnostics.Code(t.Text)+" is a keyword and cannot name a constant")
	case t.Kind == Variable && len(t.Text) > 1:
		p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a module-level value is a constant, whose name is all uppercase, as in "+diagnostics.Code(strings.ToUpper(t.Text)))
	case t.Kind == Variable || isTypeClass(t.Kind):
		p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a module-level value is a constant, whose name is all uppercase and has two or more characters; "+diagnostics.Code(t.Text)+" is a name of another class")
	}
}

// typeDeclaration parses the declarations that begin with a type name, or returns nil when none applies.
func (p *parser) typeDeclaration(start int) Decl {
	t := p.tok()
	after := p.peek(1).Kind
	switch t.Kind {
	case TypeName:
		switch after {
		case LBrace:
			return p.structOrEnum(start)
		case Assign, Hash:
			return p.aliasOrSum(start)
		case Define:
			p.next()
			p.next()
			repr := p.parseType()
			return &Distinct{Origin: p.org(start, DistinctDecl), Name: t.Text, Repr: repr}
		case KwIs, Less:
			return p.conformance(start)
		}
	case EntityType:
		switch after {
		case LBrace:
			return p.entity(start)
		case Assign, Hash:
			return p.aliasOrSum(start)
		case KwIs:
			return p.conformance(start)
		}
	case ServiceType:
		switch after {
		case LBrace:
			p.next()
			p.next()
			p.skipNewlines()
			if !p.at(RBrace) {
				b := p.tok()
				p.failAt(b.Start, b.End, diagnostics.ServiceBodyNotEmpty, "a service body is empty")
			}
			p.next()
			return &Service{Origin: p.org(start, ServiceDecl), Name: t.Text}
		case Assign:
			return p.aliasOrSum(start)
		case KwIs:
			return p.conformance(start)
		}
	case ProjectionType:
		switch after {
		case LBrace:
			return p.projection(start)
		case Assign:
			return p.aliasOrSum(start)
		}
	case ErrorType:
		switch after {
		case Assign:
			return p.aliasOrSum(start)
		case LBrace, Newline, EOF:
			p.next()
			var fields []*Field
			if p.at(LBrace) {
				fields = p.fields(false)
			}
			return &Error{Origin: p.org(start, ErrorDecl), Name: t.Text, Fields: fields}
		}
	}
	return nil
}

// methodAhead reports whether a receiver, a dot, a name, and a parenthesis begin at the current token.
func (p *parser) methodAhead() bool {
	i := 0
	if p.peek(i).Kind == Variable && p.peek(i+1).Kind == ColonColon {
		i += 2
	}
	switch p.peek(i).Kind {
	case TypeName, TypeParam, EntityType, ServiceType, ProjectionType:
		i++
	default:
		return false
	}
	if p.peek(i).Kind == Less {
		for depth := 0; ; i++ {
			switch p.peek(i).Kind {
			case Less:
				depth++
			case Greater:
				depth--
			case EOF, Newline:
				return false
			}
			if depth == 0 {
				i++
				break
			}
		}
	}
	return p.peek(i).Kind == Dot && p.peek(i+1).Kind == Variable && p.peek(i+2).Kind == LParen
}

func (p *parser) function(start int) Decl {
	name := p.next().Text
	f := &Func{Name: name}
	p.signature(f)
	f.Body = p.block()
	f.Origin = p.org(start, FuncDecl)
	return f
}

func (p *parser) method(start int) Decl {
	receiver := p.named()
	p.expect(Dot)
	name := p.expect(Variable).Text
	if p.toks[start].Kind == ProjectionType || p.toks[start].Kind == Variable && p.toks[start+2].Kind == ProjectionType {
		q := &Query{Receiver: receiver, Name: name}
		p.expect(LParen)
		q.Params = p.params()
		p.expect(RParen)
		q.Clauses = p.queryBlock()
		q.Origin = p.org(start, QueryDecl)
		return q
	}
	f := &Func{Receiver: receiver, Name: name}
	p.signature(f)
	f.Body = p.block()
	f.Origin = p.org(start, FuncDecl)
	return f
}

// signature parses the parameter list, the return signature, and the where clause of a routine.
func (p *parser) signature(f *Func) {
	p.expect(LParen)
	f.Params = p.params()
	p.expect(RParen)
	if p.got(Arrow) {
		p.returnSignature(f)
	}
	if p.at(KwWhere) {
		f.Bounds = p.whereClause()
	}
}

func (p *parser) params() []*Param {
	var params []*Param
	for !p.at(RParen) {
		start := p.pos
		t := p.parseType()
		name := p.expect(Variable).Text
		var def Expr
		if p.got(Assign) {
			def = p.initializer()
		}
		params = append(params, &Param{Origin: p.org(start, ParamNode), Type: t, Name: name, Default: def})
		if !p.got(Comma) {
			break
		}
	}
	return params
}

func (p *parser) whereClause() []*Bound {
	p.expect(KwWhere)
	var bounds []*Bound
	for {
		start := p.pos
		param := p.expect(TypeParam).Text
		p.expect(KwIs)
		contracts := []Type{p.named()}
		for p.got(Comma) {
			contracts = append(contracts, p.named())
		}
		bounds = append(bounds, &Bound{Origin: p.org(start, BoundNode), Param: param, Contracts: contracts})
		if !p.got(KwAnd) {
			return bounds
		}
	}
}

func (p *parser) contract() Decl {
	start := p.pos
	p.expect(KwContract)
	name := p.expect(TypeName).Text
	p.expect(LBrace)
	var methods []*Func
	p.skipNewlines()
	for !p.at(RBrace) && !p.at(EOF) {
		ms := p.pos
		receiver := p.named()
		p.expect(Dot)
		m := &Func{Receiver: receiver, Name: p.expect(Variable).Text}
		p.signature(m)
		if p.at(LBrace) {
			m.Body = p.block()
		}
		m.Origin = p.org(ms, FuncDecl)
		methods = append(methods, m)
		p.terminate()
	}
	p.expect(RBrace)
	return &Contract{Origin: p.org(start, ContractDecl), Name: name, Methods: methods}
}

func (p *parser) conformance(start int) Decl {
	provider := p.named()
	p.expect(KwIs)
	contract := p.named()
	return &Conformance{Origin: p.org(start, ConformanceDecl), Provider: provider, Contract: contract}
}

func (p *parser) structOrEnum(start int) Decl {
	name := p.next().Text
	if p.peek(1).Kind == Constant || p.peek(1).Kind == Newline && p.peek(2).Kind == Constant {
		p.expect(LBrace)
		var members []*EnumMember
		p.skipNewlines()
		for !p.at(RBrace) && !p.at(EOF) {
			ms := p.pos
			m := &EnumMember{Name: p.expect(Constant).Text}
			if p.got(Colon) {
				m.Value = p.enumLiteral()
			}
			m.Origin = p.org(ms, EnumMemberNode)
			members = append(members, m)
			if !p.listSep() {
				break
			}
		}
		p.expect(RBrace)
		return &Enum{Origin: p.org(start, EnumDecl), Name: name, Members: members}
	}
	fields := p.fields(false)
	return &Struct{Origin: p.org(start, StructDecl), Name: name, Fields: fields}
}

func (p *parser) enumLiteral() Expr {
	start := p.pos
	switch t := p.tok(); t.Kind {
	case IntLit, FloatLit, StringLit, RawStringLit, DurationLit, DateLit, TimeLit, DateTimeLit, KwTrue, KwFalse:
		p.next()
		return &Lit{Origin: p.org(start, LitExpr), Kind: t.Kind, Text: t.Text}
	}
	p.fail("expected a literal as the enum backing value")
	return nil
}

// fields parses a braced field list of a struct, error, or entity.
func (p *parser) fields(entity bool) []*Field {
	p.expect(LBrace)
	var fields []*Field
	p.skipNewlines()
	for !p.at(RBrace) && !p.at(EOF) {
		start := p.pos
		f := &Field{}
		if entity {
			f.Unique = p.got(KwUnique)
		} else {
			f.Embed = p.got(KwEmbed)
		}
		f.Type = p.parseType()
		f.Name = p.expect(Variable).Text
		if p.got(Assign) {
			f.Default = p.initializer()
		}
		f.Origin = p.org(start, FieldNode)
		fields = append(fields, f)
		if !p.listSep() {
			break
		}
	}
	p.expect(RBrace)
	return fields
}

func (p *parser) entity(start int) Decl {
	name := p.next().Text
	fields := p.fields(true)
	return &Entity{Origin: p.org(start, EntityDecl), Name: name, Fields: fields}
}

// aliasOrSum parses an alias, or a sum when a bar separates members before the terminator.
func (p *parser) aliasOrSum(start int) Decl {
	name := p.next().Text
	if p.got(Hash) {
		name += "#"
	}
	p.expect(Assign)
	sum := false
	for i := 0; ; i++ {
		k := p.peek(i).Kind
		if k == Newline || k == EOF {
			break
		}
		if k == Bar {
			sum = true
			break
		}
	}
	if !sum {
		if t := p.tok(); t.Kind == Constant || t.Kind == Variable && p.peek(1).Kind != ColonColon {
			p.failAt(t.Start, t.End, diagnostics.AliasInvalidTarget, "an alias names a type; "+diagnostics.Code(t.Text)+" is not a type")
		}
		target := p.parseType()
		return &Alias{Origin: p.org(start, AliasDecl), Name: name, Target: target}
	}
	members := []Type{p.parseType()}
	for p.got(Bar) {
		members = append(members, p.parseType())
	}
	if len(members) < 2 {
		p.fail("a sum declaration lists two or more member types")
	}
	return &Sum{Origin: p.org(start, SumDecl), Name: name, Members: members}
}

func (p *parser) freeQuery(start int) Decl {
	q := &Query{Name: p.next().Text}
	p.expect(LParen)
	q.Params = p.params()
	p.expect(RParen)
	p.expect(Arrow)
	q.Result = p.named()
	q.Clauses = p.queryBlock()
	q.Origin = p.org(start, QueryDecl)
	return q
}

func (p *parser) projection(start int) Decl {
	name := p.next().Text
	pr := &Projection{Name: name}
	p.expect(LBrace)
	p.skipNewlines()
	if p.word("from") {
		p.clause = true
		pr.From = p.fromClause()
		p.skipNewlines()
		pr.Select = p.selectClause()
		p.clause = false
		p.skipNewlines()
	} else {
		for !p.at(RBrace) && !p.at(EOF) {
			fs := p.pos
			f := &Field{Type: p.parseType()}
			f.Name = p.expect(Variable).Text
			f.Origin = p.org(fs, FieldNode)
			pr.Columns = append(pr.Columns, f)
			if !p.listSep() {
				break
			}
		}
	}
	p.expect(RBrace)
	pr.Origin = p.org(start, ProjectionDecl)
	return pr
}
