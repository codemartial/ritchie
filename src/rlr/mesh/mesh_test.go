package mesh

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// inlineTarget represents a target of one unit that serves each request on the calling goroutine.
type inlineTarget struct {
	unit    Hosted
	mu      sync.Mutex
	records []Record
}

func (t *inlineTarget) Locate(l Locator) (Unit, bool) { return t, l == t.unit.Locator }
func (t *inlineTarget) Telemetry() Sink               { return t }
func (t *inlineTarget) Record(r Record) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, r)
}
func (t *inlineTarget) Invoke(req Request) Response { return t.unit.Serve(req) }
func (t *inlineTarget) Cancel(Envelope)             {}

// inlineRuntime returns a runtime whose one unit the inline target serves, with defect reports collected in out.
func inlineRuntime(h Handler, out *bytes.Buffer) (*Runtime, *inlineTarget, Locator) {
	l := Locator{Publication: "p", Module: "m", Unit: "@Unit"}
	t := &inlineTarget{}
	rt := New([]UnitSpec{{Locator: l, Handler: h}}, func(units []Hosted) Realization {
		t.unit = units[0]
		return t
	}, out)
	return rt, t, l
}

func TestDispatchMintsIdentitiesAndRecordsTheRelation(t *testing.T) {
	var seen []Envelope
	rt, target, l := inlineRuntime(func(inv *Invocation, req Request) AppResponse {
		seen = append(seen, req.Envelope)
		return AppResponse{}
	}, nil)
	client := rt.Client(time.Second)
	for range 2 {
		<-client.Dispatch(l, "run", false, nil)
	}
	other := rt.Client(time.Second)
	<-other.Dispatch(l, "run", false, nil)
	ids := map[uint64]bool{client.Envelope.RID: true, other.Envelope.RID: true}
	for _, env := range seen {
		if ids[env.RID] || ids[env.Attempt] || env.RID == env.Attempt {
			t.Errorf("envelope %+v reuses an identity", env)
		}
		ids[env.RID], ids[env.Attempt] = true, true
		if env.Destination != l || env.Source != (Locator{}) || env.Budget <= 0 || env.Budget > time.Second {
			t.Errorf("envelope %+v, want destination %+v, no source, and the sent budget", env, l)
		}
	}
	if seen[0].Origin != client.Envelope.Origin || seen[1].Origin != client.Envelope.Origin || seen[2].Origin != other.Envelope.Origin {
		t.Errorf("origins %d %d %d do not follow their clients", seen[0].Origin, seen[1].Origin, seen[2].Origin)
	}
	dispatched := target.records[:0]
	for _, rec := range target.records {
		if rec.Kind == Dispatched {
			dispatched = append(dispatched, rec)
		}
	}
	for i, rec := range dispatched {
		want := Record{Kind: Dispatched, Envelope: seen[i], Parent: client.Envelope.RID, Method: "run"}
		if i == 2 {
			want.Parent = other.Envelope.RID
		}
		if rec != want {
			t.Errorf("record %d = %+v, want %+v", i, rec, want)
		}
	}
}

func TestDispatchToAnUnrealizedUnitIsDeclined(t *testing.T) {
	rt, _, _ := inlineRuntime(func(*Invocation, Request) AppResponse {
		t.Error("a handler ran")
		return AppResponse{}
	}, nil)
	if r := <-rt.Client(time.Second).Dispatch(Locator{Unit: "@Absent"}, "run", false, nil); r.Done || !r.Clean {
		t.Errorf("response = %+v, want not done and clean", r)
	}
}

func TestDispatchReturnsWhileTheTargetRuns(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	rt, _, l := inlineRuntime(func(*Invocation, Request) AppResponse {
		close(entered)
		<-release
		return AppResponse{}
	}, nil)
	pending := rt.Client(time.Second).Dispatch(l, "run", false, nil)
	<-entered
	select {
	case r := <-pending:
		t.Fatalf("response before release = %+v", r)
	default:
	}
	close(release)
	if r := <-pending; !r.Done || !r.Clean {
		t.Errorf("response = %+v, want done and clean", r)
	}
}

func TestContainmentRejectsAFaultOnce(t *testing.T) {
	out := &bytes.Buffer{}
	rt, _, l := inlineRuntime(func(*Invocation, Request) AppResponse { panic("handler fault") }, out)
	r := <-rt.Client(time.Second).Dispatch(l, "run", false, nil)
	if !r.Done || !r.Clean || !r.AppResponse.Rejected {
		t.Errorf("response = %+v, want done, clean, rejected", r)
	}
	if got := strings.Count(out.String(), "ritchie runtime: internal defect: handler fault\n"); got != 1 {
		t.Errorf("%d defect reports, want 1:\n%s", got, out)
	}
	out.Reset()
	rt, _, l = inlineRuntime(nil, out)
	if r := <-rt.Client(time.Second).Dispatch(l, "run", false, nil); !r.Done || !r.Clean || !r.AppResponse.Rejected || out.Len() != 0 {
		t.Errorf("a unit without a handler responded %+v with report %q", r, out)
	}
}

func TestCleanNeedsCleanResponsesFromDirectCalls(t *testing.T) {
	for _, faults := range []bool{false, true} {
		rt, _, l := inlineRuntime(func(inv *Invocation, req Request) AppResponse {
			inv.Obtain(Settled(Response{Done: true}))
			if faults {
				panic("fault after an unclean call")
			}
			return AppResponse{}
		}, &bytes.Buffer{})
		r := <-rt.Client(time.Second).Dispatch(l, "run", false, nil)
		if r.Done != !faults || r.Clean || r.AppResponse.Rejected || r.AppResponse.Err != nil || r.AppResponse.Results != nil {
			t.Errorf("faults=%v: response = %+v, want done=%v, not clean, and an empty application response", faults, r, !faults)
		}
	}
}

func TestAStartAfterTheCallerDeadlineSettlesOnceAndWithoutAWait(t *testing.T) {
	rt, _, l := inlineRuntime(func(*Invocation, Request) AppResponse { return AppResponse{} }, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 2000 {
			c := rt.Client(0).Start(l, "run", false, nil)
			if r, settled := c.Settle(); !settled || !r.Expired || r.Done {
				t.Errorf("response = %+v, settled %t, want an expired response that settled", r, settled)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a start after the caller deadline did not settle")
	}
}

func TestHostFaultsAreContainedUnlessThePanicVariableIsOne(t *testing.T) {
	for value, want := range map[string]bool{"": true, "0": true, "true": true, "11": true, "1": false} {
		t.Setenv(PanicEnv, value)
		if got := Contains(); got != want {
			t.Errorf("Contains() under %s=%q = %t, want %t", PanicEnv, value, got, want)
		}
	}
}

func TestAnInvocationHoldsItsReadsBeyondTheWindowSoThatAReadOfAnotherInvocationWaitsBehindAtMostTheWindow(t *testing.T) {
	started := make(chan string, 8)
	release := make(chan struct{})
	rt, _, l := inlineRuntime(func(_ *Invocation, req Request) AppResponse {
		started <- req.Method
		<-release
		return AppResponse{}
	}, nil)
	rt.Limit = 2
	first, second := rt.Client(time.Minute), rt.Client(time.Minute)
	var calls []*Call
	for _, method := range []string{"a1", "a2", "a3", "a4"} {
		calls = append(calls, first.Start(l, method, false, nil))
	}
	calls = append(calls, second.Start(l, "b1", false, nil))
	if ahead := first.StartAhead(l, "a5", nil, nil); ahead != nil {
		t.Errorf("a read ahead under a full window was dispatched")
	}
	if first.Free() || !second.Free() {
		t.Errorf("free places = %t and %t, want none for the first invocation and one for the second", first.Free(), second.Free())
	}
	next := func() string {
		select {
		case method := <-started:
			return method
		case <-time.After(10 * time.Second):
			t.Fatal("no further handler started")
			return ""
		}
	}
	if x, y := next(), next(); x == y || (x != "a1" && x != "a2") || (y != "a1" && y != "a2") {
		t.Errorf("the handlers of %s and %s started first, want those of the two reads under the window of the first invocation", x, y)
	}
	release <- struct{}{}
	if third := next(); third != "b1" {
		t.Errorf("the handler of %s started third, want that of the read of the second invocation, which waits behind no read that the first invocation holds", third)
	}
	for range len(calls) - 1 {
		release <- struct{}{}
	}
	for _, c := range calls {
		if r, settled := c.Settle(); !settled || !r.Done {
			t.Errorf("response = %+v, settled %t, want a completed response", r, settled)
		}
	}
}
