// This file is the parser core: token access, failure and recovery, and the statement
// forms.
package syntax

import (
	"strconv"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/source"
)

const maxDepth = 1000

type parser struct {
	file        *source.File
	toks        []Token
	pos         int
	diags       []diagnostics.Record
	clause      bool
	allowAssign bool
	pattern     bool
	speculating int
	depth       int
}

// bailout is the panic value that abandons the statement being parsed after its diagnostic.
type bailout struct{}

// Parse lexes and parses one file into its tree, with the diagnostics of every lexical and syntax fault.
func Parse(f *source.File) (*File, []diagnostics.Record) {
	toks, diags := Lex(f)
	p := &parser{file: f, toks: toks, diags: diags}
	file := &File{Origin{File: f, Start: f.ContentStart(), End: len(f.Text), Construct: FileNode}, nil}
	p.skipNewlines()
	for !p.at(EOF) {
		start := p.pos
		var d Decl
		if !p.recovered(start, func() { d = p.declaration(); p.terminate() }) {
			if p.pos == start {
				p.pos++
			}
			d = &Bad{p.org(start, BadNode)}
		}
		file.Decls = append(file.Decls, d)
		p.skipNewlines()
	}
	return file, p.diags
}

func (p *parser) tok() Token { return p.toks[p.pos] }

func (p *parser) peek(n int) Token {
	if p.pos+n < len(p.toks) {
		return p.toks[p.pos+n]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) at(k Kind) bool { return p.toks[p.pos].Kind == k }

func (p *parser) next() Token {
	t := p.toks[p.pos]
	if t.Kind != EOF {
		p.pos++
	}
	return t
}

func (p *parser) got(k Kind) bool {
	if p.at(k) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(k Kind) Token {
	if !p.at(k) {
		p.fail("expected " + diagnostics.Code(k.String()))
	}
	return p.next()
}

func (p *parser) skipNewlines() {
	for p.at(Newline) {
		p.pos++
	}
}

// word reports whether the current token is the bare word text.
func (p *parser) word(text string) bool {
	return p.at(Variable) && p.tok().Text == text
}

// prevEnd is the end of the last consumed token.
func (p *parser) prevEnd() int {
	if p.pos == 0 {
		return p.toks[0].Start
	}
	return p.toks[p.pos-1].End
}

// org builds the origin of a node parsed from token index start to the last consumed token.
func (p *parser) org(start int, c Form) Origin {
	return Origin{File: p.file, Start: p.toks[start].Start, End: max(p.prevEnd(), p.toks[start].Start), Construct: c}
}

// failAt records one diagnostic over a byte range and abandons the statement.
func (p *parser) failAt(start, end int, reason, message string) {
	if p.speculating == 0 && !p.at(Invalid) {
		p.diags = append(p.diags, p.file.Record(reason, start, end, message))
	}
	panic(bailout{})
}

// fail reports the current token as unexpected.
func (p *parser) fail(message string) {
	t := p.tok()
	if t.Kind == EOF {
		message += " before the end of the file"
	} else {
		message += "; found " + diagnostics.Code(t.Kind.String())
	}
	p.failAt(t.Start, max(t.End, t.Start+1), diagnostics.UnexpectedToken, message)
}

// descend enters one level of nesting and rejects a program nested deeper than maxDepth.
func (p *parser) descend() {
	if p.depth == maxDepth {
		t := p.tok()
		p.failAt(t.Start, max(t.End, t.Start+1), diagnostics.UnsupportedSyntax, "nesting deeper than "+strconv.Itoa(maxDepth)+" levels is not supported")
	}
	p.depth++
}

// ascend leaves the level of nesting that descend entered.
func (p *parser) ascend() { p.depth-- }

// recovered runs parse and, when it bails out, resynchronises after the failed statement and reports false.
func (p *parser) recovered(start int, parse func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, is := r.(bailout); !is {
				panic(r)
			}
			p.allowAssign = false
			p.clause = false
			p.pattern = false
			p.resync(start)
			ok = false
		}
	}()
	parse()
	return true
}

// resync skips to the next newline at the bracket depth of the statement that started at token index start, or to the closing brace of its block.
func (p *parser) resync(start int) {
	depth := 0
	for i := start; i < p.pos; i++ {
		depth += bracketDelta(p.toks[i].Kind)
	}
	for {
		k := p.tok().Kind
		if k == EOF || depth == 0 && (k == Newline || k == RBrace) {
			return
		}
		if d := bracketDelta(k); d > 0 || depth > 0 {
			depth += d
		}
		p.pos++
	}
}

func bracketDelta(k Kind) int {
	switch k {
	case LParen, LBracket, LBrace:
		return 1
	case RParen, RBracket, RBrace:
		return -1
	}
	return 0
}

// speculate runs parse and reports whether it succeeded, restoring the position and recording nothing when it did not.
func (p *parser) speculate(parse func()) (ok bool) {
	start := p.pos
	p.speculating++
	defer func() {
		p.speculating--
		if r := recover(); r != nil {
			if _, is := r.(bailout); !is {
				panic(r)
			}
			p.pos = start
			ok = false
		}
	}()
	parse()
	return true
}

// tryType parses a type prefix followed by a VARIABLE or CONSTANT, or restores the position.
func (p *parser) tryType() (Type, bool) {
	start := p.pos
	var t Type
	if !p.speculate(func() { t = p.parseType() }) || !(p.at(Variable) || p.at(Constant)) {
		p.pos = start
		return nil, false
	}
	return t, true
}

// isKeyword reports whether a token is a reserved word.
func isKeyword(t Token) bool { return t.Kind >= KwAnd && t.Kind <= KwWhere }

// misnamed reports the token in the position of a variable name that is a keyword or a name of another class, stating the rule that the name breaks.
func (p *parser) misnamed(t Token) {
	switch {
	case isKeyword(t):
		p.failAt(t.Start, t.End, diagnostics.UnexpectedToken, diagnostics.Code(t.Text)+" is a keyword and cannot name a variable")
	case t.Kind == TypeParam:
		p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a variable name starts with a lowercase letter or "+diagnostics.Code("_")+"; the single uppercase letter "+diagnostics.Code(t.Text)+" is a type parameter")
	}
	p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a variable name starts with a lowercase letter or "+diagnostics.Code("_")+"; "+diagnostics.Code(t.Text)+" is a name of another class")
}

// typedName returns the token after a type prefix when an initializer follows it, which is the token in the position of a declared name, and restores the position.
func (p *parser) typedName() (Token, bool) {
	start := p.pos
	defer func() { p.pos = start }()
	if !p.speculate(func() { p.parseType() }) || p.pos == start || p.peek(1).Kind != Assign {
		return Token{}, false
	}
	return p.tok(), true
}

// misnamedLocal reports a declaration of a local whose name is a keyword or a name of a class that no local has.
func (p *parser) misnamedLocal() {
	if t, ok := p.typedName(); ok && (isKeyword(t) || isTypeClass(t.Kind)) {
		p.misnamed(t)
	}
}

// isTypeClass reports whether a token class names a type, a type parameter, a query, or an error.
func isTypeClass(k Kind) bool {
	switch k {
	case TypeParam, TypeName, EntityType, ServiceType, ProjectionType, QueryName, ErrorType:
		return true
	}
	return false
}

// misnamedLoop reports a loop variable before in that is a keyword or a name of a class that no variable has, where the tokens before in are names separated by commas.
func (p *parser) misnamedLoop() {
	var wrong *Token
	for i := 0; ; i += 2 {
		t := p.peek(i)
		switch {
		case t.Kind == Variable || t.Kind == Constant:
		case isKeyword(t) || isTypeClass(t.Kind):
			if wrong == nil {
				wrong = &t
			}
		default:
			return
		}
		switch p.peek(i + 1).Kind {
		case KwIn:
			if wrong != nil {
				p.misnamed(*wrong)
			}
			return
		case Comma:
		default:
			return
		}
	}
}

// terminate requires the end of a statement: a newline, a closing brace, or the end of the file.
func (p *parser) terminate() {
	switch {
	case p.at(Newline):
		p.skipNewlines()
	case p.at(RBrace), p.at(EOF):
	default:
		p.fail("expected a newline after the statement")
	}
}

// statements parses the statements of a block up to its closing brace.
func (p *parser) statements() []Stmt {
	var stmts []Stmt
	p.skipNewlines()
	for !p.at(RBrace) && !p.at(EOF) {
		start := p.pos
		var s Stmt
		if !p.recovered(start, func() { s = p.statement(); p.terminate() }) {
			if p.pos == start {
				p.pos++
			}
			s = &Bad{p.org(start, BadNode)}
		}
		stmts = append(stmts, s)
		p.skipNewlines()
	}
	return stmts
}

func (p *parser) block() *Block {
	start := p.pos
	p.expect(LBrace)
	stmts := p.statements()
	p.expect(RBrace)
	return &Block{Origin: p.org(start, BlockStmt), Stmts: stmts}
}

func (p *parser) statement() Stmt {
	p.descend()
	defer p.ascend()
	start := p.pos
	var s Stmt
	switch p.tok().Kind {
	case KwReturn:
		p.next()
		values := p.optionalValues()
		s = &Return{Origin: p.org(start, ReturnStmt), Values: values}
	case KwResume:
		p.next()
		values := p.optionalValues()
		s = &Resume{Origin: p.org(start, ResumeStmt), Values: values}
	case KwIf:
		s = p.ifStmt()
	case KwFor:
		s = p.forStmt(start, "")
	case KwBreak:
		p.next()
		s = &Break{Origin: p.org(start, BreakStmt), Label: p.optionalLabel()}
	case KwContinue:
		p.next()
		s = &Continue{Origin: p.org(start, ContinueStmt), Label: p.optionalLabel()}
	case KwMatch:
		s = p.matchStmt()
	case LBrace:
		s = p.block()
	case Variable:
		switch {
		case p.peek(1).Kind == Colon && p.peek(2).Kind == KwFor:
			label := p.next().Text
			p.next()
			s = p.forStmt(start, label)
		case p.peek(1).Kind == Colon:
			t := p.tok()
			p.failAt(t.Start, t.End, diagnostics.LabelNonLoop, "a label names a for loop; "+diagnostics.Code(t.Text)+" labels another statement")
		case p.word("join") && (p.peek(1).Kind == Newline || p.peek(1).Kind == RBrace || p.peek(1).Kind == EOF):
			p.next()
			s = &Join{p.org(start, JoinStmt)}
		default:
			s = p.simpleStatement()
		}
	case Constant:
		if p.peek(1).Kind == Colon {
			t := p.tok()
			p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a loop label must be a VARIABLE name; "+diagnostics.Code(t.Text)+" is a CONSTANT")
		}
		s = p.simpleStatement()
	default:
		s = p.simpleStatement()
	}
	if p.at(KwHandle) {
		s = p.handled(start, s)
	}
	return s
}

func (p *parser) optionalLabel() string {
	switch t := p.tok(); t.Kind {
	case Variable:
		return p.next().Text
	case TypeParam, Constant, TypeName, EntityType, ServiceType, ProjectionType, QueryName, ErrorType:
		p.failAt(t.Start, t.End, diagnostics.UnexpectedClass, "a loop label is a VARIABLE name; "+diagnostics.Code(t.Text)+" is a name of another class")
	}
	return ""
}

// startsExpr reports whether a token can begin an expression.
func startsExpr(t Token) bool {
	switch t.Kind {
	case Variable, TypeParam, Constant, TypeName, EntityType, ServiceType, ProjectionType, QueryName, ErrorType,
		IntLit, FloatLit, StringLit, RawStringLit, BytesLit, RawBytesLit, FStringLit, DurationLit, DateLit, TimeLit, DateTimeLit,
		KwTrue, KwFalse, KwNone, KwSelf, KwCreate, KwNot, Minus, LParen, LBracket:
		return true
	}
	return false
}

func (p *parser) optionalValues() []Expr {
	if !startsExpr(p.tok()) {
		return nil
	}
	return p.valueList()
}

// valueList parses expressions separated by commas, stopping before a comma that begins another arm.
func (p *parser) valueList() []Expr {
	values := []Expr{p.expr()}
	for p.at(Comma) && startsExpr(p.peek(1)) && !p.armFollows() {
		p.next()
		values = append(values, p.expr())
	}
	return values
}

// armFollows reports whether the tokens after the current comma begin a handler arm.
func (p *parser) armFollows() bool {
	switch p.peek(1).Kind {
	case ErrorType:
		return p.peek(2).Kind == FatArrow || p.peek(2).Kind == Variable && p.peek(3).Kind == FatArrow
	case Variable:
		return p.peek(2).Kind == ColonColon && p.peek(3).Kind == ErrorType
	}
	return false
}

func (p *parser) simpleStatement() Stmt {
	start := p.pos
	if t, ok := p.tryType(); ok {
		if p.at(Constant) && p.peek(1).Kind == Comma {
			return p.varDecl(start, t)
		}
		if p.at(Constant) {
			name := p.next()
			p.initialized(name)
			value := p.initializer()
			return &ConstDecl{Origin: p.org(start, LocalConstStmt), Type: t, Name: name.Text, Value: value}
		}
		return p.varDecl(start, t)
	}
	p.misnamedLocal()
	p.allowAssign = true
	x := p.expr()
	p.allowAssign = false
	switch k := p.tok().Kind; {
	case k == Assign:
		return p.store(start, x)
	case k == Comma && isTarget(x):
		return p.multiStore(start, x)
	case isCompoundOp(k):
		p.assignable(x)
		op := p.next().Text
		p.allowAssign = true
		value := p.expr()
		p.allowAssign = false
		if p.atAssignOp() {
			p.next()
			p.expr()
			p.failAt(p.toks[start].Start, p.prevEnd(), diagnostics.UnexpectedToken, "a compound assignment cannot be chained")
		}
		return &CompoundStore{Origin: p.org(start, CompoundStoreStmt), Target: x, Op: op, Value: value}
	}
	sent := false
	if w, ok := x.(*Wait); ok {
		_, sent = w.X.(*Send)
	}
	switch x.(type) {
	case *Call, *MethodCall, *ServiceCall, *QueryCall, *GetOrCreate, *Send:
	default:
		if sent {
			break
		}
		o := x.Where()
		p.failAt(o.Start, o.End, diagnostics.DiscardedValue, "the value of the expression is discarded; only a call can stand as a statement")
	}
	return &ExprStmt{Origin: p.org(start, ExprStmtNode), X: x}
}

func isCompoundOp(k Kind) bool {
	switch k {
	case PlusAssign, MinusAssign, StarAssign, SlashAssign, PercentAssign, CoalesceAssign:
		return true
	}
	return false
}

func (p *parser) atAssignOp() bool { return p.at(Assign) || isCompoundOp(p.tok().Kind) }

// isTarget reports whether x is a variable or self with field and index selectors.
func isTarget(x Expr) bool {
	for {
		switch r := x.(type) {
		case *FieldRead:
			x = r.X
		case *IndexRead:
			x = r.X
		case *Self:
			return true
		case *Name:
			return r.Module == "" && r.Owner == nil
		default:
			return false
		}
	}
}

// assignable requires x to be an assignment target.
func (p *parser) assignable(x Expr) {
	if !isTarget(x) {
		o := x.Where()
		p.failAt(o.Start, o.End, diagnostics.AssignmentInvalidTarget, "an assignment target must be a variable or self with field and index selectors")
	}
}

func (p *parser) store(start int, target Expr) Stmt {
	p.assignable(target)
	targets := []Expr{target}
	p.expect(Assign)
	p.allowAssign = true
	value := p.expr()
	for p.at(Assign) {
		p.assignable(value)
		targets = append(targets, value)
		p.next()
		value = p.expr()
	}
	p.allowAssign = false
	return &Store{Origin: p.org(start, StoreStmt), Targets: targets, Values: []Expr{value}, Chain: len(targets) > 1}
}

func (p *parser) multiStore(start int, first Expr) Stmt {
	p.assignable(first)
	targets := []Expr{first}
	p.allowAssign = true
	for p.got(Comma) {
		x := p.expr()
		p.assignable(x)
		targets = append(targets, x)
	}
	p.allowAssign = false
	p.expect(Assign)
	values := p.valueList()
	return &Store{Origin: p.org(start, StoreStmt), Targets: targets, Values: values}
}

func (p *parser) varDecl(start int, t Type) Stmt {
	targetStart := start
	kind := Variable
	if p.at(Constant) {
		kind = Constant
	}
	targets := []*VarTarget{p.varTarget(targetStart, t, kind)}
	for p.got(Comma) {
		targetStart = p.pos
		if mixed, ok := p.tryType(); ok {
			targets = append(targets, p.varTarget(targetStart, mixed, kind))
		} else {
			targets = append(targets, p.varTarget(targetStart, t, kind))
		}
	}
	var values []Expr
	switch {
	case len(targets) > 1:
		if !p.at(Assign) {
			p.failAt(p.toks[start].Start, p.prevEnd(), diagnostics.VariableMissingInitializer, "a declaration of several names has an initializer")
		}
		p.next()
		values = p.valueList()
	case p.got(Assign):
		values = []Expr{p.initializer()}
	}
	return &VarDecl{Origin: p.org(start, VarDeclStmt), Targets: targets, Values: values}
}

// initialized consumes the = of a constant declaration, reporting a constant without an initializer at its name.
func (p *parser) initialized(name Token) {
	if !p.at(Assign) {
		p.failAt(name.Start, name.End, diagnostics.VariableMissingInitializer, "a constant declaration has an initializer")
	}
	p.next()
}

// varTarget parses one target whose name has the kind of the first target of its declaration.
func (p *parser) varTarget(start int, t Type, kind Kind) *VarTarget {
	name := p.expect(kind).Text
	return &VarTarget{Origin: p.org(start, VarDeclStmt), Type: t, Name: name}
}

// initializer parses a leading-dot member or an expression.
func (p *parser) initializer() Expr {
	start := p.pos
	if !p.got(Dot) {
		return p.expr()
	}
	switch p.tok().Kind {
	case Constant:
		return &DotMember{Origin: p.org(start, DotMemberExpr), Name: p.next().Text}
	case Variable:
		name := p.next().Text
		if p.at(LParen) {
			args := p.args()
			return &DotMember{Origin: p.org(start, DotMemberExpr), Name: name, Call: true, Args: args}
		}
		return &DotMember{Origin: p.org(start, DotMemberExpr), Name: name}
	}
	p.fail("expected a member name after the leading dot")
	return nil
}

func (p *parser) ifStmt() Stmt {
	p.descend()
	defer p.ascend()
	start := p.pos
	p.expect(KwIf)
	cond := p.expr()
	then := p.block()
	var otherwise Stmt
	if p.got(KwElse) {
		if p.at(KwIf) {
			otherwise = p.ifStmt()
		} else {
			otherwise = p.block()
		}
	}
	return &If{Origin: p.org(start, IfStmt), Cond: cond, Then: then, Else: otherwise}
}

func (p *parser) forStmt(start int, label string) Stmt {
	p.expect(KwFor)
	if p.at(LBrace) {
		body := p.block()
		return &For{Origin: p.org(start, ForStmt), Label: label, Body: body}
	}
	if p.iteratorAhead() {
		vars := []string{p.next().Text}
		for p.got(Comma) {
			vars = append(vars, p.expect(Variable).Text)
		}
		p.expect(KwIn)
		src := p.expr()
		body := p.block()
		return &IterFor{Origin: p.org(start, IterForStmt), Label: label, Vars: vars, Source: src, Body: body}
	}
	p.misnamedLoop()
	cond := p.expr()
	body := p.block()
	return &CondFor{Origin: p.org(start, CondForStmt), Label: label, Cond: cond, Body: body}
}

// iteratorAhead reports whether loop variables followed by in begin at the current token.
func (p *parser) iteratorAhead() bool {
	i := 0
	for p.peek(i).Kind == Variable {
		i++
		if p.peek(i).Kind == KwIn {
			return true
		}
		if p.peek(i).Kind != Comma {
			return false
		}
		i++
	}
	return false
}

func (p *parser) matchStmt() Stmt {
	start := p.pos
	head := p.expect(KwMatch)
	var subject Expr
	if !p.at(LBrace) {
		subject = p.expr()
	}
	p.expect(LBrace)
	arms := p.arms(false)
	if len(arms) == 0 {
		p.failAt(head.Start, head.End, diagnostics.MatchEmpty, "a match requires one or more arms")
	}
	p.expect(RBrace)
	return &Match{Origin: p.org(start, MatchStmt), Subject: subject, Arms: arms}
}

// arms parses the arm list of a match or of a brace-form handler up to the closing brace.
func (p *parser) arms(handler bool) []*Arm {
	var arms []*Arm
	p.skipNewlines()
	for !p.at(RBrace) && !p.at(EOF) {
		arms = append(arms, p.arm(handler, arms))
		p.skipNewlines()
		if p.got(Comma) {
			p.skipNewlines()
		}
	}
	return arms
}

// arm parses one arm after the arms before it, which decide whether its head is permitted.
func (p *parser) arm(handler bool, before []*Arm) *Arm {
	start := p.pos
	head := p.tok()
	arm := &Arm{}
	if handler {
		p.handlerPattern(arm)
	} else {
		p.matchPattern(arm)
	}
	p.armOrder(handler, before, arm, head)
	arrow := p.expect(FatArrow)
	if p.at(Newline) || p.at(RBrace) || p.at(Comma) || p.at(EOF) {
		p.failAt(arrow.Start, arrow.End, diagnostics.MatchMissingBody, "the arm requires a body after =>")
	}
	switch {
	case !handler, p.at(LBrace), p.at(KwResume), p.at(KwReturn), p.at(KwBreak), p.at(KwContinue):
		bodyStart := p.pos
		if !p.recovered(bodyStart, func() { arm.Body = p.statement() }) {
			arm.Body = &Bad{p.org(bodyStart, BadNode)}
		}
	default:
		arm.Values = p.valueList()
	}
	arm.Origin = p.org(start, ArmNode)
	return arm
}

func (p *parser) matchPattern(arm *Arm) {
	switch p.tok().Kind {
	case KwCase:
		head := p.next()
		if p.at(FatArrow) {
			p.failAt(head.Start, head.End, diagnostics.MatchMissingCandidate, "a case requires one or more candidates")
		}
		if p.at(KwNone) && p.peek(1).Kind == FatArrow {
			p.next()
			arm.Kind = NoneArm
			return
		}
		if targets, ok := p.typedPattern(); ok {
			if len(targets) == 1 {
				arm.Kind, arm.Type, arm.Binding = TypedArm, targets[0].Type, targets[0].Name
			} else {
				arm.Kind, arm.Targets = ResultsArm, targets
			}
			return
		}
		arm.Kind = CaseArm
		p.pattern = true
		arm.Patterns = []Expr{p.expr()}
		for p.got(Comma) {
			arm.Patterns = append(arm.Patterns, p.expr())
		}
		p.pattern = false
	case KwAny:
		p.next()
		arm.Kind = AnyArm
	case KwAnyError:
		p.next()
		arm.Kind = AnyErrorArm
		arm.Binding = p.optionalLabel()
	default:
		p.fail("expected case, any, or any! to begin an arm")
	}
}

// typedPattern parses a comma-separated list of types each followed by a binding, up to =>, or restores the position.
func (p *parser) typedPattern() ([]*VarTarget, bool) {
	start := p.pos
	var targets []*VarTarget
	for {
		targetStart := p.pos
		var t Type
		if !p.speculate(func() { t = p.parseType() }) || !p.at(Variable) {
			break
		}
		name := p.next().Text
		targets = append(targets, &VarTarget{Origin: p.org(targetStart, ArmNode), Type: t, Name: name})
		if p.at(FatArrow) {
			return targets, true
		}
		if !p.got(Comma) {
			break
		}
	}
	p.pos = start
	return nil, false
}

func (p *parser) handlerPattern(arm *Arm) {
	if p.got(KwAny) {
		arm.Kind = AnyArm
		arm.Binding = p.optionalLabel()
		return
	}
	arm.Kind = TypedArm
	arm.Type = p.parseType()
	arm.Binding = p.optionalLabel()
}

// armOrder rejects an arm head that follows a catch-all arm.
func (p *parser) armOrder(handler bool, before []*Arm, arm *Arm, head Token) {
	for _, b := range before {
		after := b.Kind == AnyArm && (arm.Kind != AnyErrorArm || handler) || b.Kind == AnyErrorArm && arm.Kind != AnyArm
		if !after {
			continue
		}
		reason := diagnostics.MatchArmAfterAny
		if handler {
			reason = diagnostics.HandleArmAfterAny
		}
		p.failAt(head.Start, head.End, reason, "an arm cannot follow the catch-all arm")
	}
}

func (p *parser) handled(start int, s Stmt) Stmt {
	p.expect(KwHandle)
	exhaustive := p.got(KwExhaustive)
	var arms []*Arm
	if p.got(LBrace) {
		arms = p.arms(true)
		p.expect(RBrace)
	} else {
		if p.at(Newline) {
			p.fail("expected a handler arm or a brace on the line of handle")
		}
		arms = []*Arm{p.arm(true, nil)}
	}
	return &Handled{Origin: p.org(start, HandledStmt), Stmt: s, Exhaustive: exhaustive, Arms: arms}
}
