// This file is the traversal of the tree, in which each node visits itself and then its
// children in source order.
package syntax

func walkExpr(f func(Node) bool, x Expr) {
	if x != nil {
		x.walk(f)
	}
}

func walkType(f func(Node) bool, t Type) {
	if t != nil {
		t.walk(f)
	}
}

// walkAll visits each node of a list.
func walkAll[N Node](f func(Node) bool, ns []N) {
	for _, n := range ns {
		n.walk(f)
	}
}

func walkBlock(f func(Node) bool, b *Block) {
	if b != nil {
		b.walk(f)
	}
}

func (n *Bad) walk(f func(Node) bool) { f(n) }

func (n *File) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Decls)
}

func (n *Func) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Receiver)
	walkAll(f, n.Params)
	walkAll(f, n.Results)
	walkAll(f, n.Raises)
	walkAll(f, n.Bounds)
	walkBlock(f, n.Body)
}

func (n *Param) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkExpr(f, n.Default)
}

func (n *Bound) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Contracts)
	}
}

func (n *Struct) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Fields)
	}
}

func (n *Field) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkExpr(f, n.Default)
}

func (n *Enum) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Members)
}

func (n *EnumMember) walk(f func(Node) bool) {
	if f(n) {
		walkExpr(f, n.Value)
	}
}

func (n *Error) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Fields)
	}
}

func (n *Entity) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Fields)
	}
}

func (n *Service) walk(f func(Node) bool) { f(n) }

func (n *Projection) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Columns)
	walkAll(f, n.From)
	if n.Select != nil {
		n.Select.walk(f)
	}
}

func (n *Query) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Receiver)
	walkAll(f, n.Params)
	walkType(f, n.Result)
	if n.Clauses != nil {
		n.Clauses.walk(f)
	}
}

func (n *QueryBlock) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.From)
	walkExpr(f, n.Filter)
	walkAll(f, n.Group)
	if n.Select != nil {
		n.Select.walk(f)
	}
	walkExpr(f, n.Having)
	walkAll(f, n.Sort)
	walkExpr(f, n.Limit)
}

func (n *Source) walk(f func(Node) bool) {
	if f(n) {
		walkType(f, n.Type)
	}
}

func (n *Select) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Entries)
}

func (n *SelectEntry) walk(f func(Node) bool) {
	if f(n) {
		walkExpr(f, n.Value)
	}
}

func (n *SortKey) walk(f func(Node) bool) {
	if f(n) {
		walkExpr(f, n.Path)
	}
}

func (n *Alias) walk(f func(Node) bool) {
	if f(n) {
		walkType(f, n.Target)
	}
}

func (n *Sum) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Members)
	}
}

func (n *Distinct) walk(f func(Node) bool) {
	if f(n) {
		walkType(f, n.Repr)
	}
}

func (n *Const) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkExpr(f, n.Value)
}

func (n *Contract) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Methods)
}

func (n *Conformance) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Provider)
	walkType(f, n.Contract)
}

func (n *VarDecl) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Targets)
	walkAll(f, n.Values)
}

func (n *VarTarget) walk(f func(Node) bool) {
	if f(n) {
		walkType(f, n.Type)
	}
}

func (n *ConstDecl) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkExpr(f, n.Value)
}

func (n *Store) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Targets)
	walkAll(f, n.Values)
}

func (n *CompoundStore) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Target.walk(f)
	n.Value.walk(f)
}

func (n *ExprStmt) walk(f func(Node) bool) {
	if f(n) {
		n.X.walk(f)
	}
}

func (n *For) walk(f func(Node) bool) {
	if f(n) {
		walkBlock(f, n.Body)
	}
}

func (n *CondFor) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Cond.walk(f)
	walkBlock(f, n.Body)
}

func (n *IterFor) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Source.walk(f)
	walkBlock(f, n.Body)
}

func (n *Break) walk(f func(Node) bool)    { f(n) }
func (n *Join) walk(f func(Node) bool)     { f(n) }
func (n *Continue) walk(f func(Node) bool) { f(n) }

func (n *Return) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Values)
	}
}

func (n *Resume) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Values)
	}
}

func (n *Block) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Stmts)
}

func (n *If) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Cond.walk(f)
	walkBlock(f, n.Then)
	if n.Else != nil {
		n.Else.walk(f)
	}
}

func (n *Match) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkExpr(f, n.Subject)
	walkAll(f, n.Arms)
}

func (n *Handled) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Stmt.walk(f)
	walkAll(f, n.Arms)
}

func (n *Arm) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Patterns)
	walkType(f, n.Type)
	walkAll(f, n.Targets)
	if n.Body != nil {
		n.Body.walk(f)
	}
	walkAll(f, n.Values)
}

func (n *Lit) walk(f func(Node) bool)  { f(n) }
func (n *Self) walk(f func(Node) bool) { f(n) }

func (n *Name) walk(f func(Node) bool) {
	if f(n) {
		walkType(f, n.Owner)
	}
}

func (n *Prim) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Args)
	}
}

func (n *Format) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	for _, part := range n.Parts {
		walkExpr(f, part.Slot)
	}
}

func (n *Arg) walk(f func(Node) bool) {
	if f(n) {
		n.Value.walk(f)
	}
}

func (n *Call) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Callee.walk(f)
	walkAll(f, n.Args)
}

func (n *MethodCall) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Receiver.walk(f)
	walkAll(f, n.Args)
}

func (n *Closure) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Params)
	walkBlock(f, n.Body)
	walkExpr(f, n.Expr)
}

func (n *Construct) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkAll(f, n.Args)
}

func (n *ArrayLit) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Elems)
	}
}

func (n *DictLit) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Entries)
}

func (n *DictEntry) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Key.walk(f)
	n.Value.walk(f)
}

func (n *FieldRead) walk(f func(Node) bool) {
	if f(n) {
		n.X.walk(f)
	}
}

func (n *IndexRead) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.X.walk(f)
	n.Index.walk(f)
}

func (n *Slice) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.X.walk(f)
	walkExpr(f, n.Lo)
	walkExpr(f, n.Hi)
}

func (n *Create) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkAll(f, n.Args)
}

func (n *Lookup) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	n.Value.walk(f)
}

func (n *ServiceCall) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Service)
	walkAll(f, n.Args)
}

func (n *QueryCall) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Projection)
	walkAll(f, n.Args)
}

func (n *Ternary) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Cond.walk(f)
	n.Then.walk(f)
	n.Else.walk(f)
}

func (n *Coalesce) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Left.walk(f)
	n.Right.walk(f)
}

func (n *Logical) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Left.walk(f)
	n.Right.walk(f)
}

func (n *Chain) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Operands)
	}
}

func (n *DotMember) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Args)
	}
}

func (n *GetOrCreate) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Lookup.walk(f)
	n.Create.walk(f)
	walkBlock(f, n.Then)
}

func (n *Named) walk(f func(Node) bool) {
	if f(n) {
		walkAll(f, n.Args)
	}
}

func (n *Array) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Elem.walk(f)
	walkExpr(f, n.Length)
}

func (n *Dict) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.Key.walk(f)
	n.Value.walk(f)
}

func (n *Nullable) walk(f func(Node) bool) {
	if f(n) {
		n.Elem.walk(f)
	}
}

func (n *FuncType) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkAll(f, n.Params)
	walkAll(f, n.Results)
}

func (n *Future) walk(f func(Node) bool) {
	if f(n) {
		n.Elem.walk(f)
	}
}

func (n *Send) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	n.X.walk(f)
	walkAll(f, n.Arms)
}

func (n *SendArm) walk(f func(Node) bool) {
	if !f(n) {
		return
	}
	walkType(f, n.Type)
	walkAll(f, n.Values)
}

func (n *Wait) walk(f func(Node) bool) {
	if f(n) {
		n.X.walk(f)
	}
}
