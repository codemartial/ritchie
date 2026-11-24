// This file maps Ritchie types to Go types and writes the value operations of each
// type through rlr: copy, display, equality, order, conversion, and literals.
package gen

import (
	"fmt"
	"strconv"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/check"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
	"ritchie.dev/rlr"
)

var scalars = map[string]string{
	"Int": "int64", "Int64": "int64", "Int8": "int8", "Int16": "int16", "Int32": "int32",
	"Float": "float64", "Bool": "bool", "Byte": "uint8", "String": "string", "BitSet": "rlr.BitSet",
	"Decimal": "rlr.Decimal", "Date": "rlr.Date", "Time": "rlr.Time", "DateTime": "rlr.DateTime",
	"Duration": "rlr.Duration", "Error!": "rlr.RitchieError", "ErrorStep": "rlr.ErrorStep",
	"ErrorType": "rlr.ErrorTypeDesc", "ErrorField": "rlr.ErrorFieldDesc", "ErrorLocation": "rlr.ErrorLocationDesc",
}

var rlrValues = map[string]bool{
	"Decimal": true, "Date": true, "Time": true, "DateTime": true, "Duration": true,
	"ErrorStep": true, "ErrorType": true, "ErrorField": true, "ErrorLocation": true,
}

// builtinName returns the name of the built-in type that a type is, or the empty string.
func builtinName(t syntax.Type) string {
	if n, ok := t.(*syntax.Named); ok && n.Module == "" && len(n.Args) == 0 {
		return n.Name
	}
	return ""
}

func isError(t syntax.Type) bool {
	n, ok := t.(*syntax.Named)
	return ok && strings.HasSuffix(n.Name, "!")
}

func isNone(t syntax.Type) bool { return builtinName(t) == "none" }

func isBytes(t syntax.Type) bool {
	a, ok := t.(*syntax.Array)
	return ok && builtinName(a.Elem) == "Byte"
}

// entry returns the declaration of a named type of the program, or nil.
func (e *emitter) entry(t syntax.Type) *resolve.Entry {
	n, ok := t.(*syntax.Named)
	if !ok {
		return nil
	}
	en := e.t.Lookup(n.Module, n.Name)
	if en == nil || en.Decl == nil {
		return nil
	}
	return en
}

// structOf returns the declaration of a struct type, or nil.
func (e *emitter) structOf(t syntax.Type) *resolve.Entry {
	if en := e.entry(t); en != nil && en.Kind == resolve.StructKind {
		return en
	}
	return nil
}

// enumOf returns the declaration of an enum type, or nil.
func (e *emitter) enumOf(t syntax.Type) *resolve.Entry {
	if en := e.entry(t); en != nil && en.Kind == resolve.EnumKind {
		return en
	}
	return nil
}

// backed reports whether an enum pins a backing value to each member.
func backed(en *resolve.Entry) bool { return en.Decl.(*syntax.Enum).Members[0].Value != nil }

// goType returns the Go type of a Ritchie type.
func (e *emitter) goType(t syntax.Type) string {
	switch t := t.(type) {
	case *syntax.Named:
		if isError(t) {
			return "rlr.RitchieError"
		}
		if g, ok := scalars[builtinName(t)]; ok {
			return g
		}
		if t.Module == "r" && (t.Name == "Clock" || t.Name == "Random") {
			return "rlr." + t.Name
		}
		if en := e.structOf(t); en != nil {
			return typeName(en.Module, en.Name)
		}
		if en := e.enumOf(t); en != nil {
			return typeName(en.Module, en.Name)
		}
	case *syntax.Future:
		return "rlr.Future[" + e.goType(t.Elem) + "]"
	case *syntax.Array:
		if elem := futureOf(t.Elem); elem != nil {
			return "rlr.Futures[" + e.goType(elem) + "]"
		}
		return "[]" + e.goType(t.Elem)
	case *syntax.Dict:
		p, ok := e.projection(t.Key)
		if !ok {
			break
		}
		if futureOf(t.Value) != nil {
			return e.futureMap(t)
		}
		if !e.keyable(t.Key) {
			return "map[" + p + "]rlr.Keyed[" + e.goType(t.Key) + ", " + e.goType(t.Value) + "]"
		}
		return "map[" + p + "]" + e.goType(t.Value)
	case *syntax.Nullable:
		return e.nullable(t).goType
	case *syntax.FuncType:
		var params, results []string
		for _, p := range t.Params {
			params = append(params, e.goType(p))
		}
		for _, r := range t.Results {
			results = append(results, e.goType(r))
		}
		return "func(" + strings.Join(params, ", ") + ") (" + strings.Join(append(results, "error"), ", ") + ")"
	}
	e.unsupportedType(t)
	return "any"
}

// unsupportedType reports a type that the backend does not map yet, at the routine being generated.
func (e *emitter) unsupportedType(t syntax.Type) {
	e.limit(e.at, fmt.Sprintf("the type %s is not supported by the backend", syntax.Print(t)))
}

// keyable reports whether a dictionary key type has a Go type whose equality is the Ritchie equality.
func (e *emitter) keyable(t syntax.Type) bool { return e.comparable(t, map[*resolve.Entry]bool{}) }

// projected reports whether a dictionary with a key type is a map from the key projection to entries that retain the key value.
func (e *emitter) projected(key syntax.Type) bool {
	if e.keyable(key) {
		return false
	}
	_, ok := e.projection(key)
	return ok
}

// projection returns the comparable Go type whose equality is the Ritchie equality of a key type, and false for a type with none.
func (e *emitter) projection(t syntax.Type) (string, bool) {
	if e.keyable(t) {
		return e.goType(t), true
	}
	if p, ok := builtin.KeyProjections[builtinName(t)]; ok {
		return "rlr." + p.Type, true
	}
	en := e.structOf(t)
	if en == nil {
		return "", false
	}
	var fields []string
	for _, f := range resolve.FieldsOf(en.Decl) {
		p, ok := e.projection(e.t.Type(f.Type))
		if !ok {
			return "", false
		}
		fields = append(fields, fieldName(f.Name)+" "+p)
	}
	return "struct{ " + strings.Join(fields, "; ") + " }", true
}

// project returns the Go expression of the projection of a key value x of a type.
func (e *emitter) project(t syntax.Type, x string) string {
	if e.keyable(t) {
		return x
	}
	if p, ok := builtin.KeyProjections[builtinName(t)]; ok {
		return "rlr." + p.Go + "(" + x + ")"
	}
	p, _ := e.projection(t)
	var fields []string
	for _, f := range resolve.FieldsOf(e.structOf(t).Decl) {
		fields = append(fields, e.project(e.t.Type(f.Type), x+"."+fieldName(f.Name)))
	}
	return p + "{" + strings.Join(fields, ", ") + "}"
}

// projectFn returns the Go function that projects a key value of a type.
func (e *emitter) projectFn(t syntax.Type) string {
	p, _ := e.projection(t)
	return "func(k " + e.goType(t) + ") " + p + " { return " + e.project(t, "k") + " }"
}

// entryValue returns the Go expression of the value stored in the Go map m of a dictionary with a key type, given the Go expression of the key projection p.
func (e *emitter) entryValue(key syntax.Type, m, p string) string {
	if e.projected(key) {
		return m + "[" + p + "].Val"
	}
	return m + "[" + p + "]"
}

// stored returns the Go expression of what the Go map m of a dictionary type stores under the projection p for the key k and the value v.
func (e *emitter) stored(t *syntax.Dict, m, p, k, v string) string {
	if e.projected(t.Key) {
		return "rlr.EntryKeyed(" + m + ", " + p + ", " + k + ", " + v + ")"
	}
	return v
}

// comparable reports whether Go equality on the Go type of a Ritchie type is the Ritchie equality, visiting each struct once.
func (e *emitter) comparable(t syntax.Type, seen map[*resolve.Entry]bool) bool {
	if n, ok := t.(*syntax.Nullable); ok {
		return e.comparable(n.Elem, seen)
	}
	if en := e.structOf(t); en != nil {
		if seen[en] {
			return false
		}
		seen[en] = true
		for _, f := range resolve.FieldsOf(en.Decl) {
			if !e.comparable(e.t.Type(f.Type), seen) {
				return false
			}
		}
		return true
	}
	if e.enumOf(t) != nil {
		return true
	}
	switch builtinName(t) {
	case "Int", "Int64", "Int8", "Int16", "Int32", "Bool", "Byte", "String", "BitSet", "Date", "Time":
		return true
	}
	return false
}

// recursive reports whether a type is a struct that contains itself through its fields and nullables, whose nullable is an rlr.Box: a Go struct cannot contain itself, and a copy of a box shares the recursive descendants that a write copies route by route.
func (e *emitter) recursive(t syntax.Type) bool { return e.p.Recursive(t) }

// nullable is the Go representation of a nullable type and the rlr functions of its operations, an rlr.Box for a struct that contains itself and an rlr.Option otherwise, which every consumer of a nullable reads here so that no consumer can omit a representation.
type nullable struct {
	boxed  bool
	goType string
	none   string
	some   string
	decode string
	view   string
	equal  string
}

// nullable returns the representation of a nullable type.
func (e *emitter) nullable(n *syntax.Nullable) nullable {
	elem := e.goType(n.Elem)
	if e.boxed(n.Elem) {
		return nullable{
			boxed:  true,
			goType: "rlr.Box[" + elem + "]",
			none:   "rlr.BoxNone[" + elem + "]()",
			some:   "rlr.BoxSome[" + elem + "]",
			decode: "rlr.DecodeBox",
			view:   "rlr.MapBox",
			equal:  "rlr.EqualBoxes",
		}
	}
	return nullable{
		goType: "rlr.Option[" + elem + "]",
		none:   "rlr.None[" + elem + "]()",
		some:   "rlr.Some[" + elem + "]",
		decode: "rlr.DecodeOption",
		view:   "rlr.MapOption",
		equal:  "rlr.EqualOptions",
	}
}

// boxed reports whether the nullable of a value type is an rlr.Box: the one choice of representation, which nullable, a narrowed read of the present value, and the sharing of a copy read.
func (e *emitter) boxed(t syntax.Type) bool { return e.recursive(t) }

// none returns the Go expression of the absent value of a nullable type.
func (e *emitter) none(n *syntax.Nullable) string { return e.nullable(n).none }

// deep reports whether a value of a type shares storage that a copy must duplicate, which is the deep copy class.
func (e *emitter) deep(t syntax.Type) bool {
	if elem := futureOf(t); elem != nil {
		return e.deep(elem)
	}
	return e.p.Copy(t) == builtin.Deep
}

// clone returns the Go expression of an independent copy of a value.
func (e *emitter) clone(t syntax.Type, x string) string {
	if !e.deep(t) {
		return x
	}
	switch t := t.(type) {
	case *syntax.Future:
		return x + ".Copy(" + e.cloner(t.Elem) + ")"
	case *syntax.Array:
		if elem := futureOf(t.Elem); elem != nil {
			return x + ".Copy(" + e.cloner(elem) + ")"
		}
		if e.deep(t.Elem) {
			return "rlr.CloneDeep(" + x + ", " + e.cloneFn(t.Elem) + ")"
		}
		return "rlr.Clone(" + x + ")"
	case *syntax.Dict:
		if elem := futureOf(t.Value); elem != nil {
			return x + ".Copy(" + e.cloneFn(&syntax.Dict{Key: t.Key, Value: elem}) + ")"
		}
		if e.projected(t.Key) {
			return "rlr.CloneKeyed(" + x + ", " + e.cloneFn(t.Value) + ")"
		}
		return "rlr.CloneDict(" + x + ", " + e.cloneFn(t.Value) + ")"
	case *syntax.Nullable:
		return "rlr.CloneOption(" + x + ", " + e.cloneFn(t.Elem) + ")"
	}
	return x + ".clone()"
}

// cloneFn returns the Go function that copies a value of a type.
func (e *emitter) cloneFn(t syntax.Type) string {
	g := e.goType(t)
	switch {
	case !e.deep(t):
		return "rlr.Same[" + g + "]"
	case e.structOf(t) != nil:
		return g + ".clone"
	}
	return "func(v " + g + ") " + g + " { return " + e.clone(t, "v") + " }"
}

// plainDisplay reports whether rlr renders a value of a type as it is.
func (e *emitter) plainDisplay(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.Array:
		return isBytes(t) && t.Length == nil
	case *syntax.Dict, *syntax.FuncType:
		return false
	case *syntax.Nullable:
		return !e.nullable(t).boxed && e.plainDisplay(t.Elem)
	case *syntax.Named:
		return e.enumOf(t) == nil
	}
	return true
}

// display returns the Go expression of the value that rlr renders as the default display of a value.
func (e *emitter) display(t syntax.Type, x string) string {
	if e.enumOf(t) != nil {
		return x + ".form()"
	}
	if e.plainDisplay(t) {
		return x
	}
	switch t := t.(type) {
	case *syntax.Array:
		return "rlr.Elements(" + x + ", " + e.embedFn(t.Elem) + ")"
	case *syntax.Dict:
		if e.projected(t.Key) {
			return "rlr.EntriesKeyed(" + x + ", " + e.embedFn(t.Key) + ", " + e.embedFn(t.Value) + ")"
		}
		return "rlr.Entries(" + x + ", " + e.embedFn(t.Key) + ", " + e.embedFn(t.Value) + ")"
	case *syntax.Nullable:
		return e.nullable(t).view + "(" + x + ", " + e.embedFn(t.Elem) + ")"
	}
	return "rlr.Opaque(" + strconv.Quote(syntax.Print(t)) + ")"
}

// embedFn returns the Go function that maps a value of a type to the value rlr renders.
func (e *emitter) embedFn(t syntax.Type) string {
	g := e.goType(t)
	if e.plainDisplay(t) {
		return "rlr.Plain[" + g + "]"
	}
	return "func(v " + g + ") any { return " + e.display(t, "v") + " }"
}

// equatable reports whether the backend can compare two values of a type.
func (e *emitter) equatable(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.FuncType:
		return false
	case *syntax.Array:
		return e.equatable(t.Elem)
	case *syntax.Dict:
		return e.equatable(t.Value)
	case *syntax.Nullable:
		return e.equatable(t.Elem)
	case *syntax.Named:
		return builtinName(t) != "Error!" && t.Module != "r"
	}
	return true
}

// equal returns the Go expression that two values of a type are equal.
func (e *emitter) equal(t syntax.Type, a, b string) string {
	switch t := t.(type) {
	case *syntax.Array:
		if isBytes(t) {
			return "rlr.EqualBytes(" + a + ", " + b + ")"
		}
		return "rlr.EqualSlices(" + a + ", " + b + ", " + e.equalFn(t.Elem) + ")"
	case *syntax.Nullable:
		return e.nullable(t).equal + "(" + a + ", " + b + ", " + e.equalFn(t.Elem) + ")"
	case *syntax.Dict:
		if e.projected(t.Key) {
			return "rlr.EqualKeyed(" + a + ", " + b + ", " + e.equalFn(t.Value) + ")"
		}
		return "rlr.EqualDicts(" + a + ", " + b + ", " + e.equalFn(t.Value) + ")"
	}
	if name := builtinName(t); rlrValues[name] {
		return "rlr.Equal" + name + "(" + a + ", " + b + ")"
	}
	if isError(t) {
		if en := e.entry(t); en != nil && len(resolve.FieldsOf(en.Decl)) > 0 {
			return "rlr.EqualErrors(" + a + ", " + b + ", " + typeName(en.Module, en.Name) + ".equal)"
		}
		return "rlr.SameType(" + a + ", " + b + ")"
	}
	if e.structOf(t) != nil {
		return a + ".equal(" + b + ")"
	}
	return a + " == " + b
}

// equalFn returns the Go function that compares two values of a type.
func (e *emitter) equalFn(t syntax.Type) string {
	g := e.goType(t)
	switch {
	case e.structOf(t) != nil:
		return g + ".equal"
	case rlrValues[builtinName(t)]:
		return "rlr.Equal" + builtinName(t)
	}
	if _, ok := scalars[builtinName(t)]; ok {
		return "rlr.Equal[" + g + "]"
	}
	return "func(a, b " + g + ") bool { return " + e.equal(t, "a", "b") + " }"
}

// lessFn returns the Go function of the natural order of an ordered type.
func (e *emitter) lessFn(t syntax.Type) string {
	if isBytes(t) {
		return "rlr.LessBytes"
	}
	switch name := builtinName(t); name {
	case "Float", "Decimal", "Date", "Time", "DateTime", "Duration":
		return "rlr.Less" + name
	}
	return "rlr.Less[" + e.goType(t) + "]"
}

var signedWidths = map[string]int{"Int8": 8, "Int16": 16, "Int32": 32, "Int": 64, "Int64": 64}

// convert returns the Go expression of a value of one type at a position that requires another: integer widening, the injection into a nullable, and the future or the collection of futures that holds a value.
func (e *emitter) convert(from, to syntax.Type, x string) string {
	if from == nil || to == nil || check.Same(from, to) {
		return x
	}
	if futureOf(to) != nil || check.Futures(to) {
		return e.future(from, to, x)
	}
	if n, ok := to.(*syntax.Nullable); ok {
		elem := e.goType(n.Elem)
		switch f := from.(type) {
		case *syntax.Nullable:
			if signedWidths[builtinName(f.Elem)] != 0 && !check.Same(f.Elem, n.Elem) {
				return "rlr.WidenOption[" + e.goType(f.Elem) + ", " + elem + "](" + x + ")"
			}
			return x
		}
		if isNone(from) {
			return e.none(n)
		}
		return e.nullable(n).some + "(" + e.convert(from, n.Elem, x) + ")"
	}
	if w, v := signedWidths[builtinName(from)], signedWidths[builtinName(to)]; w != 0 && v != 0 && w != v {
		return e.goType(to) + "(" + x + ")"
	}
	return x
}

// literal returns the Go expression of a literal of a type, where a negated literal is one literal.
func (e *emitter) literal(x syntax.Expr, t syntax.Type) string {
	if lit, ok := x.(*syntax.Lit); ok && lit.Kind == syntax.KwNone {
		if n, ok := t.(*syntax.Nullable); ok {
			return e.none(n)
		}
		return "rlr.None[any]()"
	}
	if n, ok := t.(*syntax.Nullable); ok {
		t = n.Elem
	}
	name := builtinName(t)
	if isBytes(t) {
		name = "Byte[]"
	}
	if p, ok := x.(*syntax.Prim); ok && name == "Decimal" {
		return "rlr.NegDecimal(" + e.literal(p.Args[0], t) + ")"
	}
	v, ok := builtin.Value(x, name)
	if !ok {
		panic(fmt.Sprintf("gen: literal %s has no value of type %s", syntax.Print(x), syntax.Print(t)))
	}
	switch v := v.(type) {
	case int64:
		return e.goType(t) + "(" + strconv.FormatInt(v, 10) + ")"
	case uint8:
		return "uint8(" + strconv.FormatUint(uint64(v), 10) + ")"
	case rlr.BitSet:
		return "rlr.BitSet(" + strconv.FormatUint(uint64(v), 10) + ")"
	case float64:
		return "float64(" + strconv.FormatFloat(v, 'g', -1, 64) + ")"
	case bool:
		return strconv.FormatBool(v)
	case string:
		return strconv.Quote(v)
	case []byte:
		return "[]byte(" + strconv.Quote(string(v)) + ")"
	case rlr.Decimal:
		hi, lo := v.Bits()
		return fmt.Sprintf("rlr.DecimalFromBits(%#x, %#x)", hi, lo)
	case rlr.Duration:
		return fmt.Sprintf("rlr.DurationFromBits(%d)", v.Bits())
	case rlr.Date:
		return fmt.Sprintf("rlr.DateFromDays(%d)", v.Days())
	case rlr.Time:
		return fmt.Sprintf("rlr.TimeFromNanos(%d)", v.Nanos())
	case rlr.DateTime:
		sec, nsec := v.Unix()
		return fmt.Sprintf("rlr.DateTimeFromParts(%d, %d, %d)", sec, nsec, v.Offset())
	}
	panic(fmt.Sprintf("gen: unexpected literal value %T", v))
}
