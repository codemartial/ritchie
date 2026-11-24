// This file is the typer: one type switch over every expression kind, taking a scope
// and the expected type of the position.
package check

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// TypeOf types an expression that must yield one value, under the expected type of its position, and attaches and returns its type, or nil after reporting.
func (c *checker) TypeOf(x syntax.Expr, sc *scope, r *routine, want syntax.Type) syntax.Type {
	var wants []syntax.Type
	if want != nil {
		wants = []syntax.Type{want}
	}
	types, ok := c.results(x, sc, r, wants)
	if !ok {
		return nil
	}
	if len(types) != 1 {
		c.faultAt(x, diagnostics.ExpressionArity, fmt.Sprintf("the expression yields %d values where one is required", len(types)))
		return nil
	}
	return c.attach(x, types[0])
}

// results types an expression in a position that accepts its arity, returning its result types and whether they are valid, where a position that requires a future gives the expression the context of the type that the future holds, because a value of that type stands there.
func (c *checker) results(x syntax.Expr, sc *scope, r *routine, want []syntax.Type) ([]syntax.Type, bool) {
	var one syntax.Type
	if len(want) == 1 {
		one = want[0]
	}
	if f, ok := one.(*syntax.Future); ok {
		one = f.Elem
		want = []syntax.Type{one}
	}
	switch x := x.(type) {
	case *syntax.Call:
		results, ok := c.call(x, sc, r, want)
		if ok && len(results) == 1 {
			c.attach(x, results[0])
			c.fold(x, sc, r)
		}
		return results, ok
	case *syntax.MethodCall:
		results, ok := c.methodCall(x, sc, r)
		if ok && len(results) == 1 {
			c.attach(x, results[0])
			c.fold(x, sc, r)
		}
		return results, ok
	case *syntax.ServiceCall:
		results, ok := c.serviceCall(x, sc, r)
		if ok && len(results) == 1 {
			c.attach(x, results[0])
		}
		return results, ok
	case *syntax.DotMember:
		return c.dotMember(x, sc, r, one)
	case *syntax.Send:
		return c.send(x, sc, r)
	case *syntax.Wait:
		return c.wait(x, sc, r)
	case *syntax.Bad:
		return nil, false
	}
	t := c.single(x, sc, r, one)
	if t == nil {
		return nil, false
	}
	c.attach(x, t)
	c.fold(x, sc, r)
	return []syntax.Type{t}, true
}

// fold evaluates an expression that is a constant expression by its form, recording its value and reporting its faults.
func (c *checker) fold(x syntax.Expr, sc *scope, r *routine) {
	if c.unselected > 0 {
		return
	}
	switch x.(type) {
	case *syntax.Prim, *syntax.Format, *syntax.Call, *syntax.MethodCall, *syntax.Slice, *syntax.IndexRead, *syntax.Chain, *syntax.Logical:
		if _, done := c.p.constant[x]; !done {
			c.constantOf(x, sc, r)
		}
	}
}

// single types an expression kind that yields exactly one value.
func (c *checker) single(x syntax.Expr, sc *scope, r *routine, want syntax.Type) syntax.Type {
	switch x := x.(type) {
	case *syntax.Lit:
		return c.literal(x, want)
	case *syntax.Name:
		return c.name(x, sc, r)
	case *syntax.Self:
		if r.receiver == nil {
			c.faultAt(x, diagnostics.NameSelfOutsideMethod, "self is in scope only inside a method body")
			return nil
		}
		return r.receiver
	case *syntax.Prim:
		return c.prim(x, sc, r, want)
	case *syntax.Format:
		return c.format(x, sc, r)
	case *syntax.Closure:
		return c.closure(x, sc, r, want)
	case *syntax.Construct:
		return c.construct(x, sc, r)
	case *syntax.ArrayLit:
		return c.arrayLit(x, sc, r, want)
	case *syntax.DictLit:
		return c.dictLit(x, sc, r, want)
	case *syntax.FieldRead:
		return c.fieldRead(x, sc, r)
	case *syntax.IndexRead:
		return c.indexRead(x, sc, r)
	case *syntax.Slice:
		return c.slice(x, sc, r)
	case *syntax.Create:
		return c.create(x, sc, r)
	case *syntax.Lookup:
		return c.lookup(x, sc, r)
	case *syntax.QueryCall:
		c.faultAt(x, diagnostics.UnsupportedSyntax, "a query call is not supported")
		return nil
	case *syntax.GetOrCreate:
		c.faultAt(x, diagnostics.UnsupportedSyntax, "get-or-create is not supported")
		return nil
	case *syntax.Ternary:
		return c.ternary(x, sc, r, want)
	case *syntax.Coalesce:
		return c.coalesce(x, sc, r, want)
	case *syntax.Logical:
		return c.logical(x, sc, r)
	case *syntax.Chain:
		return c.chain(x, sc, r)
	}
	panic(fmt.Sprintf("check: unexpected expression %T", x))
}

// isNoneLit reports whether an expression is the literal none.
func isNoneLit(x syntax.Expr) bool {
	lit, ok := x.(*syntax.Lit)
	return ok && lit.Kind == syntax.KwNone
}

// contextual reports whether an expression takes its type from its context before anything else: a numeric literal, a negated one, none, and the empty collections.
func contextual(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Lit:
		return x.Kind == syntax.IntLit || x.Kind == syntax.FloatLit || x.Kind == syntax.KwNone
	case *syntax.Prim:
		return x.Op == "-" && len(x.Args) == 1 && contextual(x.Args[0])
	case *syntax.ArrayLit:
		return len(x.Elems) == 0
	case *syntax.DictLit:
		return len(x.Entries) == 0
	}
	return false
}

var integerBounds = map[string][2]*big.Int{
	"Int8":   {big.NewInt(-128), big.NewInt(127)},
	"Int16":  {big.NewInt(-32768), big.NewInt(32767)},
	"Int32":  {big.NewInt(-2147483648), big.NewInt(2147483647)},
	"Int":    {new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63)), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))},
	"Byte":   {big.NewInt(0), big.NewInt(255)},
	"BitSet": {big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))},
}

// literal types a literal under the type of its position: an integer literal takes the integer type the context requires, a fractional literal Decimal where required, and none its nullable context.
func (c *checker) literal(x *syntax.Lit, want syntax.Type) syntax.Type {
	return c.literalValue(x, x, false, want)
}

// literalValue types a literal, possibly negated, checking an integer against the range of its context type.
func (c *checker) literalValue(at syntax.Expr, x *syntax.Lit, negative bool, want syntax.Type) syntax.Type {
	name := builtinName(unwrap(want))
	switch x.Kind {
	case syntax.IntLit:
		bounds, integer := integerBounds[name]
		if !integer {
			name, bounds = "Int", integerBounds["Int"]
		}
		n, _ := new(big.Int).SetString(x.Canonical(), 10)
		if negative {
			n.Neg(n)
		}
		if n.Cmp(bounds[0]) < 0 || n.Cmp(bounds[1]) > 0 {
			c.faultAt(at, diagnostics.ConstantOutOfRange, fmt.Sprintf("the constant %s is out of range for %s", n, name))
		}
		return named(name)
	case syntax.FloatLit:
		if name == "Decimal" {
			return decimalType
		}
		if f, _ := strconv.ParseFloat(x.Canonical(), 64); math.IsInf(f, 0) {
			c.faultAt(at, diagnostics.ConstantOutOfRange, fmt.Sprintf("the constant %s is out of range for Float", x.Text))
		}
		return floatType
	case syntax.StringLit, syntax.RawStringLit:
		return stringType
	case syntax.BytesLit, syntax.RawBytesLit:
		return bytesType
	case syntax.DurationLit:
		return named("Duration")
	case syntax.DateLit:
		return named("Date")
	case syntax.TimeLit:
		return named("Time")
	case syntax.DateTimeLit:
		return named("DateTime")
	case syntax.KwTrue, syntax.KwFalse:
		return boolType
	case syntax.KwNone:
		if _, ok := want.(*syntax.Nullable); ok {
			return want
		}
		return noneType
	}
	panic(fmt.Sprintf("check: unexpected literal kind %v", x.Kind))
}

// name types a reference to a local, a constant, an enum member, or a function.
func (c *checker) name(x *syntax.Name, sc *scope, r *routine) syntax.Type {
	t := c.p.Table
	if x.Owner != nil {
		return c.typeMember(x, r)
	}
	if x.Module == "" {
		if b := sc.lookup(x.Name); b != nil {
			if b.typ == nil {
				return nil
			}
			if holdsFuture(b.typ) && sc.captures(x.Name) {
				c.faultAt(x, diagnostics.FutureMisplaced, "a closure captures no future")
				return nil
			}
			return c.read(x, b, r)
		}
	}
	var e *resolve.Entry
	switch {
	case x.Module != "":
		if !t.Module(x.Module) {
			c.fault(x.Origin, x.Start, x.Start+len(x.Module), diagnostics.ModuleUnknownModule, "unknown module "+x.Module)
			return nil
		}
		e = t.Lookup(x.Module, x.Name)
		if e == nil {
			c.faultName(x, x.Start+len(x.Module), x.Name, diagnostics.ModuleUnknownMember, fmt.Sprintf("module %s declares no %s", x.Module, x.Name))
			return nil
		}
		if !resolve.Visible(e, r.module) {
			c.faultName(x, x.Start+len(x.Module), x.Name, diagnostics.ModuleUnpublished, fmt.Sprintf("module %s does not publish %s", x.Module, x.Name))
			return nil
		}
	default:
		e = t.Scope(r.module, x.Name)
	}
	switch {
	case e == nil && t.Module(x.Name):
		c.faultAt(x, diagnostics.ModuleNotValue, "a module name is not a value")
		return nil
	case e == nil || e.Decl == nil:
		c.faultAt(x, diagnostics.NameUnknownName, "unknown name "+x.Name)
		return nil
	case e.Kind == resolve.ConstantKind:
		d := e.Decl.(*syntax.Const)
		c.record(x, c.global(d, x))
		return t.Type(d.Type)
	case e.Kind == resolve.FunctionKind:
		return c.funcType(e.Decl.(*syntax.Func))
	}
	c.faultAt(x, diagnostics.NameUnknownName, fmt.Sprintf("%s is a %s and not a value", diagnostics.Code(x.Name), e.Kind))
	return nil
}

// read types the read of a local binding, checking that every path assigned it.
func (c *checker) read(x *syntax.Name, b *binding, r *routine) syntax.Type {
	b.read = true
	c.p.bindings[x] = b
	if b.constant {
		c.record(x, b.value)
		return b.typ
	}
	if !b.param && r.facts != nil && !r.facts.assigned[b] {
		c.reportAt(x, diagnostics.VariableUseBeforeAssign, diagnostics.Code(x.Name)+" cannot be read while unassigned")
	}
	return c.narrowed(x, b.typ, r)
}

// narrowed types a read of a path at the value type of its nullable type where the path is present, and at its declared type elsewhere, recording the narrowing anew at every walk, because a loop body is walked again until its facts are stable.
func (c *checker) narrowed(x syntax.Expr, t syntax.Type, r *routine) syntax.Type {
	if _, nullable := t.(*syntax.Nullable); nullable && r.facts != nil && r.facts.present[c.pathOf(x)] {
		c.p.narrowed[x] = true
		return unwrap(t)
	}
	delete(c.p.narrowed, x)
	return t
}

// whole types a path that a presence test reads at its declared nullable type, because the test reads the presence of the path and not its value.
func (c *checker) whole(x syntax.Expr, t syntax.Type) syntax.Type {
	if !c.p.narrowed[x] {
		return t
	}
	delete(c.p.narrowed, x)
	return c.attach(x, nullable(t))
}

// typeMember types a member reached through a type: an enum member or a method reference.
func (c *checker) typeMember(x *syntax.Name, r *routine) syntax.Type {
	owner := c.resolveType(x.Owner, r.module)
	if owner == nil {
		return nil
	}
	named, ok := owner.(*syntax.Named)
	if !ok {
		c.faultName(x, x.Owner.Where().End, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no member %s", diagnostics.Code(show(owner)), diagnostics.Code(x.Name)))
		return nil
	}
	e := c.p.Table.Lookup(named.Module, named.Name)
	if e != nil && e.Kind == resolve.EnumKind {
		for _, m := range e.Decl.(*syntax.Enum).Members {
			if m.Name == x.Name {
				if e.Module != r.module {
					c.faultName(x, x.Owner.Where().End, x.Name, diagnostics.ModuleUnpublished, "enum members are reachable only in the declaring module")
				}
				c.p.constant[x] = &syntax.Name{Origin: x.Origin, Owner: named, Name: x.Name}
				return named
			}
		}
	}
	m := c.p.Table.Method(owner, x.Name)
	if m == nil {
		c.faultName(x, x.Owner.Where().End, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no member %s", diagnostics.Code(show(owner)), diagnostics.Code(x.Name)))
		return nil
	}
	if m.Decl == nil {
		c.faultName(x, x.Owner.Where().End, x.Name, diagnostics.UnsupportedSyntax, "a built-in method is not a value")
		return nil
	}
	if !c.visibleMethod(x, x.Owner.Where().End, m, r) {
		return nil
	}
	return c.funcType(m.Decl.(*syntax.Func))
}

// funcType is the function type of a declared routine.
func (c *checker) funcType(f *syntax.Func) *syntax.FuncType {
	t := c.p.Table
	ft := &syntax.FuncType{}
	for _, p := range f.Params {
		ft.Params = append(ft.Params, t.Type(p.Type))
	}
	for _, res := range f.Results {
		ft.Results = append(ft.Results, t.Type(res))
	}
	return ft
}

// resolveType resolves a type written in a body to its canonical form, reporting an unknown type at the written node.
func (c *checker) resolveType(ty syntax.Type, module string) syntax.Type {
	r, diags := c.p.Table.ResolveType(ty, module)
	c.faults = append(c.faults, diags...)
	return r
}

// prim types a primitive operator from the rows of its name, matched in one step against the operand types.
func (c *checker) prim(x *syntax.Prim, sc *scope, r *routine, want syntax.Type) syntax.Type {
	if x.Op == "-" && len(x.Args) == 1 {
		if lit, ok := x.Args[0].(*syntax.Lit); ok && (lit.Kind == syntax.IntLit || lit.Kind == syntax.FloatLit) {
			n := len(c.faults)
			t := c.literalValue(x, lit, true, want)
			c.attach(lit, t)
			if len(c.faults) > n {
				c.p.constant[x] = nil
			}
			return t
		}
	}
	ctx := want
	switch x.Op {
	case "==", "!=", "<", ">", "<=", ">=", "not":
		ctx = nil
	}
	types, ok := c.operands(x.Args, sc, r, ctx)
	if !ok {
		return nil
	}
	test := (x.Op == "==" || x.Op == "!=") && len(x.Args) == 2 && (isNoneLit(x.Args[0]) || isNoneLit(x.Args[1]))
	if test {
		for i, a := range x.Args {
			types[i] = c.whole(a, types[i])
		}
	}
	row, bind, at := c.selectRow(builtin.Operator, x.Op, types)
	if row == nil {
		if at >= 0 {
			c.operatorFault(x, x.Op, types, r)
		}
		return nil
	}
	if x.Op == "==" || x.Op == "!=" {
		for i, t := range types {
			if !comparable(t) {
				c.operatorFault(x, x.Op, types, r)
				return nil
			}
			if i == 0 && !test && c.laterEquality(x, t) {
				return nil
			}
			if i == 0 && isNone(t) && isNone(types[1]) {
				return boolType
			}
		}
	}
	c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.resolved(row, bind, types)}
	return instantiate(row.Results[0], bind, want)
}

// laterEquality reports at a node, and returns true, when the equality of a type is the structural equality of structs or of nested collections, which is not available yet.
func (c *checker) laterEquality(at syntax.Node, t syntax.Type) bool {
	if !c.structural(t, false) {
		return false
	}
	c.faultAt(at, diagnostics.UnsupportedSyntax, fmt.Sprintf("equality of %s is not supported", show(t)))
	return true
}

// structural reports whether the equality of a type compares a struct or a collection inside a collection.
func (c *checker) structural(t syntax.Type, nested bool) bool {
	switch t := t.(type) {
	case *syntax.Nullable:
		return c.structural(t.Elem, nested)
	case *syntax.Array:
		return nested || c.structural(t.Elem, true)
	case *syntax.Dict:
		return nested || c.structural(t.Value, true)
	case *syntax.Named:
		e := c.p.Table.Lookup(t.Module, t.Name)
		return e != nil && e.Kind == resolve.StructKind
	}
	return false
}

// operatorFault reports that no mapping of an operator accepts the operand types, at the operator.
func (c *checker) operatorFault(x syntax.Node, op string, types []syntax.Type, r *routine) {
	var shown []string
	for _, t := range types {
		shown = append(shown, show(t))
	}
	message := fmt.Sprintf("%s does not apply to %s", diagnostics.Code(op), strings.Join(shown, " and "))
	for _, a := range operandsOf(x) {
		if _, nullable := c.p.types[a].(*syntax.Nullable); nullable {
			if why := c.absence(a, r); why != "" {
				message += ", because " + why
				break
			}
		}
	}
	start, end := c.opSpan(x, op)
	c.fault(x.Where(), start, end, diagnostics.OperatorInvalidOperands, message)
}

// absence describes why a nullable path is not present at a use: the origin that the flow facts record for its removal, or that no test, early exit, or store establishes it on every path, and nothing for an expression that is not a path.
func (c *checker) absence(x syntax.Expr, r *routine) string {
	p := c.pathOf(x)
	switch {
	case p == "":
		return ""
	case r != nil && r.facts != nil && r.facts.lost[p] != "":
		return fmt.Sprintf("%s may be absent %s", diagnostics.Code(p), r.facts.lost[p])
	}
	return fmt.Sprintf("%s is not known to hold a value on every path that reaches this use", diagnostics.Code(p))
}

// operandsOf returns the operands of an operator expression.
func operandsOf(x syntax.Node) []syntax.Expr {
	switch x := x.(type) {
	case *syntax.Prim:
		return x.Args
	case *syntax.Logical:
		return []syntax.Expr{x.Left, x.Right}
	case *syntax.Chain:
		return x.Operands
	case *syntax.CompoundStore:
		return []syntax.Expr{x.Target, x.Value}
	}
	return nil
}

// opSpan locates the operator text of an operator expression: before the operand of a prefix form, between the operands otherwise.
func (c *checker) opSpan(x syntax.Node, op string) (int, int) {
	o := x.Where()
	operands := operandsOf(x)
	from := o.Start
	if len(operands) > 1 {
		from = operands[0].Where().End
	}
	if i := strings.Index(o.File.Text[from:o.End], op); i >= 0 {
		return from + i, from + i + len(op)
	}
	return o.Start, o.End
}

// operands types the operands of an operator or built-in: every operand that has a type of its own first, then each contextual literal under the type beside it or the type of the position.
func (c *checker) operands(args []syntax.Expr, sc *scope, r *routine, ctx syntax.Type) ([]syntax.Type, bool) {
	return c.operandsUnder(args, sc, r, ctx, nil)
}

// operandsUnder types operands as operands does, where params, given the types of the operands that are typed so far, returns the type each position requires, and an operand at a position with a required type is typed under that type.
func (c *checker) operandsUnder(args []syntax.Expr, sc *scope, r *routine, ctx syntax.Type, params func([]syntax.Type) []syntax.Type) ([]syntax.Type, bool) {
	types := make([]syntax.Type, len(args))
	ok := true
	var sibling syntax.Type
	var required []syntax.Type
	if params != nil {
		required = params(types)
	}
	for i, a := range args {
		if contextual(a) {
			continue
		}
		want := ctx
		if required != nil && required[i] != nil {
			want = required[i]
		}
		if types[i] = c.TypeOf(a, sc, r, want); types[i] == nil {
			ok = false
		} else if sibling == nil {
			sibling = types[i]
		}
	}
	required = nil
	if params != nil && ok {
		required = params(types)
	}
	for i, a := range args {
		if !contextual(a) {
			continue
		}
		want := ctx
		switch {
		case required != nil && required[i] != nil:
			want = required[i]
		case sibling != nil:
			want = sibling
		}
		if types[i] = c.TypeOf(a, sc, r, want); types[i] == nil {
			ok = false
		} else if sibling == nil && !isNone(types[i]) {
			sibling = types[i]
		}
	}
	return types, ok
}

// format types a formatted string, requiring each slot to have a default display.
func (c *checker) format(x *syntax.Format, sc *scope, r *routine) syntax.Type {
	ok := true
	var slots []syntax.Type
	for _, part := range x.Parts {
		if part.Slot == nil {
			continue
		}
		t := c.TypeOf(part.Slot, sc, r, nil)
		if t == nil {
			ok = false
			continue
		}
		if !displayable(t) {
			c.faultAt(part.Slot, diagnostics.InterpolationUnsupportedType, fmt.Sprintf("a value of type %s has no display form", show(t)))
			ok = false
		}
		slots = append(slots, t)
	}
	if !ok {
		return nil
	}
	c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.resolved(builtin.Lookup(builtin.Function, "format")[0], nil, slots)}
	return stringType
}

// closure types a function literal: its parameter types come from a function-typed position or are written, and its body is a routine of its own.
func (c *checker) closure(x *syntax.Closure, sc *scope, r *routine, want syntax.Type) syntax.Type {
	pin, _ := want.(*syntax.FuncType)
	if pin != nil && len(pin.Params) != len(x.Params) {
		pin = nil
	}
	ft := &syntax.FuncType{}
	inner := &routine{closure: x, module: r.module, receiver: r.receiver, node: Node{Closure: x}}
	body := newScope(sc)
	body.outer = true
	ok := true
	for i, p := range x.Params {
		var t syntax.Type
		switch {
		case p.Type != nil:
			t = c.resolveType(p.Type, r.module)
		case pin != nil:
			t = pin.Params[i]
		default:
			c.faultAt(p, diagnostics.ParameterMissingType, "a closure parameter writes its type unless the closure is the value of a function-typed position")
		}
		if t == nil {
			ok = false
			continue
		}
		if body.declared(p.Name) {
			c.faultName(p, p.Start, p.Name, diagnostics.NameDuplicateName, fmt.Sprintf("parameter %s is declared more than once", p.Name))
		}
		body.names[p.Name] = &binding{name: p.Name, typ: t, decl: p, param: true}
		c.p.locals[p] = t
		ft.Params = append(ft.Params, t)
	}
	if !ok {
		return nil
	}
	c.nodes = append(c.nodes, inner.node)
	if pin != nil {
		inner.results = pin.Results
		inner.none = len(pin.Results) == 0
		ft.Results = pin.Results
	}
	if x.Expr != nil {
		var want syntax.Type
		if len(inner.results) == 1 {
			want = inner.results[0]
		}
		facts := newFlow()
		if r.facts != nil {
			facts = r.facts.copy()
			facts.enclosed(where(x))
		}
		inner.facts = &facts
		t := c.TypeOf(x.Expr, body, inner, want)
		if t == nil {
			return nil
		}
		if pin == nil {
			ft.Results = []syntax.Type{t}
			inner.results = ft.Results
		} else if len(inner.results) == 0 {
			c.faultAt(x.Expr, diagnostics.ExpressionArity, "the closure returns nothing, so its body is a block")
			return nil
		} else if !Assignable(t, inner.results[0]) {
			c.mismatch(x.Expr, t, inner.results[0])
			return nil
		}
		c.p.raises[inner.node] = RaiseSet{}
		return ft
	}
	if pin == nil {
		inner.none = true
	}
	start := newFlow()
	if r.facts != nil {
		start = r.facts.copy()
		start.enclosed(where(x))
	}
	c.body(x.Body, body, inner, start)
	return ft
}

// mismatch reports a value whose type is not assignable to the type its position requires.
func (c *checker) mismatch(x syntax.Expr, got, want syntax.Type) {
	c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("a value of type %s cannot stand where %s is required", show(got), show(want)))
}

// assign types an expression under the type its position requires and checks that its value is assignable, returning the type of the value or nil.
func (c *checker) assign(x syntax.Expr, sc *scope, r *routine, want syntax.Type) syntax.Type {
	t := c.TypeOf(x, sc, r, want)
	if t == nil {
		return nil
	}
	if isNone(t) {
		c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("none cannot stand where %s is required", show(want)))
		return nil
	}
	if !Assignable(t, want) && !c.fixedConstant(x, t, want) {
		c.mismatch(x, t, want)
		return nil
	}
	return t
}

// fixedConstant reports whether a constant expression's value has the cardinality that every fixed length of a required array type states, so that its dynamic array type stands where the fixed one is required.
func (c *checker) fixedConstant(x syntax.Expr, t, want syntax.Type) bool {
	if k := c.p.collections[x]; k != nil {
		return k.v.cardinal(want) && Assignable(dynamic(t), dynamic(want))
	}
	v := c.p.constant[x]
	if lit, ok := x.(*syntax.Lit); ok && v == nil {
		v = lit
	}
	return v != nil && cardinal(v, want) && Assignable(dynamic(t), dynamic(want))
}

// cardinal reports whether an array value has the length of an array type at every level where the type fixes one.
func cardinal(v syntax.Expr, t syntax.Type) bool {
	a, ok := t.(*syntax.Array)
	if !ok {
		return true
	}
	var n int
	switch v := v.(type) {
	case *syntax.ArrayLit:
		for _, e := range v.Elems {
			if !cardinal(e, a.Elem) {
				return false
			}
		}
		n = len(v.Elems)
	case *syntax.Lit:
		if v.Kind != syntax.BytesLit && v.Kind != syntax.RawBytesLit {
			return false
		}
		n = len(v.Bytes())
	default:
		return false
	}
	m, fixed := lengthOf(a)
	return !fixed || int64(n) == m
}

// dynamic returns an array type with every fixed length dropped.
func dynamic(t syntax.Type) syntax.Type {
	if a, ok := t.(*syntax.Array); ok {
		return &syntax.Array{Origin: a.Origin, Elem: dynamic(a.Elem)}
	}
	return t
}

// arrayLit types an array literal from its context or its first element, checking every element against the element type and a fixed context against the count.
func (c *checker) arrayLit(x *syntax.ArrayLit, sc *scope, r *routine, want syntax.Type) syntax.Type {
	array, _ := unwrap(want).(*syntax.Array)
	if len(x.Elems) == 0 {
		if array == nil {
			c.faultAt(x, diagnostics.TypeContextRequired, "an empty array literal requires an array context")
			return nil
		}
		if n, fixed := lengthOf(array); fixed && n > 0 {
			c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("an empty array literal cannot stand where %s is required", show(array)))
			return nil
		}
		return array
	}
	var elem syntax.Type
	if array != nil {
		elem = array.Elem
	}
	ok := true
	for _, e := range x.Elems {
		var t syntax.Type
		if elem != nil {
			t = c.assign(e, sc, r, elem)
		} else {
			t = c.TypeOf(e, sc, r, nil)
		}
		if t == nil {
			ok = false
			continue
		}
		if elem == nil {
			elem = t
		} else if array == nil {
			if j, joined := join(elem, t); joined {
				elem = j
			} else {
				c.mismatch(e, t, elem)
				ok = false
			}
		}
	}
	if !ok {
		return nil
	}
	if array != nil {
		if n, fixed := lengthOf(array); fixed && n != int64(len(x.Elems)) {
			c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("an array literal of %d elements cannot stand where %s is required", len(x.Elems), show(array)))
			return nil
		}
		return &syntax.Array{Elem: elem, Length: array.Length}
	}
	return &syntax.Array{Elem: elem}
}

// dictLit types a dictionary literal from its context or its entries.
func (c *checker) dictLit(x *syntax.DictLit, sc *scope, r *routine, want syntax.Type) syntax.Type {
	dict, _ := unwrap(want).(*syntax.Dict)
	if len(x.Entries) == 0 {
		if dict == nil {
			c.faultAt(x, diagnostics.TypeContextRequired, "an empty dictionary literal requires a dictionary context")
			return nil
		}
		return dict
	}
	var key, value syntax.Type
	if dict != nil {
		key, value = dict.Key, dict.Value
	}
	ok := true
	for _, e := range x.Entries {
		var k, v syntax.Type
		if key != nil {
			k = c.assign(e.Key, sc, r, key)
		} else {
			k = c.TypeOf(e.Key, sc, r, nil)
		}
		if value != nil {
			v = c.assign(e.Value, sc, r, value)
		} else {
			v = c.TypeOf(e.Value, sc, r, nil)
		}
		if k == nil || v == nil {
			ok = false
			continue
		}
		if key == nil {
			key, value = k, v
		}
	}
	if !ok {
		return nil
	}
	if dict == nil && !c.p.Table.ValidKey(key) {
		c.faultAt(x, diagnostics.TypeInvalidKey, fmt.Sprintf("%s is not a dictionary key type", diagnostics.Code(show(key))))
		return nil
	}
	return &syntax.Dict{Key: key, Value: value}
}

// fieldRead types the read of a field of a struct, entity, or error value, or a built-in member.
func (c *checker) fieldRead(x *syntax.FieldRead, sc *scope, r *routine) syntax.Type {
	recv := c.TypeOf(x.X, sc, r, nil)
	if recv == nil {
		return nil
	}
	return c.narrowed(x, c.field(x, recv, r), r)
}

// field returns the type of a member of a value of a type, reporting an unknown or unreachable member at its name.
func (c *checker) field(x *syntax.FieldRead, recv syntax.Type, r *routine) syntax.Type {
	from := x.X.Where().End
	if _, ok := recv.(*syntax.Nullable); ok {
		message := fmt.Sprintf("a member of a value of type %s is reached only after its absence is resolved", show(recv))
		if why := c.absence(x.X, r); why != "" {
			message += ", and " + why
		}
		c.faultName(x, from, x.Name, diagnostics.NullableUnresolved, message)
		return nil
	}
	named, ok := recv.(*syntax.Named)
	if !ok {
		c.faultName(x, from, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no member %s", diagnostics.Code(show(recv)), diagnostics.Code(x.Name)))
		return nil
	}
	for _, row := range builtin.Members(syntax.Print(named)) {
		if row.Form == builtin.Field && row.Member == x.Name {
			return row.Results[0]
		}
	}
	if isError(named) {
		for _, row := range builtin.Members("Error!") {
			if row.Form == builtin.Field && row.Member == x.Name {
				return row.Results[0]
			}
		}
	}
	e := c.p.Table.Lookup(named.Module, named.Name)
	if e == nil || e.Decl == nil {
		c.faultName(x, from, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no member %s", diagnostics.Code(show(recv)), diagnostics.Code(x.Name)))
		return nil
	}
	for _, f := range resolve.FieldsOf(e.Decl) {
		if f.Name != x.Name {
			continue
		}
		switch {
		case e.Kind == resolve.EntityKind && (r.entry == nil || r.entry.Owner != e):
			c.faultName(x, from, x.Name, diagnostics.ModuleUnpublished, "entity fields are reachable only in the entity's own methods")
			return nil
		case e.Kind != resolve.ErrorKind && e.Module != r.module:
			c.faultName(x, from, x.Name, diagnostics.ModuleUnpublished, "fields are reachable only in the declaring module")
			return nil
		}
		return c.p.Table.Type(f.Type)
	}
	c.faultName(x, from, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no member %s", diagnostics.Code(show(recv)), diagnostics.Code(x.Name)))
	return nil
}

// pathOf returns the text of a narrowable path, a name or self followed by fields, or the empty string.
func (c *checker) pathOf(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.Name:
		if x.Module == "" && x.Owner == nil {
			return x.Name
		}
	case *syntax.Self:
		return "self"
	case *syntax.FieldRead:
		if p := c.pathOf(x.X); p != "" {
			return p + "." + x.Name
		}
	case *syntax.Wait:
		if n, ok := x.X.(*syntax.Name); ok {
			return c.pathOf(n)
		}
	}
	return ""
}

// indexRead types an element read of an array or a dictionary, and rejects a constant array index that no sequence admits, because the reference makes such an index invalid and a compile-time rejection is preferred wherever the compiler can decide it.
func (c *checker) indexRead(x *syntax.IndexRead, sc *scope, r *routine) syntax.Type {
	recv := c.TypeOf(x.X, sc, r, nil)
	if recv == nil {
		return nil
	}
	switch t := recv.(type) {
	case *syntax.Array:
		index := c.assign(x.Index, sc, r, intType)
		if index == nil || !c.admitsIndex(x.Index, t, sc, r) {
			return nil
		}
		c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.indexed("[]", t, index)}
		return t.Elem
	case *syntax.Dict:
		key := c.assign(x.Index, sc, r, t.Key)
		if key == nil {
			return nil
		}
		c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.indexed("[]", t, key)}
		return t.Value
	case *syntax.Nullable:
		message := fmt.Sprintf("a value of type %s is indexed only after its absence is resolved", show(recv))
		if why := c.absence(x.X, r); why != "" {
			message += ", and " + why
		}
		c.faultAt(x.X, diagnostics.NullableUnresolved, message)
		return nil
	}
	c.faultAt(x, diagnostics.IndexUnsupported, fmt.Sprintf("a value of type %s cannot be indexed", show(recv)))
	return nil
}

// admitsIndex reports whether an array admits a constant index and reports one it does not admit as out of range, where an index that is not constant is admitted and checked at run time.
func (c *checker) admitsIndex(index syntax.Expr, t *syntax.Array, sc *scope, r *routine) bool {
	n, ok := c.constantInt(index, sc, r)
	if !ok {
		return true
	}
	if n < 0 {
		c.faultAt(index, diagnostics.ConstantOutOfRange, fmt.Sprintf("the index %d is negative", n))
		return false
	}
	if length, fixed := lengthOf(t); fixed && n >= length {
		c.faultAt(index, diagnostics.ConstantOutOfRange, fmt.Sprintf("the index %d is outside an array of %d elements", n, length))
		return false
	}
	return true
}

// slice types a slice of an array, a string, or a byte array, and rejects a constant bound that no sequence admits, because the reference makes such a bound invalid and a compile-time rejection is preferred wherever the compiler can decide it.
func (c *checker) slice(x *syntax.Slice, sc *scope, r *routine) syntax.Type {
	recv := c.TypeOf(x.X, sc, r, nil)
	if recv == nil {
		return nil
	}
	bounds := map[syntax.Expr]int64{}
	types := []syntax.Type{recv}
	for _, b := range []syntax.Expr{x.Lo, x.Hi} {
		if b == nil {
			continue
		}
		t := c.assign(b, sc, r, intType)
		if t == nil {
			return nil
		}
		types = append(types, t)
		if n, ok := c.constantInt(b, sc, r); ok {
			if n < 0 {
				c.faultAt(b, diagnostics.ConstantOutOfRange, fmt.Sprintf("the bound %d is negative", n))
				return nil
			}
			bounds[b] = n
		}
	}
	if lo, ok := bounds[x.Lo]; ok {
		if hi, ok := bounds[x.Hi]; ok && lo > hi {
			c.faultAt(x.Lo, diagnostics.ConstantOutOfRange, fmt.Sprintf("the low bound %d is above the high bound %d", lo, hi))
			return nil
		}
	}
	switch t := recv.(type) {
	case *syntax.Array:
		if length, fixed := lengthOf(t); fixed {
			for _, b := range []syntax.Expr{x.Lo, x.Hi} {
				if n, ok := bounds[b]; ok && n > length {
					c.faultAt(b, diagnostics.ConstantOutOfRange, fmt.Sprintf("the bound %d is outside an array of %d elements", n, length))
					return nil
				}
			}
		}
		bind := map[string]syntax.Type{"T": t.Elem, "[N": t}
		c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.resolved(builtin.Lookup(builtin.Operator, "[:]")[0], bind, types)}
		return &syntax.Array{Elem: t.Elem}
	case *syntax.Named:
		if builtinName(t) == "String" {
			c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.resolved(builtin.Lookup(builtin.Operator, "[:]")[1], nil, types)}
			return stringType
		}
	}
	c.faultAt(x, diagnostics.IndexUnsupported, fmt.Sprintf("a value of type %s cannot be sliced", show(recv)))
	return nil
}

// create types an entity creation.
func (c *checker) create(x *syntax.Create, sc *scope, r *routine) syntax.Type {
	t := c.resolveType(x.Type, r.module)
	if t == nil {
		return nil
	}
	e := c.entityOf(x.Type, t, r)
	if e == nil {
		return nil
	}
	if !c.initList(x, x.Args, resolve.FieldsOf(e.Decl), e, sc, r) {
		return nil
	}
	c.edge(r, x, &callee{kind: syntax.EntityMethod, entry: e, remote: true})
	return t
}

// lookup types an entity lookup by identity or by a unique key.
func (c *checker) lookup(x *syntax.Lookup, sc *scope, r *routine) syntax.Type {
	t := c.resolveType(x.Type, r.module)
	if t == nil {
		return nil
	}
	e := c.entityOf(x.Type, t, r)
	if e == nil {
		return nil
	}
	var want syntax.Type = &syntax.Named{Module: e.Module, Name: e.Name + "#"}
	if x.Key != "" {
		want = nil
		for _, f := range resolve.FieldsOf(e.Decl) {
			if f.Name == x.Key {
				want = c.p.Table.Type(f.Type)
			}
		}
		if want == nil {
			c.faultName(x, x.Type.Where().End, x.Key, diagnostics.MemberUnknown, fmt.Sprintf("%s has no field %s", diagnostics.Code(e.Name), diagnostics.Code(x.Key)))
			return nil
		}
	}
	if c.assign(x.Value, sc, r, want) == nil {
		return nil
	}
	c.edge(r, x, &callee{kind: syntax.EntityMethod, entry: e, remote: true, lookup: true})
	return t
}

// entityOf returns the entity declaration of a resolved type, requiring the use to be in the declaring module.
func (c *checker) entityOf(written syntax.Type, t syntax.Type, r *routine) *resolve.Entry {
	named, ok := t.(*syntax.Named)
	if !ok {
		return nil
	}
	e := c.p.Table.Lookup(named.Module, named.Name)
	if e == nil || e.Kind != resolve.EntityKind || e.Identity {
		c.faultAt(written, diagnostics.TypeMismatch, fmt.Sprintf("%s is not an entity type", diagnostics.Code(show(t))))
		return nil
	}
	if e.Module != r.module {
		c.faultAt(written, diagnostics.ModuleUnpublished, "an entity is created and looked up only in its declaring module")
		return nil
	}
	return e
}

// ternary types a conditional expression, narrowing the condition's test in each arm.
func (c *checker) ternary(x *syntax.Ternary, sc *scope, r *routine, want syntax.Type) syntax.Type {
	cond := c.condition(x.Cond, sc, r)
	if cond == nil {
		return nil
	}
	saved := r.facts
	yes, no := c.narrow(x.Cond, *saved)
	r.facts = &yes
	then := c.TypeOf(x.Then, sc, r, want)
	r.facts = &no
	var other syntax.Type
	if then != nil && want == nil && !contextual(x.Then) {
		other = c.TypeOf(x.Else, sc, r, then)
	} else {
		other = c.TypeOf(x.Else, sc, r, want)
	}
	r.facts = saved
	if then == nil || other == nil {
		return nil
	}
	if want != nil {
		if !Assignable(then, want) {
			c.mismatch(x.Then, then, want)
			return nil
		}
		if !Assignable(other, want) {
			c.mismatch(x.Else, other, want)
			return nil
		}
		return want
	}
	if contextual(x.Then) && !contextual(x.Else) {
		then = c.TypeOf(x.Then, sc, r, other)
	}
	if j, ok := join(then, other); ok {
		return j
	}
	c.mismatch(x.Else, other, then)
	return nil
}

// condition types a condition: a Bool, or a nullable tested for presence.
func (c *checker) condition(x syntax.Expr, sc *scope, r *routine) syntax.Type {
	t := c.TypeOf(x, sc, r, boolType)
	if t == nil {
		return nil
	}
	if c.pathOf(x) != "" {
		t = c.whole(x, t)
	}
	if _, nullable := t.(*syntax.Nullable); nullable && c.pathOf(x) != "" {
		return t
	}
	if !Same(t, boolType) {
		c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("a condition is a Bool and not %s", show(t)))
		return nil
	}
	return t
}

// coalesce types a ?? expression: the left is nullable and the right matches its base type exactly.
func (c *checker) coalesce(x *syntax.Coalesce, sc *scope, r *routine, want syntax.Type) syntax.Type {
	var leftWant syntax.Type
	if want != nil {
		leftWant = nullable(want)
	}
	left := c.TypeOf(x.Left, sc, r, leftWant)
	if left == nil {
		return nil
	}
	n, ok := left.(*syntax.Nullable)
	if !ok {
		c.faultAt(x.Left, diagnostics.TypeMismatch, fmt.Sprintf("the left operand of ?? is a nullable and not %s", show(left)))
		return nil
	}
	if v := c.constantOf(x.Left, sc, r); v != nil && !isNoneLit(v) || c.p.collections[x.Left] != nil {
		c.unselected++
		defer func() { c.unselected-- }()
	}
	right := c.TypeOf(x.Right, sc, r, n.Elem)
	if right == nil {
		return nil
	}
	if !Same(unwrap(right), n.Elem) {
		c.faultAt(x.Right, diagnostics.TypeMismatch, fmt.Sprintf("the right operand of ?? has the base type %s and not %s", show(n.Elem), show(right)))
		return nil
	}
	return right
}

// logical types a short-circuit operator, narrowing the left test in the right operand.
func (c *checker) logical(x *syntax.Logical, sc *scope, r *routine) syntax.Type {
	left := c.operand(x.Left, sc, r)
	saved := r.facts
	if left != nil && saved != nil {
		yes, no := c.narrow(x.Left, *saved)
		if x.Op == "and" {
			r.facts = &yes
		} else {
			r.facts = &no
		}
	}
	right := c.operand(x.Right, sc, r)
	r.facts = saved
	if left == nil || right == nil {
		return nil
	}
	if !c.isTest(x.Left, left) || !c.isTest(x.Right, right) {
		c.operatorFault(x, x.Op, []syntax.Type{left, right}, r)
		return nil
	}
	return boolType
}

// operand types an operand of and or or, where a bare path is a presence test that reads the path at its declared type.
func (c *checker) operand(x syntax.Expr, sc *scope, r *routine) syntax.Type {
	t := c.TypeOf(x, sc, r, boolType)
	if t != nil && c.pathOf(x) != "" {
		return c.whole(x, t)
	}
	return t
}

// isTest reports whether an operand of and or or is a Bool or a nullable path tested for presence.
func (c *checker) isTest(x syntax.Expr, t syntax.Type) bool {
	if Same(t, boolType) {
		return true
	}
	_, nullable := t.(*syntax.Nullable)
	return nullable && c.pathOf(x) != ""
}

// chain types a chained comparison as the conjunction of its two comparisons.
func (c *checker) chain(x *syntax.Chain, sc *scope, r *routine) syntax.Type {
	types, ok := c.operands(x.Operands, sc, r, nil)
	if !ok {
		return nil
	}
	ops := make([]*Operation, len(x.Ops))
	for i, op := range x.Ops {
		pair := []syntax.Type{types[i], types[i+1]}
		row, bind, _ := c.selectRow(builtin.Operator, op, pair)
		if row == nil || !comparable(pair[0]) {
			c.operatorFault(x, op, pair, r)
			return nil
		}
		ops[i] = c.resolved(row, bind, pair)
	}
	c.p.chains[x] = ops
	return boolType
}

// narrow returns the flow facts of the true and false paths of a condition, adding the nullable paths that the test knows present.
func (c *checker) narrow(cond syntax.Expr, facts flow) (yes, no flow) {
	yes, no = facts.copy(), facts.copy()
	switch x := cond.(type) {
	case *syntax.Prim:
		if len(x.Args) == 2 && (x.Op == "==" || x.Op == "!=") {
			for i, a := range x.Args {
				if isNoneLit(x.Args[1-i]) {
					if p := c.pathOf(a); p != "" && c.isNullable(a) {
						if x.Op == "!=" {
							yes.add(p)
						} else {
							no.add(p)
						}
					}
				}
			}
		}
		if x.Op == "not" && len(x.Args) == 1 {
			no, yes = c.narrow(x.Args[0], facts)
		}
	case *syntax.Logical:
		l1, l0 := c.narrow(x.Left, facts)
		if x.Op == "and" {
			r1, r0 := c.narrow(x.Right, l1)
			return r1, meet(l0, r0)
		}
		r1, r0 := c.narrow(x.Right, l0)
		return meet(l1, r1), r0
	default:
		if p := c.pathOf(cond); p != "" && c.isNullable(cond) {
			yes.add(p)
		}
	}
	return yes, no
}

// isNullable reports whether the declared type attached to a path expression is nullable.
func (c *checker) isNullable(x syntax.Expr) bool {
	t := c.p.types[x]
	if t == nil {
		return false
	}
	_, ok := t.(*syntax.Nullable)
	return ok || c.p.narrowed[x]
}
