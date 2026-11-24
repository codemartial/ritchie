package rlr

import (
	"testing"
)

func TestFaultsNameTheCatalogAndOriginateWithoutHistory(t *testing.T) {
	installMetadata(t)
	names := map[Fault]string{
		NoFault:               "",
		DivisionByZero:        "DivisionByZero!",
		OutOfRange:            "OutOfRange!",
		InvalidStringBoundary: "InvalidStringBoundary!",
		Malformed:             "Malformed!",
		MissingKey:            "MissingKey!",
		LengthMismatch:        "LengthMismatch!",
		Unavailable:           "Unavailable!",
		Rejected:              "Rejected!",
		Unverified:            "Unverified!",
		Conflict:              "Conflict!",
		Unmodified:            "Unmodified!",
		Cancelled:             "Cancelled!",
		NotFound:              "NotFound!",
		Duplicate:             "Duplicate!",
		Frozen:                "Frozen!",
	}
	if faults := Faults(); len(faults) != len(names)-1 || faults[0] != DivisionByZero || faults[len(faults)-1] != Frozen {
		t.Errorf("Faults() = %v, want every fault from DivisionByZero to Frozen", faults)
	}
	for f, want := range names {
		if got := f.Name(); got != want {
			t.Errorf("Fault(%d).Name() = %q, want %q", f, got, want)
		}
		if f == NoFault {
			continue
		}
		e := Origin(f, 3, "")
		if !e.raised || len(History(e)) != 0 || Payload(e) != nil {
			t.Errorf("Origin(%v) = raised %t, history %v, payload %v; want a raised fieldless error with no history", f, e.raised, History(e), Payload(e))
		}
		if got := e.Display("app"); got != want {
			t.Errorf("Origin(%v).Display = %q, want the bare catalog name %q", f, got, want)
		}
		if got := RenderError(e, "app"); got != want+"\n  at app::main" {
			t.Errorf("RenderError(Origin(%v)) = %q", f, got)
		}
		desc := History(Bubble(e, 2))[0].Type()
		if desc.Name != want || desc.Module != "" || len(desc.Fields) != 0 {
			t.Errorf("catalog descriptor of %v = %+v", f, desc)
		}
	}
	if int(FirstDeclared) != len(catalog) {
		t.Errorf("FirstDeclared = %d, want the catalog size %d", FirstDeclared, len(catalog))
	}
}

func TestDeclaredTypesRegisterAfterTheCatalog(t *testing.T) {
	installMetadata(t)
	if got := errorTypes.at(FirstDeclared).Name; got != "Boom!" {
		t.Errorf("errorTypes[FirstDeclared] = %q, want the first declared type", got)
	}
	if got := errorTypes.at(DivisionByZero.TypeID()).Name; got != "DivisionByZero!" {
		t.Errorf("catalog entry after registration = %q", got)
	}
	e := Raise(NewError(FirstDeclared+1, nil), 3, "")
	if got := e.Display("app"); got != "Quiet!" {
		t.Errorf("declared type at FirstDeclared+1 displays as %q", got)
	}
}

func TestBubbledOriginRecordsTheCheckLocationsOnly(t *testing.T) {
	installMetadata(t)
	e := Bubble(Cross(Bubble(Origin(OutOfRange, 0, ""), 1), "app"), 3)
	want := "OutOfRange!\n  at app::main\n  via OutOfRange! at lib::mid"
	if got := RenderError(e, "app"); got != want {
		t.Errorf("RenderError =\n%s\nwant\n%s", got, want)
	}
	steps := History(e)
	if len(steps) != 1 || steps[0].Location().Function != "mid" || len(steps[0].FieldVals()) != 0 {
		t.Errorf("history after the module boundary = %+v, want the boundary step alone", steps)
	}
}
