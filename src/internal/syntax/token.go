// Package syntax lexes and parses Ritchie source into the one syntax tree of
// source and kernel programs, prints it canonically, and compares trees.
package syntax

// Kind is the lexical class of a token.
type Kind uint8

const (
	Invalid Kind = iota
	EOF
	Newline

	Variable
	TypeParam
	Constant
	TypeName
	EntityType
	ServiceType
	ProjectionType
	QueryName
	ErrorType

	IntLit
	FloatLit
	StringLit
	RawStringLit
	BytesLit
	RawBytesLit
	FStringLit
	DurationLit
	DateLit
	TimeLit
	DateTimeLit

	KwAnd
	KwAny
	KwAnyError
	KwBreak
	KwCase
	KwContract
	KwContinue
	KwCreate
	KwElse
	KwEmbed
	KwExhaustive
	KwFalse
	KwFor
	KwHandle
	KwIf
	KwIn
	KwIs
	KwMatch
	KwNone
	KwNot
	KwOr
	KwResume
	KwReturn
	KwSelf
	KwThen
	KwTrue
	KwUnique
	KwWhere

	Plus
	Minus
	Star
	Slash
	Percent
	EqEq
	NotEq
	Less
	Greater
	LessEq
	GreaterEq
	CoalesceOp
	Assign
	PlusAssign
	MinusAssign
	StarAssign
	SlashAssign
	PercentAssign
	CoalesceAssign
	Question
	Colon
	LParen
	RParen
	LBrace
	RBrace
	LBracket
	RBracket
	Comma
	Dot
	ColonColon
	FatArrow
	Arrow
	Define
	Hash
	Bar
)

var kindNames = [...]string{
	Invalid: "invalid token", EOF: "end of file", Newline: "newline",
	Variable: "VARIABLE", TypeParam: "TYPE_PARAM", Constant: "CONSTANT", TypeName: "TYPE",
	EntityType: "ENTITY_TYPE", ServiceType: "SERVICE_TYPE", ProjectionType: "PROJECTION_TYPE",
	QueryName: "QUERY", ErrorType: "ERROR_TYPE",
	IntLit: "integer literal", FloatLit: "float literal", StringLit: "string literal",
	RawStringLit: "raw string literal", BytesLit: "byte array literal", RawBytesLit: "raw byte array literal",
	FStringLit: "formatted string literal", DurationLit: "duration literal", DateLit: "date literal",
	TimeLit: "time literal", DateTimeLit: "datetime literal",
	KwAnd: "and", KwAny: "any", KwAnyError: "any!", KwBreak: "break", KwCase: "case", KwContract: "contract",
	KwContinue: "continue", KwCreate: "create", KwElse: "else", KwEmbed: "embed", KwExhaustive: "exhaustive",
	KwFalse: "false", KwFor: "for", KwHandle: "handle", KwIf: "if", KwIn: "in", KwIs: "is", KwMatch: "match",
	KwNone: "none", KwNot: "not", KwOr: "or", KwResume: "resume", KwReturn: "return", KwSelf: "self",
	KwThen: "then", KwTrue: "true", KwUnique: "unique", KwWhere: "where",
	Plus: "+", Minus: "-", Star: "*", Slash: "/", Percent: "%", EqEq: "==", NotEq: "!=",
	Less: "<", Greater: ">", LessEq: "<=", GreaterEq: ">=", CoalesceOp: "??", Assign: "=",
	PlusAssign: "+=", MinusAssign: "-=", StarAssign: "*=", SlashAssign: "/=", PercentAssign: "%=",
	CoalesceAssign: "??=", Question: "?", Colon: ":", LParen: "(", RParen: ")", LBrace: "{",
	RBrace: "}", LBracket: "[", RBracket: "]", Comma: ",", Dot: ".", ColonColon: "::",
	FatArrow: "=>", Arrow: "->", Define: ":=", Hash: "#", Bar: "|",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "token"
}

var keywordKinds = map[string]Kind{
	"and": KwAnd, "any": KwAny, "break": KwBreak, "case": KwCase, "contract": KwContract,
	"continue": KwContinue, "create": KwCreate, "else": KwElse, "embed": KwEmbed,
	"exhaustive": KwExhaustive, "false": KwFalse, "for": KwFor, "handle": KwHandle, "if": KwIf,
	"in": KwIn, "is": KwIs, "match": KwMatch, "none": KwNone, "not": KwNot, "or": KwOr,
	"resume": KwResume, "return": KwReturn, "self": KwSelf, "then": KwThen, "true": KwTrue,
	"unique": KwUnique, "where": KwWhere,
}

var Keywords = func() map[string]bool {
	m := make(map[string]bool, len(keywordKinds))
	for w := range keywordKinds {
		m[w] = true
	}
	return m
}()

var contextual = map[string]bool{
	"from": true, "select": true, "group": true, "by": true, "having": true,
	"sort": true, "limit": true, "asc": true, "desc": true, "count": true,
	"sum": true, "avg": true, "min": true, "max": true, "bucket": true,
}

// ClassifyBare sorts a bare word that is not a reserved word into its archetype.
func ClassifyBare(word string) Kind {
	c := word[0]
	if c >= 'a' && c <= 'z' || c == '_' {
		return Variable
	}
	if len(word) == 1 {
		return TypeParam
	}
	for i := 1; i < len(word); i++ {
		if word[i] >= 'a' && word[i] <= 'z' {
			return TypeName
		}
	}
	return Constant
}

// Token is one lexical unit with its half-open byte span.
type Token struct {
	Kind  Kind
	Start int
	End   int
	Text  string
	Parts []Part
}

// Part is one text part or one slot of a formatted string.
type Part struct {
	Text  string
	Slot  bool
	Start int
	End   int
}

// endsOperand reports whether a token can be the last token of an operand.
func (k Kind) endsOperand() bool {
	switch k {
	case Variable, TypeParam, Constant, TypeName, EntityType, ServiceType, ProjectionType, QueryName, ErrorType,
		IntLit, FloatLit, StringLit, RawStringLit, BytesLit, RawBytesLit, FStringLit, DurationLit, DateLit, TimeLit, DateTimeLit,
		KwTrue, KwFalse, KwNone, KwSelf, RParen, RBracket, RBrace:
		return true
	}
	return false
}
