// Package resolve collects the declarations of a source set into one table with their
// kinds, canonical types, visibility, and methods.
package resolve

import (
	"fmt"
	"strings"

	"ritchie.dev/internal/syntax"
)

// Kind is the kind of a declaration.
type Kind uint8

const (
	StructKind Kind = iota
	EnumKind
	ErrorKind
	AliasKind
	SumKind
	DistinctKind
	ConstantKind
	FunctionKind
	MethodKind
	ServiceKind
	EntityKind
	ProjectionKind
	QueryKind
	ContractKind
	BuiltinKind
)

// reach is the rule of the visibility table that a kind follows outside its declaring module.
type reach uint8

const (
	declaringModule reach = iota
	anyModule
	publishedType
	entityReach
	receiverReach
)

var kinds = [...]struct {
	name  string
	reach reach
}{
	StructKind:     {"struct", publishedType},
	EnumKind:       {"enum", publishedType},
	ErrorKind:      {"error type", anyModule},
	AliasKind:      {"alias", declaringModule},
	SumKind:        {"sum type", publishedType},
	DistinctKind:   {"distinct type", publishedType},
	ConstantKind:   {"constant", declaringModule},
	FunctionKind:   {"function", declaringModule},
	MethodKind:     {"method", receiverReach},
	ServiceKind:    {"service", anyModule},
	EntityKind:     {"entity type", entityReach},
	ProjectionKind: {"projection", anyModule},
	QueryKind:      {"query", receiverReach},
	ContractKind:   {"contract", anyModule},
	BuiltinKind:    {"built-in type", anyModule},
}

func (k Kind) String() string { return kinds[k].name }

// Routine is the kind of a routine, which follows from the archetype of its receiver.
type Routine uint8

const (
	NotRoutine Routine = iota
	FreeRoutine
	StructMethod
	EntityMethod
	ServiceMethod
)

// Level says whether a method is called on its type, on a value, or either.
type Level uint8

const (
	TypeLevel Level = iota
	InstanceLevel
	EitherLevel
)

// Key identifies a top-level declaration by its module and name.
type Key struct {
	Module string
	Name   string
}

// Entry is one declaration of the table.
type Entry struct {
	Kind       Kind
	Module     string
	Name       string
	Decl       syntax.Node
	Owner      *Entry
	Routine    Routine
	Level      Level
	Published  bool
	Identity   bool
	Standalone bool
	Type       syntax.Type
	members    map[string]*Entry
}

// Table is the declaration table of a source set.
type Table struct {
	entries  map[Key]*Entry
	builtins map[string]*Entry
	modules  map[string]bool
	graph    map[string][]string
	decls    map[syntax.Node]*Entry
	types    map[syntax.Type]syntax.Type
	c        *collector
}

func newTable() *Table {
	return &Table{
		entries:  map[Key]*Entry{},
		builtins: map[string]*Entry{},
		modules:  map[string]bool{},
		graph:    map[string][]string{},
		decls:    map[syntax.Node]*Entry{},
		types:    map[syntax.Type]syntax.Type{},
	}
}

// Lookup returns the top-level declaration of a module by name, where the module of the built-in types is empty.
func (t *Table) Lookup(module, name string) *Entry {
	if e := t.entries[Key{module, name}]; e != nil {
		return e
	}
	if module == "" {
		return t.builtins[name]
	}
	return nil
}

// Errors returns the error types that the program declares, without the built-in catalog errors, in no particular order.
func (t *Table) Errors() []Key {
	var out []Key
	for k, e := range t.entries {
		if e.Kind == ErrorKind && e.Decl != nil {
			out = append(out, k)
		}
	}
	return out
}

// Of returns the entry of a declaration node.
func (t *Table) Of(decl syntax.Node) *Entry { return t.decls[decl] }

// Type returns the canonical form of a type written in a declaration, which is resolved on demand while the table is collected, because the evaluation of a constant that a fixed array type writes as its length reads the declared types of the names that it uses.
func (t *Table) Type(written syntax.Type) syntax.Type {
	r, seen := t.types[written]
	if !seen && written != nil && t.c.collecting && written.Where().File != nil {
		return t.c.resolveType(written, moduleOf(written))
	}
	return r
}

// Fix records the canonical type of a constant declaration whose dynamic array lengths the value of the constant fixes.
func (t *Table) Fix(written, canonical syntax.Type) { t.types[written] = canonical }

// Dependencies returns the modules that a module refers to.
func (t *Table) Dependencies(module string) []string { return t.graph[module] }

// Method returns the method of a canonical type by name.
func (t *Table) Method(ty syntax.Type, name string) *Entry {
	switch ty := ty.(type) {
	case *syntax.Named:
		if owner := t.Lookup(ty.Module, ty.Name); owner != nil {
			return owner.members[name]
		}
	case *syntax.Array:
		if owner := t.builtins[syntax.Print(ty)]; owner != nil {
			return owner.members[name]
		}
	case *syntax.Dict:
		if owner := t.builtins["[K: V]"]; owner != nil {
			return owner.members[name]
		}
	case *syntax.Nullable, *syntax.FuncType, *syntax.Future, *syntax.Bad:
	default:
		panic(fmt.Sprintf("resolve: unexpected type %T", ty))
	}
	return nil
}

// Scope returns the declaration that an unqualified name denotes in a module: the module's own, or a built-in.
func (t *Table) Scope(module, name string) *Entry {
	if e := t.entries[Key{module, name}]; e != nil {
		return e
	}
	return t.builtins[name]
}

// Module reports whether a module name is one of the source set or a standard module.
func (t *Table) Module(name string) bool { return t.modules[name] }

// FieldsOf returns the fields of a struct, error, or entity declaration, and nil for any other declaration.
func FieldsOf(d syntax.Node) []*syntax.Field {
	switch d := d.(type) {
	case *syntax.Struct:
		return d.Fields
	case *syntax.Error:
		return d.Fields
	case *syntax.Entity:
		return d.Fields
	}
	return nil
}

// Visible reports whether a declaration may be named from a module.
func Visible(e *Entry, from string) bool {
	return e.Module == from || public(e)
}

// public reports whether a declaration may be named from every module.
func public(e *Entry) bool {
	switch kinds[e.Kind].reach {
	case anyModule:
		return true
	case publishedType:
		return e.Published
	case entityReach:
		return e.Identity
	case receiverReach:
		return e.Owner != nil && !strings.HasPrefix(e.Name, "_") && public(e.Owner)
	}
	return false
}
