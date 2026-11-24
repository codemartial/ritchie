package rlr

import (
	"math"
	"slices"
	"testing"
)

// collect ranges over a progression as a generated loop does.
func collect[T Integer](t *testing.T, lo, hi, step T) []T {
	t.Helper()
	var out []T
	for cur := range Progression(lo, hi, step) {
		out = append(out, cur)
		if len(out) > 300 {
			t.Fatalf("range(%d, %d, %d) supplied more than 300 values", lo, hi, step)
		}
	}
	return out
}

func TestRangeProgressions(t *testing.T) {
	tests := []struct {
		name         string
		lo, hi, step int64
		want         []int64
	}{
		{"ascending span", 1, 4, 1, []int64{1, 2, 3}},
		{"equal bounds", 4, 4, 1, nil},
		{"lo above hi with a positive step", 5, 1, 1, nil},
		{"positive step", 1, 8, 3, []int64{1, 4, 7}},
		{"positive step landing on the bound", 1, 7, 3, []int64{1, 4}},
		{"negative step", 7, 0, -3, []int64{7, 4, 1}},
		{"negative step landing on the bound", 6, 0, -3, []int64{6, 3}},
		{"zero step", 1, 4, 0, nil},
		{"negative step below hi", 1, 4, -1, nil},
		{"equal bounds with a negative step", 4, 4, -1, nil},
		{"descending span", 3, 0, -1, []int64{3, 2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collect(t, tt.lo, tt.hi, tt.step); !slices.Equal(got, tt.want) {
				t.Errorf("range(%d, %d, %d) = %v, want %v", tt.lo, tt.hi, tt.step, got, tt.want)
			}
		})
	}
}

func TestRangeTerminatesAtEveryInt8Boundary(t *testing.T) {
	tests := []struct {
		name         string
		lo, hi, step int8
		want         []int8
	}{
		{"up to the maximum", 126, 127, 1, []int8{126}},
		{"maximum bound", 125, 127, 1, []int8{125, 126}},
		{"step past the maximum", 120, 127, 5, []int8{120, 125}},
		{"step landing on the maximum", 117, 127, 5, []int8{117, 122}},
		{"down to the minimum", -127, -128, -1, []int8{-127}},
		{"step past the minimum", -120, -128, -5, []int8{-120, -125}},
		{"maximum step across the width", -128, 127, 127, []int8{-128, -1, 126}},
		{"minimum step across the width", 127, -128, -128, []int8{127, -1}},
		{"minimum step whose successor equals the bound", 127, -1, -128, []int8{127}},
		{"maximum step whose successor equals the bound", -128, -1, 127, []int8{-128}},
		{"from the minimum with the maximum step", -128, 126, 127, []int8{-128, -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collect(t, tt.lo, tt.hi, tt.step); !slices.Equal(got, tt.want) {
				t.Errorf("range(%d, %d, %d) = %v, want %v", tt.lo, tt.hi, tt.step, got, tt.want)
			}
		})
	}
	t.Run("whole width descending", func(t *testing.T) {
		got := collect(t, int8(127), int8(-128), int8(-1))
		if len(got) != 255 || got[0] != 127 || got[254] != -127 {
			t.Errorf("range(127, -128, -1) supplied %d values from %d to %d, want 255 from 127 to -127", len(got), got[0], got[len(got)-1])
		}
	})
	t.Run("whole width ascending", func(t *testing.T) {
		got := collect(t, int8(-128), int8(127), int8(1))
		if len(got) != 255 || got[0] != -128 || got[254] != 126 {
			t.Errorf("range(-128, 127, 1) supplied %d values, want 255 from -128 to 126", len(got))
		}
	})
}

func TestRangeTerminatesAtEveryWidthExtreme(t *testing.T) {
	if got := collect(t, int16(math.MaxInt16-3), int16(math.MaxInt16), int16(2)); !slices.Equal(got, []int16{math.MaxInt16 - 3, math.MaxInt16 - 1}) {
		t.Errorf("Int16 ascending extreme = %v", got)
	}
	if got := collect(t, int16(math.MinInt16+3), int16(math.MinInt16), int16(-2)); !slices.Equal(got, []int16{math.MinInt16 + 3, math.MinInt16 + 1}) {
		t.Errorf("Int16 descending extreme = %v", got)
	}
	if got := collect(t, int32(math.MaxInt32-1), int32(math.MaxInt32), int32(1)); !slices.Equal(got, []int32{math.MaxInt32 - 1}) {
		t.Errorf("Int32 ascending extreme = %v", got)
	}
	if got := collect(t, int32(math.MinInt32), int32(math.MaxInt32), int32(math.MaxInt32)); !slices.Equal(got, []int32{math.MinInt32, -1, math.MaxInt32 - 1}) {
		t.Errorf("Int32 maximum step = %v", got)
	}
	if got := collect(t, int64(math.MaxInt64-1), int64(math.MaxInt64), int64(1)); !slices.Equal(got, []int64{math.MaxInt64 - 1}) {
		t.Errorf("Int64 ascending extreme = %v", got)
	}
	if got := collect(t, int64(math.MinInt64+1), int64(math.MinInt64), int64(-1)); !slices.Equal(got, []int64{math.MinInt64 + 1}) {
		t.Errorf("Int64 descending extreme = %v", got)
	}
	if got := collect(t, int64(math.MinInt64), int64(math.MaxInt64), int64(math.MaxInt64)); !slices.Equal(got, []int64{math.MinInt64, -1, math.MaxInt64 - 1}) {
		t.Errorf("Int64 maximum step = %v", got)
	}
	if got := collect(t, int64(math.MaxInt64), int64(math.MinInt64), int64(math.MinInt64)); !slices.Equal(got, []int64{math.MaxInt64, -1}) {
		t.Errorf("Int64 minimum step = %v", got)
	}
	if got := collect(t, int64(math.MaxInt64), int64(-1), int64(math.MinInt64)); !slices.Equal(got, []int64{math.MaxInt64}) {
		t.Errorf("Int64 minimum step whose successor equals the bound = %v", got)
	}
}
