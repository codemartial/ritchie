// This file parses the clause blocks of queries and sourced projections, in which the
// contextual words are keywords.
package syntax

var (
	multiplicative = levels[3].ops
	additive       = levels[4].ops
)

// queryBlock parses the braced clause block of a query.
func (p *parser) queryBlock() *QueryBlock {
	start := p.pos
	p.expect(LBrace)
	p.clause = true
	q := &QueryBlock{}
	p.skipNewlines()
	if !p.word("from") {
		p.fail("expected a from clause")
	}
	q.From = p.fromClause()
	p.skipNewlines()
	if p.at(KwWhere) {
		p.next()
		q.Filter = p.predicate()
		p.skipNewlines()
	}
	if p.word("group") {
		p.next()
		if !p.word("by") {
			p.fail("expected by after group")
		}
		p.next()
		q.Group = []Expr{p.groupKey()}
		for p.got(Comma) {
			q.Group = append(q.Group, p.groupKey())
		}
		p.skipNewlines()
	}
	if p.word("select") {
		q.Select = p.selectClause()
		p.skipNewlines()
	}
	if p.word("having") {
		p.next()
		q.Having = p.predicate()
		p.skipNewlines()
	}
	if p.word("sort") {
		p.next()
		q.Sort = []*SortKey{p.sortKey()}
		for p.got(Comma) {
			q.Sort = append(q.Sort, p.sortKey())
		}
		p.skipNewlines()
	}
	if p.word("limit") {
		p.next()
		ls := p.pos
		switch p.tok().Kind {
		case IntLit:
			t := p.next()
			q.Limit = &Lit{Origin: p.org(ls, LitExpr), Kind: t.Kind, Text: t.Text}
		case Constant:
			q.Limit = p.queryConstant()
		case Variable:
			if p.peek(1).Kind == ColonColon {
				q.Limit = p.queryConstant()
			} else {
				q.Limit = &Name{Origin: p.org(ls, NameExpr), Name: p.next().Text}
			}
		default:
			p.fail("expected a limit count")
		}
		p.skipNewlines()
	}
	p.clause = false
	p.expect(RBrace)
	q.Origin = p.org(start, QueryBlockNode)
	return q
}

func (p *parser) fromClause() []*Source {
	p.next()
	var sources []*Source
	for {
		start := p.pos
		s := &Source{Type: p.named()}
		s.Name = p.expect(Variable).Text
		s.Origin = p.org(start, SourceNode)
		sources = append(sources, s)
		if !p.got(Comma) {
			return sources
		}
	}
}

func (p *parser) selectClause() *Select {
	start := p.pos
	if !p.word("select") {
		p.fail("expected a select clause")
	}
	p.next()
	p.expect(LBrace)
	s := &Select{}
	p.skipNewlines()
	for !p.at(RBrace) && !p.at(EOF) {
		es := p.pos
		e := &SelectEntry{}
		if p.at(Variable) && p.peek(1).Kind == Colon {
			e.Name = p.next().Text
			p.next()
		}
		e.Value = p.selectExpr()
		e.Origin = p.org(es, SelectEntryNode)
		s.Entries = append(s.Entries, e)
		if !p.listSep() {
			break
		}
	}
	p.expect(RBrace)
	s.Origin = p.org(start, SelectNode)
	return s
}

func (p *parser) groupKey() Expr {
	if p.word("bucket") {
		return p.bucket()
	}
	return p.queryPath()
}

func (p *parser) sortKey() *SortKey {
	start := p.pos
	k := &SortKey{Path: p.queryPath()}
	if p.word("asc") || p.word("desc") {
		k.Order = p.next().Text
	}
	k.Origin = p.org(start, SortKeyNode)
	return k
}

// queryPath parses a variable with field selectors.
func (p *parser) queryPath() Expr {
	start := p.pos
	if !p.at(Variable) || contextual[p.tok().Text] {
		p.fail("expected a query path")
	}
	var x Expr = &Name{Origin: p.org(start, NameExpr), Name: p.next().Text}
	for p.got(Dot) {
		name := p.expect(Variable).Text
		x = &FieldRead{Origin: p.org(start, FieldReadExpr), X: x, Name: name}
	}
	return x
}

func (p *parser) queryConstant() Expr {
	start := p.pos
	module := ""
	if p.at(Variable) {
		module = p.next().Text
		p.expect(ColonColon)
	}
	name := p.expect(Constant).Text
	return &Name{Origin: p.org(start, NameExpr), Module: module, Name: name}
}

func (p *parser) bucket() Expr {
	start := p.pos
	p.next()
	p.expect(LParen)
	path := p.queryPath()
	p.expect(Comma)
	var width Expr
	switch p.tok().Kind {
	case DurationLit:
		ws := p.pos
		t := p.next()
		width = &Lit{Origin: p.org(ws, LitExpr), Kind: t.Kind, Text: t.Text}
	case Variable:
		if p.peek(1).Kind == ColonColon {
			width = p.queryConstant()
		} else {
			width = p.queryPath()
		}
	case Constant:
		width = p.queryConstant()
	default:
		p.fail("expected a bucket width")
	}
	p.expect(RParen)
	return &Prim{Origin: p.org(start, PrimExpr), Op: "bucket", Args: []Expr{path, width}}
}

// leftAssoc parses operands joined by the operators of one level into left-nested primitives.
func (p *parser) leftAssoc(ops []Kind, operand func() Expr) Expr {
	start := p.pos
	x := operand()
	for p.atAny(ops) {
		op := p.next().Text
		y := operand()
		x = &Prim{Origin: p.org(start, PrimExpr), Op: op, Args: []Expr{x, y}}
	}
	return x
}

// selectExpr parses the additive select grammar over aggregates, paths, constants, and numbers.
func (p *parser) selectExpr() Expr { return p.leftAssoc(additive, p.selectTerm) }

func (p *parser) selectTerm() Expr { return p.leftAssoc(multiplicative, p.selectAtom) }

func (p *parser) selectAtom() Expr {
	p.descend()
	defer p.ascend()
	start := p.pos
	switch t := p.tok(); t.Kind {
	case IntLit, FloatLit:
		p.next()
		return &Lit{Origin: p.org(start, LitExpr), Kind: t.Kind, Text: t.Text}
	case LParen:
		p.next()
		x := p.selectExpr()
		p.expect(RParen)
		return x
	case Constant:
		return p.queryConstant()
	case Variable:
		switch {
		case t.Text == "bucket":
			return p.bucket()
		case contextual[t.Text] && p.peek(1).Kind == LParen:
			p.next()
			p.next()
			path := p.queryPath()
			p.expect(RParen)
			return &Prim{Origin: p.org(start, PrimExpr), Op: t.Text, Args: []Expr{path}}
		case p.peek(1).Kind == ColonColon:
			return p.queryConstant()
		}
		return p.queryPath()
	}
	p.fail("expected a select expression")
	return nil
}

// predicate parses the restricted predicate grammar of where and having clauses.
func (p *parser) predicate() Expr {
	start := p.pos
	x := p.predicateAnd()
	for p.at(KwOr) {
		p.next()
		y := p.predicateAnd()
		x = &Logical{Origin: p.org(start, LogicalExpr), Op: "or", Left: x, Right: y}
	}
	return x
}

func (p *parser) predicateAnd() Expr {
	start := p.pos
	x := p.predicateNot()
	for p.at(KwAnd) {
		p.next()
		y := p.predicateNot()
		x = &Logical{Origin: p.org(start, LogicalExpr), Op: "and", Left: x, Right: y}
	}
	return x
}

func (p *parser) predicateNot() Expr {
	p.descend()
	defer p.ascend()
	start := p.pos
	if p.got(KwNot) {
		x := p.predicateNot()
		return &Prim{Origin: p.org(start, PrimExpr), Op: "not", Args: []Expr{x}}
	}
	return p.comparison()
}

func isComparison(k Kind) bool {
	switch k {
	case EqEq, NotEq, Less, Greater, LessEq, GreaterEq:
		return true
	}
	return false
}

// comparison parses one comparison, a two-operator chain, a membership test, or a parenthesised predicate.
func (p *parser) comparison() Expr {
	start := p.pos
	if p.at(LParen) && p.predicateAhead() {
		p.next()
		x := p.predicate()
		p.expect(RParen)
		return x
	}
	x := p.queryValue()
	if p.got(KwIn) {
		y := p.queryValue()
		return &Prim{Origin: p.org(start, PrimExpr), Op: "in", Args: []Expr{x, y}}
	}
	if !isComparison(p.tok().Kind) {
		p.fail("expected a comparison operator")
	}
	op1 := p.next().Text
	y := p.queryValue()
	if !isComparison(p.tok().Kind) {
		return &Prim{Origin: p.org(start, PrimExpr), Op: op1, Args: []Expr{x, y}}
	}
	op2 := p.next().Text
	z := p.queryValue()
	return &Chain{Origin: p.org(start, ChainExpr), Operands: []Expr{x, y, z}, Ops: []string{op1, op2}}
}

// predicateAhead reports whether the parenthesis at the current token encloses a predicate.
func (p *parser) predicateAhead() bool {
	depth := 0
	for i := 0; ; i++ {
		switch k := p.peek(i).Kind; {
		case k == LParen:
			depth++
		case k == RParen:
			depth--
			if depth == 0 {
				return false
			}
		case k == EOF:
			return false
		case depth == 1 && (isComparison(k) || k == KwIn || k == KwAnd || k == KwOr || k == KwNot):
			return true
		}
	}
}

func (p *parser) queryValue() Expr { return p.leftAssoc(additive, p.queryTerm) }

func (p *parser) queryTerm() Expr { return p.leftAssoc(multiplicative, p.queryAtom) }

func (p *parser) queryAtom() Expr {
	p.descend()
	defer p.ascend()
	start := p.pos
	switch t := p.tok(); t.Kind {
	case IntLit, FloatLit, StringLit, RawStringLit, DateLit, TimeLit, DateTimeLit, DurationLit, KwTrue, KwFalse, KwNone:
		p.next()
		return &Lit{Origin: p.org(start, LitExpr), Kind: t.Kind, Text: t.Text}
	case LParen:
		p.next()
		x := p.queryValue()
		p.expect(RParen)
		return x
	case Constant:
		return p.queryConstant()
	case TypeName:
		return p.enumMember(start, "")
	case Variable:
		if p.peek(1).Kind != ColonColon {
			return p.queryPath()
		}
		if p.peek(2).Kind == TypeName {
			module := p.next().Text
			p.next()
			return p.enumMember(start, module)
		}
		return p.queryConstant()
	}
	p.fail("expected a query value")
	return nil
}

func (p *parser) enumMember(start int, module string) Expr {
	ts := p.pos
	owner := &Named{Module: module, Name: p.next().Text}
	owner.Origin = p.org(ts, NamedType)
	p.expect(Dot)
	name := p.expect(Constant).Text
	return &Name{Origin: p.org(start, NameExpr), Owner: owner, Name: name}
}
