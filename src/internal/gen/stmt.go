// This file writes routines and statements: the uniform trailing error result, scopes,
// loops with their labels, matches as tagless switches, and outcome matches on the
// dynamic error type.
package gen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// fn is the state of the Go function written for one routine or closure, with the Go variable of the group of its reads when its body holds a read in flight, whether an arm of a send returns from the routine, where the body of a loop whose reads run ahead finds what an iteration kept, and, in the function that runs ahead for one iteration of such a loop, the Go variables of its reads and of its record and the entry of the loop.
type fn struct {
	e       *emitter
	out     *strings.Builder
	module  string
	loc     int
	raiser  string
	results []syntax.Type
	scopes  []map[string]syntax.Type
	loops   []*loop
	caught  []string
	relayed map[string]string
	self    syntax.Type
	copies  []entryCopy
	group   string
	leaves  bool
	kept    map[syntax.Node]kept
	lead    string
	rec     string
	table   *check.Ahead
}

// entryCopy is the copy of a dictionary entry that a mutand path changes through: a Go map element is not addressable, so a store or a method call through the entry copies the entry into a temporary after its key is validated, copies it again just before the change so that the change sees every earlier write, changes the temporary, and writes it back.
type entryCopy struct {
	read string
	back string
}

const copyLine = "\x00"

// validate writes one element of a collected validation list: the test of a fault, or the copy of an entry that the next validations read.
func (f *fn) validate(c string) {
	if line, ok := strings.CutPrefix(c, copyLine); ok {
		f.line("%s", line)
		return
	}
	f.check(c)
}

// loop is one Go loop with its label and whether a transfer names the label.
type loop struct {
	label  string
	source string
	used   bool
}

func (f *fn) line(format string, args ...any) {
	fmt.Fprintf(f.out, format, args...)
	f.out.WriteByte('\n')
}

// capture returns the Go text that write emits and keeps it out of the function body.
func (f *fn) capture(write func()) string {
	saved := f.out
	var b strings.Builder
	f.out = &b
	write()
	f.out = saved
	return b.String()
}

// temp returns a fresh Go name for a temporary.
func (f *fn) temp() string {
	f.e.serial++
	return "t" + strconv.Itoa(f.e.serial)
}

func (f *fn) push() { f.scopes = append(f.scopes, map[string]syntax.Type{}) }

func (f *fn) pop() { f.scopes = f.scopes[:len(f.scopes)-1] }

// declare enters a local into the innermost scope, marking it used for Go when Ritchie lets it stay unread.
func (f *fn) declare(name string, t syntax.Type, unread bool) {
	f.scopes[len(f.scopes)-1][name] = t
	if unread {
		f.line("_ = %s", localName(name))
	}
}

// local returns the type of a local name in scope, or false for a top-level name.
func (f *fn) local(name string) (syntax.Type, bool) {
	for i := len(f.scopes) - 1; i >= 0; i-- {
		if t, ok := f.scopes[i][name]; ok {
			return t, true
		}
	}
	return nil, false
}

// routine writes the Go function of a free function or method: a pointer-receiver method for a struct method with self, a Go function otherwise.
func (e *emitter) routine(d *syntax.Func) {
	en := e.t.Of(d)
	module := d.Where().File.Module
	owner := ""
	if en.Owner != nil {
		owner = en.Owner.Name
	}
	f := &fn{e: e, out: &e.decls, module: module, loc: e.location(module, owner, d.Name)}
	f.push()
	params := []string{invocation + " rlr.Invocation"}
	var args []string
	for _, p := range d.Params {
		t := e.t.Type(p.Type)
		f.scopes[0][p.Name] = t
		params = append(params, localName(p.Name)+" "+e.goType(t))
		args = append(args, fmt.Sprintf("rlr.Argument{Name: %q, Value: %s}", p.Name, e.display(t, localName(p.Name))))
	}
	f.raiser = fmt.Sprintf("rlr.Snapshot(%s)", strings.Join(append([]string{strconv.Itoa(f.loc)}, args...), ", "))
	for _, r := range d.Results {
		f.results = append(f.results, e.t.Type(r))
	}
	head := "func " + e.funcName(d)
	if en.Owner != nil && en.Level != resolve.TypeLevel {
		f.self = &syntax.Named{Module: en.Owner.Module, Name: en.Owner.Name}
		head = fmt.Sprintf("func (self *%s) %s", typeName(en.Owner.Module, en.Owner.Name), e.funcName(d))
	}
	f.line("%s(%s) %s {", head, strings.Join(params, ", "), f.signature())
	f.open(d.Body)
	f.stmts(d.Body.Stmts)
	f.end()
	f.line("}\n")
}

// signature returns the named results of the Go function, whose last result is the error.
func (f *fn) signature() string {
	var parts []string
	for i, r := range f.results {
		parts = append(parts, fmt.Sprintf("r%d %s", i, f.e.goType(r)))
	}
	return "(" + strings.Join(append(parts, "err error"), ", ") + ")"
}

// end closes a body: a routine without results returns no error, and one with results has returned on every path.
func (f *fn) end() {
	if len(f.results) == 0 {
		f.line("return nil")
	} else {
		f.line("panic(\"unreachable\")")
	}
}

// fail returns the Go return statement that leaves the routine with an error, or that ends the function that runs ahead for one iteration, whose body raises the failure when it reaches the operation.
func (f *fn) fail(err string) string {
	if f.lead != "" {
		return "return"
	}
	return "return " + strings.Join(append(resultNames(len(f.results)), err), ", ")
}

// resultNames returns the Go names of the first n results of a routine.
func resultNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = "r" + strconv.Itoa(i)
	}
	return names
}

// passOn returns the error a routine passes on when a call it made failed: the history extended at this routine, crossing a module boundary first.
func (f *fn) passOn(err string, callee *syntax.Func) string {
	return f.extend(f.received(err, callee), callee)
}

// extend returns a received error with the propagation step of this routine, which a system error that a service call raised here does not add.
func (f *fn) extend(received string, callee *syntax.Func) string {
	if f.e.remote(callee) {
		return fmt.Sprintf("rlr.Leave(%s, %d)", received, f.loc)
	}
	return fmt.Sprintf("rlr.Bubble(%s, %d)", received, f.loc)
}

// received returns the error of a failed call as this routine receives it, with the callee module's history compacted when the callee is declared in another module, and a system error of a service call raised here.
func (f *fn) received(err string, callee *syntax.Func) string {
	if f.e.remote(callee) {
		return f.arrived(err)
	}
	x := err + ".(rlr.RitchieError)"
	if callee != nil && callee.Where().File.Module != f.module {
		x = fmt.Sprintf("rlr.Cross(%s, %q)", x, f.module)
	}
	return x
}

// arrived returns the failure of a remote call as this routine receives it, with a system error of the call raised here.
func (f *fn) arrived(err string) string {
	return fmt.Sprintf("rlr.Arrive(%s, %q, %d, %s)", err, f.module, f.loc, f.raiser)
}

// stmts writes a statement list, leaving out each declaration that unreadCopy finds.
func (f *fn) stmts(list []syntax.Stmt) {
	for i, s := range list {
		if d, ok := s.(*syntax.VarDecl); ok && unreadCopy(d, list[i+1:]) {
			continue
		}
		f.stmt(s)
	}
}

// unreadCopy reports whether a declaration copies a path into one local that no later statement of its block reads, as the binding of a lowered source match does when its arm does not read it, so that the declaration is not written: the copy has no effect, and Go rejects an unused local.
func unreadCopy(d *syntax.VarDecl, later []syntax.Stmt) bool {
	if len(d.Targets) != 1 || len(d.Values) != 1 || !isPath(d.Values[0]) {
		return false
	}
	return !slices.ContainsFunc(later, func(s syntax.Stmt) bool { return reads(s, d.Targets[0].Name) })
}

// isPath reports whether an expression is a local or self followed by field steps.
func isPath(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Name:
		return x.Module == "" && x.Owner == nil
	case *syntax.Self:
		return true
	case *syntax.FieldRead:
		return isPath(x.X)
	}
	return false
}

// stmt writes one kernel statement.
func (f *fn) stmt(s syntax.Stmt) {
	switch s := s.(type) {
	case *syntax.VarDecl:
		f.varDecl(s)
	case *syntax.ConstDecl:
		f.constDecl(s)
	case *syntax.Store:
		if k, ok := f.kept[s]; ok {
			f.taken(s, k, func() { f.store(s) })
		} else {
			f.store(s)
		}
	case *syntax.ExprStmt:
		f.discard(s.X)
	case *syntax.For:
		f.forLoop(s)
	case *syntax.IterFor:
		f.iterLoop(s)
	case *syntax.Join:
		f.joinAll()
	case *syntax.Break:
		l := f.target(s.Label)
		l.used = true
		f.line("break %s", l.label)
	case *syntax.Continue:
		l := f.target(s.Label)
		switch {
		case s.Label == "":
			f.line("continue")
		default:
			l.used = true
			f.line("continue %s", l.label)
		}
	case *syntax.Return:
		f.ret(s)
	case *syntax.Block:
		f.line("{")
		f.push()
		f.stmts(s.Stmts)
		f.pop()
		f.line("}")
	case *syntax.Match:
		if k, ok := f.kept[s]; ok {
			f.selected(s, k)
		} else {
			f.match(s)
		}
	default:
		panic(fmt.Sprintf("gen: unexpected statement %T", s))
	}
}

// varDecl declares each target after evaluating the values, from one value per target or from the results of one call, and needs no read of its own, because the checker rejects an unread local, except for a constant, every use of which the lowering may have substituted.
func (f *fn) varDecl(s *syntax.VarDecl) {
	if k, ok := f.kept[s]; ok && k.n > 0 {
		t := s.Targets[0]
		typ := f.e.p.LocalType(t)
		f.line("var %s %s", localName(t.Name), f.e.goType(typ))
		f.taken(s, k, func() {
			_, values := f.declared(s)
			f.line("%s = %s", localName(t.Name), values[0])
		})
		f.declare(t.Name, typ, syntax.ClassifyBare(t.Name) == syntax.Constant)
		return
	}
	types, values := f.declared(s)
	for i, t := range s.Targets {
		if values == nil {
			f.line("var %s %s", localName(t.Name), f.e.goType(types[i]))
		} else {
			f.line("var %s %s = %s", localName(t.Name), f.e.goType(types[i]), values[i])
		}
	}
	for i, t := range s.Targets {
		_, future := types[i].(*syntax.Future)
		f.declare(t.Name, types[i], future || syntax.ClassifyBare(t.Name) == syntax.Constant)
	}
}

// constDecl declares a local constant whose value is a collection, built where its declaration runs, and marks it used for Go, because the lowering may have substituted every use of it.
func (f *fn) constDecl(s *syntax.ConstDecl) {
	t := f.e.p.LocalType(s)
	v := f.values([]syntax.Expr{s.Value}, []syntax.Type{t})[0]
	f.line("var %s %s = %s", localName(s.Name), f.e.goType(t), v)
	f.declare(s.Name, t, true)
}

// declared returns the types of the targets of a declaration and its evaluated values, which are nil for a declaration without an initializer.
func (f *fn) declared(s *syntax.VarDecl) ([]syntax.Type, []string) {
	types := make([]syntax.Type, len(s.Targets))
	for i, t := range s.Targets {
		types[i] = f.e.p.LocalType(t)
	}
	switch {
	case len(s.Values) == 0:
		return types, nil
	case len(s.Values) == 1 && len(s.Targets) > 1:
		return types, f.spread(s.Values[0], types)
	}
	return types, f.values(s.Values, types)
}

// spread evaluates a call of several results, or a send of such a call, into temporaries converted to the types of their targets.
func (f *fn) spread(x syntax.Expr, types []syntax.Type) []string {
	if s, ok := x.(*syntax.Send); ok {
		return f.futures(s)
	}
	results := f.e.p.Results(x)
	out := f.multi(x)
	for i := range out {
		out[i] = f.e.convert(results[i], types[i], out[i])
	}
	return out
}

// values evaluates a value list in order, each converted to the type of its position and copied when it reads a place.
func (f *fn) values(xs []syntax.Expr, types []syntax.Type) []string {
	out := f.operands(xs)
	for i, x := range xs {
		out[i] = f.e.convert(f.e.p.TypeOf(x), types[i], f.copy(x, out[i]))
	}
	return out
}

// store evaluates the index and key operands of the targets from left to right, then the values, and then stores left to right, where a store to one target validates each selector of its path after its own operand and its final index after the value, and a store to several targets validates every place after all values and before the first store.
func (f *fn) store(s *syntax.Store) {
	n := len(s.Targets)
	if ix, ok := s.Targets[0].(*syntax.IndexRead); ok && n == 1 && len(s.Values) == 1 && check.Futures(f.e.p.TypeOf(ix.X)) {
		f.setElement(ix, s.Values[0])
		return
	}
	types := make([]syntax.Type, n)
	places := make([]string, n)
	paths := make([][]string, n)
	finals := make([]string, n)
	entries := make([]func(string) string, n)
	copies := make([][]entryCopy, n)
	for i, t := range s.Targets {
		types[i] = f.e.p.TypeOf(t)
		collect := &paths[i]
		if n == 1 {
			collect = nil
		}
		places[i], finals[i], entries[i] = f.storePlace(t, collect)
		copies[i] = f.takeCopies()
	}
	var values []string
	switch {
	case len(s.Values) == 1 && n > 1:
		values = f.spread(s.Values[0], types)
	default:
		values = f.values(s.Values, types)
		if slices.ContainsFunc(s.Targets, f.throughBox) {
			f.settle(s.Values, values)
		}
	}
	for i, entry := range entries {
		if entry != nil {
			values[i] = entry(values[i])
		}
	}
	if n > 1 {
		for _, path := range paths {
			for _, c := range path {
				f.validate(c)
			}
		}
	}
	copied := slices.ContainsFunc(copies, func(c []entryCopy) bool { return len(c) > 0 })
	if n == 1 && copied {
		f.reread(copies[0])
	}
	for _, c := range finals {
		if c != "" {
			f.check(c)
		}
	}
	if !copied {
		f.line("%s = %s", strings.Join(places, ", "), strings.Join(values, ", "))
		return
	}
	if n > 1 {
		f.settle(s.Values, values)
	}
	for i := range places {
		if n > 1 {
			f.reread(copies[i])
		}
		f.line("%s = %s", places[i], values[i])
		f.writeBack(copies[i])
	}
}

// takeCopies returns the entry copies that the paths translated since the last call need, and clears them.
func (f *fn) takeCopies() []entryCopy {
	out := f.copies
	f.copies = nil
	return out
}

// reread copies each dictionary entry of a mutand path again, outermost first, just before the change, so that the change sees every write since the validation.
func (f *fn) reread(copies []entryCopy) {
	for _, c := range copies {
		f.line("%s", c.read)
	}
}

// writeBack writes each copied dictionary entry of a mutand path back to its dictionary, innermost first.
func (f *fn) writeBack(copies []entryCopy) {
	for i := len(copies) - 1; i >= 0; i-- {
		f.line("%s", copies[i].back)
	}
}

// storePlace returns the Go place of an assignment target with its index and key operands evaluated, collecting the validation of each selector of its path and returning the bounds check of its final index and, for a final key, the function from the stored value to the stored map entry.
func (f *fn) storePlace(x syntax.Expr, checks *[]string) (string, string, func(string) string) {
	ix, ok := x.(*syntax.IndexRead)
	if !ok {
		return f.place(x, checks), "", nil
	}
	base := f.place(ix.X, checks)
	key := f.indexKey(ix.Index, f.widened(ix, f.stableExpr(ix.Index)))
	if dict, ok := f.e.p.TypeOf(ix.X).(*syntax.Dict); ok {
		p := f.e.project(dict.Key, key)
		return base + "[" + p + "]", "", func(v string) string { return f.e.stored(dict, base, p, key, v) }
	}
	return base + "[" + key + "]", fmt.Sprintf("rlr.CheckIndex(%s, %s)", base, key), nil
}

// place returns the Go place of a target path, validating each index and key selector on the way, or collecting the validations when checks is not nil, and selects a field after a dictionary entry on a copy of the entry that the caller writes back through takeCopies and writeBack.
func (f *fn) place(x syntax.Expr, checks *[]string) string {
	switch x := x.(type) {
	case *syntax.Name:
		return f.livePayload(x, localName(x.Name))
	case *syntax.Self:
		return "(*self)"
	case *syntax.FieldRead:
		if ix, ok := x.X.(*syntax.IndexRead); ok {
			if _, dict := f.e.p.TypeOf(ix.X).(*syntax.Dict); dict {
				return f.livePayload(x, f.entry(ix, checks)+"."+fieldName(x.Name))
			}
		}
		return f.livePayload(x, f.place(x.X, checks)+"."+fieldName(x.Name))
	case *syntax.IndexRead:
		base := f.place(x.X, checks)
		key := f.indexKey(x.Index, f.widened(x, f.stableExpr(x.Index)))
		check := fmt.Sprintf("rlr.CheckIndex(%s, %s)", base, key)
		place := base + "[" + key + "]"
		if dict, ok := f.e.p.TypeOf(x.X).(*syntax.Dict); ok {
			p := f.e.project(dict.Key, key)
			check = fmt.Sprintf("rlr.CheckKey(%s, %s)", base, p)
			place = f.e.entryValue(dict.Key, base, p)
		}
		if checks != nil {
			*checks = append(*checks, check)
		} else {
			f.check(check)
		}
		return place
	}
	f.e.unsupported(x)
	return "_"
}

// entry returns a temporary that holds a copy of a dictionary entry of a mutand path, validating its key as place does, and records the copy-out and the write-back of the temporary.
func (f *fn) entry(ix *syntax.IndexRead, checks *[]string) string {
	dict := f.e.p.TypeOf(ix.X).(*syntax.Dict)
	base := f.place(ix.X, checks)
	key := f.widened(ix, f.stableExpr(ix.Index))
	p := f.e.project(dict.Key, key)
	t := f.temp()
	f.line("var %s %s", t, f.e.goType(dict.Value))
	c := entryCopy{
		read: fmt.Sprintf("%s = %s", t, f.e.entryValue(dict.Key, base, p)),
		back: fmt.Sprintf("%s[%s] = %s", base, p, f.e.stored(dict, base, p, key, t)),
	}
	check := fmt.Sprintf("rlr.CheckKey(%s, %s)", base, p)
	if checks != nil {
		*checks = append(*checks, check, copyLine+c.read)
	} else {
		f.check(check)
		f.line("%s", c.read)
	}
	f.copies = append(f.copies, c)
	return t
}

// check writes the test of a built-in fault and the failure it raises at this routine.
func (f *fn) check(fault string) {
	v := f.temp()
	f.line("if %s := %s; %s != rlr.NoFault {", v, fault, v)
	f.leave(fmt.Sprintf("rlr.Origin(%s, %d, %s)", v, f.loc, f.raiser), nil)
	f.line("}")
}

// forLoop writes the unconditional loop.
func (f *fn) forLoop(s *syntax.For) {
	f.goLoop("for {", s.Label, s.Body, nil)
}

// iterLoop writes the iterator loop as a Go range, which holds the array or dictionary that the one evaluation of the source selected, or ranges over the iterator of a range row, and copies the element, or the dictionary value, into its loop variable at each visit unless the variable refers to its element.
func (f *fn) iterLoop(s *syntax.IterFor) {
	if check.Futures(f.e.p.TypeOf(s.Source)) {
		f.futureLoop(s)
		return
	}
	if a := f.e.p.Ahead[s]; a != nil {
		f.pipeline(s, a)
		return
	}
	src := f.loopSource(s)
	r := f.ranged(s, src)
	f.goLoop(r.head, s.Label, s.Body, r.bind)
}

// ranged holds the parts of the Go range of an iterator loop: its header and the statements that bind the loop variables.
type ranged struct {
	head string
	bind func()
}

// ranged returns the Go range of an iterator loop over its evaluated source, which holds the array or dictionary that the one evaluation of the source selected, or ranges over the iterator of a range row, and copies the element, or the dictionary value, into its loop variable at each visit unless the variable refers to its element.
func (f *fn) ranged(s *syntax.IterFor, src string) ranged {
	var r ranged
	types := f.e.p.LoopVarTypes(s)
	vars := make([]string, len(s.Vars))
	for i, v := range s.Vars {
		vars[i] = localName(v)
	}
	copied := len(vars) - 1
	r.head = fmt.Sprintf("for %s := range %s {", strings.Join(vars, ", "), src)
	var fields []string
	switch t := f.e.p.TypeOf(s.Source).(type) {
	case *syntax.Array:
		r.head = fmt.Sprintf("for _, %s := range %s {", vars[0], src)
	case *syntax.Dict:
		if f.e.projected(t.Key) {
			entry := f.temp()
			r.head = fmt.Sprintf("for _, %s := range %s {", entry, src)
			fields = []string{entry + ".Key", entry + ".Val"}
		}
	default:
		copied = -1
	}
	r.bind = func() {
		for i, field := range fields[:min(len(fields), len(vars))] {
			f.line("%s := %s", vars[i], field)
		}
		if copied >= 0 && f.e.deep(types[copied]) && !f.e.modes.Refers(s) {
			f.line("%s = %s", vars[copied], f.e.clone(types[copied], vars[copied]))
		}
		for i, v := range s.Vars {
			f.declare(v, types[i], true)
		}
	}
	return r
}

// loopSource evaluates the source of an iterator loop once, as an independent copy where the table of operand modes states that the loop walks one.
func (f *fn) loopSource(s *syntax.IterFor) string {
	src := f.expr(s.Source)
	if f.e.modes.Snapshot(s) {
		return f.e.clone(f.e.p.TypeOf(s.Source), src)
	}
	return src
}

// goLoop writes a Go loop from its header and the statements that head writes at the top of its body, with a Go label that every break names, since a break inside a Go switch would leave the switch, and that is fresh, since a Go label is scoped to its function.
func (f *fn) goLoop(header, label string, body *syntax.Block, head func()) {
	f.e.serial++
	l := &loop{source: label, label: "L" + strconv.Itoa(f.e.serial)}
	text := f.capture(func() {
		f.loops = append(f.loops, l)
		f.push()
		if head != nil {
			head()
		}
		f.stmts(body.Stmts)
		f.pop()
		f.loops = f.loops[:len(f.loops)-1]
	})
	if l.used {
		f.line("%s:", l.label)
	}
	f.line("%s", header)
	f.out.WriteString(text)
	f.line("}")
}

// target returns the loop that a break or continue leaves: the innermost one, or the one with the label.
func (f *fn) target(label string) *loop {
	for i := len(f.loops) - 1; i >= 0; i-- {
		if label == "" || f.loops[i].source == label {
			return f.loops[i]
		}
	}
	panic(fmt.Sprintf("gen: no loop for label %q", label))
}

// ret writes a return of values, or the raising of an error value, which inside an error arm records the caught error in the history of the new one.
func (f *fn) ret(s *syntax.Return) {
	if len(s.Values) == 1 && isError(f.e.p.TypeOf(s.Values[0])) {
		v := f.expr(s.Values[0])
		raise := fmt.Sprintf("rlr.Raise(%s, %d, %s)", v, f.loc, f.raiser)
		if len(f.caught) > 0 {
			raise = fmt.Sprintf("rlr.RecoveryRaise(%s, %s, %d, %s)", f.caught[len(f.caught)-1], v, f.loc, f.raiser)
		}
		f.line("%s", f.fail(raise))
		return
	}
	var values []string
	if len(s.Values) == 1 && len(f.results) > 1 {
		values = f.spread(s.Values[0], f.results)
	} else {
		values = f.values(s.Values, f.results)
	}
	f.line("return %s", strings.Join(append(values, "nil"), ", "))
}

// match writes a match: an outcome match when its subject is a fallible operation and an arm selects an outcome, the evaluation of a subject that only an any arm follows, a value match, or a match without a subject.
func (f *fn) match(s *syntax.Match) {
	switch {
	case s.Subject == nil:
		f.cases(s.Arms, nil, nil, f.plainArm)
	case fallible(s.Subject) && outcomeArms(s.Arms):
		f.outcome(s)
	case len(s.Arms) == 1 && s.Arms[0].Kind == syntax.AnyArm:
		if fallible(s.Subject) {
			f.discard(s.Subject)
		} else {
			f.line("_ = %s", f.expr(s.Subject))
		}
		f.armBody(s.Arms[0], "", false)
	default:
		t := f.e.p.TypeOf(s.Subject)
		subject := f.stableExpr(s.Subject)
		f.cases(s.Arms, t, func() string { return subject }, f.plainArm)
	}
}

// plainArm writes the body of an arm that binds no value.
func (f *fn) plainArm(arm *syntax.Arm) { f.armBody(arm, "", false) }

// fallible reports whether an expression has a form whose evaluation can fail with an outcome of its own.
func fallible(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Prim, *syntax.Format, *syntax.IndexRead, *syntax.Slice, *syntax.Call, *syntax.MethodCall, *syntax.ServiceCall:
		return true
	case *syntax.Wait:
		_, sent := x.X.(*syntax.Send)
		return sent
	}
	return false
}

// outcomeArms reports whether an arm list has an arm form that only an outcome match admits.
func outcomeArms(arms []*syntax.Arm) bool {
	for _, a := range arms {
		switch a.Kind {
		case syntax.TypedArm, syntax.NoneArm, syntax.AnyErrorArm, syntax.ResultsArm:
			return true
		}
	}
	return false
}

// cases writes arms as a tagless switch in which each candidate is tested only when selection reaches it, and a candidate that needs statements before its test opens a nested switch under default with those statements in front of it, where body writes what the selected arm runs.
func (f *fn) cases(arms []*syntax.Arm, t syntax.Type, subject func() string, body func(*syntax.Arm)) {
	open := false
	clause := func(format string, args ...any) {
		if !open {
			f.line("switch {")
			open = true
		}
		f.line(format, args...)
	}
	for k, arm := range arms {
		switch arm.Kind {
		case syntax.CaseArm:
			var conds []string
			for i, p := range arm.Patterns {
				var c string
				pre := f.capture(func() { c = f.candidate(p, t, subject) })
				if pre != "" && (open || len(conds) > 0) {
					if len(conds) > 0 {
						clause("case %s:", strings.Join(conds, " || "))
						body(arm)
					}
					rest := append([]*syntax.Arm{{Origin: arm.Origin, Kind: syntax.CaseArm, Patterns: arm.Patterns[i:], Body: arm.Body}}, arms[k+1:]...)
					clause("default:")
					f.cases(rest, t, subject, body)
					f.line("}")
					return
				}
				f.out.WriteString(pre)
				conds = append(conds, c)
			}
			clause("case %s:", strings.Join(conds, " || "))
			body(arm)
		case syntax.AnyArm:
			clause("default:")
			body(arm)
		default:
			panic(fmt.Sprintf("gen: unexpected arm kind %d", arm.Kind))
		}
	}
	if open {
		f.line("}")
	}
}

// candidate returns the Go test of one candidate: the condition itself without a subject, an equality with the subject otherwise.
func (f *fn) candidate(p syntax.Expr, t syntax.Type, subject func() string) string {
	if subject == nil {
		return f.expr(p)
	}
	v := f.e.convert(f.e.p.TypeOf(p), t, f.expr(p))
	return f.e.equal(t, subject(), v)
}

// armBody writes the body of an arm in its own scope and first binds the variable of the arm to a value that the body reads, which is a copy of the value unless the binding takes the value itself.
func (f *fn) armBody(arm *syntax.Arm, value string, taken bool) {
	f.push()
	if arm.Binding != "" && value != "" && reads(arm.Body, arm.Binding) {
		t := f.e.p.LocalType(arm)
		if !taken {
			value = f.e.clone(t, value)
		}
		f.line("%s := %s", localName(arm.Binding), value)
		f.declare(arm.Binding, t, true)
	}
	f.body(arm.Body)
	f.pop()
}

// reads reports whether a statement reads a local.
func reads(s syntax.Stmt, local string) bool {
	found := false
	syntax.Inspect(s, func(n syntax.Node) bool {
		if x, ok := n.(*syntax.Name); ok && x.Name == local && x.Module == "" && x.Owner == nil {
			found = true
		}
		return !found
	})
	return found
}

// resultsBody writes the body of a success arm with several bindings in its own scope, binding each target to the result at its position.
func (f *fn) resultsBody(arm *syntax.Arm, results []string) {
	f.push()
	for i, t := range arm.Targets {
		f.line("%s := %s", localName(t.Name), results[i])
		f.declare(t.Name, f.e.p.LocalType(t), true)
	}
	f.body(arm.Body)
	f.pop()
}

// body writes an arm body, whose braces the case clause supplies.
func (f *fn) body(s syntax.Stmt) {
	if b, ok := s.(*syntax.Block); ok {
		f.stmts(b.Stmts)
		return
	}
	if s != nil {
		f.stmt(s)
	}
}

// outcome writes an outcome match: the results and the error of the subject, then a branch on the dynamic type of a failure, where an unselected failure leaves the routine with an extended history, and where the binding of a success arm takes the value of the subject itself unless the mode of the subject is a copy.
func (f *fn) outcome(s *syntax.Match) {
	taken := f.e.modes.Of(s.Subject) != check.Copy
	a := f.attempt(s.Subject, true)
	for _, r := range a.results {
		if isTemp(r) {
			f.line("_ = %s", r)
		}
	}
	err, callee := f.failure(a)
	var success []*syntax.Arm
	var typed []*syntax.Arm
	var anyError *syntax.Arm
	for _, arm := range s.Arms {
		switch {
		case arm.Kind == syntax.AnyErrorArm:
			anyError = arm
		case arm.Kind == syntax.TypedArm && isError(f.e.t.Type(arm.Type)):
			if builtinName(f.e.t.Type(arm.Type)) == "Error!" {
				anyError = arm
			} else {
				typed = append(typed, arm)
			}
		default:
			success = append(success, arm)
		}
	}
	if err != "" {
		f.line("if %s != nil {", err)
		caught := f.temp()
		f.line("%s := %s", caught, f.received(err, callee))
		relay := ""
		if a.err != "" {
			relay = f.extend(caught, callee)
		}
		f.line("switch rlr.TypeIDOf(%s) {", caught)
		for _, arm := range typed {
			f.line("case %s:", f.e.errorID(f.e.t.Type(arm.Type)))
			f.errorArm(arm, caught, relay)
		}
		f.line("default:")
		if anyError != nil {
			f.errorArm(anyError, caught, relay)
		} else {
			f.leave(f.passOn(err, callee), callee)
		}
		f.line("}")
		f.line("} else {")
	}
	var t syntax.Type
	value := ""
	if len(a.results) == 1 {
		t = f.resultTypes(s.Subject)[0]
		value = a.results[0]
	}
	_, optional := t.(*syntax.Nullable)
	f.line("switch {")
	for _, arm := range success {
		switch {
		case optional && arm.Kind == syntax.NoneArm:
			f.line("case %s.IsNone():", value)
			f.armBody(arm, "", false)
		case optional && arm.Kind == syntax.TypedArm && !check.Same(f.e.t.Type(arm.Type), t):
			f.line("case %s.IsSome():", value)
			f.armBody(arm, value+".Value()", taken)
		case arm.Kind == syntax.TypedArm || arm.Kind == syntax.NoneArm:
			f.line("case true:")
			f.armBody(arm, value, taken)
		case arm.Kind == syntax.ResultsArm:
			f.line("case true:")
			f.resultsBody(arm, a.results)
		case arm.Kind == syntax.AnyArm:
			f.line("default:")
			f.armBody(arm, "", false)
		case arm.Kind == syntax.CaseArm:
			var conds []string
			for _, p := range arm.Patterns {
				conds = append(conds, f.candidate(p, t, func() string { return value }))
			}
			f.line("case %s:", strings.Join(conds, " || "))
			f.armBody(arm, "", false)
		}
	}
	f.line("}")
	if err != "" {
		f.line("}")
	}
}

// resultTypes returns the result types of a fallible operation: those of a call, or the one type of an operator, index, slice, or formatted string.
func (f *fn) resultTypes(x syntax.Expr) []syntax.Type {
	switch x.(type) {
	case *syntax.Call, *syntax.MethodCall, *syntax.ServiceCall, *syntax.Wait:
		return f.e.p.Results(x)
	}
	return []syntax.Type{f.e.p.TypeOf(x)}
}

// errorArm writes an error arm with its binding, inside which a raised error records the caught one and a report of the binding records the propagation step of a failed routine call.
func (f *fn) errorArm(arm *syntax.Arm, caught, relay string) {
	f.caught = append(f.caught, caught)
	bound := localName(arm.Binding)
	prior, shadowed := f.relayed[bound]
	if arm.Binding != "" {
		if f.relayed == nil {
			f.relayed = map[string]string{}
		}
		f.relayed[bound] = relay
	}
	f.armBody(arm, caught, false)
	if shadowed {
		f.relayed[bound] = prior
	} else {
		delete(f.relayed, bound)
	}
	f.caught = f.caught[:len(f.caught)-1]
}

// closure writes the Go function literal of a kernel closure, called at once with a copy of each captured local, since a Go closure captures variables and a Ritchie closure captures copies.
func (f *fn) closure(x *syntax.Closure) string {
	ft := f.e.p.TypeOf(x).(*syntax.FuncType)
	inner := &fn{e: f.e, out: &strings.Builder{}, module: f.module, loc: f.loc, raiser: `""`, results: ft.Results}
	inner.push()
	var params []string
	for i, p := range x.Params {
		inner.scopes[0][p.Name] = ft.Params[i]
		params = append(params, localName(p.Name)+" "+f.e.goType(ft.Params[i]))
	}
	captured := map[string]bool{}
	var names, types, values []string
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Name:
			_, param := inner.scopes[0][n.Name]
			if t, ok := f.local(n.Name); ok && n.Module == "" && n.Owner == nil && !captured[n.Name] && !param {
				captured[n.Name] = true
				names = append(names, localName(n.Name))
				types = append(types, f.e.goType(t))
				values = append(values, f.e.clone(t, localName(n.Name)))
				inner.scopes[0][n.Name] = t
			}
		case *syntax.Self:
			if !captured["self"] && f.self != nil {
				captured["self"] = true
				g := f.e.goType(f.self)
				names, types = append(names, "self"), append(types, "*"+g)
				values = append(values, "&[]"+g+"{"+f.e.clone(f.self, "(*self)")+"}[0]")
				inner.self = f.self
			}
		}
		return true
	})
	if x.Body != nil {
		inner.open(x.Body)
		inner.stmts(x.Body.Stmts)
		inner.end()
	} else {
		v := inner.values([]syntax.Expr{x.Expr}, ft.Results)
		inner.line("return %s, nil", v[0])
	}
	var decls []string
	for i := range names {
		decls = append(decls, names[i]+" "+types[i])
	}
	g := f.e.goType(ft)
	return fmt.Sprintf("func(%s) %s {\nreturn func(%s) %s {\n%s}\n}(%s)", strings.Join(decls, ", "), g,
		strings.Join(params, ", "), inner.signature(), inner.out.String(), strings.Join(values, ", "))
}
