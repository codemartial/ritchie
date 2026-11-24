// This file maps the two K2 service forms: a service declaration becomes a unit of
// the inventory, and a service call a dispatch through the caller's invocation.
package gen

import (
	"fmt"
	"strconv"
	"strings"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
	"ritchie.dev/rlr"
)

const invocation = "inv"

// Options are the build inputs of one generation: the main routine, the publication of the root modules with the publication of each dependency module, the module component of the routes of a program without modules, and whether the test facility asks for every copy, which withholds the table of operand modes.
type Options struct {
	Main         *syntax.Func
	Publication  string
	Publications map[string]string
	Route        string
	EveryCopy    bool
}

// Unit is one resident unit of the application: a service of the dependency closure with its methods in declaration order and the units its methods call.
type Unit struct {
	Locator rlr.Locator
	Module  string
	Name    string
	Route   string
	Methods []*syntax.Func
	Calls   []rlr.Locator
}

// Inventory is the units of an application and the units its entry calls.
type Inventory struct {
	Entry      bool
	Units      []*Unit
	EntryCalls []rlr.Locator
}

// Units derives the inventory of a checked kernel program: one unit per service declaration of the modules that the root modules reach, one call per service that a method reaches through the local routines it calls.
func Units(p *check.Program, opts Options) Inventory {
	e := &emitter{p: p, t: p.Table, opts: opts}
	return e.inventory()
}

// inventory derives the inventory of the program being generated.
func (e *emitter) inventory() Inventory {
	closure := e.closure()
	inv := Inventory{Entry: e.opts.Main != nil}
	index := map[*resolve.Entry]*Unit{}
	for _, f := range e.p.Files {
		for _, d := range f.Decls {
			s, ok := d.(*syntax.Service)
			if !ok || !closure[f.File.Module] {
				continue
			}
			en := e.t.Of(s)
			u := &Unit{Locator: e.locatorOf(en), Module: en.Module, Name: en.Name, Route: en.Module}
			if u.Route == "" {
				u.Route = e.opts.Route
			}
			index[en] = u
			inv.Units = append(inv.Units, u)
		}
	}
	for _, f := range e.p.Files {
		for _, d := range f.Decls {
			if fn, ok := d.(*syntax.Func); ok && e.remote(fn) {
				if u := index[e.t.Of(fn).Owner]; u != nil {
					u.Methods = append(u.Methods, fn)
				}
			}
		}
	}
	for _, u := range inv.Units {
		u.Calls = e.reached(u.Methods)
	}
	if e.opts.Main != nil {
		inv.EntryCalls = e.reached([]*syntax.Func{e.opts.Main})
	}
	return inv
}

// closure returns the modules that the root modules reach, where a root module is one that no dependency publication supplies.
func (e *emitter) closure() map[string]bool {
	reached := map[string]bool{}
	var visit func(string)
	visit = func(m string) {
		if reached[m] {
			return
		}
		reached[m] = true
		for _, d := range e.t.Dependencies(m) {
			visit(d)
		}
	}
	for _, f := range e.p.Files {
		if _, dependency := e.opts.Publications[f.File.Module]; !dependency {
			visit(f.File.Module)
		}
	}
	return reached
}

// reached returns the Locators of the services that the routines call, directly or through the local routines they call, in order of first call.
func (e *emitter) reached(roots []*syntax.Func) []rlr.Locator {
	var out []rlr.Locator
	seen := map[*resolve.Entry]bool{}
	visited := map[*syntax.Func]bool{}
	var visit func(fn *syntax.Func)
	visit = func(fn *syntax.Func) {
		if visited[fn] || fn.Body == nil {
			return
		}
		visited[fn] = true
		syntax.Inspect(fn.Body, func(n syntax.Node) bool {
			x, ok := n.(syntax.Expr)
			if !ok {
				return true
			}
			k := e.p.CalleeOf(x)
			if k == nil || k.Func() == nil {
				return true
			}
			if _, service := x.(*syntax.ServiceCall); !service {
				visit(k.Func())
				return true
			}
			if dst := e.t.Of(k.Func()).Owner; !seen[dst] {
				seen[dst] = true
				out = append(out, e.locatorOf(dst))
			}
			return true
		})
	}
	for _, fn := range roots {
		visit(fn)
	}
	return out
}

// remote reports whether a callee is a service method, whose call crosses a unit boundary.
func (e *emitter) remote(callee *syntax.Func) bool {
	if callee == nil {
		return false
	}
	en := e.t.Of(callee)
	return en != nil && en.Routine == resolve.ServiceMethod
}

// locatorOf returns the Locator of a service: its publication, its module, and its name.
func (e *emitter) locatorOf(en *resolve.Entry) rlr.Locator {
	publication, dependency := e.opts.Publications[en.Module]
	if !dependency {
		publication = e.opts.Publication
	}
	return rlr.Locator{Publication: publication, Module: en.Module, Unit: en.Name}
}

// locator returns the Go literal of a Locator.
func locator(l rlr.Locator) string {
	return fmt.Sprintf("rlr.Locator{Publication: %q, Module: %q, Unit: %q}", l.Publication, l.Module, l.Unit)
}

// serviceCall writes a call of a service method whose response the caller obtains at the call: a dispatch of the copied arguments by the Locator of the service.
func (f *fn) serviceCall(x *syntax.ServiceCall, keep bool) attempt {
	callee := f.e.p.CalleeOf(x).Func()
	results := f.e.p.Results(x)
	a := attempt{err: f.temp(), callee: callee}
	values := "_"
	if keep && len(results) > 0 {
		values = f.temp()
	}
	_, vs := f.argList(x.Args, callee)
	owner := f.e.t.Of(callee).Owner
	f.line("%s, %s := %s.Call(%s, %q, %t, []any{%s})", values, a.err, invocation, locator(f.e.locatorOf(owner)), callee.Name, f.e.p.Marks.Mutates(callee), strings.Join(vs, ", "))
	if values != "_" {
		a.results = f.unboxed(values, a.err, results)
	}
	return a
}

// application writes the application of a program with resident units and the main function, which starts the units on the installed target and runs the entry as their client.
func (e *emitter) application(out *strings.Builder, inv Inventory) {
	main := "nil"
	module := ""
	if e.opts.Main != nil {
		main = e.funcName(e.opts.Main)
		module = e.opts.Main.Where().File.Module
	}
	out.WriteString("func main() {\nrlr.Start(application())\n}\n\n")
	fmt.Fprintf(out, "func application() rlr.Application {\nreturn rlr.Application{Module: %q, Main: %s, Units: []rlr.UnitSpec{\n", module, main)
	for _, u := range inv.Units {
		calls := make([]string, len(u.Calls))
		for i, c := range u.Calls {
			calls[i] = locator(c)
		}
		fmt.Fprintf(out, "{Locator: %s, Module: %q, Kind: \"service\", Route: %q, Calls: []rlr.Locator{%s}, Methods: []rlr.Method{\n", locator(u.Locator), u.Module, u.Route, strings.Join(calls, ", "))
		for _, m := range u.Methods {
			e.method(out, m)
		}
		out.WriteString("}},\n")
	}
	out.WriteString("}}\n}\n\n")
}

// method writes the published form of one service method: its parameters, its invocation over boxed arguments, and its codec, where the decoder evaluates each default as an expression, so that every invocation receives its own copy of an aggregate default.
func (e *emitter) method(out *strings.Builder, m *syntax.Func) {
	e.at = m
	var names, flags, args, declared, cases, vals []string
	defaulted := false
	for i, p := range m.Params {
		t := e.t.Type(p.Type)
		g := e.goType(t)
		names = append(names, strconv.Quote(p.Name))
		args = append(args, fmt.Sprintf("args[%d].(%s)", i, g))
		v := fmt.Sprintf("v%d", i)
		vals = append(vals, v)
		declaration := fmt.Sprintf("var %s %s", v, g)
		if p.Default != nil {
			f := &fn{e: e, out: &strings.Builder{}, module: m.Where().File.Module}
			f.push()
			value := f.values([]syntax.Expr{p.Default}, []syntax.Type{t})[0]
			declaration = f.out.String() + declaration + " = " + value
			defaulted = true
		}
		declared = append(declared, declaration)
		flags = append(flags, strconv.FormatBool(p.Default != nil))
		cases = append(cases, fmt.Sprintf("case %d:\n%s, err = %s(in)", i, v, e.decoder(t)))
	}
	results := resultNames(len(m.Results))
	var encoded, decoded, declaredResults []string
	for i, r := range m.Results {
		t := e.t.Type(r)
		declaredResults = append(declaredResults, fmt.Sprintf("var %s %s", results[i], e.goType(t)))
		encoded = append(encoded, e.display(t, fmt.Sprintf("results[%d].(%s)", i, e.goType(t))))
		decoded = append(decoded, fmt.Sprintf("case %d:\nr%d, err = %s(in)", i, i, e.decoder(t)))
	}
	call := e.funcName(m) + "(" + strings.Join(append([]string{invocation}, args...), ", ") + ")"
	optional := "nil"
	if defaulted {
		optional = "[]bool{" + strings.Join(flags, ", ") + "}"
	}
	fmt.Fprintf(out, "{Name: %q, Mutates: %t,\n", m.Name, e.p.Marks.Mutates(m))
	fmt.Fprintf(out, "Invoke: func(inv rlr.Invocation, args []any) ([]any, error) {\n%s := %s\nif err != nil {\nreturn nil, err\n}\nreturn []any{%s}, nil\n},\n",
		strings.Join(append(results, "err"), ", "), call, strings.Join(results, ", "))
	fmt.Fprintf(out, "Decode: func(in *rlr.Tokens) ([]any, error) {\n%s\nerr := rlr.DecodeObject(in, []string{%s}, %s, func(i int) (err error) {\nswitch i {\n%s\n}\nreturn err\n})\nif err != nil {\nreturn nil, err\n}\nreturn []any{%s}, nil\n},\n",
		strings.Join(declared, "\n"), strings.Join(names, ", "), optional, strings.Join(cases, "\n"), strings.Join(vals, ", "))
	fmt.Fprintf(out, "Encode: func(results []any) []any {\nreturn []any{%s}\n},\n", strings.Join(encoded, ", "))
	if len(results) > 0 {
		fmt.Fprintf(out, "Results: func(in *rlr.Tokens) ([]any, error) {\n%s\nerr := rlr.DecodeResults(in, %d, func(i int) (err error) {\nswitch i {\n%s\n}\nreturn err\n})\nif err != nil {\nreturn nil, err\n}\nreturn []any{%s}, nil\n},\n",
			strings.Join(declaredResults, "\n"), len(results), strings.Join(decoded, "\n"), strings.Join(results, ", "))
	}
	out.WriteString("},\n")
}

// decoder returns the Go function that decodes the JSON form of a boundary value of a type.
func (e *emitter) decoder(t syntax.Type) string {
	g := e.goType(t)
	switch t := t.(type) {
	case *syntax.Named:
		if isError(t) && builtinName(t) == "Error!" {
			return "rlr.DecodeError"
		}
		if isError(t) {
			return fmt.Sprintf("func(in *rlr.Tokens) (rlr.RitchieError, error) {\nreturn rlr.DecodeErrorOf(in, %s)\n}", e.errorID(t))
		}
		switch builtinName(t) {
		case "Int", "Int64", "Int8", "Int16", "Int32":
			return "rlr.DecodeInt[" + g + "]"
		case "Float":
			return "rlr.DecodeFloat"
		case "Bool":
			return "rlr.DecodeBool"
		case "Byte":
			return "rlr.DecodeByte"
		case "String":
			return "rlr.DecodeString"
		case "Decimal", "Date", "Time", "DateTime", "Duration", "BitSet":
			return "rlr.Decode" + builtinName(t)
		}
		if en := e.structOf(t); en != nil {
			return e.structDecoder(en)
		}
		if en := e.enumOf(t); en != nil {
			return fmt.Sprintf("func(in *rlr.Tokens) (%s, error) {\nreturn rlr.DecodeEnum[%[1]s](in, []string%s)\n}", g, strings.TrimPrefix(e.memberNames(en.Decl.(*syntax.Enum)), "[...]string"))
		}
	case *syntax.Array:
		if isBytes(t) && t.Length == nil {
			return "rlr.DecodeBytes"
		}
		if t.Length == nil {
			return fmt.Sprintf("func(in *rlr.Tokens) (%s, error) {\nreturn rlr.DecodeArray(in, %s)\n}", g, e.decoder(t.Elem))
		}
		return fmt.Sprintf("func(in *rlr.Tokens) (%s, error) {\nreturn rlr.DecodeFixed(in, %s, %s)\n}", g, syntax.Print(t.Length), e.decoder(t.Elem))
	case *syntax.Dict:
		if e.projected(t.Key) {
			return fmt.Sprintf("func(in *rlr.Tokens) (%s, error) {\nreturn rlr.DecodeKeyed(in, %s, %s, %s)\n}", g, e.projectFn(t.Key), e.decoder(t.Key), e.decoder(t.Value))
		}
		return fmt.Sprintf("func(in *rlr.Tokens) (%s, error) {\nreturn rlr.DecodeDict(in, %s, %s)\n}", g, e.decoder(t.Key), e.decoder(t.Value))
	case *syntax.Nullable:
		return fmt.Sprintf("func(in *rlr.Tokens) (%s, error) {\nreturn %s(in, %s)\n}", g, e.nullable(t).decode, e.decoder(t.Elem))
	}
	e.internal(e.at, fmt.Sprintf("a service method takes %s, which the boundary check accepts and which has no decoder", syntax.Print(t)))
	return "nil"
}

// structDecoder returns the name of the Go function that decodes the JSON object of a struct value, every field of which the object holds, reading each field as the text reaches it and writing the function once per struct so that a recursive struct decodes through it.
func (e *emitter) structDecoder(en *resolve.Entry) string {
	g := typeName(en.Module, en.Name)
	name := decoderName(en.Module, en.Name)
	if e.decoding[en] {
		return name
	}
	e.decoding[en] = true
	var names, cases []string
	for i, f := range resolve.FieldsOf(en.Decl) {
		names = append(names, strconv.Quote(f.Name))
		cases = append(cases, fmt.Sprintf("case %d:\nv.%s, err = %s(in)", i, fieldName(f.Name), e.decoder(e.t.Type(f.Type))))
	}
	fmt.Fprintf(&e.decls, "func %s(in *rlr.Tokens) (v %s, err error) {\nerr = rlr.DecodeObject(in, []string{%s}, nil, func(i int) (err error) {\nswitch i {\n%s\n}\nreturn err\n})\nreturn v, err\n}\n\n", name, g, strings.Join(names, ", "), strings.Join(cases, "\n"))
	return name
}
