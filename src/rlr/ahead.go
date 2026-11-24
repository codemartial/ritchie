// This file holds the loop pipeline: the reads that an iterator loop sends ahead of
// the iterations in front of them, and the walk of the source that keeps them.
package rlr

import (
	"iter"
	"maps"
)

// Lead holds the reads that one iteration of a loop sent ahead of its body.
type Lead struct {
	group *Group
	tasks []*Task
}

// Read sends a read ahead of its send and returns its task, which is no read of the routine until the body adopts it, or returns nil without a dispatch when the group starts no task or no place under the window of the invocation is free.
func (l *Lead) Read(dst Locator, method string, args []any) *Task {
	g := l.group
	if !g.starts() {
		return nil
	}
	t := &Task{group: g, ahead: true}
	if t.call = g.scope.inv.StartAhead(dst, method, args, g.watch(t)); t.call == nil {
		return nil
	}
	l.tasks = append(l.tasks, t)
	return t
}

// Run starts the body of a reader ahead of its send and returns its task as Read does, where the reads of the body take their places under the window when the body sends them.
func (l *Lead) Run(body func(Invocation) ([]any, error)) *Task {
	g := l.group
	if !g.starts() || !g.scope.inv.Free() {
		return nil
	}
	t := g.run(body)
	t.ahead = true
	l.tasks = append(l.tasks, t)
	return t
}

// drop cancels the reads that an iteration sent ahead and that its body did not reach.
func (l *Lead) drop() {
	for _, t := range l.tasks {
		if t.ahead {
			t.abandon()
		}
	}
	l.tasks = nil
}

// Adopt makes a task that was sent ahead a read of the routine at the send that the body has reached, so that the reads of the routine are created in the order of their sends, where a group that starts no task abandons the read, and a failure that the task reported while it was ahead is reported again, because the handler that the send gives the task can select it.
func (g *Group) Adopt(t *Task) {
	live := g.starts()
	t.ahead = false
	g.tasks = append(g.tasks, t)
	switch {
	case !live:
		t.abandon()
	case t.early != nil:
		g.report(t, t.early)
		t.early = nil
	}
}

// iteration is one iteration of a loop that the walk of the source has reached and the body has not: its element, what the generated code keeps for it, and the reads that it sent ahead.
type iteration[E, K any] struct {
	elem E
	kept K
	lead Lead
}

// Ahead returns the iterations of a loop whose reads run ahead: it walks the source once, at most the window of the invocation ahead of the body and only while a place under the window is free, calls lead for each element that the walk reaches, which computes what the reads of the iteration need and sends them, and yields each element with what lead kept for it, in the order of the source, where the reads of an iteration that its body did not reach are cancelled when the body ends, and those of the iterations that the body does not run when the loop ends.
func Ahead[E, K any](g *Group, src iter.Seq[E], lead func(E, *Lead) K) iter.Seq2[E, K] {
	return func(yield func(E, K) bool) {
		var queue []iteration[E, K]
		defer func() {
			for i := range queue {
				queue[i].lead.drop()
			}
		}()
		body := func() bool {
			it := queue[0]
			queue[0] = iteration[E, K]{}
			queue = queue[1:]
			defer it.lead.drop()
			return yield(it.elem, it.kept)
		}
		inv := g.scope.inv
		live := true
		src(func(e E) bool {
			for live && len(queue) > 0 && (len(queue) >= inv.Window() || !inv.Free()) {
				live = body()
			}
			if !live {
				return false
			}
			queue = append(queue, iteration[E, K]{elem: e, lead: Lead{group: g}})
			it := &queue[len(queue)-1]
			it.kept = lead(e, &it.lead)
			return true
		})
		for live && len(queue) > 0 {
			live = body()
		}
	}
}

// Each returns the entries of a dictionary as the source of a loop whose reads run ahead.
func Each[K comparable, V any](m map[K]V) iter.Seq[Keyed[K, V]] {
	return func(yield func(Keyed[K, V]) bool) {
		for k, v := range m {
			if !yield(Keyed[K, V]{Key: k, Val: v}) {
				return
			}
		}
	}
}

// EachKeyed returns the entries of a projected dictionary as the source of a loop whose reads run ahead.
func EachKeyed[P comparable, K, V any](m map[P]Keyed[K, V]) iter.Seq[Keyed[K, V]] {
	return maps.Values(m)
}
