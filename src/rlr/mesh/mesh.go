// Package mesh is the target-independent invocation layer beneath a language runtime.
package mesh

import (
	"io"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Locator names a logical unit independently of process.
type Locator struct {
	Publication, Module, Unit string
}

// Envelope identifies one invocation with its allowance as a remaining duration and the margin of its call tree.
type Envelope struct {
	Source, Destination  Locator
	Origin, RID, Attempt uint64
	Budget               time.Duration
	Margin               time.Duration
}

// Request is one invocation as a unit receives it, where the target closes Stop when the cancel signal of the caller arrives.
type Request struct {
	Envelope Envelope
	Method   string
	Args     []any
	Stop     <-chan struct{}
}

// AppResponse is the final response that the handler of a completed invocation returns.
type AppResponse struct {
	Rejected bool
	Err      error
	Results  []any
}

// Response is the one response of a started invocation.
type Response struct {
	Done, Clean bool
	Expired     bool
	AppResponse AppResponse
}

// Declined is the response of an invocation whose handler did not start.
func Declined() Response { return Response{Clean: true} }

// Expired is the response of an invocation whose handler did not start before its usable budget ended.
func Expired() Response { return Response{Clean: true, Expired: true} }

// Pending delivers the one response of a started invocation.
type Pending <-chan Response

// Settled is a pending invocation whose response is already r.
func Settled(r Response) Pending {
	ch := make(chan Response, 1)
	ch <- r
	return ch
}

// Handler runs one invocation of a unit and returns its final response.
type Handler func(inv *Invocation, req Request) AppResponse

// Observer is a unit that observes each invocation of it with the arguments of the call when the caller enqueues the invocation, before the caller's telemetry records it.
type Observer interface {
	Enqueued(Request)
}

// Unit starts invocations on one logical unit and delivers the cancel signal of a caller to the invocation that the envelope identifies.
type Unit interface {
	Invoke(Request) Response
	Cancel(Envelope)
	Telemetry() Sink
}

// Realization is what a target supplies as the units of an application behind one interface.
type Realization interface {
	Locate(Locator) (Unit, bool)
	Telemetry() Sink
}

// UnitSpec is one unit of the application as the runtime hands it to the mesh.
type UnitSpec struct {
	Locator Locator
	Handler Handler
}

// Hosted is one unit as a target receives it.
type Hosted struct {
	Locator Locator
	Serve   func(Request) Response
}

// RecordKind classifies a telemetry record.
type RecordKind uint8

const (
	UnitConstructed RecordKind = iota + 1
	EntryStarted
	Dispatched
	HandlerStarted
	HandlerFinished
	ResponseProduced
	Enqueued
	InvocationSettled
	CancelSent
	CancelReceived
)

// Record is one telemetry record, where Method is the method of an outgoing invocation and Outcome is the error that the caller of a settled invocation receives, or the empty name for a value.
type Record struct {
	Kind        RecordKind
	Envelope    Envelope
	Parent      uint64
	Done, Clean bool
	Mutates     bool
	Method      string
	Outcome     string
}

// Sink receives telemetry.
type Sink interface {
	Record(Record)
}

const DefaultLimit = 255

// Runtime is the caller runtime of one process, whose Limit bounds the outgoing invocations that each unit has in flight and the reads that each invocation has in the queue of its unit or in flight, and whose Outcome names the error a caller receives for a response of a method that can or cannot mutate, or the empty name for a value.
type Runtime struct {
	Limit       int
	Outcome     func(r Response, mutates bool) string
	realization Realization
	defects     io.Writer
	contains    bool
	ids         atomic.Uint64
	lanes       sync.Map
}

// lane holds the outgoing invocations of one unit: the count in flight and the invocations that wait for a place under the limit, in order of dispatch.
type lane struct {
	mu      sync.Mutex
	active  int
	waiting []*Call
}

// New is the caller runtime of a process whose units the realization built by host serves and whose contained host faults are reported to defects.
func New(units []UnitSpec, host func([]Hosted) Realization, defects io.Writer) *Runtime {
	rt := &Runtime{defects: defects, contains: Contains()}
	hosted := make([]Hosted, len(units))
	for i, u := range units {
		hosted[i] = Hosted{Locator: u.Locator, Serve: rt.contained(u.Handler)}
	}
	rt.realization = host(hosted)
	return rt
}

// Realization is the realization the runtime dispatches through.
func (rt *Runtime) Realization() Realization { return rt.realization }

// limit returns the bound of the runtime on the outgoing invocations of a unit in flight, which is the window on the reads of an invocation.
func (rt *Runtime) limit() int {
	if rt.Limit <= 0 {
		return DefaultLimit
	}
	return rt.Limit
}

// Invocation is the record of one invocation in progress, with the window on its reads under the mutex: the count of its reads that hold a place, which are those in the queue of its unit or in flight, and the reads that it holds until a place is free, in order of dispatch.
type Invocation struct {
	Envelope Envelope
	rt       *Runtime
	unclean  atomic.Bool
	altered  atomic.Bool
	deadline time.Time
	stop     <-chan struct{}

	mu     sync.Mutex
	placed int
	held   []*Call
}

// Window returns the count of reads that the invocation can have in the queue of its unit or in flight.
func (inv *Invocation) Window() int { return inv.rt.limit() }

// Free reports whether a place under the window of the invocation is free.
func (inv *Invocation) Free() bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	return inv.placed < inv.rt.limit()
}

// reserve takes a place under the window of the invocation when one is free and reports whether it took one.
func (inv *Invocation) reserve() bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if inv.placed >= inv.rt.limit() {
		return false
	}
	inv.placed++
	return true
}

// vacate frees the place of a settled read and gives it to the first read that the invocation holds and that has not settled, which enters the queue of its unit at the end.
func (inv *Invocation) vacate(c *Call) {
	inv.mu.Lock()
	if !c.placed {
		inv.mu.Unlock()
		return
	}
	c.placed = false
	var next *Call
	for len(inv.held) > 0 && next == nil {
		h := inv.held[0]
		inv.held[0] = nil
		inv.held = inv.held[1:]
		if h.state.Load() == callWaiting {
			next, h.placed = h, true
		}
	}
	if next == nil {
		inv.placed--
	}
	inv.mu.Unlock()
	if next != nil {
		next.enter()
	}
}

// Stopped is closed when the cancel signal of the caller has arrived.
func (inv *Invocation) Stopped() <-chan struct{} { return inv.stop }

// Altered reports whether the invocation may have altered relevant state: an outgoing invocation that can mutate completed, or was sent and settled without a response.
func (inv *Invocation) Altered() bool { return inv.altered.Load() || inv.unclean.Load() }

// Client is the record of a caller outside every unit under the budget of an external trigger.
func (rt *Runtime) Client(budget time.Duration) *Invocation {
	origin := rt.ids.Add(1)
	return &Invocation{Envelope: Envelope{Origin: origin, RID: origin, Budget: budget}, rt: rt, deadline: time.Now().Add(budget)}
}

const allowanceShare = 70

// Trigger is the record of an external trigger under an entry budget, whose allowance pool is allowanceShare percent of the budget and whose margin is the rest.
func (rt *Runtime) Trigger(entry time.Duration) *Invocation {
	inv := rt.Client(entry * allowanceShare / 100)
	inv.Envelope.Margin = entry - inv.Envelope.Budget
	return inv
}

// Entry is the trigger of the program's entry run under the entry budget and records that the entry has started.
func (rt *Runtime) Entry(budget time.Duration) *Invocation {
	inv := rt.Trigger(budget)
	rt.realization.Telemetry().Record(Record{Kind: EntryStarted, Envelope: inv.Envelope})
	return inv
}

// Remaining is the time left of the invocation's budget.
func (inv *Invocation) Remaining() time.Duration { return time.Until(inv.deadline) }

const (
	callWaiting uint32 = iota
	callSent
	callDispatched
	callEnded
)

// Call is one outgoing invocation of a caller from its dispatch to its local settlement, which is the arrival of its response, the caller deadline, or the caller's abandonment, whichever is first, and which passes from waiting for a place under the limit, to holding one as sent, to being dispatched to its destination when allowance remains, to ended when it settles, where placed, under the mutex of its invocation, records that a read holds a place under the window.
type Call struct {
	inv     *Invocation
	unit    Unit
	mutates bool
	placed  bool
	lane    *lane
	state   atomic.Uint32
	reply   chan Response

	mu     sync.Mutex
	req    Request
	expiry *time.Timer
	watch  func(Response)
}

// Start dispatches one invocation of method at dst with the arguments, recording whether the method carries the mark "can mutate", and returns without a wait: the invocation is sent at once when the calling unit has a place under its limit and otherwise when an earlier invocation of the unit settles, and it settles at the caller deadline at the latest.
func (inv *Invocation) Start(dst Locator, method string, mutates bool, args []any) *Call {
	return inv.start(dst, method, mutates, args, true, false, nil)
}

// StartWatched dispatches one invocation of a method that cannot mutate as Start does, and its settlement calls watch with the response, on the goroutine that settles it, after the response is ready for the demand point.
func (inv *Invocation) StartWatched(dst Locator, method string, args []any, watch func(Response)) *Call {
	return inv.start(dst, method, false, args, true, false, watch)
}

// StartAhead dispatches a read as StartWatched does when a place under the window of the invocation is free, and returns nil without a dispatch when none is, because a read that runs ahead of its statement takes no place from a read that its routine has reached.
func (inv *Invocation) StartAhead(dst Locator, method string, args []any, watch func(Response)) *Call {
	return inv.start(dst, method, false, args, true, true, watch)
}

// start dispatches one invocation as Start does, with a caller deadline when bounded, and it gives up the place it took when the expiry settles the invocation before the send, which is why the mutex of a call guards the request budget that the send writes and the expiry timer that the expiry goroutine reads while the start arms it, where a read enters the queue of its unit when it has a place under the window of the invocation, and the invocation holds it otherwise, unless it runs ahead, when a window without a free place refuses it.
func (inv *Invocation) start(dst Locator, method string, mutates bool, args []any, bounded, ahead bool, watch func(Response)) *Call {
	c := &Call{inv: inv, mutates: mutates, reply: make(chan Response, 1), watch: watch}
	unit, found := inv.rt.realization.Locate(dst)
	if !found {
		c.state.Store(callEnded)
		c.reply <- Declined()
		if watch != nil {
			watch(Declined())
		}
		return c
	}
	if ahead {
		if !inv.reserve() {
			return nil
		}
		c.placed = true
	}
	c.unit = unit
	c.req = Request{Method: method, Args: args, Envelope: Envelope{
		Source: inv.Envelope.Destination, Destination: dst,
		Origin: inv.Envelope.Origin, RID: inv.rt.ids.Add(1), Attempt: inv.rt.ids.Add(1),
		Margin: inv.Envelope.Margin,
	}}
	if o, ok := unit.(Observer); ok {
		o.Enqueued(c.req)
	}
	c.record(Record{Kind: Enqueued})
	if bounded {
		c.mu.Lock()
		c.expiry = time.AfterFunc(inv.Remaining()+inv.Envelope.Margin, c.expire)
		c.mu.Unlock()
	}
	l, found := inv.rt.lanes.Load(inv.Envelope.Destination)
	if !found {
		l, _ = inv.rt.lanes.LoadOrStore(inv.Envelope.Destination, &lane{})
	}
	c.lane = l.(*lane)
	if !mutates && !ahead {
		inv.mu.Lock()
		if inv.placed >= inv.rt.limit() {
			inv.held = append(inv.held, c)
			inv.mu.Unlock()
			return c
		}
		inv.placed++
		c.placed = true
		inv.mu.Unlock()
	}
	c.enter()
	return c
}

// enter puts an invocation at the end of the queue of its unit: it is sent at once when the unit has a place under its limit and otherwise when an earlier invocation of the unit settles.
func (c *Call) enter() {
	c.lane.mu.Lock()
	if c.lane.active >= c.inv.rt.limit() {
		c.lane.waiting = append(c.lane.waiting, c)
		c.lane.mu.Unlock()
		return
	}
	c.lane.active++
	c.lane.mu.Unlock()
	if !c.state.CompareAndSwap(callWaiting, callSent) {
		c.release()
		return
	}
	c.send()
}

// record writes a telemetry record of the invocation to the sink of its destination, filling the fields that identify the invocation.
func (c *Call) record(r Record) {
	c.mu.Lock()
	r.Envelope, r.Parent, r.Mutates, r.Method = c.req.Envelope, c.inv.Envelope.RID, c.mutates, c.req.Method
	c.mu.Unlock()
	c.unit.Telemetry().Record(r)
}

// end settles the invocation locally with its response: it records the settlement with the error the caller receives, notes that a completed mutation may have altered the caller's state, frees the place of a read under the window of its invocation, and delivers the response to the demand point.
func (c *Call) end(r Response) {
	c.mu.Lock()
	if c.expiry != nil {
		c.expiry.Stop()
	}
	c.mu.Unlock()
	if c.mutates && r.Done && !r.AppResponse.Rejected && r.AppResponse.Err == nil {
		c.inv.altered.Store(true)
	}
	var outcome string
	if c.inv.rt.Outcome != nil {
		outcome = c.inv.rt.Outcome(r, c.mutates)
	}
	c.record(Record{Kind: InvocationSettled, Outcome: outcome})
	c.inv.vacate(c)
	c.reply <- r
	if c.watch != nil {
		c.watch(r)
	}
}

// expire settles the invocation at the caller deadline from the evidence of the caller: an invocation that was not dispatched is not sent, and a dispatched one receives the cancel signal.
func (c *Call) expire() {
	switch {
	case c.state.CompareAndSwap(callWaiting, callEnded):
		c.end(Expired())
	case c.state.CompareAndSwap(callSent, callEnded):
		c.release()
		c.end(Expired())
	case c.state.CompareAndSwap(callDispatched, callEnded):
		c.cancel()
		if c.mutates {
			c.inv.unclean.Store(true)
		}
		c.end(Response{})
	}
}

// send sends an invocation that holds a place under the limit with the allowance that remains, or settles it unsent when no allowance remains.
func (c *Call) send() {
	budget := c.inv.Remaining()
	c.mu.Lock()
	c.req.Envelope.Budget = budget
	c.mu.Unlock()
	if budget <= 0 {
		if c.state.CompareAndSwap(callSent, callEnded) {
			c.release()
			c.end(Expired())
		}
		return
	}
	if !c.state.CompareAndSwap(callSent, callDispatched) {
		return
	}
	c.record(Record{Kind: Dispatched})
	go func() {
		r := c.unit.Invoke(c.req)
		if c.state.CompareAndSwap(callDispatched, callEnded) {
			c.release()
			c.end(r)
		}
	}()
}

// release gives the place of a settled invocation to the first waiting invocation of its unit that has not been abandoned.
func (c *Call) release() {
	l := c.lane
	l.mu.Lock()
	for len(l.waiting) > 0 {
		next := l.waiting[0]
		l.waiting = l.waiting[1:]
		if next.state.CompareAndSwap(callWaiting, callSent) {
			l.mu.Unlock()
			next.send()
			return
		}
	}
	l.active--
	l.mu.Unlock()
}

// Abandon settles an invocation whose outcome the caller no longer needs: an invocation that was not dispatched is not sent, and the destination of a dispatched one without a response receives the cancel signal.
func (c *Call) Abandon() {
	switch {
	case c.state.CompareAndSwap(callWaiting, callEnded):
		c.end(Expired())
	case c.state.CompareAndSwap(callSent, callEnded):
		c.release()
		c.end(Expired())
	case c.state.CompareAndSwap(callDispatched, callEnded):
		c.cancel()
		if c.mutates {
			c.inv.unclean.Store(true)
		}
		c.end(Response{})
	}
}

// cancel sends the cancel signal of a sent invocation to its destination and gives up its place.
func (c *Call) cancel() {
	c.record(Record{Kind: CancelSent})
	c.mu.Lock()
	envelope := c.req.Envelope
	c.mu.Unlock()
	c.unit.Cancel(envelope)
	c.release()
}

// Settle is the demand point of an invocation, at which the caller waits for its local settlement, which the caller deadline bounds, unless one of the stops closes first, in which case the caller abandons the invocation and the second result is false.
func (c *Call) Settle(stops ...<-chan struct{}) (Response, bool) {
	var first, second <-chan struct{}
	if len(stops) > 0 {
		first = stops[0]
	}
	if len(stops) > 1 {
		second = stops[1]
	}
	select {
	case r := <-c.reply:
		return c.inv.Obtain(Settled(r)), true
	case <-first:
	case <-second:
	}
	c.Abandon()
	return <-c.reply, false
}

// Await is the demand point of an invocation as Settle is, except that a receipt from wake returns before the settlement, with the second result false, and leaves the invocation in flight.
func (c *Call) Await(wake, first, second <-chan struct{}) (Response, bool) {
	select {
	case r := <-c.reply:
		return c.inv.Obtain(Settled(r)), true
	case <-wake:
		return Response{}, false
	case <-first:
	case <-second:
	}
	c.Abandon()
	return <-c.reply, true
}

// Dispatch starts one invocation as Start does, without a caller deadline, and returns the delivery of its response.
func (inv *Invocation) Dispatch(dst Locator, method string, mutates bool, args []any) Pending {
	return inv.start(dst, method, mutates, args, false, false, nil).reply
}

// Obtain is the demand point of a dispatched invocation without a caller deadline.
func (inv *Invocation) Obtain(p Pending) Response {
	r := <-p
	if !r.Clean {
		inv.unclean.Store(true)
	}
	return r
}

const PanicEnv = "RITCHIE_PANIC"

// Contains reports whether the recover sites of the runtime recover a host fault, which the environment variable PanicEnv turns off with the value 1, so that a compiler developer receives the panic report of Go with the stack of the fault, where the runtime reads the answer once for its handlers and tasks and the entry reads it once at its start, and a deferred function that finds it false returns without a call of recover, because a recovered fault that is raised again loses the stack of the fault.
func Contains() bool { return os.Getenv(PanicEnv) != "1" }

// Contains reports whether the runtime of the invocation contains host faults.
func (inv *Invocation) Contains() bool { return inv.rt.contains }

// contained wraps a handler so that a recoverable host fault ends only its own invocation.
func (rt *Runtime) contained(h Handler) func(Request) Response {
	return func(req Request) (resp Response) {
		inv := &Invocation{Envelope: req.Envelope, rt: rt, deadline: time.Now().Add(req.Envelope.Budget), stop: req.Stop}
		defer func() {
			if !rt.contains {
				return
			}
			fault := recover()
			if fault == nil {
				return
			}
			if _, halted := fault.(Halt); halted {
				resp = Response{Clean: !inv.unclean.Load()}
				return
			}
			clean := !inv.unclean.Load()
			Defect(rt.defects, fault)
			resp = Response{Done: clean, Clean: clean, AppResponse: AppResponse{Rejected: clean}}
		}()
		if h == nil {
			return Response{Done: true, Clean: true, AppResponse: AppResponse{Rejected: true}}
		}
		appResponse := h(inv, req)
		return Response{Done: true, Clean: !inv.unclean.Load(), AppResponse: appResponse}
	}
}

// Halt is the panic value with which the body of a cancelled invocation or task ends at a point where it would wait, which the recover sites of the runtime take as the end of the body and do not report.
type Halt struct{}

// Defect writes the report of an internal defect.
func Defect(w io.Writer, fault any) {
	var text string
	switch f := fault.(type) {
	case error:
		text = f.Error()
	case string:
		text = f
	default:
		text = "a host fault without a message"
	}
	io.WriteString(w, "ritchie runtime: internal defect: "+text+"\n"+string(debug.Stack()))
}
