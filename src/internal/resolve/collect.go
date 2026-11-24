// This file builds the declaration table of a source set and checks the declaration
// rules that need no routine bodies.
package resolve

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
)

// state is the progress of a declaration through an evaluation that rejects cycles.
type state uint8

const (
	unvisited state = iota
	active
	done
	failed
)

// Step is one declaration under evaluation with the reference that entered it.
type Step struct {
	Decl syntax.Node
	Ref  syntax.Node
}

// Length evaluates the constant that a fixed array type writes as its length in a module, and returns the literal of its value, or the faults of its evaluation.
type Length func(t *Table, x *syntax.Name, module string) (syntax.Expr, []diagnostics.Record)

// member is one field, enum member, or method in the member namespace of a type.
type member struct {
	name   string
	node   syntax.Node
	from   int
	method *syntax.Func
}

// collector holds the table under construction and its diagnostics, where resolving holds the written types whose resolution is in progress and collecting is set until every declaration rule is checked.
type collector struct {
	t          *Table
	diags      []diagnostics.Record
	members    map[*Entry][]member
	aliases    map[*Entry]state
	aliasPath  []Step
	dicts      []*syntax.Dict
	lengths    Length
	resolving  map[syntax.Type]bool
	collecting bool
}

// Collect builds the declaration table of the parsed files of every module in a source set, where length evaluates each constant that a fixed array type writes as its length.
func Collect(files []*syntax.File, length Length) (*Table, []diagnostics.Record) {
	c := &collector{
		t:          newTable(),
		members:    map[*Entry][]member{},
		aliases:    map[*Entry]state{},
		lengths:    length,
		resolving:  map[syntax.Type]bool{},
		collecting: true,
	}
	c.t.c = c
	c.t.addStandard()
	for _, f := range files {
		c.t.modules[f.File.Module] = true
	}
	for _, f := range files {
		for _, d := range f.Decls {
			c.declare(d)
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			c.publish(d)
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			c.attach(d)
		}
	}
	c.settleMembers()
	for _, f := range files {
		for _, d := range f.Decls {
			c.resolveDecl(d)
		}
	}
	c.checkEntry(files)
	c.checkContainment(files)
	c.checkBoundaries(files)
	c.collecting = false
	c.checkKeys()
	c.buildGraph(files)
	diagnostics.Sort(c.diags)
	return c.t, c.diags
}

func moduleOf(n syntax.Node) string { return n.Where().File.Module }

func (c *collector) report(o syntax.Origin, start, end int, reason, message string) {
	c.diags = append(c.diags, o.File.Record(reason, start, end, message))
}

// at reports a diagnostic over the whole origin of a node.
func (c *collector) at(n syntax.Node, reason, message string) {
	o := n.Where()
	c.report(o, o.Start, o.End, reason, message)
}

// atName reports a diagnostic at the first occurrence of a name in a node's origin at or after an offset.
func (c *collector) atName(n syntax.Node, from int, name, reason, message string) {
	o := n.Where()
	start, end := o.NameSpan(from, name)
	c.report(o, start, end, reason, message)
}

// declName returns the declared name of a top-level declaration and the offset from which it is written.
func declName(d syntax.Decl) (string, int) {
	from := d.Where().Start
	switch d := d.(type) {
	case *syntax.Struct:
		return d.Name, from
	case *syntax.Enum:
		return d.Name, from
	case *syntax.Error:
		return d.Name, from
	case *syntax.Entity:
		return d.Name, from
	case *syntax.Service:
		return d.Name, from
	case *syntax.Projection:
		return d.Name, from
	case *syntax.Alias:
		return d.Name, from
	case *syntax.Sum:
		return d.Name, from
	case *syntax.Distinct:
		return d.Name, from
	case *syntax.Contract:
		return d.Name, from
	case *syntax.Const:
		return d.Name, d.Type.Where().End
	case *syntax.Func:
		if d.Receiver != nil {
			return d.Name, d.Receiver.Where().End
		}
		return d.Name, from
	case *syntax.Query:
		if d.Receiver != nil {
			return d.Name, d.Receiver.Where().End
		}
		return d.Name, from
	}
	panic(fmt.Sprintf("resolve: unexpected declaration %T", d))
}

// declare enters a top-level declaration other than a method into the table.
func (c *collector) declare(d syntax.Decl) {
	switch d := d.(type) {
	case *syntax.Struct:
		c.add(d, StructKind)
	case *syntax.Enum:
		c.add(d, EnumKind)
	case *syntax.Error:
		if r := c.t.builtins[d.Name]; r != nil && r.Kind == ErrorKind {
			c.atName(d, d.Start, d.Name, diagnostics.NameReservedName, fmt.Sprintf("the name %s is reserved by the built-in error catalog", d.Name))
			return
		}
		c.add(d, ErrorKind)
	case *syntax.Entity:
		if e := c.add(d, EntityKind); e != nil {
			c.t.entries[Key{e.Module, e.Name + "#"}] = &Entry{Kind: EntityKind, Module: e.Module, Name: e.Name + "#", Decl: d, Identity: true}
		}
	case *syntax.Service:
		c.add(d, ServiceKind)
	case *syntax.Projection:
		c.add(d, ProjectionKind)
	case *syntax.Alias:
		if strings.HasSuffix(d.Name, "#") {
			c.atName(d, d.Start, d.Name, diagnostics.UnsupportedSyntax, "an alias of an entity identity is not supported")
			return
		}
		c.add(d, AliasKind)
	case *syntax.Sum:
		c.add(d, SumKind)
	case *syntax.Distinct:
		c.add(d, DistinctKind)
	case *syntax.Const:
		c.add(d, ConstantKind)
	case *syntax.Contract:
		c.add(d, ContractKind)
	case *syntax.Func:
		if d.Receiver == nil {
			if e := c.add(d, FunctionKind); e != nil {
				e.Routine = FreeRoutine
			}
		}
	case *syntax.Query:
		if d.Receiver == nil {
			c.add(d, QueryKind)
		}
	case *syntax.Conformance, *syntax.Bad:
	default:
		panic(fmt.Sprintf("resolve: unexpected declaration %T", d))
	}
}

// add enters a declaration under its module and name, or reports a duplicate.
func (c *collector) add(d syntax.Decl, kind Kind) *Entry {
	name, from := declName(d)
	key := Key{moduleOf(d), name}
	if c.t.entries[key] != nil {
		c.atName(d, from, name, diagnostics.ModuleDuplicateDecl, fmt.Sprintf("the name %s is declared more than once in the module", name))
		return nil
	}
	e := &Entry{Kind: kind, Module: key.Module, Name: name, Decl: d}
	c.t.entries[key] = e
	c.t.decls[d] = e
	return e
}

// publish marks the receiver of a published method as published, before any reference is resolved.
func (c *collector) publish(d syntax.Decl) {
	fn, ok := d.(*syntax.Func)
	if !ok || strings.HasPrefix(fn.Name, "_") {
		return
	}
	if r, ok := fn.Receiver.(*syntax.Named); ok && (r.Module == "" || r.Module == moduleOf(fn)) {
		if e := c.t.entries[Key{moduleOf(fn), r.Name}]; e != nil {
			e.Published = true
		}
	}
}

// attach gathers the member namespace of each type: its fields and enum members, and the methods declared on it.
func (c *collector) attach(d syntax.Decl) {
	switch d := d.(type) {
	case *syntax.Struct:
		c.fieldMembers(d, d.Fields)
	case *syntax.Error:
		c.fieldMembers(d, d.Fields)
	case *syntax.Entity:
		c.fieldMembers(d, d.Fields)
	case *syntax.Projection:
		c.fieldMembers(d, d.Columns)
	case *syntax.Enum:
		if e := c.t.decls[d]; e != nil {
			for _, m := range d.Members {
				c.members[e] = append(c.members[e], member{name: m.Name, node: m, from: m.Start})
			}
		}
	case *syntax.Func:
		if d.Receiver == nil {
			return
		}
		if owner := c.receiver(d.Receiver, moduleOf(d), StructKind, EnumKind, DistinctKind, SumKind, EntityKind, ServiceKind); owner != nil {
			c.members[owner] = append(c.members[owner], member{name: d.Name, node: d, from: d.Receiver.Where().End, method: d})
		}
	case *syntax.Query:
		if d.Receiver == nil {
			return
		}
		if owner := c.receiver(d.Receiver, moduleOf(d), ProjectionKind); owner != nil {
			c.members[owner] = append(c.members[owner], member{name: d.Name, node: d, from: d.Receiver.Where().End})
		}
	case *syntax.Service, *syntax.Alias, *syntax.Sum, *syntax.Distinct, *syntax.Const, *syntax.Contract, *syntax.Conformance, *syntax.Bad:
	default:
		panic(fmt.Sprintf("resolve: unexpected declaration %T", d))
	}
}

func (c *collector) fieldMembers(d syntax.Decl, fields []*syntax.Field) {
	e := c.t.decls[d]
	if e == nil {
		return
	}
	for _, f := range fields {
		c.members[e] = append(c.members[e], member{name: f.Name, node: f, from: f.Type.Where().End})
	}
}

// receiver resolves the receiver of a method to a type of one of the given kinds that the method's module declares.
func (c *collector) receiver(n syntax.Type, module string, allowed ...Kind) *Entry {
	named, ok := n.(*syntax.Named)
	if !ok {
		return nil
	}
	if named.Module != "" && named.Module != module && c.t.modules[named.Module] {
		c.at(n, diagnostics.MethodReceiverNotOwned, "a method is declared only in the module that declares its receiver type")
		return nil
	}
	e := c.head(named, module)
	for e != nil && e.Kind == AliasKind {
		target, ok := c.alias(e, named).(*syntax.Named)
		if !ok {
			e = nil
			break
		}
		e = c.t.Lookup(target.Module, target.Name)
	}
	if e == nil {
		return nil
	}
	if e.Module != module || e.Decl == nil || !slices.Contains(allowed, e.Kind) {
		c.at(n, diagnostics.MethodReceiverNotOwned, "a method is declared only in the module that declares its receiver type")
		return nil
	}
	return e
}

// settleMembers rejects duplicate members and enters each method under its receiver.
func (c *collector) settleMembers() {
	for owner, ms := range c.members {
		slices.SortStableFunc(ms, func(a, b member) int {
			oa, ob := a.node.Where(), b.node.Where()
			return cmp.Or(cmp.Compare(oa.File.Index, ob.File.Index), cmp.Compare(oa.Start, ob.Start))
		})
		owner.members = map[string]*Entry{}
		seen := map[string]member{}
		for _, m := range ms {
			if prior, dup := seen[m.name]; dup {
				reason := diagnostics.MemberDuplicateDeclaration
				if prior.method != nil && m.method != nil {
					reason = diagnostics.MethodDuplicateDeclaration
				}
				c.atName(m.node, m.from, m.name, reason, fmt.Sprintf("the member %s is declared more than once on %s", m.name, owner.Name))
				continue
			}
			seen[m.name] = m
			switch n := m.node.(type) {
			case *syntax.Func:
				e := &Entry{Kind: MethodKind, Module: owner.Module, Name: n.Name, Decl: n, Owner: owner,
					Routine: routineOf(owner.Kind), Level: levelOf(n)}
				owner.members[n.Name] = e
				c.t.decls[n] = e
			case *syntax.Query:
				e := &Entry{Kind: QueryKind, Module: owner.Module, Name: n.Name, Decl: n, Owner: owner}
				owner.members[n.Name] = e
				c.t.decls[n] = e
			}
		}
	}
}

// routineOf returns the routine kind of a method on a receiver of a kind.
func routineOf(receiver Kind) Routine {
	switch receiver {
	case EntityKind:
		return EntityMethod
	case ServiceKind:
		return ServiceMethod
	}
	return StructMethod
}

// levelOf returns the level of a method from whether its body uses self.
func levelOf(f *syntax.Func) Level {
	level := TypeLevel
	if f.Body != nil {
		syntax.Inspect(f.Body, func(n syntax.Node) bool {
			if _, is := n.(*syntax.Self); is {
				level = InstanceLevel
			}
			return level == TypeLevel
		})
	}
	return level
}

// resolveDecl resolves every written type of a declaration and checks its parameter list.
func (c *collector) resolveDecl(d syntax.Decl) {
	m := moduleOf(d)
	switch d := d.(type) {
	case *syntax.Struct:
		c.resolveFields(d.Fields, m)
	case *syntax.Error:
		c.resolveFields(d.Fields, m)
		c.errorFields(d.Fields)
	case *syntax.Entity:
		c.resolveFields(d.Fields, m)
	case *syntax.Projection:
		c.resolveFields(d.Columns, m)
		for _, s := range d.From {
			c.resolveType(s.Type, m)
		}
	case *syntax.Alias:
		if e := c.t.decls[d]; e != nil {
			c.alias(e, nil)
		}
	case *syntax.Sum:
		for _, t := range d.Members {
			c.resolveType(t, m)
		}
	case *syntax.Distinct:
		c.resolveType(d.Repr, m)
	case *syntax.Const:
		c.resolveType(d.Type, m)
	case *syntax.Func:
		c.resolveParams(d.Params, m)
		for _, t := range d.Results {
			c.resolveType(t, m)
		}
		for _, t := range d.Raises {
			c.resolveType(t, m)
		}
	case *syntax.Query:
		c.resolveParams(d.Params, m)
		if d.Result != nil {
			c.resolveType(d.Result, m)
		}
	case *syntax.Enum, *syntax.Service, *syntax.Contract, *syntax.Conformance, *syntax.Bad:
	default:
		panic(fmt.Sprintf("resolve: unexpected declaration %T", d))
	}
}

func (c *collector) resolveFields(fields []*syntax.Field, module string) {
	for _, f := range fields {
		c.resolveType(f.Type, module)
	}
}

// errorFields rejects the reserved member name and the field types that an error value cannot carry.
func (c *collector) errorFields(fields []*syntax.Field) {
	for _, f := range fields {
		if f.Name == "history" {
			c.atName(f, f.Type.Where().End, f.Name, diagnostics.NameReservedName, "the member name history is reserved on every error")
		}
		if bad, what := c.uncarried(c.t.types[f.Type]); bad != nil {
			c.at(bad, diagnostics.TypeInvalidField, "an error field cannot hold "+what)
		}
	}
}

// uncarried returns the part of a canonical type that an error field cannot hold, with its category.
func (c *collector) uncarried(t syntax.Type) (syntax.Type, string) {
	switch t := t.(type) {
	case *syntax.Named:
		if strings.HasSuffix(t.Name, "!") {
			return t, "an error"
		}
		if e := c.t.Lookup(t.Module, t.Name); e != nil && e.Kind == ServiceKind {
			return t, "a service"
		} else if e != nil && e.Kind == EntityKind && !e.Identity {
			return t, "an entity"
		}
		for _, a := range t.Args {
			if bad, what := c.uncarried(a); bad != nil {
				return bad, what
			}
		}
	case *syntax.Array:
		return c.uncarried(t.Elem)
	case *syntax.Nullable:
		return c.uncarried(t.Elem)
	case *syntax.Dict:
		if bad, what := c.uncarried(t.Key); bad != nil {
			return bad, what
		}
		return c.uncarried(t.Value)
	case *syntax.FuncType:
		return t, "a function"
	case *syntax.Future:
		return t, "a future"
	case nil:
	default:
		panic(fmt.Sprintf("resolve: unexpected type %T", t))
	}
	return nil, ""
}

// resolveParams resolves the parameter types and checks that names are distinct and defaults form a suffix.
func (c *collector) resolveParams(params []*syntax.Param, module string) {
	seen := map[string]bool{}
	defaulted, ordered := false, true
	for _, p := range params {
		c.resolveType(p.Type, module)
		from := p.Type.Where().End
		if seen[p.Name] {
			c.atName(p, from, p.Name, diagnostics.NameDuplicateName, fmt.Sprintf("parameter %s is declared more than once", p.Name))
		}
		seen[p.Name] = true
		if p.Default != nil {
			defaulted = true
		} else if defaulted && ordered {
			ordered = false
			c.atName(p, from, p.Name, diagnostics.ParameterDefaultOrder, "parameters with defaults follow every parameter without one")
		}
	}
}

// checkEntry checks the main() rule: no parameters and no return clause, and at most one root module declaring it.
func (c *collector) checkEntry(files []*syntax.File) {
	var mains []*syntax.Func
	modules := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*syntax.Func)
			if !ok || fn.Receiver != nil || fn.Name != "main" || c.t.decls[fn] == nil {
				continue
			}
			if len(fn.Params) > 0 || len(fn.Results) > 0 || fn.ResultNone || len(fn.Raises) > 0 {
				c.atName(fn, fn.Start, fn.Name, diagnostics.MainInvalidSignature, "main has no parameters and no return clause")
			}
			mains = append(mains, fn)
			modules[moduleOf(fn)] = true
		}
	}
	if len(modules) < 2 {
		return
	}
	for _, fn := range mains {
		c.atName(fn, fn.Start, fn.Name, diagnostics.MainMultipleEntry, "more than one root module declares main")
	}
}

// checkContainment rejects a struct or error type that contains itself by value.
func (c *collector) checkContainment(files []*syntax.File) {
	states := map[*Entry]state{}
	var path []Step
	var visit func(e *Entry)
	edge := func(ref syntax.Type) {
		if e := c.byValue(c.t.types[ref]); e != nil {
			switch states[e] {
			case unvisited:
				path = append(path, Step{Decl: e.Decl, Ref: ref})
				visit(e)
				path = path[:len(path)-1]
			case active:
				members, at := CloseCycle(path, e.Decl, ref)
				for _, m := range members {
					states[c.t.decls[m]] = failed
				}
				c.at(at, diagnostics.TypeDependencyCycle, "a type contains itself by value")
			}
		}
	}
	visit = func(e *Entry) {
		states[e] = active
		for _, f := range FieldsOf(e.Decl) {
			if states[e] != active {
				return
			}
			edge(f.Type)
		}
		if d, ok := e.Decl.(*syntax.Distinct); ok {
			edge(d.Repr)
		}
		if states[e] == active {
			states[e] = done
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			if e := c.t.decls[d]; e != nil && states[e] == unvisited && (e.Kind == StructKind || e.Kind == ErrorKind || e.Kind == DistinctKind) {
				path = []Step{{Decl: d}}
				visit(e)
			}
		}
	}
}

// byValue returns the declared type that a value of a canonical type holds inline, if any.
func (c *collector) byValue(t syntax.Type) *Entry {
	switch t := t.(type) {
	case *syntax.Named:
		if e := c.t.Lookup(t.Module, t.Name); e != nil && e.Decl != nil && (e.Kind == StructKind || e.Kind == ErrorKind || e.Kind == DistinctKind) {
			return e
		}
	case *syntax.Array:
		if t.Length != nil {
			return c.byValue(t.Elem)
		}
	case *syntax.Dict, *syntax.Nullable, *syntax.FuncType, *syntax.Future, nil:
	default:
		panic(fmt.Sprintf("resolve: unexpected type %T", t))
	}
	return nil
}

// CloseCycle returns the members of the cycle that ref closes back to decl and the reference that leaves the member declared first.
func CloseCycle(path []Step, decl, ref syntax.Node) ([]syntax.Node, syntax.Node) {
	i := len(path) - 1
	for path[i].Decl != decl {
		i--
	}
	cycle := path[i:]
	members := make([]syntax.Node, len(cycle))
	first := 0
	for k, s := range cycle {
		members[k] = s.Decl
		if before(s.Decl, cycle[first].Decl) {
			first = k
		}
	}
	if first+1 < len(cycle) {
		return members, cycle[first+1].Ref
	}
	return members, ref
}

func before(a, b syntax.Node) bool {
	oa, ob := a.Where(), b.Where()
	return oa.File.Index < ob.File.Index || oa.File.Index == ob.File.Index && oa.Start < ob.Start
}

// checkBoundaries checks that every parameter and result type of a service method crosses a service boundary.
func (c *collector) checkBoundaries(files []*syntax.File) {
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*syntax.Func)
			e := c.t.decls[d]
			if !ok || e == nil || e.Routine != ServiceMethod {
				continue
			}
			for _, t := range append(paramTypes(fn.Params), fn.Results...) {
				if r := c.t.types[t]; r != nil && !c.crosses(r, map[*Entry]bool{}) {
					c.at(t, diagnostics.ServiceBoundaryType, "a service method takes and returns only public values")
				}
			}
		}
	}
}

func paramTypes(params []*syntax.Param) []syntax.Type {
	types := make([]syntax.Type, len(params))
	for i, p := range params {
		types[i] = p.Type
	}
	return types
}

// crosses reports whether values of a canonical type may cross a service boundary: the values, structs, enums, collections of these, errors, and entity identities of the reference's boundary rule, with a distinct type taking its representation's form, and not a sum type, which neither the rule nor the HTTP/JSON adapter gives a form.
func (c *collector) crosses(t syntax.Type, seen map[*Entry]bool) bool {
	switch t := t.(type) {
	case *syntax.Named:
		e := c.t.Lookup(t.Module, t.Name)
		if e == nil || seen[e] {
			return e != nil
		}
		seen[e] = true
		switch e.Kind {
		case BuiltinKind, ErrorKind, EnumKind:
			return public(e)
		case EntityKind:
			return e.Identity
		case StructKind:
			if e.Decl == nil || !public(e) {
				return false
			}
			for _, f := range FieldsOf(e.Decl) {
				if r := c.t.types[f.Type]; r != nil && !c.crosses(r, seen) {
					return false
				}
			}
			return true
		case DistinctKind:
			r := c.t.types[e.Decl.(*syntax.Distinct).Repr]
			return public(e) && (r == nil || c.crosses(r, seen))
		case SumKind:
			return false
		}
		return false
	case *syntax.Array:
		return c.crosses(t.Elem, seen)
	case *syntax.Dict:
		return c.crosses(t.Key, seen) && c.crosses(t.Value, seen)
	case *syntax.Nullable:
		return c.crosses(t.Elem, seen)
	case *syntax.FuncType, *syntax.Future:
		return false
	}
	panic(fmt.Sprintf("resolve: unexpected type %T", t))
}

// buildGraph records the module dependency graph from every module qualifier and rejects its cycles.
func (c *collector) buildGraph(files []*syntax.File) {
	first := map[[2]string]syntax.Node{}
	for _, f := range files {
		from := f.File.Module
		syntax.Inspect(f, func(n syntax.Node) bool {
			to := ""
			switch n := n.(type) {
			case *syntax.Named:
				to = n.Module
			case *syntax.Name:
				to = n.Module
			}
			if to == "" || to == from || !c.t.modules[to] || builtin.Module(to) {
				return true
			}
			if _, seen := first[[2]string{from, to}]; !seen {
				first[[2]string{from, to}] = n
				c.t.graph[from] = append(c.t.graph[from], to)
			}
			return true
		})
	}
	for m := range c.t.graph {
		slices.Sort(c.t.graph[m])
	}
	for _, component := range c.components() {
		var ref syntax.Node
		for edge, n := range first {
			if slices.Contains(component, edge[0]) && slices.Contains(component, edge[1]) && (ref == nil || before(n, ref)) {
				ref = n
			}
		}
		o := ref.Where()
		rec := o.File.Record(diagnostics.ModuleDependencyCycle, o.Start, o.End,
			fmt.Sprintf("the modules %s depend on each other in a cycle", strings.Join(component, ", ")))
		rec.CycleModules = component
		c.diags = append(c.diags, rec)
	}
}

// components returns the strongly connected components of the module graph with more than one module, each sorted.
func (c *collector) components() [][]string {
	var modules []string
	for m := range c.t.modules {
		modules = append(modules, m)
	}
	slices.Sort(modules)
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	var connect func(v string)
	connect = func(v string) {
		index[v], low[v] = len(index), len(index)
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range c.t.graph[v] {
			if _, seen := index[w]; !seen {
				connect(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		var component []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			component = append(component, w)
			if w == v {
				break
			}
		}
		if len(component) > 1 {
			slices.Sort(component)
			out = append(out, component)
		}
	}
	for _, m := range modules {
		if _, seen := index[m]; !seen {
			connect(m)
		}
	}
	slices.SortFunc(out, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return out
}
