// Package check types every expression of a source set before lowering, builds the
// call graph and raise sets, and performs the kernel checks that Ritchie makes and Go
// would not.
package check

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// Program is a checked source set with the types, callees, and raise sets attached to its nodes, and, for a kernel program, the table of operand modes and, under the schedule with reads in flight, the table of the iterator loops whose reads run ahead.
type Program struct {
	Files   []*syntax.File
	Table   *resolve.Table
	Graph   *CallGraph
	Marks   Marks
	Options Options
	Ahead   map[*syntax.IterFor]*Ahead
	Modes   *Modes

	types       map[syntax.Expr]syntax.Type
	results     map[syntax.Expr][]syntax.Type
	callees     map[syntax.Expr]*callee
	constant    map[syntax.Expr]syntax.Expr
	collections map[syntax.Expr]*Constant
	values      map[syntax.Node]*Constant
	defaults    map[syntax.Node]syntax.Expr
	chains      map[*syntax.Chain][]*Operation
	locals      map[syntax.Node]syntax.Type
	loopVars    map[*syntax.IterFor][]syntax.Type
	narrowed    map[syntax.Expr]bool
	bindings    map[syntax.Expr]*binding
	stored      map[syntax.Expr]*binding
	noop        map[*syntax.Arm]map[string]bool
	recovered   map[*syntax.Handled][]syntax.Type
	raises      map[Node]RaiseSet
	sites       map[syntax.Expr]RaiseSet
	escapes     map[syntax.Stmt]RaiseSet
	cyclic      map[*resolve.Entry]bool
}

// Recovered returns the result types of the one statement that a handler recovers, or nil for a handler on a block or a loop, or on a statement without results.
func (p *Program) Recovered(s *syntax.Handled) []syntax.Type { return p.recovered[s] }

// TypeOf returns the type attached to a single-valued expression, or nil.
func (p *Program) TypeOf(x syntax.Expr) syntax.Type { return p.types[x] }

// Results returns the result types attached to a call expression of any arity.
func (p *Program) Results(x syntax.Expr) []syntax.Type { return p.results[x] }

// Constant returns the literal of an expression that is a constant expression with a scalar value, or nil.
func (p *Program) Constant(x syntax.Expr) syntax.Expr { return p.constant[x] }

// Collection reports whether an expression is a constant expression whose value is a collection, which the kernel holds as the name of its constant, as its literal, or as its operation.
func (p *Program) Collection(x syntax.Expr) bool { return p.collections[x] != nil }

// Retained reports whether an expression is a constant operation whose value is a collection, which the kernel keeps and which raises nothing, because the compiler has evaluated it.
func (p *Program) Retained(x syntax.Expr) bool {
	if p.collections[x] == nil {
		return false
	}
	switch x.(type) {
	case *syntax.Name, *syntax.Lit, *syntax.ArrayLit, *syntax.DictLit:
		return false
	}
	return true
}

// ReadsConstant reports whether an expression is a path of field and index selections that starts at a constant whose value is a collection, which no statement writes.
func (p *Program) ReadsConstant(x syntax.Expr) bool {
	for {
		switch y := x.(type) {
		case *syntax.Name:
			return p.collections[y] != nil
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			x = y.X
		default:
			return false
		}
	}
}

// Value returns the evaluated value of a module constant, of a local constant declaration, or of a target of a declaration of several local constants, or nil for a constant whose evaluation failed.
func (p *Program) Value(d syntax.Node) *Constant { return p.values[d] }

// Default returns the literal of the evaluated default of a parameter or of a field.
func (p *Program) Default(n syntax.Node) syntax.Expr { return p.defaults[n] }

// LocalType returns the canonical type of a variable target, a parameter, or a loop variable declared in a body.
func (p *Program) LocalType(n syntax.Node) syntax.Type { return p.locals[n] }

// LoopVarTypes returns the canonical type of each variable of an iterator loop, in declaration order.
func (p *Program) LoopVarTypes(s *syntax.IterFor) []syntax.Type { return p.loopVars[s] }

// Narrowed reports whether a read of a nullable path was narrowed to its value type by a presence test on its path.
func (p *Program) Narrowed(x syntax.Expr) bool { return p.narrowed[x] }

// Raises returns the raise set of a routine or closure.
func (p *Program) Raises(n Node) RaiseSet { return p.raises[n] }

// Site returns the raise set of a call expression after inference.
func (p *Program) Site(x syntax.Expr) RaiseSet { return p.sites[x] }

// Escapes returns the errors that can leave a statement unselected by its own arms, which reach an enclosing handler or the caller of the routine.
func (p *Program) Escapes(s syntax.Stmt) RaiseSet { return p.escapes[s] }

// Local returns the declaration of the local that a plain name refers to at a read or as the target of a store: a variable target, a parameter, an arm, a local constant, or an iterator loop, or nil for any other name.
func (p *Program) Local(x syntax.Expr) syntax.Node {
	if b := p.bindings[x]; b != nil {
		return b.decl
	}
	if b := p.stored[x]; b != nil {
		return b.decl
	}
	return nil
}

// Callee is what a call reaches.
type Callee = callee

// CalleeOf returns the callee of a call expression.
func (p *Program) CalleeOf(x syntax.Expr) *Callee { return p.callees[x] }

// Operation returns the resolved form of a built-in call, or nil for any other expression.
func (p *Program) Operation(x syntax.Expr) *Operation {
	if k := p.callees[x]; k != nil {
		return k.op
	}
	return nil
}

// ChangesReceiver reports whether a method call can change the place of its receiver: a call of an instance method of a struct, or of a built-in method that changes its receiver.
func (p *Program) ChangesReceiver(x *syntax.MethodCall) bool {
	k := p.callees[x]
	switch {
	case k == nil:
		return false
	case k.op != nil:
		return k.op.Row.Ref
	}
	return k.kind == syntax.StructMethod
}

// PlaceSelectors returns the index and key selectors of the receiver path of a declared method call, innermost first, when the path selects an element or entry whose resolution belongs to the call, or nil.
func (p *Program) PlaceSelectors(x *syntax.MethodCall) []*syntax.IndexRead {
	k := p.callees[x]
	if k == nil || k.fn == nil || k.entry == nil || k.entry.Level == resolve.TypeLevel {
		return nil
	}
	var out []*syntax.IndexRead
	for r := x.Receiver; ; {
		switch y := r.(type) {
		case *syntax.FieldRead:
			r = y.X
		case *syntax.IndexRead:
			out = append(out, y)
			r = y.X
		case *syntax.Name, *syntax.Self:
			slices.Reverse(out)
			return out
		default:
			return nil
		}
	}
}

// Options selects the rules that a compile command relaxes and holds the declarations of a fixture: the service methods that it declares mutating, each written as its service name, a dot, and its method name, and the services that it declares as entity-type targets, each written as its name.
type Options struct {
	Debug    bool
	Mutating []string
	Entities []string
}

// progress is the state of a declaration-level constant expression through its one evaluation, which rejects cycles.
type progress uint8

const (
	unvisited progress = iota
	active
	done
	failed
)

// checker holds the state of one check of a source set, where settled, path, and held are the progress, the chain, and the values of the demands for declaration-level constant expressions, evaluated remembers each evaluation of an expression of a body that gave a value or reported a fault, and defect is the first internal error of the constant evaluator.
type checker struct {
	p          *Program
	kernel     bool
	debug      bool
	settled    map[syntax.Node]progress
	path       []resolve.Step
	held       map[syntax.Node]value
	evaluated  map[syntax.Expr]evaluated
	defect     error
	readable   []*binding
	diags      []diagnostics.Record
	faults     []diagnostics.Record
	nodes      []Node
	edges      map[Node][]Edge
	live       map[origin]bool
	seen       map[origin]bool
	deadAt     map[origin]*syntax.Arm
	handlerArm map[*syntax.Arm]bool
	unselected int
	domain     map[string]bool
	deferred   map[syntax.Expr]deferredValue
}

// deferredValue is the scope and routine of a contextual value of a block or loop handler arm, which is typed against each statement the arm recovers once raise sets are known.
type deferredValue struct {
	sc *scope
	r  *routine
}

// origin keys a source construct by its file and span.
type origin struct {
	file  string
	index int
	start int
	end   int
}

func keyOf(o syntax.Origin) origin {
	return origin{file: o.File.Path, index: o.File.Index, start: o.Start, end: o.End}
}

// Check collects the declarations of a source set, types it in reporting mode, and performs every check, returning the program with its diagnostics, where the diagnostics of the collection end the check, and the internal error of a defect of the constant evaluator.
func Check(files []*syntax.File, opts Options) (*Program, []diagnostics.Record, error) {
	c := newChecker(files, false)
	c.debug = opts.Debug
	c.p.Options = opts
	if diags := c.collect(); len(diags) > 0 {
		return c.p, diags, nil
	}
	c.run()
	if len(c.faults) == 0 && len(c.diags) == 0 {
		c.unreadLocals()
	}
	diags := append(c.faults, c.diags...)
	diagnostics.Sort(diags)
	return c.p, diags, c.defect
}

// collect builds the declaration table of the source set, where the checker evaluates each constant that a fixed array type writes as its length.
func (c *checker) collect() []diagnostics.Record {
	t, diags := resolve.Collect(c.p.Files, c.length)
	c.p.Table = t
	return diags
}

// unreadLocals reports each declared local variable and optional arm binding that no statement reads in a program without another fault, as Go rejects it, where loop variables, parameters, and the binding a typed match arm requires may stay unread.
func (c *checker) unreadLocals() {
	for _, b := range c.readable {
		if !b.read {
			c.faultName(b.decl, b.decl.Where().Start, b.name, diagnostics.VariableUnread, diagnostics.Code(b.name)+" is declared and not read")
		}
	}
}

// mustRead records a variable or an optional arm binding that a statement must read.
func (c *checker) mustRead(b *binding) *binding {
	c.readable = append(c.readable, b)
	return b
}

// CheckKernel collects the declarations of a kernel program and types it in silent mode under the declarations of a fixture, where a fault of the collection or of the typing is an internal error, and performs the kernel checks.
func CheckKernel(files []*syntax.File, opts Options) (*Program, []diagnostics.Record, error) {
	c := newChecker(files, true)
	c.p.Options = opts
	if diags := c.collect(); len(diags) > 0 {
		return c.p, nil, internal("the kernel program does not collect", diags)
	}
	c.run()
	if c.defect != nil {
		return c.p, nil, c.defect
	}
	if len(c.faults) > 0 {
		diagnostics.Sort(c.faults)
		return c.p, nil, internal("the kernel program does not type", c.faults)
	}
	diagnostics.Sort(c.diags)
	return c.p, c.diags, nil
}

// internal is the error of a kernel program that a stage of the check rejects.
func internal(what string, diags []diagnostics.Record) error {
	var parts []string
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("%s at %s:%d:%d: %s", d.Reason, d.Path, d.Span.Start.Line, d.Span.Start.Column, d.Message))
	}
	return fmt.Errorf("internal error: %s: %s", what, strings.Join(parts, "; "))
}

func newChecker(files []*syntax.File, kernel bool) *checker {
	return &checker{
		p: &Program{
			Files:       files,
			types:       map[syntax.Expr]syntax.Type{},
			results:     map[syntax.Expr][]syntax.Type{},
			callees:     map[syntax.Expr]*callee{},
			constant:    map[syntax.Expr]syntax.Expr{},
			collections: map[syntax.Expr]*Constant{},
			values:      map[syntax.Node]*Constant{},
			defaults:    map[syntax.Node]syntax.Expr{},
			chains:      map[*syntax.Chain][]*Operation{},
			locals:      map[syntax.Node]syntax.Type{},
			loopVars:    map[*syntax.IterFor][]syntax.Type{},
			narrowed:    map[syntax.Expr]bool{},
			bindings:    map[syntax.Expr]*binding{},
			stored:      map[syntax.Expr]*binding{},
			noop:        map[*syntax.Arm]map[string]bool{},
			recovered:   map[*syntax.Handled][]syntax.Type{},
			raises:      map[Node]RaiseSet{},
			sites:       map[syntax.Expr]RaiseSet{},
			escapes:     map[syntax.Stmt]RaiseSet{},
			cyclic:      map[*resolve.Entry]bool{},
		},
		kernel:     kernel,
		settled:    map[syntax.Node]progress{},
		held:       map[syntax.Node]value{},
		evaluated:  map[syntax.Expr]evaluated{},
		edges:      map[Node][]Edge{},
		live:       map[origin]bool{},
		seen:       map[origin]bool{},
		deadAt:     map[origin]*syntax.Arm{},
		handlerArm: map[*syntax.Arm]bool{},
		deferred:   map[syntax.Expr]deferredValue{},
	}
}

// run types every declaration, builds the call graph, infers raise sets, and performs the kernel checks, where every declaration-level constant expression is settled before a body is typed, so that a body meets each constant with its value, and the backing values of the enums are typed before those, because the display of an enum member reads them.
func (c *checker) run() {
	c.declarations(func(d syntax.Decl) {
		if d, ok := d.(*syntax.Enum); ok {
			c.enumValues(d)
		}
	})
	c.declarations(c.initializers)
	c.declarations(func(d syntax.Decl) {
		if d, ok := d.(*syntax.Func); ok {
			c.routine(d, c.p.Table.Of(d), d.Where().File.Module)
		}
	})
	c.p.Graph = &CallGraph{Nodes: c.nodes, Edges: c.edges}
	c.domain = domainErrors(c.p.Table)
	c.inferRaises()
	c.reportRaises()
	c.p.Marks = c.p.Graph.marks(c.mutating, c.entity)
}

// declarations visits every top-level declaration of the source set in order.
func (c *checker) declarations(visit func(syntax.Decl)) {
	for _, f := range c.p.Files {
		for _, d := range f.Decls {
			visit(d)
		}
	}
}

// mutating reports whether a fixture declares a service method mutating.
func (c *checker) mutating(fn *syntax.Func) bool {
	e := c.p.Table.Of(fn)
	return e != nil && e.Owner != nil && slices.Contains(c.p.Options.Mutating, strings.TrimPrefix(e.Owner.Name, "@")+"."+fn.Name)
}

// entity reports whether a fixture declares a service as an entity-type target.
func (c *checker) entity(e *resolve.Entry) bool {
	return slices.Contains(c.p.Options.Entities, strings.TrimPrefix(e.Name, "@"))
}

// fault reports a name, type, or arity fault of the typer at a span.
func (c *checker) fault(o syntax.Origin, start, end int, reason, message string) {
	c.faults = append(c.faults, o.File.Record(reason, start, end, message))
}

// faultAt reports a typer fault over the whole origin of a node.
func (c *checker) faultAt(n syntax.Node, reason, message string) {
	o := n.Where()
	c.fault(o, o.Start, o.End, reason, message)
}

// faultName reports a typer fault at the first occurrence of a name in a node's origin at or after an offset.
func (c *checker) faultName(n syntax.Node, from int, name, reason, message string) {
	o := n.Where()
	start, end := o.NameSpan(from, name)
	c.fault(o, start, end, reason, message)
}

// report reports a diagnostic of a kernel check at a span.
func (c *checker) report(o syntax.Origin, start, end int, reason, message string) {
	c.diags = append(c.diags, o.File.Record(reason, start, end, message))
}

// reportAt reports a kernel-check diagnostic over the whole origin of a node.
func (c *checker) reportAt(n syntax.Node, reason, message string) {
	o := n.Where()
	c.report(o, o.Start, o.End, reason, message)
}

// reportName reports a kernel-check diagnostic at the first occurrence of a name in a node's origin at or after an offset.
func (c *checker) reportName(n syntax.Node, from int, name, reason, message string) {
	o := n.Where()
	start, end := o.NameSpan(from, name)
	c.report(o, start, end, reason, message)
}

// attach records the type of a single-valued expression.
func (c *checker) attach(x syntax.Expr, t syntax.Type) syntax.Type {
	if t != nil {
		c.p.types[x] = t
	}
	return t
}

// initializers settles the constant expressions of one top-level declaration: the value of a constant, the defaults of fields, and the defaults of parameters.
func (c *checker) initializers(d syntax.Decl) {
	switch d := d.(type) {
	case *syntax.Func:
		c.paramDefaults(d.Params)
	case *syntax.Query:
		c.paramDefaults(d.Params)
	case *syntax.Const:
		if c.p.Table.Of(d) != nil {
			c.global(d, nil)
		}
	case *syntax.Struct:
		c.fieldDefaults(d.Fields)
	case *syntax.Error:
		c.fieldDefaults(d.Fields)
	case *syntax.Entity:
		c.fieldDefaults(d.Fields)
	case *syntax.Enum, *syntax.Projection, *syntax.Service, *syntax.Alias, *syntax.Sum, *syntax.Distinct,
		*syntax.Contract, *syntax.Conformance, *syntax.Bad:
	default:
		panic(fmt.Sprintf("check: unexpected declaration %T", d))
	}
}

// initializer types a declaration-level constant expression of a module under its declared type, in a scope that holds the parameters beside a parameter default and no other name, and reports whether it has a type, where an expression of a form that no constant expression holds is reported and not typed, and where the check that the value stands where the declared type is required is made only when asked for, because the check of a constant against a fixed array type reads its value.
func (c *checker) initializer(x syntax.Expr, want syntax.Type, module string, params []*syntax.Param, assigned bool) bool {
	if x == nil || want == nil {
		return false
	}
	if n := excluded(x); n != nil {
		c.nonconstant(n)
		return false
	}
	r := &routine{module: module}
	sc := newScope(nil)
	for _, p := range params {
		sc.names[p.Name] = &binding{name: p.Name, typ: c.p.Table.Type(p.Type), decl: p, param: true}
	}
	unselected := c.unselected
	c.unselected = 0
	defer func() { c.unselected = unselected }()
	if assigned {
		return c.assign(x, sc, r, want) != nil
	}
	return c.TypeOf(x, sc, r, want) != nil
}

// settle evaluates a declaration-level constant expression once, rejecting a cycle of declarations that depend on each other.
func (c *checker) settle(n, ref syntax.Node, eval func() (value, bool)) (value, bool) {
	switch c.settled[n] {
	case done:
		return c.held[n], true
	case failed:
		return value{}, false
	case active:
		members, at := resolve.CloseCycle(c.path, n, ref)
		for _, m := range members {
			c.settled[m] = failed
		}
		c.faultAt(at, diagnostics.ConstantDependencyCycle, "a constant depends on itself")
		return value{}, false
	}
	c.settled[n] = active
	c.path = append(c.path, resolve.Step{Decl: n, Ref: ref})
	v, ok := eval()
	c.path = c.path[:len(c.path)-1]
	if !ok || c.settled[n] == failed {
		c.settled[n] = failed
		return value{}, false
	}
	c.settled[n], c.held[n] = done, v
	return v, true
}

// global returns the value of a module constant, or nil when it has none, where the first demand for the value types the initializer under the declared type, evaluates it, fixes the dynamic array lengths of the declared type by the value, and then checks the initializer against that type.
func (c *checker) global(d *syntax.Const, ref syntax.Node) *Constant {
	t := c.p.Table
	module := d.Where().File.Module
	_, ok := c.settle(d, ref, func() (value, bool) {
		want := t.Type(d.Type)
		if !c.initializer(d.Value, want, module, nil, false) || c.settled[d] == failed {
			return value{}, false
		}
		v, _, ok := c.required(d.Value, want, module)
		if !ok {
			return v, false
		}
		fixed, ok := cardinalized(want, v)
		if !ok {
			c.faultAt(d.Value, diagnostics.ConstantLengthMismatch, "the nested arrays of a constant have unequal lengths")
			return value{}, false
		}
		t.Fix(d.Type, fixed)
		c.p.values[d] = c.constant(v, fixed, d.Value.Where())
		c.record(d.Value, c.p.values[d])
		c.initializer(d.Value, fixed, module, nil, true)
		return v, true
	})
	if !ok {
		return nil
	}
	return c.p.values[d]
}

// fieldDefault returns the value of the default of a field, which is evaluated in the module that declares the field.
func (c *checker) fieldDefault(f *syntax.Field, ref syntax.Node) (value, bool) {
	return c.settle(f, ref, func() (value, bool) {
		return c.defaulted(f, f.Default, c.p.Table.Type(f.Type), nil)
	})
}

// defaulted types and evaluates the default of a field or of a parameter under its declared type and records its literal.
func (c *checker) defaulted(n syntax.Node, x syntax.Expr, want syntax.Type, params []*syntax.Param) (value, bool) {
	module := x.Where().File.Module
	if !c.initializer(x, want, module, params, true) || c.settled[n] == failed {
		return value{}, false
	}
	v, _, ok := c.required(x, want, module)
	if ok {
		c.p.defaults[n] = v.expr(x.Where())
	}
	return v, ok
}

func (c *checker) fieldDefaults(fields []*syntax.Field) {
	for _, f := range fields {
		if f.Default != nil {
			c.fieldDefault(f, nil)
		}
	}
}

func (c *checker) paramDefaults(params []*syntax.Param) {
	for _, p := range params {
		if p.Default != nil {
			c.settle(p, nil, func() (value, bool) {
				return c.defaulted(p, p.Default, c.p.Table.Type(p.Type), params)
			})
		}
	}
}

// length evaluates the constant that a fixed array type of a module writes as its length, and returns the literal of its value, which is a signed integer that is not negative, or the faults of its evaluation.
func (c *checker) length(t *resolve.Table, x *syntax.Name, module string) (syntax.Expr, []diagnostics.Record) {
	c.p.Table = t
	from := len(c.faults)
	var lit syntax.Expr
	typ := c.TypeOf(x, newScope(nil), &routine{module: module}, nil)
	if typ != nil {
		v, _, ok := c.required(x, typ, module)
		n, integer := whole(v.form)
		switch {
		case !ok:
		case !integer:
			c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("the length of a fixed array is a signed integer and not a %s", show(typ)))
		case n < 0:
			c.faultAt(x, diagnostics.ConstantOutOfRange, fmt.Sprintf("the length %d of a fixed array is negative", n))
		default:
			lit = &syntax.Lit{Origin: x.Where(), Kind: syntax.IntLit, Text: strconv.FormatInt(n, 10)}
		}
	}
	diags := slices.Clone(c.faults[from:])
	c.faults = c.faults[:from]
	return lit, diags
}

// enumValues checks that the backing values of an enum share one type.
func (c *checker) enumValues(d *syntax.Enum) {
	var backing syntax.Type
	for _, m := range d.Members {
		if m.Value == nil {
			continue
		}
		t := c.TypeOf(m.Value, newScope(nil), &routine{module: d.Where().File.Module}, backing)
		if t == nil {
			continue
		}
		if backing == nil {
			backing = t
		} else if !Assignable(t, backing) {
			c.faultAt(m.Value, diagnostics.TypeMismatch, fmt.Sprintf("every backing value of %s has the type %s", d.Name, show(backing)))
		}
	}
}
