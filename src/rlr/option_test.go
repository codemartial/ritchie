package rlr

import "testing"

func TestOptionDisplaysPayloadOrNone(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"absent int", None[int64](), "none"},
		{"present int", Some(int64(7)), "7"},
		{"present zero", Some(int64(0)), "0"},
		{"present false", Some(false), "false"},
		{"standalone string", Some("Ada"), "Ada"},
		{"embedded string", ArrayValue{Elems: []any{Some("Ada"), None[string]()}}, "[\"Ada\", none]"},
		{"nested array", MapOption(Some([]int64{1, 2}), func(xs []int64) any { return Elements(xs, Plain[int64]) }), "[1, 2]"},
		{"absent array", MapOption(None[[]int64](), func(xs []int64) any { return Elements(xs, Plain[int64]) }), "none"},
		{"struct field", StructValue{Name: "P", Fields: []Field{{Name: "s", Value: Some("x")}, {Name: "n", Value: None[int64]()}}}, "P { s: \"x\", n: none }"},
	}
	for _, tt := range tests {
		if got := render(tt.v, ""); got != tt.want {
			t.Errorf("%s: render = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestOptionEqualityFollowsPresenceThenPayload(t *testing.T) {
	eq := Equal[int64]
	if !EqualOptions(None[int64](), None[int64](), eq) {
		t.Error("two absent values are unequal")
	}
	if EqualOptions(Some(int64(0)), None[int64](), eq) || EqualOptions(None[int64](), Some(int64(0)), eq) {
		t.Error("a present zero equals absence")
	}
	if !EqualOptions(Some(int64(3)), Some(int64(3)), eq) || EqualOptions(Some(int64(3)), Some(int64(4)), eq) {
		t.Error("present payloads do not compare by payload equality")
	}
	nan := Some(0.0 / func() float64 { return 0 }())
	if EqualOptions(nan, nan, Equal[float64]) {
		t.Error("a present NaN equals itself")
	}
	if !OptionEqual(EqualBytes)(Some([]byte("a")), Some([]byte("a"))) {
		t.Error("the option equality function does not apply the payload equality")
	}
}

func TestOptionWideningAndCloning(t *testing.T) {
	if got := WidenOption[int8, int64](Some(int8(-128))); got != Some(int64(-128)) {
		t.Errorf("WidenOption(Some(-128)) = %+v", got)
	}
	if got := WidenOption[int8, int64](None[int8]()); got.IsSome() {
		t.Errorf("WidenOption(None) = %+v, want absent", got)
	}
	original := Some([]int64{1, 2})
	copied := CloneOption(original, Clone[int64])
	copied.Unwrap()[0] = 9
	if original.Unwrap()[0] != 1 {
		t.Error("CloneOption shares the payload's storage")
	}
	if CloneOption(None[[]int64](), Clone[int64]).IsSome() {
		t.Error("CloneOption of an absent value is present")
	}
	ref := Some(int64(1))
	*ref.Ref() = 5
	if ref.Unwrap() != 5 {
		t.Error("Ref does not reach the live payload")
	}
	defer func() {
		if recover() == nil {
			t.Error("Ref on an absent option did not panic")
		}
	}()
	var absent Option[int64]
	absent.Ref()
}

func TestFlatEndpointsReturnTheElementItself(t *testing.T) {
	xs := []Option[int64]{None[int64](), Some(int64(7)), None[int64]()}
	if got := FirstFlat(xs, Same[Option[int64]]); got.IsSome() {
		t.Errorf("FirstFlat = %+v, want the absent element", got)
	}
	if got := LastFlat(xs, Same[Option[int64]]); got.IsSome() {
		t.Errorf("LastFlat = %+v, want the absent element", got)
	}
	if got := FirstFlat(xs[1:], Same[Option[int64]]); got != Some(int64(7)) {
		t.Errorf("FirstFlat = %+v, want 7", got)
	}
	if FirstFlat(nil, Same[Option[int64]]).IsSome() || LastFlat([]Option[int64]{}, Same[Option[int64]]).IsSome() {
		t.Error("an empty array has a present endpoint")
	}
	out, _ := stubProcess(t)
	Log("", Elements(xs, Plain[Option[int64]]))
	if out.String() != "[none, 7, none]\n" {
		t.Errorf("logged %q", out.String())
	}
}
