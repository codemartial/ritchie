// This file is the expression parser: one precedence-climbing routine over the twelve
// levels, then the postfix and primary forms.
package syntax

import (
	"slices"

	"ritchie.dev/internal/diagnostics"
)

// levelForm is how a precedence level combines its operands.
type levelForm uint8

const (
	postfixForm levelForm = iota
	prefixForm
	leftForm
	rightForm
	chainForm
	notForm
	ternaryForm
	assignForm
)

// level is one row of the precedence table.
type level struct {
	form levelForm
	ops  []Kind
}

var levels = [...]level{
	1:  {postfixForm, []Kind{Dot, LParen, LBracket}},
	2:  {prefixForm, []Kind{Minus}},
	3:  {leftForm, []Kind{Star, Slash, Percent}},
	4:  {leftForm, []Kind{Plus, Minus}},
	5:  {rightForm, []Kind{CoalesceOp}},
	6:  {chainForm, []Kind{Less, Greater, LessEq, GreaterEq}},
	7:  {chainForm, []Kind{EqEq, NotEq}},
	8:  {notForm, []Kind{KwNot}},
	9:  {leftForm, []Kind{KwAnd}},
	10: {leftForm, []Kind{KwOr}},
	11: {ternaryForm, []Kind{Question}},
	12: {assignForm, []Kind{Assign, PlusAssign, MinusAssign, StarAssign, SlashAssign, PercentAssign, CoalesceAssign}},
}

const (
	ternaryLevel = 11
	assignLevel  = 12
)

func (p *parser) atAny(ops []Kind) bool { return slices.Contains(ops, p.tok().Kind) }

// expr parses one expression and rejects an assignment operator after it where a statement is not permitted.
func (p *parser) expr() Expr {
	p.descend()
	defer p.ascend()
	start := p.pos
	x := p.level(ternaryLevel)
	if !p.allowAssign && p.atAny(levels[assignLevel].ops) {
		p.next()
		p.allowAssign = true
		p.level(ternaryLevel)
		p.allowAssign = false
		p.failAt(p.toks[start].Start, p.prevEnd(), diagnostics.UnexpectedToken, "an assignment is a statement and cannot stand as an operand")
	}
	return x
}

// level parses an expression of precedence level n of the table.
func (p *parser) level(n int) Expr {
	start := p.pos
	lv := levels[n]
	switch lv.form {
	case postfixForm:
		return p.postfix()
	case prefixForm:
		if p.atAny(lv.ops) {
			op := p.next().Text
			x := p.nested(n)
			return &Prim{Origin: p.org(start, PrimExpr), Op: op, Args: []Expr{x}}
		}
		switch {
		case p.word("wait") && p.peek(1).Kind == Variable && p.peek(1).Text == "send" && callAhead(p.peek(2)):
			p.next()
			return &Wait{Origin: p.org(start, WaitExpr), X: p.send(false)}
		case p.word("wait") && p.peek(1).Kind == Variable:
			p.next()
			x := p.nested(n - 1)
			return &Wait{Origin: p.org(start, WaitExpr), X: x}
		case p.word("send") && callAhead(p.peek(1)):
			return p.send(true)
		}
		return p.level(n - 1)
	case leftForm:
		x := p.level(n - 1)
		for p.atAny(lv.ops) {
			op := p.next()
			y := p.level(n - 1)
			if op.Kind == KwAnd || op.Kind == KwOr {
				x = &Logical{Origin: p.org(start, LogicalExpr), Op: op.Text, Left: x, Right: y}
			} else {
				x = &Prim{Origin: p.org(start, PrimExpr), Op: op.Text, Args: []Expr{x, y}}
			}
		}
		return x
	case rightForm:
		x := p.level(n - 1)
		if !p.atAny(lv.ops) {
			return x
		}
		op := p.next()
		y := p.nested(n)
		p.noTernaryOperand(x)
		p.noTernaryOperand(y)
		return &Coalesce{Origin: p.org(start, CoalesceExpr), Op: op.Start, Left: x, Right: y}
	case chainForm:
		return p.chain(n)
	case notForm:
		if p.atAny(lv.ops) {
			p.next()
			x := p.nested(n)
			return &Prim{Origin: p.org(start, PrimExpr), Op: "not", Args: []Expr{x}}
		}
		return p.level(n - 1)
	case ternaryForm:
		c := p.level(n - 1)
		if !p.atAny(lv.ops) {
			return c
		}
		q := p.next()
		a := p.level(n - 1)
		p.expect(Colon)
		b := p.level(n - 1)
		for i, x := range []Expr{c, a, b} {
			switch x := x.(type) {
			case *Ternary:
				if i == 0 {
					p.failAt(q.Start, q.End, diagnostics.TernaryNesting, "a ternary cannot be the condition of another ternary")
				} else {
					p.failAt(x.Op, x.Op+len("?"), diagnostics.TernaryNesting, "a ternary cannot be an arm of another ternary")
				}
			case *Coalesce:
				p.failAt(x.Op, x.Op+len("??"), diagnostics.TernaryCoalescingMix, "?? cannot be a direct operand of a ternary")
			}
		}
		if p.atAny(lv.ops) {
			p.fail("a ternary cannot be an arm of another ternary")
		}
		return &Ternary{Origin: p.org(start, TernaryExpr), Op: q.Start, Cond: c, Then: a, Else: b}
	}
	p.fail("expected an expression")
	return nil
}

// callAhead reports whether a token can begin the call that a send takes, which no token after a name in a source program can.
func callAhead(t Token) bool {
	switch t.Kind {
	case Variable, TypeName, ServiceType, ProjectionType, QueryName:
		return true
	}
	return false
}

// send parses the send form of the kernel at the word send: the call that it hands to the runtime, and its arms when the form can carry them and a brace follows.
func (p *parser) send(arms bool) Expr {
	start := p.pos
	p.next()
	x := p.nested(1)
	switch x.(type) {
	case *Call, *ServiceCall, *QueryCall:
	default:
		o := x.Where()
		p.failAt(o.Start, o.End, diagnostics.UnexpectedToken, "a send takes a call")
	}
	s := &Send{X: x}
	if arms && p.got(LBrace) {
		p.skipNewlines()
		for !p.at(RBrace) && !p.at(EOF) {
			s.Arms = append(s.Arms, p.sendArm())
			p.skipNewlines()
		}
		p.expect(RBrace)
	}
	s.Origin = p.org(start, SendExpr)
	return s
}

// sendArm parses one arm of a send: case and an error type, or any!, then the known values that the arm supplies, an empty brace pair for none, or return with the values that the routine returns.
func (p *parser) sendArm() *SendArm {
	start := p.pos
	arm := &SendArm{}
	if !p.got(KwAnyError) {
		p.expect(KwCase)
		arm.Type = p.named()
	}
	p.expect(FatArrow)
	switch {
	case p.got(KwReturn):
		arm.Return = true
		arm.Values = p.optionalValues()
	case p.at(LBrace):
		p.next()
		p.expect(RBrace)
	default:
		arm.Values = p.valueList()
	}
	arm.Origin = p.org(start, SendArmNode)
	return arm
}

// nested parses the operand of a prefix or right-associative operator of level n one level deeper.
func (p *parser) nested(n int) Expr {
	p.descend()
	defer p.ascend()
	return p.level(n)
}

// chain parses a comparison level, where two operators over three operands form one chain.
func (p *parser) chain(n int) Expr {
	start := p.pos
	ops := levels[n].ops
	x := p.level(n - 1)
	if !p.atAny(ops) {
		return x
	}
	op1 := p.next()
	y := p.level(n - 1)
	if !p.atAny(ops) {
		return &Prim{Origin: p.org(start, PrimExpr), Op: op1.Text, Args: []Expr{x, y}}
	}
	op2 := p.next()
	z := p.level(n - 1)
	if !sameDirection(op1.Kind, op2.Kind) {
		p.failAt(op2.Start, op2.End, diagnostics.InvalidChain, "a chained comparison uses operators of one direction")
	}
	if p.atAny(ops) {
		t := p.tok()
		p.failAt(t.Start, t.End, diagnostics.InvalidChain, "a chained comparison has at most three operands")
	}
	return &Chain{Origin: p.org(start, ChainExpr), Operands: []Expr{x, y, z}, Ops: []string{op1.Text, op2.Text}}
}

func sameDirection(a, b Kind) bool {
	ascending := func(k Kind) bool { return k == Less || k == LessEq }
	descending := func(k Kind) bool { return k == Greater || k == GreaterEq }
	return ascending(a) && ascending(b) || descending(a) && descending(b) || a == b
}

func (p *parser) noTernaryOperand(x Expr) {
	if t, is := x.(*Ternary); is {
		p.failAt(t.Op, t.Op+len("?"), diagnostics.TernaryCoalescingMix, "a ternary cannot be a direct operand of ??")
	}
}

func (p *parser) postfix() Expr {
	start := p.pos
	x := p.primary()
	for {
		switch p.tok().Kind {
		case Dot:
			p.next()
			if t := p.tok(); t.Kind == Constant {
				p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a member name is a VARIABLE name; "+diagnostics.Code(t.Text)+" is a CONSTANT")
			}
			name := p.expect(Variable).Text
			if p.at(LParen) {
				args := p.args()
				x = &MethodCall{Origin: p.org(start, MethodCallExpr), Receiver: x, Name: name, Args: args}
			} else {
				x = &FieldRead{Origin: p.org(start, FieldReadExpr), X: x, Name: name}
			}
		case LParen:
			args := p.args()
			x = &Call{Origin: p.org(start, CallExpr), Callee: x, Args: args}
		case LBracket:
			p.next()
			var lo, hi Expr
			if !p.at(Colon) {
				lo = p.expr()
			}
			if p.got(Colon) {
				if !p.at(RBracket) {
					hi = p.expr()
				}
				p.expect(RBracket)
				x = &Slice{Origin: p.org(start, SliceExpr), X: x, Lo: lo, Hi: hi}
				continue
			}
			p.expect(RBracket)
			x = &IndexRead{Origin: p.org(start, IndexReadExpr), X: x, Index: lo}
		default:
			return x
		}
	}
}

func (p *parser) primary() Expr {
	start := p.pos
	t := p.tok()
	switch t.Kind {
	case IntLit, FloatLit, StringLit, RawStringLit, BytesLit, RawBytesLit, DurationLit, DateLit, TimeLit, DateTimeLit,
		KwTrue, KwFalse, KwNone:
		p.next()
		return &Lit{Origin: p.org(start, LitExpr), Kind: t.Kind, Text: t.Text}
	case FStringLit:
		p.next()
		return p.format(start, t)
	case KwSelf:
		p.next()
		return &Self{p.org(start, SelfExpr)}
	case KwCreate:
		return p.create()
	case Variable:
		if p.peek(1).Kind == FatArrow && !p.pattern {
			return p.closure(start)
		}
		if p.peek(1).Kind != ColonColon {
			p.next()
			return &Name{Origin: p.org(start, NameExpr), Name: t.Text}
		}
		module := p.next().Text
		p.next()
		switch p.tok().Kind {
		case Variable, Constant:
			name := p.next().Text
			p.noRepeatedQualifier()
			return &Name{Origin: p.org(start, NameExpr), Module: module, Name: name}
		}
		return p.typedPrimary(start, module)
	case Constant:
		p.next()
		return &Name{Origin: p.org(start, NameExpr), Name: t.Text}
	case TypeName, TypeParam, EntityType, ServiceType, ProjectionType, ErrorType:
		return p.typedPrimary(start, "")
	case QueryName:
		p.next()
		args := p.args()
		return &QueryCall{Origin: p.org(start, QueryCallExpr), Name: t.Text, Args: args}
	case LParen:
		if !p.pattern && p.closureAhead() {
			return p.closure(start)
		}
		p.next()
		x := p.expr()
		p.expect(RParen)
		return x
	case LBracket:
		return p.collection()
	case KwNot:
		p.fail("not cannot be the operand of a tighter operator; write (not ...)")
	}
	p.fail("expected an expression")
	return nil
}

// noRepeatedQualifier rejects a second module separator after a qualified name.
func (p *parser) noRepeatedQualifier() {
	if p.at(ColonColon) {
		t := p.tok()
		p.failAt(t.Start, t.End, diagnostics.RepeatedQualifier, "a name has at most one module qualifier")
	}
}

// typedPrimary parses the primary forms that begin with a type name: construction, member references, lookups, and direct calls.
func (p *parser) typedPrimary(start int, module string) Expr {
	t := p.next()
	named := &Named{Origin: p.org(start, NamedType), Module: module, Name: t.Text}
	switch t.Kind {
	case TypeName:
		if p.at(Less) {
			named.Args = p.typeArgs()
			named.Origin = p.org(start, NamedType)
		}
		p.noRepeatedQualifier()
		if p.at(LBrace) {
			args := p.initList()
			return &Construct{Origin: p.org(start, ConstructExpr), Type: named, Args: args}
		}
		return p.typeMember(start, p.arraySuffixes(start, named))
	case TypeParam:
		return p.typeMember(start, named)
	case EntityType:
		p.noRepeatedQualifier()
		if p.at(LBracket) {
			return p.lookup(start, named)
		}
		return p.typeMember(start, named)
	case ServiceType:
		p.noRepeatedQualifier()
		p.expect(Dot)
		name := p.expect(Variable).Text
		args := p.args()
		return &ServiceCall{Origin: p.org(start, ServiceCallExpr), Service: named, Name: name, Args: args}
	case ProjectionType:
		p.noRepeatedQualifier()
		p.expect(Dot)
		name := p.expect(Variable).Text
		args := p.args()
		return &QueryCall{Origin: p.org(start, QueryCallExpr), Projection: named, Name: name, Args: args}
	}
	p.noRepeatedQualifier()
	var args []*Arg
	if p.at(LBrace) {
		args = p.initList()
	}
	return &Construct{Origin: p.org(start, ConstructExpr), Type: named, Args: args}
}

// typeMember parses the member reference after a type-member owner.
func (p *parser) typeMember(start int, owner Type) Expr {
	if isKeyword(p.tok()) {
		p.fail("a type name cannot stand as a value; expected a construction or a member")
	}
	if !p.at(Dot) {
		p.failAt(p.toks[start].Start, p.prevEnd(), diagnostics.UnexpectedClass, "a type name cannot stand as a value; expected a construction or a member")
	}
	p.next()
	if !p.at(Variable) && !p.at(Constant) {
		p.fail("expected a member name after the type")
	}
	name := p.next().Text
	return &Name{Origin: p.org(start, NameExpr), Owner: owner, Name: name}
}

// lookup parses an entity lookup and its get-or-create continuation.
func (p *parser) lookup(start int, entity *Named) Expr {
	p.expect(LBracket)
	key := ""
	if p.at(Variable) && p.peek(1).Kind == Colon {
		key = p.next().Text
		p.next()
	}
	value := p.expr()
	p.expect(RBracket)
	l := &Lookup{Origin: p.org(start, LookupExpr), Type: entity, Key: key, Value: value}
	if !p.at(KwElse) {
		return l
	}
	p.next()
	c := p.create()
	var then *Block
	if p.got(KwThen) {
		then = p.block()
	}
	return &GetOrCreate{Origin: p.org(start, GetOrCreateExpr), Lookup: l, Create: c, Then: then}
}

func (p *parser) create() *Create {
	start := p.pos
	p.expect(KwCreate)
	ts := p.pos
	t := p.expect(EntityType)
	entity := &Named{Origin: p.org(ts, NamedType), Name: t.Text}
	args := p.initList()
	return &Create{Origin: p.org(start, CreateExpr), Type: entity, Args: args}
}

// closureAhead reports whether the parenthesis at the current token opens a closure parameter list.
func (p *parser) closureAhead() bool {
	depth := 0
	for i := 0; ; i++ {
		switch p.peek(i).Kind {
		case LParen:
			depth++
		case RParen:
			depth--
			if depth == 0 {
				return p.peek(i+1).Kind == FatArrow
			}
		case EOF:
			return false
		}
	}
}

func (p *parser) closure(start int) Expr {
	var params []*Param
	if p.at(Variable) {
		ps := p.pos
		name := p.next().Text
		params = []*Param{{Origin: p.org(ps, ParamNode), Name: name}}
	} else {
		p.expect(LParen)
		for !p.at(RParen) {
			ps := p.pos
			t, _ := p.tryType()
			name := p.expect(Variable).Text
			params = append(params, &Param{Origin: p.org(ps, ParamNode), Type: t, Name: name})
			if !p.got(Comma) {
				break
			}
		}
		p.expect(RParen)
	}
	p.expect(FatArrow)
	c := &Closure{Params: params}
	if p.at(LBrace) {
		c.Body = p.block()
	} else {
		c.Expr = p.expr()
	}
	c.Origin = p.org(start, ClosureExpr)
	return c
}

func (p *parser) collection() Expr {
	start := p.pos
	p.expect(LBracket)
	if p.got(RBracket) {
		return &ArrayLit{Origin: p.org(start, ArrayLitExpr)}
	}
	if p.at(Colon) && p.peek(1).Kind == RBracket {
		p.next()
		p.next()
		return &DictLit{Origin: p.org(start, DictLitExpr)}
	}
	es := p.pos
	first := p.expr()
	if p.got(Colon) {
		value := p.expr()
		entries := []*DictEntry{{Origin: p.org(es, DictLitExpr), Key: first, Value: value}}
		for p.got(Comma) && !p.at(RBracket) {
			es = p.pos
			key := p.expr()
			p.expect(Colon)
			value := p.expr()
			entries = append(entries, &DictEntry{Origin: p.org(es, DictLitExpr), Key: key, Value: value})
		}
		p.expect(RBracket)
		return &DictLit{Origin: p.org(start, DictLitExpr), Entries: entries}
	}
	elems := []Expr{first}
	for p.got(Comma) && !p.at(RBracket) {
		elems = append(elems, p.expr())
	}
	p.expect(RBracket)
	return &ArrayLit{Origin: p.org(start, ArrayLitExpr), Elems: elems}
}

// args parses a parenthesised argument list, positional or named.
func (p *parser) args() []*Arg {
	p.expect(LParen)
	pattern := p.pattern
	p.pattern = false
	defer func() { p.pattern = pattern }()
	var args []*Arg
	for !p.at(RParen) {
		args = append(args, p.arg())
		if !p.got(Comma) {
			break
		}
	}
	p.expect(RParen)
	return args
}

func (p *parser) arg() *Arg {
	start := p.pos
	name := ""
	if p.at(Variable) && p.peek(1).Kind == Colon {
		name = p.next().Text
		p.next()
	}
	value := p.expr()
	return &Arg{Origin: p.org(start, ArgNode), Name: name, Value: value}
}

// initList parses a braced initializer list, whose entries are separated by commas or newlines.
func (p *parser) initList() []*Arg {
	p.expect(LBrace)
	pattern := p.pattern
	p.pattern = false
	defer func() { p.pattern = pattern }()
	var args []*Arg
	p.skipNewlines()
	for !p.at(RBrace) && !p.at(EOF) {
		args = append(args, p.arg())
		if !p.listSep() {
			break
		}
	}
	p.expect(RBrace)
	return args
}

// listSep consumes a comma or newline separator and reports whether one was present.
func (p *parser) listSep() bool {
	found := p.at(Newline) || p.at(Comma)
	p.skipNewlines()
	if p.got(Comma) {
		p.skipNewlines()
	}
	return found
}

// format parses the slots of a formatted string token into a Format expression.
func (p *parser) format(start int, t Token) Expr {
	f := &Format{Origin: p.org(start, FormatExpr)}
	for _, part := range t.Parts {
		if !part.Slot {
			f.Parts = append(f.Parts, FormatPart{Text: part.Text})
			continue
		}
		f.Parts = append(f.Parts, FormatPart{Slot: p.slot(part.Start, part.End)})
	}
	return f
}

// slot lexes and parses one interpolation slot as an expression at its span.
func (p *parser) slot(start, end int) Expr {
	toks, diags := lexRange(p.file, start, end)
	sub := &parser{file: p.file, toks: toks, allowAssign: true, depth: p.depth}
	var x Expr
	failed := !sub.recovered(0, func() {
		x = sub.expr()
		if !sub.at(EOF) {
			sub.failAt(start, end, diagnostics.InterpolationInvalidExpression, "a slot holds one expression without assignment or handler")
		}
	})
	p.diags = append(p.diags, diags...)
	p.diags = append(p.diags, sub.diags...)
	if failed || len(diags) > 0 {
		panic(bailout{})
	}
	return x
}
