// This file is the call graph: routines and closures as nodes, calls with their callee
// kind as edges, built once while the bodies are typed.
package check

import (
	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// Node is a routine or a closure of the call graph.
type Node struct {
	Func    *syntax.Func
	Closure *syntax.Closure
}

// Edge is one call of a node: the call expression and what it reaches.
type Edge struct {
	Call   syntax.Expr
	Callee *Callee
}

// CallGraph holds every routine and closure with the calls each makes.
type CallGraph struct {
	Nodes []Node
	Edges map[Node][]Edge
}

// callee is what a call expression reaches: a declared routine, a function value, a built-in row, or a remote operation.
type callee struct {
	kind   syntax.CalleeKind
	fn     *syntax.Func
	entry  *resolve.Entry
	op     *Operation
	typ    *syntax.FuncType
	remote bool
	lookup bool
	create bool
}

// Kind is the callee kind that the call records.
func (k *callee) Kind() syntax.CalleeKind { return k.kind }

// Func is the declared routine the call reaches, or nil.
func (k *callee) Func() *syntax.Func { return k.fn }

// Row is the built-in row the call reaches, or nil.
func (k *callee) Row() *builtin.Row {
	if k.op == nil {
		return nil
	}
	return k.op.Row
}

// edge records a call of the routine being typed in the call graph.
func (c *checker) edge(r *routine, x syntax.Expr, k *callee) {
	c.p.callees[x] = k
	c.edges[r.node] = append(c.edges[r.node], Edge{Call: x, Callee: k})
}

// Read reports whether a call is a remote read: a call of a service method that is no effect.
func (p *Program) Read(x syntax.Expr) bool {
	k := p.callees[x]
	return k != nil && k.remote && k.fn != nil && !p.Marks.effects[x]
}

// Reader reports whether a call is a call of a local routine that is a reader.
func (p *Program) Reader(x syntax.Expr) bool {
	k := p.callees[x]
	return k != nil && !k.remote && k.fn != nil && p.Marks.readers[Node{Func: k.fn}]
}

// Marks holds the mark "can mutate" of each routine and closure, which the runtime reads, whether each call is an effect, and the routines that are readers.
type Marks struct {
	mutates map[Node]bool
	effects map[syntax.Expr]bool
	readers map[Node]bool
}

// Mutates reports whether a routine carries the mark "can mutate", which is sent with a remote call.
func (m Marks) Mutates(fn *syntax.Func) bool { return m.mutates[Node{Func: fn}] }

// Effect reports whether a call is a mutation, an entity operation, an output, a wait on the clock, a call of a local routine that can reach one of these, or a call of a function value, whose body is not known.
func (m Marks) Effect(x syntax.Expr) bool { return m.effects[x] }

// marks computes the marks of every routine and closure, where mutating reports the operations that mutate, entity reports the services that stand for an entity type, and a mutation that a closure reaches marks the routine that writes the closure, so that every routine of a call cycle gets one result.
func (g *CallGraph) marks(mutating func(*syntax.Func) bool, entity func(*resolve.Entry) bool) Marks {
	inside := map[Node][]Node{}
	for _, n := range g.Nodes {
		if n.Func == nil || n.Func.Body == nil {
			continue
		}
		syntax.Inspect(n.Func.Body, func(x syntax.Node) bool {
			if c, ok := x.(*syntax.Closure); ok {
				inside[n] = append(inside[n], Node{Closure: c})
			}
			return true
		})
	}
	declared := func(e Edge) bool { return e.Callee.Func() != nil }
	local := func(e Edge) bool { return declared(e) && !e.Callee.remote }
	mutation := func(e Edge) bool { return declared(e) && mutating(e.Callee.Func()) }
	m := Marks{mutates: g.reach(inside, mutation, declared), effects: map[syntax.Expr]bool{}}
	for _, n := range g.Nodes {
		if n.Func != nil && mutating(n.Func) {
			m.mutates[n] = true
		}
	}
	acts := func(e Edge) bool {
		k := e.Callee
		switch {
		case k.remote:
			return k.fn == nil && !k.lookup || k.fn != nil && m.mutates[Node{Func: k.fn}]
		case k.op != nil:
			return k.op.Row.Effect
		}
		return k.kind == syntax.FunctionValue
	}
	operates := func(e Edge) bool {
		k := e.Callee
		return k.remote && (k.kind == syntax.EntityMethod || k.entry.Owner != nil && entity(k.entry.Owner))
	}
	can := g.reach(nil, acts, local)
	for _, n := range g.Nodes {
		for _, e := range g.Edges[n] {
			m.effects[e.Call] = acts(e) || operates(e) || local(e) && can[Node{Func: e.Callee.Func()}]
		}
	}
	m.readers = g.readers(m, inside, local)
	return m
}

// readers returns the routines that are readers, where a routine is contained when its body holds no loop without an iterator and no closure, makes no call that is an effect, and calls only local routines that are contained, so that no routine of a call cycle is contained, where a reader is a contained routine that reaches a remote read and writes no parameter and no receiver, and where a built-in that observes the clock or draws from a random source prevents neither, because the time at which a routine observes the clock is no result.
func (g *CallGraph) readers(m Marks, inside map[Node][]Node, local func(Edge) bool) map[Node]bool {
	read := func(e Edge) bool { return e.Callee.remote && e.Callee.fn != nil && !m.effects[e.Call] }
	reads := g.reach(nil, read, local)
	contained := map[Node]bool{}
	for changed := true; changed; {
		changed = false
		for _, n := range g.Nodes {
			if contained[n] || n.Func == nil || n.Func.Body == nil || len(inside[n]) > 0 || unbounded(n.Func.Body) {
				continue
			}
			ok := true
			for _, e := range g.Edges[n] {
				k := e.Callee
				switch {
				case m.effects[e.Call]:
					ok = false
				case local(e):
					ok = ok && contained[Node{Func: k.fn}]
				case k.op == nil && !read(e):
					ok = false
				}
			}
			if ok {
				contained[n], changed = true, true
			}
		}
	}
	out := map[Node]bool{}
	for n := range contained {
		if reads[n] && !writesOwn(n.Func, g.Edges[n]) {
			out[n] = true
		}
	}
	return out
}

// unbounded reports whether a body holds a loop without an iterator, whose count of iterations the body decides.
func unbounded(b *syntax.Block) bool {
	found := false
	syntax.Inspect(b, func(n syntax.Node) bool {
		_, loop := n.(*syntax.For)
		found = found || loop
		return !found
	})
	return found
}

// writesOwn reports whether the body of a routine with the calls that it makes holds a store to a place of a parameter or of the receiver, or a call on one of a declared method or of a built-in method that changes its receiver.
func writesOwn(fn *syntax.Func, calls []Edge) bool {
	declared := map[syntax.Expr]bool{}
	for _, e := range calls {
		declared[e.Call] = e.Callee.op == nil || e.Callee.op.Row.Ref
	}
	params := map[string]bool{}
	for _, p := range fn.Params {
		params[p.Name] = true
	}
	var own func(x syntax.Expr) bool
	own = func(x syntax.Expr) bool {
		switch x := x.(type) {
		case *syntax.Name:
			return x.Module == "" && x.Owner == nil && params[x.Name]
		case *syntax.Self:
			return true
		case *syntax.FieldRead:
			return own(x.X)
		case *syntax.IndexRead:
			return own(x.X)
		}
		return false
	}
	found := false
	syntax.Inspect(fn.Body, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Store:
			for _, t := range n.Targets {
				found = found || own(t)
			}
		case *syntax.MethodCall:
			found = found || declared[n] && own(n.Receiver)
		}
		return !found
	})
	return found
}

// reach returns the nodes that make a call that seed selects, directly, through a callee whose call pass selects, or through a closure that inside lists for them.
func (g *CallGraph) reach(inside map[Node][]Node, seed, pass func(Edge) bool) map[Node]bool {
	reaches := map[Node]bool{}
	for changed := true; changed; {
		changed = false
		for _, n := range g.Nodes {
			if reaches[n] {
				continue
			}
			found := false
			for _, e := range g.Edges[n] {
				found = found || seed(e) || pass(e) && reaches[Node{Func: e.Callee.Func()}]
			}
			for _, c := range inside[n] {
				found = found || reaches[c]
			}
			if found {
				reaches[n], changed = true, true
			}
		}
	}
	return reaches
}
