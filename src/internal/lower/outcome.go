// This file is phase B, the handler lowering of RITCHIE-MIN.md: inside a protected
// block every operation that a handler can recover becomes the subject of an outcome
// match with the rest of its statement in the success arm, each error arm is decided
// from the raise set of the operation and the arms of the enclosing handlers, and an
// arm of an outer handler leaves the recovered statement by nesting the statements
// that follow under the paths that continue.
package lower

import (
	"fmt"
	"strings"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
)

// outcomes is the state of phase B over one routine, with what the rewrite of futures decides for the program.
type outcomes struct {
	p       *check.Program
	fl      *flights
	module  string
	names   map[string]bool
	targets map[syntax.Stmt]syntax.Stmt
	spread  map[syntax.Expr][]syntax.Expr
	diags   []diagnostics.Record
	stopped bool
}

// segment is the rest of an enclosing statement list after the statement being lowered: how to lower it in its own context, whether it is empty, and the loop whose body the list is.
type segment struct {
	lower func() ([]syntax.Stmt, int)
	empty bool
	loop  syntax.Stmt
}

// exit holds the loop whose break leaves a region that is a loop.
type exit struct {
	loop syntax.Stmt
}

// resultKind is what the results of a recovered statement are.
type resultKind uint8

const (
	noResults resultKind = iota
	storeResults
	returnResults
	resumeResults
)

// results is what a resume supplies: the targets of a store, or the values of a return.
type results struct {
	kind    resultKind
	targets []syntax.Expr
}

// region is a statement that a handler recovers as a whole, or an arm body, with the number of segments outside it, so that the segments inside it are the ones a continuation nests, the loop that runs an arm body once when a resume inside it must leave a loop the arm declares, and the region of the enclosing arm, into which a resume supplies its values when the recovered statement is itself a resume.
type region struct {
	stmt  syntax.Stmt
	res   results
	exit  *exit
	base  int
	wrap  *syntax.For
	outer *region
}

// handler is one enclosing block handler with the loops outside it and the region it recovers.
type handler struct {
	arms   []*syntax.Arm
	loops  []syntax.Stmt
	region region
}

// original is the error that an enclosing arm caught, which continues outward when a failure inside the arm is one that no handler selects, with the original of the arm whose code raised it.
type original struct {
	typ     string
	binding string
	outer   *original
}

// ctx is the position of phase B: the enclosing handlers innermost first, the enclosing loops, the rests of the enclosing lists innermost first, the arm body being lowered, and the original error.
type ctx struct {
	hs    []handler
	loops []syntax.Stmt
	cont  []*segment
	arm   *region
	orig  *original
}

// with returns the position with one more enclosing rest.
func (c ctx) with(seg *segment) ctx {
	c.cont = append([]*segment{seg}, c.cont...)
	return c
}

// enter returns the position inside the body of a loop, where the loop replaces a statement of the phase A tree.
func (c ctx) enter(loop, from syntax.Stmt) ctx {
	for _, h := range c.hs {
		if h.region.stmt == from {
			h.region.exit.loop = loop
		}
	}
	c.loops = append(append([]syntax.Stmt(nil), c.loops...), loop)
	return c
}

// inside returns the number of enclosing rests inside a region.
func (c ctx) inside(r *region) int { return len(c.cont) - r.base }

func (b *outcomes) fresh(hint string) string { return fresh(b.names, hint) }

func (b *outcomes) written(t syntax.Type, o syntax.Origin) syntax.Type {
	return written(t, b.module, o)
}

// unsupported reports a form that phase B has no rule for and stops the routine.
func (b *outcomes) unsupported(n syntax.Node) {
	unsupported(&b.diags, n)
	b.stopped = true
}

// file lowers the routines of a file after phase A.
func (b *outcomes) file(f *syntax.File) *syntax.File {
	b.module = f.File.Module
	out := &syntax.File{Origin: f.Origin}
	for _, d := range f.Decls {
		if fn, ok := d.(*syntax.Func); ok && fn.Body != nil {
			out.Decls = append(out.Decls, b.routine(fn))
			continue
		}
		out.Decls = append(out.Decls, d)
	}
	return out
}

// routine lowers the handlers of one routine and resolves the labels of the transfers it placed.
func (b *outcomes) routine(fn *syntax.Func) *syntax.Func {
	b.names = names(fn)
	b.targets = map[syntax.Stmt]syntax.Stmt{}
	b.spread = map[syntax.Expr][]syntax.Expr{}
	b.stopped = false
	out := *fn
	results := make([]syntax.Type, len(fn.Results))
	for i, r := range fn.Results {
		results[i] = b.p.Table.Type(r)
	}
	shadows := b.fl.analyse(b, fn.Body, fn.Params, results)
	stmts, _ := b.list(fn.Body.Stmts, ctx{}, nil)
	out.Body = b.fl.joined(block(fn.Body.Origin, append(shadows, stmts...)))
	b.labels(out.Body, nil)
	return &out
}

// list lowers a statement list that is the body of the given loop or of none, stopping at a statement that consumed its own rest, and returns the number of enclosing rests that its statements consumed by nesting.
func (b *outcomes) list(stmts []syntax.Stmt, c ctx, loop syntax.Stmt) ([]syntax.Stmt, int) {
	var out []syntax.Stmt
	for i, s := range stmts {
		if b.stopped {
			break
		}
		rest := stmts[i+1:]
		seg := &segment{loop: loop, empty: len(rest) == 0, lower: func() ([]syntax.Stmt, int) { return b.list(rest, c, loop) }}
		lowered, k := b.stmt(s, c.with(seg))
		out = append(out, lowered...)
		if k > 0 {
			return out, k - 1
		}
	}
	return out, 0
}

// continuation lowers enclosing rests in order, skipping the ones that an earlier rest nested.
func (b *outcomes) continuation(segs []*segment) []syntax.Stmt {
	var out []syntax.Stmt
	for i := 0; i < len(segs); {
		lowered, k := segs[i].lower()
		out = append(out, lowered...)
		i += 1 + k
	}
	return out
}

// stmt lowers one statement of the phase A tree and returns the number of enclosing rests it consumed.
func (b *outcomes) stmt(s syntax.Stmt, c ctx) ([]syntax.Stmt, int) {
	switch s := s.(type) {
	case *syntax.Handled:
		return b.handled(s, c)
	case *syntax.Block:
		stmts, k := b.list(s.Stmts, c, nil)
		if spliced(s) {
			return stmts, k
		}
		return []syntax.Stmt{rebuilt(s, stmts)}, k
	case *syntax.For:
		loop := &syntax.For{Origin: s.Origin, Label: s.Label}
		stmts, k := b.list(s.Body.Stmts, c.enter(loop, s), loop)
		loop.Body = block(s.Body.Origin, stmts)
		return []syntax.Stmt{loop}, k
	case *syntax.IterFor:
		return b.iterFor(s, c)
	case *syntax.Break:
		n := *s
		b.transfer(&n, s.Label, c.loops)
		return []syntax.Stmt{&n}, 0
	case *syntax.Continue:
		n := *s
		b.transfer(&n, s.Label, c.loops)
		return []syntax.Stmt{&n}, 0
	case *syntax.Resume:
		return b.resume(s, c)
	case *syntax.Match:
		return b.match(s, c)
	case *syntax.VarDecl, *syntax.Store, *syntax.ExprStmt, *syntax.Return:
		if b.fl.dropped[s] {
			return nil, 0
		}
		return b.simple(s, c)
	case *syntax.ConstDecl, *syntax.Join:
		return []syntax.Stmt{s}, 0
	}
	panic(fmt.Sprintf("lower: unexpected statement %T after phase A", s))
}

// iterFor lowers an iterator loop: the operations of its source that a handler recovers become sites in front of the loop, and its body is a statement list of the loop.
func (b *outcomes) iterFor(s *syntax.IterFor, c ctx) ([]syntax.Stmt, int) {
	residue := func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
		loop := &syntax.IterFor{Origin: s.Origin, Label: s.Label, Vars: s.Vars, Source: b.rebuild(s.Source, repl)}
		stmts, k := b.list(s.Body.Stmts, c.enter(loop, s), loop)
		loop.Body = block(s.Body.Origin, stmts)
		return []syntax.Stmt{loop}, k
	}
	plan := b.sites(b.operations([]syntax.Expr{s.Source}), c)
	if plan == nil {
		return residue(nil)
	}
	return b.nest(plan, residue, nil, c)
}

// transfer records the loop that a break or continue leaves or continues, for the label pass.
func (b *outcomes) transfer(s syntax.Stmt, label string, loops []syntax.Stmt) {
	var target syntax.Stmt
	switch {
	case label == "" && len(loops) > 0:
		target = loops[len(loops)-1]
	case label != "":
		for i := len(loops) - 1; i >= 0; i-- {
			if *labelOf(loops[i]) == label {
				target = loops[i]
				break
			}
		}
	}
	if target != nil {
		b.targets[s] = target
	}
}

// exitBreak returns a break that leaves a loop, recorded for the label pass.
func (b *outcomes) exitBreak(o syntax.Origin, target syntax.Stmt) syntax.Stmt {
	br := &syntax.Break{Origin: o}
	b.targets[br] = target
	return br
}

// handled lowers a block handler: each direct statement of the block is a region that the handler recovers as a whole, and a block that a rewrite introduced is spliced into its context.
func (b *outcomes) handled(s *syntax.Handled, c ctx) ([]syntax.Stmt, int) {
	blk, ok := s.Stmt.(*syntax.Block)
	if !ok {
		panic(fmt.Sprintf("lower: a handler on %T after phase A", s.Stmt))
	}
	out, k := b.protected(s, blk.Stmts, c)
	if spliced(blk) {
		return out, k
	}
	return []syntax.Stmt{rebuilt(blk, out)}, k
}

// protected lowers the direct statements of a protected block under its handler.
func (b *outcomes) protected(s *syntax.Handled, stmts []syntax.Stmt, c ctx) ([]syntax.Stmt, int) {
	var out []syntax.Stmt
	for i, st := range stmts {
		if b.stopped {
			break
		}
		rest := stmts[i+1:]
		seg := &segment{empty: len(rest) == 0, lower: func() ([]syntax.Stmt, int) { return b.protected(s, rest, c) }}
		ci := c.with(seg)
		h := handler{arms: s.Arms, loops: c.loops, region: region{stmt: st, res: resultsOf(st), exit: &exit{}, base: len(ci.cont), outer: c.arm}}
		ci.hs = append([]handler{h}, ci.hs...)
		lowered, k := b.stmt(st, ci)
		out = append(out, lowered...)
		if k > 0 {
			return out, k - 1
		}
	}
	return out, 0
}

// spliced reports whether a block was introduced by a handler rewrite of phase A, so that the kernel program omits its braces.
func spliced(b *syntax.Block) bool { return b.Origin.Construct == syntax.HandledStmt }

// resultsOf returns the results of a statement that a handler recovers, where a region block, or a block that holds only a region, has the results of the region's last statement, and the match of a coalescing store has the results of the store in its arm.
func resultsOf(s syntax.Stmt) results {
	switch s := s.(type) {
	case *syntax.Match:
		if s.Origin.Construct == syntax.CompoundStoreStmt && len(s.Arms) == 1 {
			if b, ok := s.Arms[0].Body.(*syntax.Block); ok && len(b.Stmts) > 0 {
				return resultsOf(b.Stmts[len(b.Stmts)-1])
			}
		}
	case *syntax.VarDecl:
		if len(s.Values) == 0 {
			return results{}
		}
		var targets []syntax.Expr
		for _, t := range s.Targets {
			targets = append(targets, name(t.Origin, t.Name))
		}
		return results{kind: storeResults, targets: targets}
	case *syntax.Store:
		return results{kind: storeResults, targets: s.Targets}
	case *syntax.Return:
		if len(s.Values) == 0 {
			return results{}
		}
		return results{kind: returnResults}
	case *syntax.Resume:
		if len(s.Values) == 0 {
			return results{}
		}
		return results{kind: resumeResults}
	case *syntax.Block:
		if s.Region && len(s.Stmts) > 0 {
			return resultsOf(s.Stmts[len(s.Stmts)-1])
		}
		if len(s.Stmts) == 1 && isRegion(s.Stmts[0]) {
			return resultsOf(s.Stmts[0])
		}
	case *syntax.Handled:
		return resultsOf(s.Stmt)
	}
	return results{}
}

// resume lowers a resume: its values go to the results of the statement the arm recovers, or where the resume of the enclosing arm sends them when the recovered statement is that resume, and the rest of each arm body left behind is dropped.
func (b *outcomes) resume(s *syntax.Resume, c ctx) ([]syntax.Stmt, int) {
	if c.arm == nil {
		panic("lower: resume outside a handler arm")
	}
	var out []syntax.Stmt
	k := 0
	switch c.arm.res.kind {
	case storeResults:
		if len(s.Values) > 0 {
			out, k = b.simple(&syntax.Store{Origin: s.Origin, Targets: c.arm.res.targets, Values: copies(s.Values)}, c)
		}
	case returnResults:
		out, k = b.simple(&syntax.Return{Origin: s.Origin, Values: copies(s.Values)}, c)
	case resumeResults:
		outer := c
		outer.arm = c.arm.outer
		return b.resume(s, outer)
	default:
		if len(s.Values) > 0 {
			panic("lower: a resume with values recovers a statement without results")
		}
	}
	if w := c.arm.wrap; w != nil {
		consumed := c.inside(c.arm)
		for i, seg := range c.cont[:consumed] {
			if seg.loop != nil {
				consumed = i + 1
				break
			}
		}
		return append(out, b.armBreak(s.Origin, w)), max(k, consumed)
	}
	for _, seg := range c.cont[:c.inside(c.arm)] {
		if seg.loop != nil {
			b.unsupported(s)
			return nil, 0
		}
	}
	return out, max(k, c.inside(c.arm))
}

// copies returns resumed values with each literal copied, because an arm that recovers several statements resumes into all of them, and the kernel check types each store's literal under its own target, so that one [] can be an Int[] and a String[].
func copies(values []syntax.Expr) []syntax.Expr {
	out := make([]syntax.Expr, len(values))
	for i, v := range values {
		out[i] = freshLiteral(v)
	}
	return out
}

// freshLiteral copies a literal, a negated literal, or an array or dictionary literal of them, and returns any other expression unchanged.
func freshLiteral(x syntax.Expr) syntax.Expr {
	switch x := x.(type) {
	case *syntax.Lit:
		n := *x
		return &n
	case *syntax.Prim:
		n := *x
		n.Args = copies(x.Args)
		return &n
	case *syntax.ArrayLit:
		n := *x
		n.Elems = copies(x.Elems)
		return &n
	case *syntax.DictLit:
		n := *x
		n.Entries = make([]*syntax.DictEntry, len(x.Entries))
		for i, e := range x.Entries {
			n.Entries[i] = &syntax.DictEntry{Origin: e.Origin, Key: freshLiteral(e.Key), Value: freshLiteral(e.Value)}
		}
		return &n
	}
	return x
}

// armBreak returns a break that leaves the loop that runs an arm body once.
func (b *outcomes) armBreak(o syntax.Origin, wrap *syntax.For) syntax.Stmt {
	br := &syntax.Break{Origin: o, Label: wrap.Label}
	b.targets[br] = wrap
	return br
}

// resumesInLoop reports whether an arm body resumes from inside a loop that the arm declares, outside the arms of the handlers it contains.
func resumesInLoop(n syntax.Node, inLoop bool) bool {
	found := false
	syntax.Inspect(n, func(m syntax.Node) bool {
		if found || m == n {
			return !found
		}
		switch m := m.(type) {
		case *syntax.Resume:
			found = inLoop
		case *syntax.For, *syntax.IterFor:
			found = resumesInLoop(m, true)
			return false
		case *syntax.Handled:
			found = resumesInLoop(m.Stmt, inLoop)
			return false
		case *syntax.Closure:
			return false
		}
		return !found
	})
	return found
}

// placed is one error arm placed at a site: the arm, the number of enclosing rests its code consumed, and whether its code ends in a transfer.
type placed struct {
	arm       *syntax.Arm
	consumed  int
	transfers bool
}

// site is one fallible operation of a statement in evaluation order: its bound names, one per result, its results, the error arms of its outcome match, which are nil for an operation that is only lifted, and whether it is a selector of an assignment target that is read only to validate its place.
type site struct {
	x        syntax.Expr
	names    []string
	results  []syntax.Type
	arms     []placed
	validate bool
}

// candidate is one operation of a statement in evaluation order, with whether it is evaluated conditionally and whether it only validates a target place.
type candidate struct {
	x                     syntax.Expr
	conditional, validate bool
}

// operations returns the operations of some expressions in evaluation order.
func (b *outcomes) operations(roots []syntax.Expr) []candidate {
	var out []candidate
	for _, r := range roots {
		b.opsIn(r, false, func(x syntax.Expr, conditional bool) {
			out = append(out, candidate{x: x, conditional: conditional})
		})
	}
	return out
}

// storeOperations returns the operations of a store in evaluation order: a store to one target validates the selectors of its path before its values and its last selector after them, and a store to several targets validates every selector after all values.
func (b *outcomes) storeOperations(s *syntax.Store) []candidate {
	var path, last []candidate
	for _, t := range s.Targets {
		selectors(t, &path, &last)
	}
	values := b.operations(s.Values)
	if len(s.Targets) == 1 {
		return append(append(path, values...), last...)
	}
	return append(append(values, path...), last...)
}

// selectors appends the index selectors that lead to the last selector of a target path, and that last selector, as operations that validate the place.
func selectors(x syntax.Expr, path, last *[]candidate) {
	switch x := x.(type) {
	case *syntax.FieldRead:
		selectors(x.X, path, path)
	case *syntax.IndexRead:
		selectors(x.X, path, path)
		*last = append(*last, candidate{x: x, validate: true})
	}
}

// opsIn visits the operations of an expression in evaluation order, with the operands of and and or that are evaluated conditionally marked, where a read whose future stays in flight is no operation of its statement, because the routine does not wait for it there, and neither is an append to a local collection of futures, because an outcome match binds no collection of futures.
func (b *outcomes) opsIn(x syntax.Expr, conditional bool, visit func(syntax.Expr, bool)) {
	switch x := x.(type) {
	case nil, *syntax.Lit, *syntax.Name, *syntax.Self, *syntax.Closure:
	case *syntax.Prim:
		for _, a := range x.Args {
			b.opsIn(a, conditional, visit)
		}
		visit(x, conditional)
	case *syntax.Format:
		for _, p := range x.Parts {
			b.opsIn(p.Slot, conditional, visit)
		}
		visit(x, conditional)
	case *syntax.Call:
		b.opsIn(x.Callee, conditional, visit)
		for _, a := range x.Args {
			b.opsIn(a.Value, conditional, visit)
		}
		if !b.fl.kept[x] {
			visit(x, conditional)
		}
	case *syntax.ServiceCall:
		for _, a := range x.Args {
			b.opsIn(a.Value, conditional, visit)
		}
		if !b.fl.kept[x] {
			visit(x, conditional)
		}
	case *syntax.Send:
		for _, o := range b.operandsOf(x.X) {
			b.opsIn(o, conditional, visit)
		}
	case *syntax.Wait:
		if s, ok := x.X.(*syntax.Send); ok {
			b.opsIn(s, conditional, visit)
			visit(x, conditional)
			break
		}
		b.opsIn(x.X, conditional, visit)
	case *syntax.MethodCall:
		for _, r := range b.receiverOperands(x) {
			b.opsIn(r, conditional, visit)
		}
		for _, a := range x.Args {
			b.opsIn(a.Value, conditional, visit)
		}
		visit(x, conditional)
	case *syntax.Construct:
		for _, a := range x.Args {
			b.opsIn(a.Value, conditional, visit)
		}
	case *syntax.ArrayLit:
		for _, e := range x.Elems {
			b.opsIn(e, conditional, visit)
		}
	case *syntax.DictLit:
		for _, e := range x.Entries {
			b.opsIn(e.Key, conditional, visit)
			b.opsIn(e.Value, conditional, visit)
		}
	case *syntax.FieldRead:
		b.opsIn(x.X, conditional, visit)
	case *syntax.IndexRead:
		b.opsIn(x.X, conditional, visit)
		b.opsIn(x.Index, conditional, visit)
		visit(x, conditional)
	case *syntax.Slice:
		b.opsIn(x.X, conditional, visit)
		b.opsIn(x.Lo, conditional, visit)
		b.opsIn(x.Hi, conditional, visit)
		visit(x, conditional)
	case *syntax.Logical:
		b.opsIn(x.Left, conditional, visit)
		b.opsIn(x.Right, true, visit)
	default:
		panic(fmt.Sprintf("lower: unexpected expression %T after phase A", x))
	}
}

// resultsOfExpr returns the result types of an operation: those of a call, or the one type of any other expression.
func (b *outcomes) resultsOfExpr(x syntax.Expr) []syntax.Type {
	if rs := b.p.Results(x); rs != nil || isCall(x) {
		return rs
	}
	if t := b.p.TypeOf(x); t != nil {
		return []syntax.Type{t}
	}
	return nil
}

// sites returns the operations of a statement up to the last one that a handler recovers, each with a fresh name, or nil when no handler recovers any operation.
func (b *outcomes) sites(ops []candidate, c ctx) []*site {
	var all []*site
	last := -1
	for _, op := range ops {
		s := &site{x: op.x, results: b.resultsOfExpr(op.x), validate: op.validate}
		if b.selects(op.x, c, nil, false) {
			if op.conditional {
				b.unsupported(op.x)
				continue
			}
			s.arms = b.armsFor(op.x, c, nil, false)
			last = len(all)
		} else if op.validate && b.p.Site(op.x).Empty() {
			continue
		}
		all = append(all, s)
	}
	if last < 0 {
		return nil
	}
	all = all[:last+1]
	for _, s := range all {
		hint := "t"
		if s.arms != nil {
			hint = "v"
		}
		s.names = make([]string, max(1, len(s.results)))
		for i := range s.names {
			s.names[i] = b.fresh(hint)
		}
	}
	return all
}

// nest builds the nested outcome matches of the sites of a statement from the innermost outward: a lifted operation becomes a declaration, the residue of the statement goes into the innermost success arm followed by the enclosing rests that the arms skip and that the residue does not nest in its own arms, and each arm that continues receives the rests between the statement it recovers and the outermost one skipped.
func (b *outcomes) nest(plan []*site, residue func(map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int), discard syntax.Expr, c ctx) ([]syntax.Stmt, int) {
	repl := map[syntax.Expr]syntax.Expr{}
	for _, s := range plan {
		if !s.validate {
			repl[s.x] = name(s.x.Where(), s.names[0])
		}
		if len(s.names) > 1 {
			spread := make([]syntax.Expr, len(s.names))
			for i, n := range s.names {
				spread[i] = name(s.x.Where(), n)
			}
			b.spread[s.x] = spread
		}
	}
	inner, nested := residue(repl)
	consumed := nested
	for _, s := range plan {
		for _, a := range s.arms {
			consumed = max(consumed, a.consumed)
		}
	}
	if consumed > nested {
		inner = append(inner, b.continuation(c.cont[nested:consumed])...)
	}
	for i := len(plan) - 1; i >= 0; i-- {
		s := plan[i]
		before := map[syntax.Expr]syntax.Expr{}
		for _, earlier := range plan[:i] {
			if r, ok := repl[earlier.x]; ok {
				before[earlier.x] = r
			}
		}
		subject := b.rebuild(s.x, before)
		o := s.x.Where()
		if s.arms == nil && s.validate {
			inner = []syntax.Stmt{&syntax.Match{Origin: o, Subject: subject, Arms: []*syntax.Arm{anyArm(o, block(o, inner))}}}
			continue
		}
		if s.arms == nil {
			d := &syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: b.written(b.p.TypeOf(s.x), o), Name: s.names[0]}}, Values: []syntax.Expr{subject}}
			inner = append([]syntax.Stmt{d}, inner...)
			continue
		}
		var success *syntax.Arm
		switch {
		case len(s.results) == 0:
			success = &syntax.Arm{Origin: o, Kind: syntax.NoneArm, Body: block(o, inner)}
		case s.x == discard || s.validate:
			success = anyArm(o, block(o, inner))
		case len(s.results) > 1:
			success = &syntax.Arm{Origin: o, Kind: syntax.ResultsArm, Body: block(o, inner)}
			for k, t := range s.results {
				success.Targets = append(success.Targets, &syntax.VarTarget{Origin: o, Type: b.written(t, o), Name: s.names[k]})
			}
		default:
			success = memberArm(o, b.written(s.results[0], o), s.names[0], block(o, inner))
		}
		var arms []*syntax.Arm
		if success.Kind != syntax.AnyArm {
			arms = append(arms, success)
		}
		for _, a := range s.arms {
			arms = append(arms, b.continued(a, consumed, c))
		}
		if success.Kind == syntax.AnyArm {
			arms = append(arms, success)
		}
		inner = []syntax.Stmt{&syntax.Match{Origin: o, Subject: subject, Arms: arms}}
	}
	return inner, consumed
}

// continued appends to a placed arm that continues the enclosing rests between what its code skipped and what the statement nests.
func (b *outcomes) continued(a placed, consumed int, c ctx) *syntax.Arm {
	if a.transfers || a.consumed >= consumed {
		return a.arm
	}
	body := a.arm.Body.(*syntax.Block)
	a.arm.Body = block(body.Origin, append(body.Stmts, b.continuation(c.cont[a.consumed:consumed])...))
	return a.arm
}

// rebuild returns an expression with its sites replaced by their names, its reads and its uses of futures in their kernel forms, and its closure bodies lowered.
func (b *outcomes) rebuild(x syntax.Expr, repl map[syntax.Expr]syntax.Expr) syntax.Expr {
	var f func(y syntax.Expr) (syntax.Expr, bool)
	inner := func(y syntax.Expr) syntax.Expr {
		return rebuild(y, func(z syntax.Expr) (syntax.Expr, bool) {
			if z == y {
				return nil, false
			}
			return f(z)
		}, b.closure)
	}
	f = func(y syntax.Expr) (syntax.Expr, bool) {
		if r, ok := repl[y]; ok {
			return r, true
		}
		return b.fl.rewrite(y, inner)
	}
	return rebuild(x, f, b.closure)
}

// rebuildAll rebuilds a list of expressions, where a site with several results becomes the names of its results.
func (b *outcomes) rebuildAll(xs []syntax.Expr, repl map[syntax.Expr]syntax.Expr) []syntax.Expr {
	out := make([]syntax.Expr, 0, len(xs))
	for _, x := range xs {
		if _, ok := repl[x]; ok && b.spread[x] != nil {
			out = append(out, b.spread[x]...)
			continue
		}
		out = append(out, b.rebuild(x, repl))
	}
	return out
}

// closure lowers the body of a closure as a routine of its own.
func (b *outcomes) closure(x *syntax.Closure) *syntax.Closure {
	out := *x
	if x.Body == nil {
		out.Expr = b.rebuild(x.Expr, nil)
		return &out
	}
	var results []syntax.Type
	if ft, ok := b.p.TypeOf(x).(*syntax.FuncType); ok {
		results = ft.Results
	}
	shadows := b.fl.analyse(b, x.Body, x.Params, results)
	stmts, _ := b.list(x.Body.Stmts, ctx{}, nil)
	out.Body = b.fl.joined(block(x.Body.Origin, append(shadows, stmts...)))
	return &out
}

// simple lowers a statement whose operations may be recovered: the operations become nested outcome matches in evaluation order, and a declaration splits into its declarations and the store of its values.
func (b *outcomes) simple(s syntax.Stmt, c ctx) ([]syntax.Stmt, int) {
	var ops []candidate
	var discard syntax.Expr
	if st, ok := s.(*syntax.Store); ok && b.fl.merged[st] != nil {
		s = &syntax.VarDecl{Origin: st.Origin, Targets: b.fl.merged[st], Values: st.Values}
	}
	switch s := s.(type) {
	case *syntax.VarDecl:
		ops = b.operations(s.Values)
	case *syntax.Store:
		ops = b.storeOperations(s)
	case *syntax.ExprStmt:
		ops = b.operations([]syntax.Expr{s.X})
		discard = s.X
	case *syntax.Return:
		ops = b.operations(s.Values)
	}
	plan := b.sites(ops, c)
	residue := func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
		switch s := s.(type) {
		case *syntax.VarDecl:
			return []syntax.Stmt{&syntax.VarDecl{Origin: s.Origin, Targets: b.fl.targets(s.Targets), Values: b.rebuildAll(s.Values, repl)}}, 0
		case *syntax.Store:
			return []syntax.Stmt{&syntax.Store{Origin: s.Origin, Targets: b.rebuildAll(s.Targets, repl), Values: b.rebuildAll(s.Values, repl)}}, 0
		case *syntax.ExprStmt:
			if repl[s.X] != nil {
				return nil, 0
			}
			return []syntax.Stmt{&syntax.ExprStmt{Origin: s.Origin, X: b.rebuild(s.X, repl)}}, 0
		case *syntax.Return:
			return []syntax.Stmt{&syntax.Return{Origin: s.Origin, Values: b.rebuildAll(s.Values, repl)}}, 0
		}
		panic(fmt.Sprintf("lower: unexpected statement %T", s))
	}
	if plan == nil {
		return residue(nil)
	}
	var head []syntax.Stmt
	if d, ok := s.(*syntax.VarDecl); ok {
		var targets []syntax.Expr
		for _, t := range b.fl.targets(d.Targets) {
			head = append(head, &syntax.VarDecl{Origin: d.Origin, Targets: []*syntax.VarTarget{t}})
			targets = append(targets, name(t.Origin, t.Name))
		}
		st := &syntax.Store{Origin: d.Origin, Targets: targets, Values: d.Values}
		residue = func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
			return []syntax.Stmt{&syntax.Store{Origin: st.Origin, Targets: st.Targets, Values: b.rebuildAll(st.Values, repl)}}, 0
		}
	}
	out, k := b.nest(plan, residue, discard, c)
	return append(head, out...), k
}

// match lowers a match: a source outcome match gains arms for the failures its own arms leave to the enclosing handlers, the operations of a subject become sites, a candidate with a site begins a nested match inside the arm that selection reaches, and the enclosing rests that one arm nests are appended to every other arm that continues.
func (b *outcomes) match(s *syntax.Match, c ctx) ([]syntax.Stmt, int) {
	o := s.Origin
	if b.isOutcome(s) {
		exclude := map[string]bool{}
		all := false
		for _, arm := range s.Arms {
			switch arm.Kind {
			case syntax.TypedArm:
				if n := b.errorKey(arm.Type); strings.HasSuffix(n, "!") {
					exclude[n] = true
				}
			case syntax.AnyErrorArm:
				all = true
			}
		}
		extra := b.armsFor(s.Subject, c, exclude, all)
		plan := b.sites(b.operations(b.operandsOf(s.Subject)), c)
		residue := func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
			var arms []placed
			for _, arm := range s.Arms {
				arms = append(arms, b.arm(arm, b.rebuildAll(arm.Patterns, repl), c))
			}
			arms = append(arms, extra...)
			out, k := b.joined(arms, c, b.covered(s))
			return []syntax.Stmt{&syntax.Match{Origin: o, Subject: b.rebuild(s.Subject, repl), Arms: out}}, k
		}
		return b.nest(plan, residue, nil, c)
	}
	var roots []syntax.Expr
	if s.Subject != nil {
		roots = []syntax.Expr{s.Subject}
	}
	plan := b.sites(b.operations(roots), c)
	var head []syntax.Stmt
	subject := s.Subject
	consumed := 0
	if subject != nil && !isName(subject) && b.candidateSites(s.Arms, c) {
		t := b.fresh("t")
		head = append(head, &syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: b.written(b.p.TypeOf(s.Subject), o), Name: t}}})
		residue := func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
			return []syntax.Stmt{store(o, name(o, t), b.rebuild(s.Subject, repl))}, 0
		}
		var out []syntax.Stmt
		out, consumed = b.nest(plan, residue, nil, c)
		head = append(head, out...)
		plan = nil
		subject = name(o, t)
	}
	residue := func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
		again := func() syntax.Expr { return b.rebuild(subject, repl) }
		arms, nested, k := b.candidateArms(again, s.Arms, c, b.covered(s))
		if nested {
			return arms[0].Body.(*syntax.Block).Stmts, k
		}
		return []syntax.Stmt{&syntax.Match{Origin: o, Subject: again(), Arms: arms}}, k
	}
	var out []syntax.Stmt
	k := 0
	if plan == nil {
		out, k = residue(nil)
	} else {
		out, k = b.nest(plan, residue, nil, c)
	}
	return append(head, out...), max(k, consumed)
}

// isOutcome reports whether a match of the phase A tree selects among the outcomes of its subject.
func (b *outcomes) isOutcome(s *syntax.Match) bool {
	if s.Subject == nil || !outcomeArms(s.Arms) {
		return false
	}
	return isCall(s.Subject) || !b.p.Site(s.Subject).Empty()
}

// covered reports whether a match completes normally only through its arms: a value match has an any arm, and an outcome match selects its success outcome.
func (b *outcomes) covered(s *syntax.Match) bool {
	outcome := b.isOutcome(s)
	for _, arm := range s.Arms {
		switch arm.Kind {
		case syntax.AnyArm:
			return true
		case syntax.NoneArm, syntax.ResultsArm:
			if outcome {
				return true
			}
		case syntax.TypedArm:
			if outcome && !strings.HasSuffix(b.errorKey(arm.Type), "!") {
				return true
			}
		}
	}
	return false
}

// joined appends to every arm that continues the enclosing rests that another arm nested, and adds an any arm that holds those rests for the paths through no arm when the match does not cover them and the rests hold a statement.
func (b *outcomes) joined(arms []placed, c ctx, covered bool) ([]*syntax.Arm, int) {
	consumed := 0
	for _, a := range arms {
		consumed = max(consumed, a.consumed)
	}
	out := make([]*syntax.Arm, len(arms))
	for i, a := range arms {
		out[i] = b.continued(a, consumed, c)
	}
	if consumed > 0 && !covered {
		if rest := b.continuation(c.cont[:consumed]); len(rest) > 0 {
			o := out[len(out)-1].Origin
			out = append(out, anyArm(o, block(o, rest)))
		}
	}
	return out, consumed
}

// receiverOperands returns the operands of the receiver of a method call: the index and key operands of a receiver place whose resolution belongs to the call, or the receiver itself.
func (b *outcomes) receiverOperands(x *syntax.MethodCall) []syntax.Expr {
	selectors := b.p.PlaceSelectors(x)
	if selectors == nil {
		return []syntax.Expr{x.Receiver}
	}
	out := make([]syntax.Expr, len(selectors))
	for i, s := range selectors {
		out[i] = s.Index
	}
	return out
}

// operandsOf returns the operands of a subject operation, whose failures are failures of the statement and not outcomes of the operation.
func (b *outcomes) operandsOf(x syntax.Expr) []syntax.Expr {
	var out []syntax.Expr
	switch x := x.(type) {
	case *syntax.Wait:
		if s, ok := x.X.(*syntax.Send); ok {
			return b.operandsOf(s.X)
		}
	case *syntax.Call:
		out = append(out, x.Callee)
		for _, a := range x.Args {
			out = append(out, a.Value)
		}
	case *syntax.MethodCall:
		out = append(out, b.receiverOperands(x)...)
		for _, a := range x.Args {
			out = append(out, a.Value)
		}
	case *syntax.ServiceCall:
		for _, a := range x.Args {
			out = append(out, a.Value)
		}
	case *syntax.Prim:
		out = append(out, x.Args...)
	case *syntax.IndexRead:
		out = append(out, x.X, x.Index)
	case *syntax.Slice:
		out = append(out, x.X)
		if x.Lo != nil {
			out = append(out, x.Lo)
		}
		if x.Hi != nil {
			out = append(out, x.Hi)
		}
	case *syntax.Format:
		for _, p := range x.Parts {
			if p.Slot != nil {
				out = append(out, p.Slot)
			}
		}
	}
	return out
}

// candidateSites reports whether a candidate of a case arm holds an operation that a handler recovers.
func (b *outcomes) candidateSites(arms []*syntax.Arm, c ctx) bool {
	for _, arm := range arms {
		if arm.Kind != syntax.CaseArm {
			continue
		}
		found := false
		for _, p := range arm.Patterns {
			b.opsIn(p, false, func(x syntax.Expr, _ bool) {
				found = found || b.selects(x, c, nil, false)
			})
		}
		if found {
			return true
		}
	}
	return false
}

// candidateArms lowers the arms of a value or subjectless match, where the first case arm with a site and every arm after it move into a nested match inside an any arm after the outcome matches of the site, and nested reports that this any arm is the first arm, so that the enclosing match is needless.
func (b *outcomes) candidateArms(subject func() syntax.Expr, arms []*syntax.Arm, c ctx, covered bool) (out []*syntax.Arm, nested bool, consumed int) {
	var lowered []placed
	for j, arm := range arms {
		if arm.Kind != syntax.CaseArm {
			lowered = append(lowered, b.arm(arm, arm.Patterns, c))
			continue
		}
		plan := b.sites(b.operations(arm.Patterns), c)
		if plan == nil {
			lowered = append(lowered, b.arm(arm, b.rebuildAll(arm.Patterns, nil), c))
			continue
		}
		residue := func(repl map[syntax.Expr]syntax.Expr) ([]syntax.Stmt, int) {
			first := b.arm(arm, b.rebuildAll(arm.Patterns, repl), c)
			rest, again, k := b.candidateArms(subject, arms[j+1:], c, covered)
			if again {
				rest = rest[0].Body.(*syntax.Block).Stmts[0].(*syntax.Match).Arms
			}
			head := b.continued(first, k, c)
			return []syntax.Stmt{&syntax.Match{Origin: arm.Origin, Subject: subject(), Arms: append([]*syntax.Arm{head}, rest...)}}, max(k, first.consumed)
		}
		body, k := b.nest(plan, residue, nil, c)
		lowered = append(lowered, placed{arm: anyArm(arm.Origin, block(arm.Origin, body)), consumed: k})
		out, k = b.joined(lowered, c, true)
		return out, j == 0, k
	}
	out, consumed = b.joined(lowered, c, covered)
	return out, false, consumed
}

// arm lowers the body of an arm with the given candidates.
func (b *outcomes) arm(arm *syntax.Arm, patterns []syntax.Expr, c ctx) placed {
	out := &syntax.Arm{Origin: arm.Origin, Kind: arm.Kind, Patterns: patterns, Type: arm.Type, Binding: arm.Binding, Targets: arm.Targets}
	if arm.Body == nil {
		return placed{arm: out}
	}
	body, k := b.body(arm.Body, c)
	out.Body = body
	return placed{arm: out, consumed: k, transfers: endsInTransfer(body)}
}

// body lowers the body of an arm into one statement, where a spliced handler block that lowers to one block is that block.
func (b *outcomes) body(s syntax.Stmt, c ctx) (syntax.Stmt, int) {
	switch s := s.(type) {
	case *syntax.Handled:
		out, k := b.handled(s, c)
		if len(out) == 1 {
			if blk, ok := out[0].(*syntax.Block); ok {
				return blk, k
			}
		}
		return block(s.Origin, out), k
	case *syntax.Block:
		out, k := b.list(s.Stmts, c, nil)
		return rebuilt(s, out), k
	}
	out, k := b.list([]syntax.Stmt{s}, c, nil)
	return block(s.Where(), out), k
}

// endsInTransfer reports whether a lowered statement ends in a break, a continue, or a return.
func endsInTransfer(s syntax.Stmt) bool {
	if blk, ok := s.(*syntax.Block); ok {
		if len(blk.Stmts) == 0 {
			return false
		}
		s = blk.Stmts[len(blk.Stmts)-1]
	}
	switch s.(type) {
	case *syntax.Break, *syntax.Continue, *syntax.Return:
		return true
	}
	return false
}

// isCall reports whether an expression is a call.
func isCall(x syntax.Expr) bool {
	switch x.(type) {
	case *syntax.Call, *syntax.MethodCall, *syntax.ServiceCall:
		return true
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

// errorKey returns the canonical name of a written error type, as a raise set lists it.
func (b *outcomes) errorKey(t syntax.Type) string {
	r, _ := b.p.Table.ResolveType(t, b.module)
	n, ok := r.(*syntax.Named)
	if !ok {
		return ""
	}
	if n.Module == "" {
		return n.Name
	}
	return n.Module + "::" + n.Name
}

// errorType returns the written type of an error from the key a raise set lists it under.
func (b *outcomes) errorType(key string, o syntax.Origin) syntax.Type {
	module, n, ok := strings.Cut(key, "::")
	if !ok {
		return &syntax.Named{Origin: o, Name: key}
	}
	if module == b.module {
		module = ""
	}
	return &syntax.Named{Origin: o, Module: module, Name: n}
}

// selects reports whether some enclosing handler selects a failure of an operation, or the operation is inside an arm, so that the operation is the subject of an outcome match.
func (b *outcomes) selects(x syntax.Expr, c ctx, exclude map[string]bool, all bool) bool {
	set := b.p.Site(x)
	if set.Empty() || all {
		return false
	}
	covered := map[string]bool{}
	for n := range exclude {
		covered[n] = true
	}
	for _, h := range c.hs {
		for _, arm := range h.arms {
			switch arm.Kind {
			case syntax.TypedArm:
				n := b.errorKey(arm.Type)
				if !covered[n] && (set.Types[n] || set.Opaque) {
					return true
				}
			case syntax.AnyArm:
				if remaining(set, covered) {
					return true
				}
			}
		}
	}
	return c.orig != nil && remaining(set, covered)
}

// remaining reports whether a raise set has a failure that no arm so far selects.
func remaining(set check.RaiseSet, covered map[string]bool) bool {
	if set.Opaque {
		return true
	}
	for n := range set.Types {
		if !covered[n] {
			return true
		}
	}
	return false
}

// armsFor returns the error arms of the outcome match of an operation: the arms of the enclosing handlers that select a failure of it, innermost first, and inside an arm a report arm for every failure that no handler selects.
func (b *outcomes) armsFor(x syntax.Expr, c ctx, exclude map[string]bool, all bool) []placed {
	set := b.p.Site(x)
	if set.Empty() || all {
		return nil
	}
	covered := map[string]bool{}
	for n := range exclude {
		covered[n] = true
	}
	var arms []placed
	done := false
	for level, h := range c.hs {
		for _, arm := range h.arms {
			switch arm.Kind {
			case syntax.TypedArm:
				n := b.errorKey(arm.Type)
				if covered[n] || !(set.Types[n] || set.Opaque) {
					continue
				}
				covered[n] = true
				arms = append(arms, b.errorArm(arm, level, c))
			case syntax.AnyArm:
				if !remaining(set, covered) {
					continue
				}
				done = true
				arms = append(arms, b.anyErrorArm(arm, level, c))
			}
			if done {
				break
			}
		}
		if done {
			break
		}
	}
	if !done && c.orig != nil {
		for _, n := range set.Names() {
			if !covered[n] {
				arms = append(arms, b.reportArm(b.errorType(n, x.Where()), c, x))
			}
		}
		if set.Opaque {
			arms = append(arms, b.reportArm(nil, c, x))
		}
	}
	return arms
}

// errorArm builds the arm of an outcome match from a typed handler arm.
func (b *outcomes) errorArm(arm *syntax.Arm, level int, c ctx) placed {
	binding := arm.Binding
	if binding == "" {
		binding = b.fresh("e")
	}
	code, k, transfers := b.armCode(arm, level, c, original{typ: b.errorKey(arm.Type), binding: binding, outer: c.orig})
	return placed{arm: &syntax.Arm{Origin: arm.Origin, Kind: syntax.TypedArm, Type: arm.Type, Binding: binding, Body: block(arm.Origin, code)}, consumed: k, transfers: transfers}
}

// anyErrorArm builds the any! arm of an outcome match from an any handler arm.
func (b *outcomes) anyErrorArm(arm *syntax.Arm, level int, c ctx) placed {
	binding := arm.Binding
	if binding == "" {
		binding = b.fresh("e")
	}
	code, k, transfers := b.armCode(arm, level, c, original{binding: binding, outer: c.orig})
	body := block(arm.Origin, code)
	if arm.Binding == "" && !reads(body, binding) {
		binding = ""
	}
	return placed{arm: &syntax.Arm{Origin: arm.Origin, Kind: syntax.AnyErrorArm, Binding: binding, Body: body}, consumed: k, transfers: transfers}
}

// armCode lowers the body of a handler arm at a site, whose sites see the handlers outside its own and whose resume stores to the recovered statement, and leaves the recovered statement by the transfer the code ends in, by a break of the outermost loop between the site and the end of that statement when every rest beyond that loop is empty, or by falling through while reporting the rests it skips, which the site nests under the paths that continue.
func (b *outcomes) armCode(arm *syntax.Arm, level int, c ctx, orig original) ([]syntax.Stmt, int, bool) {
	h := c.hs[level]
	body, ok := arm.Body.(*syntax.Block)
	if !ok {
		panic(fmt.Sprintf("lower: a handler arm body %T after phase A", arm.Body))
	}
	skipped := c.inside(&h.region)
	var wrap *syntax.For
	if resumesInLoop(body, false) {
		wrap = &syntax.For{Origin: arm.Origin, Label: b.fresh("arm")}
	}
	inner := ctx{
		hs:    c.hs[level+1:],
		loops: h.loops,
		cont:  c.cont[skipped:],
		arm:   &region{stmt: arm.Body, res: h.region.res, exit: &exit{}, base: len(c.cont) - skipped, wrap: wrap, outer: h.region.outer},
		orig:  &orig,
	}
	code, k := b.list(body.Stmts, inner, nil)
	if wrap != nil {
		if !endsInTransfer(block(arm.Origin, code)) {
			code = append(code, b.armBreak(arm.Origin, wrap))
		}
		wrap.Body = block(body.Origin, code)
		code = []syntax.Stmt{wrap}
	}
	if endsInTransfer(block(arm.Origin, code)) {
		return code, 0, true
	}
	loop, beyond := leaving(c.cont[:skipped])
	switch {
	case loop == nil:
		return code, skipped + k, false
	case beyond:
		b.unsupported(arm)
		return code, 0, true
	}
	return append(code, b.exitBreak(arm.Origin, loop)), 0, true
}

// leaving returns the outermost loop whose body holds one of the given rests, and whether a rest beyond that loop is not empty, in which case a break of the loop cannot skip it.
func leaving(segs []*segment) (syntax.Stmt, bool) {
	outermost := -1
	for i, seg := range segs {
		if seg.loop != nil {
			outermost = i
		}
	}
	if outermost < 0 {
		return nil, false
	}
	for _, seg := range segs[outermost+1:] {
		if !seg.empty {
			return segs[outermost].loop, true
		}
	}
	return segs[outermost].loop, false
}

// reportArm builds the arm of a failure inside a handler arm that no handler selects, where the failure is reported and outward handling of the original error resumes, and a nil type builds the any! arm of an opaque set.
func (b *outcomes) reportArm(t syntax.Type, c ctx, x syntax.Expr) placed {
	o := x.Where()
	binding := b.fresh("b")
	code := []syntax.Stmt{&syntax.ExprStmt{Origin: o, X: reportOf(o, binding)}}
	path, k, transfers := b.originPath(c, o)
	code = append(code, path...)
	arm := &syntax.Arm{Origin: o, Kind: syntax.TypedArm, Type: t, Binding: binding, Body: block(o, code)}
	if t == nil {
		arm.Kind = syntax.AnyErrorArm
	}
	return placed{arm: arm, consumed: k, transfers: transfers}
}

// originPath returns the code of the original error past the failed handler: the arm of the next handler that selects it, or its propagation when none does.
func (b *outcomes) originPath(c ctx, o syntax.Origin) ([]syntax.Stmt, int, bool) {
	orig := *c.orig
	for level, h := range c.hs {
		for _, arm := range h.arms {
			var matches bool
			var t syntax.Type = &syntax.Named{Origin: o, Name: "Error!"}
			switch arm.Kind {
			case syntax.TypedArm:
				matches = orig.typ != "" && b.errorKey(arm.Type) == orig.typ
				t = arm.Type
			case syntax.AnyArm:
				matches = true
			}
			if !matches {
				continue
			}
			var head []syntax.Stmt
			if arm.Binding != "" {
				head = append(head, &syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: t, Name: arm.Binding}}, Values: []syntax.Expr{name(o, orig.binding)}})
			}
			code, k, transfers := b.armCode(arm, level, c, orig)
			return append(head, code...), k, transfers
		}
	}
	if orig.outer != nil {
		c.orig = orig.outer
		code, k, transfers := b.originPath(c, o)
		return append([]syntax.Stmt{&syntax.ExprStmt{Origin: o, X: reportOf(o, orig.binding)}}, code...), k, transfers
	}
	return []syntax.Stmt{&syntax.Return{Origin: o, Values: []syntax.Expr{name(o, orig.binding)}}}, 0, true
}

// reportOf returns the call of the standard report of an error binding, which the module qualifier keeps from any declaration of the program.
func reportOf(o syntax.Origin, binding string) *syntax.Call {
	callee := &syntax.Name{Origin: o, Module: "r", Name: "report"}
	return &syntax.Call{Origin: o, Callee: callee, Args: []*syntax.Arg{{Origin: o, Value: name(o, binding)}}}
}

// labels resolves the label of every transfer that phase B placed: a transfer whose loop is not the innermost one around it is labelled with that loop's label, which it receives fresh when it has none.
func (b *outcomes) labels(n syntax.Node, loops []syntax.Stmt) {
	switch n := n.(type) {
	case *syntax.For:
		b.labels(n.Body, append(loops, n))
		return
	case *syntax.IterFor:
		b.labels(n.Source, loops)
		b.labels(n.Body, append(loops, n))
		return
	case *syntax.Break:
		b.relabel(n, &n.Label, loops)
		return
	case *syntax.Continue:
		b.relabel(n, &n.Label, loops)
		return
	case *syntax.Closure:
		if n.Body != nil {
			b.labels(n.Body, nil)
		}
		return
	}
	syntax.Inspect(n, func(m syntax.Node) bool {
		if m == n {
			return true
		}
		b.labels(m, loops)
		return false
	})
}

// relabel labels one transfer against the loops around it in the kernel tree.
func (b *outcomes) relabel(s syntax.Stmt, label *string, loops []syntax.Stmt) {
	target := b.targets[s]
	if target == nil || len(loops) > 0 && loops[len(loops)-1] == target {
		return
	}
	l := labelOf(target)
	if *l == "" {
		*l = b.fresh("loop")
	}
	*label = *l
}

// labelOf returns the label of a kernel loop.
func labelOf(loop syntax.Stmt) *string {
	switch loop := loop.(type) {
	case *syntax.For:
		return &loop.Label
	case *syntax.IterFor:
		return &loop.Label
	}
	panic(fmt.Sprintf("lower: a transfer target %T is not a loop", loop))
}
