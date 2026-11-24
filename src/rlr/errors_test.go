package rlr

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// payload represents a stand-in generated payload record.
type payload struct {
	code int64
	note string
}

// installMetadata registers metadata for error tests.
func installMetadata(t *testing.T) {
	t.Helper()
	prevTypes, prevLocs := errorTypes, locations
	t.Cleanup(func() { errorTypes, locations = prevTypes, prevLocs })
	RegisterErrorTypes([]ErrorTypeMeta{
		{Module: "lib", Name: "Boom!",
			Fields: []ErrorFieldMeta{{Name: "code", TypeName: "Int"}, {Name: "note", TypeName: "String"}},
			Render: func(p any) []string {
				v := p.(payload)
				return []string{render(v.code, "lib"), QuoteString(v.note)}
			}},
		{Module: "app", Name: "Quiet!"},
	})
	RegisterLocations([]LocationMeta{
		{Module: "lib", Function: "deep"},
		{Module: "lib", Function: "mid"},
		{Module: "app", Function: "caller"},
		{Module: "app", Function: "main"},
		{Module: "lib", TypeName: "Vault", Function: "open"},
	})
}

func TestInertConstructionHasEmptyHistoryAndNoLocation(t *testing.T) {
	installMetadata(t)
	e := NewError(FirstDeclared, payload{code: 7, note: "x"})
	if e.raised {
		t.Error("an inert value reports raised")
	}
	if len(History(e)) != 0 {
		t.Errorf("history = %v, want empty", History(e))
	}
	if got := Payload(e).(payload); got.code != 7 || got.note != "x" {
		t.Errorf("payload = %+v", got)
	}
}

func TestRaiseOfInertValueStartsANewChainWithoutASte(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	if !e.raised || len(History(e)) != 0 {
		t.Errorf("raised = %v history = %v, want raised with empty history", e.raised, History(e))
	}
}

func TestBubbleRecordsStepsDeepestFirstWithinAModule(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Bubble(e, 1)
	steps := History(e)
	if len(steps) != 1 {
		t.Fatalf("history length = %d, want 1", len(steps))
	}
	if got := steps[0].Location().Function; got != "deep" {
		t.Errorf("step location = %q, want the prior current location", got)
	}
	if got := steps[0].FieldVals(); !slices.Equal(got, []string{"1", "\"n\""}) {
		t.Errorf("fieldVals = %v, want canonical declaration-order strings", got)
	}
	if got := steps[0].Type(); got.Module != "lib" || got.Name != "Boom!" || len(got.Fields) != 2 {
		t.Errorf("step type = %+v", got)
	}
}

func TestRaiseOfARaisedValuePropagatesItsChain(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Raise(e, 1, "")
	if steps := History(e); len(steps) != 1 || steps[0].Location().Function != "deep" {
		t.Errorf("history = %v, want the prior current as one step", steps)
	}
}

// functions returns the routine of each history step in order.
func functions(steps []ErrorStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Location().Function
	}
	return out
}

func TestModuleBoundaryCompactionRemovesTheExitedModulesSegment(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "deep(x: 1)")
	e = Bubble(e, 1)
	received := Cross(e, "app")

	if got := functions(History(received)); len(got) != 0 {
		t.Fatalf("history on receipt = %v, want empty", got)
	}
	if received.location != 1 || received.raiser != "deep(x: 1)" {
		t.Errorf("received current = %d %q, want lib::mid with the deep snapshot", received.location, received.raiser)
	}
	e = Bubble(received, 2)
	if got := functions(History(e)); !slices.Equal(got, []string{"mid"}) {
		t.Fatalf("history after app's propagation = %v, want only the boundary step at lib::mid", got)
	}
	e = Bubble(e, 3)
	if got := functions(History(e)); !slices.Equal(got, []string{"mid", "caller"}) {
		t.Errorf("history = %v, want mid then caller", got)
	}
}

func TestCrossWithinTheReceivingModuleChangesNothing(t *testing.T) {
	installMetadata(t)
	e := Bubble(Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, ""), 1)
	same := Cross(e, "lib")
	if got := functions(History(same)); !slices.Equal(got, []string{"deep"}) || same.location != 1 {
		t.Errorf("Cross from a lib callee into lib = %v at %d, want deep at lib::mid unchanged", got, same.location)
	}
}

func TestDeeperBoundaryStepsSurviveALaterCrossing(t *testing.T) {
	installMetadata(t)

	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Bubble(Cross(e, "app"), 2)
	e = Bubble(e, 3)
	e = Bubble(Cross(e, "lib"), 4)
	if got := functions(History(e)); !slices.Equal(got, []string{"deep", "main"}) {
		t.Errorf("history = %v, want the lib boundary step deep then the app boundary step main", got)
	}
}

func TestRepeatedCrossingsRetainTheEarlierBoundaryStep(t *testing.T) {
	installMetadata(t)

	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Bubble(e, 1)
	e = Bubble(Cross(e, "app"), 2)
	e = Bubble(Cross(e, "lib"), 0)
	e = Bubble(e, 4)
	e = Bubble(Cross(e, "app"), 3)
	if got := functions(History(e)); !slices.Equal(got, []string{"mid", "caller", "open"}) {
		t.Errorf("history = %v, want mid, caller, open", got)
	}
}

func TestCompactionSharesNoStorageWithAStoredCopy(t *testing.T) {
	installMetadata(t)
	e := Bubble(Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, ""), 1)
	stored := e
	received := Bubble(Cross(e, "app"), 2)
	_ = Bubble(received, 3)
	if got := functions(History(stored)); !slices.Equal(got, []string{"deep"}) {
		t.Errorf("stored copy history = %v, want deep unchanged by later compaction and propagation", got)
	}
}

func TestStoredChainStateIsIndependentOfLaterPropagation(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Bubble(e, 1)
	stored := e
	storedSteps := History(stored)
	e = Bubble(e, 1)
	_ = Bubble(stored, 1)
	if got := History(stored); !slices.Equal(mapFuncs(got), mapFuncs(storedSteps)) {
		t.Errorf("stored history changed: %v vs %v", got, storedSteps)
	}
	if len(History(e)) != 2 {
		t.Errorf("propagated history length = %d, want 2", len(History(e)))
	}
}

// mapFuncs returns the function name at each error step.
func mapFuncs(steps []ErrorStep) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Location().Function
	}
	return out
}

func TestReplaceAddsTheCaughtCurrentAfterItsHistory(t *testing.T) {
	installMetadata(t)
	caught := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	caught = Bubble(caught, 1)
	next := NewError(FirstDeclared+1, nil)
	e := ReplaceError(caught, next, 2, "")
	if !e.raised {
		t.Fatal("the replacement is not raised")
	}
	steps := History(e)
	if len(steps) != 2 {
		t.Fatalf("history = %v, want the caught history then the caught current", steps)
	}
	if steps[0].Location().Function != "deep" || steps[1].Location().Function != "mid" {
		t.Errorf("history order = %v", mapFuncs(steps))
	}
	if steps[1].Type().Name != "Boom!" {
		t.Errorf("caught current step type = %q, want the caught type", steps[1].Type().Name)
	}
}

func TestHistoryAndDescriptorAccessorsReturnIndependentValues(t *testing.T) {
	installMetadata(t)
	e := Bubble(Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, ""), 1)
	steps := History(e)
	steps[0] = ErrorStep{}
	if History(e)[0].Location().Function != "deep" {
		t.Error("mutating a History copy changed the carrier")
	}
	s := History(e)[0]
	vals := s.FieldVals()
	vals[0] = "mutated"
	if s.FieldVals()[0] != "1" {
		t.Error("mutating a FieldVals copy changed the step")
	}
	desc := s.Type()
	desc.Fields[0].Name = "mutated"
	if s.Type().Fields[0].Name != "code" {
		t.Error("mutating a descriptor copy changed the metadata")
	}
}

func TestDisplayQualifiesByFormattingModule(t *testing.T) {
	installMetadata(t)
	e := NewError(FirstDeclared, payload{code: 3, note: "hi"})
	if got := e.Display("lib"); got != "Boom! { code: 3, note: \"hi\" }" {
		t.Errorf("Display(lib) = %q", got)
	}
	if got := e.Display("app"); got != "lib::Boom! { code: 3, note: \"hi\" }" {
		t.Errorf("Display(app) = %q", got)
	}
	if got := NewError(FirstDeclared+1, nil).Display("app"); got != "Quiet!" {
		t.Errorf("fieldless Display = %q, want the bare name", got)
	}
}

func TestTerminalRenderingListsCurrentLocationAndRetainedSteps(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Bubble(e, 1)
	got := RenderError(e, "lib")
	want := "Boom! { code: 1, note: \"n\" }\n" +
		"  at lib::mid\n" +
		"  via Boom! [1, \"n\"] at lib::deep"
	if got != want {
		t.Errorf("RenderError =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(RenderError(e, "app"), "lib::Boom!") {
		t.Error("a foreign formatting module does not qualify the type")
	}
}

func TestTerminalRenderingCanOmitOnlyRetainedSteps(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	e = Bubble(e, 1)
	before := History(e)
	got := renderError(e, "lib", false)
	want := "Boom! { code: 1, note: \"n\" }\n  at lib::mid"
	if got != want {
		t.Errorf("suppressed rendering =\n%s\nwant\n%s", got, want)
	}
	if steps := History(e); !slices.Equal(mapFuncs(steps), mapFuncs(before)) ||
		!slices.Equal(steps[0].FieldVals(), before[0].FieldVals()) {
		t.Errorf("history changed from %v to %v", before, steps)
	}
}

func TestEnclosingTypeAppearsInLocationText(t *testing.T) {
	installMetadata(t)
	e := Raise(NewError(FirstDeclared+1, nil), 4, "")
	if got := RenderError(e, "app"); got != "Quiet!\n  at lib::Vault.open" {
		t.Errorf("RenderError = %q", got)
	}
	loc := History(Bubble(e, 3))[0].Location()
	if name, present := loc.TypeName.Get(); !present || name != "Vault" {
		t.Errorf("TypeName = (%q, %v), want present Vault", name, present)
	}
	if _, present := ErrorStep(History(Bubble(Raise(NewError(FirstDeclared+1, nil), 0, ""), 1))[0]).Location().TypeName.Get(); present {
		t.Error("a location without an enclosing type reports a present TypeName")
	}
}

func TestRunReportsAReturnedErrorOnceWithStatusOne(t *testing.T) {
	installMetadata(t)
	out, exits := stubProcess(t)
	Run("app", func() error {
		Log("app", int64(1))
		return Raise(NewError(FirstDeclared, payload{code: 9, note: "bad"}), 3, "")
	})
	want := "1\nlib::Boom! { code: 9, note: \"bad\" }\n  at app::main\n"
	if got := out.String(); got != want {
		t.Errorf("sink output = %q, want %q", got, want)
	}
	if got := *exits; len(got) != 1 || got[0] != 1 {
		t.Errorf("exit calls = %v, want one failing completion", got)
	}
}

func TestRecoveryRaiseSelectsTheChainByRaisedState(t *testing.T) {
	RegisterErrorTypes([]ErrorTypeMeta{{Name: "Caught!"}, {Name: "Fresh!"}})
	RegisterLocations([]LocationMeta{{Function: "deep"}, {Function: "arm"}})
	defer RegisterErrorTypes(nil)
	caught := Raise(NewError(FirstDeclared, nil), 0, "")
	fresh := NewError(FirstDeclared+1, nil)

	replaced := RecoveryRaise(caught, fresh, 1, "")
	if TypeIDOf(replaced) != FirstDeclared+1 || !replaced.raised || replaced.location != 1 {
		t.Errorf("replaced = %+v, want the fresh error current at the arm", replaced)
	}
	if steps := History(replaced); len(steps) != 1 || steps[0].typ != FirstDeclared || steps[0].location != 0 {
		t.Errorf("replaced history = %+v, want the caught current as one step", steps)
	}

	forwarded := RecoveryRaise(caught, caught, 1, "")
	if TypeIDOf(forwarded) != FirstDeclared || forwarded.location != 1 {
		t.Errorf("forwarded = %+v, want the caught error propagated to the arm", forwarded)
	}
	if steps := History(forwarded); len(steps) != 1 || steps[0].location != 0 {
		t.Errorf("forwarded history = %+v, want one propagation step", steps)
	}
	if len(History(caught)) != 0 {
		t.Error("the caught error's history changed while the recovery ran")
	}
}

func TestReportUnrecoveredWritesOneReport(t *testing.T) {
	RegisterErrorTypes([]ErrorTypeMeta{{Name: "Side!"}})
	RegisterLocations([]LocationMeta{{Function: "deep"}, {Function: "arm"}})
	defer RegisterErrorTypes(nil)
	out, _ := stubProcess(t)
	e := Bubble(Raise(NewError(FirstDeclared, nil), 0, ""), 1)
	ReportUnrecovered("", e)
	if got, want := out.String(), "Side!\n  at arm\n  via Side! at deep\n"; got != want {
		t.Errorf("report = %q, want %q", got, want)
	}
	if !SameType(e, Raise(NewError(FirstDeclared, nil), 1, "")) || SameType(e, Origin(OutOfRange, 1, "")) {
		t.Error("SameType compares the current nominal type alone")
	}
}

func TestSnapshotRendersTheRaisingRoutineAndArguments(t *testing.T) {
	installMetadata(t)
	text := Snapshot(4, Argument{Name: "s", Value: "a\n"}, Argument{Name: "n", Value: int64(7)}, Argument{Name: "fn", Value: Opaque("(Int) -> Int")})
	if want := `Vault.open(s: "a\n", n: 7, fn: <(Int) -> Int>)`; text != want {
		t.Errorf("Snapshot = %q, want %q", text, want)
	}

	if got := Snapshot(0, Argument{Name: "e", Value: NewError(FirstDeclared, payload{code: 1, note: "n"})}); got != `deep(e: Boom! { code: 1, note: "n" })` {
		t.Errorf("Snapshot with an error argument = %q", got)
	}
	e := Origin(DivisionByZero, 0, "explode(n: 7)")
	if e.raiser != "explode(n: 7)" {
		t.Errorf("Raiser after Origin = %q", e.raiser)
	}
	e = Bubble(e, 1)
	if e.raiser != "explode(n: 7)" {
		t.Errorf("Raiser after Bubble = %q, want the innermost raiser retained", e.raiser)
	}
	full := RenderError(e, "")
	if want := "DivisionByZero!\n  at lib::mid\n  raised by explode(n: 7)"; full != want {
		t.Errorf("RenderError = %q, want %q, where the raising routine replaces the step at which it raised the error", full, want)
	}
	if suppressed := renderError(e, "", false); !strings.HasSuffix(suppressed, "\n  raised by explode(n: 7)") || strings.Contains(suppressed, "via") {
		t.Errorf("suppressed rendering = %q, want the snapshot retained without history", suppressed)
	}
	if Raise(e, 2, "other()").raiser != "explode(n: 7)" {
		t.Error("re-raising a raised error replaced its snapshot")
	}
	replaced := RecoveryRaise(e, NewError(FirstDeclared+1, nil), 2, "arm(x: 1)")
	if replaced.raiser != "arm(x: 1)" {
		t.Errorf("Raiser after replacement = %q, want the replacing routine's snapshot", replaced.raiser)
	}
	if RecoveryRaise(e, e, 2, "arm(x: 1)").raiser != "explode(n: 7)" {
		t.Error("forwarding the caught error replaced its snapshot")
	}
	bare := Origin(OutOfRange, 1, "")
	if got := RenderError(bare, ""); strings.Contains(got, "raised by") {
		t.Errorf("a parameterless raise renders a snapshot: %q", got)
	}
}

func TestLocationRevisionRendersAsASuffixAndReadsAsADescriptor(t *testing.T) {
	installMetadata(t)
	RegisterLocations([]LocationMeta{{Module: "lib", Function: "deep", Revision: "r7"}})
	if got := locationText(0); got != "lib::deep @ r7" {
		t.Errorf("locationText = %q, want the revision suffix", got)
	}
	e := Bubble(Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, ""), 0)
	steps := History(e)
	if len(steps) != 1 {
		t.Fatalf("history length = %d, want 1", len(steps))
	}
	if got := steps[0].Location(); got.Revision != "r7" || got.Function != "deep" {
		t.Errorf("location descriptor = %+v, want revision r7 at deep", got)
	}
}

func TestPropagationThroughManyFramesIsLinear(t *testing.T) {
	installMetadata(t)
	const depth = 100_000
	start := time.Now()
	e := Raise(NewError(FirstDeclared, payload{code: 1, note: "n"}), 0, "")
	for range depth {
		e = Bubble(e, 1)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("propagating through %d frames took %v, want well under two seconds", depth, elapsed)
	}
	steps := History(e)
	if len(steps) != depth {
		t.Fatalf("history length = %d, want %d", len(steps), depth)
	}
	if steps[0].Location().Function != "deep" || steps[depth-1].Location().Function != "mid" {
		t.Errorf("history ends = %q, %q; want deep first and mid last", steps[0].Location().Function, steps[depth-1].Location().Function)
	}
	if got := Cross(e, "app"); len(History(got)) != 0 {
		t.Errorf("history after leaving lib = %d steps, want none", len(History(got)))
	}
}
