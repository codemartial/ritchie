// This file decides type identity and assignability, the only structural comparisons of
// types in the compiler, and names the built-in types the checker constructs.
package check

import (
	"strconv"

	"ritchie.dev/internal/syntax"
)

func named(name string) *syntax.Named { return &syntax.Named{Name: name} }

var (
	intType     = named("Int")
	boolType    = named("Bool")
	stringType  = named("String")
	floatType   = named("Float")
	decimalType = named("Decimal")
	byteType    = named("Byte")
	errorType   = named("Error!")
	noneType    = named("none")
	bytesType   = &syntax.Array{Elem: byteType}
)

var signedWidths = map[string]int{"Int8": 8, "Int16": 16, "Int32": 32, "Int": 64}

// norm returns a type with Int64 written as Int.
func norm(t syntax.Type) syntax.Type {
	if n, ok := t.(*syntax.Named); ok && n.Module == "" && n.Name == "Int64" && len(n.Args) == 0 {
		return intType
	}
	return t
}

// builtinName returns the name of the built-in type that a type is, or the empty string.
func builtinName(t syntax.Type) string {
	if n, ok := norm(t).(*syntax.Named); ok && n.Module == "" && len(n.Args) == 0 {
		return n.Name
	}
	return ""
}

// width returns the width of a signed integer type, or zero.
func width(t syntax.Type) int { return signedWidths[builtinName(t)] }

// lengthOf returns the fixed length of an array type, or false for the dynamic form.
func lengthOf(t *syntax.Array) (int64, bool) {
	lit, ok := t.Length.(*syntax.Lit)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(lit.Canonical(), 10, 64)
	return n, err == nil
}

// constantInt returns the value of a constant Int expression, or false when the expression is not constant or not an integer.
func (c *checker) constantInt(x syntax.Expr, sc *scope, r *routine) (int64, bool) {
	v := c.constantOf(x, sc, r)
	negative := false
	if p, ok := v.(*syntax.Prim); ok && p.Op == "-" && len(p.Args) == 1 {
		v, negative = p.Args[0], true
	}
	lit, ok := v.(*syntax.Lit)
	if !ok || lit.Kind != syntax.IntLit {
		return 0, false
	}
	n, err := strconv.ParseInt(lit.Canonical(), 10, 64)
	if err != nil {
		return 0, false
	}
	if negative {
		n = -n
	}
	return n, true
}

// Same reports whether two types are one type: named types by declaration, composites by their parts.
func Same(a, b syntax.Type) bool {
	a, b = norm(a), norm(b)
	switch a := a.(type) {
	case *syntax.Named:
		b, ok := b.(*syntax.Named)
		if !ok || a.Module != b.Module || a.Name != b.Name || len(a.Args) != len(b.Args) {
			return false
		}
		for i := range a.Args {
			if !Same(a.Args[i], b.Args[i]) {
				return false
			}
		}
		return true
	case *syntax.Array:
		b, ok := b.(*syntax.Array)
		if !ok || !Same(a.Elem, b.Elem) {
			return false
		}
		an, afixed := lengthOf(a)
		bn, bfixed := lengthOf(b)
		return afixed == bfixed && an == bn
	case *syntax.Dict:
		b, ok := b.(*syntax.Dict)
		return ok && Same(a.Key, b.Key) && Same(a.Value, b.Value)
	case *syntax.Nullable:
		b, ok := b.(*syntax.Nullable)
		return ok && Same(a.Elem, b.Elem)
	case *syntax.Future:
		b, ok := b.(*syntax.Future)
		return ok && Same(a.Elem, b.Elem)
	case *syntax.FuncType:
		b, ok := b.(*syntax.FuncType)
		if !ok || len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
			return false
		}
		for i := range a.Params {
			if !Same(a.Params[i], b.Params[i]) {
				return false
			}
		}
		for i := range a.Results {
			if !Same(a.Results[i], b.Results[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// Assignable reports whether a value of one type stands where another is required: the same type, a narrower signed integer, a value injected into its nullable, an error type where Error! is required, or a value or a collection of values where a future or a collection of futures of the same type is required, which then holds the values.
func Assignable(from, to syntax.Type) bool {
	if from == nil || to == nil {
		return false
	}
	if Same(from, to) {
		return true
	}
	if f, ok := to.(*syntax.Future); ok {
		return Same(from, f.Elem)
	}
	if Futures(to) && !Futures(from) {
		return Same(from, Waited(to))
	}
	if w, v := width(from), width(to); w != 0 && v != 0 {
		return w < v
	}
	if n, ok := to.(*syntax.Nullable); ok {
		if isNone(from) {
			return true
		}
		if f, ok := from.(*syntax.Nullable); ok {
			return Assignable(f.Elem, n.Elem)
		}
		return Assignable(from, n.Elem)
	}
	return isError(from) && builtinName(to) == "Error!"
}

// Futures reports whether a type is a collection whose elements are futures: an array of futures, or a dictionary whose values are futures.
func Futures(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.Array:
		_, ok := t.Elem.(*syntax.Future)
		return ok
	case *syntax.Dict:
		_, ok := t.Value.(*syntax.Future)
		return ok
	}
	return false
}

// Waited returns the type of the value that a wait yields for a future or for a collection of futures, or nil for any other type.
func Waited(t syntax.Type) syntax.Type {
	switch t := t.(type) {
	case *syntax.Future:
		return t.Elem
	case *syntax.Array:
		if f, ok := t.Elem.(*syntax.Future); ok {
			return &syntax.Array{Elem: f.Elem, Length: t.Length}
		}
	case *syntax.Dict:
		if f, ok := t.Value.(*syntax.Future); ok {
			return &syntax.Dict{Key: t.Key, Value: f.Elem}
		}
	}
	return nil
}

// isNone reports whether a type is the type of the literal none outside a nullable context.
func isNone(t syntax.Type) bool { return builtinName(t) == "none" }

// isError reports whether a type is an error type.
func isError(t syntax.Type) bool {
	n, ok := t.(*syntax.Named)
	return ok && len(n.Name) > 1 && n.Name[len(n.Name)-1] == '!'
}

// join returns the type that two types share: the wider signed integer, the nullable of a value, or false.
func join(a, b syntax.Type) (syntax.Type, bool) {
	switch {
	case Assignable(b, a):
		return a, true
	case Assignable(a, b):
		return b, true
	}
	return nil, false
}

// comparable reports whether == applies to values of a type.
func comparable(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.FuncType:
		return false
	case *syntax.Named:
		return t.Module != "r"
	case *syntax.Array:
		return comparable(t.Elem)
	case *syntax.Dict:
		return comparable(t.Value)
	case *syntax.Nullable:
		return comparable(t.Elem)
	}
	return false
}

// displayable reports whether a value of a type has a default display.
func displayable(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.FuncType, *syntax.Future:
		return false
	case *syntax.Array:
		return displayable(t.Elem)
	case *syntax.Dict:
		return displayable(t.Key) && displayable(t.Value)
	case *syntax.Nullable:
		return displayable(t.Elem)
	}
	return true
}

// unwrap returns the element of a nullable type, or the type itself.
func unwrap(t syntax.Type) syntax.Type {
	if n, ok := t.(*syntax.Nullable); ok {
		return n.Elem
	}
	return t
}

// nullable returns the nullable of a type, where a nullable stays as it is.
func nullable(t syntax.Type) syntax.Type {
	if _, ok := t.(*syntax.Nullable); ok {
		return t
	}
	return &syntax.Nullable{Elem: t}
}

// show writes a type for a diagnostic.
func show(t syntax.Type) string {
	if t == nil {
		return "an invalid type"
	}
	return syntax.Print(t)
}
