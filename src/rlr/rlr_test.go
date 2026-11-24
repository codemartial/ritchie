package rlr

import (
	"bytes"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"testing"
)

// stubProcess captures sink output and process completion.
func stubProcess(t *testing.T) (*bytes.Buffer, *[]int) {
	t.Helper()
	var out bytes.Buffer
	return &out, stubSink(t, &out)
}

// stubSink routes sink output to w at the reporting level that writes every report.
func stubSink(t *testing.T, w io.Writer) *[]int {
	t.Helper()
	var exits []int
	t.Setenv(LogLevelEnv, "3")
	oldDiag, oldOut, oldExit, oldLevel := Diag, Out, Exit, level
	Diag, Out, level = w, w, recoveryLevel
	Exit = func(code int) { exits = append(exits, code) }
	t.Cleanup(func() { Diag, Out, Exit, level = oldDiag, oldOut, oldExit, oldLevel })
	return &exits
}

func TestLogRendersAdmittedValuesWithOneLineBreak(t *testing.T) {
	out, _ := stubProcess(t)
	Log("", int64(42))
	Log("", int64(-7))
	Log("", int8(-128))
	Log("", int16(32767))
	Log("", int32(0))
	Log("", true)
	Log("", false)
	Log("", must(ParseDecimal("1.50")))
	want := "42\n-7\n-128\n32767\n0\ntrue\nfalse\n1.50\n"
	if got := out.String(); got != want {
		t.Errorf("sink output = %q, want %q", got, want)
	}
}

func TestDecimalRendersNestedAsItsOwnText(t *testing.T) {
	out, _ := stubProcess(t)
	d := NegDecimal(must(ParseDecimal("0.25")))
	Log("", ArrayValue{Elems: []any{d, d}})
	Log("", StructValue{Name: "P", Fields: []Field{{Name: "x", Value: d}}})
	Log("", Some(d))
	want := "[-0.25, -0.25]\nP { x: -0.25 }\n-0.25\n"
	if got := out.String(); got != want {
		t.Errorf("Log output = %q, want %q", got, want)
	}
	got := must(Format("", "a=", ArrayValue{Elems: []any{d}}, " s=", StructValue{Name: "P", Fields: []Field{{Name: "x", Value: d}}}, " o=", Some(d), " d=", d))
	if want := "a=[-0.25] s=P { x: -0.25 } o=-0.25 d=-0.25"; got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
}

func TestFloatDisplay(t *testing.T) {
	tenth, fifth := 0.1, 0.2
	tests := []struct {
		value float64
		want  string
	}{
		{1.5, "1.5"},
		{3, "3"},
		{0.1, "0.1"},
		{tenth + fifth, "0.30000000000000004"},
		{100000, "100000"},
		{123456, "123456"},
		{1000000, "1e+06"},
		{1234567, "1.234567e+06"},
		{0.0001, "0.0001"},
		{0.00001, "1e-05"},
		{1e300, "1e+300"},
		{5e-324, "5e-324"},
		{-2.5, "-2.5"},
		{math.Copysign(0, -1), "-0"},
		{0, "0"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "+Inf"},
		{math.Inf(-1), "-Inf"},
	}
	for _, tt := range tests {
		if got := render(tt.value, ""); got != tt.want {
			t.Errorf("render(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestLogWritesInCallOrder(t *testing.T) {
	out, _ := stubProcess(t)
	for i := int64(1); i <= 3; i++ {
		Log("", i)
	}
	if got := out.String(); got != "1\n2\n3\n" {
		t.Errorf("sink output = %q, want the call order", got)
	}
}

func TestRunInvokesMainThenCompletesWithZero(t *testing.T) {
	out, exits := stubProcess(t)
	ran := false
	Run("", func() error {
		ran = true
		Log("", int64(1))
		return nil
	})
	if !ran {
		t.Fatal("the entry runner did not invoke main")
	}
	if got := *exits; len(got) != 1 || got[0] != 0 {
		t.Errorf("exit calls = %v, want one completion with status 0", got)
	}
	if out.String() != "1\n" {
		t.Errorf("sink output = %q, want main's line", out.String())
	}
}

func TestIntegerDivisionTruncatesAndWrapsAtEveryWidth(t *testing.T) {
	tests := []struct {
		a, b, div, rem int64
	}{
		{7, 2, 3, 1},
		{-7, 2, -3, -1},
		{7, -2, -3, 1},
		{-7, 3, -2, -1},
		{-1 << 63, -1, -1 << 63, 0},
	}
	for _, tt := range tests {
		if got := must(Div(tt.a, tt.b)); got != tt.div {
			t.Errorf("Div(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.div)
		}
		if got := must(Rem(tt.a, tt.b)); got != tt.rem {
			t.Errorf("Rem(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.rem)
		}
	}
	if got := must(Div(int8(-128), int8(-1))); got != -128 {
		t.Errorf("Div[int8](-128, -1) = %d, want the minimum", got)
	}
	if got := must(Rem(int8(-128), int8(-1))); got != 0 {
		t.Errorf("Rem[int8](-128, -1) = %d, want 0", got)
	}
	if got := must(Div(int16(-32768), int16(-1))); got != -32768 {
		t.Errorf("Div[int16](-32768, -1) = %d, want the minimum", got)
	}
	if got := must(Div(int32(-2147483648), int32(-1))); got != -2147483648 {
		t.Errorf("Div[int32] at the minimum = %d, want the minimum", got)
	}
}

func TestZeroDivisorEscapingMainIsReportedOnceWithStatusOne(t *testing.T) {
	installMetadata(t)
	out, exits := stubProcess(t)
	later := false
	deep := func() (int64, error) {
		q, f := Div(int64(1), int64(0))
		if f != NoFault {
			return 0, Origin(f, 0, "")
		}
		return q, nil
	}
	Run("app", func() error {
		Log("app", int64(1))
		q, err := deep()
		if err != nil {
			return Bubble(err.(RitchieError), 3)
		}
		Log("app", q)
		later = true
		return nil
	})
	if later {
		t.Error("a statement after the failed division executed")
	}
	want := "1\nDivisionByZero!\n  at app::main\n  via DivisionByZero! at lib::deep\n"
	if got := out.String(); got != want {
		t.Errorf("sink output = %q, want %q", got, want)
	}
	if got := *exits; len(got) != 1 || got[0] != 1 {
		t.Errorf("exit calls = %v, want one failing completion", got)
	}
	out.Reset()
	*exits = nil
	Run("app", func() error {
		if _, f := Rem(int8(5), int8(0)); f != NoFault {
			return Origin(f, 3, "")
		}
		return nil
	})
	if got := out.String(); got != "DivisionByZero!\n  at app::main\n" {
		t.Errorf("sink output = %q, want the catalog error at main", got)
	}
	if got := *exits; len(got) != 1 || got[0] != 1 {
		t.Errorf("exit calls = %v, want one failing completion", got)
	}
}

func TestHostFaultInEntryIsADefect(t *testing.T) {
	out, exits := stubProcess(t)
	Run("", func() error { panic("host fault") })
	if got := out.String(); !strings.HasPrefix(got, "ritchie runtime: internal defect: host fault\n") || !strings.Contains(got, "goroutine ") {
		t.Errorf("sink output = %q, want the defect report with the fault value and the host stack", got)
	}
	if got := *exits; len(got) != 1 || got[0] != 2 {
		t.Errorf("exit calls = %v, want one completion with status 2", got)
	}
}

func TestFloatRemainder(t *testing.T) {
	inf, nan := math.Inf(1), math.NaN()
	tests := []struct {
		a, b, want float64
	}{
		{7.5, 2, 1.5},
		{-7.5, 2, -1.5},
		{7.5, -2, 1.5},
		{5, inf, 5},
		{-5, -inf, -5},
		{5, 0, nan},
		{inf, 2, nan},
		{nan, 2, nan},
		{5, nan, nan},
	}
	for _, tt := range tests {
		got := RemFloat(tt.a, tt.b)
		if math.IsNaN(tt.want) {
			if !math.IsNaN(got) {
				t.Errorf("RemFloat(%v, %v) = %v, want NaN", tt.a, tt.b, got)
			}
			continue
		}
		if got != tt.want || math.Signbit(got) != math.Signbit(tt.want) {
			t.Errorf("RemFloat(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestIntegerHelpersAtTheMinimum(t *testing.T) {
	if got := Abs(int8(-128)); got != -128 {
		t.Errorf("Abs[int8](-128) = %d, want the minimum", got)
	}
	if got := Abs(int64(-5)); got != 5 {
		t.Errorf("Abs(-5) = %d, want 5", got)
	}
	if got := Min(int16(3), int16(-32768), int16(7)); got != -32768 {
		t.Errorf("Min = %d, want the minimum", got)
	}
	if got := Max(int32(3)); got != 3 {
		t.Errorf("Max of one = %d, want 3", got)
	}
	if got := Max(int64(-1), int64(9), int64(4)); got != 9 {
		t.Errorf("Max = %d, want 9", got)
	}
}

func TestFloatHelpersOnSpecialValues(t *testing.T) {
	inf, nan, negZero := math.Inf(1), math.NaN(), math.Copysign(0, -1)
	if got := AbsFloat(negZero); got != 0 || math.Signbit(got) {
		t.Errorf("AbsFloat(-0) = %v, want +0", got)
	}
	if got := AbsFloat(-inf); !math.IsInf(got, 1) {
		t.Errorf("AbsFloat(-Inf) = %v, want +Inf", got)
	}
	if got := AbsFloat(nan); !math.IsNaN(got) {
		t.Errorf("AbsFloat(NaN) = %v, want NaN", got)
	}
	if got := AbsFloat(-2.5); got != 2.5 {
		t.Errorf("AbsFloat(-2.5) = %v, want 2.5", got)
	}
	for _, args := range [][]float64{{1, nan}, {nan, 1}, {2, 3, nan, 1}} {
		if got := MinFloat(args...); !math.IsNaN(got) {
			t.Errorf("MinFloat(%v) = %v, want NaN", args, got)
		}
		if got := MaxFloat(args...); !math.IsNaN(got) {
			t.Errorf("MaxFloat(%v) = %v, want NaN", args, got)
		}
	}
	if got := MinFloat(0, negZero); !math.Signbit(got) {
		t.Errorf("MinFloat(+0, -0) = %v, want -0", got)
	}
	if got := MinFloat(negZero, 0); !math.Signbit(got) {
		t.Errorf("MinFloat(-0, +0) = %v, want -0", got)
	}
	if got := MaxFloat(negZero, 0); math.Signbit(got) {
		t.Errorf("MaxFloat(-0, +0) = %v, want +0", got)
	}
	if got := MaxFloat(0, negZero); math.Signbit(got) {
		t.Errorf("MaxFloat(+0, -0) = %v, want +0", got)
	}
	if got := MinFloat(negZero, negZero); !math.Signbit(got) {
		t.Errorf("MinFloat(-0, -0) = %v, want -0", got)
	}
	if got := MinFloat(1, -inf, 3); !math.IsInf(got, -1) {
		t.Errorf("MinFloat with -Inf = %v, want -Inf", got)
	}
	if got := MaxFloat(1, inf, 3); !math.IsInf(got, 1) {
		t.Errorf("MaxFloat with +Inf = %v, want +Inf", got)
	}
	if got := MaxFloat(1.5, 2.5, 0.5); got != 2.5 {
		t.Errorf("MaxFloat = %v, want 2.5", got)
	}
}

func TestABusyServePortIsStatedAsBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	_, err = net.Listen("tcp", ServeAddress(port))
	if err == nil {
		t.Fatal("a second listener on a busy port succeeded")
	}
	tests := []struct {
		port int
		want string
	}{
		{port, "cannot serve on port " + strconv.Itoa(port) + " because another process is using it"},
		{0, "cannot serve on the default port 7824 because another process is using it; give --serve=PORT to serve on another port"},
	}
	for _, tt := range tests {
		if got := listenFailure(tt.port, err); got != tt.want {
			t.Errorf("listenFailure(%d) = %q, want %q", tt.port, got, tt.want)
		}
	}
}
