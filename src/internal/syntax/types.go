// This file parses written types.
package syntax

import "ritchie.dev/internal/diagnostics"

// parseType parses a type: a named type with its nullable and array suffixes, a function type, a dictionary type, or the future type of the kernel, which the word future before < begins.
func (p *parser) parseType() Type {
	p.descend()
	defer p.ascend()
	start := p.pos
	switch p.tok().Kind {
	case LParen:
		p.next()
		params := p.typeList(RParen)
		p.expect(RParen)
		var results []Type
		if p.got(Arrow) {
			results = p.returnType()
		}
		return &FuncType{Origin: p.org(start, FuncTypeNode), Params: params, Results: results}
	case LBracket:
		p.next()
		key := p.parseType()
		p.expect(Colon)
		value := p.parseType()
		p.expect(RBracket)
		return p.arraySuffixes(start, &Dict{Origin: p.org(start, DictType), Key: key, Value: value})
	case Variable:
		if p.word("future") && p.peek(1).Kind == Less {
			p.next()
			p.next()
			elem := p.parseType()
			p.expect(Greater)
			return p.arraySuffixes(start, &Future{Origin: p.org(start, FutureType), Elem: elem})
		}
	}
	return p.arraySuffixes(start, p.named())
}

// arraySuffixes parses the nullable and array suffixes after an element type, each array dynamic or with a length written as an integer literal or a constant name.
func (p *parser) arraySuffixes(start int, t Type) Type {
	for p.at(LBracket) || p.at(Question) {
		if p.got(Question) {
			t = &Nullable{Origin: p.org(start, NullableType), Elem: t}
			continue
		}
		p.next()
		var length Expr
		ls := p.pos
		switch {
		case p.at(IntLit):
			lit := p.next()
			length = &Lit{Origin: p.org(ls, LitExpr), Kind: lit.Kind, Text: lit.Text}
		case p.at(Constant):
			name := p.next().Text
			length = &Name{Origin: p.org(ls, NameExpr), Name: name}
		case p.at(Variable) && p.peek(1).Kind == ColonColon && p.peek(2).Kind == Constant:
			module := p.next().Text
			p.next()
			name := p.next().Text
			length = &Name{Origin: p.org(ls, NameExpr), Module: module, Name: name}
		}
		p.expect(RBracket)
		t = &Array{Origin: p.org(start, ArrayType), Elem: t, Length: length}
	}
	return t
}

// named parses a named type with its optional module qualifier, type arguments, and id-type suffix.
func (p *parser) named() *Named {
	start := p.pos
	module := ""
	if p.at(Variable) && p.peek(1).Kind == ColonColon {
		module = p.next().Text
		p.next()
		if p.at(Variable) && p.peek(1).Kind == ColonColon {
			t := p.peek(1)
			p.failAt(t.Start, t.End, diagnostics.RepeatedQualifier, "a name has at most one module qualifier")
		}
	}
	t := p.tok()
	switch t.Kind {
	case TypeName, TypeParam, EntityType, ServiceType, ProjectionType, ErrorType:
		p.next()
	default:
		p.fail("expected a type")
	}
	n := &Named{Module: module, Name: t.Text}
	if t.Kind == TypeName && p.at(Less) {
		n.Args = p.typeArgs()
	}
	if t.Kind == EntityType && p.got(Hash) {
		n.Name += "#"
	}
	p.noRepeatedQualifier()
	n.Origin = p.org(start, NamedType)
	return n
}

func (p *parser) typeArgs() []Type {
	p.expect(Less)
	args := p.typeList(Greater)
	p.expect(Greater)
	return args
}

// typeList parses comma-separated types up to the closing token.
func (p *parser) typeList(close Kind) []Type {
	var list []Type
	for !p.at(close) {
		list = append(list, p.parseType())
		if !p.got(Comma) {
			break
		}
	}
	return list
}

// returnType parses the result of a routine: one type, or two or more types in parentheses.
func (p *parser) returnType() []Type {
	p.descend()
	defer p.ascend()
	if !p.at(LParen) {
		return []Type{p.parseType()}
	}
	start := p.pos
	p.next()
	list := p.typeList(RParen)
	p.expect(RParen)
	if p.at(Arrow) {
		p.next()
		results := p.returnType()
		return []Type{&FuncType{Origin: p.org(start, FuncTypeNode), Params: list, Results: results}}
	}
	if len(list) < 2 {
		p.failAt(p.toks[start].Start, p.prevEnd(), diagnostics.FunctionResultArity, "a parenthesised result list names two or more types")
	}
	return list
}

// returnSignature parses a success form and the declared error types after it.
func (p *parser) returnSignature(f *Func) {
	if p.got(KwNone) {
		f.ResultNone = true
	} else {
		f.Results = p.returnType()
	}
	for p.got(Bar) {
		f.Raises = append(f.Raises, p.named())
	}
}
