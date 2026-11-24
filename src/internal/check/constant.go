// This file is the constant evaluator, which runs after the typer over typed
// expressions: it reads the type of each literal and the resolved operation of each
// call, holds each value in its run-time form, executes a built-in operation through
// the base function of its row, and writes the literal of a value that leaves it.
package check

import (
	"cmp"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
	"ritchie.dev/rlr"
)

// value is an evaluated constant in its run-time form, where lit is the literal that wrote it, if one did, and parts are the elements of an array that a literal wrote.
type value struct {
	form  any
	lit   syntax.Expr
	parts []value
}

// member is the form of an enum member.
type member struct {
	module, enum, name string
}

// record is the form of a struct or error value: its type, the names of its fields when the construction wrote them, and its fields in declared order.
type record struct {
	typ    *syntax.Named
	names  []string
	fields []value
}

// Equal reports whether another constant is a record of the same type with equal fields.
func (r record) Equal(other any) bool {
	o, ok := other.(record)
	return ok && Same(r.typ, o.typ) && slices.EqualFunc(r.fields, o.fields, func(a, b value) bool { return builtin.EqualValues(a.form, b.form) })
}

// dict is the form of a dictionary: its entries in the order of their first insertion and the position of each key.
type dict struct {
	keys, vals []value
	index      map[any]int
}

// Equal reports whether another constant is a dictionary of the same keys with equal values.
func (d dict) Equal(other any) bool {
	o, ok := other.(dict)
	if !ok || len(d.keys) != len(o.keys) {
		return false
	}
	for id, at := range d.index {
		if i, found := o.index[id]; !found || !builtin.EqualValues(d.vals[at].form, o.vals[i].form) {
			return false
		}
	}
	return true
}

// reference is the form of a reference to a function or to a method.
type reference struct {
	name *syntax.Name
}

// tuple is the form of the results of a call with several results.
type tuple []value

// structKey is the comparable form of a struct key.
type structKey struct {
	first, rest any
}

// Constant is the value of an evaluated constant expression.
type Constant struct {
	v          value
	at         syntax.Origin
	collection bool
}

// Collection reports whether the value is a collection that a program builds once, which is an array or a dictionary that holds no reference to a function.
func (k *Constant) Collection() bool { return k.collection }

// Literal returns the literal tree of the value.
func (k *Constant) Literal() syntax.Expr { return k.v.expr(k.at) }

// constant returns the constant of an evaluated value of a type at an origin, where the literal of a scalar value is built at once and the literal of a collection only on demand, and nil for the results of a call with several results.
func (c *checker) constant(v value, t syntax.Type, at syntax.Origin) *Constant {
	k := &Constant{v: v, at: at}
	switch v.form.(type) {
	case tuple:
		return nil
	case []byte:
		k.collection = true
	case []any, dict:
		k.collection = !c.p.refers(t, map[*resolve.Entry]bool{})
	}
	if !k.collection {
		k.v.lit = v.expr(at)
	}
	return k
}

// refers reports whether a value of a type can hold a reference to a function as an element, a key, a value, or a field at some depth, because a function value is bound to the call of the routine that takes it.
func (p *Program) refers(t syntax.Type, seen map[*resolve.Entry]bool) bool {
	switch t := t.(type) {
	case *syntax.FuncType:
		return true
	case *syntax.Array:
		return p.refers(t.Elem, seen)
	case *syntax.Dict:
		return p.refers(t.Key, seen) || p.refers(t.Value, seen)
	case *syntax.Nullable:
		return p.refers(t.Elem, seen)
	case *syntax.Named:
		if e := p.structOf(t); e != nil && !seen[e] {
			seen[e] = true
			for _, f := range resolve.FieldsOf(e.Decl) {
				if p.refers(p.Table.Type(f.Type), seen) {
					return true
				}
			}
		}
	}
	return false
}

// count returns the number of elements of a constant array or of entries of a constant dictionary.
func (v value) count() int {
	switch f := v.form.(type) {
	case []any:
		return len(f)
	case []byte:
		return len(f)
	case dict:
		return len(f.keys)
	}
	return 0
}

// element returns one element of a constant array.
func (v value) element(i int) value {
	if v.parts != nil {
		return v.parts[i]
	}
	if b, ok := v.form.([]byte); ok {
		return value{form: b[i]}
	}
	return value{form: v.form.([]any)[i]}
}

// expr returns the literal tree of a constant at an origin, or nil for the results of a call with several results.
func (v value) expr(o syntax.Origin) syntax.Expr {
	if v.lit != nil {
		return v.lit
	}
	switch f := v.form.(type) {
	case nil:
		return &syntax.Lit{Origin: o, Kind: syntax.KwNone, Text: "none"}
	case member:
		return &syntax.Name{Origin: o, Owner: &syntax.Named{Origin: o, Module: f.module, Name: f.enum}, Name: f.name}
	case reference:
		return f.name
	case record:
		var args []*syntax.Arg
		for i, field := range f.fields {
			arg := &syntax.Arg{Origin: o, Value: field.expr(o)}
			if f.names != nil {
				arg.Name = f.names[i]
			}
			args = append(args, arg)
		}
		return &syntax.Construct{Origin: o, Type: f.typ, Args: args}
	case []any:
		elems := make([]syntax.Expr, len(f))
		for i := range elems {
			elems[i] = v.element(i).expr(o)
		}
		return &syntax.ArrayLit{Origin: o, Elems: elems}
	case dict:
		var entries []*syntax.DictEntry
		for i, k := range f.keys {
			entries = append(entries, &syntax.DictEntry{Origin: o, Key: k.expr(o), Value: f.vals[i].expr(o)})
		}
		return &syntax.DictLit{Origin: o, Entries: entries}
	case tuple:
		return nil
	}
	return builtin.Literal(v.form, o)
}

// cardinal reports whether an array constant has the length of an array type at every level where the type fixes one.
func (v value) cardinal(t syntax.Type) bool {
	a, ok := t.(*syntax.Array)
	if !ok {
		return true
	}
	switch v.form.(type) {
	case []any:
		if _, nested := a.Elem.(*syntax.Array); nested {
			for i := range v.count() {
				if !v.element(i).cardinal(a.Elem) {
					return false
				}
			}
		}
	case []byte:
	default:
		return false
	}
	n, fixed := lengthOf(a)
	return !fixed || int64(v.count()) == n
}

// cardinalized returns the canonical type of a constant of a declared type and value, where each dynamic length of an array type is the length that the value has there, or false when nested arrays have unequal lengths.
func cardinalized(t syntax.Type, v value) (syntax.Type, bool) {
	a, ok := t.(*syntax.Array)
	if !ok {
		return t, true
	}
	elem := a.Elem
	switch v.form.(type) {
	case []any:
		if _, nested := a.Elem.(*syntax.Array); !nested {
			break
		}
		for i := range v.count() {
			et, ok := cardinalized(a.Elem, v.element(i))
			if !ok || i > 0 && syntax.Print(et) != syntax.Print(elem) {
				return nil, false
			}
			elem = et
		}
	case []byte:
	default:
		return t, true
	}
	length := a.Length
	if length == nil {
		length = &syntax.Lit{Origin: a.Origin, Kind: syntax.IntLit, Text: strconv.Itoa(v.count())}
	}
	return &syntax.Array{Origin: a.Origin, Elem: elem, Length: length}, true
}

// array returns the constant array of evaluated elements.
func array(elems []value) value {
	forms := make([]any, len(elems))
	for i, e := range elems {
		forms[i] = e.form
	}
	return value{form: forms, parts: elems}
}

// dictionary returns the constant dictionary of evaluated keys and values, where a later entry of a key replaces the value of an earlier one.
func dictionary(keys, vals []value) value {
	d := dict{index: map[any]int{}}
	for i, k := range keys {
		id := identity(k.form)
		if at, ok := d.index[id]; ok {
			d.vals[at] = vals[i]
			continue
		}
		d.index[id] = len(d.keys)
		d.keys, d.vals = append(d.keys, k), append(d.vals, vals[i])
	}
	return value{form: d}
}

// identity returns the comparable form of a constant key, which two keys share when they are equal.
func identity(form any) any {
	if r, ok := form.(record); ok {
		var key any = r.typ.Module + "::" + r.typ.Name
		for _, f := range slices.Backward(r.fields) {
			key = structKey{identity(f.form), key}
		}
		return key
	}
	if p := builtin.ProjectKey(form); p != nil {
		return p
	}
	return form
}

// lookup returns the position of a key in a constant dictionary, or -1.
func (d dict) lookup(k value) int {
	if at, ok := d.index[identity(k.form)]; ok {
		return at
	}
	return -1
}

// whole returns a signed integer constant as an Int, or false for a constant of another form.
func whole(form any) (int64, bool) {
	switch n := form.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// sized returns an integer as the constant of a signed integer type, or false when the type is no signed integer type or does not hold the integer.
func sized(n int64, t syntax.Type) (any, bool) {
	switch builtinName(unwrap(t)) {
	case "Int8":
		return int8(n), n >= math.MinInt8 && n <= math.MaxInt8
	case "Int16":
		return int16(n), n >= math.MinInt16 && n <= math.MaxInt16
	case "Int32":
		return int32(n), n >= math.MinInt32 && n <= math.MaxInt32
	case "Int":
		return n, true
	}
	return nil, false
}

// widened returns a constant as a position of a type holds it, which changes the form of a signed integer in a position of a wider signed integer type and of no other constant.
func widened(v value, to syntax.Type) value {
	if n, ok := whole(v.form); ok {
		if s, ok := sized(n, to); ok {
			v.form = s
		}
	}
	return v
}

// evaluation is one evaluation of a constant expression that a module writes, where blame is the first node that makes the expression no constant expression.
type evaluation struct {
	c      *checker
	module string
	blame  syntax.Node
}

// evaluated is the memory of one evaluation that gave a value or reported a fault.
type evaluated struct {
	v  value
	ok bool
}

// required evaluates an expression in a position of a type that requires a constant expression, reporting the node that makes it none.
func (c *checker) required(x syntax.Expr, t syntax.Type, module string) (value, *Constant, bool) {
	e := evaluation{c: c, module: module}
	v, ok := e.eval(x)
	if !ok {
		if e.blame != nil {
			c.nonconstant(e.blame)
		}
		return v, nil, false
	}
	v = widened(v, t)
	return v, c.constant(v, t, x.Where()), true
}

// excluded returns the first node of an expression, in the order of evaluation, whose form no constant expression holds, where a method call whose receiver holds such a node is that node, or nil.
func excluded(x syntax.Expr) syntax.Node {
	var operands []syntax.Expr
	switch x := x.(type) {
	case *syntax.Ternary, *syntax.Closure, *syntax.Self, *syntax.FieldRead, *syntax.Create,
		*syntax.Lookup, *syntax.ServiceCall, *syntax.QueryCall, *syntax.GetOrCreate:
		return x
	case *syntax.Prim:
		operands = x.Args
	case *syntax.Logical:
		operands = []syntax.Expr{x.Left, x.Right}
	case *syntax.Chain:
		operands = x.Operands
	case *syntax.Coalesce:
		operands = []syntax.Expr{x.Left}
	case *syntax.Format:
		for _, part := range x.Parts {
			if part.Slot != nil {
				operands = append(operands, part.Slot)
			}
		}
	case *syntax.ArrayLit:
		operands = x.Elems
	case *syntax.DictLit:
		for _, entry := range x.Entries {
			operands = append(operands, entry.Key, entry.Value)
		}
	case *syntax.Construct:
		operands = values(x.Args)
	case *syntax.Call:
		operands = values(x.Args)
	case *syntax.DotMember:
		operands = values(x.Args)
	case *syntax.MethodCall:
		if excluded(x.Receiver) != nil {
			return x
		}
		operands = values(x.Args)
	case *syntax.IndexRead:
		operands = []syntax.Expr{x.X, x.Index}
	case *syntax.Slice:
		operands = []syntax.Expr{x.X}
		for _, bound := range []syntax.Expr{x.Lo, x.Hi} {
			if bound != nil {
				operands = append(operands, bound)
			}
		}
	}
	for _, operand := range operands {
		if n := excluded(operand); n != nil {
			return n
		}
	}
	return nil
}

// values returns the values of an argument list.
func values(args []*syntax.Arg) []syntax.Expr {
	out := make([]syntax.Expr, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// nonconstant reports a node that makes an expression no constant expression in a position that requires one.
func (c *checker) nonconstant(n syntax.Node) {
	c.faultAt(n, diagnostics.ConstantNonconstant, "a constant expression holds only literals, constants, operators, enum members, and references to functions")
}

// inconstant records a node that is no constant expression.
func (e *evaluation) inconstant(n syntax.Node) (value, bool) {
	if e.blame == nil {
		e.blame = n
	}
	return value{}, false
}

// defect records an expression that the rules of constant expressions admit and that the evaluator does not execute, which is an internal error.
func (e *evaluation) defect(x syntax.Node, why string) (value, bool) {
	if e.c.defect == nil {
		o := x.Where()
		pos := o.File.Pos(o.Start)
		e.c.defect = fmt.Errorf("internal error: %s:%d:%d: the constant evaluator does not execute %s: %s", o.File.Path, pos.Line, pos.Column, syntax.Print(x), why)
	}
	return value{}, false
}

// outOfRange reports a constant that its type does not hold.
func (e *evaluation) outOfRange(x syntax.Node, shown, typ string) (value, bool) {
	e.c.faultAt(x, diagnostics.ConstantOutOfRange, fmt.Sprintf("the constant %s is out of range for %s", shown, typ))
	return value{}, false
}

// divisionByZero reports a constant division whose divisor is zero.
func (e *evaluation) divisionByZero(divisor syntax.Node) (value, bool) {
	e.c.faultAt(divisor, diagnostics.ConstantDivisionByZero, "a constant expression divides by zero")
	return value{}, false
}

// raised reports the eligible error that a built-in operation raises as the constant diagnostic of that error.
func (e *evaluation) raised(x syntax.Node, f rlr.Fault) (value, bool) {
	c := e.c
	switch f {
	case rlr.DivisionByZero:
		c.faultAt(x, diagnostics.ConstantDivisionByZero, "a constant expression divides by zero")
	case rlr.OutOfRange:
		c.faultAt(x, diagnostics.ConstantOutOfRange, fmt.Sprintf("the constant %s is out of range", syntax.Print(x)))
	case rlr.InvalidStringBoundary:
		c.faultAt(x, diagnostics.ConstantInvalidBoundary, "a constant slice bound divides a UTF-8 encoding")
	case rlr.Malformed:
		c.faultAt(x, diagnostics.ConstantMalformed, "a constant conversion cannot parse its input")
	case rlr.MissingKey:
		c.faultAt(x, diagnostics.ConstantMissingKey, "a constant dictionary access names an absent key")
	case rlr.LengthMismatch:
		c.faultAt(x, diagnostics.ConstantLengthMismatch, "a constant zip has inputs of unequal length")
	default:
		return e.defect(x, "the operation raises "+f.Name())
	}
	return value{}, false
}

// eval evaluates a typed expression, and returns false after it reports a fault, records the node that is no constant expression, or meets an expression whose fault is reported already.
func (e *evaluation) eval(x syntax.Expr) (value, bool) {
	c := e.c
	if r, done := c.evaluated[x]; done {
		return r.v, r.ok
	}
	switch x := x.(type) {
	case *syntax.Lit:
		return e.literal(x, c.p.types[x])
	case *syntax.Name:
		return e.name(x)
	case *syntax.DotMember:
		if x.Call {
			return e.call(x, x.Args)
		}
		if n, ok := c.p.constant[x].(*syntax.Name); ok {
			return enumerator(n), true
		}
	case *syntax.Prim:
		return e.prim(x)
	case *syntax.Logical:
		l, lok := e.eval(x.Left)
		r, rok := e.eval(x.Right)
		if !lok || !rok {
			return value{}, false
		}
		lb, lbool := l.form.(bool)
		rb, rbool := r.form.(bool)
		if !lbool || !rbool {
			return e.inconstant(x)
		}
		return value{form: x.Op == "and" && lb && rb || x.Op == "or" && (lb || rb)}, true
	case *syntax.Chain:
		return e.chain(x)
	case *syntax.Coalesce:
		l, ok := e.eval(x.Left)
		if !ok {
			return value{}, false
		}
		if l.form == nil {
			return e.eval(x.Right)
		}
		return l, true
	case *syntax.Format:
		return e.format(x)
	case *syntax.ArrayLit:
		t, ok := unwrap(c.p.types[x]).(*syntax.Array)
		if !ok {
			return e.defect(x, "the literal has no array type")
		}
		elems := make([]value, len(x.Elems))
		for i, elem := range x.Elems {
			v, ok := e.eval(elem)
			if !ok {
				return value{}, false
			}
			elems[i] = widened(v, t.Elem)
		}
		return array(elems), true
	case *syntax.DictLit:
		t, ok := unwrap(c.p.types[x]).(*syntax.Dict)
		if !ok {
			return e.defect(x, "the literal has no dictionary type")
		}
		var keys, vals []value
		for _, entry := range x.Entries {
			k, kok := e.eval(entry.Key)
			v, vok := e.eval(entry.Value)
			if !kok || !vok {
				return value{}, false
			}
			keys, vals = append(keys, widened(k, t.Key)), append(vals, widened(v, t.Value))
		}
		return dictionary(keys, vals), true
	case *syntax.Construct:
		return e.construct(x)
	case *syntax.Call:
		return e.call(x, x.Args)
	case *syntax.MethodCall:
		return e.methodCall(x)
	case *syntax.IndexRead:
		return e.index(x)
	case *syntax.Slice:
		return e.slice(x)
	case *syntax.Bad:
		return value{}, false
	}
	return e.inconstant(x)
}

// enumerator returns the constant of the enum member that the typer recorded as a name with its enum type.
func enumerator(n *syntax.Name) value {
	owner := n.Owner.(*syntax.Named)
	return value{form: member{owner.Module, owner.Name, n.Name}, lit: n}
}

// valueType returns the name under which the built-in table holds the values of a type, looking through a nullable, or the empty string for a type whose values the table does not hold.
func valueType(t syntax.Type) string {
	t = unwrap(t)
	if a, ok := t.(*syntax.Array); ok && builtinName(a.Elem) == "Byte" {
		return "Byte[]"
	}
	return builtinName(t)
}

// literal evaluates a literal, or a negated integer literal, under the type that the typer gave it, where a literal that its integer type or Float does not hold has the diagnostic of the typer.
func (e *evaluation) literal(x syntax.Expr, t syntax.Type) (value, bool) {
	lit, plain := x.(*syntax.Lit)
	if plain && lit.Kind == syntax.KwNone {
		return value{lit: x}, true
	}
	name := valueType(t)
	form, ok := builtin.Value(x, name)
	if n, isInt := form.(int64); ok && isInt {
		form, ok = sized(n, t)
	}
	if ok {
		return value{form: form, lit: x}, true
	}
	if _, typed := integerBounds[name]; typed || name == "Float" || !plain {
		return value{}, false
	}
	return e.outOfRange(x, lit.Text, name)
}

// name evaluates a reference to a local constant, a module constant, an enum member, or a function.
func (e *evaluation) name(x *syntax.Name) (value, bool) {
	c := e.c
	t := c.p.Table
	if x.Owner != nil {
		if n, ok := c.p.constant[x].(*syntax.Name); ok {
			return enumerator(n), true
		}
		owner := t.Type(x.Owner)
		if m := t.Method(owner, x.Name); owner == nil || m == nil || m.Decl == nil {
			return e.inconstant(x)
		}
		ref := &syntax.Name{Origin: x.Origin, Owner: owner, Name: x.Name}
		return value{form: reference{ref}, lit: ref}, true
	}
	if b := c.p.bindings[x]; b != nil {
		switch {
		case !b.constant:
			return e.inconstant(x)
		case b.value == nil:
			return value{}, false
		}
		return b.value.v, true
	}
	var entry *resolve.Entry
	if x.Module != "" {
		entry = t.Lookup(x.Module, x.Name)
	} else {
		entry = t.Scope(e.module, x.Name)
	}
	switch {
	case entry == nil || entry.Decl == nil:
	case entry.Kind == resolve.ConstantKind:
		if k := c.global(entry.Decl.(*syntax.Const), x); k != nil {
			return k.v, true
		}
		return value{}, false
	case entry.Kind == resolve.FunctionKind:
		ref := &syntax.Name{Origin: x.Origin, Module: entry.Module, Name: entry.Name}
		return value{form: reference{ref}, lit: ref}, true
	}
	return e.inconstant(x)
}

// operands evaluates the operands of an operation and gives each the form of its parameter.
func (e *evaluation) operands(op *Operation, xs []syntax.Expr) ([]value, bool) {
	vs := make([]value, len(xs))
	for i, x := range xs {
		v, ok := e.eval(x)
		if !ok {
			return nil, false
		}
		vs[i] = v
	}
	return converted(op, vs), true
}

// converted gives each operand of an operation the form of its parameter.
func converted(op *Operation, vs []value) []value {
	for i := range vs {
		if i < len(op.Arguments) && op.Arguments[i].To != nil {
			vs[i] = widened(vs[i], op.Arguments[i].To)
		}
	}
	return vs
}

// prim evaluates an operator.
func (e *evaluation) prim(x *syntax.Prim) (value, bool) {
	c := e.c
	if lit, ok := x.Args[0].(*syntax.Lit); ok && x.Op == "-" && len(x.Args) == 1 {
		switch lit.Kind {
		case syntax.IntLit:
			return e.literal(x, c.p.types[x])
		case syntax.FloatLit:
			v, ok := e.eval(lit)
			if !ok {
				return value{}, false
			}
			if f, isFloat := v.form.(float64); isFloat {
				return value{form: -f + 0}, true
			}
			return value{form: rlr.NegDecimal(v.form.(rlr.Decimal))}, true
		}
	}
	k := c.p.callees[x]
	if k == nil || k.op == nil {
		vs, ok := e.operands(&Operation{}, x.Args)
		if !ok {
			return value{}, false
		}
		if len(vs) != 2 || x.Op != "==" && x.Op != "!=" {
			return e.inconstant(x)
		}
		return value{form: builtin.EqualValues(vs[0].form, vs[1].form) == (x.Op == "==")}, true
	}
	vs, ok := e.operands(k.op, x.Args)
	if !ok {
		return value{}, false
	}
	switch x.Op {
	case "==", "!=", "<", ">", "<=", ">=":
		holds, ok := e.compare(x, k.op.Row, vs[0], vs[1])
		return value{form: holds}, ok
	case "not":
		return value{form: !vs[0].form.(bool)}, true
	}
	if !slices.ContainsFunc(vs, func(v value) bool { return !arithmetic(v.form) }) {
		return e.exact(x, vs)
	}
	return e.fold(x, k.op.Row, vs)
}

// exact evaluates an arithmetic operator over signed integers or Floats.
func (e *evaluation) exact(x *syntax.Prim, vs []value) (value, bool) {
	if len(vs) == 1 {
		return e.negated(x, vs[0])
	}
	if a, ok := whole(vs[0].form); ok {
		b, _ := whole(vs[1].form)
		return e.integers(x, a, b)
	}
	return e.floats(x, vs[0].form.(float64), vs[1].form.(float64))
}

// arithmetic reports whether a constant is a signed integer or a Float, whose arithmetic the evaluator performs exactly.
func arithmetic(form any) bool {
	_, integer := whole(form)
	_, float := form.(float64)
	return integer || float
}

// negated evaluates the negation of a signed integer or a Float.
func (e *evaluation) negated(x *syntax.Prim, v value) (value, bool) {
	if f, ok := v.form.(float64); ok {
		return value{form: -f + 0}, true
	}
	n, _ := whole(v.form)
	t := e.c.p.types[x]
	if form, ok := sized(-n, t); ok && n != math.MinInt64 {
		return value{form: form}, true
	}
	return e.outOfRange(x, new(big.Int).Neg(big.NewInt(n)).String(), show(t))
}

// integers evaluates a binary operator over two signed integers by the checked operation of Int, where a result that the type of the expression does not hold is out of range.
func (e *evaluation) integers(x *syntax.Prim, a, b int64) (value, bool) {
	var n int64
	exact := true
	switch x.Op {
	case "+":
		n = a + b
		exact = (b >= 0) == (n >= a)
	case "-":
		n = a - b
		exact = (b >= 0) == (n <= a)
	case "*":
		n = a * b
		exact = a == 0 || n/a == b && (a != -1 || b != math.MinInt64)
	case "/", "%":
		switch {
		case b == 0:
			return e.divisionByZero(x.Args[1])
		case b == -1:
			n, exact = -a, x.Op == "%" || a != math.MinInt64
			if x.Op == "%" {
				n = 0
			}
		case x.Op == "/":
			n = a / b
		default:
			n = a % b
		}
	}
	t := e.c.p.types[x]
	if form, ok := sized(n, t); ok && exact {
		return value{form: form}, true
	}
	return e.outOfRange(x, wide(x.Op, a, b), show(t))
}

// wide returns the text of the mathematical result of an integer operator, which the diagnostic of a result out of range states.
func wide(op string, a, b int64) string {
	n, x, y := new(big.Int), big.NewInt(a), big.NewInt(b)
	switch op {
	case "+":
		n.Add(x, y)
	case "-":
		n.Sub(x, y)
	case "*":
		n.Mul(x, y)
	case "/":
		n.Quo(x, y)
	default:
		n.Rem(x, y)
	}
	return n.String()
}

// floats evaluates a binary operator over two Floats, where a result that is no finite number is out of range.
func (e *evaluation) floats(x *syntax.Prim, a, b float64) (value, bool) {
	var f float64
	switch x.Op {
	case "+":
		f = a + b
	case "-":
		f = a - b
	case "*":
		f = a * b
	case "/", "%":
		if b == 0 {
			return e.divisionByZero(x.Args[1])
		}
		if x.Op == "/" {
			f = a / b
		} else {
			f = math.Mod(a, b)
		}
	}
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return e.outOfRange(x, syntax.Print(x), "Float")
	}
	return value{form: f + 0}, true
}

// compare evaluates a comparison of two constants through the row of its operator: by the function of the row where it has one, by the order of Go for an integer, a Float, or a String, and by the equality of constants otherwise.
func (e *evaluation) compare(x syntax.Node, row *builtin.Row, l, r value) (bool, bool) {
	order := 0
	switch {
	case row.Impl != nil:
		results, _, ok := row.Fold([]any{l.form, r.form})
		if !ok || len(results) != 1 {
			_, ok := e.defect(x, "the function of "+row.Signature+" does not take the operands")
			return false, ok
		}
		if holds, isBool := results[0].(bool); isBool {
			return holds == (row.Name == "=="), true
		}
		order = results[0].(int)
	case row.Name == "==" || row.Name == "!=":
		return builtin.EqualValues(l.form, r.form) == (row.Name == "=="), true
	default:
		switch a := l.form.(type) {
		case uint8:
			order = cmp.Compare(a, r.form.(uint8))
		case float64:
			order = cmp.Compare(a, r.form.(float64))
		case string:
			order = cmp.Compare(a, r.form.(string))
		default:
			a64, aok := whole(l.form)
			b64, bok := whole(r.form)
			if !aok || !bok {
				_, ok := e.defect(x, "the operands of "+row.Signature+" have no order")
				return false, ok
			}
			order = cmp.Compare(a64, b64)
		}
	}
	switch row.Name {
	case "<":
		return order < 0, true
	case ">":
		return order > 0, true
	case "<=":
		return order <= 0, true
	}
	return order >= 0, true
}

// chain evaluates a chained comparison as the conjunction of its comparisons.
func (e *evaluation) chain(x *syntax.Chain) (value, bool) {
	ops := e.c.p.chains[x]
	vs := make([]value, len(x.Operands))
	for i, o := range x.Operands {
		v, ok := e.eval(o)
		if !ok {
			return value{}, false
		}
		vs[i] = v
	}
	result := true
	for i, op := range ops {
		holds, ok := e.compare(x, op.Row, widened(vs[i], op.Arguments[0].To), widened(vs[i+1], op.Arguments[1].To))
		if !ok {
			return value{}, false
		}
		result = result && holds
	}
	return value{form: result}, true
}

// arguments returns the argument values of a call of a built-in in the order of the parameters of its row.
func arguments(row *builtin.Row, in []*syntax.Arg) []syntax.Expr {
	names := builtin.Names(builtin.Lookup(row.Form, row.Name))
	args := make([]syntax.Expr, len(in))
	for i, a := range in {
		at := i
		if a.Name != "" {
			at = slices.Index(names, a.Name)
		}
		args[at] = a.Value
	}
	return args
}

// call evaluates a call, which is a constant expression when it reaches a constant-capable operation.
func (e *evaluation) call(x syntax.Expr, args []*syntax.Arg) (value, bool) {
	k := e.c.p.callees[x]
	if k == nil || k.op == nil || !k.op.Row.Constant {
		return e.inconstant(x)
	}
	vs, ok := e.operands(k.op, arguments(k.op.Row, args))
	if !ok {
		return value{}, false
	}
	return e.operation(x, k.op, vs)
}

// methodCall evaluates a call of a method of a built-in type, where a receiver that is no constant expression makes the call none.
func (e *evaluation) methodCall(x *syntax.MethodCall) (value, bool) {
	k := e.c.p.callees[x]
	if k == nil || k.op == nil || !k.op.Row.Constant {
		return e.inconstant(x)
	}
	receiver := evaluation{c: e.c, module: e.module}
	recv, ok := receiver.eval(x.Receiver)
	switch {
	case !ok && receiver.blame != nil:
		return e.inconstant(x)
	case !ok:
		return value{}, false
	}
	vs, ok := e.operands(&Operation{}, arguments(k.op.Row, x.Args))
	if !ok {
		return value{}, false
	}
	return e.operation(x, k.op, converted(k.op, append([]value{recv}, vs...)))
}

// operation executes a constant-capable operation over the values of its operands: an operation over a dictionary and the count of an array by the evaluator, a formatted string by the formatting of rlr, and every other operation by the base function of its row.
func (e *evaluation) operation(x syntax.Expr, op *Operation, vs []value) (value, bool) {
	row := op.Row
	if slices.Contains(row.Extra, builtin.Formatting) {
		return e.formatted(x, op, vs)
	}
	if len(vs) == 0 {
		return e.fold(x, row, vs)
	}
	d, isDict := vs[0].form.(dict)
	_, isString := vs[0].form.(string)
	switch {
	case row.Name == "count" && !isString:
		return value{form: int64(vs[0].count())}, true
	case row.Name == "zip":
		if vs[0].count() != vs[1].count() {
			return e.raised(x, rlr.LengthMismatch)
		}
		keys, vals := make([]value, vs[0].count()), make([]value, vs[1].count())
		for i := range keys {
			keys[i], vals[i] = vs[0].element(i), vs[1].element(i)
		}
		return dictionary(keys, vals), true
	case !isDict:
		return e.fold(x, row, vs)
	case row.Name == "has":
		return value{form: d.lookup(vs[1]) >= 0}, true
	case row.Name == "remove":
		at := d.lookup(vs[1])
		var keys, vals []value
		for i := range d.keys {
			if i != at {
				keys, vals = append(keys, d.keys[i]), append(vals, d.vals[i])
			}
		}
		return dictionary(keys, vals), true
	case row.Member == "get":
		if at := d.lookup(vs[1]); at >= 0 {
			return d.vals[at], true
		}
		return value{}, true
	}
	return e.defect(x, "the evaluator has no function for "+row.Signature)
}

// isBytes reports whether a type is an array of Byte.
func isBytes(t syntax.Type) bool {
	a, ok := t.(*syntax.Array)
	return ok && builtinName(a.Elem) == "Byte"
}

// fold calls the base function of a row with the forms of its operands, reports the eligible error that the function raises, and holds its results.
func (e *evaluation) fold(x syntax.Expr, row *builtin.Row, vs []value) (value, bool) {
	args := make([]any, len(vs))
	for i, v := range vs {
		args[i] = v.form
	}
	results, fault, ok := row.Fold(args)
	if !ok || len(results) == 0 {
		return e.defect(x, "the function of "+row.Signature+" does not take the operands")
	}
	if fault != rlr.NoFault {
		return e.raised(x, fault)
	}
	if n, isInt := whole(results[0]); isInt && row.Name == "abs" && n < 0 {
		return e.outOfRange(x, new(big.Int).Neg(big.NewInt(n)).String(), show(e.c.p.types[x]))
	}
	held := make(tuple, len(results))
	for i, r := range results {
		form, ok := e.hold(x, r)
		if !ok {
			return value{}, false
		}
		held[i] = value{form: form}
	}
	if len(held) == 1 {
		return held[0], true
	}
	return value{form: held}, true
}

// hold returns the form in which a constant holds a result of an rlr function: an absent nullable as nil, a present one as its payload, an array of a Go element type as an array of forms, and every other result as it is.
func (e *evaluation) hold(x syntax.Expr, result any) (any, bool) {
	switch r := result.(type) {
	case nil, []byte, []any:
		return r, true
	case float64:
		if math.IsInf(r, 0) || math.IsNaN(r) {
			_, ok := e.outOfRange(x, syntax.Print(x), "Float")
			return nil, ok
		}
		return r, true
	}
	switch rv := reflect.ValueOf(result); rv.Kind() {
	case reflect.Slice:
		forms := make([]any, rv.Len())
		for i := range forms {
			form, ok := e.hold(x, rv.Index(i).Interface())
			if !ok {
				return nil, false
			}
			forms[i] = form
		}
		return forms, true
	case reflect.Struct:
		if get := rv.MethodByName("Get"); get.IsValid() {
			got := get.Call(nil)
			if !got[1].Bool() {
				return nil, true
			}
			return e.hold(x, got[0].Interface())
		}
	}
	return result, true
}

// index evaluates an element read of a constant array, with the bounds check of rlr, or of a constant dictionary, with its key check.
func (e *evaluation) index(x *syntax.IndexRead) (value, bool) {
	k := e.c.p.callees[x]
	if k == nil || k.op == nil || !k.op.Row.Constant {
		return e.inconstant(x)
	}
	vs, ok := e.operands(k.op, []syntax.Expr{x.X, x.Index})
	if !ok {
		return value{}, false
	}
	if d, ok := vs[0].form.(dict); ok {
		at := d.lookup(vs[1])
		if at < 0 {
			return e.raised(x, rlr.MissingKey)
		}
		return d.vals[at], true
	}
	i, _ := whole(vs[1].form)
	if i < 0 || i >= int64(vs[0].count()) {
		return e.raised(x, rlr.OutOfRange)
	}
	return vs[0].element(int(i)), true
}

// slice evaluates a slice of a constant array, string, or byte array, where a bound that the source leaves out is the start or the count of the operand.
func (e *evaluation) slice(x *syntax.Slice) (value, bool) {
	k := e.c.p.callees[x]
	if k == nil || k.op == nil {
		return e.inconstant(x)
	}
	recv, ok := e.eval(x.X)
	if !ok {
		return value{}, false
	}
	n := recv.count()
	if s, isString := recv.form.(string); isString {
		n = len(s)
	}
	bounds := []value{recv, {form: int64(0)}, {form: int64(n)}}
	for i, b := range []syntax.Expr{x.Lo, x.Hi} {
		if b == nil {
			continue
		}
		v, ok := e.eval(b)
		if !ok {
			return value{}, false
		}
		bounds[i+1] = widened(v, intType)
	}
	if b, isBytes := recv.form.([]byte); isBytes {
		lo, hi := bounds[1].form.(int64), bounds[2].form.(int64)
		if lo < 0 || lo > hi || hi > int64(len(b)) {
			return e.raised(x, rlr.OutOfRange)
		}
		return value{form: slices.Clone(b[lo:hi])}, true
	}
	return e.fold(x, k.op.Row, bounds)
}

// construct evaluates a struct or error construction into a complete value: the supplied values and the default of every field that the construction leaves out.
func (e *evaluation) construct(x *syntax.Construct) (value, bool) {
	c := e.c
	named, ok := c.p.types[x].(*syntax.Named)
	if !ok {
		return e.inconstant(x)
	}
	entry := c.p.Table.Lookup(named.Module, named.Name)
	if entry == nil || entry.Decl == nil {
		return e.inconstant(x)
	}
	fields := resolve.FieldsOf(entry.Decl)
	supplied := map[*syntax.Field]value{}
	for i, a := range x.Args {
		f := fields[min(i, len(fields)-1)]
		for _, g := range fields {
			if a.Name != "" && g.Name == a.Name {
				f = g
			}
		}
		v, ok := e.eval(a.Value)
		if !ok {
			return value{}, false
		}
		supplied[f] = widened(v, c.p.Table.Type(f.Type))
	}
	r := record{typ: named}
	for _, f := range fields {
		v, ok := supplied[f]
		if !ok {
			if v, ok = c.fieldDefault(f, x); !ok {
				return value{}, false
			}
		}
		if len(x.Args) > 0 && x.Args[0].Name != "" {
			r.names = append(r.names, f.Name)
		}
		r.fields = append(r.fields, v)
	}
	return value{form: r}, true
}

// format evaluates a formatted string whose slots are constant expressions.
func (e *evaluation) format(x *syntax.Format) (value, bool) {
	k := e.c.p.callees[x]
	if k == nil || k.op == nil {
		return e.inconstant(x)
	}
	parts := []any{}
	slot := 0
	for _, part := range x.Parts {
		if part.Slot == nil {
			parts = append(parts, part.Text)
			continue
		}
		t := k.op.Arguments[slot].From
		slot++
		if e.c.p.unordered(t, map[*resolve.Entry]bool{}) {
			return e.inconstant(part.Slot)
		}
		v, ok := e.eval(part.Slot)
		if !ok {
			return value{}, false
		}
		parts = append(parts, e.c.display(v.form, t))
	}
	return e.rendered(x, parts)
}

// formatted evaluates a call of a built-in that formats its arguments.
func (e *evaluation) formatted(x syntax.Expr, op *Operation, vs []value) (value, bool) {
	parts := make([]any, len(vs))
	for i, v := range vs {
		t := op.Arguments[i].From
		if e.c.p.unordered(t, map[*resolve.Entry]bool{}) {
			return e.inconstant(x)
		}
		parts[i] = e.c.display(v.form, t)
	}
	return e.rendered(x, parts)
}

// rendered returns the formatted string of display values under the module of the evaluation.
func (e *evaluation) rendered(x syntax.Expr, parts []any) (value, bool) {
	text, fault := rlr.Format(e.module, parts...)
	if fault != rlr.NoFault {
		return e.raised(x, fault)
	}
	return value{form: text}, true
}

// unordered reports whether the default display of a value of a type depends on an order that the language leaves unspecified, which is the order of the entries of a dictionary at some depth.
func (p *Program) unordered(t syntax.Type, seen map[*resolve.Entry]bool) bool {
	switch t := t.(type) {
	case *syntax.Dict:
		return true
	case *syntax.Array:
		return p.unordered(t.Elem, seen)
	case *syntax.Nullable:
		return p.unordered(t.Elem, seen)
	case *syntax.Named:
		e := p.Table.Lookup(t.Module, t.Name)
		if e != nil && e.Decl != nil && !seen[e] {
			seen[e] = true
			for _, f := range resolve.FieldsOf(e.Decl) {
				if p.unordered(p.Table.Type(f.Type), seen) {
					return true
				}
			}
		}
	}
	return false
}

// failure is the display of a constant error value, which renders as the runtime renders an error: its name under the formatting module and each field canonically under its declaring module.
type failure struct {
	module, name string
	fields       []rlr.Field
}

// Display renders the error value under the formatting module.
func (f failure) Display(module string) string {
	text := f.name
	if f.module != module && f.module != "" {
		text = f.module + "::" + f.name
	}
	for i, field := range f.fields {
		if i == 0 {
			text += " { "
		} else {
			text += ", "
		}
		text += field.Name + ": " + rlr.CanonicalField(field.Value, f.module)
	}
	if len(f.fields) > 0 {
		text += " }"
	}
	return text
}

// display returns the value that rlr renders as the default display of a constant of a type.
func (c *checker) display(form any, t syntax.Type) any {
	t = unwrap(t)
	switch f := form.(type) {
	case nil:
		return rlr.None[any]()
	case member:
		enum := c.p.Table.Lookup(f.module, f.enum).Decl.(*syntax.Enum)
		shown := rlr.EnumValue{Name: f.name}
		for _, m := range enum.Members {
			if m.Name == f.name && m.Value != nil {
				shown.Value, _ = builtin.Value(m.Value, valueType(c.p.types[enum.Members[0].Value]))
			}
		}
		return shown
	case record:
		entry := c.p.Table.Lookup(f.typ.Module, f.typ.Name)
		fields := make([]rlr.Field, len(f.fields))
		for i, decl := range resolve.FieldsOf(entry.Decl) {
			fields[i] = rlr.Field{Name: decl.Name, Value: c.display(f.fields[i].form, c.p.Table.Type(decl.Type))}
		}
		if entry.Kind == resolve.ErrorKind {
			return failure{entry.Module, entry.Name, fields}
		}
		return rlr.StructValue{Module: entry.Module, Name: entry.Name, Fields: fields}
	case []byte:
		a, _ := t.(*syntax.Array)
		if a == nil || a.Length == nil {
			return f
		}
		elems := make([]any, len(f))
		for i, b := range f {
			elems[i] = b
		}
		return rlr.ArrayValue{Elems: elems}
	case []any:
		a, ok := t.(*syntax.Array)
		if !ok {
			return rlr.ArrayValue{Elems: f}
		}
		if isBytes(a) && a.Length == nil {
			bytes := make([]byte, len(f))
			for i, b := range f {
				bytes[i] = b.(uint8)
			}
			return bytes
		}
		elems := make([]any, len(f))
		for i, elem := range f {
			elems[i] = c.display(elem, a.Elem)
		}
		return rlr.ArrayValue{Elems: elems}
	}
	return form
}
