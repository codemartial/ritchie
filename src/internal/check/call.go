// This file types calls, method calls, constructions, and the matching of arguments
// against declared parameters and against the rows of the builtin table.
package check

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// call types a call of a function, a function value, a built-in, or a type-level method, and fills its callee kind.
func (c *checker) call(x *syntax.Call, sc *scope, r *routine, want []syntax.Type) ([]syntax.Type, bool) {
	t := c.p.Table
	switch callee := x.Callee.(type) {
	case *syntax.Name:
		if callee.Owner != nil {
			return c.typeLevelCall(x, callee, sc, r, want)
		}
		if callee.Module != "" {
			if rows := builtin.Lookup(builtin.Function, callee.Module+"::"+callee.Name); len(rows) > 0 && builtin.Module(callee.Module) {
				return c.builtinCall(x, rows, c.argValues(x.Args, rows), nil, sc, r, want)
			}
			if !t.Module(callee.Module) {
				c.fault(callee.Origin, callee.Start, callee.Start+len(callee.Module), diagnostics.ModuleUnknownModule, "unknown module "+callee.Module)
				return nil, false
			}
			e := t.Lookup(callee.Module, callee.Name)
			if e == nil || e.Kind != resolve.FunctionKind {
				c.faultName(callee, callee.Start+len(callee.Module), callee.Name, diagnostics.ModuleUnknownMember, fmt.Sprintf("module %s declares no function %s", callee.Module, callee.Name))
				return nil, false
			}
			if !resolve.Visible(e, r.module) {
				c.faultName(callee, callee.Start+len(callee.Module), callee.Name, diagnostics.ModuleUnpublished, fmt.Sprintf("module %s does not publish %s", callee.Module, callee.Name))
				return nil, false
			}
			return c.routineCall(x, e, sc, r)
		}
		if b := sc.lookup(callee.Name); b != nil {
			if b.typ == nil {
				return nil, false
			}
			ft, ok := b.typ.(*syntax.FuncType)
			if !ok {
				c.faultAt(callee, diagnostics.CallNotCallable, fmt.Sprintf("%s is a value of type %s and not a function", diagnostics.Code(callee.Name), diagnostics.Code(show(b.typ))))
				return nil, false
			}
			c.read(callee, b, r)
			c.attach(callee, ft)
			return c.valueCall(x, ft, sc, r)
		}
		if e := t.Scope(r.module, callee.Name); e != nil && e.Decl != nil {
			switch e.Kind {
			case resolve.FunctionKind:
				return c.routineCall(x, e, sc, r)
			case resolve.ConstantKind:
				c.faultAt(callee, diagnostics.CallNotCallable, fmt.Sprintf("%s is a constant and not a function", diagnostics.Code(callee.Name)))
				return nil, false
			}
		}
		if callee.Name == "range" {
			c.faultAt(callee, diagnostics.RangeInvalidContext, "range is a loop source and not a value")
			return nil, false
		}
		if rows := builtin.Lookup(builtin.Function, callee.Name); len(rows) > 0 {
			return c.builtinCall(x, rows, c.argValues(x.Args, rows), nil, sc, r, want)
		}
		if t.Module(callee.Name) {
			c.faultAt(callee, diagnostics.ModuleNotValue, "a module name is not a function")
			return nil, false
		}
		c.faultAt(callee, diagnostics.CallUnknownFunction, "unknown function "+callee.Name)
		return nil, false
	case *syntax.Bad:
		return nil, false
	}
	ft, ok := c.TypeOf(x.Callee, sc, r, nil).(*syntax.FuncType)
	if !ok {
		if c.p.types[x.Callee] != nil {
			c.faultAt(x.Callee, diagnostics.CallNotCallable, fmt.Sprintf("a value of type %s is not a function", show(c.p.types[x.Callee])))
		}
		return nil, false
	}
	return c.valueCall(x, ft, sc, r)
}

// argValues returns the argument values of a call of a built-in in parameter order, where a named argument takes the place of the parameter of that name among the names the rows give, or nil when the arguments do not fill the parameters.
func (c *checker) argValues(args []*syntax.Arg, rows []*builtin.Row) []syntax.Expr {
	names := builtin.Names(rows)
	out := make([]syntax.Expr, len(args))
	named := len(args) > 0 && args[0].Name != ""
	for i, a := range args {
		at := slices.Index(names, a.Name)
		switch {
		case a.Name == "" && named:
			c.faultAt(a, diagnostics.CallMixedArguments, "a positional argument cannot follow a named one")
			return nil
		case a.Name != "" && !named:
			c.faultAt(a.Value, diagnostics.CallMixedArguments, "arguments are all positional or all named")
			return nil
		case a.Name == "":
			out[i] = a.Value
			continue
		case names == nil:
			c.faultAt(a, diagnostics.CallMixedArguments, "a built-in takes positional arguments")
			return nil
		case at < 0:
			c.faultAt(a, diagnostics.CallUnknownArgument, "no parameter is named "+a.Name)
			return nil
		case at >= len(out):
			c.faultAt(a, diagnostics.CallMissingArgument, "the parameters before "+a.Name+" have no arguments")
			return nil
		case out[at] != nil:
			c.faultAt(a, diagnostics.CallDuplicateArgument, "the parameter "+a.Name+" has two arguments")
			return nil
		}
		out[at] = a.Value
	}
	return out
}

// typeLevelCall types a call through a type: a user type-level method or a built-in conversion.
func (c *checker) typeLevelCall(x *syntax.Call, callee *syntax.Name, sc *scope, r *routine, want []syntax.Type) ([]syntax.Type, bool) {
	owner := c.resolveType(callee.Owner, r.module)
	if owner == nil {
		return nil, false
	}
	from := callee.Owner.Where().End
	if named, ok := owner.(*syntax.Named); ok {
		if e := c.p.Table.Lookup(named.Module, named.Name); e != nil && e.Kind == resolve.EnumKind {
			for _, m := range e.Decl.(*syntax.Enum).Members {
				if m.Name == callee.Name {
					c.faultName(callee, from, callee.Name, diagnostics.CallNotCallable, "an enum member is not a function")
					return nil, false
				}
			}
		}
	}
	m := c.p.Table.Method(owner, callee.Name)
	if m == nil {
		c.faultName(callee, from, callee.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no method %s", diagnostics.Code(show(owner)), diagnostics.Code(callee.Name)))
		return nil, false
	}
	if m.Level == resolve.InstanceLevel {
		c.faultName(callee, from, callee.Name, diagnostics.CallReceiverKind, fmt.Sprintf("%s is an instance method and is called on a value", diagnostics.Code(callee.Name)))
		return nil, false
	}
	if m.Decl == nil {
		rows := builtin.Lookup(builtin.TypeMethod, syntax.Print(owner)+"."+callee.Name)
		return c.builtinCall(x, rows, c.argValues(x.Args, rows), nil, sc, r, want)
	}
	if !c.visibleMethod(callee, from, m, r) {
		return nil, false
	}
	return c.routineCall(x, m, sc, r)
}

// visibleMethod checks that a method may be called from the current module, reporting a private method at its name.
func (c *checker) visibleMethod(at syntax.Node, from int, m *resolve.Entry, r *routine) bool {
	if strings.HasPrefix(m.Name, "_") && m.Module != r.module {
		c.faultName(at, from, m.Name, diagnostics.ModuleUnpublished, fmt.Sprintf("the method %s is private to module %s", m.Name, m.Module))
		return false
	}
	return true
}

// routineCall types a call of a declared routine, matching its arguments against the parameters.
func (c *checker) routineCall(x syntax.Expr, e *resolve.Entry, sc *scope, r *routine) ([]syntax.Type, bool) {
	fn := e.Decl.(*syntax.Func)
	var args []*syntax.Arg
	kind := syntax.LocalFunction
	switch x := x.(type) {
	case *syntax.Call:
		args = x.Args
		if e.Kind == resolve.MethodKind {
			kind = syntax.TypeMethod
		}
	case *syntax.MethodCall:
		args = x.Args
		kind = syntax.StructMethod
	case *syntax.ServiceCall:
		args = x.Args
		kind = syntax.ServiceMethod
	}
	switch e.Routine {
	case resolve.EntityMethod:
		kind = syntax.EntityMethod
	case resolve.ServiceMethod:
		kind = syntax.ServiceMethod
	}
	remote := kind == syntax.EntityMethod && (r.entry == nil || r.entry.Owner != e.Owner) || kind == syntax.ServiceMethod
	k := &callee{kind: kind, fn: fn, entry: e, remote: remote}
	c.edge(r, x, k)
	ok := c.matchParams(x, args, fn.Params, sc, r)
	results := c.funcType(fn).Results
	for _, t := range results {
		if t == nil {
			return nil, false
		}
	}
	c.p.results[x] = results
	return results, ok
}

// matchParams checks an argument list against declared parameters: all positional or all named, each required parameter supplied once, each value assignable.
func (c *checker) matchParams(call syntax.Expr, args []*syntax.Arg, params []*syntax.Param, sc *scope, r *routine) bool {
	t := c.p.Table
	ok, typed := true, true
	named := len(args) > 0 && args[0].Name != ""
	supplied := map[string]bool{}
	for i, a := range args {
		if (a.Name != "") != named {
			if named {
				c.faultAt(a.Value, diagnostics.CallMixedArguments, "arguments are all positional or all named")
			} else {
				c.faultAt(a, diagnostics.CallMixedArguments, "arguments are all positional or all named")
			}
			return false
		}
		var p *syntax.Param
		if named {
			for _, q := range params {
				if q.Name == a.Name {
					p = q
				}
			}
			if p == nil {
				c.faultName(a, a.Start, a.Name, diagnostics.CallUnknownArgument, fmt.Sprintf("no parameter is called %s", a.Name))
				return false
			}
			if supplied[a.Name] {
				c.faultName(a, a.Start, a.Name, diagnostics.CallDuplicateArgument, fmt.Sprintf("the parameter %s is supplied more than once", a.Name))
				ok = false
				continue
			}
		} else {
			if i >= len(params) {
				c.faultAt(a.Value, diagnostics.CallExtraArgument, fmt.Sprintf("the call supplies more than %d arguments", len(params)))
				return false
			}
			p = params[i]
		}
		supplied[p.Name] = true
		if c.assign(a.Value, sc, r, t.Type(p.Type)) == nil {
			typed = false
		}
	}
	if !typed {
		return false
	}
	for _, p := range params {
		if !supplied[p.Name] && p.Default == nil {
			o := call.Where()
			c.fault(o, o.End-1, o.End, diagnostics.CallMissingArgument, fmt.Sprintf("the parameter %s is not supplied", p.Name))
			return false
		}
	}
	return ok
}

// valueCall types a call of a function value.
func (c *checker) valueCall(x *syntax.Call, ft *syntax.FuncType, sc *scope, r *routine) ([]syntax.Type, bool) {
	k := &callee{kind: syntax.FunctionValue, typ: ft}
	c.edge(r, x, k)
	ok := true
	for i, a := range x.Args {
		if a.Name != "" {
			c.faultAt(a, diagnostics.CallMixedArguments, "a function value takes positional arguments")
			return nil, false
		}
		if i >= len(ft.Params) {
			c.faultAt(a.Value, diagnostics.CallExtraArgument, fmt.Sprintf("the call supplies more than %d arguments", len(ft.Params)))
			return nil, false
		}
		if c.assign(a.Value, sc, r, ft.Params[i]) == nil {
			ok = false
		}
	}
	if !ok {
		return nil, false
	}
	if len(x.Args) < len(ft.Params) {
		c.fault(x.Origin, x.End-1, x.End, diagnostics.CallMissingArgument, fmt.Sprintf("the call supplies %d of %d arguments", len(x.Args), len(ft.Params)))
		return nil, false
	}
	c.p.results[x] = ft.Results
	return ft.Results, true
}

// methodCall types a method call on a receiver value, deciding the callee kind from the receiver's type.
func (c *checker) methodCall(x *syntax.MethodCall, sc *scope, r *routine) ([]syntax.Type, bool) {
	recv := c.TypeOf(x.Receiver, sc, r, nil)
	if recv == nil {
		return nil, false
	}
	from := x.Receiver.Where().End
	if _, ok := recv.(*syntax.Nullable); ok {
		message := fmt.Sprintf("a method of a value of type %s is called only after its absence is resolved", show(recv))
		if why := c.absence(x.Receiver, r); why != "" {
			message += ", and " + why
		}
		c.faultName(x, from, x.Name, diagnostics.NullableUnresolved, message)
		return nil, false
	}
	if _, ok := recv.(*syntax.FuncType); ok {
		c.faultName(x, from, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no member %s", diagnostics.Code(show(recv)), diagnostics.Code(x.Name)))
		return nil, false
	}
	m := c.p.Table.Method(recv, x.Name)
	if m == nil {
		if named, ok := recv.(*syntax.Named); ok {
			if e := c.p.Table.Lookup(named.Module, named.Name); e != nil && e.Decl != nil {
				for _, f := range resolve.FieldsOf(e.Decl) {
					if f.Name == x.Name {
						c.faultName(x, from, x.Name, diagnostics.CallNotCallable, fmt.Sprintf("%s is a field and not a method", diagnostics.Code(x.Name)))
						return nil, false
					}
				}
			}
		}
		c.faultName(x, from, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no method %s", diagnostics.Code(show(recv)), diagnostics.Code(x.Name)))
		return nil, false
	}
	if m.Level == resolve.TypeLevel {
		c.faultName(x, from, x.Name, diagnostics.CallReceiverKind, fmt.Sprintf("%s is a type-level method and is called on its type", diagnostics.Code(x.Name)))
		return nil, false
	}
	if m.Decl == nil {
		owner := syntax.Print(recv)
		if _, ok := recv.(*syntax.Dict); ok {
			owner = "[K: V]"
		}
		rows := builtin.Lookup(builtin.Method, owner+"."+x.Name)
		args := c.argValues(x.Args, rows)
		if args == nil && len(x.Args) > 0 {
			return nil, false
		}
		return c.builtinCall(x, rows, append([]syntax.Expr{x.Receiver}, args...), recv, sc, r, nil)
	}
	if !c.visibleMethod(x, from, m, r) {
		return nil, false
	}
	results, ok := c.routineCall(x, m, sc, r)
	if r.facts != nil {
		r.facts.mutate(c.pathOf(x.Receiver), fmt.Sprintf("after the call of %s at %s", x.Name, where(x)))
	}
	return results, ok
}

// serviceCall types a call of a service method.
func (c *checker) serviceCall(x *syntax.ServiceCall, sc *scope, r *routine) ([]syntax.Type, bool) {
	t := c.resolveType(x.Service, r.module)
	if t == nil {
		return nil, false
	}
	m := c.p.Table.Method(t, x.Name)
	if m == nil {
		c.faultName(x, x.Service.Where().End, x.Name, diagnostics.MemberUnknown, fmt.Sprintf("%s has no method %s", diagnostics.Code(show(t)), diagnostics.Code(x.Name)))
		return nil, false
	}
	if !c.visibleMethod(x, x.Service.Where().End, m, r) {
		return nil, false
	}
	return c.routineCall(x, m, sc, r)
}

// dotMember types a leading-dot member against the declared type of its position.
func (c *checker) dotMember(x *syntax.DotMember, sc *scope, r *routine, want syntax.Type) ([]syntax.Type, bool) {
	named, ok := unwrap(want).(*syntax.Named)
	if !ok {
		c.faultAt(x, diagnostics.TypeContextRequired, "a leading-dot member resolves against the named type written to its left")
		return nil, false
	}
	ref := &syntax.Name{Origin: x.Origin, Owner: named, Name: x.Name}
	if !x.Call {
		t := c.typeMember(ref, r)
		if t == nil {
			return nil, false
		}
		if v := c.p.constant[ref]; v != nil {
			c.p.constant[x] = v
		}
		return []syntax.Type{t}, true
	}
	call := &syntax.Call{Origin: x.Origin, Callee: ref, Args: x.Args}
	results, ok := c.typeLevelCall(call, ref, sc, r, []syntax.Type{want})
	if k := c.p.callees[call]; k != nil {
		c.p.callees[x] = k
	}
	c.p.results[x] = results
	return results, ok
}

// construct types a struct or error construction, matching its initializer against the fields.
func (c *checker) construct(x *syntax.Construct, sc *scope, r *routine) syntax.Type {
	t := c.resolveType(x.Type, r.module)
	if t == nil {
		return nil
	}
	named, ok := t.(*syntax.Named)
	if !ok {
		c.faultAt(x.Type, diagnostics.TypeMismatch, fmt.Sprintf("%s is not a struct or error type", diagnostics.Code(show(t))))
		return nil
	}
	e := c.p.Table.Lookup(named.Module, named.Name)
	if e == nil || e.Kind != resolve.StructKind && e.Kind != resolve.ErrorKind || e.Decl == nil {
		catalog := e != nil && e.Kind == resolve.ErrorKind && builtinName(named) != "Error!"
		switch {
		case catalog && !c.kernel && !c.debug:
			c.faultAt(x, diagnostics.ErrorCatalogConstruction, fmt.Sprintf("%s is a catalog error, which application code constructs only in a build with --debug", diagnostics.Code(show(t))))
			return nil
		case catalog && len(x.Args) == 0:
			return named
		}
		c.faultAt(x.Type, diagnostics.TypeMismatch, fmt.Sprintf("%s cannot be constructed", diagnostics.Code(show(t))))
		return nil
	}
	if e.Module != r.module {
		c.faultAt(x, diagnostics.ModuleUnpublished, fmt.Sprintf("a literal of %s is written only in module %s", e.Name, e.Module))
		return nil
	}
	if !c.initList(x, x.Args, resolve.FieldsOf(e.Decl), e, sc, r) {
		return nil
	}
	return named
}

// initList matches an initializer list against the fields of a type: all positional or all named, each field without a default supplied once, each value assignable.
func (c *checker) initList(x syntax.Expr, args []*syntax.Arg, fields []*syntax.Field, e *resolve.Entry, sc *scope, r *routine) bool {
	t := c.p.Table
	ok := true
	named := len(args) > 0 && args[0].Name != ""
	supplied := map[string]bool{}
	for i, a := range args {
		if (a.Name != "") != named {
			c.faultAt(a, diagnostics.CallMixedArguments, "an initializer is all positional or all named")
			return false
		}
		var f *syntax.Field
		if named {
			for _, g := range fields {
				if g.Name == a.Name {
					f = g
				}
			}
			if f == nil {
				c.faultName(a, a.Start, a.Name, diagnostics.CallUnknownArgument, fmt.Sprintf("%s has no field %s", diagnostics.Code(e.Name), diagnostics.Code(a.Name)))
				ok = false
				continue
			}
			if supplied[a.Name] {
				c.faultName(a, a.Start, a.Name, diagnostics.CallDuplicateArgument, fmt.Sprintf("the field %s is supplied more than once", a.Name))
				ok = false
				continue
			}
		} else {
			if i >= len(fields) {
				c.faultAt(a.Value, diagnostics.CallExtraArgument, fmt.Sprintf("%s has %d fields", diagnostics.Code(e.Name), len(fields)))
				return false
			}
			f = fields[i]
		}
		supplied[f.Name] = true
		if c.assign(a.Value, sc, r, t.Type(f.Type)) == nil {
			ok = false
		}
	}
	for _, f := range fields {
		if !supplied[f.Name] && f.Default == nil {
			c.faultAt(x, diagnostics.CallMissingArgument, fmt.Sprintf("the field %s is not supplied", f.Name))
			return false
		}
	}
	return ok
}

// builtinCall types a call of a built-in from its rows, matched in one step against the argument types, where the receiver of a method is the first argument.
func (c *checker) builtinCall(x syntax.Expr, rows []*builtin.Row, args []syntax.Expr, recv syntax.Type, sc *scope, r *routine, want []syntax.Type) ([]syntax.Type, bool) {
	if args == nil && len(rows) > 0 && len(rows[0].Params) > 0 {
		return nil, false
	}
	least, most := -1, -1
	for _, row := range rows {
		n := len(row.Inputs)
		if least < 0 || n < least {
			least = n
		}
		if row.Variadic {
			most = 1 << 30
		} else if n > most {
			most = n
		}
	}
	o := x.Where()
	offset := 0
	if recv != nil {
		offset = 1
	}
	if len(args) < least {
		c.fault(o, o.End-1, o.End, diagnostics.CallMissingArgument, fmt.Sprintf("%s requires %d or more arguments", diagnostics.Code(rows[0].Name), least-offset))
		return nil, false
	}
	if len(args) > most {
		c.faultAt(args[most], diagnostics.CallExtraArgument, fmt.Sprintf("%s takes at most %d arguments", diagnostics.Code(rows[0].Name), most-offset))
		return nil, false
	}
	var one syntax.Type
	if len(want) == 1 {
		one = want[0]
	}
	var ctx syntax.Type
	if one != nil && (rows[0].Name == "abs" || rows[0].Name == "min" || rows[0].Name == "max") {
		ctx = one
	}
	var types []syntax.Type
	ok := true
	elems, filled := unwrap(one).(*syntax.Array)
	entries, zipped := unwrap(one).(*syntax.Dict)
	switch {
	case recv != nil:
		rest, restOk := c.operandsUnder(args[1:], sc, r, ctx, func(rest []syntax.Type) []syntax.Type {
			return requiredParams(rows, append([]syntax.Type{recv}, rest...))[1:]
		})
		types, ok = append([]syntax.Type{recv}, rest...), restOk
	case filled && rows[0].Name == "fill" && len(args) == 2:
		types = []syntax.Type{c.TypeOf(args[0], sc, r, elems.Elem), c.TypeOf(args[1], sc, r, nil)}
		ok = types[0] != nil && types[1] != nil
	case zipped && rows[0].Name == "zip" && len(args) == 2:
		types = []syntax.Type{c.TypeOf(args[0], sc, r, &syntax.Array{Elem: entries.Key}), c.TypeOf(args[1], sc, r, &syntax.Array{Elem: entries.Value})}
		ok = types[0] != nil && types[1] != nil
	default:
		types, ok = c.operandsUnder(args, sc, r, ctx, func(types []syntax.Type) []syntax.Type {
			return requiredParams(rows, types)
		})
	}
	if !ok {
		return nil, false
	}
	for i, t := range types {
		if isNone(t) && i < len(args) {
			c.faultAt(args[i], diagnostics.TypeMismatch, "none has a type only where a nullable type is required")
			return nil, false
		}
	}
	row, bind, at := c.selectRowOf(rows, types)
	if row != nil && slices.Contains(row.Extra, builtin.Equal) && c.laterEquality(x, types[0]) {
		return nil, false
	}
	if row == nil {
		if at >= 0 && at < len(args) {
			c.faultAt(args[at], diagnostics.TypeMismatch, fmt.Sprintf("%s does not accept a value of type %s here", diagnostics.Code(rows[0].Name), diagnostics.Code(show(types[at]))))
		} else if at >= 0 {
			c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("%s does not accept these argument types", diagnostics.Code(rows[0].Name)))
		}
		return nil, false
	}
	if row.Name == "range" {
		var first syntax.Type
		for i, a := range args {
			switch {
			case contextual(a), !Same(row.Params[i], row.Params[0]):
			case first == nil:
				first = types[i]
			case !Same(types[i], first):
				c.faultAt(a, diagnostics.TypeMismatch, fmt.Sprintf("every typed operand of range has the type %s and not %s", show(first), show(types[i])))
				return nil, false
			}
		}
	}
	if row.Name == "fill" {
		if filled {
			if n, ok := lengthOf(elems); ok {
				value := c.p.constant[args[1]]
				if value == nil {
					value = args[1]
				}
				count, isLit := value.(*syntax.Lit)
				if !isLit || count.Canonical() != strconv.FormatInt(n, 10) {
					c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("a fixed result of %d elements requires a constant count of %d", n, n))
					return nil, false
				}
			}
		}
	}
	if row.Name == "sort" && !ordered(bind["T"]) {
		c.faultAt(x, diagnostics.TypeMismatch, fmt.Sprintf("sort requires an ordered element type and not %s", show(bind["T"])))
		return nil, false
	}
	k := &callee{kind: syntax.BuiltinFunction, op: c.resolved(row, bind, types)}
	if row.Form == builtin.TypeMethod {
		k.kind = syntax.TypeMethod
	}
	var results []syntax.Type
	for _, res := range row.Results {
		t := instantiate(res, bind, one)
		if key := c.ineligibleKey(t); key != nil && !Same(t, one) {
			c.faultAt(x, diagnostics.TypeInvalidKey, fmt.Sprintf("%s is not a dictionary key type", diagnostics.Code(show(key))))
			return nil, false
		}
		results = append(results, t)
	}
	c.edge(r, x, k)
	c.p.results[x] = results
	return results, true
}

// ineligibleKey returns the key type of the first dictionary within a type that cannot key a dictionary, or nil.
func (c *checker) ineligibleKey(t syntax.Type) syntax.Type {
	switch t := t.(type) {
	case *syntax.Dict:
		if !c.p.Table.ValidKey(t.Key) {
			return t.Key
		}
		return c.ineligibleKey(t.Value)
	case *syntax.Array:
		return c.ineligibleKey(t.Elem)
	case *syntax.Nullable:
		return c.ineligibleKey(t.Elem)
	}
	return nil
}

// ordered reports whether values of a type have a natural order.
func ordered(t syntax.Type) bool { return t != nil && order(t) != builtin.NoOrder }

// selectRow selects among the rows of a form and name the first whose parameters accept the argument types, returning the row, its letter bindings, and the index of the first argument that no row accepts.
func (c *checker) selectRow(form builtin.Form, name string, types []syntax.Type) (*builtin.Row, map[string]syntax.Type, int) {
	return c.selectRowOf(builtin.Lookup(form, name), types)
}

func (c *checker) selectRowOf(rows []*builtin.Row, types []syntax.Type) (*builtin.Row, map[string]syntax.Type, int) {
	best := 0
	for _, row := range rows {
		if row.Variadic && len(types) == 0 || !row.Variadic && len(row.Inputs) != len(types) {
			continue
		}
		bind := map[string]syntax.Type{}
		matched := true
		for i, t := range types {
			p := row.Inputs[min(i, len(row.Inputs)-1)]
			if t == nil || !unify(p, t, bind) {
				matched = false
				best = max(best, i)
				break
			}
		}
		if matched && !row.Futures && slices.ContainsFunc(types, holdsFuture) {
			matched = false
		}
		if matched {
			return row, bind, -1
		}
	}
	return nil, nil, best
}

// requiredParams returns, for each untyped position of types, the parameter type without letters on which every row that accepts the typed positions agrees, or nil where no such type exists.
func requiredParams(rows []*builtin.Row, types []syntax.Type) []syntax.Type {
	out := make([]syntax.Type, len(types))
	agreed := make([]bool, len(types))
	for i := range agreed {
		agreed[i] = types[i] == nil
	}
	accepted := false
	for _, row := range rows {
		if row.Variadic && len(types) == 0 || !row.Variadic && len(row.Inputs) != len(types) {
			continue
		}
		bind := map[string]syntax.Type{}
		matched := true
		for i, t := range types {
			if t != nil && !unify(row.Inputs[min(i, len(row.Inputs)-1)], t, bind) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		accepted = true
		for i, t := range types {
			if t != nil || !agreed[i] {
				continue
			}
			p := instantiate(row.Inputs[min(i, len(row.Inputs)-1)], bind, nil)
			switch {
			case hasLetter(p):
				agreed[i] = false
			case out[i] == nil:
				out[i] = p
			case !Same(out[i], p):
				agreed[i] = false
			}
		}
	}
	for i := range out {
		if !accepted || !agreed[i] {
			out[i] = nil
		}
	}
	return out
}

// hasLetter reports whether a signature type still contains a letter that stands for any type.
func hasLetter(t syntax.Type) bool {
	if _, ok := builtin.Letter(t); ok {
		return true
	}
	switch t := t.(type) {
	case *syntax.Array:
		return hasLetter(t.Elem)
	case *syntax.Dict:
		return hasLetter(t.Key) || hasLetter(t.Value)
	case *syntax.Nullable:
		return hasLetter(t.Elem)
	case *syntax.FuncType:
		return slices.ContainsFunc(t.Params, hasLetter) || slices.ContainsFunc(t.Results, hasLetter)
	}
	return false
}

// unify matches a signature pattern against an argument type, binding letters and array forms consistently.
func unify(pattern, actual syntax.Type, bind map[string]syntax.Type) bool {
	return match(pattern, actual, bind, false)
}

// match unifies a pattern that stands inside a collection or outside one, where a letter that a collection bound keeps its type: a later collection has the same type there, and a later value is assignable to it.
func match(pattern, actual syntax.Type, bind map[string]syntax.Type, inside bool) bool {
	if letter, ok := builtin.Letter(pattern); ok {
		bound, seen := bind[letter]
		switch _, held := bind["="+letter]; {
		case held && inside:
			return Same(bound, actual)
		case held:
			return Assignable(actual, bound)
		case !seen || isNone(bound):
			bind[letter] = actual
		case isNone(actual):
			bind[letter] = nullable(bound)
		default:
			j, ok := join(bound, actual)
			if !ok {
				return false
			}
			bind[letter] = j
		}
		if inside {
			bind["="+letter] = bind[letter]
		}
		return true
	}
	switch p := pattern.(type) {
	case *syntax.Named:
		return Assignable(actual, p) && !isNone(actual)
	case *syntax.Array:
		a, ok := actual.(*syntax.Array)
		if !ok {
			return false
		}
		if letter, ok := builtin.Cardinality(p.Length); ok {
			if bound, seen := bind["["+letter]; seen && !Same(bound, a) {
				return false
			}
			bind["["+letter] = a
		} else {
			pn, pfixed := lengthOf(p)
			an, afixed := lengthOf(a)
			if pfixed != afixed || pn != an {
				return false
			}
		}
		return match(p.Elem, a.Elem, bind, true)
	case *syntax.Dict:
		a, ok := actual.(*syntax.Dict)
		return ok && match(p.Key, a.Key, bind, true) && match(p.Value, a.Value, bind, true)
	case *syntax.Nullable:
		if a, ok := actual.(*syntax.Nullable); ok {
			return match(p.Elem, a.Elem, bind, inside)
		}
		return match(p.Elem, actual, bind, inside)
	case *syntax.FuncType:
		a, ok := actual.(*syntax.FuncType)
		if !ok || len(a.Params) != len(p.Params) || len(a.Results) != len(p.Results) {
			return false
		}
		for i := range p.Params {
			if !match(p.Params[i], a.Params[i], bind, inside) {
				return false
			}
		}
		for i := range p.Results {
			if !match(p.Results[i], a.Results[i], bind, inside) {
				return false
			}
		}
		return true
	}
	return false
}

// instantiate replaces the letters of a signature result by their bindings, taking the form of an unbound array letter from the type of the position.
func instantiate(pattern syntax.Type, bind map[string]syntax.Type, want syntax.Type) syntax.Type {
	if letter, ok := builtin.Letter(pattern); ok {
		if t := bind[letter]; t != nil {
			return t
		}
		return pattern
	}
	switch p := pattern.(type) {
	case *syntax.Named:
		return p
	case *syntax.Array:
		elem := instantiate(p.Elem, bind, nil)
		if letter, ok := builtin.Cardinality(p.Length); ok {
			if a, ok := bind["["+letter].(*syntax.Array); ok {
				return &syntax.Array{Elem: elem, Length: a.Length}
			}
			if a, ok := unwrap(want).(*syntax.Array); ok {
				return &syntax.Array{Elem: elem, Length: a.Length}
			}
			return &syntax.Array{Elem: elem}
		}
		return &syntax.Array{Elem: elem, Length: p.Length}
	case *syntax.Dict:
		return &syntax.Dict{Key: instantiate(p.Key, bind, nil), Value: instantiate(p.Value, bind, nil)}
	case *syntax.Nullable:
		return nullable(instantiate(p.Elem, bind, nil))
	case *syntax.FuncType:
		ft := &syntax.FuncType{}
		for _, q := range p.Params {
			ft.Params = append(ft.Params, instantiate(q, bind, nil))
		}
		for _, q := range p.Results {
			ft.Results = append(ft.Results, instantiate(q, bind, nil))
		}
		return ft
	}
	return pattern
}
