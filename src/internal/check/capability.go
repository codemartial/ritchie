// This file derives the copy class and the order class of a type from the capability
// table of the built-ins and from the parts of a declared type.
package check

import (
	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// structOf returns the declaration of a struct type, or nil.
func (p *Program) structOf(t syntax.Type) *resolve.Entry {
	n, ok := t.(*syntax.Named)
	if !ok {
		return nil
	}
	if e := p.Table.Lookup(n.Module, n.Name); e != nil && e.Decl != nil && e.Kind == resolve.StructKind {
		return e
	}
	return nil
}

// Recursive reports whether a type is a struct that contains itself through its fields and nullables, whose nullable shares the contained value and so is of the flat copy class.
func (p *Program) Recursive(t syntax.Type) bool {
	e := p.structOf(t)
	if e == nil {
		return false
	}
	r, ok := p.cyclic[e]
	if !ok {
		r = p.contains(e, e.Decl, map[syntax.Node]bool{})
		p.cyclic[e] = r
	}
	return r
}

// contains reports whether a declaration holds a value of a struct type by value, through its fields and nullables.
func (p *Program) contains(target *resolve.Entry, decl syntax.Node, seen map[syntax.Node]bool) bool {
	if seen[decl] {
		return false
	}
	seen[decl] = true
	for _, f := range resolve.FieldsOf(decl) {
		t := p.Table.Type(f.Type)
		if n, ok := t.(*syntax.Nullable); ok {
			t = n.Elem
		}
		if e := p.structOf(t); e != nil && (e == target || p.contains(target, e.Decl, seen)) {
			return true
		}
	}
	return false
}

// Copy returns the copy class of a type: deep for a type that holds an array or a dictionary at some depth outside the nullable of a recursive struct, and flat for every other type.
func (p *Program) Copy(t syntax.Type) builtin.Copy {
	return p.copy(t, map[*resolve.Entry]bool{})
}

// copy returns the copy class of a type, visiting each struct once.
func (p *Program) copy(t syntax.Type, seen map[*resolve.Entry]bool) builtin.Copy {
	if c, ok := builtin.Of(t); ok {
		return c.Copy
	}
	switch t := t.(type) {
	case *syntax.Array, *syntax.Dict:
		return builtin.Deep
	case *syntax.Nullable:
		if p.Recursive(t.Elem) {
			return builtin.Flat
		}
		return p.copy(t.Elem, seen)
	case *syntax.Named:
		if e := p.structOf(t); e != nil && !seen[e] {
			seen[e] = true
			for _, f := range resolve.FieldsOf(e.Decl) {
				if p.copy(p.Table.Type(f.Type), seen) == builtin.Deep {
					return builtin.Deep
				}
			}
		}
	}
	return builtin.Flat
}

// order returns the order class of a type, which only the capability table gives.
func order(t syntax.Type) builtin.Order {
	c, _ := builtin.Of(t)
	return c.Order
}
