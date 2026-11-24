// This file is the canonical printer: one layout for source and kernel trees, with
// parentheses decided by precedence.
package syntax

import (
	"fmt"
	"slices"
	"strings"
)

type printer struct {
	b       strings.Builder
	indent  int
	rename  bool
	handler bool
	scopes  []map[string]string
	locals  int
	labels  int
}

// Print writes a tree in the canonical layout.
func Print(n Node) string {
	p := &printer{}
	n.print(p)
	return p.b.String()
}

func (p *printer) w(s string) { p.b.WriteString(s) }

// nl starts a new line at the current indentation.
func (p *printer) nl() {
	p.b.WriteByte('\n')
	for i := 0; i < p.indent; i++ {
		p.b.WriteString("    ")
	}
}

// capture returns what f prints without writing it.
func (p *printer) capture(f func()) string {
	saved := p.b
	p.b = strings.Builder{}
	f()
	out := p.b.String()
	p.b = saved
	return out
}

func (p *printer) push() { p.scopes = append(p.scopes, map[string]string{}) }
func (p *printer) pop()  { p.scopes = p.scopes[:len(p.scopes)-1] }

// reset starts the naming of locals afresh at a top-level declaration.
func (p *printer) reset() {
	p.scopes = []map[string]string{{}}
	p.locals, p.labels = 0, 0
}

// bind declares a local name in the innermost scope and returns the name to print.
func (p *printer) bind(name string) string {
	if !p.rename || name == "" {
		return name
	}
	alias := fmt.Sprintf("_l%d", p.locals)
	p.locals++
	p.scopes[len(p.scopes)-1][name] = alias
	return alias
}

func (p *printer) bindLabel(name string) string {
	if !p.rename || name == "" {
		return name
	}
	alias := fmt.Sprintf("_L%d", p.labels)
	p.labels++
	p.scopes[len(p.scopes)-1][":"+name] = alias
	return alias
}

// ref returns the name to print for a use of name.
func (p *printer) ref(name string) string {
	for _, scope := range slices.Backward(p.scopes) {
		if alias, ok := scope[name]; ok {
			return alias
		}
	}
	return name
}

func (p *printer) refLabel(name string) string {
	if !p.rename || name == "" {
		return name
	}
	return p.ref(":" + name)
}

// expr prints an expression at the top level, where no parentheses are needed.
func (p *printer) expr(x Expr) { x.print(p) }

// list prints nodes separated by sep.
func list[N Node](p *printer, ns []N, sep string) {
	for i, n := range ns {
		if i > 0 {
			p.w(sep)
		}
		n.print(p)
	}
}

// operand prints x as an operand of an operator at level parent, parenthesised when precedence requires it.
func (p *printer) operand(x Expr, parent int, right bool) {
	l := x.level()
	paren := l > parent
	switch {
	case l != parent:
	case parent == 6, parent == 7, parent == 2, parent == ternaryLevel:
		paren = true
	case parent == 5:
		paren = !right
	default:
		paren = right
	}
	if (parent == 6 || parent == 7) && (l == 6 || l == 7) {
		paren = true
	}
	if paren {
		p.w("(")
	}
	x.print(p)
	if paren {
		p.w(")")
	}
}

func (p *printer) exprs(xs []Expr) { list(p, xs, ", ") }

func (p *printer) args(args []*Arg) {
	p.w("(")
	list(p, args, ", ")
	p.w(")")
}

// initList prints a braced initializer list on one line.
func (p *printer) initList(args []*Arg) {
	if len(args) == 0 {
		p.w("{}")
		return
	}
	p.w("{ ")
	list(p, args, ", ")
	p.w(" }")
}

func (p *printer) types(ts []Type) { list(p, ts, ", ") }

func (p *printer) params(params []*Param) {
	p.w("(")
	list(p, params, ", ")
	p.w(")")
}

// block prints a statement block over several lines.
func (p *printer) block(b *Block) {
	if b == nil || len(b.Stmts) == 0 {
		p.w("{}")
		return
	}
	p.w("{")
	p.indent++
	p.push()
	for _, s := range b.Stmts {
		p.nl()
		s.print(p)
	}
	p.pop()
	p.indent--
	p.nl()
	p.w("}")
}

// armBody prints an arm body braced, on one line when it is one simple statement.
func (p *printer) armBody(s Stmt) {
	b, ok := s.(*Block)
	if !ok {
		b = &Block{Stmts: []Stmt{s}}
	}
	if len(b.Stmts) == 1 && simple(b.Stmts[0]) {
		p.w("{ ")
		p.push()
		b.Stmts[0].print(p)
		p.pop()
		p.w(" }")
		return
	}
	p.block(b)
}

// simple reports whether a statement prints on one line, which a statement that holds a send with arms does not.
func simple(s Stmt) bool {
	switch s.(type) {
	case *Block, *If, *For, *CondFor, *IterFor, *Match, *Handled:
		return false
	}
	arms := false
	s.walk(func(n Node) bool {
		x, ok := n.(*Send)
		arms = arms || ok && len(x.Arms) > 0
		return !arms
	})
	return !arms
}

// fields prints a field list over several lines.
func (p *printer) fields(fields []*Field) {
	if len(fields) == 0 {
		p.w("{}")
		return
	}
	p.w("{")
	p.indent++
	for _, f := range fields {
		p.nl()
		f.print(p)
	}
	p.indent--
	p.nl()
	p.w("}")
}

func (p *printer) arms(arms []*Arm, handler bool) {
	saved := p.handler
	p.handler = handler
	p.w("{")
	p.indent++
	for _, a := range arms {
		p.nl()
		a.print(p)
	}
	p.indent--
	p.nl()
	p.w("}")
	p.handler = saved
}

func (n *Bad) print(p *printer) { p.w("<invalid syntax>") }

func (n *File) print(p *printer) {
	for i, d := range n.Decls {
		if i > 0 {
			p.w("\n\n")
		}
		p.reset()
		d.print(p)
	}
	p.w("\n")
}

func (n *Func) print(p *printer) {
	if n.Receiver != nil {
		n.Receiver.print(p)
		p.w(".")
	}
	p.w(n.Name)
	p.params(n.Params)
	switch {
	case n.ResultNone, len(n.Results) == 0 && len(n.Raises) > 0:
		p.w(" -> none")
	case len(n.Results) == 1:
		p.w(" -> ")
		n.Results[0].print(p)
	case len(n.Results) > 1:
		p.w(" -> (")
		p.types(n.Results)
		p.w(")")
	}
	for _, r := range n.Raises {
		p.w(" | ")
		r.print(p)
	}
	for i, b := range n.Bounds {
		if i == 0 {
			p.w(" where ")
		} else {
			p.w(" and ")
		}
		b.print(p)
	}
	if n.Body != nil {
		p.w(" ")
		p.block(n.Body)
	}
}

func (n *Param) print(p *printer) {
	if n.Type != nil {
		n.Type.print(p)
		p.w(" ")
	}
	p.w(n.Name)
	if n.Default != nil {
		p.w(" = ")
		p.expr(n.Default)
	}
}

func (n *Bound) print(p *printer) {
	p.w(n.Param + " is ")
	p.types(n.Contracts)
}

func (n *Struct) print(p *printer) {
	p.w(n.Name + " ")
	p.fields(n.Fields)
}

func (n *Field) print(p *printer) {
	if n.Embed {
		p.w("embed ")
	}
	if n.Unique {
		p.w("unique ")
	}
	n.Type.print(p)
	p.w(" " + n.Name)
	if n.Default != nil {
		p.w(" = ")
		p.expr(n.Default)
	}
}

func (n *Enum) print(p *printer) {
	p.w(n.Name + " {")
	p.indent++
	for _, m := range n.Members {
		p.nl()
		m.print(p)
	}
	p.indent--
	p.nl()
	p.w("}")
}

func (n *EnumMember) print(p *printer) {
	p.w(n.Name)
	if n.Value != nil {
		p.w(": ")
		p.expr(n.Value)
	}
}

func (n *Error) print(p *printer) {
	p.w(n.Name)
	if len(n.Fields) > 0 {
		p.w(" ")
		p.fields(n.Fields)
	}
}

func (n *Entity) print(p *printer) {
	p.w(n.Name + " ")
	p.fields(n.Fields)
}

func (n *Service) print(p *printer) { p.w(n.Name + " {}") }

func (n *Projection) print(p *printer) {
	p.w(n.Name + " ")
	if n.Select == nil {
		p.fields(n.Columns)
		return
	}
	p.w("{")
	p.indent++
	p.nl()
	p.from(n.From)
	p.nl()
	n.Select.print(p)
	p.indent--
	p.nl()
	p.w("}")
}

func (p *printer) from(sources []*Source) {
	p.w("from ")
	list(p, sources, ", ")
}

func (n *Query) print(p *printer) {
	if n.Receiver != nil {
		n.Receiver.print(p)
		p.w(".")
	}
	p.w(n.Name)
	p.params(n.Params)
	if n.Result != nil {
		p.w(" -> ")
		n.Result.print(p)
	}
	p.w(" ")
	n.Clauses.print(p)
}

func (n *QueryBlock) print(p *printer) {
	p.w("{")
	p.indent++
	p.nl()
	p.from(n.From)
	if n.Filter != nil {
		p.nl()
		p.w("where ")
		p.expr(n.Filter)
	}
	if len(n.Group) > 0 {
		p.nl()
		p.w("group by ")
		p.exprs(n.Group)
	}
	if n.Select != nil {
		p.nl()
		n.Select.print(p)
	}
	if n.Having != nil {
		p.nl()
		p.w("having ")
		p.expr(n.Having)
	}
	for i, k := range n.Sort {
		if i == 0 {
			p.nl()
			p.w("sort ")
		} else {
			p.w(", ")
		}
		k.print(p)
	}
	if n.Limit != nil {
		p.nl()
		p.w("limit ")
		p.expr(n.Limit)
	}
	p.indent--
	p.nl()
	p.w("}")
}

func (n *Source) print(p *printer) {
	n.Type.print(p)
	p.w(" " + n.Name)
}

func (n *Select) print(p *printer) {
	p.w("select {")
	p.indent++
	for _, e := range n.Entries {
		p.nl()
		e.print(p)
	}
	p.indent--
	p.nl()
	p.w("}")
}

func (n *SelectEntry) print(p *printer) {
	if n.Name != "" {
		p.w(n.Name + ": ")
	}
	p.expr(n.Value)
}

func (n *SortKey) print(p *printer) {
	p.expr(n.Path)
	if n.Order != "" {
		p.w(" " + n.Order)
	}
}

func (n *Alias) print(p *printer) {
	p.w(n.Name + " = ")
	n.Target.print(p)
}

func (n *Sum) print(p *printer) {
	p.w(n.Name + " = ")
	list(p, n.Members, " | ")
}

func (n *Distinct) print(p *printer) {
	p.w(n.Name + " := ")
	n.Repr.print(p)
}

func (n *Const) print(p *printer) {
	n.Type.print(p)
	p.w(" " + n.Name + " = ")
	p.expr(n.Value)
}

func (n *Contract) print(p *printer) {
	p.w("contract " + n.Name + " {")
	p.indent++
	for _, m := range n.Methods {
		p.nl()
		m.print(p)
	}
	p.indent--
	p.nl()
	p.w("}")
}

func (n *Conformance) print(p *printer) {
	n.Provider.print(p)
	p.w(" is ")
	n.Contract.print(p)
}

func (n *VarDecl) print(p *printer) {
	values := p.capture(func() { p.exprs(n.Values) })
	previous := ""
	for i, t := range n.Targets {
		if i > 0 {
			p.w(", ")
		}
		typeText := p.capture(func() { t.Type.print(p) })
		if typeText != previous {
			p.w(typeText + " ")
			previous = typeText
		}
		p.w(p.bind(t.Name))
	}
	if len(n.Values) > 0 {
		p.w(" = " + values)
	}
}

func (n *VarTarget) print(p *printer) {
	n.Type.print(p)
	p.w(" " + n.Name)
}

func (n *ConstDecl) print(p *printer) {
	value := p.capture(func() { p.expr(n.Value) })
	n.Type.print(p)
	p.w(" " + p.bind(n.Name) + " = " + value)
}

func (n *Store) print(p *printer) {
	if n.Chain {
		for _, t := range n.Targets {
			p.expr(t)
			p.w(" = ")
		}
	} else {
		p.exprs(n.Targets)
		p.w(" = ")
	}
	p.exprs(n.Values)
}

func (n *CompoundStore) print(p *printer) {
	p.expr(n.Target)
	p.w(" " + n.Op + " ")
	p.expr(n.Value)
}

func (n *ExprStmt) print(p *printer) { p.expr(n.X) }

func (p *printer) label(name string) {
	if name != "" {
		p.w(p.bindLabel(name) + ": ")
	}
}

func (n *For) print(p *printer) {
	p.label(n.Label)
	p.w("for ")
	p.block(n.Body)
}

func (n *CondFor) print(p *printer) {
	p.label(n.Label)
	p.w("for ")
	p.expr(n.Cond)
	p.w(" ")
	p.block(n.Body)
}

func (n *IterFor) print(p *printer) {
	p.label(n.Label)
	source := p.capture(func() { p.expr(n.Source) })
	p.push()
	p.w("for ")
	for i, v := range n.Vars {
		if i > 0 {
			p.w(", ")
		}
		p.w(p.bind(v))
	}
	p.w(" in " + source + " ")
	p.block(n.Body)
	p.pop()
}

func (n *Break) print(p *printer) {
	p.w("break")
	if n.Label != "" {
		p.w(" " + p.refLabel(n.Label))
	}
}

func (n *Continue) print(p *printer) {
	p.w("continue")
	if n.Label != "" {
		p.w(" " + p.refLabel(n.Label))
	}
}

func (n *Return) print(p *printer) {
	p.w("return")
	if len(n.Values) > 0 {
		p.w(" ")
		p.exprs(n.Values)
	}
}

func (n *Join) print(p *printer) { p.w("join") }

func (n *Resume) print(p *printer) {
	p.w("resume")
	if len(n.Values) > 0 {
		p.w(" ")
		p.exprs(n.Values)
	}
}

func (n *Block) print(p *printer) { p.block(n) }

func (n *If) print(p *printer) {
	p.w("if ")
	p.expr(n.Cond)
	p.w(" ")
	p.block(n.Then)
	switch e := n.Else.(type) {
	case *If:
		p.w(" else ")
		e.print(p)
	case *Block:
		p.w(" else ")
		p.block(e)
	}
}

func (n *Match) print(p *printer) {
	p.w("match ")
	if n.Subject != nil {
		p.expr(n.Subject)
		p.w(" ")
	}
	p.arms(n.Arms, false)
}

func (n *Handled) print(p *printer) {
	n.Stmt.print(p)
	p.w(" handle ")
	if n.Exhaustive {
		p.w("exhaustive ")
	}
	p.arms(n.Arms, true)
}

func (n *Arm) print(p *printer) {
	p.push()
	defer p.pop()
	switch n.Kind {
	case CaseArm:
		p.w("case ")
		p.exprs(n.Patterns)
	case TypedArm:
		if !p.handler {
			p.w("case ")
		}
		n.Type.print(p)
		if n.Binding != "" {
			p.w(" " + p.bind(n.Binding))
		}
	case NoneArm:
		p.w("case none")
	case ResultsArm:
		p.w("case ")
		for i, t := range n.Targets {
			if i > 0 {
				p.w(", ")
			}
			t.Type.print(p)
			p.w(" " + p.bind(t.Name))
		}
	case AnyArm:
		p.w("any")
		if n.Binding != "" {
			p.w(" " + p.bind(n.Binding))
		}
	case AnyErrorArm:
		p.w("any!")
		if n.Binding != "" {
			p.w(" " + p.bind(n.Binding))
		}
	}
	p.w(" => ")
	if n.Body != nil {
		p.armBody(n.Body)
		return
	}
	p.exprs(n.Values)
}

func (n *Lit) print(p *printer) { p.w(n.Canonical()) }

func (n *Name) print(p *printer) {
	if n.Module != "" {
		p.w(n.Module + "::")
	}
	if n.Owner != nil {
		n.Owner.print(p)
		p.w("." + n.Name)
		return
	}
	if n.Module == "" {
		p.w(p.ref(n.Name))
		return
	}
	p.w(n.Name)
}

func (n *Self) print(p *printer) { p.w("self") }

func (n *Prim) print(p *printer) {
	l := n.level()
	switch {
	case l == 0:
		p.w(n.Op + "(")
		p.exprs(n.Args)
		p.w(")")
	case len(n.Args) == 1 && n.Op == "not":
		p.w("not ")
		p.operand(n.Args[0], l, false)
	case len(n.Args) == 1:
		p.w(n.Op)
		p.operand(n.Args[0], l, false)
	default:
		p.operand(n.Args[0], l, false)
		p.w(" " + n.Op + " ")
		p.operand(n.Args[1], l, true)
	}
}

func (n *Format) print(p *printer) {
	slots := make([]string, len(n.Parts))
	raw := false
	for i, part := range n.Parts {
		if part.Slot != nil {
			slots[i] = p.capture(func() { p.expr(part.Slot) })
			raw = raw || strings.Contains(slots[i], `"`)
		}
	}
	quote := `"`
	if raw {
		quote = "`"
	}
	p.w("f" + quote)
	for i, part := range n.Parts {
		if part.Slot != nil {
			p.w("{" + slots[i] + "}")
			continue
		}
		p.w(formatText(part.Text, raw))
	}
	p.w(quote)
}

func (n *Arg) print(p *printer) {
	if n.Name != "" {
		p.w(n.Name + ": ")
	}
	p.expr(n.Value)
}

func (n *Call) print(p *printer) {
	p.operand(n.Callee, 1, false)
	p.args(n.Args)
}

func (n *MethodCall) print(p *printer) {
	p.operand(n.Receiver, 1, false)
	p.w("." + n.Name)
	p.args(n.Args)
}

func (n *Closure) print(p *printer) {
	p.push()
	defer p.pop()
	if len(n.Params) == 1 && n.Params[0].Type == nil {
		p.w(p.bind(n.Params[0].Name))
	} else {
		p.w("(")
		for i, param := range n.Params {
			if i > 0 {
				p.w(", ")
			}
			if param.Type != nil {
				param.Type.print(p)
				p.w(" ")
			}
			p.w(p.bind(param.Name))
		}
		p.w(")")
	}
	p.w(" => ")
	if n.Body != nil {
		p.block(n.Body)
		return
	}
	p.expr(n.Expr)
}

func (n *Construct) print(p *printer) {
	n.Type.print(p)
	if named, ok := n.Type.(*Named); ok && strings.HasSuffix(named.Name, "!") && len(n.Args) == 0 {
		return
	}
	p.w(" ")
	p.initList(n.Args)
}

func (n *ArrayLit) print(p *printer) {
	p.w("[")
	p.exprs(n.Elems)
	p.w("]")
}

func (n *DictLit) print(p *printer) {
	if len(n.Entries) == 0 {
		p.w("[:]")
		return
	}
	p.w("[")
	list(p, n.Entries, ", ")
	p.w("]")
}

func (n *DictEntry) print(p *printer) {
	p.expr(n.Key)
	p.w(": ")
	p.expr(n.Value)
}

func (n *FieldRead) print(p *printer) {
	p.operand(n.X, 1, false)
	p.w("." + n.Name)
}

func (n *IndexRead) print(p *printer) {
	p.operand(n.X, 1, false)
	p.w("[")
	p.expr(n.Index)
	p.w("]")
}

func (n *Slice) print(p *printer) {
	p.operand(n.X, 1, false)
	p.w("[")
	if n.Lo != nil {
		p.expr(n.Lo)
	}
	p.w(":")
	if n.Hi != nil {
		p.expr(n.Hi)
	}
	p.w("]")
}

func (n *Create) print(p *printer) {
	p.w("create ")
	n.Type.print(p)
	p.w(" ")
	p.initList(n.Args)
}

func (n *Lookup) print(p *printer) {
	n.Type.print(p)
	p.w("[")
	if n.Key != "" {
		p.w(n.Key + ": ")
	}
	p.expr(n.Value)
	p.w("]")
}

func (n *ServiceCall) print(p *printer) {
	n.Service.print(p)
	p.w("." + n.Name)
	p.args(n.Args)
}

func (n *QueryCall) print(p *printer) {
	if n.Projection != nil {
		n.Projection.print(p)
		p.w(".")
	}
	p.w(n.Name)
	p.args(n.Args)
}

func (n *Ternary) print(p *printer) {
	p.operand(n.Cond, ternaryLevel, false)
	p.w(" ? ")
	p.operand(n.Then, ternaryLevel, false)
	p.w(" : ")
	p.operand(n.Else, ternaryLevel, true)
}

func (n *Coalesce) print(p *printer) {
	p.operand(n.Left, 5, false)
	p.w(" ?? ")
	p.operand(n.Right, 5, true)
}

func (n *Logical) print(p *printer) {
	l := n.level()
	p.operand(n.Left, l, false)
	p.w(" " + n.Op + " ")
	p.operand(n.Right, l, true)
}

func (n *Chain) print(p *printer) {
	l := n.level()
	for i, x := range n.Operands {
		if i > 0 {
			p.w(" " + n.Ops[i-1] + " ")
		}
		p.operand(x, l, i > 0)
	}
}

func (n *DotMember) print(p *printer) {
	p.w("." + n.Name)
	if n.Call {
		p.args(n.Args)
	}
}

func (n *GetOrCreate) print(p *printer) {
	n.Lookup.print(p)
	p.w(" else ")
	n.Create.print(p)
	if n.Then != nil {
		p.w(" then ")
		p.block(n.Then)
	}
}

func (n *Named) print(p *printer) {
	if n.Module != "" {
		p.w(n.Module + "::")
	}
	p.w(n.Name)
	if len(n.Args) > 0 {
		p.w("<")
		p.types(n.Args)
		p.w(">")
	}
}

func (n *Array) print(p *printer) {
	n.Elem.print(p)
	p.w("[")
	if n.Length != nil {
		p.expr(n.Length)
	}
	p.w("]")
}

func (n *Dict) print(p *printer) {
	p.w("[")
	n.Key.print(p)
	p.w(": ")
	n.Value.print(p)
	p.w("]")
}

func (n *Nullable) print(p *printer) {
	n.Elem.print(p)
	p.w("?")
}

func (n *FuncType) print(p *printer) {
	p.w("(")
	p.types(n.Params)
	p.w(")")
	switch len(n.Results) {
	case 0:
	case 1:
		p.w(" -> ")
		n.Results[0].print(p)
	default:
		p.w(" -> (")
		p.types(n.Results)
		p.w(")")
	}
}

func (n *Future) print(p *printer) {
	p.w("future<")
	n.Elem.print(p)
	p.w(">")
}

func (n *Send) print(p *printer) {
	p.w("send ")
	p.expr(n.X)
	if len(n.Arms) == 0 {
		return
	}
	p.w(" {")
	p.indent++
	for _, a := range n.Arms {
		p.nl()
		a.print(p)
	}
	p.indent--
	p.nl()
	p.w("}")
}

func (n *SendArm) print(p *printer) {
	if n.Type == nil {
		p.w("any! => ")
	} else {
		p.w("case ")
		n.Type.print(p)
		p.w(" => ")
	}
	switch {
	case n.Return && len(n.Values) == 0:
		p.w("return")
	case n.Return:
		p.w("return ")
		p.exprs(n.Values)
	case len(n.Values) == 0:
		p.w("{}")
	default:
		p.exprs(n.Values)
	}
}

func (n *Wait) print(p *printer) {
	p.w("wait ")
	if _, send := n.X.(*Send); send {
		p.expr(n.X)
		return
	}
	p.operand(n.X, 1, false)
}

// level is the precedence level of an expression as an operand, where 0 is a primary.
func (*Bad) level() int         { return 0 }
func (*Lit) level() int         { return 0 }
func (*Name) level() int        { return 0 }
func (*Self) level() int        { return 0 }
func (*Format) level() int      { return 0 }
func (*Call) level() int        { return 0 }
func (*MethodCall) level() int  { return 0 }
func (*Closure) level() int     { return ternaryLevel }
func (*Construct) level() int   { return 0 }
func (*ArrayLit) level() int    { return 0 }
func (*DictLit) level() int     { return 0 }
func (*FieldRead) level() int   { return 0 }
func (*IndexRead) level() int   { return 0 }
func (*Slice) level() int       { return 0 }
func (*Create) level() int      { return 0 }
func (*Lookup) level() int      { return 0 }
func (*ServiceCall) level() int { return 0 }
func (*QueryCall) level() int   { return 0 }
func (*Ternary) level() int     { return ternaryLevel }
func (*Coalesce) level() int    { return 5 }
func (*DotMember) level() int   { return 0 }
func (*GetOrCreate) level() int { return ternaryLevel }
func (*Send) level() int        { return 2 }
func (*Wait) level() int        { return 2 }

func (n *Logical) level() int {
	if n.Op == "and" {
		return 9
	}
	return 10
}

func (n *Chain) level() int {
	if n.Ops[0] == "==" || n.Ops[0] == "!=" {
		return 7
	}
	return 6
}

func (n *Prim) level() int {
	switch n.Op {
	case "not":
		return 8
	case "-":
		if len(n.Args) == 1 {
			return 2
		}
		return 4
	case "+":
		return 4
	case "*", "/", "%":
		return 3
	case "<", ">", "<=", ">=":
		return 6
	case "==", "!=", "in":
		return 7
	}
	return 0
}
