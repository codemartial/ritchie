// This file resolves written types to their canonical form: the declaring module filled
// in and aliases expanded.
package resolve

import (
	"fmt"
	"strconv"
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
)

// resolveType returns the canonical form of a type written in a module, or nil when it does not resolve, where a type whose resolution demands the type itself, through the constant that it writes as a length, does not resolve.
func (c *collector) resolveType(ty syntax.Type, module string) syntax.Type {
	if r, seen := c.t.types[ty]; seen {
		return r
	}
	if c.resolving[ty] {
		c.at(ty, diagnostics.ConstantDependencyCycle, "a constant depends on itself")
		c.t.types[ty] = nil
		return nil
	}
	c.resolving[ty] = true
	r := c.canonical(ty, module)
	delete(c.resolving, ty)
	c.t.types[ty] = r
	return r
}

func (c *collector) canonical(ty syntax.Type, module string) syntax.Type {
	switch ty := ty.(type) {
	case *syntax.Named:
		e := c.head(ty, module)
		if e == nil {
			return nil
		}
		if e.Kind == AliasKind {
			return c.alias(e, ty)
		}
		args := make([]syntax.Type, len(ty.Args))
		for i, a := range ty.Args {
			if args[i] = c.resolveType(a, module); args[i] == nil {
				return nil
			}
		}
		return &syntax.Named{Origin: ty.Origin, Module: e.Module, Name: e.Name, Args: args}
	case *syntax.Array:
		elem := c.resolveType(ty.Elem, module)
		length, ok := c.length(ty.Length, module)
		if elem != nil && ok {
			return &syntax.Array{Origin: ty.Origin, Elem: elem, Length: length}
		}
	case *syntax.Dict:
		key, value := c.resolveType(ty.Key, module), c.resolveType(ty.Value, module)
		if key != nil && value != nil {
			c.dicts = append(c.dicts, ty)
			return &syntax.Dict{Origin: ty.Origin, Key: key, Value: value}
		}
	case *syntax.Nullable:
		elem := c.resolveType(ty.Elem, module)
		if _, nested := elem.(*syntax.Nullable); nested {
			c.at(ty, diagnostics.NullableRepeated, fmt.Sprintf("absence does not nest, and %s is already nullable", syntax.Print(ty.Elem)))
			return nil
		}
		if elem != nil {
			return &syntax.Nullable{Origin: ty.Origin, Elem: elem}
		}
	case *syntax.Future:
		if elem := c.resolveType(ty.Elem, module); elem != nil {
			return &syntax.Future{Origin: ty.Origin, Elem: elem}
		}
	case *syntax.FuncType:
		params := c.resolveTypes(ty.Params, module)
		results := c.resolveTypes(ty.Results, module)
		if params != nil && results != nil {
			return &syntax.FuncType{Origin: ty.Origin, Params: params, Results: results}
		}
	case *syntax.Bad:
	default:
		panic(fmt.Sprintf("resolve: unexpected type %T", ty))
	}
	return nil
}

// ResolveType resolves a type written in a body of a module to its canonical form.
func (t *Table) ResolveType(ty syntax.Type, module string) (syntax.Type, []diagnostics.Record) {
	c := t.c
	from := len(c.diags)
	r := c.resolveType(ty, module)
	if !c.collecting {
		c.checkKeys()
	}
	diags := append([]diagnostics.Record(nil), c.diags[from:]...)
	c.diags = c.diags[:from]
	return r, diags
}

// ValidKey reports whether a canonical type may key a dictionary: a primitive other than Float, an enum, or a struct whose fields recursively qualify.
func (t *Table) ValidKey(ty syntax.Type) bool { return t.validKey(ty, map[*Entry]bool{}) }

func (t *Table) validKey(ty syntax.Type, seen map[*Entry]bool) bool {
	n, ok := ty.(*syntax.Named)
	if !ok || n.Module == "" && n.Name == "Float" && len(n.Args) == 0 || len(n.Name) > 1 && strings.HasSuffix(n.Name, "!") {
		return false
	}
	e := t.Lookup(n.Module, n.Name)
	if e == nil || e.Kind != StructKind || seen[e] {
		return true
	}
	seen[e] = true
	for _, f := range FieldsOf(e.Decl) {
		if ft := t.types[f.Type]; ft != nil && !t.validKey(ft, seen) {
			return false
		}
	}
	return true
}

// checkKeys reports each written dictionary type resolved since the last report whose key type cannot key a dictionary.
func (c *collector) checkKeys() {
	for _, d := range c.dicts {
		if key := c.t.types[d.Key]; !c.t.ValidKey(key) {
			c.at(d, diagnostics.TypeInvalidKey, syntax.Print(key)+" is not a dictionary key type")
		}
	}
	c.dicts = c.dicts[:0]
}

// resolveTypes resolves a list of types, returning nil when one of them does not resolve.
func (c *collector) resolveTypes(list []syntax.Type, module string) []syntax.Type {
	out := make([]syntax.Type, len(list))
	for i, t := range list {
		if out[i] = c.resolveType(t, module); out[i] == nil {
			return nil
		}
	}
	return out
}

// head returns the declaration that a named type denotes from a module, applying qualification and visibility.
func (c *collector) head(n *syntax.Named, module string) *Entry {
	return c.member(n, n.Module, n.Name, module, func(e *Entry) bool { return e.Kind != ConstantKind && e.Kind != FunctionKind }, diagnostics.TypeUnknown, "unknown type "+n.Name)
}

// member returns the declaration that a possibly qualified name denotes from a module, reporting at the node.
func (c *collector) member(n syntax.Node, qualifier, name, module string, fits func(*Entry) bool, unknown, message string) *Entry {
	o := n.Where()
	if qualifier == "" {
		e := c.t.Scope(module, name)
		if e == nil || !fits(e) {
			c.atName(n, o.Start, name, unknown, message)
			return nil
		}
		return e
	}
	if !c.t.modules[qualifier] {
		c.report(o, o.Start, o.Start+len(qualifier), diagnostics.ModuleUnknownModule, "unknown module "+qualifier)
		return nil
	}
	e := c.t.entries[Key{qualifier, name}]
	if e == nil || !fits(e) {
		c.atName(n, o.Start+len(qualifier), name, diagnostics.ModuleUnknownMember, fmt.Sprintf("module %s declares no %s", qualifier, name))
		return nil
	}
	if !Visible(e, module) {
		c.atName(n, o.Start+len(qualifier), name, diagnostics.ModuleUnpublished, fmt.Sprintf("module %s does not publish %s", qualifier, name))
		return nil
	}
	return e
}

// alias returns the canonical target of an alias, rejecting a cycle among aliases and a head of the wrong form.
func (c *collector) alias(e *Entry, ref *syntax.Named) syntax.Type {
	switch c.aliases[e] {
	case done:
		return e.Type
	case failed:
		return nil
	case active:
		members, at := CloseCycle(c.aliasPath, e.Decl, ref)
		for _, m := range members {
			c.aliases[c.t.decls[m]] = failed
		}
		c.at(at, diagnostics.AliasDependencyCycle, "an alias target resolves without revisiting an alias")
		return nil
	}
	c.aliases[e] = active
	var r syntax.Node
	if ref != nil {
		r = ref
	}
	c.aliasPath = append(c.aliasPath, Step{Decl: e.Decl, Ref: r})
	d := e.Decl.(*syntax.Alias)
	target := c.resolveType(d.Target, e.Module)
	c.aliasPath = c.aliasPath[:len(c.aliasPath)-1]
	if c.aliases[e] == failed {
		return nil
	}
	if target == nil {
		c.aliases[e] = failed
		return nil
	}
	if form := c.form(target); form != aliasForm(e.Name) {
		c.atName(d, d.Start, d.Name, diagnostics.AliasFormMismatch, fmt.Sprintf("an alias of %s is written in the form %s", syntax.Print(target), form))
	}
	c.aliases[e] = done
	e.Type = target
	return target
}

// aliasForm returns the lexical form of an alias name: its sigil, or empty for a plain type.
func aliasForm(name string) string {
	switch {
	case strings.HasPrefix(name, "$"):
		return "$T"
	case strings.HasPrefix(name, "@"):
		return "@T"
	case strings.HasPrefix(name, "%"):
		return "%T"
	case strings.HasSuffix(name, "!"):
		return "T!"
	}
	return "T"
}

// form returns the lexical form that a canonical type requires of an alias name.
func (c *collector) form(t syntax.Type) string {
	var n *syntax.Named
	switch t := t.(type) {
	case *syntax.Named:
		n = t
	case *syntax.Array, *syntax.Dict, *syntax.Nullable, *syntax.FuncType, *syntax.Future:
		return "T"
	default:
		panic(fmt.Sprintf("resolve: unexpected type %T", t))
	}
	if e := c.t.Lookup(n.Module, n.Name); e != nil {
		switch e.Kind {
		case EntityKind:
			if e.Identity {
				return "$T#"
			}
			return "$T"
		case ServiceKind:
			return "@T"
		case ProjectionKind:
			return "%T"
		case ErrorKind:
			return "T!"
		}
	}
	return "T"
}

// length returns the literal length of a fixed array, which has an Int value, where a length written as a constant name is replaced by the value of that constant.
func (c *collector) length(x syntax.Expr, module string) (syntax.Expr, bool) {
	switch x := x.(type) {
	case *syntax.Lit:
		if x.Kind != syntax.IntLit {
			return x, true
		}
		if _, err := strconv.ParseInt(x.Canonical(), 10, 64); err != nil {
			c.at(x, diagnostics.ConstantOutOfRange, fmt.Sprintf("the constant %s is out of range for Int", x.Canonical()))
			return nil, false
		}
	case *syntax.Name:
		lit, diags := c.lengths(c.t, x, module)
		c.diags = append(c.diags, diags...)
		return lit, lit != nil
	}
	return x, true
}
