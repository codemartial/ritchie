package rlr

import (
	"bytes"
	"io"
	"maps"
	"strings"
	"testing"
	"time"

	"ritchie.dev/rlr/mesh"
)

const patience = 10 * time.Second

// clientScope returns the scope of a caller outside every unit, over a runtime without units.
func clientScope() *Scope {
	rt := mesh.New(nil, func([]mesh.Hosted) mesh.Realization {
		return &echoRealization{units: map[mesh.Locator]mesh.Hosted{}}
	}, io.Discard)
	return &Scope{inv: rt.Client(time.Minute)}
}

// awaitClosed fails the test when a channel is not closed within the patience of the tests.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(patience):
		t.Fatalf("%s did not happen within %v", what, patience)
	}
}

func TestTheBodiesOfTwoTasksRunBeforeTheRoutineWaitsForEither(t *testing.T) {
	g := Open(clientScope())
	defer g.Cancel()
	first, second := make(chan struct{}), make(chan struct{})
	a := g.Run(func(Invocation) ([]any, error) {
		close(first)
		<-second
		return []any{int64(1)}, nil
	})
	b := g.Run(func(Invocation) ([]any, error) {
		close(second)
		<-first
		return []any{int64(2)}, nil
	})
	x, y := Pending[int64](a, 0), Pending[int64](b, 0)
	if err := y.Wait(nil); err != nil {
		t.Fatalf("wait for the second task = %v, want no failure", err)
	}
	if err := x.Wait(nil); err != nil {
		t.Fatalf("wait for the first task = %v, want no failure", err)
	}
	if x.V != 1 || y.V != 2 {
		t.Errorf("results = %d and %d, want 1 and 2", x.V, y.V)
	}
}

func TestAHandlerSuppliesTheResultsOfATaskWhoseBodyFails(t *testing.T) {
	g := Open(clientScope())
	defer g.Cancel()
	task := g.Run(func(Invocation) ([]any, error) {
		return nil, NewError(OutOfRange.TypeID(), nil)
	})
	task.Handle(func(error) ([]any, error) { return []any{int64(7)}, nil })
	f := Pending[int64](task, 0)
	if err := f.Wait(nil); err != nil || f.V != 7 {
		t.Errorf("wait = %d, %v, want 7 and no failure", f.V, err)
	}
}

func TestADictionaryOfFuturesHoldsAnEntryForEachValueAndWaitsForTheValuesInFlight(t *testing.T) {
	g := Open(clientScope())
	defer g.Cancel()
	task := g.Run(func(Invocation) ([]any, error) { return []any{int64(2)}, nil })
	var d FutureMap[string, int64, int64]
	d.Set("known", Ready(int64(1)), Same[int64])
	d.Set("pending", Pending[int64](task, 0), Same[int64])
	if d.Len() != 2 {
		t.Fatalf("count before the wait = %d, want 2", d.Len())
	}
	moved := d.At("pending", d.V["pending"], nil)
	second := d.Copy(maps.Clone[map[string]int64])
	if err := d.Wait(nil); err != nil || d.V["known"] != 1 || d.V["pending"] != 2 {
		t.Fatalf("wait = %v, %v, want the values 1 and 2 and no failure", d.V, err)
	}
	if err := moved.Wait(nil); err != nil || moved.V != 2 {
		t.Errorf("wait for the moved value = %d, %v, want 2 and no failure", moved.V, err)
	}
	if err := second.Wait(nil); err != nil || second.V["pending"] != 2 {
		t.Errorf("wait for the copy = %v, %v, want the value 2 and no failure", second.V, err)
	}
	failing := g.Run(func(Invocation) ([]any, error) { return nil, NewError(OutOfRange.TypeID(), nil) })
	d.Set("failed", Pending[int64](failing, 0), Same[int64])
	if err := d.Wait(nil); err == nil || TypeIDOf(err.(RitchieError)) != OutOfRange.TypeID() {
		t.Errorf("wait for a dictionary whose value fails = %v, want OutOfRange!", err)
	}
}

func TestCancellingAGroupEndsTheBodyOfATaskAndTheBodiesItStarted(t *testing.T) {
	g := Open(clientScope())
	started, outer, inner := make(chan struct{}), make(chan struct{}), make(chan struct{})
	g.Run(func(inv Invocation) ([]any, error) {
		defer close(outer)
		own := Open(inv)
		defer own.Cancel()
		nested := own.Run(func(inv Invocation) ([]any, error) {
			defer close(inner)
			close(started)
			<-inv.cancelled()
			return nil, nil
		})
		return nil, nested.Join()
	})
	awaitClosed(t, started, "the start of the nested body")
	g.Cancel()
	awaitClosed(t, inner, "the end of the nested body after the cancellation")
	awaitClosed(t, outer, "the end of the body after the cancellation")
	if err := g.Join(); err != nil {
		t.Errorf("join after the cancellation = %v, want no task left", err)
	}
}

func TestABodyThatWaitsForATaskUnderACancelledScopeReceivesCancelled(t *testing.T) {
	g := Open(clientScope())
	got := make(chan error, 1)
	release := make(chan struct{})
	defer close(release)
	g.Run(func(inv Invocation) ([]any, error) {
		own := Open(inv)
		defer own.Cancel()
		err := own.Run(func(Invocation) ([]any, error) {
			<-release
			return nil, nil
		}).Join()
		got <- err
		return nil, err
	})
	g.Cancel()
	select {
	case err := <-got:
		if err == nil || TypeIDOf(err.(RitchieError)) != Cancelled.TypeID() {
			t.Errorf("join under a cancelled scope = %v, want Cancelled!", err)
		}
	case <-time.After(patience):
		t.Fatalf("the join under a cancelled scope did not return within %v", patience)
	}
}

func TestABodyWaitedForAtOnceRunsInPlaceOnlyInAGroupWithoutAnotherTask(t *testing.T) {
	g := Open(clientScope())
	defer g.Cancel()
	ran := false
	alone := g.RunAtOnce(func(Invocation) ([]any, error) {
		ran = true
		return []any{int64(4)}, nil
	})
	if !ran {
		t.Errorf("the body of a group without another task had not run when the start returned")
	}
	if got, err := alone.Outcome(); err != nil || got[0] != int64(4) {
		t.Errorf("outcome of the body that ran in place = %v, %v, want 4", got, err)
	}
	release := make(chan struct{})
	earlier := g.Run(func(Invocation) ([]any, error) {
		<-release
		return []any{int64(1)}, nil
	})
	started := make(chan struct{})
	later := g.RunAtOnce(func(Invocation) ([]any, error) {
		close(started)
		<-release
		return []any{int64(2)}, nil
	})
	awaitClosed(t, started, "the start of the body beside an earlier task")
	close(release)
	if err := later.Join(); err != nil {
		t.Errorf("join of the two tasks = %v, want none", err)
	}
	if got, err := earlier.Outcome(); err != nil || got[0] != int64(1) {
		t.Errorf("outcome of the earlier task after the join = %v, %v, want 1", got, err)
	}
}

func TestASleepInTheBodyOfACancelledTaskEndsBeforeItsDuration(t *testing.T) {
	g := Open(clientScope())
	started, ended := make(chan struct{}), make(chan struct{})
	g.Run(func(inv Invocation) ([]any, error) {
		defer close(ended)
		close(started)
		Sleep(fineDuration(int64(time.Hour)), inv)
		return nil, nil
	})
	awaitClosed(t, started, "the start of the body")
	g.Cancel()
	awaitClosed(t, ended, "the end of the sleeping body after the cancellation")
}

func TestAHostFaultOfABodyIsTheRejectedFailureOfItsTask(t *testing.T) {
	var report bytes.Buffer
	previous := Diag
	Diag = &report
	t.Cleanup(func() { Diag = previous })
	g := Open(clientScope())
	defer g.Cancel()
	faulty := g.Run(func(Invocation) ([]any, error) { panic("host fault in the body") })
	sound := g.Run(func(Invocation) ([]any, error) { return []any{int64(4)}, nil })
	err := faulty.Join()
	if err == nil || TypeIDOf(err.(RitchieError)) != Rejected.TypeID() {
		t.Errorf("join of a body that faults = %v, want Rejected!", err)
	}
	if !strings.Contains(report.String(), "internal defect: host fault in the body") {
		t.Errorf("defect report = %q, want the report of the host fault", report.String())
	}
	f := Pending[int64](sound, 0)
	if err := f.Wait(nil); err != nil || f.V != 4 {
		t.Errorf("wait for a later task after the fault = %d, %v, want 4 and no failure", f.V, err)
	}
}
