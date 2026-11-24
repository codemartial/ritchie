// This file holds the remote calls of a routine: the scope under which it sends them,
// and the group, tasks, and futures through which generated code sends a read, or
// starts the body of a reader, before the routine waits for its outcome.
package rlr

import (
	"context"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"ritchie.dev/rlr/mesh"
)

// Scope is the part of an invocation under which one routine sends its calls, with the context of the task whose body the routine runs in, which is nil outside a task and whose cancellation ends every wait under the scope.
type Scope struct {
	inv *mesh.Invocation
	ctx context.Context
}

// cancelled returns the channel that is closed when the task of the scope is cancelled, which is nil outside a task and for no scope.
func (s *Scope) cancelled() <-chan struct{} {
	if s == nil || s.ctx == nil {
		return nil
	}
	return s.ctx.Done()
}

// stopped returns the channel that is closed when the cancel signal of the caller of the invocation arrives, which is nil for no scope.
func (s *Scope) stopped() <-chan struct{} {
	if s == nil || s.inv == nil {
		return nil
	}
	return s.inv.Stopped()
}

// halted reports whether the cancel signal of the caller has arrived or the task of the scope is cancelled, after which the scope sends no call.
func (s *Scope) halted() bool {
	select {
	case <-s.stopped():
		return true
	case <-s.cancelled():
		return true
	default:
		return false
	}
}

// Invocation is the record a generated routine carries so that it can call services as part of its own invocation.
type Invocation = *Scope

// Call sends one remote call and waits for its outcome, where a halted scope does not send the call and its outcome is that of a call that was not sent.
func (s *Scope) Call(dst Locator, method string, mutates bool, args []any) ([]any, error) {
	if s.halted() {
		return nil, failureOf(mesh.Expired(), mutates)
	}
	r, _ := s.inv.Start(dst, method, mutates, args).Settle(s.inv.Stopped(), s.cancelled())
	return r.AppResponse.Results, failureOf(r, mutates)
}

// Task is a read that a routine sent, or the body of a reader that it started, and has not waited for, with the handler that decides how a failure ends it, where a read has a call, a body has the channel that receives its outcome and the function that cancels its context, a body that ran in place and a task that the group did not start have their outcome from the start, a task that the routine has begun to wait for is awaited, and a task that a loop sent ahead of its send is ahead until the body adopts it, and keeps a failure that it reported before as early.
type Task struct {
	group     *Group
	call      *mesh.Call
	ended     chan outcome
	ready     *outcome
	cancel    context.CancelFunc
	handler   func(error) ([]any, error)
	total     bool
	abandoned bool
	awaited   bool
	ahead     bool
	early     error
	results   []any
	err       error
	done      bool
}

// outcome is what the body of a task ended with: its results or its failure.
type outcome struct {
	results []any
	err     error
}

// failure is a task that ended with a failure, as the goroutine that ended it reports it to the group.
type failure struct {
	task *Task
	err  error
}

// Group holds the tasks that one activation of a routine has created and has not waited for, in the order of their creation, and which the goroutine of the activation alone uses with its tasks, except for the failures that a task that ends reports under the mutex with their count and the channel that tells the activation of one, and which is failed once a task has ended with a failure that its handler does not select, after which every exit of the routine lies at or before that task and the group starts no task.
type Group struct {
	scope  *Scope
	tasks  []*Task
	failed bool

	reported atomic.Int32
	mu       sync.Mutex
	failures []failure
	wake     chan struct{}
}

// Open opens the group of a routine body.
func Open(inv Invocation) *Group { return &Group{scope: inv} }

// report records, from the goroutine that ended a task, that the task ended with a failure, and tells an activation that waits.
func (g *Group) report(t *Task, err error) {
	g.mu.Lock()
	g.failures = append(g.failures, failure{t, err})
	wake := g.wake
	g.mu.Unlock()
	g.reported.Add(1)
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// waker returns the channel that tells the activation of a reported failure while it waits for a task beside others, which a group with one task does not need, because the failure of a task can cancel only another task.
func (g *Group) waker() <-chan struct{} {
	if len(g.tasks) < 2 {
		return nil
	}
	g.mu.Lock()
	if g.wake == nil {
		g.wake = make(chan struct{}, 1)
	}
	wake := g.wake
	g.mu.Unlock()
	g.sweep()
	return wake
}

// sweep takes the reported failures and, for each that the handler of its task does not select, cancels the tasks created after that task, because the routine leaves at or before it and the exit cancels them, where the cancelled tasks are always the last of the group, so that each task is cancelled once, where the failure of a task that the routine has begun to wait for cancels none, because that wait takes the failure, as an outcome that an arm can select or as the exit of the routine, and where the failure of a task that is ahead is kept on the task, because the task is no read of the routine yet.
func (g *Group) sweep() {
	if g.reported.Load() == 0 {
		return
	}
	g.mu.Lock()
	failures := g.failures
	g.failures = nil
	g.reported.Store(0)
	g.mu.Unlock()
	for _, f := range failures {
		t := f.task
		if t.done || t.total || t.abandoned || t.awaited {
			continue
		}
		if t.ahead {
			t.early = f.err
			continue
		}
		if t.handler != nil {
			if _, err := t.handler(f.err); err == nil {
				continue
			}
		}
		g.failed = true
		later := g.tasks[slices.Index(g.tasks, t)+1:]
		for _, l := range later {
			if l.abandoned {
				break
			}
			l.abandon()
		}
	}
}

// starts reports whether the group starts a new task, which it does not after a task has failed or when its scope is halted.
func (g *Group) starts() bool {
	g.sweep()
	return !g.failed && !g.scope.halted()
}

// settled adds to the group a task that holds its outcome from the start.
func (g *Group) settled(o outcome) *Task {
	t := &Task{group: g, ready: &o}
	g.tasks = append(g.tasks, t)
	return t
}

// unsent adds to the group the task of a read or a body that the group does not start, whose outcome is that of a read that was not sent.
func (g *Group) unsent() *Task {
	t := g.settled(outcome{err: failureOf(mesh.Expired(), false)})
	t.abandoned = true
	return t
}

// Read sends a read and returns its task, whose response the routine has not waited for, where a group that starts no task does not send the read.
func (g *Group) Read(dst Locator, method string, args []any) *Task {
	if !g.starts() {
		return g.unsent()
	}
	t := &Task{group: g}
	g.tasks = append(g.tasks, t)
	t.call = g.scope.inv.StartWatched(dst, method, args, g.watch(t))
	return t
}

// watch returns the function that reports the failure of the read of a task to the group when the read settles.
func (g *Group) watch(t *Task) func(mesh.Response) {
	return func(r mesh.Response) {
		if err := failureOf(r, false); err != nil {
			g.report(t, err)
		}
	}
}

// Run starts the body of a reader as a task of the group and returns the task, whose outcome the routine has not waited for, where a group that starts no task does not start the body.
func (g *Group) Run(body func(Invocation) ([]any, error)) *Task {
	if !g.starts() {
		return g.unsent()
	}
	t := g.run(body)
	g.tasks = append(g.tasks, t)
	return t
}

// run starts the body of a reader on a goroutine of its own, under a scope whose context the cancellation of the task cancels, and returns its task, where the body shares no storage with the routine, so the goroutine needs no lock, and sends one outcome to a channel with room for it, so the goroutine ends whether or not the routine waits, and where a host fault of the body is contained as the fault of an entry is: its defect report is written, and the failure of the task is the Rejected! that the caller of a remote read receives when the handler of the read faults.
func (g *Group) run(body func(Invocation) ([]any, error)) *Task {
	parent := g.scope.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	t := &Task{group: g, ended: make(chan outcome, 1), cancel: cancel}
	scope := &Scope{inv: g.scope.inv, ctx: ctx}
	go func() {
		o := outcome{err: NewError(Rejected.TypeID(), nil)}
		guarded(g.scope.inv.Contains(), func() int {
			results, err := body(scope)
			o = outcome{results: results, err: err}
			return 0
		})
		t.ended <- o
		if o.err != nil {
			g.report(t, o.err)
		}
	}()
	return t
}

// RunAtOnce starts the body of a reader that the routine waits for before it sends anything else, where a group that holds no other task runs the body in place under its own scope, because no task can overlap it or be cancelled by it, and returns a task that holds the outcome.
func (g *Group) RunAtOnce(body func(Invocation) ([]any, error)) *Task {
	if len(g.tasks) > 0 || !g.starts() {
		return g.Run(body)
	}
	results, err := body(g.scope)
	return g.settled(outcome{results: results, err: err})
}

// Handle gives a task the handler of its call, which returns for a failure the results that an arm supplies, or the failure that stays: the failure itself when no arm selects it, and the exit of an arm that leaves the routine.
func (t *Task) Handle(handler func(error) ([]any, error)) { t.handler = handler }

// exit is the failure of a task whose handler arm leaves the routine with values, which a wait of the routine turns into its return.
type exit struct{ values []any }

// Error names the exit for a report, which a routine that acts on every exit does not write.
func (*exit) Error() string { return "the handler of a read left its routine" }

// Leaving returns the failure with which a handler arm leaves the routine with values.
func Leaving(values ...any) error { return &exit{values: values} }

// Left returns the values of a handler arm that leaves the routine when a failure is such an exit.
func Left(err error) ([]any, bool) {
	e, ok := err.(*exit)
	if !ok {
		return nil, false
	}
	return e.values, true
}

// Total records that the handler of a task selects every failure, so that the task cannot fail and a use of the results of a later task does not wait for it.
func (t *Task) Total() { t.total = true }

// abandon cancels a task that the routine has not waited for and leaves it in its group, so that a wait for it takes the earlier tasks first.
func (t *Task) abandon() {
	t.abandoned = true
	switch {
	case t.done:
	case t.call != nil:
		t.call.Abandon()
	case t.cancel != nil:
		t.cancel()
	}
}

// before returns the first task that the group created before a task, has not waited for, and that can fail, or nil when there is none.
func (g *Group) before(t *Task) *Task {
	for _, e := range g.tasks {
		if e == t {
			break
		}
		if !e.total {
			return e
		}
	}
	return nil
}

// Earlier records that the routine has begun to wait for a task, waits for every task that can fail and that the group created before the task and has not waited for, in the order of creation, and returns the first failure in that order.
func (t *Task) Earlier() error {
	t.awaited = true
	for !t.done {
		e := t.group.before(t)
		if e == nil {
			break
		}
		if err := e.settle(); err != nil {
			return err
		}
	}
	return nil
}

// Join waits for every task that the group created before a task, in the order of creation, and then for the task, and returns the first failure in that order.
func (t *Task) Join() error {
	if err := t.Earlier(); err != nil {
		return err
	}
	return t.settle()
}

// Outcome waits for the response of a task and returns its results, or its failure.
func (t *Task) Outcome() ([]any, error) {
	err := t.settle()
	return t.results, err
}

// finish waits for the body of a task and returns its results or its failure, where a cancel signal of the caller or the cancellation of the scope of the routine cancels the body and is the failure, and where a reported failure makes the group cancel the tasks that it dooms.
func (t *Task) finish() ([]any, error) {
	scope := t.group.scope
	for {
		select {
		case o := <-t.ended:
			t.cancel()
			return o.results, o.err
		case <-t.group.waker():
		case <-scope.inv.Stopped():
			t.cancel()
			return nil, NewError(Cancelled.TypeID(), nil)
		case <-scope.cancelled():
			t.cancel()
			return nil, NewError(Cancelled.TypeID(), nil)
		}
	}
}

// respond waits for the response of the read of a task, where a reported failure makes the group cancel the tasks that it dooms.
func (t *Task) respond() mesh.Response {
	scope := t.group.scope
	for {
		if r, settled := t.call.Await(t.group.waker(), scope.inv.Stopped(), scope.cancelled()); settled {
			return r
		}
	}
}

// settle waits for the outcome of a task once, takes the task out of its group, and returns its failure, where a failure that the handler of the task selects becomes the results that the handler supplies or the exit of the arm that leaves the routine.
func (t *Task) settle() error {
	if t.done {
		return t.err
	}
	t.done = true
	switch {
	case t.call != nil:
		r := t.respond()
		t.results, t.err = r.AppResponse.Results, failureOf(r, false)
	case t.ended != nil:
		t.results, t.err = t.finish()
	default:
		t.results, t.err = t.ready.results, t.ready.err
	}
	if t.err != nil && t.handler != nil {
		t.results, t.err = t.handler(t.err)
	}
	g := t.group
	if len(g.tasks) > 0 && g.tasks[0] == t {
		g.tasks[0] = nil
		g.tasks = g.tasks[1:]
	} else if i := slices.Index(g.tasks, t); i >= 0 {
		g.tasks = slices.Delete(g.tasks, i, i+1)
	}
	return t.err
}

// Join waits for every task of the group, in the order of creation, and returns the first failure in that order.
func (g *Group) Join() error {
	for len(g.tasks) > 0 {
		if err := g.tasks[0].settle(); err != nil {
			return err
		}
	}
	return nil
}

// Cancel abandons every read and cancels every body of the group that the routine has not waited for.
func (g *Group) Cancel() {
	for _, t := range g.tasks {
		t.abandon()
		t.done, t.err = true, NewError(Cancelled.TypeID(), nil)
	}
	g.tasks = nil
}

// Future is the value of a local that can hold the result of a read: the value itself, or the task and the position of the result that the value comes from.
type Future[T any] struct {
	V    T
	task *Task
	at   int
}

// Ready returns the future of a known value.
func Ready[T any](v T) Future[T] { return Future[T]{V: v} }

// Pending returns the future of the result of a task at a position.
func Pending[T any](t *Task, at int) Future[T] { return Future[T]{task: t, at: at} }

// Wait waits for the response that a future comes from and makes the future hold its value, copied by clone when the value shares storage, or returns the failure of the response.
func (f *Future[T]) Wait(clone func(T) T) error {
	if f.task == nil {
		return nil
	}
	if err := f.task.Join(); err != nil {
		return err
	}
	f.V = f.task.results[f.at].(T)
	if clone != nil {
		f.V = clone(f.V)
	}
	f.task = nil
	return nil
}

// Copy returns a future that a move puts into a second place, with a known value copied by clone when it shares storage.
func (f Future[T]) Copy(clone func(T) T) Future[T] {
	if f.task == nil && clone != nil {
		f.V = clone(f.V)
	}
	return f
}

// Futures is the value of a local array whose elements can hold the results of reads: the elements, and for each element that is not known the future that it comes from.
type Futures[T any] struct {
	V       []T
	pending []Future[T]
}

// ReadyAll returns the array of futures of a known array.
func ReadyAll[T any](v []T) Futures[T] { return Futures[T]{V: v} }

// Collect returns the array of the futures.
func Collect[T any](elems ...Future[T]) Futures[T] {
	var a Futures[T]
	for _, e := range elems {
		a.Append(e)
	}
	return a
}

// Len returns the count of the elements of an array of futures.
func (a Futures[T]) Len() int { return len(a.V) }

// Append adds a future as the last element of an array of futures.
func (a *Futures[T]) Append(f Future[T]) {
	a.V = append(a.V, f.V)
	if f.task != nil || a.pending != nil {
		a.pending = append(slices.Grow(a.pending, len(a.V))[:len(a.V)-1], f)
	}
}

// Set stores a future in the element of an array of futures at an index that the caller has checked.
func (a *Futures[T]) Set(i int64, f Future[T]) {
	a.V[i] = f.V
	if f.task != nil || a.pending != nil {
		a.pending = slices.Grow(a.pending, len(a.V))[:len(a.V)]
		a.pending[i] = f
	}
}

// At returns the future of the element of an array of futures at an index that the caller has checked.
func (a Futures[T]) At(i int64, clone func(T) T) Future[T] {
	if a.pending != nil && a.pending[i].task != nil {
		return a.pending[i]
	}
	return Ready(a.V[i]).Copy(clone)
}

// Wait waits for each element that is not known, in the order of the elements, and makes the array hold its values, or returns the first failure.
func (a *Futures[T]) Wait(clone func(T) T) error {
	for i := range a.pending {
		if a.pending[i].task == nil {
			continue
		}
		if err := a.pending[i].Wait(clone); err != nil {
			return err
		}
		a.V[i] = a.pending[i].V
	}
	a.pending = nil
	return nil
}

// Copy returns an array of futures that a move puts into a second place, with its known elements copied by clone when they share storage.
func (a Futures[T]) Copy(clone func(T) T) Futures[T] {
	out := Futures[T]{V: slices.Clone(a.V), pending: slices.Clone(a.pending)}
	if clone != nil {
		for i := range out.V {
			out.V[i] = clone(out.V[i])
		}
	}
	return out
}

// FutureMap is the value of a local dictionary whose values can hold the results of reads: the Go map of the dictionary, which holds an entry with no value for each value that is not known, and for each such value the future that it comes from with the function that makes the entry of the value.
type FutureMap[P comparable, E, V any] struct {
	V       map[P]E
	pending map[P]pendingEntry[E, V]
}

// pendingEntry is the future of a value of a dictionary with the function that makes the entry of the dictionary from the value.
type pendingEntry[E, V any] struct {
	future Future[V]
	entry  func(V) E
}

// ReadyMap returns the dictionary of futures of a known dictionary.
func ReadyMap[P comparable, E, V any](m map[P]E) FutureMap[P, E, V] { return FutureMap[P, E, V]{V: m} }

// EntryOfKeyed returns the function that makes the entry stored under the projected key p from a value, retaining the key value of an entry already there.
func EntryOfKeyed[P comparable, K, V any](m map[P]Keyed[K, V], p P, k K) func(V) Keyed[K, V] {
	if entry, found := m[p]; found {
		k = entry.Key
	}
	return func(v V) Keyed[K, V] { return Keyed[K, V]{Key: k, Val: v} }
}

// Len returns the count of the entries of a dictionary of futures.
func (d FutureMap[P, E, V]) Len() int { return len(d.V) }

// Set stores a future as the value under a key, with the function that makes the entry of the dictionary from a value.
func (d *FutureMap[P, E, V]) Set(p P, f Future[V], entry func(V) E) {
	if d.V == nil {
		d.V = map[P]E{}
	}
	d.V[p] = entry(f.V)
	delete(d.pending, p)
	if f.task != nil {
		if d.pending == nil {
			d.pending = map[P]pendingEntry[E, V]{}
		}
		d.pending[p] = pendingEntry[E, V]{f, entry}
	}
}

// At returns the future of the value under a key that the caller has checked, where known is the value that the entry of the key holds.
func (d FutureMap[P, E, V]) At(p P, known V, clone func(V) V) Future[V] {
	if e, ok := d.pending[p]; ok {
		return e.future
	}
	return Ready(known).Copy(clone)
}

// Wait waits for each value that is not known and makes the dictionary hold its values, or returns the first failure, which is the failure of the read that the routine created first, because the wait for a future takes every earlier read first.
func (d *FutureMap[P, E, V]) Wait(clone func(V) V) error {
	for p, e := range d.pending {
		if err := e.future.Wait(clone); err != nil {
			return err
		}
		d.V[p] = e.entry(e.future.V)
		delete(d.pending, p)
	}
	return nil
}

// Copy returns a dictionary of futures that a move puts into a second place, with its Go map copied by clone.
func (d FutureMap[P, E, V]) Copy(clone func(map[P]E) map[P]E) FutureMap[P, E, V] {
	return FutureMap[P, E, V]{V: clone(d.V), pending: maps.Clone(d.pending)}
}
