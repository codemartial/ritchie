// This file writes the loop pipeline: an iterator loop of the table of loops whose
// reads run ahead, as one Go loop over the iterations that rlr walks ahead of the
// body, with the function that computes and sends ahead for one iteration.
package gen

import (
	"fmt"
	"slices"
	"strings"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/syntax"
)

// kept is where the body of a loop finds what an iteration computed ahead for a send, a statement, or a match: the Go variable of the record of the iteration and the number of the node in the entry of the loop.
type kept struct {
	rec string
	n   int
}

// pipelined reports whether a routine or closure body holds, outside its closures, a loop of the table of loops whose reads run ahead.
func (e *emitter) pipelined(body *syntax.Block) bool {
	found := false
	syntax.Inspect(body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.IterFor:
			found = found || e.p.Ahead[n] != nil
		}
		return !found
	})
	return found
}

// valueType returns the type of the local that a statement of an entry stores a value to.
func (f *fn) valueType(s syntax.Stmt) syntax.Type {
	if d, ok := s.(*syntax.VarDecl); ok {
		return f.e.p.LocalType(d.Targets[0])
	}
	return f.e.p.LocalType(f.e.p.Local(s.(*syntax.Store).Targets[0]))
}

// valueName returns the Go variable of the local that a statement of an entry stores a value to.
func valueName(s syntax.Stmt) string {
	if d, ok := s.(*syntax.VarDecl); ok {
		return localName(d.Targets[0].Name)
	}
	return localName(s.(*syntax.Store).Targets[0].(*syntax.Name).Name)
}

// pipeline writes an iterator loop whose reads run ahead: the type of the record that an iteration keeps, with the count of the values that it computed ahead, each value, the arm that each match selected, and the task of each send, and one Go loop over the iterations of rlr.Ahead, which calls the function written by lead for each element at most the window of the runtime ahead of the body, where an array is walked by index, so that the body reads each element when it reaches it, and the entries of a dictionary and the values of a range are kept with their iteration.
func (f *fn) pipeline(s *syntax.IterFor, a *check.Ahead) {
	f.e.serial++
	typ := fmt.Sprintf("k%d", f.e.serial)
	fields := make([]string, len(a.Values)+len(a.Matches)+len(a.Sends))
	values := 0
	for v, n := range a.Values {
		if n > 0 {
			fields[n-1] = fmt.Sprintf("v%d %s", n, f.e.goType(f.valueType(v)))
			values++
		}
	}
	for _, n := range a.Matches {
		fields[values+n-1] = fmt.Sprintf("m%d int", n)
	}
	for _, n := range a.Sends {
		fields[values+len(a.Matches)+n-1] = fmt.Sprintf("s%d *rlr.Task", n)
	}
	fields = fields[:values+len(a.Matches)+len(a.Sends)]
	if values > 0 {
		fields = append(fields, "n int")
	}
	f.line("type %s struct {\n%s\n}", typ, strings.Join(fields, "\n"))
	src := f.spillText(f.loopSource(s))
	elem, rec, lead := f.temp(), f.temp(), f.temp()
	types := f.e.p.LoopVarTypes(s)
	seq, elemType, parts, copied := src, f.e.goType(types[0]), []string{elem}, -1
	switch t := f.e.p.TypeOf(s.Source).(type) {
	case *syntax.Array:
		seq, elemType, parts, copied = fmt.Sprintf("rlr.Upto(int64(len(%s)))", src), "int64", []string{src + "[" + elem + "]"}, 0
	case *syntax.Dict:
		seq = "rlr.Each(" + src + ")"
		if f.e.projected(t.Key) {
			seq = "rlr.EachKeyed(" + src + ")"
		}
		elemType = "rlr.Keyed[" + f.e.goType(t.Key) + ", " + f.e.goType(t.Value) + "]"
		parts, copied = []string{elem + ".Key", elem + ".Val"}, len(s.Vars)-1
	}
	bind := func(in *fn, clone bool) {
		for i, v := range s.Vars {
			part := parts[i]
			if clone && i == copied && in.e.deep(types[i]) && !in.e.modes.Refers(s) {
				part = in.e.clone(types[i], part)
			}
			in.line("%s := %s", localName(v), part)
			in.declare(v, types[i], true)
		}
	}
	ahead := *f
	ahead.out, ahead.scopes, ahead.loops, ahead.kept = &strings.Builder{}, slices.Clone(f.scopes), nil, nil
	ahead.lead, ahead.rec, ahead.table = lead, rec, a
	ahead.push()
	if a.Vars {
		bind(&ahead, false)
	}
	ahead.leads(s.Body.Stmts)
	if f.kept == nil {
		f.kept = map[syntax.Node]kept{}
	}
	for x, n := range a.Sends {
		f.kept[x] = kept{rec, n}
	}
	for v, n := range a.Values {
		f.kept[v] = kept{rec, n}
	}
	for m, n := range a.Matches {
		f.kept[m] = kept{rec, n}
	}
	header := fmt.Sprintf("for %s, %s := range rlr.Ahead(%s, %s, func(%s %s, %s *rlr.Lead) (%s %s) {\n%sreturn\n}) {",
		elem, rec, f.group, seq, elem, elemType, lead, rec, typ, ahead.out.String())
	f.goLoop(header, s.Label, s.Body, func() { bind(f, true) })
}

// leads writes, in the function that runs ahead for one iteration, what a statement list of the body computes and sends ahead: each statement of the entry that stores a value, with the value kept in the record, each match of the entry, with the arm that it selects kept and the statements of that arm, and each send of the entry, where an exit of the iteration ends the function, as the failure of an operation does.
func (f *fn) leads(stmts []syntax.Stmt) {
	for _, s := range stmts {
		switch s := s.(type) {
		case *syntax.Block:
			f.push()
			f.leads(s.Stmts)
			f.pop()
			continue
		case *syntax.Break, *syntax.Continue, *syntax.Return:
			f.line("return")
			return
		case *syntax.Match:
			if n, ok := f.table.Matches[s]; ok {
				sel := f.temp()
				f.line("%s := %d", sel, len(s.Arms)+1)
				f.selection(s, sel)
				f.line("%s.m%d = %s", f.rec, n, sel)
				f.line("switch %s {", sel)
				for i, arm := range s.Arms {
					f.line("case %d:", i+1)
					f.push()
					f.leads(armStmts(arm))
					f.pop()
				}
				f.line("}")
				continue
			}
		}
		if n, ok := f.table.Values[s]; ok {
			f.stmt(s)
			if n > 0 {
				f.line("%s.v%d, %s.n = %s, %d", f.rec, n, f.rec, f.e.clone(f.valueType(s), valueName(s)), n)
			} else {
				f.line("_ = %s", valueName(s))
			}
			continue
		}
		syntax.Inspect(s, func(x syntax.Node) bool {
			switch x := x.(type) {
			case *syntax.Closure:
				return false
			case *syntax.Send:
				if n, ok := f.table.Sends[x]; ok {
					f.line("%s.s%d = %s", f.rec, n, f.read(x.X, false))
				}
			}
			return true
		})
	}
}

// armStmts returns the statements of the body of an arm.
func armStmts(arm *syntax.Arm) []syntax.Stmt {
	if b, ok := arm.Body.(*syntax.Block); ok {
		return b.Stmts
	}
	return []syntax.Stmt{arm.Body}
}

// selection writes the selection of an arm of a match without an outcome into a Go variable, which receives the number of the first arm whose condition holds and keeps its value when none holds.
func (f *fn) selection(s *syntax.Match, sel string) {
	number := map[syntax.Stmt]int{}
	for i, arm := range s.Arms {
		number[arm.Body] = i + 1
	}
	pick := func(arm *syntax.Arm) { f.line("%s = %d", sel, number[arm.Body]) }
	if s.Subject == nil {
		f.cases(s.Arms, nil, nil, pick)
		return
	}
	t := f.e.p.TypeOf(s.Subject)
	subject := f.stableExpr(s.Subject)
	f.cases(s.Arms, t, func() string { return subject }, pick)
}

// selected writes a match of an entry in the body of its loop: the arm that the iteration selected ahead, or the selection when the iteration did not reach the match ahead, and the body of that arm.
func (f *fn) selected(s *syntax.Match, k kept) {
	sel := f.temp()
	f.line("%s := %s.m%d", sel, k.rec, k.n)
	f.line("if %s == 0 {\n%s = %d", sel, sel, len(s.Arms)+1)
	f.selection(s, sel)
	f.line("}\nswitch %s {", sel)
	for i, arm := range s.Arms {
		f.line("case %d:", i+1)
		f.armBody(arm, "", false)
	}
	f.line("}")
}

// taken writes a statement of an entry in the body of its loop, which takes the value that the iteration computed ahead, or stores the value as written when the iteration did not reach the statement ahead.
func (f *fn) taken(s syntax.Stmt, k kept, write func()) {
	f.line("if %s.n >= %d {\n%s = %s.v%d\n} else {", k.rec, k.n, valueName(s), k.rec, k.n)
	write()
	f.line("}")
}

// adopted writes a send of an entry in the body of its loop and returns its task: the read that the iteration sent ahead, which becomes a read of the routine here, or the read that write dispatches when the iteration sent none ahead.
func (f *fn) adopted(k kept, write func() string) string {
	task := f.temp()
	f.line("%s := %s.s%d", task, k.rec, k.n)
	f.line("if %s == nil {", task)
	f.line("%s = %s", task, write())
	f.line("} else {\n%s.Adopt(%s)\n}", f.group, task)
	return task
}
