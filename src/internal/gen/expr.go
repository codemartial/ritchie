// This file writes expressions: every effect and every fallible operation is hoisted
// into a statement in evaluation order, so the Go expression that remains reads values
// only.
package gen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/check"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// attempt is the evaluation of a fallible operation: its result expressions and the Go error or the rlr fault that tells its failure, or both when a fault skips the call.
type attempt struct {
	results []string
	err     string
	fault   string
	callee  *syntax.Func
}

// expr returns the Go expression of a single-valued kernel expression, writing the statements its evaluation needs before it.
func (f *fn) expr(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.Lit:
		return f.e.literal(x, f.e.p.TypeOf(x))
	case *syntax.Name:
		return f.name(x)
	case *syntax.Self:
		return "(*self)"
	case *syntax.Prim:
		return f.prim(x)
	case *syntax.Logical:
		return f.logical(x)
	case *syntax.Format, *syntax.Call, *syntax.MethodCall, *syntax.ServiceCall, *syntax.IndexRead, *syntax.Slice:
		a := f.attempt(x, true)
		f.propagate(a)
		return a.results[0]
	case *syntax.Send:
		return f.futures(x)[0]
	case *syntax.Wait:
		if s, ok := x.X.(*syntax.Send); ok {
			a := f.waitSend(s, true)
			f.propagate(a)
			return a.results[0]
		}
		return f.payload(x, f.wait(x))
	case *syntax.Closure:
		return f.closure(x)
	case *syntax.Construct:
		return f.construct(x)
	case *syntax.ArrayLit:
		t := f.e.p.TypeOf(x).(*syntax.Array)
		if futureOf(t.Elem) != nil {
			return f.collected(x, t)
		}
		elems := make([]syntax.Type, len(x.Elems))
		for i := range elems {
			elems[i] = t.Elem
		}
		return f.e.goType(t) + "{" + strings.Join(f.values(x.Elems, elems), ", ") + "}"
	case *syntax.DictLit:
		return f.dict(x)
	case *syntax.FieldRead:
		return f.payload(x, f.field(x))
	case *syntax.Create, *syntax.Lookup, *syntax.QueryCall:
		f.e.unsupported(x)
		return "nil"
	default:
		panic(fmt.Sprintf("gen: unexpected expression %T", x))
	}
}

// payload returns the present value of a nullable path that the kernel narrows, read without a presence test because the presence facts of the kernel checker hold, and the value of any other expression.
func (f *fn) payload(x syntax.Expr, v string) string {
	if f.e.p.Narrowed(x) {
		return v + ".Value()"
	}
	return v
}

// livePayload returns the place of the present value of a nullable path that the kernel narrows, taken without a presence test, and any other place unchanged.
func (f *fn) livePayload(x syntax.Expr, place string) string {
	if !f.e.p.Narrowed(x) {
		return place
	}
	if t := f.e.p.TypeOf(x); f.e.boxed(t) {
		return "(*" + place + ".Place(" + f.e.cloneFn(t) + "))"
	}
	return "(*" + place + ".Place())"
}

// throughBox reports whether a place lies under the present value of a narrowed nullable that is an rlr.Box, whose write first gives the box its own copy of the value.
func (f *fn) throughBox(x syntax.Expr) bool {
	for {
		if f.e.p.Narrowed(x) && f.e.boxed(f.e.p.TypeOf(x)) {
			return true
		}
		switch y := x.(type) {
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			x = y.X
		default:
			return false
		}
	}
}

// settle stores each value that is not stable in a temporary, so that a value that copies a box is taken before a write through a box of the same statement replaces the shared value with a copy.
func (f *fn) settle(xs []syntax.Expr, vs []string) {
	for i, v := range vs {
		if !f.stable(xs[i], v) {
			vs[i] = f.spillText(v)
		}
	}
}

// name returns a local, a member of an enum, a module constant, or a top-level function as a function value.
func (f *fn) name(x *syntax.Name) string {
	if x.Owner != nil {
		if en := f.e.enumOf(f.e.t.Type(x.Owner)); en != nil {
			return memberName(en.Module, en.Name, x.Name)
		}
		f.e.unsupported(x)
		return "nil"
	}
	if _, ok := f.local(x.Name); ok && x.Module == "" {
		return f.payload(x, localName(x.Name))
	}
	var en *resolve.Entry
	if x.Module != "" {
		en = f.e.t.Lookup(x.Module, x.Name)
	} else {
		en = f.e.t.Scope(f.module, x.Name)
	}
	if en.Kind == resolve.ConstantKind {
		return constName(en.Module, en.Name)
	}
	return f.bound(en.Decl.(*syntax.Func), f.e.p.TypeOf(x).(*syntax.FuncType))
}

// bound returns a top-level function as a function value of type ft, which calls it under the invocation of the routine that took the value.
func (f *fn) bound(fn *syntax.Func, ft *syntax.FuncType) string {
	params := make([]string, len(ft.Params))
	args := []string{invocation}
	for i, p := range ft.Params {
		a := "a" + strconv.Itoa(i)
		params[i] = a + " " + f.e.goType(p)
		args = append(args, a)
	}
	var results []string
	for _, r := range ft.Results {
		results = append(results, f.e.goType(r))
	}
	return fmt.Sprintf("func(%s) (%s) {\nreturn %s(%s)\n}", strings.Join(params, ", "), strings.Join(append(results, "error"), ", "), f.e.funcName(fn), strings.Join(args, ", "))
}

// propagate writes the test after a fallible operation that leaves the routine with its failure.
func (f *fn) propagate(a attempt) {
	if a.fault != "" {
		f.line("if %s != rlr.NoFault {", a.fault)
		f.leave(fmt.Sprintf("rlr.Origin(%s, %d, %s)", a.fault, f.loc, f.raiser), nil)
		f.line("}")
	}
	if a.err != "" {
		f.line("if %s != nil {", a.err)
		f.leave(f.passOn(a.err, a.callee), a.callee)
		f.line("}")
	}
}

// multi evaluates a call of any arity and returns its results, leaving the routine on failure.
func (f *fn) multi(x syntax.Expr) []string {
	a := f.attempt(x, true)
	f.propagate(a)
	return a.results
}

// discard evaluates an expression statement, whose results are not read.
func (f *fn) discard(x syntax.Expr) {
	if s, ok := x.(*syntax.Send); ok {
		f.send(s)
		return
	}
	f.propagate(f.attempt(x, false))
}

// failure returns the Go error of an attempt, built from its fault when the operation is a built-in one or a resolution failed.
func (f *fn) failure(a attempt) (string, *syntax.Func) {
	if a.fault == "" {
		return a.err, a.callee
	}
	if a.err == "" {
		a.err = f.temp()
		f.line("var %s error", a.err)
	}
	f.line("if %s != rlr.NoFault {", a.fault)
	f.line("%s = rlr.Origin(%s, %d, %s)", a.err, a.fault, f.loc, f.raiser)
	f.line("}")
	return a.err, a.callee
}

// attempt evaluates a fallible operation without testing its failure: a call, or an operator, index, slice, or formatted string with a fault set.
func (f *fn) attempt(x syntax.Expr, keep bool) attempt {
	switch x := x.(type) {
	case *syntax.Call:
		return f.call(x, keep)
	case *syntax.MethodCall:
		return f.methodCall(x, keep)
	case *syntax.ServiceCall:
		return f.serviceCall(x, keep)
	case *syntax.Wait:
		return f.waitSend(x.X.(*syntax.Send), keep)
	case *syntax.IndexRead:
		if check.Futures(f.e.p.TypeOf(x.X)) {
			return f.element(x)
		}
		vals := f.operands([]syntax.Expr{x.X, x.Index})
		fault := f.temp()
		vals[1] = f.widened(x, vals[1])
		if key := f.e.p.Operation(x).Bindings["K"]; key != nil {
			p := f.e.project(key, vals[1])
			f.line("%s := rlr.CheckKey(%s, %s)", fault, vals[0], p)
			return attempt{results: []string{f.e.entryValue(key, vals[0], p)}, fault: fault}
		}
		vals[1] = f.indexKey(x.Index, vals[1])
		if f.e.p.Retained(x) {
			return attempt{results: []string{vals[0] + "[" + vals[1] + "]"}}
		}
		f.line("%s := rlr.CheckIndex(%s, %s)", fault, vals[0], vals[1])
		return attempt{results: []string{vals[0] + "[" + vals[1] + "]"}, fault: fault}
	case *syntax.Slice:
		return f.slice(x, keep)
	case *syntax.Format:
		return f.format(x, keep)
	case *syntax.Prim:
		if op := f.arithmetic(x); op != nil {
			return f.builtin(op, x, x.Args, keep)
		}
		return attempt{results: []string{f.prim(x)}}
	default:
		panic(fmt.Sprintf("gen: unexpected fallible expression %T", x))
	}
}

// invoke writes a call of a routine with a uniform error result, testing the error only when the callee's raise set is not empty.
func (f *fn) invoke(call string, results int, site check.RaiseSet, callee *syntax.Func, keep bool) attempt {
	var a attempt
	a.callee = callee
	var left []string
	for range results {
		if keep {
			t := f.temp()
			a.results = append(a.results, t)
			left = append(left, t)
		} else {
			left = append(left, "_")
		}
	}
	if !site.Empty() {
		a.err = f.temp()
		left = append(left, a.err)
	} else {
		left = append(left, "_")
	}
	switch {
	case len(a.results) == 0 && a.err == "" && results == 0:
		f.line("%s", call)
	case len(a.results) == 0 && a.err == "":
		f.line("%s = %s", strings.Join(left, ", "), call)
	default:
		f.line("%s := %s", strings.Join(left, ", "), call)
	}
	return a
}

// call writes a call of a declared function, a function value, a type-level method, or a built-in.
func (f *fn) call(x *syntax.Call, keep bool) attempt {
	if op := f.e.p.Operation(x); op != nil {
		if len(x.Args) > 0 && check.Futures(f.e.p.TypeOf(x.Args[0].Value)) {
			if op.Row.Name == "append" {
				return f.appendFuture(x, op.Bindings["T"])
			}
			return attempt{results: []string{"int64(" + f.expr(x.Args[0].Value) + ".Len())"}}
		}
		return f.builtin(op, x, argValues(x.Args), keep)
	}
	results := len(f.e.p.Results(x))
	if fn := f.e.p.CalleeOf(x).Func(); fn != nil {
		args := f.args(x.Args, fn, false)
		return f.invoke(f.e.funcName(fn)+"("+args+")", results, f.e.p.Site(x), fn, keep)
	}
	ft := f.e.p.TypeOf(x.Callee).(*syntax.FuncType)
	vals := f.operands(append([]syntax.Expr{x.Callee}, argValues(x.Args)...))
	for i, a := range x.Args {
		vals[i+1] = f.e.convert(f.e.p.TypeOf(a.Value), ft.Params[i], f.copy(a.Value, vals[i+1]))
	}
	return f.invoke(vals[0]+"("+strings.Join(vals[1:], ", ")+")", results, f.e.p.Site(x), nil, keep)
}

// methodCall writes a call on a receiver: a struct method on the receiver's place, a type-level method of a struct, or a built-in method.
func (f *fn) methodCall(x *syntax.MethodCall, keep bool) attempt {
	if op := f.e.p.Operation(x); op != nil {
		return f.builtin(op, x, append([]syntax.Expr{x.Receiver}, argValues(x.Args)...), keep)
	}
	fn := f.e.p.CalleeOf(x).Func()
	results := len(f.e.p.Results(x))
	if f.e.t.Of(fn).Level == resolve.TypeLevel {
		args := f.args(x.Args, fn, false)
		return f.invoke(f.e.funcName(fn)+"("+args+")", results, f.e.p.Site(x), fn, keep)
	}
	if f.e.p.PlaceSelectors(x) != nil {
		return f.placeCall(x, fn, keep)
	}
	recv := f.receiver(x.Receiver)
	args := f.args(x.Args, fn, f.throughBox(x.Receiver))
	return f.invoke(recv+"."+f.e.funcName(fn)+"("+args+")", results, f.e.p.Site(x), fn, keep)
}

// placeCall writes a method call on an element or entry receiver as one operation: the index and key operands, the arguments, the resolution of the place, the call through copies of the dictionary entries on the path, and their write-back, where a failed resolution is the fault of the attempt and skips the call.
func (f *fn) placeCall(x *syntax.MethodCall, fn *syntax.Func, keep bool) attempt {
	var checks []string
	recv := f.mutand(x.Receiver, &checks)
	copies := f.takeCopies()
	args := f.args(x.Args, fn, f.throughBox(x.Receiver))
	a := attempt{callee: fn, fault: f.temp(), err: f.temp()}
	f.line("%s := %s", a.fault, checks[0])
	for _, c := range checks[1:] {
		f.line("if %s == rlr.NoFault {", a.fault)
		if line, ok := strings.CutPrefix(c, copyLine); ok {
			f.line("%s", line)
		} else {
			f.line("%s = %s", a.fault, c)
		}
		f.line("}")
	}
	var left []string
	for _, t := range f.e.p.Results(x) {
		if !keep {
			left = append(left, "_")
			continue
		}
		r := f.temp()
		f.line("var %s %s", r, f.e.goType(t))
		a.results = append(a.results, r)
		left = append(left, r)
	}
	f.line("var %s error", a.err)
	f.line("if %s == rlr.NoFault {", a.fault)
	f.reread(copies)
	f.line("%s = %s.%s(%s)", strings.Join(append(left, a.err), ", "), recv, f.e.funcName(fn), args)
	f.writeBack(copies)
	f.line("}")
	return a
}

// mutand returns the Go place of a method receiver path as place does, where a receiver that is itself a dictionary entry is a copy of the entry, because a Go map element is not addressable.
func (f *fn) mutand(x syntax.Expr, checks *[]string) string {
	if ix, ok := x.(*syntax.IndexRead); ok {
		if _, dict := f.e.p.TypeOf(ix.X).(*syntax.Dict); dict {
			return f.entry(ix, checks)
		}
	}
	return f.place(x, checks)
}

// receiver returns the addressable Go place of a method receiver, so that the method changes the value the receiver names.
func (f *fn) receiver(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.Self:
		return "self"
	case *syntax.Name, *syntax.FieldRead:
		return f.place(x, nil)
	case *syntax.IndexRead:
		if _, dict := f.e.p.TypeOf(x.X).(*syntax.Dict); dict {
			f.e.internal(x, "a method call on a dictionary entry did not go through placeCall")
			return "_"
		}
		return f.place(x, nil)
	}
	v := f.expr(x)
	t := f.temp()
	f.line("%s := %s", t, v)
	return t
}

func argValues(args []*syntax.Arg) []syntax.Expr {
	out := make([]syntax.Expr, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// args returns the Go argument list of a call of a declared routine: the invocation of the caller, then the kernel arguments, positional in parameter order, each converted to its parameter type and copied when its mode is a copy, and settled when the receiver lies under a box.
func (f *fn) args(args []*syntax.Arg, fn *syntax.Func, settled bool) string {
	xs, vs := f.argList(args, fn)
	if settled {
		f.settle(xs, vs)
	}
	return strings.Join(append([]string{invocation}, vs...), ", ")
}

// argList returns the kernel arguments of a call of a declared routine and their converted Go values.
func (f *fn) argList(args []*syntax.Arg, fn *syntax.Func) ([]syntax.Expr, []string) {
	xs := argValues(args)
	out := f.operands(xs)
	for i, x := range xs {
		out[i] = f.e.convert(f.e.p.TypeOf(x), f.e.t.Type(fn.Params[i].Type), f.copy(x, out[i]))
	}
	return xs, out
}

// converted returns the Go expression of an argument of a built-in call after the conversion that the typer recorded for it.
func (e *emitter) converted(c check.Conversion, v string) string {
	switch c.Kind {
	case check.Widening:
		return e.convert(c.From, c.To, v)
	case check.KeyProjection:
		return e.project(c.To, e.convert(c.From, c.To, v))
	case check.Display:
		return e.display(c.From, v)
	case check.Address:
		return "&" + v
	}
	return v
}

// builtin writes a call of the rlr function of a built-in operation, which is the in-place function of the row when the call consumes its first argument: the formatting module first where the row takes one, each argument under its recorded conversion, and the functions of the row's extra parameters after the arguments, where a receiver taken by address under a dictionary entry is a copy of the entry that is written back after the call.
func (f *fn) builtin(op *check.Operation, at syntax.Expr, args []syntax.Expr, keep bool) attempt {
	row := op.Row
	name, extras := f.e.rowFunction(op)
	if name == "" {
		f.e.internal(at, fmt.Sprintf("the built-in %s has no function in rlr for these arguments", row.Name))
		return attempt{results: []string{"nil"}}
	}
	var goArgs []string
	if slices.Contains(row.Extra, builtin.Formatting) {
		goArgs = append(goArgs, strconv.Quote(f.module))
	}
	if row.InPlaceGo != "" && f.e.modes.Of(args[0]) == check.Consume {
		key := op.Bindings["K"]
		name, extras = row.InPlaceFunction(op.Variant, key != nil && f.e.projected(key))
	}
	var vals []string
	var copies []entryCopy
	if len(args) > 0 && op.Arguments[0].Kind == check.Address && f.e.throughEntry(args[0]) {
		outer := f.takeCopies()
		recv := f.mutand(args[0], nil)
		copies, f.copies = f.takeCopies(), outer
		vals = append([]string{recv}, f.operands(args[1:])...)
	} else {
		vals = f.operands(args)
	}
	for i, v := range vals {
		c := op.Arguments[i]
		switch {
		case row.Name == "r::report" && f.relayed[v] != "":
			v = f.relayed[v]
		case c.Kind == check.Address && !isTemp(v) && !isPlace(args[i]):
			v = f.e.converted(c, f.spillText(v))
		default:
			v = f.e.converted(c, v)
		}
		goArgs = append(goArgs, v)
	}
	for _, x := range extras {
		switch x {
		case builtin.Projection:
			goArgs = append(goArgs, f.e.projectFn(op.Bindings["K"]))
		case builtin.Clone:
			goArgs = append(goArgs, f.e.copier(op))
		case builtin.Equal:
			goArgs = append(goArgs, f.e.equalFn(op.Bindings["T"]))
		case builtin.Less:
			goArgs = append(goArgs, f.e.lessFn(op.Bindings["T"]))
		case builtin.Caller:
			goArgs = append(goArgs, invocation)
		case builtin.Formatting:
		default:
			panic(fmt.Sprintf("gen: unexpected extra parameter %d of %s", x, row.Name))
		}
	}
	f.reread(copies)
	a := f.rlrCall("rlr."+name+"("+strings.Join(goArgs, ", ")+")", row, keep, f.e.p.Retained(at))
	f.writeBack(copies)
	return a
}

// throughEntry reports whether a place lies under a dictionary entry, which has no address in Go.
func (e *emitter) throughEntry(x syntax.Expr) bool {
	for {
		switch y := x.(type) {
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			if e.p.Operation(y).Bindings["K"] != nil {
				return true
			}
			x = y.X
		default:
			return false
		}
	}
}

// copier returns the copy function that an rlr function of a built-in operation takes for its elements, which is nil for the in-place function of a block copy.
func (e *emitter) copier(op *check.Operation) string {
	if op.Variant == builtin.BlockCopy {
		return "nil"
	}
	return e.cloneFn(op.Element())
}

// rowFunction returns the rlr function of the variant of a built-in operation with its extra parameters, where a nullable element type selects the flat function of the row and a dictionary with projected keys its keyed function, or an empty name when rlr has no function for these bindings.
func (e *emitter) rowFunction(op *check.Operation) (string, []builtin.Extra) {
	row := op.Row
	name, extras := row.Function(op.Variant)
	_, nullable := op.Element().(*syntax.Nullable)
	flat := nullable && row.FlatGo != ""
	if flat {
		name = row.FlatGo
	}
	if key := op.Bindings["K"]; name != "" && key != nil && e.projected(key) {
		name, extras = row.KeyedGo, row.KeyedExtra
		if flat {
			name = row.FlatKeyedGo
		}
	}
	return name, extras
}

// isPlace reports whether an expression reads a place, whose Go expression has an address.
func isPlace(x syntax.Expr) bool {
	switch x.(type) {
	case *syntax.Name, *syntax.Self, *syntax.FieldRead, *syntax.IndexRead:
		return true
	}
	return false
}

// rlrCall writes the call of a row's rlr function by the results the row has: its values, and the fault it raises when its fault set is not empty, where the fault of a retained call is discarded, because the compiler has evaluated the call.
func (f *fn) rlrCall(call string, row *builtin.Row, keep, retained bool) attempt {
	var a attempt
	var left []string
	for range row.Results {
		if keep {
			t := f.temp()
			a.results = append(a.results, t)
			left = append(left, t)
		} else {
			left = append(left, "_")
		}
	}
	switch {
	case len(row.Faults) > 0 && retained:
		f.line("%s := %s", strings.Join(append(left, "_"), ", "), call)
	case len(row.Faults) > 0:
		a.fault = f.temp()
		f.line("%s := %s", strings.Join(append(left, a.fault), ", "), call)
	case keep && len(left) == 1 && !row.Standalone:
		a.results = []string{call}
	case len(left) == 0:
		f.line("%s", call)
	case keep:
		f.line("%s := %s", strings.Join(left, ", "), call)
	default:
		f.line("%s = %s", strings.Join(left, ", "), call)
	}
	return a
}

// slice writes a slice through the rlr function of its row, which is the in-place function when the slice consumes its sequence, where an omitted bound is the start or the end of the sequence and the elements of an array are copied.
func (f *fn) slice(x *syntax.Slice, keep bool) attempt {
	op := f.e.p.Operation(x)
	parts := []syntax.Expr{x.X}
	for _, b := range []syntax.Expr{x.Lo, x.Hi} {
		if b != nil {
			parts = append(parts, b)
		}
	}
	vals := f.operands(parts)
	for i := range vals {
		vals[i] = f.e.converted(op.Arguments[i], vals[i])
	}
	lo, hi := "int64(0)", "int64(len("+vals[0]+"))"
	rest := vals[1:]
	if x.Lo != nil {
		lo, rest = rest[0], rest[1:]
	}
	if x.Hi != nil {
		hi = rest[0]
	}
	args := []string{vals[0], lo, hi}
	name, extras := op.Row.Function(op.Variant)
	if op.Row.InPlaceGo != "" && f.e.modes.Of(x.X) == check.Consume {
		name, extras = op.Row.InPlaceFunction(op.Variant, false)
	}
	if slices.Contains(extras, builtin.Clone) {
		args = append(args, f.e.cloneFn(op.Element()))
	}
	return f.rlrCall("rlr."+name+"("+strings.Join(args, ", ")+")", op.Row, keep, f.e.p.Retained(x))
}

// format writes a formatted string through the rlr function of its row, with each slot as its display value.
func (f *fn) format(x *syntax.Format, keep bool) attempt {
	op := f.e.p.Operation(x)
	var slots []syntax.Expr
	for _, p := range x.Parts {
		if p.Slot != nil {
			slots = append(slots, p.Slot)
		}
	}
	vals := f.operands(slots)
	parts := []string{strconv.Quote(f.module)}
	i := 0
	for _, p := range x.Parts {
		if p.Slot == nil {
			parts = append(parts, strconv.Quote(p.Text))
			continue
		}
		parts = append(parts, f.e.converted(op.Arguments[i], vals[i]))
		i++
	}
	return f.rlrCall("rlr."+op.Row.Go+"("+strings.Join(parts, ", ")+")", op.Row, keep, false)
}

// arithmetic returns the operation of an operator whose rlr function computes its value, or nil for a comparison or a Go operator.
func (f *fn) arithmetic(x *syntax.Prim) *check.Operation {
	op := f.e.p.Operation(x)
	if op == nil || op.Row.Go == "" {
		return nil
	}
	switch x.Op {
	case "<", ">", "<=", ">=", "==", "!=":
		return nil
	}
	return op
}

// prim writes an operator: through the rlr function of its row where it has one, as the Go operator otherwise.
func (f *fn) prim(x *syntax.Prim) string {
	if x.Op == "-" && len(x.Args) == 1 {
		if _, ok := x.Args[0].(*syntax.Lit); ok {
			return f.e.literal(x, f.e.p.TypeOf(x))
		}
	}
	if op := f.arithmetic(x); op != nil {
		a := f.builtin(op, x, x.Args, true)
		f.propagate(a)
		return a.results[0]
	}
	if test, ok := f.presenceTest(x); ok {
		return test
	}
	op := f.e.p.Operation(x)
	if op == nil {
		return strconv.FormatBool(x.Op == "==")
	}
	vals := f.operands(x.Args)
	for i := range vals {
		vals[i] = f.e.converted(op.Arguments[i], vals[i])
	}
	if op.Row.Go != "" {
		call := "rlr." + op.Row.Go + "(" + strings.Join(vals, ", ") + ")"
		switch x.Op {
		case "<", ">", "<=", ">=":
			return "(" + call + " " + x.Op + " 0)"
		case "!=":
			return "!" + call
		}
		return call
	}
	switch {
	case len(vals) == 1 && x.Op == "not":
		return "!" + vals[0]
	case len(vals) == 1:
		return "(" + x.Op + vals[0] + ")"
	case x.Op == "==":
		return "(" + f.e.equal(op.Bindings["T"], vals[0], vals[1]) + ")"
	case x.Op == "!=":
		return "!(" + f.e.equal(op.Bindings["T"], vals[0], vals[1]) + ")"
	}
	return "(" + vals[0] + " " + x.Op + " " + vals[1] + ")"
}

// presenceTest writes the comparison of a nullable with the literal none as the presence flag of the nullable, an rlr.Option or an rlr.Box.
func (f *fn) presenceTest(x *syntax.Prim) (string, bool) {
	if len(x.Args) != 2 || x.Op != "==" && x.Op != "!=" {
		return "", false
	}
	for i, a := range x.Args {
		if _, ok := f.e.p.TypeOf(a).(*syntax.Nullable); ok && isNoneLit(x.Args[1-i]) {
			v := f.expr(a)
			if x.Op == "==" {
				return v + ".IsNone()", true
			}
			return v + ".IsSome()", true
		}
	}
	return "", false
}

// isNoneLit reports whether an expression is the literal none.
func isNoneLit(x syntax.Expr) bool {
	lit, ok := x.(*syntax.Lit)
	return ok && lit.Kind == syntax.KwNone
}

// logical writes and and or, evaluating the right operand and its statements only when the left one does not decide the result.
func (f *fn) logical(x *syntax.Logical) string {
	left := f.expr(x.Left)
	var right string
	pre := f.capture(func() { right = f.expr(x.Right) })
	op, test := "&&", ""
	if x.Op == "or" {
		op, test = "||", "!"
	}
	if pre == "" {
		return "(" + left + " " + op + " " + right + ")"
	}
	t := f.temp()
	f.line("%s := %s", t, left)
	f.line("if %s%s {", test, t)
	f.out.WriteString(pre)
	f.line("%s = %s", t, right)
	f.line("}")
	return t
}

// construct writes a struct value or an error value from its arguments in field order, where a catalog error, which a debug build admits, has no fields.
func (f *fn) construct(x *syntax.Construct) string {
	t := f.e.p.TypeOf(x)
	en := f.e.entry(t)
	if en == nil {
		if n, ok := t.(*syntax.Named); ok && n.Module == "" && len(x.Args) == 0 && len(builtin.Lookup(builtin.ErrorRow, n.Name)) > 0 {
			return "rlr.NewError(" + f.e.errorID(t) + ", nil)"
		}
		f.e.internal(x, "the construction has no declared struct or error type")
		return "nil"
	}
	fields := resolve.FieldsOf(en.Decl)
	types := make([]syntax.Type, len(x.Args))
	names := make([]string, len(x.Args))
	for i, a := range x.Args {
		name := a.Name
		if name == "" {
			name = fields[i].Name
		}
		for _, fd := range fields {
			if fd.Name == name {
				types[i] = f.e.t.Type(fd.Type)
			}
		}
		names[i] = name
	}
	vals := f.values(argValues(x.Args), types)
	var inits []string
	for i, v := range vals {
		inits = append(inits, fieldName(names[i])+": "+v)
	}
	value := typeName(en.Module, en.Name) + "{" + strings.Join(inits, ", ") + "}"
	if en.Kind != resolve.ErrorKind {
		return value
	}
	if len(fields) == 0 {
		value = "nil"
	}
	return "rlr.NewError(" + f.e.errorID(t) + ", " + value + ")"
}

// dict writes a dictionary literal as a map filled entry by entry in order.
func (f *fn) dict(x *syntax.DictLit) string {
	t := f.e.p.TypeOf(x).(*syntax.Dict)
	if futureOf(t.Value) != nil {
		return f.futureDict(x, t)
	}
	var xs []syntax.Expr
	var types []syntax.Type
	for _, en := range x.Entries {
		xs = append(xs, en.Key, en.Value)
		types = append(types, t.Key, t.Value)
	}
	vals := f.values(xs, types)
	m := f.temp()
	f.line("%s := make(%s, %d)", m, f.e.goType(t), len(x.Entries))
	for i := 0; i < len(vals); i += 2 {
		p := f.e.project(t.Key, vals[i])
		f.line("%s[%s] = %s", m, p, f.e.stored(t, m, p, vals[i], vals[i+1]))
	}
	return m
}

var errorMembers = map[string]string{
	"Error!.history": "rlr.History(%s)", "ErrorStep.typ": "%s.Type()", "ErrorStep.location": "%s.Location()",
	"ErrorStep.fieldVals": "%s.FieldVals()", "ErrorType.module": "%s.Module", "ErrorType.name": "%s.Name",
	"ErrorType.fields": "%s.Fields", "ErrorField.name": "%s.Name", "ErrorField.typeName": "%s.TypeName",
	"ErrorLocation.revision": "%s.Revision", "ErrorLocation.module": "%s.Module",
	"ErrorLocation.typeName": "%s.TypeName", "ErrorLocation.function": "%s.Function",
}

// field writes the read of a field of a struct, of the payload of an error, or of a built-in member.
func (f *fn) field(x *syntax.FieldRead) string {
	t := f.e.p.TypeOf(x.X)
	v := f.expr(x.X)
	if en := f.e.entry(t); en != nil && en.Kind == resolve.ErrorKind {
		for _, fd := range resolve.FieldsOf(en.Decl) {
			if fd.Name == x.Name {
				return "rlr.Payload(" + v + ").(" + typeName(en.Module, en.Name) + ")." + fieldName(x.Name)
			}
		}
	}
	if f.e.structOf(t) != nil {
		return v + "." + fieldName(x.Name)
	}
	owner := builtinName(t)
	if isError(t) {
		owner = "Error!"
	}
	if form, ok := errorMembers[owner+"."+x.Name]; ok {
		return fmt.Sprintf(form, v)
	}
	f.e.unsupported(x)
	return "nil"
}

// operands evaluates sibling expressions in order and writes the statements of each at its place, where an operand is read in the expression that uses it, because the kernel program stores every operand that a later sibling can change.
func (f *fn) operands(xs []syntax.Expr) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = f.expr(x)
	}
	return out
}

// stable reports whether the value of a Go expression cannot change before it is used: a temporary, a literal, or a local of a type that no method changes in place.
func (f *fn) stable(x syntax.Expr, v string) bool {
	if isTemp(v) {
		return true
	}
	switch x := x.(type) {
	case *syntax.Lit:
		return true
	case *syntax.Prim:
		_, lit := x.Args[0].(*syntax.Lit)
		return x.Op == "-" && len(x.Args) == 1 && lit
	case *syntax.Name:
		t, ok := f.local(x.Name)
		_, scalar := scalars[builtinName(t)]
		return ok && scalar
	}
	return false
}

// stableExpr returns a Go expression of a value that is read more than once, in a temporary unless it is stable.
func (f *fn) stableExpr(x syntax.Expr) string {
	v := f.expr(x)
	if f.stable(x, v) {
		return v
	}
	return f.spillText(v)
}

// widened returns the Go expression of the index or key operand of an index selector under the type that its container requires.
func (f *fn) widened(x *syntax.IndexRead, v string) string {
	c := f.e.p.Operation(x).Arguments[1]
	return f.e.convert(c.From, c.To, v)
}

// indexKey returns the Go text of an array index, stored in a temporary when the index is a literal, because Go rejects a constant negative index at compile time where Ritchie raises OutOfRange! at run time after the check that precedes every index.
func (f *fn) indexKey(x syntax.Expr, v string) string {
	if literalIndex(x) {
		return f.spillText(v)
	}
	return v
}

// literalIndex reports whether an index expression is a literal or a negated literal.
func literalIndex(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Lit:
		return true
	case *syntax.Prim:
		if x.Op != "-" || len(x.Args) != 1 {
			return false
		}
		_, lit := x.Args[0].(*syntax.Lit)
		return lit
	}
	return false
}

// spillText stores a Go expression in a temporary and returns the temporary.
func (f *fn) spillText(v string) string {
	t := f.temp()
	f.line("%s := %s", t, v)
	return t
}

func isTemp(v string) bool {
	if len(v) < 2 || v[0] != 't' {
		return false
	}
	_, err := strconv.Atoi(v[1:])
	return err == nil
}

// copy returns an independent copy of a value that reads a place or a constant when the mode of the operand is a copy, so that a store, a declaration, or an argument creates no aliasing, and the value itself under every other mode.
func (f *fn) copy(x syntax.Expr, v string) string {
	switch x := x.(type) {
	case *syntax.Name:
		if _, ok := f.local(x.Name); !ok && !f.e.p.Collection(x) {
			return v
		}
	case *syntax.Self, *syntax.FieldRead, *syntax.IndexRead:
	case *syntax.Wait:
		if _, sent := x.X.(*syntax.Send); sent {
			return v
		}
	default:
		return v
	}
	if f.e.modes.Of(x) != check.Copy {
		return v
	}
	return f.e.clone(f.e.p.TypeOf(x), v)
}
