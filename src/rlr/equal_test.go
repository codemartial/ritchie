package rlr

import "testing"

func TestEqualSlicesComparesLengthThenElements(t *testing.T) {
	eqInt := func(a, b int64) bool { return a == b }
	tests := []struct {
		name string
		a, b []int64
		want bool
	}{
		{"two empty", nil, []int64{}, true},
		{"unequal lengths", []int64{1, 2}, []int64{1, 2, 3}, false},
		{"elementwise mismatch", []int64{1, 2, 3}, []int64{1, 9, 3}, false},
		{"equal", []int64{1, 2, 3}, []int64{1, 2, 3}, true},
	}
	for _, tt := range tests {
		if got := EqualSlices(tt.a, tt.b, eqInt); got != tt.want {
			t.Errorf("%s: EqualSlices = %v, want %v", tt.name, got, tt.want)
		}
	}
	eqNested := func(a, b []int64) bool { return EqualSlices(a, b, eqInt) }
	if !EqualSlices([][]int64{{1}, {2, 3}}, [][]int64{{1}, {2, 3}}, eqNested) {
		t.Error("equal nested slices compare unequal")
	}
	if EqualSlices([][]int64{{1}, {2, 3}}, [][]int64{{1}, {2}}, eqNested) {
		t.Error("nested slices with one shorter inner slice compare equal")
	}
}

func TestEqualDictsComparesKeySetsThenValues(t *testing.T) {
	eqInt := func(a, b int64) bool { return a == b }
	tests := []struct {
		name string
		a, b map[string]int64
		want bool
	}{
		{"two empty", nil, map[string]int64{}, true},
		{"unequal counts", map[string]int64{"a": 1}, map[string]int64{"a": 1, "b": 2}, false},
		{"different keys", map[string]int64{"a": 1}, map[string]int64{"b": 1}, false},
		{"different values", map[string]int64{"a": 1}, map[string]int64{"a": 2}, false},
		{"equal", map[string]int64{"a": 1, "b": 2}, map[string]int64{"b": 2, "a": 1}, true},
	}
	for _, tt := range tests {
		if got := EqualDicts(tt.a, tt.b, eqInt); got != tt.want {
			t.Errorf("%s: EqualDicts = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEqualErrorsComparesTypesThenPayloads(t *testing.T) {
	eqInt := func(a, b int64) bool { return a == b }
	a, b, c := NewError(FirstDeclared, int64(1)), NewError(FirstDeclared, int64(1)), NewError(FirstDeclared, int64(2))
	other := NewError(FirstDeclared+1, int64(1))
	if !EqualErrors(a, b, eqInt) || EqualErrors(a, c, eqInt) || EqualErrors(a, other, eqInt) {
		t.Error("EqualErrors does not compare the type and then the payload")
	}
}
