// This file is the capability table, which states what a type can do, and the rule
// that derives from it whether constant evaluation admits a mapping.
package builtin

import (
	"slices"

	"ritchie.dev/internal/syntax"
)

// Copy is the copy class of a type: flat for a value that shares no storage, and deep for a value that holds an array or a dictionary at some depth outside a boxed nullable.
type Copy uint8

const (
	Flat Copy = iota + 1
	Deep
)

// Equality is the equality class of a type: native where the Go operator on its Go type is its equality, custom where a function is, and none for a type without equality.
type Equality uint8

const (
	NoEquality Equality = iota
	Native
	Custom
)

// Order is the order class of a type: none for a type without a natural order, indistinguishable where two values that the order treats as equal are the same value, and distinguishable otherwise.
type Order uint8

const (
	NoOrder Order = iota
	Indistinguishable
	Distinguishable
)

// Capabilities state what a value of a type can do, where Constant reports whether the type has constant values.
type Capabilities struct {
	Copy     Copy
	Equality Equality
	Order    Order
	Constant bool
}

var (
	ordinal = Capabilities{Flat, Native, Indistinguishable, true}
	nominal = Capabilities{Flat, Native, NoOrder, true}
	record  = Capabilities{Flat, Custom, NoOrder, true}
	opaque  = Capabilities{Flat, NoEquality, NoOrder, false}
	bytes   = Capabilities{Deep, Custom, Indistinguishable, true}
)

// Of returns the capabilities that the table states for a type, which is a built-in type or an array of Byte, or false for a type whose capabilities its parts determine.
func Of(t syntax.Type) (Capabilities, bool) {
	switch t := t.(type) {
	case *syntax.Named:
		if rows := Lookup(TypeRow, syntax.Print(t)); len(rows) == 1 {
			return rows[0].Capabilities, true
		}
	case *syntax.Array:
		if e, ok := t.Elem.(*syntax.Named); ok && e.Module == "" && e.Name == "Byte" {
			return bytes, true
		}
	}
	return Capabilities{}, false
}

// constantValues reports whether a type of a signature has constant values, where a letter stands for a type that has them and a composite type has them when every part has them.
func constantValues(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.Named:
		if _, ok := Letter(t); ok {
			return true
		}
		rows := Lookup(TypeRow, syntax.Print(t))
		return len(rows) == 1 && rows[0].Capabilities.Constant
	case *syntax.Array:
		return constantValues(t.Elem)
	case *syntax.Dict:
		return constantValues(t.Key) && constantValues(t.Value)
	case *syntax.Nullable:
		return constantValues(t.Elem)
	}
	return false
}

// constant reports whether a row is a constant-capable operation, by the rule that the reference states with the eligible error set.
func (r *Row) constant() bool {
	switch {
	case r.Form != Function && r.Form != Operator && r.Form != TypeMethod && r.Form != Method:
		return false
	case r.ResultNone, r.Iterator, r.Effect, r.Standalone, r.Unordered:
		return false
	}
	for _, f := range r.Faults {
		if e := Lookup(ErrorRow, f); len(e) != 1 || !e[0].Eligible {
			return false
		}
	}
	return !slices.ContainsFunc(slices.Concat(r.Inputs, r.Results), func(t syntax.Type) bool { return !constantValues(t) })
}
