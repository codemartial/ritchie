// This file records the resolved form of each built-in call, which every stage after
// the typer reads in place of deriving it from the types of the arguments.
package check

import (
	"slices"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/syntax"
)

// ConversionKind is what a built-in call does to one argument before its function takes it.
type ConversionKind uint8

const (
	Unconverted ConversionKind = iota
	Widening
	KeyProjection
	Display
	Address
)

// Conversion is the conversion of one argument of the type From, where To is the parameter type that a widening or a key projection gives it.
type Conversion struct {
	Kind     ConversionKind
	From, To syntax.Type
}

// Operation is the resolved form of one built-in call: the row that the typer selected, the types that it bound to the letters of the row's signature, the variant of the row that the classes of its element type select, and the conversion of each argument.
type Operation struct {
	Row       *builtin.Row
	Bindings  map[string]syntax.Type
	Variant   builtin.Variant
	Arguments []Conversion
}

// Element returns the type of the values that the collection of the call holds.
func (o *Operation) Element() syntax.Type {
	if v := o.Bindings["V"]; v != nil {
		return v
	}
	return o.Bindings["T"]
}

// resolved returns the operation of a row selected for argument types under the bindings of its letters and array forms.
func (c *checker) resolved(row *builtin.Row, bind map[string]syntax.Type, types []syntax.Type) *Operation {
	op := &Operation{Row: row, Bindings: map[string]syntax.Type{}, Arguments: make([]Conversion, len(types))}
	for letter, t := range bind {
		if len(letter) == 1 {
			op.Bindings[letter] = t
		}
	}
	if elem := op.Element(); elem != nil {
		op.Variant = row.Select(c.p.Copy(elem), order(elem))
	}
	formatting := slices.Contains(row.Extra, builtin.Formatting)
	for i, t := range types {
		c := Conversion{From: t}
		param := row.Inputs[min(i, len(row.Inputs)-1)]
		letter, isLetter := builtin.Letter(param)
		to := instantiate(param, bind, nil)
		switch {
		case i == 0 && row.Ref:
			c.Kind = Address
		case isLetter && formatting:
			c.Kind = Display
		case letter == "K":
			c.Kind, c.To = KeyProjection, to
		case !Same(t, to):
			c.Kind, c.To = Widening, to
		}
		op.Arguments[i] = c
	}
	return op
}

// indexed returns the operation of an index operator on an array or a dictionary with its index or key and, for a store, its value.
func (c *checker) indexed(name string, container syntax.Type, rest ...syntax.Type) *Operation {
	rows := builtin.Lookup(builtin.Operator, name)
	types := append([]syntax.Type{container}, rest...)
	switch t := container.(type) {
	case *syntax.Array:
		return c.resolved(rows[0], map[string]syntax.Type{"T": t.Elem, "[N": t}, types)
	case *syntax.Dict:
		return c.resolved(rows[1], map[string]syntax.Type{"K": t.Key, "V": t.Value}, types)
	}
	return nil
}
