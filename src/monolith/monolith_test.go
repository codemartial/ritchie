package monolith

import (
	"bytes"
	"runtime"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"ritchie.dev/rlr/mesh"
)

// locator returns the Locator of a test unit.
func locator(name string) mesh.Locator {
	return mesh.Locator{Publication: "test", Module: "test", Unit: name}
}

// host returns a runtime over a monolith hosting the handlers under cfg.
func host(cfg Config, handlers map[string]mesh.Handler) *mesh.Runtime {
	var specs []mesh.UnitSpec
	for name, h := range handlers {
		specs = append(specs, mesh.UnitSpec{Locator: locator(name), Handler: h})
	}
	return mesh.New(specs, func(units []mesh.Hosted) mesh.Realization { return Start(cfg, units) }, &defects)
}

var defects bytes.Buffer

// bytesArg returns the argument list holding one byte sequence.
func bytesArg(v []byte) []any { return []any{slices.Clone(v)} }

// bytesOf returns the byte sequence a response carries.
func bytesOf(r mesh.Response) []byte { return r.AppResponse.Results[0].([]byte) }

// echo returns a handler that returns the byte sequence it received.
func echo(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
	return mesh.AppResponse{Results: []any{req.Args[0].([]byte)}}
}

// captureDefects routes defect reports into a buffer.
func captureDefects(t *testing.T) *bytes.Buffer {
	t.Helper()
	defects.Reset()
	t.Cleanup(defects.Reset)
	return &defects
}

func TestUnitCapacityDefaultsTo256(t *testing.T) {
	target := Start(Config{Units: map[mesh.Locator]UnitConfig{locator("b"): {Capacity: 3}}},
		[]mesh.Hosted{{Locator: locator("a")}, {Locator: locator("b")}})
	if got := target.units[locator("a")].capacity; got != 256 {
		t.Errorf("default capacity = %d, want 256", got)
	}
	if got := target.units[locator("b")].capacity; got != 3 {
		t.Errorf("configured capacity = %d, want 3", got)
	}
	if got := cap(target.units[locator("a")].queue); got != 256 {
		t.Errorf("default queue capacity = %d, want 256", got)
	}
	if got := cap(target.units[locator("b")].queue); got != 3 {
		t.Errorf("configured queue capacity = %d, want 3", got)
	}
}

func TestIdleUnitHoldsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := host(Config{}, map[string]mesh.Handler{"echo": echo})
		client := rt.Client(time.Second)
		synctest.Wait()
		contexts := runtime.NumGoroutine()
		for range 100 {
			if r := <-client.Dispatch(locator("echo"), "run", false, bytesArg([]byte("x"))); !r.Done || !r.Clean {
				t.Fatalf("response = %+v", r)
			}
		}
		synctest.Wait()
		u, _ := rt.Realization().Locate(locator("echo"))
		unit := u.(*unit)
		unit.mu.Lock()
		workers, queued := unit.workers, len(unit.queue)
		unit.mu.Unlock()
		if workers != 0 || queued != 0 {
			t.Errorf("idle unit holds %d workers and %d queued requests", workers, queued)
		}
		if got := runtime.NumGoroutine(); got != contexts {
			t.Errorf("execution contexts went from %d to %d", contexts, got)
		}
	})
}

func TestWorkersStayWithinCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		entered := make(chan struct{}, 20)
		rt := host(Config{Capacity: 3}, map[string]mesh.Handler{"busy": func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
			entered <- struct{}{}
			<-gate
			return mesh.AppResponse{}
		}})
		client := rt.Client(time.Minute)
		var admitted []mesh.Pending
		for range 3 {
			admitted = append(admitted, client.Dispatch(locator("busy"), "run", false, nil))
			<-entered
		}
		for range 3 {
			admitted = append(admitted, client.Dispatch(locator("busy"), "run", false, nil))
		}
		synctest.Wait()
		select {
		case <-entered:
			t.Fatal("more than three handlers entered")
		default:
		}
		u, _ := rt.Realization().Locate(locator("busy"))
		unit := u.(*unit)
		unit.mu.Lock()
		workers, queued := unit.workers, len(unit.queue)
		unit.mu.Unlock()
		if workers != 3 || queued != 3 {
			t.Errorf("unit holds %d workers and %d queued requests, want 3 and 3", workers, queued)
		}
		contexts := runtime.NumGoroutine()
		startedAt := time.Now()
		var declined []mesh.Pending
		for range 14 {
			declined = append(declined, client.Dispatch(locator("busy"), "run", false, nil))
		}
		synctest.Wait()
		if elapsed := time.Since(startedAt); elapsed != 0 {
			t.Errorf("full-queue declines took %v, want no clock advance", elapsed)
		}
		for _, p := range declined {
			select {
			case r := <-p:
				if r.Done || !r.Clean {
					t.Errorf("full-queue response = %+v, want not done and clean", r)
				}
			default:
				t.Error("full queue left a request waiting")
			}
		}
		synctest.Wait()
		if got := runtime.NumGoroutine(); got != contexts {
			t.Errorf("declined requests changed execution contexts from %d to %d", contexts, got)
		}
		unit.mu.Lock()
		workers, queued = unit.workers, len(unit.queue)
		unit.mu.Unlock()
		if workers != 3 || queued != 3 {
			t.Errorf("declines left %d workers and %d queued requests, want 3 and 3", workers, queued)
		}
		close(gate)
		for _, p := range admitted {
			if r := <-p; !r.Done {
				t.Fatalf("response = %+v", r)
			}
		}
	})
}

func TestCallCycleOnOneSlotIsDeclinedByBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		handlers := map[string]mesh.Handler{}
		handlers["a"] = func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
			depth := req.Args[0].([]byte)
			if len(depth) >= 2 {
				return mesh.AppResponse{Results: bytesArg(depth)}
			}
			return inv.Obtain(inv.Dispatch(locator("b"), "run", false, bytesArg(append(depth, 'a')))).AppResponse
		}
		handlers["b"] = func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
			depth := req.Args[0].([]byte)
			r := inv.Obtain(inv.Dispatch(locator("a"), "run", false, bytesArg(append(depth, 'b'))))
			if !r.Done {
				return mesh.AppResponse{Results: bytesArg([]byte("inner declined"))}
			}
			return r.AppResponse
		}
		rt := host(Config{Capacity: 1}, handlers)
		client := rt.Client(time.Minute)
		p := client.Dispatch(locator("a"), "run", false, bytesArg(nil))
		time.Sleep(59 * time.Second)
		synctest.Wait()
		select {
		case r := <-p:
			t.Fatalf("the cycle completed with %+v before the inner budget ended", r)
		default:
		}
		time.Sleep(2 * time.Second)
		r := <-p
		if got := string(bytesOf(r)); !r.Done || got != "inner declined" {
			t.Errorf("response = %+v with %q, want the caller of the declined invocation to report it", r, got)
		}
	})
}

func TestCallCycleCompletesWithAFreeSlot(t *testing.T) {
	handlers := map[string]mesh.Handler{}
	handlers["a"] = func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
		depth := req.Args[0].([]byte)
		if len(depth) >= 2 {
			return mesh.AppResponse{Results: bytesArg(depth)}
		}
		return inv.Obtain(inv.Dispatch(locator("b"), "run", false, bytesArg(append(depth, 'a')))).AppResponse
	}
	handlers["b"] = func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
		depth := req.Args[0].([]byte)
		return inv.Obtain(inv.Dispatch(locator("a"), "run", false, bytesArg(append(depth, 'b')))).AppResponse
	}
	rt := host(Config{Capacity: 2}, handlers)
	client := rt.Client(time.Minute)
	r := <-client.Dispatch(locator("a"), "run", false, bytesArg(nil))
	if got := string(bytesOf(r)); !r.Done || !r.Clean || got != "ab" {
		t.Errorf("response = %+v with %q, want done, clean, ab", r, got)
	}
}

func TestCallerMutationDoesNotReachTheCallee(t *testing.T) {
	reading := make(chan struct{})
	done := make(chan struct{})
	handlers := map[string]mesh.Handler{"reader": func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
		got := req.Args[0].([]byte)
		close(reading)
		sum := 0
		for range 1000 {
			for _, b := range got {
				sum += int(b)
			}
		}
		<-done
		return mesh.AppResponse{Results: []any{[]byte{byte(sum / (1000 * len(got)))}}}
	}}
	rt := host(Config{}, handlers)
	client := rt.Client(time.Minute)
	value := bytes.Repeat([]byte{7}, 4096)
	p := client.Dispatch(locator("reader"), "run", false, bytesArg(value))
	<-reading
	for i := range value {
		value[i] = 9
	}
	close(done)
	if got := bytesOf(<-p); got[0] != 7 {
		t.Errorf("the callee read %d; the caller's later writes reached it", got[0])
	}
}

func TestExpiredRequestIsDeclinedAndLeavesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		started := make(chan struct{}, 100)
		rt := host(Config{Capacity: 1, Reserve: time.Millisecond}, map[string]mesh.Handler{"slow": func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
			started <- struct{}{}
			<-gate
			return mesh.AppResponse{}
		}})
		client := rt.Client(time.Minute)
		first := client.Dispatch(locator("slow"), "run", false, bytesArg(nil))
		<-started
		synctest.Wait()
		contexts := runtime.NumGoroutine()
		waiting := rt.Client(6*time.Millisecond).Dispatch(locator("slow"), "run", false, bytesArg(nil))
		synctest.Wait()
		u, _ := rt.Realization().Locate(locator("slow"))
		unit := u.(*unit)
		unit.mu.Lock()
		workers, queued := unit.workers, len(unit.queue)
		unit.mu.Unlock()
		if workers != 1 || queued != 1 {
			t.Errorf("unit holds %d workers and %d queued requests, want 1 and 1", workers, queued)
		}
		if r := <-rt.Client(time.Millisecond).Dispatch(locator("slow"), "run", false, bytesArg(nil)); r.Done || !r.Clean {
			t.Errorf("an arrival whose budget the reserve uses up got %+v, want not done and clean", r)
		}
		time.Sleep(4 * time.Millisecond)
		synctest.Wait()
		select {
		case r := <-waiting:
			t.Errorf("a waiting request got %+v before its budget ended", r)
		default:
		}
		time.Sleep(time.Millisecond)
		if r := <-waiting; r.Done || !r.Clean {
			t.Fatalf("response = %+v, want not done and clean", r)
		}
		synctest.Wait()
		if got := runtime.NumGoroutine(); got != contexts {
			t.Errorf("declined requests left execution contexts: %d, was %d", got, contexts)
		}
		unit.mu.Lock()
		workers, queued = unit.workers, len(unit.queue)
		unit.mu.Unlock()
		if workers != 1 || queued != 1 {
			t.Errorf("after expiry unit holds %d workers and %d queued requests, want 1 and 1", workers, queued)
		}
		close(gate)
		if r := <-first; !r.Done {
			t.Errorf("the running invocation got %+v", r)
		}
		synctest.Wait()
		unit.mu.Lock()
		workers, queued = unit.workers, len(unit.queue)
		unit.mu.Unlock()
		if workers != 0 || queued != 0 {
			t.Errorf("idle unit holds %d workers and %d queued requests", workers, queued)
		}
		select {
		case <-started:
			t.Error("a declined request started its handler")
		default:
		}
	})
}

// gatedSink holds each response-produced record until the test releases it.
type gatedSink struct {
	recording chan struct{}
	release   chan struct{}
}

func (s gatedSink) Record(rec mesh.Record) {
	if rec.Kind == mesh.ResponseProduced {
		s.recording <- struct{}{}
		<-s.release
	}
}

func TestResponseIsRecordedBeforeTheCallerReceivesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := gatedSink{recording: make(chan struct{}), release: make(chan struct{})}
		rt := host(Config{Sink: sink}, map[string]mesh.Handler{"echo": echo})
		p := rt.Client(time.Minute).Dispatch(locator("echo"), "run", false, bytesArg([]byte("x")))
		<-sink.recording
		synctest.Wait()
		var r mesh.Response
		select {
		case r = <-p:
			t.Error("the caller received its response while the response-produced record was still being written")
		default:
		}
		close(sink.release)
		if !r.Done {
			r = <-p
		}
		if !r.Done || !r.Clean {
			t.Errorf("response = %+v, want done and clean", r)
		}
	})
}

func TestFaultedWorkerKeepsServing(t *testing.T) {
	defects := captureDefects(t)
	calls := 0
	entered := make(chan struct{})
	rt := host(Config{Capacity: 1}, map[string]mesh.Handler{"flaky": func(inv *mesh.Invocation, req mesh.Request) mesh.AppResponse {
		calls++
		if calls == 1 {
			close(entered)
			panic("first call faults")
		}
		return echo(inv, req)
	}})
	client := rt.Client(time.Minute)
	first := client.Dispatch(locator("flaky"), "run", false, bytesArg(nil))
	<-entered
	second := client.Dispatch(locator("flaky"), "run", false, bytesArg([]byte("ok")))
	if r := <-first; !r.Done || !r.Clean || !r.AppResponse.Rejected {
		t.Errorf("faulted invocation got %+v, want done, clean, rejected", r)
	}
	if r := <-second; !r.Done || r.AppResponse.Rejected || string(bytesOf(r)) != "ok" {
		t.Errorf("the invocation after the fault got %+v", r)
	}
	if got := bytes.Count(defects.Bytes(), []byte("internal defect: first call faults")); got != 1 {
		t.Errorf("%d defect reports, want 1:\n%s", got, defects)
	}
}
