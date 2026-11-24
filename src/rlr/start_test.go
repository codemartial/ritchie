package rlr

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"ritchie.dev/rlr/mesh"
)

// echoRealization serves each request on the calling goroutine and keeps the telemetry.
type echoRealization struct {
	units   map[mesh.Locator]mesh.Hosted
	records []mesh.Record
}

func (r *echoRealization) Locate(l mesh.Locator) (mesh.Unit, bool) {
	u, found := r.units[l]
	return &echoUnit{r, u}, found
}
func (r *echoRealization) Telemetry() mesh.Sink           { return r }
func (r *echoRealization) Record(rec mesh.Record)         { r.records = append(r.records, rec) }
func (u *echoUnit) Invoke(req mesh.Request) mesh.Response { return u.unit.Serve(req) }
func (u *echoUnit) Telemetry() mesh.Sink                  { return u.r }
func (u *echoUnit) Cancel(mesh.Envelope)                  {}

type echoUnit struct {
	r    *echoRealization
	unit mesh.Hosted
}

// installEcho installs an echo realization for one Start and restores the previous target afterwards.
func installEcho(t *testing.T) *echoRealization {
	t.Helper()
	r := &echoRealization{units: map[mesh.Locator]mesh.Hosted{}}
	previous := host
	t.Cleanup(func() { host = previous })
	InstallTarget(func(units []mesh.Hosted) mesh.Realization {
		for _, u := range units {
			r.units[u.Locator] = u
		}
		return r
	})
	return r
}

var pricing = Locator{Module: "shop", Unit: "@Pricing"}

// pricingUnit is a unit whose quote method returns, raises, or faults by its argument.
func pricingUnit(mutates bool) UnitSpec {
	return UnitSpec{Locator: pricing, Module: "shop", Kind: "service", Methods: []Method{{
		Name: "quote", Mutates: mutates,
		Invoke: func(inv Invocation, args []any) ([]any, error) {
			qty := args[0].(int64)
			switch {
			case qty < 0:
				return nil, Raise(NewError(DivisionByZero.TypeID(), nil), 0, "quote")
			case qty > 100:
				panic("host fault in the handler")
			}
			return []any{qty * 2}, nil
		},
	}}}
}

func TestStartRunsTheEntryAsAClientOfTheUnits(t *testing.T) {
	out, exits := stubProcess(t)
	RegisterLocations([]LocationMeta{{Module: "shop", Function: "quote"}})
	r := installEcho(t)
	var seen []string
	note := func(label string, ok bool) {
		seen = append(seen, label+" "+map[bool]string{true: "ok", false: "wrong"}[ok])
	}
	Start(Application{Module: "shop", Units: []UnitSpec{pricingUnit(false)}, Main: func(inv Invocation) error {
		results, err := inv.Call(pricing, "quote", false, []any{int64(4)})
		note("typed", err == nil && results[0].(int64) == 8)
		_, err = inv.Call(pricing, "quote", false, []any{int64(-1)})
		note("domain", err != nil && TypeIDOf(err.(RitchieError)) == DivisionByZero.TypeID())
		_, err = inv.Call(pricing, "quote", false, []any{int64(500)})
		note("rejected", err != nil && TypeIDOf(err.(RitchieError)) == Rejected.TypeID())
		_, err = inv.Call(pricing, "absent", false, nil)
		note("unknown method", err != nil && TypeIDOf(err.(RitchieError)) == Rejected.TypeID())
		_, err = inv.Call(Locator{Unit: "@Nowhere"}, "quote", false, nil)
		note("unrealized", err != nil && TypeIDOf(err.(RitchieError)) == Unavailable.TypeID())
		return nil
	}})
	if got := strings.Join(seen, ", "); got != "typed ok, domain ok, rejected ok, unknown method ok, unrealized ok" {
		t.Errorf("observations: %s", got)
	}
	if got := *exits; len(got) != 1 || got[0] != 0 {
		t.Errorf("completion %v, want status 0", got)
	}
	if strings.Count(out.String(), "DivisionByZero!\n  at ") != 1 || strings.Count(out.String(), "internal defect: host fault in the handler") != 1 {
		t.Errorf("diagnostics lack the one service report and the one defect report:\n%s", out)
	}
	if len(r.records) == 0 || r.records[0].Kind != mesh.UnitConstructed && r.records[0].Kind != mesh.EntryStarted {
		t.Errorf("telemetry %+v", r.records)
	}
}

func TestStartWithoutMainOrServeRejectsTheProgram(t *testing.T) {
	out, exits := stubProcess(t)
	r := installEcho(t)
	Start(Application{Module: "shop", Units: []UnitSpec{pricingUnit(false)}})
	if got := *exits; len(got) != 1 || got[0] != 2 {
		t.Errorf("completion %v, want 2", got)
	}
	if !strings.Contains(out.String(), "no main entry point") || !strings.Contains(out.String(), ServeOption) {
		t.Errorf("diagnostics %q, want the rejection naming %s", out, ServeOption)
	}
	if len(r.records) != 0 {
		t.Errorf("telemetry %v, want no unit constructed", r.records)
	}
}

func TestArriveRaisesASystemErrorInTheCallerOnce(t *testing.T) {
	RegisterLocations([]LocationMeta{{Module: "shop", Function: "main"}, {Module: "lib", Function: "quote"}})
	e := Arrive(NewError(Unavailable.TypeID(), nil), "shop", 0, "main()")
	e = Leave(e, 0)
	if len(History(e)) != 0 || e.location != 0 || !e.raised || e.raiser != "main()" {
		t.Errorf("system error leaving the caller = %+v, want raised at the caller with empty history", e)
	}
	if e = Bubble(e, 0); len(History(e)) != 1 {
		t.Errorf("a later propagation step was not recorded: %+v", e)
	}
	transported := Raise(NewError(DivisionByZero.TypeID(), nil), 1, "quote()")
	transported = Leave(Arrive(transported, "shop", 0, "main()"), 0)
	if len(History(transported)) != 1 || History(transported)[0].Location().Function != "quote" || transported.location != 0 {
		t.Errorf("transported error = %+v, want the callee step kept and the caller as the current location", transported)
	}
}

const faultingEntryEnv = "RITCHIE_TEST_FAULTING_ENTRY"

func TestFaultingEntry(t *testing.T) {
	if os.Getenv(faultingEntryEnv) != "1" {
		t.Skip("runs only as the subprocess of the containment test")
	}
	Run("app", func() error { panic("host fault in the entry") })
}

func TestAFaultingEntryEndsWithADefectReportOrWithThePanicOfGoUnderThePanicVariable(t *testing.T) {
	run := func(extra ...string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFaultingEntry$")
		cmd.Env = append(os.Environ(), append(extra, faultingEntryEnv+"=1")...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("subprocess ended with %v, want an exit status", err)
		}
		return stderr.String(), exit.ExitCode()
	}
	report, status := run(PanicEnv + "=")
	if status != 2 || !strings.Contains(report, "ritchie runtime: internal defect: host fault in the entry") || strings.Contains(report, "panic: ") {
		t.Errorf("contained fault = status %d, standard error %q, want status 2 and the defect report without a panic line", status, report)
	}
	report, status = run(PanicEnv + "=1")
	if status != 2 || !strings.HasPrefix(report, "panic: host fault in the entry") || !strings.Contains(report, "goroutine ") || strings.Contains(report, "internal defect") {
		t.Errorf("fault under %s=1 = status %d, standard error %q, want status 2 and the panic report of Go with its goroutine trace", PanicEnv, status, report)
	}
}
