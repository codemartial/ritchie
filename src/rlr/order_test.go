package rlr

import (
	"math"
	"slices"
	"testing"
)

func TestLessFloatPlacesNaNsFirstAndTreatsSignedZerosAsEqual(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		a, b float64
		want bool
	}{
		{nan, 1, true}, {1, nan, false}, {nan, nan, false},
		{nan, math.Inf(-1), true}, {math.Inf(-1), nan, false},
		{-1, 1, true}, {1, -1, false}, {2, 2, false},
		{math.Copysign(0, -1), 0, false}, {0, math.Copysign(0, -1), false},
		{math.Inf(-1), math.Inf(1), true},
	}
	for _, tt := range tests {
		if got := LessFloat(tt.a, tt.b); got != tt.want {
			t.Errorf("LessFloat(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSortedIsFreshStableAndAscending(t *testing.T) {
	xs := []int64{3, 1, 2, 1}
	out := Sorted(xs, Less[int64], Same[int64])
	if !slices.Equal(out, []int64{1, 1, 2, 3}) || !slices.Equal(xs, []int64{3, 1, 2, 1}) {
		t.Errorf("Sorted(%v) = %v and left %v", []int64{3, 1, 2, 1}, out, xs)
	}
	out[0] = 9
	if xs[1] != 1 {
		t.Error("Sorted shares storage with its input")
	}
	if got := Sorted([]int64{}, Less[int64], Same[int64]); got == nil || len(got) != 0 {
		t.Errorf("Sorted of an empty array = %v, want an empty non-nil array", got)
	}
	words := Sorted([]string{"b", "B", "a", ""}, Less[string], Same[string])
	if !slices.Equal(words, []string{"", "B", "a", "b"}) {
		t.Errorf("Sorted strings = %q, want bytewise order", words)
	}
	bytes := Sorted([][]byte{{2}, {1, 1}, {1}}, LessBytes, Clone[uint8])
	if len(bytes) != 3 || !slices.Equal(bytes[0], []byte{1}) || !slices.Equal(bytes[1], []byte{1, 1}) || !slices.Equal(bytes[2], []byte{2}) {
		t.Errorf("Sorted byte arrays = %v, want lexicographic order", bytes)
	}

	nanA, nanB := math.Float64frombits(0x7ff8000000000001), math.Float64frombits(0x7ff8000000000002)
	negZero := math.Copysign(0, -1)
	fs := Sorted([]float64{1, nanA, negZero, math.Inf(1), 0, nanB, -1, negZero}, LessFloat, Same[float64])
	bits := make([]uint64, len(fs))
	for i, f := range fs {
		bits[i] = math.Float64bits(f)
	}
	want := []uint64{math.Float64bits(nanA), math.Float64bits(nanB), math.Float64bits(-1),
		math.Float64bits(negZero), math.Float64bits(0), math.Float64bits(negZero), math.Float64bits(1), math.Float64bits(math.Inf(1))}
	if !slices.Equal(bits, want) {
		t.Errorf("Sorted floats = %v, want NaNs first, then numeric order with the zeros in input order", fs)
	}
}

func TestFillIsCheckedAndIndependent(t *testing.T) {
	out := must(Fill(int64(7), 3, Same[int64]))
	if !slices.Equal(out, []int64{7, 7, 7}) {
		t.Errorf("Fill(7, 3) = %v", out)
	}
	if got := must(Fill(int64(7), 0, Same[int64])); got == nil || len(got) != 0 {
		t.Errorf("Fill(7, 0) = %v, want an empty non-nil array", got)
	}
	if err := second(Fill(int64(7), -1, Same[int64])); err != OutOfRange {
		t.Errorf("Fill(7, -1) raised %v, want OutOfRange!", err)
	}
	row := []int64{1}
	grid := must(Fill(row, 2, Clone[int64]))
	grid[0][0] = 9
	row[0] = 5
	if grid[1][0] != 1 || grid[0][0] != 9 {
		t.Errorf("Fill shares element storage: %v", grid)
	}
}
