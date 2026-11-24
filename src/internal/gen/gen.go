// Package gen maps the kernel forms of a checked kernel program to Go source that
// realizes them, with rlr as the fixed part of every generated program.
package gen

import (
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// emitter holds the state of one generation: the program, the table of operand modes, which is nil when the options ask for every copy, the declarations written so far, and the registered error types and locations.
type emitter struct {
	p      *check.Program
	t      *resolve.Table
	decls  strings.Builder
	errors []*resolve.Entry
	locs   []string
	serial int
	at     syntax.Node
	diags  []diagnostics.Record
	opts   Options
	modes  *check.Modes

	decoding map[*resolve.Entry]bool
	served   bool
	fault    error
}

// Program returns the Go source of a checked kernel program built under the options, or the internal error of a defect, which includes a form that the backend does not map, because the limits pass rejects such a program before lowering.
func Program(p *check.Program, opts Options) ([]byte, error) {
	e := &emitter{p: p, t: p.Table, opts: opts, decoding: map[*resolve.Entry]bool{}}
	if !opts.EveryCopy {
		e.modes = p.Modes
	}
	return e.program()
}

// program writes the Go source of the program of the emitter.
func (e *emitter) program() ([]byte, error) {
	p, opts := e.p, e.opts
	for _, f := range p.Files {
		for _, d := range f.Decls {
			if d, ok := d.(*syntax.Error); ok {
				e.errors = append(e.errors, e.t.Of(d))
			}
		}
	}
	for _, f := range p.Files {
		for _, d := range f.Decls {
			e.declaration(d)
		}
	}
	var out strings.Builder
	out.WriteString("package main\n\nimport \"ritchie.dev/rlr\"\n\n")
	if inv := e.inventory(); len(inv.Units) > 0 {
		e.served = true
		e.application(&out, inv)
	} else {
		fmt.Fprintf(&out, "func main() {\nrlr.Run(%s, func() error {\nreturn %s(nil)\n})\n}\n\n", strconv.Quote(opts.Main.Where().File.Module), e.funcName(opts.Main))
	}
	if e.fault != nil {
		return nil, e.fault
	}
	if len(e.diags) > 0 {
		diagnostics.Sort(e.diags)
		d := e.diags[0]
		return nil, fmt.Errorf("internal error: %s:%d:%d: %s, which the limits pass did not reject", d.Path, d.Span.Start.Line, d.Span.Start.Column, d.Message)
	}
	e.registrations(&out)
	out.WriteString(e.decls.String())
	src, err := format.Source([]byte(out.String()))
	if err != nil {
		return nil, fmt.Errorf("internal error: the generated Go does not parse: %v", err)
	}
	return src, nil
}

// registrations writes the init function that registers the error types and locations of the program.
func (e *emitter) registrations(out *strings.Builder) {
	out.WriteString("func init() {\nrlr.RegisterErrorTypes([]rlr.ErrorTypeMeta{\n")
	for _, en := range e.errors {
		d := en.Decl.(*syntax.Error)
		payload := typeName(en.Module, en.Name)
		fmt.Fprintf(out, "{Module: %q, Name: %q", en.Module, en.Name)
		if len(d.Fields) > 0 {
			out.WriteString(", Fields: []rlr.ErrorFieldMeta{")
			for _, f := range d.Fields {
				fmt.Fprintf(out, "{Name: %q, TypeName: %q}, ", f.Name, syntax.Print(f.Type))
			}
			fmt.Fprintf(out, "},\nRender: func(p any) []string {\nv := p.(%s)\nreturn []string{", payload)
			for _, f := range d.Fields {
				fmt.Fprintf(out, "rlr.CanonicalField(%s, %q), ", e.display(e.t.Type(f.Type), "v."+fieldName(f.Name)), en.Module)
			}
			fmt.Fprintf(out, "}\n},\nForm: func(p any) rlr.StructValue { return p.(%s).DisplayForm() }", payload)
			if e.served {
				fmt.Fprintf(out, ",\nDecode: func(in *rlr.Tokens) (any, error) { return %s(in) }", e.structDecoder(en))
			}
		}
		out.WriteString("},\n")
	}
	out.WriteString("})\nrlr.RegisterLocations([]rlr.LocationMeta{\n")
	for _, l := range e.locs {
		out.WriteString(l + ",\n")
	}
	out.WriteString("})\n}\n\n")
}

// declaration writes the Go declarations of one top-level kernel declaration.
func (e *emitter) declaration(d syntax.Decl) {
	e.at = d
	switch d := d.(type) {
	case *syntax.Func:
		e.routine(d)
	case *syntax.Struct:
		e.record(e.t.Of(d), d.Fields, false)
	case *syntax.Error:
		if len(d.Fields) > 0 {
			e.record(e.t.Of(d), d.Fields, true)
		}
	case *syntax.Enum:
		e.enum(e.t.Of(d), d)
	case *syntax.Service:
	case *syntax.Const:
		e.constant(d)
	case *syntax.Entity, *syntax.Projection, *syntax.Query:
		e.unsupported(d)
	default:
		panic(fmt.Sprintf("gen: unexpected declaration %T", d))
	}
}

// constant writes the Go variable of a module constant, whose initializer builds the value once when the program starts and after the constants that it uses, because Go initializes the variables of a package in the order of their dependencies.
func (e *emitter) constant(d *syntax.Const) {
	en := e.t.Of(d)
	t := e.t.Type(d.Type)
	f := &fn{e: e, module: d.Where().File.Module}
	f.push()
	var value string
	body := f.capture(func() { value = f.values([]syntax.Expr{d.Value}, []syntax.Type{t})[0] })
	g := e.goType(t)
	if body == "" {
		fmt.Fprintf(&e.decls, "var %s %s = %s\n\n", constName(en.Module, en.Name), g, value)
		return
	}
	fmt.Fprintf(&e.decls, "var %s %s = func() %s {\n%sreturn %s\n}()\n\n", constName(en.Module, en.Name), g, g, body, value)
}

// unsupported records a kernel form that the backend does not map, at its origin.
func (e *emitter) unsupported(n syntax.Node) {
	e.limit(n, fmt.Sprintf("%s is not supported by the backend", n.Where().Construct))
}

// internal records the first internal error of the backend: a kernel form that the kernel stage accepts and that the backend fails to map, which is a defect of the compiler and not a limit that a user can be told about.
func (e *emitter) internal(n syntax.Node, message string) {
	if e.fault == nil {
		o := n.Where()
		pos := o.File.Pos(o.Start)
		e.fault = fmt.Errorf("internal error: %s:%d:%d: %s", o.File.Path, pos.Line, pos.Column, message)
	}
}

// limit records a limit of the backend at the origin of a node, which Program reports as an internal error and Limits as a source diagnostic.
func (e *emitter) limit(n syntax.Node, message string) {
	o := n.Where()
	d := o.File.Record(diagnostics.UnsupportedSyntax, o.Start, o.End, message)
	if !slices.ContainsFunc(e.diags, func(r diagnostics.Record) bool { return r.Span == d.Span && r.Message == d.Message }) {
		e.diags = append(e.diags, d)
	}
}

// record writes the Go struct of a struct or error payload with its display form, its copy, and its equality.
func (e *emitter) record(en *resolve.Entry, fields []*syntax.Field, payload bool) {
	name := typeName(en.Module, en.Name)
	fmt.Fprintf(&e.decls, "type %s struct {\n", name)
	for _, f := range fields {
		fmt.Fprintf(&e.decls, "%s %s\n", fieldName(f.Name), e.goType(e.t.Type(f.Type)))
	}
	e.decls.WriteString("}\n\n")
	fmt.Fprintf(&e.decls, "func (v %s) DisplayForm() rlr.StructValue {\nreturn rlr.StructValue{Module: %q, Name: %q, Fields: []rlr.Field{", name, en.Module, en.Name)
	for _, f := range fields {
		fmt.Fprintf(&e.decls, "{Name: %q, Value: %s}, ", f.Name, e.display(e.t.Type(f.Type), "v."+fieldName(f.Name)))
	}
	e.decls.WriteString("}}\n}\n\n")
	deep := false
	for _, f := range fields {
		deep = deep || e.deep(e.t.Type(f.Type))
	}
	if deep && !payload {
		fmt.Fprintf(&e.decls, "func (v %s) clone() %s {\n", name, name)
		for _, f := range fields {
			if t := e.t.Type(f.Type); e.deep(t) {
				fmt.Fprintf(&e.decls, "v.%s = %s\n", fieldName(f.Name), e.clone(t, "v."+fieldName(f.Name)))
			}
		}
		e.decls.WriteString("return v\n}\n\n")
	}
	var tests []string
	for _, f := range fields {
		t := e.t.Type(f.Type)
		if !e.equatable(t) {
			return
		}
		n := fieldName(f.Name)
		tests = append(tests, e.equal(t, "a."+n, "b."+n))
	}
	if len(tests) == 0 {
		tests = []string{"true"}
	}
	fmt.Fprintf(&e.decls, "func (a %s) equal(b %s) bool {\nreturn %s\n}\n\n", name, name, strings.Join(tests, " && "))
}

// enum writes the Go type of an enum, which holds the ordinal of its member, a constant for each member, the display, a bare enum by member name and a backed enum by its pinned value, and the display form, whose JSON form is the member name.
func (e *emitter) enum(en *resolve.Entry, d *syntax.Enum) {
	name := typeName(en.Module, en.Name)
	fmt.Fprintf(&e.decls, "type %s uint32\n\nconst (\n", name)
	for i, m := range d.Members {
		fmt.Fprintf(&e.decls, "%s %s = %d\n", memberName(en.Module, en.Name, m.Name), name, i)
	}
	e.decls.WriteString(")\n\n")
	value := "nil"
	if backed(en) {
		value = "v.value()"
	}
	fmt.Fprintf(&e.decls, "func (v %s) form() rlr.EnumValue {\nreturn rlr.EnumValue{Name: %s[v], Value: %s}\n}\n\n", name, e.memberNames(d), value)
	if !backed(en) {
		fmt.Fprintf(&e.decls, "func (v %s) Display(string) string {\nreturn [...]string{", name)
		for _, m := range d.Members {
			fmt.Fprintf(&e.decls, "%q, ", m.Name)
		}
		e.decls.WriteString("}[v]\n}\n\n")
		return
	}
	t := e.p.TypeOf(d.Members[0].Value)
	fmt.Fprintf(&e.decls, "func (v %s) value() %[2]s {\nreturn [...]%[2]s{", name, e.goType(t))
	for _, m := range d.Members {
		fmt.Fprintf(&e.decls, "%s, ", e.literal(m.Value, t))
	}
	e.decls.WriteString("}[v]\n}\n\n")
}

// memberNames returns the Go array literal of the member names of an enum in declaration order.
func (e *emitter) memberNames(d *syntax.Enum) string {
	var b strings.Builder
	b.WriteString("[...]string{")
	for _, m := range d.Members {
		fmt.Fprintf(&b, "%q, ", m.Name)
	}
	b.WriteString("}")
	return b.String()
}

// location registers the location of a routine and returns its identifier.
func (e *emitter) location(module, owner, function string) int {
	e.locs = append(e.locs, fmt.Sprintf("{Module: %q, TypeName: %q, Function: %q}", module, owner, function))
	return len(e.locs) - 1
}

// funcName is the Go name of a free function or a method.
func (e *emitter) funcName(f *syntax.Func) string {
	en := e.t.Of(f)
	if en.Owner != nil {
		return methodName(en.Owner.Module, en.Owner.Name, en.Name)
	}
	return freeName(en.Module, en.Name)
}

// errorID is the Go expression of the registered type identifier of an error type: a catalog error by its rlr fault, a declared one after the catalog.
func (e *emitter) errorID(t syntax.Type) string {
	n := t.(*syntax.Named)
	if n.Module == "" && len(builtin.Lookup(builtin.ErrorRow, n.Name)) > 0 {
		return "rlr." + strings.TrimSuffix(n.Name, "!") + ".TypeID()"
	}
	en := e.t.Lookup(n.Module, n.Name)
	for i, d := range e.errors {
		if d == en {
			return fmt.Sprintf("rlr.FirstDeclared+%d", i)
		}
	}
	panic(fmt.Sprintf("gen: unknown error type %s", syntax.Print(t)))
}
