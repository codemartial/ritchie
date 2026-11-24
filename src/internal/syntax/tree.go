// This file is the one syntax tree of source and kernel programs.
package syntax

import (
	"strings"

	"ritchie.dev/internal/source"
)

// Form identifies the source construct that a node records in its origin.
type Form uint8

const (
	BadNode Form = iota
	FileNode
	FuncDecl
	StructDecl
	EnumDecl
	ErrorDecl
	EntityDecl
	ServiceDecl
	ProjectionDecl
	QueryDecl
	AliasDecl
	SumDecl
	DistinctDecl
	ConstDeclNode
	ContractDecl
	ConformanceDecl
	ParamNode
	FieldNode
	EnumMemberNode
	BoundNode
	SourceNode
	SelectNode
	SelectEntryNode
	SortKeyNode
	QueryBlockNode
	VarDeclStmt
	StoreStmt
	ExprStmtNode
	ForStmt
	BreakStmt
	ContinueStmt
	ReturnStmt
	BlockStmt
	MatchStmt
	IfStmt
	CondForStmt
	IterForStmt
	CompoundStoreStmt
	HandledStmt
	ResumeStmt
	LocalConstStmt
	JoinStmt
	ArmNode
	SendArmNode
	LitExpr
	NameExpr
	SelfExpr
	PrimExpr
	FormatExpr
	CallExpr
	MethodCallExpr
	ClosureExpr
	ConstructExpr
	ArrayLitExpr
	DictLitExpr
	FieldReadExpr
	IndexReadExpr
	SliceExpr
	CreateExpr
	LookupExpr
	ServiceCallExpr
	QueryCallExpr
	TernaryExpr
	CoalesceExpr
	LogicalExpr
	ChainExpr
	DotMemberExpr
	GetOrCreateExpr
	SendExpr
	WaitExpr
	ArgNode
	NamedType
	ArrayType
	DictType
	NullableType
	FuncTypeNode
	FutureType
)

var constructNames = [...]string{
	BadNode: "invalid syntax", FileNode: "file", FuncDecl: "function declaration",
	StructDecl: "struct declaration", EnumDecl: "enum declaration", ErrorDecl: "error declaration",
	EntityDecl: "entity declaration", ServiceDecl: "service declaration",
	ProjectionDecl: "projection declaration", QueryDecl: "query declaration",
	AliasDecl: "alias declaration", SumDecl: "sum declaration", DistinctDecl: "distinct type declaration",
	ConstDeclNode: "constant declaration", ContractDecl: "contract declaration",
	ConformanceDecl: "conformance declaration", ParamNode: "parameter", FieldNode: "field",
	EnumMemberNode: "enum member", BoundNode: "contract bound", SourceNode: "query source",
	SelectNode: "select clause", SelectEntryNode: "select entry", SortKeyNode: "sort key",
	QueryBlockNode: "clause block", VarDeclStmt: "variable declaration", StoreStmt: "assignment",
	ExprStmtNode: "expression statement", ForStmt: "for loop", BreakStmt: "break",
	ContinueStmt: "continue", ReturnStmt: "return", BlockStmt: "block", MatchStmt: "match",
	IfStmt: "if statement", CondForStmt: "conditional loop", IterForStmt: "iterator loop",
	CompoundStoreStmt: "compound assignment", HandledStmt: "handled statement", ResumeStmt: "resume",
	LocalConstStmt: "local constant declaration", JoinStmt: "join", ArmNode: "arm", SendArmNode: "arm of a send", LitExpr: "literal", NameExpr: "name",
	SelfExpr: "self", PrimExpr: "operator", FormatExpr: "formatted string", CallExpr: "call",
	MethodCallExpr: "method call", ClosureExpr: "closure", ConstructExpr: "construction",
	ArrayLitExpr: "array literal", DictLitExpr: "dictionary literal", FieldReadExpr: "field access",
	IndexReadExpr: "index", SliceExpr: "slice", CreateExpr: "creation", LookupExpr: "entity lookup",
	ServiceCallExpr: "service call", QueryCallExpr: "query call", TernaryExpr: "ternary",
	CoalesceExpr: "coalescing", LogicalExpr: "logical operator", ChainExpr: "chained comparison",
	DotMemberExpr: "leading-dot member", GetOrCreateExpr: "get-or-create", SendExpr: "send", WaitExpr: "wait",
	ArgNode: "argument", NamedType: "named type", ArrayType: "array type", DictType: "dictionary type",
	NullableType: "nullable type", FuncTypeNode: "function type", FutureType: "future type",
}

func (c Form) String() string {
	if int(c) < len(constructNames) && constructNames[c] != "" {
		return constructNames[c]
	}
	return "node"
}

// Origin records where a node came from: its file, its byte span, and the source construct.
type Origin struct {
	File      *source.File
	Start     int
	End       int
	Construct Form
}

// Where returns the origin itself, so that every node reports its origin.
func (o Origin) Where() Origin { return o }

// NameSpan returns the span of the first occurrence of a name in the origin at or after an offset, or the whole origin.
func (o Origin) NameSpan(from int, name string) (start, end int) {
	if i := strings.Index(o.File.Text[from:o.End], name); i >= 0 {
		return from + i, from + i + len(name)
	}
	return o.Start, o.End
}

// Node is any node of the tree.
type Node interface {
	Where() Origin
	Kernel() bool
	print(p *printer)
	walk(f func(Node) bool)
}

// Decl is a top-level declaration.
type Decl interface {
	Node
	decl()
}

// Stmt is a statement.
type Stmt interface {
	Node
	stmt()
}

// Expr is an expression.
type Expr interface {
	Node
	expr()
	level() int
}

// Type is a written or resolved type.
type Type interface {
	Node
	typ()
}

// Inspect visits n and its descendants in source order while f returns true.
func Inspect(n Node, f func(Node) bool) { n.walk(f) }

// IsKernel reports whether every node of the tree is a kernel form.
func IsKernel(n Node) bool {
	kernel := true
	n.walk(func(n Node) bool {
		if !n.Kernel() {
			kernel = false
		}
		return kernel
	})
	return kernel
}

// Bad stands in for a declaration, statement, expression, or type that failed to parse.
type Bad struct{ Origin }

// File is the tree of one source file.
type File struct {
	Origin
	Decls []Decl
}

// Func is a free function or a method, with its receiver, parameters, results, declared raise set, and body.
type Func struct {
	Origin
	Receiver   Type
	Name       string
	Params     []*Param
	Results    []Type
	ResultNone bool
	Raises     []Type
	Bounds     []*Bound
	Body       *Block
}

// Param is one parameter of a routine or closure.
type Param struct {
	Origin
	Type    Type
	Name    string
	Default Expr
}

// Bound is one contract bound of a where clause.
type Bound struct {
	Origin
	Param     string
	Contracts []Type
}

// Struct is a struct declaration.
type Struct struct {
	Origin
	Name   string
	Fields []*Field
}

// Field is a field of a struct, entity, or error, or a column of a projection.
type Field struct {
	Origin
	Embed   bool
	Unique  bool
	Type    Type
	Name    string
	Default Expr
}

// Enum is an enum declaration.
type Enum struct {
	Origin
	Name    string
	Members []*EnumMember
}

// EnumMember is one member of an enum with its optional backing literal.
type EnumMember struct {
	Origin
	Name  string
	Value Expr
}

// Error is an error type declaration.
type Error struct {
	Origin
	Name   string
	Fields []*Field
}

// Entity is an entity type declaration.
type Entity struct {
	Origin
	Name   string
	Fields []*Field
}

// Service is a service declaration.
type Service struct {
	Origin
	Name string
}

// Projection is a projection declaration, sourced or with columns.
type Projection struct {
	Origin
	Name    string
	Columns []*Field
	From    []*Source
	Select  *Select
}

// Query is a free query or a query method on a projection.
type Query struct {
	Origin
	Receiver Type
	Name     string
	Params   []*Param
	Result   Type
	Clauses  *QueryBlock
}

// QueryBlock is the clause block of a query.
type QueryBlock struct {
	Origin
	From   []*Source
	Filter Expr
	Group  []Expr
	Select *Select
	Having Expr
	Sort   []*SortKey
	Limit  Expr
}

// Source is one source variable of a from clause.
type Source struct {
	Origin
	Type Type
	Name string
}

// Select is a select clause.
type Select struct {
	Origin
	Entries []*SelectEntry
}

// SelectEntry is one entry of a select list with its optional name.
type SelectEntry struct {
	Origin
	Name  string
	Value Expr
}

// SortKey is one key of a sort clause.
type SortKey struct {
	Origin
	Path  Expr
	Order string
}

// Alias is a type alias declaration.
type Alias struct {
	Origin
	Name   string
	Target Type
}

// Sum is a sum type declaration.
type Sum struct {
	Origin
	Name    string
	Members []Type
}

// Distinct is a distinct type declaration.
type Distinct struct {
	Origin
	Name string
	Repr Type
}

// Const is a module constant declaration.
type Const struct {
	Origin
	Type  Type
	Name  string
	Value Expr
}

// Contract is a contract declaration.
type Contract struct {
	Origin
	Name    string
	Methods []*Func
}

// Conformance is a conformance declaration.
type Conformance struct {
	Origin
	Provider Type
	Contract Type
}

// VarDecl declares one or more variables with an optional value list, or several local constants when its targets have constant names.
type VarDecl struct {
	Origin
	Targets []*VarTarget
	Values  []Expr
}

// VarTarget is one declared variable with its type.
type VarTarget struct {
	Origin
	Type Type
	Name string
}

// ConstDecl is a local constant declaration.
type ConstDecl struct {
	Origin
	Type  Type
	Name  string
	Value Expr
}

// Store assigns values to targets, and a chain a = b = c lists its targets in order with one value.
type Store struct {
	Origin
	Targets []Expr
	Values  []Expr
	Chain   bool
}

// CompoundStore is a compound assignment.
type CompoundStore struct {
	Origin
	Target Expr
	Op     string
	Value  Expr
}

// ExprStmt is an expression evaluated for its effect.
type ExprStmt struct {
	Origin
	X Expr
}

// For is the unconditional loop.
type For struct {
	Origin
	Label string
	Body  *Block
}

// CondFor is a loop with a condition.
type CondFor struct {
	Origin
	Label string
	Cond  Expr
	Body  *Block
}

// IterFor is a loop over the elements of a source.
type IterFor struct {
	Origin
	Label  string
	Vars   []string
	Source Expr
	Body   *Block
}

// Break leaves a loop.
type Break struct {
	Origin
	Label string
}

// Continue starts the next iteration of a loop.
type Continue struct {
	Origin
	Label string
}

// Return leaves a routine with its values.
type Return struct {
	Origin
	Values []Expr
}

// Resume resumes a recovered statement with replacement values.
type Resume struct {
	Origin
	Values []Expr
}

// Join waits for every read that the routine created and has not waited for, which is a statement of the kernel alone.
type Join struct{ Origin }

// Block is a brace-delimited statement list, where Region marks a block that a handler rewrite introduces for one recovered statement of the source, which a handler recovers as one statement whose results are those of its last statement and which reaches no kernel program.
type Block struct {
	Origin
	Stmts  []Stmt
	Region bool
}

// If is an if chain whose Else is nil, a Block, or an If.
type If struct {
	Origin
	Cond Expr
	Then *Block
	Else Stmt
}

// Match selects the first arm that matches its subject, or the first arm whose condition holds when it has none.
type Match struct {
	Origin
	Subject Expr
	Arms    []*Arm
}

// Handled is a statement with a handler suffix.
type Handled struct {
	Origin
	Stmt       Stmt
	Exhaustive bool
	Arms       []*Arm
}

// ArmKind is the form of an arm pattern.
type ArmKind uint8

const (
	CaseArm ArmKind = iota
	TypedArm
	NoneArm
	AnyArm
	AnyErrorArm
	ResultsArm
)

// Arm is one arm of a match or a handler, where a handler arm has either a body or a value list.
type Arm struct {
	Origin
	Kind     ArmKind
	Patterns []Expr
	Type     Type
	Binding  string
	Targets  []*VarTarget
	Body     Stmt
	Values   []Expr
}

// Lit is a literal with its source text.
type Lit struct {
	Origin
	Kind Kind
	Text string
}

// Name refers to a declaration by name, with an optional module and an optional owning type.
type Name struct {
	Origin
	Module string
	Owner  Type
	Name   string
}

// Self is the receiver of a method.
type Self struct{ Origin }

// Prim applies a primitive operator to its operands.
type Prim struct {
	Origin
	Op   string
	Args []Expr
}

// Format is a formatted string of text parts and slot expressions.
type Format struct {
	Origin
	Parts []FormatPart
}

// FormatPart is one text part or one slot of a formatted string.
type FormatPart struct {
	Text string
	Slot Expr
}

// CalleeKind is the kind of routine that a call reaches, recorded after resolution.
type CalleeKind uint8

const (
	UnresolvedCallee CalleeKind = iota
	LocalFunction
	FunctionValue
	BuiltinFunction
	TypeMethod
	StructMethod
	EntityMethod
	ServiceMethod
	QueryCallee
)

// Arg is one positional or named argument.
type Arg struct {
	Origin
	Name  string
	Value Expr
}

// Call calls a function, function value, built-in, or type-level method.
type Call struct {
	Origin
	Callee Expr
	Args   []*Arg
	Kind   CalleeKind
}

// MethodCall calls a method on a receiver value.
type MethodCall struct {
	Origin
	Receiver Expr
	Name     string
	Args     []*Arg
	Kind     CalleeKind
}

// Closure is a function literal with a block body or an expression body.
type Closure struct {
	Origin
	Params []*Param
	Body   *Block
	Expr   Expr
}

// Construct builds a struct or error value.
type Construct struct {
	Origin
	Type Type
	Args []*Arg
}

// ArrayLit is an array literal.
type ArrayLit struct {
	Origin
	Elems []Expr
}

// DictLit is a dictionary literal.
type DictLit struct {
	Origin
	Entries []*DictEntry
}

// DictEntry is one key and value of a dictionary literal.
type DictEntry struct {
	Origin
	Key   Expr
	Value Expr
}

// FieldRead reads a field of a value.
type FieldRead struct {
	Origin
	X    Expr
	Name string
}

// IndexRead reads an element by index or key.
type IndexRead struct {
	Origin
	X     Expr
	Index Expr
}

// Slice reads a range of a sequence.
type Slice struct {
	Origin
	X  Expr
	Lo Expr
	Hi Expr
}

// Create creates an entity instance.
type Create struct {
	Origin
	Type Type
	Args []*Arg
}

// Lookup finds an entity by identity or by a unique key.
type Lookup struct {
	Origin
	Type  Type
	Key   string
	Value Expr
}

// ServiceCall calls a service method.
type ServiceCall struct {
	Origin
	Service Type
	Name    string
	Args    []*Arg
}

// QueryCall calls a free query by name or a query method on a projection.
type QueryCall struct {
	Origin
	Projection Type
	Name       string
	Args       []*Arg
}

// Ternary chooses between two values by a condition.
type Ternary struct {
	Origin
	Op   int
	Cond Expr
	Then Expr
	Else Expr
}

// Coalesce supplies a value when the left operand is absent.
type Coalesce struct {
	Origin
	Op    int
	Left  Expr
	Right Expr
}

// Logical is a short-circuit and or or.
type Logical struct {
	Origin
	Op    string
	Left  Expr
	Right Expr
}

// Chain is a comparison of three operands by two operators.
type Chain struct {
	Origin
	Operands []Expr
	Ops      []string
}

// DotMember is a leading-dot member resolved against the declared type.
type DotMember struct {
	Origin
	Name string
	Call bool
	Args []*Arg
}

// GetOrCreate looks an entity up by key and creates it when absent.
type GetOrCreate struct {
	Origin
	Lookup *Lookup
	Create *Create
	Then   *Block
}

// Send hands a remote read, or a call of a reader, to the runtime and yields its futures, with the arms that resolve a failure of the read, which is an expression of the kernel alone.
type Send struct {
	Origin
	X    Expr
	Arms []*SendArm
}

// SendArm is one arm of a send: the error type that it selects, or none for every failure, and the known values that it supplies to the futures of the read, or that the routine returns when Return is set.
type SendArm struct {
	Origin
	Type   Type
	Values []Expr
	Return bool
}

// Wait waits for the read of a future, or of each element of a collection of futures, and yields the value, which is an expression of the kernel alone.
type Wait struct {
	Origin
	X Expr
}

// Named is a type given by name, with its module and type arguments.
type Named struct {
	Origin
	Module string
	Name   string
	Args   []Type
}

// Array is a dynamic array type or a fixed-length array type.
type Array struct {
	Origin
	Elem   Type
	Length Expr
}

// Dict is a dictionary type.
type Dict struct {
	Origin
	Key   Type
	Value Type
}

// Nullable is the closed sum of a type and none.
type Nullable struct {
	Origin
	Elem Type
}

// FuncType is a function type.
type FuncType struct {
	Origin
	Params  []Type
	Results []Type
}

// Future is the type of a local that can hold a read in flight whose result has the element type, which is a type of the kernel alone.
type Future struct {
	Origin
	Elem Type
}

func (*Bad) decl()         {}
func (*Func) decl()        {}
func (*Struct) decl()      {}
func (*Enum) decl()        {}
func (*Error) decl()       {}
func (*Entity) decl()      {}
func (*Service) decl()     {}
func (*Projection) decl()  {}
func (*Query) decl()       {}
func (*Alias) decl()       {}
func (*Sum) decl()         {}
func (*Distinct) decl()    {}
func (*Const) decl()       {}
func (*Contract) decl()    {}
func (*Conformance) decl() {}

func (*Bad) stmt()           {}
func (*VarDecl) stmt()       {}
func (*ConstDecl) stmt()     {}
func (*Store) stmt()         {}
func (*CompoundStore) stmt() {}
func (*ExprStmt) stmt()      {}
func (*For) stmt()           {}
func (*CondFor) stmt()       {}
func (*IterFor) stmt()       {}
func (*Break) stmt()         {}
func (*Continue) stmt()      {}
func (*Return) stmt()        {}
func (*Resume) stmt()        {}
func (*Block) stmt()         {}
func (*If) stmt()            {}
func (*Match) stmt()         {}
func (*Handled) stmt()       {}
func (*Join) stmt()          {}

func (*Bad) expr()         {}
func (*Lit) expr()         {}
func (*Name) expr()        {}
func (*Self) expr()        {}
func (*Prim) expr()        {}
func (*Format) expr()      {}
func (*Call) expr()        {}
func (*MethodCall) expr()  {}
func (*Closure) expr()     {}
func (*Construct) expr()   {}
func (*ArrayLit) expr()    {}
func (*DictLit) expr()     {}
func (*FieldRead) expr()   {}
func (*IndexRead) expr()   {}
func (*Slice) expr()       {}
func (*Create) expr()      {}
func (*Lookup) expr()      {}
func (*ServiceCall) expr() {}
func (*QueryCall) expr()   {}
func (*Ternary) expr()     {}
func (*Coalesce) expr()    {}
func (*Logical) expr()     {}
func (*Chain) expr()       {}
func (*DotMember) expr()   {}
func (*GetOrCreate) expr() {}
func (*Send) expr()        {}
func (*Wait) expr()        {}

func (*Bad) typ()      {}
func (*Named) typ()    {}
func (*Array) typ()    {}
func (*Dict) typ()     {}
func (*Nullable) typ() {}
func (*FuncType) typ() {}
func (*Future) typ()   {}

func (*Bad) Kernel() bool         { return false }
func (*File) Kernel() bool        { return true }
func (*Func) Kernel() bool        { return true }
func (n *Param) Kernel() bool     { return n.Default == nil }
func (*Bound) Kernel() bool       { return true }
func (*Struct) Kernel() bool      { return true }
func (n *Field) Kernel() bool     { return n.Default == nil }
func (*Enum) Kernel() bool        { return true }
func (*EnumMember) Kernel() bool  { return true }
func (*Error) Kernel() bool       { return true }
func (*Entity) Kernel() bool      { return true }
func (*Service) Kernel() bool     { return true }
func (*Projection) Kernel() bool  { return true }
func (*Query) Kernel() bool       { return true }
func (*QueryBlock) Kernel() bool  { return true }
func (*Source) Kernel() bool      { return true }
func (*Select) Kernel() bool      { return true }
func (*SelectEntry) Kernel() bool { return true }
func (*SortKey) Kernel() bool     { return true }
func (*Alias) Kernel() bool       { return false }
func (*Sum) Kernel() bool         { return false }
func (*Distinct) Kernel() bool    { return false }
func (n *Const) Kernel() bool     { return collectionType(n.Type) }
func (*Contract) Kernel() bool    { return false }
func (*Conformance) Kernel() bool { return false }

func (*VarDecl) Kernel() bool       { return true }
func (*VarTarget) Kernel() bool     { return true }
func (n *ConstDecl) Kernel() bool   { return collectionType(n.Type) }
func (n *Store) Kernel() bool       { return !n.Chain }
func (*CompoundStore) Kernel() bool { return false }
func (*ExprStmt) Kernel() bool      { return true }
func (*For) Kernel() bool           { return true }
func (*CondFor) Kernel() bool       { return false }
func (*IterFor) Kernel() bool       { return true }
func (*Break) Kernel() bool         { return true }
func (*Continue) Kernel() bool      { return true }
func (*Return) Kernel() bool        { return true }
func (*Resume) Kernel() bool        { return false }
func (*Block) Kernel() bool         { return true }
func (*If) Kernel() bool            { return false }
func (*Handled) Kernel() bool       { return false }
func (n *Arm) Kernel() bool         { return n.Values == nil }
func (*Join) Kernel() bool          { return true }

// collectionType reports whether a written type is an array or a dictionary, or the nullable of one, which is the declared type of a constant that the kernel keeps, because the declaration of a constant with a scalar value is removed.
func collectionType(t Type) bool {
	switch t := t.(type) {
	case *Array, *Dict:
		return true
	case *Nullable:
		return collectionType(t.Elem)
	}
	return false
}

// Kernel reports whether a match is a kernel form, where a member arm and a none arm select an outcome of a fallible subject, because the kernel has no match over a nullable value and tests presence by a comparison with none.
func (n *Match) Kernel() bool {
	switch n.Subject.(type) {
	case nil, *Prim, *Format, *IndexRead, *Slice, *Call, *MethodCall, *ServiceCall, *QueryCall, *DotMember, *Wait:
		return true
	}
	for _, a := range n.Arms {
		if a.Kind == TypedArm || a.Kind == NoneArm {
			return false
		}
	}
	return true
}

func (*Lit) Kernel() bool        { return true }
func (*Name) Kernel() bool       { return true }
func (*Self) Kernel() bool       { return true }
func (*Prim) Kernel() bool       { return true }
func (*Format) Kernel() bool     { return true }
func (*Call) Kernel() bool       { return true }
func (*MethodCall) Kernel() bool { return true }
func (n *Arg) Kernel() bool      { return n.Name == "" }
func (n *Closure) Kernel() bool {
	for _, p := range n.Params {
		if p.Type == nil {
			return false
		}
	}
	return true
}
func (*Construct) Kernel() bool   { return true }
func (*ArrayLit) Kernel() bool    { return true }
func (*DictLit) Kernel() bool     { return true }
func (*DictEntry) Kernel() bool   { return true }
func (*FieldRead) Kernel() bool   { return true }
func (*IndexRead) Kernel() bool   { return true }
func (*Slice) Kernel() bool       { return true }
func (*Create) Kernel() bool      { return true }
func (*Lookup) Kernel() bool      { return true }
func (*ServiceCall) Kernel() bool { return true }
func (*QueryCall) Kernel() bool   { return true }
func (*Ternary) Kernel() bool     { return false }
func (*Coalesce) Kernel() bool    { return false }
func (*Logical) Kernel() bool     { return true }
func (*Chain) Kernel() bool       { return false }
func (*DotMember) Kernel() bool   { return false }
func (*GetOrCreate) Kernel() bool { return false }
func (*Send) Kernel() bool        { return true }
func (*SendArm) Kernel() bool     { return true }
func (*Wait) Kernel() bool        { return true }

func (*Named) Kernel() bool    { return true }
func (*Array) Kernel() bool    { return true }
func (*Dict) Kernel() bool     { return true }
func (*Nullable) Kernel() bool { return true }
func (*FuncType) Kernel() bool { return true }
func (*Future) Kernel() bool   { return true }
