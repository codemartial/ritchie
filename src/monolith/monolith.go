// Package monolith is the target that realizes every unit of an application in one
// process.
package monolith

import (
	"sync"
	"sync/atomic"
	"time"

	"ritchie.dev/rlr/mesh"
)

const DefaultCapacity = 256

// UnitConfig is the configuration of one unit.
type UnitConfig struct {
	Capacity int
	Sink     mesh.Sink
}

// Config is the configuration of a target.
type Config struct {
	Capacity int

	Reserve time.Duration
	Sink    mesh.Sink
	Units   map[mesh.Locator]UnitConfig
}

// New is the target under the production configuration.
func New(units []mesh.Hosted) mesh.Realization { return Start(Config{}, units) }

// Target hosts the units of one application.
type Target struct {
	units map[mesh.Locator]*unit
	sink  mesh.Sink
}

// Start constructs every unit.
func Start(cfg Config, units []mesh.Hosted) *Target {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultCapacity
	}
	if cfg.Sink == nil {
		cfg.Sink = discard{}
	}
	t := &Target{units: make(map[mesh.Locator]*unit, len(units)), sink: cfg.Sink}
	for _, h := range units {
		u := &unit{locator: h.Locator, serve: h.Serve, capacity: cfg.Capacity, reserve: cfg.Reserve, sink: cfg.Sink}
		if own := cfg.Units[h.Locator]; own.Capacity > 0 {
			u.capacity = own.Capacity
		}
		if own := cfg.Units[h.Locator]; own.Sink != nil {
			u.sink = own.Sink
		}
		u.queue = make(chan *job, u.capacity)
		u.jobs = map[uint64]*job{}
		t.units[h.Locator] = u
		u.sink.Record(mesh.Record{Kind: mesh.UnitConstructed, Envelope: mesh.Envelope{Destination: h.Locator}})
	}
	return t
}

// Locate resolves a Locator to its unit.
func (t *Target) Locate(l mesh.Locator) (mesh.Unit, bool) {
	u, found := t.units[l]
	return u, found
}

// Telemetry is the sink of records that belong to no unit.
func (t *Target) Telemetry() mesh.Sink { return t.sink }

// unit is one hosted unit.
type unit struct {
	locator  mesh.Locator
	serve    func(mesh.Request) mesh.Response
	capacity int
	reserve  time.Duration
	sink     mesh.Sink

	mu      sync.Mutex
	workers int
	queue   chan *job
	jobs    map[uint64]*job
}

const (
	jobQueued uint32 = iota
	jobStarted
	jobExpired
)

type job struct {
	req      mesh.Request
	reply    chan mesh.Response
	arrived  time.Time
	deadline time.Time
	state    atomic.Uint32
	stop     chan struct{}
}

func (u *unit) Telemetry() mesh.Sink { return u.sink }

// Invoke hands the request to the unit and returns at once.
func (u *unit) Invoke(req mesh.Request) mesh.Response {
	reply := make(chan mesh.Response, 1)
	usable := req.Envelope.Budget - u.reserve
	if usable <= 0 {
		return u.produced(req, mesh.Expired())
	}
	arrived := time.Now()
	j := &job{req: req, reply: reply, arrived: arrived, deadline: arrived.Add(usable), stop: make(chan struct{})}
	j.req.Stop = j.stop
	expiry := time.NewTimer(usable)
	defer expiry.Stop()
	select {
	case u.queue <- j:
		u.ensureWorker()
	default:
		return u.produced(req, mesh.Declined())
	}
	attempt := req.Envelope.Attempt
	u.mu.Lock()
	u.jobs[attempt] = j
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.jobs, attempt)
		u.mu.Unlock()
	}()
	select {
	case r := <-reply:
		return r
	case <-expiry.C:
		if j.state.CompareAndSwap(jobQueued, jobExpired) {
			return u.produced(req, mesh.Expired())
		}
	case <-j.stop:
		if j.state.CompareAndSwap(jobQueued, jobExpired) {
			return u.produced(req, mesh.Declined())
		}
	}
	return <-reply
}

// Cancel delivers the cancel signal of a caller to the invocation that the envelope identifies, which is not started when it waits for a slot and stops at its next demand point otherwise.
func (u *unit) Cancel(env mesh.Envelope) {
	u.mu.Lock()
	j := u.jobs[env.Attempt]
	u.mu.Unlock()
	if j != nil {
		u.sink.Record(mesh.Record{Kind: mesh.CancelReceived, Envelope: env})
		close(j.stop)
	}
}

func (u *unit) ensureWorker() {
	u.mu.Lock()
	start := u.workers < u.capacity
	if start {
		u.workers++
	}
	u.mu.Unlock()
	if start {
		go u.worker()
	}
}

func (u *unit) worker() {
	for {
		j := u.take()
		if j == nil {
			return
		}
		u.run(j)
	}
}

func (u *unit) take() *job {
	select {
	case j := <-u.queue:
		return j
	default:
		u.mu.Lock()
		select {
		case j := <-u.queue:
			u.mu.Unlock()
			return j
		default:
			u.workers--
			u.mu.Unlock()
			return nil
		}
	}
}

// run serves one request on the slot the caller holds.
func (u *unit) run(j *job) {
	now := time.Now()
	if !now.Before(j.deadline) {
		if j.state.CompareAndSwap(jobQueued, jobExpired) {
			u.respond(j, mesh.Expired())
		}
		return
	}
	if !j.state.CompareAndSwap(jobQueued, jobStarted) {
		return
	}
	j.req.Envelope.Budget -= now.Sub(j.arrived)
	u.started(j.req)
	resp := u.serve(j.req)
	u.sink.Record(mesh.Record{Kind: mesh.HandlerFinished, Envelope: j.req.Envelope})
	u.respond(j, resp)
}

// started records that a request holds a slot.
func (u *unit) started(req mesh.Request) {
	u.sink.Record(mesh.Record{Kind: mesh.HandlerStarted, Envelope: req.Envelope})
}

// respond records the one response of a request before the caller can act on it.
func (u *unit) respond(j *job, r mesh.Response) {
	u.sink.Record(mesh.Record{Kind: mesh.ResponseProduced, Envelope: j.req.Envelope, Done: r.Done, Clean: r.Clean})
	j.reply <- r
}

func (u *unit) produced(req mesh.Request, r mesh.Response) mesh.Response {
	u.sink.Record(mesh.Record{Kind: mesh.ResponseProduced, Envelope: req.Envelope, Done: r.Done, Clean: r.Clean})
	return r
}

// discard is the sink of a target without telemetry configuration.
type discard struct{}

func (discard) Record(mesh.Record) {}
