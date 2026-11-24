package rlr

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestWithOffsetRetainsTheInstant(t *testing.T) {
	tests := []struct{ dt, offset, want string }{
		{"2026-09-23T01:00:00Z", "10h", "2026-09-23T11:00:00+10:00"},
		{"2026-09-23T01:00:00Z", "-5h30m", "2026-09-22T19:30:00-05:30"},
		{"2026-09-23T01:00:00Z", "45m", "2026-09-23T01:45:00+00:45"},
		{"2026-09-23T11:00:00+10:00", "0s", "2026-09-23T01:00:00Z"},
		{"2026-09-23T01:00:00.5Z", "14h", "2026-09-23T15:00:00.5+14:00"},
		{"2026-09-23T01:00:00.5Z", "-14h", "2026-09-22T11:00:00.5-14:00"},
		{"2026-09-23T01:00:00Z", "840m", "2026-09-23T15:00:00+14:00"},
		{"2026-09-23T01:00:00Z", "50400s", "2026-09-23T15:00:00+14:00"},
		{"0000-01-01T00:00:00+14:00", "14h", "0000-01-01T00:00:00+14:00"},
		{"9999-12-31T23:59:59.999999999-14:00", "-14h", "9999-12-31T23:59:59.999999999-14:00"},
	}
	for _, tt := range tests {
		got, fault := WithOffset(mustDateTime(t, tt.dt), mustDuration(t, tt.offset))
		if fault != NoFault || got.String() != tt.want {
			t.Errorf("WithOffset(%s, %s) = %s %v, want %s", tt.dt, tt.offset, got, fault, tt.want)
		}
		before, after := mustDateTime(t, tt.dt), got
		if DateTimeDifference(after, before) != (Duration{}) {
			t.Errorf("WithOffset(%s, %s) moved the instant by %s", tt.dt, tt.offset, DateTimeDifference(after, before))
		}
		bs, bn := Unix(before)
		if as, an := Unix(after); as != bs || an != bn {
			t.Errorf("WithOffset(%s, %s) changed the Unix pair", tt.dt, tt.offset)
		}
	}
	rejected := []struct{ dt, offset string }{
		{"2026-09-23T01:00:00Z", "30s"},
		{"2026-09-23T01:00:00Z", "1ns"},
		{"2026-09-23T01:00:00Z", "1ms"},
		{"2026-09-23T01:00:00Z", "1m1s"},
		{"2026-09-23T01:00:00Z", "14h1m"},
		{"2026-09-23T01:00:00Z", "-14h1m"},
		{"2026-09-23T01:00:00Z", "60000000d"},
		{"2026-09-23T01:00:00Z", "-60000000d"},
		{"9999-12-31T23:00:00Z", "2h"},
		{"0000-01-01T01:00:00Z", "-2h"},
		{"0000-01-01T00:00:00+14:00", "13h59m"},
		{"9999-12-31T23:59:59.999999999-14:00", "-13h59m"},
	}
	for _, tt := range rejected {
		if got, fault := WithOffset(mustDateTime(t, tt.dt), mustDuration(t, tt.offset)); fault != OutOfRange || got != (DateTime{}) {
			t.Errorf("WithOffset(%s, %s) = %s %v, want OutOfRange", tt.dt, tt.offset, got, fault)
		}
	}
}

func TestOffsetMinutesAdmitsWholeMinutesWithinFourteenHours(t *testing.T) {
	tests := []struct {
		d    string
		want int64
		ok   bool
	}{
		{"0s", 0, true}, {"1m", 1, true}, {"-1m", -1, true}, {"14h", 840, true}, {"-14h", -840, true},
		{"13h59m", 839, true}, {"14h1m", 0, false}, {"-14h1m", 0, false}, {"59s", 0, false}, {"1m1ns", 0, false},
		{"60000000d", 0, false},
	}
	for _, tt := range tests {
		got, ok := offsetMinutes(mustDuration(t, tt.d))
		if got != tt.want || ok != tt.ok {
			t.Errorf("offsetMinutes(%s) = %d %v, want %d %v", tt.d, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDateTimeFromUnixIsExactAtEveryInt(t *testing.T) {
	tests := []struct {
		sec, nsec int64
		want      string
	}{
		{0, 0, "1970-01-01T00:00:00Z"},
		{-1, 999_999_999, "1969-12-31T23:59:59.999999999Z"},
		{-1, 0, "1969-12-31T23:59:59Z"},
		{minUnixSec, 0, "0000-01-01T00:00:00Z"},
		{maxUnixSec, 999_999_999, "9999-12-31T23:59:59.999999999Z"},
		{1_767_225_600, 5, "2026-01-01T00:00:00.000000005Z"},
	}
	for _, tt := range tests {
		got, fault := DateTimeFromUnix(tt.sec, tt.nsec)
		if fault != NoFault || got.String() != tt.want {
			t.Errorf("DateTimeFromUnix(%d, %d) = %s %v, want %s", tt.sec, tt.nsec, got, fault, tt.want)
		}
		if s, n := Unix(got); s != tt.sec || n != tt.nsec {
			t.Errorf("Unix(DateTimeFromUnix(%d, %d)) = %d, %d", tt.sec, tt.nsec, s, n)
		}
	}
	rejected := []struct{ sec, nsec int64 }{
		{0, -1}, {0, 1_000_000_000}, {0, math.MaxInt64}, {0, math.MinInt64},
		{math.MaxInt64, 0}, {math.MinInt64, 0}, {math.MinInt64, 999_999_999}, {math.MaxInt64, math.MaxInt64},
		{minUnixSec - 1, 999_999_999}, {maxUnixSec + 1, 0},
	}
	for _, tt := range rejected {
		if got, fault := DateTimeFromUnix(tt.sec, tt.nsec); fault != OutOfRange || got != (DateTime{}) {
			t.Errorf("DateTimeFromUnix(%d, %d) = %s %v, want OutOfRange", tt.sec, tt.nsec, got, fault)
		}
	}
	if minUnixSec != -62_167_219_200 || maxUnixSec != 253_402_300_799 {
		t.Errorf("UTC domain bounds = %d, %d", minUnixSec, maxUnixSec)
	}
}

func TestOffsetReadsTheRetainedOffset(t *testing.T) {
	for _, tt := range []struct{ dt, want string }{
		{"2026-09-23T01:00:00Z", "0s"},
		{"2026-09-23T11:00:00+10:00", "10h"},
		{"2026-09-22T19:30:00-05:30", "-5h30m"},
		{"2026-09-23T15:00:00+14:00", "14h"},
		{"2026-09-22T11:00:00-14:00", "-14h"},
	} {
		if got := Offset(mustDateTime(t, tt.dt)); got.String() != tt.want {
			t.Errorf("Offset(%s) = %s, want %s", tt.dt, got, tt.want)
		}
	}
	for _, offset := range []string{"5h45m", "-1m", "0s", "14h"} {
		d := mustDuration(t, offset)
		shifted, _ := WithOffset(mustDateTime(t, "2026-09-23T01:00:00Z"), d)
		if Offset(shifted) != d {
			t.Errorf("withOffset(%s).offset() = %s", offset, Offset(shifted))
		}
	}
}

func TestUnixDecomposesByFloorAndIgnoresTheOffset(t *testing.T) {
	tests := []struct {
		dt        string
		sec, nsec int64
	}{
		{"1970-01-01T00:00:00Z", 0, 0},
		{"1969-12-31T23:59:59.999999999Z", -1, 999_999_999},
		{"1969-12-31T23:59:59.5+02:00", -7201, 500_000_000},
		{"2026-01-01T00:00:00Z", 1_767_225_600, 0},
		{"2026-01-01T10:00:00+10:00", 1_767_225_600, 0},
		{"2025-12-31T14:00:00-10:00", 1_767_225_600, 0},
		{"9999-12-31T23:59:59.999999999-14:00", 253_402_351_199, 999_999_999},
		{"0000-01-01T00:00:00+14:00", -62_167_269_600, 0},
	}
	for _, tt := range tests {
		sec, nsec := Unix(mustDateTime(t, tt.dt))
		if sec != tt.sec || nsec != tt.nsec {
			t.Errorf("Unix(%s) = %d, %d, want %d, %d", tt.dt, sec, nsec, tt.sec, tt.nsec)
		}
	}

	for _, dt := range []string{"9999-12-31T23:59:59.999999999-14:00", "0000-01-01T00:00:00+14:00"} {
		if _, fault := DateTimeFromUnix(Unix(mustDateTime(t, dt))); fault != OutOfRange {
			t.Errorf("DateTimeFromUnix(Unix(%s)) = %v, want OutOfRange", dt, fault)
		}
	}
}

func TestClockValuesRetainOneOffset(t *testing.T) {
	for _, tt := range []struct{ offset, display string }{
		{"0s", "r::Clock { offset: Z }"}, {"-4h", "r::Clock { offset: -04:00 }"}, {"5h30m", "r::Clock { offset: +05:30 }"},
		{"14h", "r::Clock { offset: +14:00 }"}, {"-14h", "r::Clock { offset: -14:00 }"}, {"1m", "r::Clock { offset: +00:01 }"},
	} {
		c, fault := ClockWithOffset(mustDuration(t, tt.offset))
		if fault != NoFault || c.Display("m") != tt.display {
			t.Errorf("ClockWithOffset(%s) = %q %v, want %q", tt.offset, c.Display("m"), fault, tt.display)
		}
		if ClockOffset(c) != mustDuration(t, tt.offset) {
			t.Errorf("ClockOffset(%s) = %s", tt.offset, ClockOffset(c))
		}
	}
	for _, offset := range []string{"30s", "1ns", "1ms", "1m1s", "14h1m", "-14h1m", "60000000d"} {
		if c, fault := ClockWithOffset(mustDuration(t, offset)); fault != OutOfRange || c != (Clock{}) {
			t.Errorf("ClockWithOffset(%s) = %v %v, want OutOfRange", offset, c, fault)
		}
	}
}

func TestClockTimeReadsTheBoundClockUnderItsOffset(t *testing.T) {
	installFixed(t, time.Date(2026, 9, 23, 19, 5, 2, 500, time.FixedZone("east", 10*60*60)), 0)
	for _, tt := range []struct{ offset, want string }{
		{"0s", "2026-09-23T09:05:02.0000005Z"},
		{"-4h", "2026-09-23T05:05:02.0000005-04:00"},
		{"5h30m", "2026-09-23T14:35:02.0000005+05:30"},
		{"14h", "2026-09-23T23:05:02.0000005+14:00"},
		{"-14h", "2026-09-22T19:05:02.0000005-14:00"},
	} {
		c := must(ClockWithOffset(mustDuration(t, tt.offset)))
		if got := ClockTime(c).String(); got != tt.want {
			t.Errorf("ClockTime(%s) = %s, want %s", tt.offset, got, tt.want)
		}
		if DateTimeDifference(ClockTime(c), Now()) != (Duration{}) {
			t.Errorf("ClockTime(%s) reads a different instant from Now()", tt.offset)
		}
	}
	installFixed(t, time.Date(9999, 12, 31, 20, 0, 0, 0, time.UTC), 0)
	if got := ClockTime(must(ClockWithOffset(mustDuration(t, "3h59m")))).String(); got != "9999-12-31T23:59:00+03:59" {
		t.Errorf("reading at the ceiling = %s", got)
	}
	installFixed(t, time.Date(0, 1, 1, 5, 0, 0, 0, time.UTC), 0)
	if got := ClockTime(must(ClockWithOffset(mustDuration(t, "-5h")))).String(); got != "0000-01-01T00:00:00-05:00" {
		t.Errorf("reading at the floor = %s", got)
	}
}

func TestClockTimeOutsideTheLocalYearDomainInvalidatesTheRun(t *testing.T) {
	for _, tt := range []struct {
		instant time.Time
		offset  string
		year    string
	}{
		{time.Date(9999, 12, 31, 20, 0, 0, 0, time.UTC), "4h", "10000"},
		{time.Date(0, 1, 1, 5, 0, 0, 0, time.UTC), "-5h1m", "-1"},
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "0s", "10000"},
		{time.Date(-1, 12, 31, 23, 0, 0, 0, time.UTC), "0s", "-1"},
		{time.Date(-1_000_000_000, 1, 1, 0, 0, 0, 0, time.UTC), "0s", "-1000000000"},
	} {
		out, exits := stubProcess(t)
		installFixed(t, tt.instant, 0)
		ClockTime(must(ClockWithOffset(mustDuration(t, tt.offset))))
		if len(*exits) != 1 || (*exits)[0] != 2 {
			t.Errorf("%s under %s: exits = %v, want one status 2", tt.instant, tt.offset, *exits)
		}
		if !strings.Contains(out.String(), "year "+tt.year+",") {
			t.Errorf("%s under %s: diagnostic = %q, want year %s", tt.instant, tt.offset, out.String(), tt.year)
		}
	}

	out, exits := stubProcess(t)
	for _, tt := range []struct {
		instant time.Time
		offset  string
		want    string
	}{
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "-1h", "9999-12-31T23:00:00-01:00"},
		{time.Date(-1, 12, 31, 23, 0, 0, 0, time.UTC), "1h", "0000-01-01T00:00:00+01:00"},
		{time.Date(9999, 12, 31, 20, 0, 0, 0, time.UTC), "0s", "9999-12-31T20:00:00Z"},
	} {
		installFixed(t, tt.instant, 0)
		if got := ClockTime(must(ClockWithOffset(mustDuration(t, tt.offset)))).String(); got != tt.want {
			t.Errorf("%s under %s reads %s, want %s", tt.instant, tt.offset, got, tt.want)
		}
	}
	if len(*exits) != 0 || out.Len() != 0 {
		t.Errorf("in-domain readings exited %v with diagnostics %q", *exits, out.String())
	}
}
