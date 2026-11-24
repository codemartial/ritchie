package rlr

import "testing"

func TestTemporalComparisonKernels(t *testing.T) {
	dates := []struct {
		a, b string
		want int
	}{
		{"d2026-01-01", "d2026-01-01", 0}, {"d2026-01-01", "d2026-01-02", -1}, {"d2026-12-31", "d2026-01-02", 1},
		{"d-000001-12-31", "d0000-01-01", -1}, {"d0000-01-01", "d0000-01-01", 0}, {"d-999999-01-01", "d+999999-12-31", -1},
	}
	for _, tt := range dates {
		a, b := mustDate(t, tt.a), mustDate(t, tt.b)
		if CompareDate(a, b) != tt.want || EqualDate(a, b) != (tt.want == 0) || LessDate(a, b) != (tt.want < 0) {
			t.Errorf("Date %s vs %s: compare %d equal %v less %v, want %d", tt.a, tt.b, CompareDate(a, b), EqualDate(a, b), LessDate(a, b), tt.want)
		}
	}
	times := []struct {
		a, b string
		want int
	}{
		{"t00:00:00", "t00:00:00", 0}, {"t00:00:00", "t00:00:00.000000001", -1}, {"t23:59:59.999999999", "t23:59:59.999999998", 1},
		{"t23:00:00", "t01:00:00", 1}, {"t10:00:00.5", "t10:00:00.500", 0},
	}
	for _, tt := range times {
		a, b := mustTime(t, tt.a), mustTime(t, tt.b)
		if CompareTime(a, b) != tt.want || EqualTime(a, b) != (tt.want == 0) || LessTime(a, b) != (tt.want < 0) {
			t.Errorf("Time %s vs %s: compare %d, want %d", tt.a, tt.b, CompareTime(a, b), tt.want)
		}
	}
	dateTimes := []struct {
		a, b string
		want int
	}{
		{"2026-01-01T10:00:00+10:00", "2026-01-01T00:00:00Z", 0},
		{"2026-01-01T10:00:00+10:00", "2026-01-01T00:00:01Z", -1},
		{"2026-01-01T00:00:00.000000001Z", "2026-01-01T00:00:00Z", 1},
		{"1969-12-31T23:59:59.999999999Z", "1970-01-01T00:00:00Z", -1},
		{"0000-01-01T00:00:00+14:00", "9999-12-31T23:59:59.999999999-14:00", -1},
		{"2026-01-01T00:00:00-05:30", "2026-01-01T05:30:00Z", 0},
	}
	for _, tt := range dateTimes {
		a, b := mustDateTime(t, tt.a), mustDateTime(t, tt.b)
		if CompareDateTime(a, b) != tt.want || EqualDateTime(a, b) != (tt.want == 0) || LessDateTime(a, b) != (tt.want < 0) {
			t.Errorf("DateTime %s vs %s: compare %d, want %d", tt.a, tt.b, CompareDateTime(a, b), tt.want)
		}
		if (KeyOfDateTime(a) == KeyOfDateTime(b)) != (tt.want == 0) {
			t.Errorf("DateTime keys of %s and %s: equal %v, want %v", tt.a, tt.b, KeyOfDateTime(a) == KeyOfDateTime(b), tt.want == 0)
		}
	}
	durations := []struct {
		a, b string
		want int
	}{
		{"1h", "60m", 0}, {"0s", "0s", 0}, {"-1ms", "0s", -1}, {"0s", "1ns", -1}, {"1ns", "-1ns", 1},
		{"4611686018427ms", "4611686018427000000ns", 0},
		{"4611686018426999999ns", "4611686018427ms", -1},
		{"4611686018427387903ms", "4611686018427ms", 1},
		{"-4611686018427387903ms", "4611686018427387903ms", -1},
		{"-4611686018427387903ms", "-4611686018427387902ms", -1},
		{"-4611686018427ms", "-4611686018426999999ns", -1},
		{"2h", "1h30m", 1},
	}
	for _, tt := range durations {
		a, b := mustDuration(t, tt.a), mustDuration(t, tt.b)
		if CompareDuration(a, b) != tt.want || EqualDuration(a, b) != (tt.want == 0) || LessDuration(a, b) != (tt.want < 0) {
			t.Errorf("Duration %s vs %s: compare %d equal %v less %v, want %d", tt.a, tt.b, CompareDuration(a, b), EqualDuration(a, b), LessDuration(a, b), tt.want)
		}
		if CompareDuration(b, a) != -tt.want {
			t.Errorf("Duration %s vs %s is not antisymmetric", tt.b, tt.a)
		}
		if (KeyOfDuration(a) == KeyOfDuration(b)) != (tt.want == 0) {
			t.Errorf("Duration keys of %s and %s: equal %v, want %v", tt.a, tt.b, KeyOfDuration(a) == KeyOfDuration(b), tt.want == 0)
		}
	}
}

func TestExtremesUnderKeepTheFirstOfEquivalentValues(t *testing.T) {
	east, utc, later := mustDateTime(t, "2026-01-01T10:00:00+10:00"), mustDateTime(t, "2026-01-01T00:00:00Z"), mustDateTime(t, "2026-01-01T00:00:01Z")
	if got := MinUnder(LessDateTime, later, east, utc); got != east {
		t.Errorf("MinUnder = %s, want the first least instant %s", got, east)
	}
	if got := MaxUnder(LessDateTime, utc, east, later); got != later {
		t.Errorf("MaxUnder = %s, want %s", got, later)
	}
	if got := MaxUnder(LessDateTime, utc, east); got != utc {
		t.Errorf("MaxUnder over equal instants = %s, want the first %s", got, utc)
	}
	if got := MinUnder(LessDuration, mustDuration(t, "60m"), mustDuration(t, "1h")); got.String() != "1h" {
		t.Errorf("MinUnder over equal spans = %s", got)
	}
	xs := []Duration{mustDuration(t, "2h"), mustDuration(t, "1ns"), mustDuration(t, "-1ms"), mustDuration(t, "60m"), mustDuration(t, "0s"), mustDuration(t, "1h")}
	sorted := Sorted(xs, LessDuration, Same[Duration])
	want := "-1ms 0s 1ns 1h 1h 2h"
	var got string
	for i, d := range sorted {
		if i > 0 {
			got += " "
		}
		got += d.String()
	}
	if got != want {
		t.Errorf("Sorted = %s, want %s", got, want)
	}
}
