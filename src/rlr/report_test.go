package rlr

import (
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

// rejectingSink represents a diagnostic destination whose every write fails after taking a prefix of the text.
type rejectingSink struct {
	accept   int
	attempts int
	taken    strings.Builder
}

func (s *rejectingSink) Write(p []byte) (int, error) {
	s.attempts++
	n := min(s.accept, len(p))
	s.taken.Write(p[:n])
	return n, errors.New("diagnostic destination rejected the write")
}

// installRejectingSink routes reports into a rejecting destination.
func installRejectingSink(t *testing.T, accept int) (*rejectingSink, *[]int) {
	t.Helper()
	sink := &rejectingSink{accept: accept}
	return sink, stubSink(t, sink)
}

func TestLogToARejectingSinkChangesNothing(t *testing.T) {
	sink, exits := installRejectingSink(t, 0)
	Log("", int64(1))
	Log("", "two")
	if sink.attempts != 2 || len(*exits) != 0 {
		t.Errorf("attempts = %d exits = %v, want one attempt per log and no process completion", sink.attempts, *exits)
	}
}

func TestTerminalReportToARejectingSinkStillExitsOne(t *testing.T) {
	installMetadata(t)
	sink, exits := installRejectingSink(t, 0)
	Run("app", func() error { return Bubble(Raise(NewError(FirstDeclared+1, nil), 0, "deep()"), 3) })
	if sink.attempts != 1 || len(*exits) != 1 || (*exits)[0] != 1 {
		t.Errorf("attempts = %d exits = %v, want one reporting attempt and status 1", sink.attempts, *exits)
	}
}

func TestPartialReportWriteIsNotRetried(t *testing.T) {
	installMetadata(t)
	sink, exits := installRejectingSink(t, 6)
	Run("app", func() error { return Raise(NewError(FirstDeclared+1, nil), 3, "main()") })
	if sink.attempts != 1 || sink.taken.String() != "Quiet!" || len(*exits) != 1 || (*exits)[0] != 1 {
		t.Errorf("attempts = %d taken %q exits = %v, want one attempt with the accepted prefix and status 1", sink.attempts, sink.taken.String(), *exits)
	}
}

func TestSecondaryReportToARejectingSinkLeavesTheErrorAndDispositionAlone(t *testing.T) {
	installMetadata(t)
	sink, exits := installRejectingSink(t, 0)
	original := Bubble(Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "deep(c: 1)"), 1)
	before := RenderError(original, "app")
	ReportUnrecovered("app", Raise(NewError(FirstDeclared+1, nil), 2, "caller()"))
	if sink.attempts != 1 || len(*exits) != 0 {
		t.Errorf("attempts = %d exits = %v, want one attempt and no process completion", sink.attempts, *exits)
	}
	if got := RenderError(original, "app"); got != before {
		t.Errorf("original error after a failed secondary report =\n%s\nwant unchanged\n%s", got, before)
	}
}

func TestSnapshotOfARoutineWithoutParametersNamesTheRoutine(t *testing.T) {
	installMetadata(t)
	if got := Snapshot(0); got != "deep()" {
		t.Errorf("Snapshot(deep) = %q, want deep()", got)
	}
	if got := Snapshot(4); got != "Vault.open()" {
		t.Errorf("Snapshot(Vault.open) = %q, want Vault.open()", got)
	}
	e := Bubble(Raise(NewError(FirstDeclared+1, nil), 0, Snapshot(0)), 3)
	if got := RenderError(e, "app"); !strings.HasSuffix(got, "\n  raised by deep()") {
		t.Errorf("report lacks the raising routine:\n%s", got)
	}
}

func TestReceivedSnapshotTextIsNotReRendered(t *testing.T) {
	installMetadata(t)

	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "deep(e: Boom! { code: 1, note: \"n\" })")
	e = Bubble(Cross(e, "app"), 3)
	got := RenderError(e, "app")
	if !strings.HasSuffix(got, "\n  raised by deep(e: Boom! { code: 1, note: \"n\" })") || !strings.HasPrefix(got, "lib::Boom!") {
		t.Errorf("report =\n%s\nwant the current error under app and the snapshot text as rendered under lib", got)
	}
}

func TestRunIgnoresSIGPIPESoBrokenPipeWritesFailInBand(t *testing.T) {
	installMetadata(t)
	_, exits := stubProcess(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	defer w.Close()
	Diag, Out = w, w
	Run("app", func() error {
		Log("app", "before")
		ReportUnrecovered("app", Raise(NewError(FirstDeclared+1, nil), 2, "caller()"))
		return Raise(NewError(FirstDeclared+1, nil), 3, "main()")
	})
	if !signal.Ignored(syscall.SIGPIPE) {
		t.Error("the entry runner leaves SIGPIPE handling to the process default")
	}
	if len(*exits) != 1 || (*exits)[0] != 1 {
		t.Errorf("exits = %v, want the terminal status 1 after the broken-pipe writes", *exits)
	}
	if _, err := w.WriteString("probe\n"); !errors.Is(err, syscall.EPIPE) {
		t.Errorf("write to the reader-less pipe = %v, want EPIPE in-band", err)
	}
}
