package rlr

import (
	"math"
	"testing"
)

func TestOptionZeroValueIsAbsent(t *testing.T) {
	var absent Option[int64]
	if absent.IsSome() || !absent.IsNone() || absent != None[int64]() {
		t.Errorf("the zero option is not absent: %+v", absent)
	}
	if v, ok := absent.Get(); ok || v != 0 {
		t.Errorf("absent.Get() = %d, %v", v, ok)
	}
	present := Some("x")
	if !present.IsSome() || present.IsNone() || present.Unwrap() != "x" {
		t.Errorf("present option = %+v", present)
	}
	if v, ok := present.Get(); !ok || v != "x" {
		t.Errorf("present.Get() = %q, %v", v, ok)
	}
	defer func() {
		if recover() == nil {
			t.Error("Unwrap on an absent option did not panic")
		}
	}()
	absent.Unwrap()
}

func TestSearchesTraverseFromZero(t *testing.T) {
	xs := []int64{3, 1, 3}
	if !Contains(xs, 3, Equal[int64]) || Contains(xs, 4, Equal[int64]) || Contains([]int64{}, 3, Equal[int64]) {
		t.Error("Contains disagrees with membership")
	}
	if got := IndexOf(xs, 3, Equal[int64]); got != Some[int64](0) {
		t.Errorf("IndexOf first match = %+v, want the lowest index", got)
	}
	if got := IndexOf(xs, 1, Equal[int64]); got != Some[int64](1) {
		t.Errorf("IndexOf middle = %+v", got)
	}
	if got := IndexOf(xs, 4, Equal[int64]); got.IsSome() {
		t.Errorf("IndexOf absent = %+v", got)
	}
	bs := [][]byte{[]byte("a"), []byte("b")}
	if got := IndexOf(bs, []byte("b"), EqualBytes); got != Some[int64](1) {
		t.Errorf("IndexOf over byte arrays = %+v", got)
	}
	if !Contains([]float64{math.NaN()}, math.NaN(), Equal[float64]) == false {
		t.Error("NaN is equal to itself under Go equality")
	}
}

func TestEndpointsAreIndependentOrAbsent(t *testing.T) {
	if First([]int64{}, Same[int64]).IsSome() || Last([]int64{}, Same[int64]).IsSome() {
		t.Error("an empty array has an endpoint")
	}
	nested := [][]int64{{1}, {2}}
	first := First(nested, Clone[int64]).Unwrap()
	last := Last(nested, Clone[int64]).Unwrap()
	first[0], last[0] = 9, 9
	if nested[0][0] != 1 || nested[1][0] != 2 {
		t.Errorf("endpoints share storage with the array: %v", nested)
	}
}

func TestTransformationsAreFreshAndOrdered(t *testing.T) {
	xs := []int64{1, 2, 3}
	appended := must(Append(xs, 4, Same[int64]))
	appended[0] = 9
	if xs[0] != 1 || len(appended) != 4 || appended[3] != 4 {
		t.Errorf("Append = %v from %v", appended, xs)
	}
	inserts := map[int64][]int64{0: {7, 1, 2, 3}, 1: {1, 7, 2, 3}, 3: {1, 2, 3, 7}}
	for i, want := range inserts {
		got := must(Insert(xs, i, 7, Same[int64]))
		if len(got) != len(want) {
			t.Errorf("Insert at %d = %v, want %v", i, got, want)
			continue
		}
		for j := range want {
			if got[j] != want[j] {
				t.Errorf("Insert at %d = %v, want %v", i, got, want)
				break
			}
		}
	}
	for _, i := range []int64{-1, 4, math.MinInt64, math.MaxInt64} {
		if err := second(Insert(xs, i, 7, Same[int64])); err != OutOfRange {
			t.Errorf("Insert at %d raised %v", i, err)
		}
	}
	removes := map[int64][]int64{0: {2, 3}, 1: {1, 3}, 2: {1, 2}}
	for i, want := range removes {
		got := must(RemoveAt(xs, i, Same[int64]))
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("RemoveAt %d = %v, want %v", i, got, want)
		}
	}
	for _, i := range []int64{-1, 3, math.MaxInt64} {
		if err := second(RemoveAt(xs, i, Same[int64])); err != OutOfRange {
			t.Errorf("RemoveAt %d raised %v", i, err)
		}
	}
	if got := must(RemoveAt([]int64{5}, 0, Same[int64])); got == nil || len(got) != 0 {
		t.Errorf("removing the only element = %v, want an empty non-nil array", got)
	}
	joined := must(Concat(xs, []int64{4, 5}, Same[int64]))
	if len(joined) != 5 || joined[0] != 1 || joined[4] != 5 {
		t.Errorf("Concat = %v", joined)
	}
	if got := must(Concat([]int64{}, []int64{}, Same[int64])); got == nil || len(got) != 0 {
		t.Errorf("Concat of empties = %v", got)
	}
	reversed := Reverse(xs, Same[int64])
	if len(reversed) != 3 || reversed[0] != 3 || reversed[2] != 1 || xs[0] != 1 {
		t.Errorf("Reverse = %v from %v", reversed, xs)
	}
	if got := Reverse([]int64{}, Same[int64]); got == nil || len(got) != 0 {
		t.Errorf("Reverse of an empty array = %v", got)
	}
	nested := [][]int64{{1}, {2}}
	rev := Reverse(nested, Clone[int64])
	rev[0][0] = 9
	if nested[1][0] != 2 {
		t.Error("Reverse shares element storage with its input")
	}
	app := must(Append(nested, []int64{3}, Clone[int64]))
	app[0][0] = 9
	if nested[0][0] != 1 {
		t.Error("Append shares element storage with its input")
	}
	if xs[0] != 1 || xs[1] != 2 || xs[2] != 3 {
		t.Errorf("a transformation changed its input: %v", xs)
	}
}

func TestResultCountsAreCheckedBeforeAllocation(t *testing.T) {
	if err := second(addLength(math.MaxInt64, 1)); err != OutOfRange {
		t.Errorf("a count past Int raised %v", err)
	}
	if err := second(addLength(math.MaxInt64-1, 1)); err != NoFault {
		t.Errorf("the maximum count raised %v", err)
	}
	if err := second(addLength(math.MaxInt64, math.MaxInt64)); err != OutOfRange {
		t.Errorf("a doubled maximum raised %v", err)
	}
}
