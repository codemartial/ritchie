package rlr

import (
	"reflect"
	"strings"
	"testing"
)

// roundTrip encodes a display-form value as JSON and decodes the JSON with a decoder.
func roundTrip[T any](t *testing.T, v T, display func(T) any, d Decoder[T]) (T, string) {
	t.Helper()
	data, err := JSON(display(v))
	if err != nil {
		t.Fatalf("JSON(%v): %v", v, err)
	}
	back, err := Decode(Raw(data), d)
	if err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
	return back, string(data)
}

func TestEachDecimalSurvivesItsJSONFormWithItsQuantum(t *testing.T) {
	for _, text := range []string{
		"0", "0.00", "0E+3", "1.50", "-1.5", "-0.5", "12", "1E+2", "-1.20E-7",
		"1234567890123456789012345678901234", "-9.999999999999999999999999999999999E+6144",
		"1E-6176", "-1E-6143", "1.000000000000000000000000000000000E+6144",
	} {
		d, fault := ReadDecimal(text)
		if fault != NoFault {
			t.Fatalf("ReadDecimal(%q) = %v", text, fault)
		}
		back, data := roundTrip(t, d, func(d Decimal) any { return d }, DecodeDecimal)
		if back != d {
			t.Errorf("%s: encoded %s, decoded %s, want %s", text, data, back, d)
		}
		if !strings.HasPrefix(data, `"`) {
			t.Errorf("%s: encoded %s, want a JSON string", text, data)
		}
	}
}

func TestADecimalRequestReadsTheToNumberGrammar(t *testing.T) {
	for raw, want := range map[string]string{
		`"-1.5"`: "-1.5", `-1.5`: "-1.5", `"-0"`: "0", `-0`: "0", `"1.50"`: "1.50", `1.50`: "1.50",
		`"+5"`: "5", `".5"`: "0.5", `"5."`: "5", `"1e2"`: "1E+2",
		`"12345678901234567890123456789012345"`:   "1.234567890123456789012345678901234E+34",
		`"12345678901234567890123456789012345.5"`: "1.234567890123456789012345678901235E+34",
	} {
		d, err := Decode(Raw(raw), DecodeDecimal)
		if err != nil || d.String() != want {
			t.Errorf("DecodeDecimal(%s) = %s, %v, want %s", raw, d, err, want)
		}
	}
	for _, raw := range []string{`"NaN"`, `"Infinity"`, `"1e99999"`, `"--1"`, `"1_000"`, `"0x10"`, `""`, `"."`, `"1e"`} {
		if d, err := Decode(Raw(raw), DecodeDecimal); err == nil {
			t.Errorf("DecodeDecimal(%s) = %s, want an error", raw, d)
		}
	}
}

func TestEachTemporalValueSurvivesItsExternalText(t *testing.T) {
	dates := []string{"d2026-10-08", "d0000-01-01", "d9999-12-31", "d+010000-01-01", "d-000001-06-15"}
	for _, lit := range dates {
		d, err := ParseDate(lit)
		if err != nil {
			t.Fatal(err)
		}
		back, data := roundTrip(t, d, func(d Date) any { return d }, DecodeDate)
		if back != d || strings.Contains(data, "d") {
			t.Errorf("%s: encoded %s, decoded %v", lit, data, back)
		}
	}
	for _, lit := range []string{"t00:00:00", "t09:00:00.5", "t23:59:59.999999999"} {
		v, err := ParseTime(lit)
		if err != nil {
			t.Fatal(err)
		}
		back, data := roundTrip(t, v, func(v Time) any { return v }, DecodeTime)
		if back != v || strings.Contains(data, "t") {
			t.Errorf("%s: encoded %s, decoded %v", lit, data, back)
		}
	}
	for _, text := range []string{"2026-10-08T12:30:00Z", "2026-10-08T12:30:00.123456789+05:30", "0001-01-01T00:00:00-14:00"} {
		v, err := ParseDateTime(text)
		if err != nil {
			t.Fatal(err)
		}
		back, data := roundTrip(t, v, func(v DateTime) any { return v }, DecodeDateTime)
		if back != v || data != `"`+text+`"` {
			t.Errorf("%s: encoded %s, decoded %v", text, data, back)
		}
	}
	for _, text := range []string{"0s", "5s", "-5s", "-1.5s", "0.000000001s", "86400s", "-4611686018.426999999s", "4611686018427387.903s", "-4611686018427387.903s", "4611686018427.5s"} {
		v, err := ReadDuration(text)
		if err != nil {
			t.Fatalf("ReadDuration(%q): %v", text, err)
		}
		back, data := roundTrip(t, v, func(v Duration) any { return v }, DecodeDuration)
		if back != v || data != `"`+text+`"` {
			t.Errorf("%s: encoded %s, decoded %v", text, data, back)
		}
	}
	bits, data := roundTrip(t, BitSet(5), func(v BitSet) any { return v }, DecodeBitSet)
	if bits != 5 {
		t.Errorf("BitSet: encoded %s, decoded %v", data, bits)
	}
}

func TestTheDurationTierFollowsFromTheMagnitude(t *testing.T) {
	fine, _ := ReadDuration("4611686018.426999999s")
	coarse, _ := ReadDuration("4611686018.427s")
	if fine.Coarse() || !coarse.Coarse() {
		t.Errorf("tiers: %v %v, want fine then coarse", fine.Coarse(), coarse.Coarse())
	}
	if _, err := ReadDuration("4611686018.4270001s"); err == nil {
		t.Error("a coarse-tier magnitude with a fraction of a millisecond was accepted")
	}
}

func TestATemporalRequestReadsTheRFC3339Forms(t *testing.T) {
	var reports strings.Builder
	saved := Diag
	Diag = &reports
	defer func() { Diag = saved }()
	accepted := map[string]func(Raw) (string, error){
		`"2026-10-08"`:                      func(r Raw) (string, error) { v, err := Decode(r, DecodeDate); return DateText(v), err },
		`"+010000-01-01"`:                   func(r Raw) (string, error) { v, err := Decode(r, DecodeDate); return DateText(v), err },
		`"09:00:00.5"`:                      func(r Raw) (string, error) { v, err := Decode(r, DecodeTime); return TimeText(v), err },
		`"2026-10-08t12:30:00z"`:            func(r Raw) (string, error) { v, err := Decode(r, DecodeDateTime); return DateTimeText(v), err },
		`"2026-10-08T12:30:00.1234567891Z"`: func(r Raw) (string, error) { v, err := Decode(r, DecodeDateTime); return DateTimeText(v), err },
		`"-5s"`:                             func(r Raw) (string, error) { v, err := Decode(r, DecodeDuration); return DurationText(v), err },
	}
	want := map[string]string{
		`"2026-10-08"`: "2026-10-08", `"+010000-01-01"`: "+010000-01-01", `"09:00:00.5"`: "09:00:00.5",
		`"2026-10-08t12:30:00z"`: "2026-10-08T12:30:00Z", `"2026-10-08T12:30:00.1234567891Z"`: "2026-10-08T12:30:00.123456789Z", `"-5s"`: "-5s",
	}
	for raw, read := range accepted {
		got, err := read(Raw(raw))
		if err != nil || got != want[raw] {
			t.Errorf("%s = %s, %v, want %s", raw, got, err, want[raw])
		}
	}
	if !strings.Contains(reports.String(), "truncated after the ninth") || strings.Count(reports.String(), "\n") != 1 {
		t.Errorf("reports = %q, want one report of the truncated fraction", reports.String())
	}
	rejected := map[string]func(Raw) error{
		`"d2026-10-08"`:               func(r Raw) error { _, err := Decode(r, DecodeDate); return err },
		`"+002026-10-08"`:             func(r Raw) error { _, err := Decode(r, DecodeDate); return err },
		`"t09:00:00"`:                 func(r Raw) error { _, err := Decode(r, DecodeTime); return err },
		`"09:00:60"`:                  func(r Raw) error { _, err := Decode(r, DecodeTime); return err },
		`"09:00:00.1234567891"`:       func(r Raw) error { _, err := Decode(r, DecodeTime); return err },
		`"2026-10-08T12:30:00"`:       func(r Raw) error { _, err := Decode(r, DecodeDateTime); return err },
		`"2026-10-08T12:30:00-00:00"`: func(r Raw) error { _, err := Decode(r, DecodeDateTime); return err },
		`"1_000s"`:                    func(r Raw) error { _, err := Decode(r, DecodeDuration); return err },
		`"5"`:                         func(r Raw) error { _, err := Decode(r, DecodeDuration); return err },
		`"1h"`:                        func(r Raw) error { _, err := Decode(r, DecodeDuration); return err },
		`"1.0000000001s"`:             func(r Raw) error { _, err := Decode(r, DecodeDuration); return err },
		`"PT1.5S"`:                    func(r Raw) error { _, err := Decode(r, DecodeDuration); return err },
	}
	for raw, read := range rejected {
		if err := read(Raw(raw)); err == nil {
			t.Errorf("%s was accepted, want an error", raw)
		}
	}
}

func TestEachRepresentationThatTheBackendEmitsHasADecoderAndAnEncoder(t *testing.T) {
	type node struct{ v int64 }
	plainNode := func(n node) any { return n.v }
	decodeNode := func(in *Tokens) (node, error) { v, err := DecodeInt[int64](in); return node{v}, err }
	checks := []struct {
		name string
		run  func() (any, any)
	}{
		{"Int8", func() (any, any) { v, _ := roundTrip(t, int8(-8), Plain[int8], DecodeInt[int8]); return v, int8(-8) }},
		{"Int16", func() (any, any) {
			v, _ := roundTrip(t, int16(-16), Plain[int16], DecodeInt[int16])
			return v, int16(-16)
		}},
		{"Int32", func() (any, any) {
			v, _ := roundTrip(t, int32(-32), Plain[int32], DecodeInt[int32])
			return v, int32(-32)
		}},
		{"Int", func() (any, any) {
			v, _ := roundTrip(t, int64(-64), Plain[int64], DecodeInt[int64])
			return v, int64(-64)
		}},
		{"Byte", func() (any, any) { v, _ := roundTrip(t, uint8(255), Plain[uint8], DecodeByte); return v, uint8(255) }},
		{"Float", func() (any, any) { v, _ := roundTrip(t, 1.5, Plain[float64], DecodeFloat); return v, 1.5 }},
		{"Bool", func() (any, any) { v, _ := roundTrip(t, true, Plain[bool], DecodeBool); return v, true }},
		{"String", func() (any, any) { v, _ := roundTrip(t, "tea", Plain[string], DecodeString); return v, "tea" }},
		{"Bytes", func() (any, any) {
			v, _ := roundTrip(t, []byte{1, 2}, Plain[[]byte], DecodeBytes)
			return v, []byte{1, 2}
		}},
		{"Option", func() (any, any) {
			v, _ := roundTrip(t, Some(int64(4)), func(o Option[int64]) any { return o }, optionOf(DecodeInt[int64]))
			return v, Some(int64(4))
		}},
		{"absent Option", func() (any, any) {
			v, _ := roundTrip(t, None[int64](), func(o Option[int64]) any { return o }, optionOf(DecodeInt[int64]))
			return v, None[int64]()
		}},
		{"Box", func() (any, any) {
			v, _ := roundTrip(t, BoxSome(node{3}), func(b Box[node]) any { return MapBox(b, plainNode) }, func(in *Tokens) (Box[node], error) { return DecodeBox(in, decodeNode) })
			return v.Value(), node{3}
		}},
		{"absent Box", func() (any, any) {
			v, _ := roundTrip(t, BoxNone[node](), func(b Box[node]) any { return b }, func(in *Tokens) (Box[node], error) { return DecodeBox(in, decodeNode) })
			return v.IsNone(), true
		}},
		{"array", func() (any, any) {
			v, _ := roundTrip(t, []int64{1, 2}, func(xs []int64) any { return Elements(xs, Plain[int64]) }, arrayOf(DecodeInt[int64]))
			return v, []int64{1, 2}
		}},
		{"enum", func() (any, any) {
			type color uint32
			names := []string{"RED", "GREEN"}
			v, data := roundTrip(t, color(1), func(c color) any { return EnumValue{Name: names[c], Value: int64(9)} }, func(in *Tokens) (color, error) { return DecodeEnum[color](in, names) })
			if data != `"GREEN"` {
				t.Errorf("enum: encoded %s, want the member name", data)
			}
			return v, color(1)
		}},
		{"dictionary", func() (any, any) {
			m := map[string]int64{"a": 1}
			v, _ := roundTrip(t, m, func(m map[string]int64) any { return Entries(m, Plain[string], Plain[int64]) }, dictOf(DecodeString, DecodeInt[int64]))
			return v, m
		}},
	}
	for _, c := range checks {
		if got, want := c.run(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: decoded %v, want %v", c.name, got, want)
		}
	}
}
