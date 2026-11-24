// This file maps the kernel forms of a read in flight to the group, the tasks, and the
// futures of rlr: a send, a wait, a join, and the locals and collections of future
// types.
package gen

import (
	"fmt"
	"slices"
	"strings"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/syntax"
)

// flies reports whether a routine or closure body holds, outside its closures, a send that no wait takes at its place, so that its Go function opens a group, and whether an arm of such a send returns from the routine.
func flies(body *syntax.Block) (group, leaves bool) {
	syntax.Inspect(body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.Wait:
			if s, ok := n.X.(*syntax.Send); ok {
				syntax.Inspect(s.X, func(m syntax.Node) bool {
					if _, closure := m.(*syntax.Closure); closure {
						return false
					}
					_, sent := m.(*syntax.Send)
					group = group || sent
					return true
				})
				return false
			}
		case *syntax.Send:
			group = true
			leaves = leaves || slices.ContainsFunc(n.Arms, func(a *syntax.SendArm) bool { return a.Return })
		}
		return true
	})
	return group, leaves
}

// open opens the group of the Go function of a body that holds a read in flight or a loop whose reads run ahead, where the deferred cancellation is the part of an exit that no form states: leaving the routine cancels every read that it created and has not waited for.
func (f *fn) open(body *syntax.Block) {
	grouped := false
	if body != nil {
		grouped, f.leaves = flies(body)
	}
	if grouped || body != nil && f.e.pipelined(body) {
		f.e.serial++
		f.group = fmt.Sprintf("g%d", f.e.serial)
		f.line("%s := rlr.Open(%s)\ndefer %s.Cancel()", f.group, invocation, f.group)
	}
}

// futureOf returns the element type of a future type, or nil for any other type.
func futureOf(t syntax.Type) syntax.Type {
	if f, ok := t.(*syntax.Future); ok {
		return f.Elem
	}
	return nil
}

// futureMap returns the Go type of a dictionary whose values are futures.
func (e *emitter) futureMap(t *syntax.Dict) string {
	value := futureOf(t.Value)
	plain := &syntax.Dict{Key: t.Key, Value: value}
	p, _ := e.projection(t.Key)
	entry := strings.TrimPrefix(e.goType(plain), "map["+p+"]")
	return "rlr.FutureMap[" + p + ", " + entry + ", " + e.goType(value) + "]"
}

// cloner returns the Go function that copies the value of a future of a type, or nil for a value that shares no storage.
func (e *emitter) cloner(t syntax.Type) string {
	if !e.deep(t) {
		return "nil"
	}
	return e.cloneFn(t)
}

// failed writes the test of a wait and the failure of a read that takes effect there, where the failure that an arm of a send turned into a return of the routine returns its values.
func (f *fn) failed(wait string) {
	err := f.temp()
	f.line("if %s := %s; %s != nil {", err, wait, err)
	if f.leaves {
		vs := f.temp()
		values := make([]string, len(f.results), len(f.results)+1)
		for i, r := range f.results {
			values[i] = fmt.Sprintf("%s[%d].(%s)", vs, i, f.e.goType(r))
		}
		f.line("if %s, ok := rlr.Left(%s); ok {\n_ = %s\nreturn %s\n}", vs, err, vs, strings.Join(append(values, "nil"), ", "))
	}
	f.line("%s", f.fail(fmt.Sprintf("rlr.Leave(%s, %d)", f.arrived(err), f.loc)))
	f.line("}")
}

// joinAll writes a join in a routine that holds a read in flight: the wait for every read that the routine has created and not yet waited for, in the order of creation, where the first failure takes effect, and writes none in the function that runs ahead for one iteration of a loop, which waits for no read.
func (f *fn) joinAll() {
	if f.group != "" && f.lead == "" {
		f.failed(f.group + ".Join()")
	}
}

// leave writes the exit of the routine with a failure that arose at the statement: the failure of a local operation, or of a call of a callee that ran in place, before which the routine waits as a join does, so that the first failure in the order of creation takes effect in place of the local one, while the failure of a remote call that the routine waited for at its statement leaves at once, because that wait took the earlier reads first.
func (f *fn) leave(err string, callee *syntax.Func) {
	if !f.e.remote(callee) {
		f.joinAll()
	}
	f.line("%s", f.fail(err))
}

// sender returns the Go variable that dispatches a read: the group of the routine, or the reads of the iteration in the function that runs ahead for one iteration of a loop.
func (f *fn) sender() string {
	if f.lead != "" {
		return f.lead
	}
	return f.group
}

// dispatch writes the dispatch of the read of a send and returns its task, where a send of a loop whose reads run ahead takes the read that its iteration sent ahead.
func (f *fn) dispatch(x *syntax.Send, atOnce bool) string {
	if k, ok := f.kept[x]; ok {
		return f.adopted(k, func() string { return f.read(x.X, atOnce) })
	}
	return f.read(x.X, atOnce)
}

// read writes the dispatch of the call of a send, a remote read or the start of the body of a reader, and returns its task, where atOnce reports that the routine waits for the task at its place.
func (f *fn) read(x syntax.Expr, atOnce bool) string {
	callee := f.e.p.CalleeOf(x).Func()
	if c, ok := x.(*syntax.Call); ok {
		return f.start(c, callee, atOnce)
	}
	_, vs := f.argList(x.(*syntax.ServiceCall).Args, callee)
	t := f.temp()
	f.line("%s := %s.Read(%s, %q, []any{%s})", t, f.sender(), locator(f.e.locatorOf(f.e.t.Of(callee).Owner)), callee.Name, strings.Join(vs, ", "))
	f.line("_ = %s", t)
	return t
}

// start writes the start of the body of a reader as a task of the group and returns the task, where each argument is copied into a local of its own before the start, without lending, because the routine continues while the body runs under the scope of its task, and where a body that the routine waits for at its place runs in place when the group holds no other task.
func (f *fn) start(x *syntax.Call, callee *syntax.Func, atOnce bool) string {
	xs := argValues(x.Args)
	vs := f.operands(xs)
	args := []string{invocation}
	for i, a := range xs {
		p := f.e.t.Type(callee.Params[i].Type)
		v := f.temp()
		f.line("var %s %s = %s", v, f.e.goType(p), f.e.convert(f.e.p.TypeOf(a), p, f.copy(a, vs[i])))
		args = append(args, v)
	}
	results := make([]string, len(f.e.p.Results(x)))
	for i := range results {
		results[i] = f.temp()
	}
	err, t := f.temp(), f.temp()
	run := "Run"
	if atOnce {
		run = "RunAtOnce"
	}
	f.line("%s := %s.%s(func(%s rlr.Invocation) ([]any, error) {", t, f.sender(), run, invocation)
	f.line("%s := %s(%s)", strings.Join(append(slices.Clone(results), err), ", "), f.e.funcName(callee), strings.Join(args, ", "))
	f.line("return []any{%s}, %s", strings.Join(results, ", "), err)
	f.line("})")
	f.line("_ = %s", t)
	return t
}

// send writes a send: the dispatch of its read and the handler that its arms give the task, which a failed response runs at the wait with the values that the arms had at the statement, and returns the task.
func (f *fn) send(x *syntax.Send) string {
	task := f.dispatch(x, false)
	if len(x.Arms) == 0 {
		return task
	}
	results := f.e.p.Results(x.X)
	err := f.temp()
	fallback := "return nil, " + err
	total := true
	var cases []string
	for _, arm := range x.Arms {
		var supplied string
		if arm.Return {
			supplied = "return nil, rlr.Leaving(" + strings.Join(f.spilled(arm.Values, f.results), ", ") + ")"
			total = false
		} else {
			supplied = "return []any{" + strings.Join(f.spilled(arm.Values, results), ", ") + "}, nil"
		}
		if arm.Type == nil || builtinName(f.e.t.Type(arm.Type)) == "Error!" {
			fallback = supplied
		} else {
			cases = append(cases, fmt.Sprintf("case %s:\n%s", f.e.errorID(f.e.t.Type(arm.Type)), supplied))
		}
	}
	f.line("%s.Handle(func(%s error) ([]any, error) {", task, err)
	f.line("switch rlr.TypeIDOf(%s) {\n%s\n}", f.arrived(err), strings.Join(cases, "\n"))
	f.line("%s\n})", fallback)
	if total && fallback != "return nil, "+err {
		f.line("%s.Total()", task)
	}
	return task
}

// spilled returns the Go values of the values of an arm converted to the types, each evaluated at the statement and kept in a temporary.
func (f *fn) spilled(xs []syntax.Expr, types []syntax.Type) []string {
	vals := f.values(xs, types)
	for i, v := range vals {
		vals[i] = f.spillText(v)
	}
	return vals
}

// futures returns the Go futures of the results of a send, one for each result of its read.
func (f *fn) futures(x *syntax.Send) []string {
	task := f.send(x)
	results := f.e.p.Results(x.X)
	out := make([]string, len(results))
	for i, t := range results {
		out[i] = fmt.Sprintf("rlr.Pending[%s](%s, %d)", f.e.goType(t), task, i)
	}
	return out
}

// waitSend writes the wait for a read at the place of its send, without the test of its failure: in a routine that holds no read in flight the call itself, and elsewhere the dispatch, the wait for every earlier read, whose failure leaves the routine, and the outcome of the read.
func (f *fn) waitSend(x *syntax.Send, keep bool) attempt {
	if f.group == "" {
		return f.attempt(x.X, keep)
	}
	callee := f.e.p.CalleeOf(x.X).Func()
	results := f.e.p.Results(x.X)
	a := attempt{err: f.temp(), callee: callee}
	values := "_"
	if keep && len(results) > 0 {
		values = f.temp()
	}
	task := f.dispatch(x, true)
	f.failed(task + ".Earlier()")
	f.line("%s, %s := %s.Outcome()", values, a.err, task)
	if values != "_" {
		a.results = f.unboxed(values, a.err, results)
	}
	return a
}

// unboxed declares a Go variable for each result of a remote call or a task and gives it the value of its position when the call did not fail.
func (f *fn) unboxed(values, err string, results []syntax.Type) []string {
	out := make([]string, len(results))
	for i, t := range results {
		r := f.temp()
		g := f.e.goType(t)
		f.line("var %s %s", r, g)
		f.line("if %s == nil {", err)
		f.line("%s = %s[%d].(%s)", r, values, i, g)
		f.line("}")
		out[i] = r
	}
	return out
}

// wait writes a wait for a future or for a collection of futures, where a failure takes effect, and returns the Go expression of the value.
func (f *fn) wait(x *syntax.Wait) string {
	t := f.e.p.TypeOf(x.X)
	v := f.expr(x.X)
	if _, local := x.X.(*syntax.Name); !local {
		v = f.spillText(v)
	}
	clone := "nil"
	switch t := t.(type) {
	case *syntax.Future:
		clone = f.e.cloner(t.Elem)
	case *syntax.Array:
		clone = f.e.cloner(futureOf(t.Elem))
	case *syntax.Dict:
		clone = f.e.cloner(futureOf(t.Value))
	}
	f.failed(fmt.Sprintf("%s.Wait(%s)", v, clone))
	return v + ".V"
}

// element returns the Go future of an element of a collection of futures, with the test of its index or key written before it.
func (f *fn) element(x *syntax.IndexRead) attempt {
	vals := f.operands([]syntax.Expr{x.X, x.Index})
	key := f.widened(x, vals[1])
	fault := f.temp()
	switch t := f.e.p.TypeOf(x.X).(type) {
	case *syntax.Dict:
		p := f.e.project(t.Key, key)
		f.line("%s := rlr.CheckKey(%s.V, %s)", fault, vals[0], p)
		return attempt{results: []string{fmt.Sprintf("%s.At(%s, %s, %s)", vals[0], p, f.e.entryValue(t.Key, vals[0]+".V", p), f.e.cloner(futureOf(t.Value)))}, fault: fault}
	case *syntax.Array:
		key = f.indexKey(x.Index, key)
		f.line("%s := rlr.CheckIndex(%s.V, %s)", fault, vals[0], key)
		return attempt{results: []string{fmt.Sprintf("%s.At(%s, %s)", vals[0], key, f.e.cloner(futureOf(t.Elem)))}, fault: fault}
	}
	panic(fmt.Sprintf("gen: unexpected collection of futures %T", f.e.p.TypeOf(x.X)))
}

// setElement writes a store of a value into an element of a local collection of futures: the index or key, the value as a future, the test of an index, and the store.
func (f *fn) setElement(target *syntax.IndexRead, value syntax.Expr) {
	base := f.expr(target.X)
	key := f.widened(target, f.stableExpr(target.Index))
	switch t := f.e.p.TypeOf(target.X).(type) {
	case *syntax.Dict:
		p := f.spillText(f.e.project(t.Key, key))
		v := f.spillText(f.values([]syntax.Expr{value}, []syntax.Type{t.Value})[0])
		entry := "rlr.Same[" + f.e.goType(futureOf(t.Value)) + "]"
		if f.e.projected(t.Key) {
			entry = fmt.Sprintf("rlr.EntryOfKeyed(%s.V, %s, %s)", base, p, key)
		}
		f.line("%s.Set(%s, %s, %s)", base, p, v, entry)
	case *syntax.Array:
		key = f.indexKey(target.Index, key)
		v := f.spillText(f.values([]syntax.Expr{value}, []syntax.Type{t.Elem})[0])
		f.check(fmt.Sprintf("rlr.CheckIndex(%s.V, %s)", base, key))
		f.line("%s.Set(%s, %s)", base, key, v)
	}
}

// collected returns the Go value of an array literal whose elements are futures.
func (f *fn) collected(x *syntax.ArrayLit, t *syntax.Array) string {
	types := make([]syntax.Type, len(x.Elems))
	for i := range types {
		types[i] = t.Elem
	}
	vals := f.values(x.Elems, types)
	for i, v := range vals {
		vals[i] = f.spillText(v)
	}
	return fmt.Sprintf("rlr.Collect[%s](%s)", f.e.goType(futureOf(t.Elem)), strings.Join(vals, ", "))
}

// futureDict writes a dictionary literal whose values are futures as a dictionary of futures filled entry by entry in order.
func (f *fn) futureDict(x *syntax.DictLit, t *syntax.Dict) string {
	var xs []syntax.Expr
	var types []syntax.Type
	for _, en := range x.Entries {
		xs = append(xs, en.Key, en.Value)
		types = append(types, t.Key, t.Value)
	}
	vals := f.values(xs, types)
	m := f.temp()
	f.line("var %s %s", m, f.e.futureMap(t))
	for i := 0; i < len(vals); i += 2 {
		p := f.spillText(f.e.project(t.Key, vals[i]))
		entry := "rlr.Same[" + f.e.goType(futureOf(t.Value)) + "]"
		if f.e.projected(t.Key) {
			entry = fmt.Sprintf("rlr.EntryOfKeyed(%s.V, %s, %s)", m, p, vals[i])
		}
		f.line("%s.Set(%s, %s, %s)", m, p, vals[i+1], entry)
	}
	return m
}

// appendFuture writes a call of append on an array of futures, in place when the call consumes its first argument, and on a copy of the array otherwise.
func (f *fn) appendFuture(x *syntax.Call, elem syntax.Type) attempt {
	args := argValues(x.Args)
	vals := f.operands(args)
	array := vals[0]
	if f.e.modes.Of(args[0]) != check.Consume {
		array = f.spillText(fmt.Sprintf("%s.Copy(%s)", vals[0], f.e.cloner(futureOf(elem))))
	}
	v := f.spillText(f.e.convert(f.e.p.TypeOf(args[1]), elem, f.copy(args[1], vals[1])))
	f.line("%s.Append(%s)", array, v)
	return attempt{results: []string{array}}
}

// futureLoop writes an iterator loop over a local collection of futures, which holds the collection as it is at the one evaluation of its source and binds the future of each element to its last variable.
func (f *fn) futureLoop(s *syntax.IterFor) {
	types := f.e.p.LoopVarTypes(s)
	src := f.spillText(f.expr(s.Source))
	last := len(s.Vars) - 1
	clone := f.e.cloner(futureOf(types[last]))
	switch t := f.e.p.TypeOf(s.Source).(type) {
	case *syntax.Array:
		i := f.temp()
		f.goLoop(fmt.Sprintf("for %s := range %s.Len() {", i, src), s.Label, s.Body, func() {
			f.line("%s := %s.At(int64(%s), %s)", localName(s.Vars[0]), src, i, clone)
			f.declare(s.Vars[0], types[0], true)
		})
	case *syntax.Dict:
		p, entry := f.temp(), f.temp()
		f.goLoop(fmt.Sprintf("for %s, %s := range %s.V {", p, entry, src), s.Label, s.Body, func() {
			key, known := p, entry
			if f.e.projected(t.Key) {
				key, known = entry+".Key", entry+".Val"
			}
			if last == 1 {
				f.line("%s := %s", localName(s.Vars[0]), key)
				f.declare(s.Vars[0], types[0], true)
			}
			f.line("%s := %s.At(%s, %s, %s)", localName(s.Vars[last]), src, p, known, clone)
			f.declare(s.Vars[last], types[last], true)
		})
	}
}

// future returns the Go expression of a value at a position that requires a future type or a collection of futures: the value itself when it has that type, and the future or the collection that holds a value of any other type.
func (e *emitter) future(from, to syntax.Type, x string) string {
	switch to := to.(type) {
	case *syntax.Future:
		if futureOf(from) == nil {
			return "rlr.Ready[" + e.goType(to.Elem) + "](" + e.convert(from, to.Elem, x) + ")"
		}
	case *syntax.Array:
		if !check.Futures(from) {
			return "rlr.ReadyAll[" + e.goType(futureOf(to.Elem)) + "](" + x + ")"
		}
	case *syntax.Dict:
		if !check.Futures(from) {
			return strings.Replace(e.futureMap(to), "rlr.FutureMap", "rlr.ReadyMap", 1) + "(" + x + ")"
		}
	}
	return x
}
