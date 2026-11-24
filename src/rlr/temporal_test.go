package rlr

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

// mustDate parses a Date literal or fails the test.
func mustDate(t *testing.T, text string) Date {
	t.Helper()
	d, err := ParseDate(text)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", text, err)
	}
	return d
}

// mustTime parses a Time literal or fails the test.
func mustTime(t *testing.T, text string) Time {
	t.Helper()
	v, err := ParseTime(text)
	if err != nil {
		t.Fatalf("ParseTime(%q): %v", text, err)
	}
	return v
}

// mustDateTime parses a DateTime literal or fails the test.
func mustDateTime(t *testing.T, text string) DateTime {
	t.Helper()
	v, err := ParseDateTime(text)
	if err != nil {
		t.Fatalf("ParseDateTime(%q): %v", text, err)
	}
	return v
}

// mustDuration parses a Duration literal or fails the test.
func mustDuration(t *testing.T, text string) Duration {
	t.Helper()
	neg := strings.HasPrefix(text, "-")
	v, err := ParseDuration(strings.TrimPrefix(text, "-"))
	if err != nil {
		t.Fatalf("ParseDuration(%q): %v", text, err)
	}
	if neg {
		return NegDuration(v)
	}
	return v
}

func TestCalendarAgreesWithTheHostOracle(t *testing.T) {
	years := []int64{MinYear, -4713, -401, -400, -101, -100, -4, -1, 0, 1, 4, 100, 400, 1582, 1600, 1900, 1970, 2000, 2024, 2026, 2100, 9999, 10000, 275760, MaxYear}
	for _, y := range years {
		for m := int64(1); m <= 12; m++ {
			for _, d := range []int64{1, 28, DaysInMonth(y, m)} {
				want := time.Date(int(y), time.Month(m), int(d), 0, 0, 0, 0, time.UTC).Unix() / secondsPerDay
				if got := daysFromCivil(y, m, d); got != want {
					t.Errorf("daysFromCivil(%d, %d, %d) = %d, want %d", y, m, d, got, want)
				}
				if gy, gm, gd := civilFromDays(want); gy != y || gm != m || gd != d {
					t.Errorf("civilFromDays(%d) = %d-%d-%d, want %d-%d-%d", want, gy, gm, gd, y, m, d)
				}
			}
		}
	}
	for _, tt := range []struct {
		y    int64
		leap bool
	}{{2000, true}, {1900, false}, {2024, true}, {2023, false}, {0, true}, {-1, false}, {-4, true}, {-100, false}, {-400, true}} {
		if got := IsLeapYear(tt.y); got != tt.leap {
			t.Errorf("IsLeapYear(%d) = %v, want %v", tt.y, got, tt.leap)
		}
	}
}

func TestDateLiteralsAndDisplay(t *testing.T) {
	for _, text := range []string{"d0000-01-01", "d2026-05-31", "d+010000-01-01", "d-000001-12-31", "d+999999-12-31", "d-999999-01-01", "d2024-02-29", "d1900-02-28"} {
		if got := mustDate(t, text).String(); got != text {
			t.Errorf("%s displays as %s", text, got)
		}
	}
	for _, text := range []string{"d2026-02-29", "d1900-02-29", "d2026-13-01", "d2026-00-10", "d2026-04-31", "d+002026-01-01", "d-000000-01-01", "d+000000-01-01",
		"d2026-1-01", "d2026-01-1", "d02026-01-01", "d2026-01-01x", "d2026-01", "d2026", "2026-01-01", "d+1000000-01-01"} {
		if _, err := ParseDate(text); err == nil {
			t.Errorf("ParseDate(%q) accepted", text)
		}
	}
}

func TestDateMethods(t *testing.T) {
	tests := []struct {
		recv string
		op   string
		n    int64
		want string
	}{
		{"d2025-01-31", "months", 1, "d2025-02-28"},
		{"d2025-01-31", "months", 2, "d2025-03-31"},
		{"d2024-01-31", "months", 1, "d2024-02-29"},
		{"d2025-03-31", "months", -1, "d2025-02-28"},
		{"d2025-01-15", "months", -13, "d2023-12-15"},
		{"d0000-06-15", "months", -7, "d-000001-11-15"},
		{"d2024-02-29", "years", 1, "d2025-02-28"},
		{"d2024-02-29", "years", 4, "d2028-02-29"},
		{"d0000-03-01", "years", -1, "d-000001-03-01"},
		{"d2026-01-01", "days", -1, "d2025-12-31"},
		{"d2026-01-01", "days", 0, "d2026-01-01"},
		{"d2026-12-31", "days", 366, "d2028-01-01"},
		{"d2026-01-01", "weeks", 2, "d2026-01-15"},
		{"d2026-01-01", "weeks", -1, "d2025-12-25"},
		{"d+999999-12-31", "days", 0, "d+999999-12-31"},
	}
	for _, tt := range tests {
		var got Date
		var fault Fault
		recv := mustDate(t, tt.recv)
		switch tt.op {
		case "months":
			got, fault = AddMonths(recv, tt.n)
		case "years":
			got, fault = AddYears(recv, tt.n)
		case "days":
			got, fault = AddDays(recv, tt.n)
		case "weeks":
			got, fault = AddWeeks(recv, tt.n)
		}
		if fault != NoFault || got.String() != tt.want {
			t.Errorf("%s.add%s(%d) = %s %v, want %s", tt.recv, tt.op, tt.n, got, fault, tt.want)
		}
	}

	first, _ := AddMonths(mustDate(t, "d2025-01-31"), 1)
	second, _ := AddMonths(first, 1)
	if second.String() != "d2025-03-28" {
		t.Errorf("chained addMonths = %s, want d2025-03-28", second)
	}
	for _, tt := range []struct {
		recv string
		op   string
		n    int64
	}{
		{"d+999999-12-31", "days", 1}, {"d-999999-01-01", "days", -1}, {"d+999999-12-31", "months", 1},
		{"d-999999-01-01", "years", -1}, {"d2026-01-01", "days", math.MaxInt64}, {"d2026-01-01", "days", math.MinInt64},
		{"d2026-01-01", "weeks", math.MaxInt64}, {"d2026-01-01", "months", math.MinInt64}, {"d2026-01-01", "years", math.MaxInt64},
	} {
		var fault Fault
		recv := mustDate(t, tt.recv)
		switch tt.op {
		case "months":
			_, fault = AddMonths(recv, tt.n)
		case "years":
			_, fault = AddYears(recv, tt.n)
		case "days":
			_, fault = AddDays(recv, tt.n)
		case "weeks":
			_, fault = AddWeeks(recv, tt.n)
		}
		if fault != OutOfRange {
			t.Errorf("%s.add%s(%d) fault = %v, want OutOfRange", tt.recv, tt.op, tt.n, fault)
		}
	}
	if got := DaysUntil(mustDate(t, "d2026-01-01"), mustDate(t, "d2025-12-31")); got != -1 {
		t.Errorf("daysUntil = %d, want -1", got)
	}
	if got := DaysUntil(mustDate(t, "d-999999-01-01"), mustDate(t, "d+999999-12-31")); got != maxDay-minDay {
		t.Errorf("daysUntil across the domain = %d, want %d", got, maxDay-minDay)
	}
}

func TestTimeLiteralsAndDisplay(t *testing.T) {
	tests := []struct{ text, want string }{
		{"t00:00:00", "t00:00:00"}, {"t23:59:59.999999999", "t23:59:59.999999999"}, {"t09:05:02.1", "t09:05:02.1"},
		{"t09:05:02.100000000", "t09:05:02.1"}, {"t09:05:02.000000000", "t09:05:02"}, {"t09:05:02.000000001", "t09:05:02.000000001"},
		{"t12:00:00.5", "t12:00:00.5"}, {"t12:00:00.50", "t12:00:00.5"}, {"t12:00:00.123456789", "t12:00:00.123456789"},
	}
	for _, tt := range tests {
		if got := mustTime(t, tt.text).String(); got != tt.want {
			t.Errorf("%s displays as %s, want %s", tt.text, got, tt.want)
		}
	}
	if a, b := mustTime(t, "t09:05:02.1"), mustTime(t, "t09:05:02.100000000"); a != b {
		t.Errorf("equivalent fractions differ: %v %v", a, b)
	}
	for _, text := range []string{"t24:00:00", "t23:60:00", "t23:59:60", "t23:59:59.", "t23:59:59.0000000000", "t23:59:59.1234567890",
		"t9:05:02", "t09:5:02", "t09:05:2", "t09:05", "t09", "t09:05:02x", "t09:05:02.5x", "09:05:02", "t-1:00:00"} {
		if _, err := ParseTime(text); err == nil {
			t.Errorf("ParseTime(%q) accepted", text)
		}
	}
}

func TestTimeArithmeticIsCyclicAndExact(t *testing.T) {
	tests := []struct{ time, op, dur, want string }{
		{"t23:30:00", "+", "2h", "t01:30:00"},
		{"t00:30:00", "-", "1h", "t23:30:00"},
		{"t12:00:00", "+", "1d", "t12:00:00"},
		{"t12:00:00", "+", "60000000d", "t12:00:00"},
		{"t12:00:00", "-", "3d12h", "t00:00:00"},
		{"t23:59:59.999999999", "+", "2ns", "t00:00:00.000000001"},
		{"t00:00:00", "-", "1ns", "t23:59:59.999999999"},
		{"t12:00:00", "+", "4611686018427387903ms", "t03:36:27.903"},
		{"t12:00:00", "-", "4611686018427387903ms", "t20:23:32.097"},
		{"t12:00:00", "+", "-90m", "t10:30:00"},
		{"t12:00:00", "+", "0s", "t12:00:00"},
	}
	for _, tt := range tests {
		var got Time
		if tt.op == "+" {
			got = AddTime(mustTime(t, tt.time), mustDuration(t, tt.dur))
		} else {
			got = SubTime(mustTime(t, tt.time), mustDuration(t, tt.dur))
		}
		if got.String() != tt.want {
			t.Errorf("%s %s %s = %s, want %s", tt.time, tt.op, tt.dur, got, tt.want)
		}
	}
	diffs := []struct{ a, b, diff, until, since string }{
		{"t01:00:00", "t23:00:00", "-22h", "22h", "2h"},
		{"t23:00:00", "t01:00:00", "22h", "2h", "22h"},
		{"t12:00:00", "t12:00:00", "0s", "0s", "0s"},
		{"t00:00:00", "t23:59:59.999999999", "-23h59m59s999ms999us999ns", "23h59m59s999ms999us999ns", "1ns"},
	}
	for _, tt := range diffs {
		a, b := mustTime(t, tt.a), mustTime(t, tt.b)
		if got := TimeDifference(a, b).String(); got != tt.diff {
			t.Errorf("%s - %s = %s, want %s", tt.a, tt.b, got, tt.diff)
		}
		if got := Until(a, b).String(); got != tt.until {
			t.Errorf("%s.until(%s) = %s, want %s", tt.a, tt.b, got, tt.until)
		}
		if got := Since(a, b).String(); got != tt.since {
			t.Errorf("%s.since(%s) = %s, want %s", tt.a, tt.b, got, tt.since)
		}
		if Until(a, b) != Since(b, a) {
			t.Errorf("until and since are not complementary for %s and %s", tt.a, tt.b)
		}
		if a != b {
			sum, _ := AddDuration(Until(a, b), Until(b, a))
			if sum.String() != "1d" {
				t.Errorf("opposing distances of %s and %s sum to %s", tt.a, tt.b, sum)
			}
		}
	}
}

func TestDateTimeLiteralsAndDisplay(t *testing.T) {
	tests := []struct{ text, want string }{
		{"2026-09-07T09:05:02Z", "2026-09-07T09:05:02Z"},
		{"2026-09-07T09:05:02+00:00", "2026-09-07T09:05:02Z"},
		{"2026-09-07T19:05:02+10:00", "2026-09-07T19:05:02+10:00"},
		{"2026-09-07T19:05:02.500-05:30", "2026-09-07T19:05:02.5-05:30"},
		{"0000-01-01T00:00:00+14:00", "0000-01-01T00:00:00+14:00"},
		{"9999-12-31T23:59:59.999999999-14:00", "9999-12-31T23:59:59.999999999-14:00"},
		{"0000-01-01T00:00:00Z", "0000-01-01T00:00:00Z"},
		{"2026-01-01T00:00:00+13:59", "2026-01-01T00:00:00+13:59"},
	}
	for _, tt := range tests {
		if got := mustDateTime(t, tt.text).String(); got != tt.want {
			t.Errorf("%s displays as %s, want %s", tt.text, got, tt.want)
		}
	}
	if a, b := mustDateTime(t, "2026-09-07T09:05:02Z"), mustDateTime(t, "2026-09-07T09:05:02+00:00"); a != b {
		t.Errorf("Z and +00:00 differ: %v %v", a, b)
	}
	utc := mustDateTime(t, "2026-09-07T09:05:02Z")
	local := mustDateTime(t, "2026-09-07T19:05:02+10:00")
	if s1, n1 := utc.Unix(); true {
		if s2, n2 := local.Unix(); s1 != s2 || n1 != n2 {
			t.Errorf("the same instant under two offsets has coordinates %d.%d and %d.%d", s1, n1, s2, n2)
		}
	}
	if local.Offset() != 600 || utc.Offset() != 0 {
		t.Errorf("offsets = %d %d, want 600 0", local.Offset(), utc.Offset())
	}

	edge := mustDateTime(t, "9999-12-31T23:59:59-14:00")
	if sec, _ := edge.Unix(); sec != time.Date(10000, 1, 1, 13, 59, 59, 0, time.UTC).Unix() {
		t.Errorf("9999-12-31T23:59:59-14:00 has Unix seconds %d", sec)
	}
	for _, text := range []string{"2026-09-07T09:05:02", "2026-09-07T09:05:02-00:00", "2026-09-07T09:05:02+14:01", "2026-09-07T09:05:02+15:00",
		"2026-09-07T09:05:02+10:60", "2026-09-07T09:05:02+1000", "2026-09-07 09:05:02Z", "2026-09-07T24:00:00Z", "2026-02-29T00:00:00Z",
		"+010000-01-01T00:00:00Z", "-000001-01-01T00:00:00Z", "+002026-01-01T00:00:00Z", "2026-09-07T09:05:02.1234567890Z",
		"2026-09-07T09:05:02z", "2026-09-07T09:05:02ZZ", "2026-09-07T09:05:02+10:00x", "2026-9-07T09:05:02Z", "d2026-09-07T09:05:02Z"} {
		if _, err := ParseDateTime(text); err == nil {
			t.Errorf("ParseDateTime(%q) accepted", text)
		}
	}
}

func TestDateTimeArithmetic(t *testing.T) {
	tests := []struct{ dt, op, dur, want string }{
		{"2026-09-07T19:05:02+10:00", "+", "90m", "2026-09-07T20:35:02+10:00"},
		{"2026-09-07T19:05:02+10:00", "-", "1d", "2026-09-06T19:05:02+10:00"},
		{"2026-12-31T23:59:59.999999999Z", "+", "1ns", "2027-01-01T00:00:00Z"},
		{"2026-01-01T00:00:00Z", "-", "1ns", "2025-12-31T23:59:59.999999999Z"},
		{"1969-12-31T23:59:59.5Z", "+", "1s", "1970-01-01T00:00:00.5Z"},
		{"1970-01-01T00:00:00.25Z", "-", "1s", "1969-12-31T23:59:59.25Z"},
		{"0000-01-01T00:00:00+14:00", "+", "0s", "0000-01-01T00:00:00+14:00"},
		{"9999-12-31T23:59:59.999999999-14:00", "-", "1ns", "9999-12-31T23:59:59.999999998-14:00"},
	}
	for _, tt := range tests {
		var got DateTime
		var fault Fault
		if tt.op == "+" {
			got, fault = AddDateTime(mustDateTime(t, tt.dt), mustDuration(t, tt.dur))
		} else {
			got, fault = SubDateTime(mustDateTime(t, tt.dt), mustDuration(t, tt.dur))
		}
		if fault != NoFault || got.String() != tt.want {
			t.Errorf("%s %s %s = %s %v, want %s", tt.dt, tt.op, tt.dur, got, fault, tt.want)
		}
	}
	for _, tt := range []struct{ dt, op, dur string }{
		{"9999-12-31T23:59:59.999999999Z", "+", "1ns"},
		{"0000-01-01T00:00:00Z", "-", "1ns"},
		{"9999-12-31T23:59:59-14:00", "+", "1s"},
		{"0000-01-01T00:00:00+14:00", "-", "1s"},
		{"2026-01-01T00:00:00Z", "+", "4611686018427387903ms"},
		{"2026-01-01T00:00:00Z", "-", "4611686018427387903ms"},
	} {
		var fault Fault
		if tt.op == "+" {
			_, fault = AddDateTime(mustDateTime(t, tt.dt), mustDuration(t, tt.dur))
		} else {
			_, fault = SubDateTime(mustDateTime(t, tt.dt), mustDuration(t, tt.dur))
		}
		if fault != OutOfRange {
			t.Errorf("%s %s %s fault = %v, want OutOfRange", tt.dt, tt.op, tt.dur, fault)
		}
	}
	diffs := []struct{ a, b, want string }{
		{"2026-09-07T20:35:02+10:00", "2026-09-07T19:05:02+10:00", "1h30m"},
		{"2026-09-07T09:05:02Z", "2026-09-07T19:05:02+10:00", "0s"},
		{"2026-09-07T09:05:02.25Z", "2026-09-07T09:05:02.75Z", "-500ms"},
		{"9999-12-31T23:59:59.999999999-14:00", "0000-01-01T00:00:00+14:00", "3652426d3h59m59s999ms"},
		{"0000-01-01T00:00:00+14:00", "9999-12-31T23:59:59.999999999-14:00", "-3652426d3h59m59s999ms"},
	}
	for _, tt := range diffs {
		if got := DateTimeDifference(mustDateTime(t, tt.a), mustDateTime(t, tt.b)).String(); got != tt.want {
			t.Errorf("%s - %s = %s, want %s", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDurationLiteralsAndDisplay(t *testing.T) {
	tests := []struct {
		text, want string
		coarse     bool
	}{
		{"0s", "0s", false}, {"0d", "0s", false}, {"120m", "2h", false}, {"25h", "1d1h", false}, {"1d2h9m", "1d2h9m", false},
		{"1000000001ns", "1s1ns", false}, {"5s", "5s", false}, {"100ms", "100ms", false}, {"1h30m", "1h30m", false},
		{"2d12h", "2d12h", false}, {"1_000ms", "1s", false}, {"1500us", "1ms500us", false}, {"999ns", "999ns", false},
		{"4_611_686_018_426_999_999ns", "53375d23h53m38s426ms999us999ns", false},
		{"4_611_686_018_427_000_000ns", "53375d23h53m38s427ms", true},
		{"4611686018427ms", "53375d23h53m38s427ms", true},
		{"4611686018427387903ms", "53375995583d15h36m27s903ms", true},
		{"60000000d", "60000000d", true},
		{"4611686018427387us", "53375d23h53m38s427ms", true},
		{"4611686018426999999ns", "53375d23h53m38s426ms999us999ns", false},
	}
	for _, tt := range tests {
		d := mustDuration(t, tt.text)
		if got := d.String(); got != tt.want {
			t.Errorf("%s displays as %s, want %s", tt.text, got, tt.want)
		}
		if d.Coarse() != tt.coarse || HighRes(d) == LowRes(d) {
			t.Errorf("%s coarse = %v, want %v", tt.text, d.Coarse(), tt.coarse)
		}
		back := mustDuration(t, d.String())
		if back != d {
			t.Errorf("%s does not round-trip through %s", tt.text, d)
		}
	}

	if a, b := mustDuration(t, "4611686018426999999ns"), mustDuration(t, "4_611_686_018_426_999_999ns"); a != b {
		t.Errorf("separators change the value: %v %v", a, b)
	}
	for _, text := range []string{"1h60m", "1d129m", "1s1000ms", "1d24h", "1m1h", "1h1h", "1s1ns1ms", "1d1000us", "1x", "1", "s", "1 h",
		"1.5s", "_1s", "1_s", "1__0s", "1s_", "1e3s", "4611686018427387904ms", "4611686018427387904000us", "99999999999999999999d",
		"340282366920938463463374607431768211456ns", "-1s", "+1s", "1sx", "1h30", "1D", "1S", "1H", "1MS"} {
		if _, err := ParseDuration(text); err == nil {
			t.Errorf("ParseDuration(%q) accepted", text)
		}
	}
	zero := mustDuration(t, "0s")
	if NegDuration(zero) != zero || zero.Bits() != 0 {
		t.Errorf("zero is not unique: %v %v", NegDuration(zero), zero)
	}
	if d, _ := SubDuration(mustDuration(t, "1s"), mustDuration(t, "1s")); d != zero {
		t.Errorf("1s - 1s = %v, want the one zero", d)
	}
	if d, _ := DivDuration(mustDuration(t, "1ns"), 2); d != zero {
		t.Errorf("1ns / 2 = %v, want the one zero", d)
	}
	if d, _ := DivDuration(mustDuration(t, "-1ns"), 2); d != zero {
		t.Errorf("-1ns / 2 = %v, want the one zero", d)
	}
}

func TestDurationArithmeticSelectsTheTier(t *testing.T) {
	tests := []struct{ a, op, b, want string }{
		{"1d", "+", "129m", "1d2h9m"},
		{"1h", "-", "90m", "-30m"},
		{"4611686018426999999ns", "+", "1ns", "53375d23h53m38s427ms"},
		{"4611686018427ms", "-", "1ns", "53375d23h53m38s426ms999us999ns"},
		{"4611686018427ms", "+", "500us", "53375d23h53m38s427ms"},
		{"4611686018427387903ms", "-", "4611686018427387903ms", "0s"},
		{"4611686018427387903ms", "+", "-4611686018427387903ms", "0s"},
		{"-4611686018427387903ms", "-", "999us", "-53375995583d15h36m27s903ms"},
		{"1ms", "/", "3", "333us333ns"},
		{"-1ms", "/", "3", "-333us333ns"},
		{"1ms", "/", "-3", "-333us333ns"},
		{"-1ms", "/", "-3", "333us333ns"},
		{"4611686018427387903ms", "/", "2", "26687997791d19h48m13s951ms"},
		{"4611686018427387903ms", "/", "1000000000000", "1h16m51s686ms18us427ns"},
		{"4611686018427387903ms", "/", "-9223372036854775807", "-499us999ns"},
		{"4611686018427387903ms", "/", "-9223372036854775808", "-499us999ns"},
		{"4611686018427387903ms", "/", "1", "53375995583d15h36m27s903ms"},
		{"4611686018427387903ms", "*", "-1", "-53375995583d15h36m27s903ms"},
		{"1s", "*", "3", "3s"},
		{"1s", "*", "-3", "-3s"},
		{"-1s", "*", "-3", "3s"},
		{"1s", "*", "0", "0s"},
		{"1ns", "*", "-9223372036854775808", "-106751d23h47m16s854ms"},
		{"1ns", "*", "4611686018426999999", "53375d23h53m38s426ms999us999ns"},
		{"1ns", "*", "4611686018427000000", "53375d23h53m38s427ms"},
		{"1ms", "*", "4611686018427387903", "53375995583d15h36m27s903ms"},
		{"2ms", "*", "2305843009213693951", "53375995583d15h36m27s902ms"},
	}
	for _, tt := range tests {
		var got Duration
		var fault Fault
		a := mustDuration(t, tt.a)
		switch tt.op {
		case "+":
			got, fault = AddDuration(a, mustDuration(t, tt.b))
		case "-":
			got, fault = SubDuration(a, mustDuration(t, tt.b))
		case "*":
			got, fault = MulDuration(a, parseInt(t, tt.b))
		case "/":
			got, fault = DivDuration(a, parseInt(t, tt.b))
		}
		if fault != NoFault || got.String() != tt.want {
			t.Errorf("%s %s %s = %s %v, want %s", tt.a, tt.op, tt.b, got, fault, tt.want)
		}
	}
	overflows := []struct{ a, op, b string }{
		{"4611686018427387903ms", "+", "1ms"}, {"-4611686018427387903ms", "-", "1ms"}, {"4611686018427387903ms", "*", "2"},
		{"1ms", "*", "4611686018427387904"}, {"4611686018427387903ms", "*", "-9223372036854775808"}, {"-4611686018427387903ms", "-", "1ms"},
		{"1d", "*", "9223372036854775807"},
	}
	for _, tt := range overflows {
		var fault Fault
		a := mustDuration(t, tt.a)
		switch tt.op {
		case "+":
			_, fault = AddDuration(a, mustDuration(t, tt.b))
		case "-":
			_, fault = SubDuration(a, mustDuration(t, tt.b))
		case "*":
			_, fault = MulDuration(a, parseInt(t, tt.b))
		}
		if fault != OutOfRange {
			t.Errorf("%s %s %s fault = %v, want OutOfRange", tt.a, tt.op, tt.b, fault)
		}
	}
	if _, fault := DivDuration(mustDuration(t, "1s"), 0); fault != DivisionByZero {
		t.Errorf("1s / 0 fault = %v, want DivisionByZero", fault)
	}
	if _, fault := RatioDuration(mustDuration(t, "1s"), mustDuration(t, "0s")); fault != DivisionByZero {
		t.Errorf("1s / 0s fault = %v, want DivisionByZero", fault)
	}
	ratios := []struct {
		a, b string
		want float64
	}{
		{"1h", "30m", 2}, {"1ns", "3ns", 1.0 / 3}, {"-1d", "1h", -24}, {"0s", "1ns", 0}, {"4611686018427387903ms", "1ms", 4611686018427387903},
		{"1ms", "4611686018427387903ms", 1.0 / 4611686018427387903},
	}
	for _, tt := range ratios {
		if got, fault := RatioDuration(mustDuration(t, tt.a), mustDuration(t, tt.b)); fault != NoFault || got != tt.want {
			t.Errorf("%s / %s = %v %v, want %v", tt.a, tt.b, got, fault, tt.want)
		}
	}

	big, tiny := mustDuration(t, "4611686018427ms"), mustDuration(t, "600us")
	left, _ := AddDuration(big, tiny)
	left, _ = AddDuration(left, tiny)
	both, _ := AddDuration(tiny, tiny)
	right, _ := AddDuration(big, both)
	if left == right || left.String() != "53375d23h53m38s427ms" || right.String() != "53375d23h53m38s428ms" {
		t.Errorf("absorption: (big + tiny) + tiny = %s, big + (tiny + tiny) = %s", left, right)
	}
}

// parseInt reads a decimal integer with an optional leading minus sign.
func parseInt(t *testing.T, text string) int64 {
	t.Helper()
	var n int64
	neg := strings.HasPrefix(text, "-")
	for _, c := range strings.TrimPrefix(text, "-") {
		n = n*10 + int64(c-'0')
	}
	if neg {
		return -n
	}
	return n
}

func TestTemporalConstantCarriers(t *testing.T) {
	d := mustDate(t, "d-002566-01-01")
	if DateFromDays(d.Days()) != d {
		t.Error("Date does not rebuild from its day count")
	}
	tm := mustTime(t, "t23:59:59.999999999")
	if TimeFromNanos(tm.Nanos()) != tm || tm.Nanos() != NanosPerDay-1 {
		t.Error("Time does not rebuild from its nanosecond count")
	}
	dt := mustDateTime(t, "2026-09-07T19:05:02.5+10:00")
	sec, nsec := dt.Unix()
	if DateTimeFromParts(sec, nsec, dt.Offset()) != dt {
		t.Error("DateTime does not rebuild from its parts")
	}
	dur := mustDuration(t, "-4611686018427387903ms")
	if DurationFromBits(dur.Bits()) != dur || dur.Payload() != -CoarseMaxMillis || !dur.Coarse() {
		t.Error("Duration does not rebuild from its carrier")
	}
	if got := render(dt, ""); got != "2026-09-07T19:05:02.5+10:00" {
		t.Errorf("render = %s", got)
	}
	if got := render(ArrayValue{Elems: []any{d, tm, dur}}, ""); got != "[d-002566-01-01, t23:59:59.999999999, -53375995583d15h36m27s903ms]" {
		t.Errorf("embedded render = %s", got)
	}
}

func FuzzTemporalLiteralsRoundTrip(f *testing.F) {
	for _, s := range []string{"d2026-05-31", "d-000001-12-31", "t23:59:59.999999999", "2026-09-07T19:05:02+10:00", "1d2h9m", "4611686018427387903ms", "1x", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if d, err := ParseDate(text); err == nil {
			if back, err := ParseDate(d.String()); err != nil || back != d {
				t.Errorf("Date %q displays as %q, which reads back as %v (%v)", text, d, back, err)
			}
		}
		if v, err := ParseTime(text); err == nil {
			if back, err := ParseTime(v.String()); err != nil || back != v {
				t.Errorf("Time %q displays as %q, which reads back as %v (%v)", text, v, back, err)
			}
		}
		if v, err := ParseDateTime(text); err == nil {
			if back, err := ParseDateTime(v.String()); err != nil || back != v {
				t.Errorf("DateTime %q displays as %q, which reads back as %v (%v)", text, v, back, err)
			}
		}
		if v, err := ParseDuration(text); err == nil {
			if v.Payload() < 0 {
				t.Errorf("Duration %q is negative", text)
			}
			if v.Payload() == 0 {
				return
			}
			if back, err := ParseDuration(v.String()); err != nil || back != v {
				t.Errorf("Duration %q displays as %q, which reads back as %v (%v)", text, v, back, err)
			}
		}
	})
}

func FuzzDurationArithmeticStaysInDomain(f *testing.F) {
	f.Add(int64(1), int64(2), int64(3))
	f.Add(int64(CoarseMaxMillis<<1|1), int64(FineMaxNanos<<1), int64(-1))
	f.Fuzz(func(t *testing.T, a, b, n int64) {

		check := func(v int64) (Duration, bool) {
			d := Duration{v: v}
			p := magnitude64(d.Payload())
			if d.Payload() == math.MinInt64>>1 || (!d.Coarse() && p > FineMaxNanos) || (d.Coarse() && (p < CoarseMinMillis || p > CoarseMaxMillis)) {
				return Duration{}, false
			}
			return d, true
		}
		x, ok := check(a)
		y, ok2 := check(b)
		if !ok || !ok2 {
			return
		}
		type result struct {
			d     Duration
			fault Fault
		}
		outcome := func(d Duration, f Fault) result { return result{d, f} }
		for _, r := range []result{
			outcome(AddDuration(x, y)), outcome(SubDuration(x, y)), outcome(MulDuration(x, n)), outcome(DivDuration(x, n)), {NegDuration(x), NoFault},
		} {
			if r.fault != NoFault {
				continue
			}
			if _, valid := check(r.d.v); !valid {
				t.Errorf("result %v of %v and %v (%d) is outside the domain", r.d, x, y, n)
			}
			if back, err := ParseDuration(strings.TrimPrefix(r.d.String(), "-")); err != nil || (back != r.d && NegDuration(back) != r.d) {
				t.Errorf("result %s does not read back: %v %v", r.d, back, err)
			}
		}
		if q, fault := DivDuration(x, n); fault == NoFault && n != 0 {

			if magnitude64(q.Payload()) > magnitude64(x.Payload()) && !x.Coarse() {
				t.Errorf("%v / %d = %v grew", x, n, q)
			}
		}
	})
}

func TestTheRatioOfTwoSmallDurationsIsTheRoundedExactQuotient(t *testing.T) {
	spans := []int64{1, 3, 7, 1000, 999_999_999, 1_000_000_007, 86_400_000_000_000, 1<<53 - 1, 1 << 53, -1, -3, -1_000_000_007, -(1 << 53)}
	for _, x := range spans {
		for _, y := range spans {
			a, b := Duration{v: x << 1}, Duration{v: y << 1}
			want, _ := new(big.Rat).SetFrac(bigSpan(nanos(a)), bigSpan(nanos(b))).Float64()
			if got, fault := RatioDuration(a, b); fault != NoFault || got != want {
				t.Errorf("%dns / %dns = %v %v, want %v", x, y, got, fault, want)
			}
		}
	}
	if got, _ := RatioDuration(Duration{}, Duration{v: -1 << 1}); math.Signbit(got) {
		t.Errorf("0s / -1ns = %v, want a positive zero", got)
	}
}

func BenchmarkRatioDuration(b *testing.B) {
	x, y := Duration{v: 100_000_000_000 << 1}, Duration{v: 1_000_000_000 << 1}
	b.ReportAllocs()
	for b.Loop() {
		RatioDuration(x, y)
	}
}
