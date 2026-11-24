// This file holds the scopes of a body, the context of a routine, and the one set of
// flow facts that the statement walk carries.
package check

import (
	"fmt"
	"maps"
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// binding is a name introduced in a body: a parameter, a variable, a local constant, a loop variable, or an arm binding, which has no type for a variable whose written type does not resolve, so that a use of that variable receives no further diagnostic.
type binding struct {
	name     string
	typ      syntax.Type
	constant bool
	value    *Constant
	decl     syntax.Node
	param    bool
	errors   RaiseSet
	read     bool
}

// scope is one lexical scope of a body.
type scope struct {
	parent *scope
	names  map[string]*binding
	outer  bool
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, names: map[string]*binding{}}
}

// lookup returns the binding of a name in this scope or an enclosing one.
func (s *scope) lookup(name string) *binding {
	for sc := s; sc != nil; sc = sc.parent {
		if b := sc.names[name]; b != nil {
			return b
		}
	}
	return nil
}

// captures reports whether a use of a name in this scope reads a binding that a routine outside the innermost closure introduces.
func (s *scope) captures(name string) bool {
	crossed := false
	for sc := s; sc != nil; sc = sc.parent {
		if sc.names[name] != nil {
			return crossed
		}
		crossed = crossed || sc.outer && sc.parent != nil
	}
	return false
}

// declared reports whether this scope, or the outermost scope of the routine when this scope is it, introduces the name.
func (s *scope) declared(name string) bool {
	return s.names[name] != nil
}

// routine is the context of the body being typed: the declaration, its results, its receiver, and the control context.
type routine struct {
	fn       *syntax.Func
	closure  *syntax.Closure
	entry    *resolve.Entry
	module   string
	results  []syntax.Type
	none     bool
	receiver syntax.Type
	loops    []*loop
	arm      *armContext
	node     Node
	facts    *flow
}

// loop is one enclosing loop of the statement being walked, with the flow facts of its breaks and of its continues.
type loop struct {
	label     string
	node      syntax.Stmt
	breaks    []flow
	continues []flow
	inArm     bool
}

// armContext is the handler arm being walked: the results of the statement it recovers, and the loops outside the handled statement.
type armContext struct {
	results   []syntax.Type
	hasValues bool
	loops     []*loop
	outer     *armContext
	noop      bool
	compound  bool
}

// flow is the one set of flow facts: whether the path is live, the variables definitely assigned, the variables that a store has definitely given a value since the start of the innermost brace handler arm, the nullable paths known present, for each path that a store, a call, or a join removed the origin that removed it, and the edge that the facts reach a join by.
type flow struct {
	live     bool
	assigned map[*binding]bool
	stores   map[*binding]bool
	present  map[string]bool
	lost     map[string]string
	why      string
}

func newFlow() flow {
	return flow{live: true, assigned: map[*binding]bool{}, stores: map[*binding]bool{}, present: map[string]bool{}, lost: map[string]string{}}
}

// copy returns an independent copy of the facts.
func (f flow) copy() flow {
	return flow{live: f.live, assigned: maps.Clone(f.assigned), stores: maps.Clone(f.stores), present: maps.Clone(f.present), lost: maps.Clone(f.lost), why: f.why}
}

// dead returns the facts of a path that cannot be reached.
func (f flow) dead() flow {
	out := f.copy()
	out.live = false
	return out
}

// edge returns a copy of the facts that reach a join by the edge that why describes.
func (f flow) edge(why string) flow {
	out := f.copy()
	out.why = why
	return out
}

// add records a path as present.
func (f flow) add(path string) {
	f.present[path] = true
	delete(f.lost, path)
}

// enclosed empties the present paths at the start of a closure body, because the body can run after any later store to a captured path.
func (f flow) enclosed(at string) {
	clear(f.lost)
	f.remove("in the closure body at "+at+", which starts with no present path", func(string) bool { return true })
}

// remove removes the present paths that a store or a call ends, recording why for each.
func (f flow) remove(why string, ends func(p string) bool) {
	for p := range f.present {
		if ends(p) {
			delete(f.present, p)
			if why != "" {
				f.lost[p] = why
			}
		}
	}
}

// drop removes a stored place from the present paths: the path and every path under it.
func (f flow) drop(path, why string) {
	if path != "" {
		f.remove(why, func(p string) bool { return p == path || strings.HasPrefix(p, path+".") })
	}
}

// mutate removes the paths under the receiver of a struct method call, which keeps the receiver itself present.
func (f flow) mutate(path, why string) {
	if path != "" {
		f.remove(why, func(p string) bool { return strings.HasPrefix(p, path+".") })
	}
}

// meet joins the facts of two paths.
func meet(a, b flow) flow {
	return meetAll([]flow{a, b})
}

// meetAll joins the facts of several paths: live when one is, and assigned or present only when every live path says so, because a path that no execution takes carries every fact, where each path that the join removes records the edge of a live path that lacks it, with the store or call that ended it on that edge, so that a use of the path can report that origin.
func meetAll(paths []flow) flow {
	var live []flow
	for _, p := range paths {
		if p.live {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		out := newFlow()
		if len(paths) > 0 {
			out = paths[len(paths)-1].copy()
		}
		out.live, out.why = false, ""
		return out
	}
	out := live[0].copy()
	out.why, out.lost = "", map[string]string{}
	for _, p := range live[1:] {
		maps.DeleteFunc(out.assigned, func(v *binding, _ bool) bool { return !p.assigned[v] })
		maps.DeleteFunc(out.stores, func(v *binding, _ bool) bool { return !p.stores[v] })
		maps.DeleteFunc(out.present, func(path string, _ bool) bool { return !p.present[path] })
	}
	for _, p := range live {
		for path := range p.present {
			if out.present[path] || out.lost[path] != "" {
				continue
			}
			for _, e := range live {
				if !e.present[path] {
					out.lost[path] = e.lacks(path)
					break
				}
			}
		}
	}
	for _, p := range live {
		for path, why := range p.lost {
			if !out.present[path] && out.lost[path] == "" {
				out.lost[path] = why
			}
		}
	}
	return out
}

// lacks describes why a path is absent on the edge of these facts: the edge, followed by the store or call that ended the path on it.
func (f flow) lacks(path string) string {
	why := f.lost[path]
	switch {
	case why == "":
		return f.why
	case f.why != "" && strings.HasPrefix(why, "after "):
		return f.why + ", " + why
	}
	return why
}

// trial records how much state a walk of a loop body has appended, so that a walk from head facts that are not yet stable can be undone before the next one.
type trial struct {
	faults, diags, readable, nodes, edges int
	breaks, continues                     []int
}

// mark records the state that a walk of a loop body appends to: the diagnostics, the variables that must be read, the call graph, and the transfers to the enclosing loops.
func (c *checker) mark(r *routine) trial {
	t := trial{faults: len(c.faults), diags: len(c.diags), readable: len(c.readable), nodes: len(c.nodes), edges: len(c.edges[r.node])}
	for _, l := range r.loops {
		t.breaks = append(t.breaks, len(l.breaks))
		t.continues = append(t.continues, len(l.continues))
	}
	return t
}

// undo removes what a walk appended after a mark, where the closures the walk typed leave the call graph with their calls.
func (c *checker) undo(r *routine, t trial) {
	c.faults, c.diags, c.readable = c.faults[:t.faults], c.diags[:t.diags], c.readable[:t.readable]
	for _, n := range c.nodes[t.nodes:] {
		delete(c.edges, n)
	}
	c.nodes = c.nodes[:t.nodes]
	if edges := c.edges[r.node]; len(edges) > t.edges {
		c.edges[r.node] = edges[:t.edges]
	}
	for i, l := range r.loops {
		l.breaks, l.continues = l.breaks[:t.breaks[i]], l.continues[:t.continues[i]]
	}
}

// fixedPoint walks a loop body from the greatest fixed point of the present paths at its head: each walk starts from the entry facts restricted to the paths that the previous walk kept on every edge back to the head, and the walk whose head facts are stable is kept, because the transfer of a walk only removes facts as its head loses them.
func (c *checker) fixedPoint(l *loop, r *routine, entry flow, walk func(head flow) flow) flow {
	head := entry.copy()
	for {
		t := c.mark(r)
		l.breaks, l.continues = nil, nil
		end := walk(head.copy())
		back := fmt.Sprintf("on the edge back to the head of the loop at %s", where(l.node))
		next := meetAll(append([]flow{head.edge("on entry to the loop at " + where(l.node)), end.edge(back)}, l.continues...))
		if !entry.live || len(next.present) == len(head.present) {
			return head
		}
		c.undo(r, t)
		head.present, head.lost = next.present, next.lost
	}
}

// where returns the source position of a node as a diagnostic message states it.
func where(n syntax.Node) string {
	o := n.Where()
	p := o.File.Pos(o.Start)
	return fmt.Sprintf("line %d, column %d", p.Line, p.Column)
}

// text returns the source text of a node, quoted as a diagnostic message quotes code.
func text(n syntax.Node) string {
	o := n.Where()
	return diagnostics.Code(o.File.Text[o.Start:o.End])
}
