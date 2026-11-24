package rlr

import (
	"math"
	"testing"
)

// spare returns a buffer with spare capacity holding xs.
func spare(xs ...int64) []int64 {
	out := make([]int64, len(xs), 2*len(xs)+4)
	copy(out, xs)
	return out
}

func TestInPlaceFormsWorkInTheStorageOfTheirFirstArgumentWhileItsCapacityHoldsTheResult(t *testing.T) {
	xs := spare(1, 2, 3)
	grown := must(AppendInPlace(xs, 4, nil))
	if &grown[0] != &xs[0] || !equalInts(grown, []int64{1, 2, 3, 4}) {
		t.Errorf("AppendInPlace(%v, 4) = %v, want [1 2 3 4] over the same storage", xs, grown)
	}
	inserted := must(InsertInPlace(grown, 1, 9, nil))
	if want := []int64{1, 9, 2, 3, 4}; !equalInts(inserted, want) || &inserted[0] != &xs[0] {
		t.Errorf("InsertInPlace at 1 = %v, want %v over the same storage", inserted, want)
	}
	atEnd := must(InsertInPlace(inserted, 5, 7, nil))
	if want := []int64{1, 9, 2, 3, 4, 7}; !equalInts(atEnd, want) {
		t.Errorf("InsertInPlace at the count = %v, want %v", atEnd, want)
	}
	removed := must(RemoveAtInPlace(atEnd, 0))
	if want := []int64{9, 2, 3, 4, 7}; !equalInts(removed, want) || &removed[0] != &xs[0] {
		t.Errorf("RemoveAtInPlace at 0 = %v, want %v over the same storage", removed, want)
	}
	joined := must(ConcatInPlace(removed, []int64{8, 8}, nil))
	if want := []int64{9, 2, 3, 4, 7, 8, 8}; !equalInts(joined, want) || &joined[0] != &xs[0] {
		t.Errorf("ConcatInPlace = %v, want %v over the same storage", joined, want)
	}
	reversed := ReverseInPlace(joined)
	if want := []int64{8, 8, 7, 4, 3, 2, 9}; !equalInts(reversed, want) || &reversed[0] != &xs[0] {
		t.Errorf("ReverseInPlace = %v, want %v over the same storage", reversed, want)
	}
	if got := ReverseInPlace([]int64{}); len(got) != 0 {
		t.Errorf("ReverseInPlace of an empty array = %v", got)
	}
}

func TestInPlaceGrowthBeyondTheCapacityKeepsEveryElement(t *testing.T) {
	xs := []int64{1, 2, 3}
	grown := must(AppendInPlace(xs, 4, nil))
	grown = must(InsertInPlace(grown, 0, 0, nil))
	grown = must(ConcatInPlace(grown, []int64{5, 6, 7, 8, 9}, nil))
	if want := []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}; !equalInts(grown, want) {
		t.Errorf("growth in place from %v = %v, want %v", xs, grown, want)
	}
}

func TestInPlaceEnteringElementsAreCloned(t *testing.T) {
	row := []int64{1}
	grid := must(AppendInPlace([][]int64{}, row, Clone[int64]))
	grid = must(InsertInPlace(grid, 0, row, Clone[int64]))
	grid = must(ConcatInPlace(grid, [][]int64{row}, Clone[int64]))
	row[0] = 9
	for i, r := range grid {
		if r[0] != 1 {
			t.Errorf("element %d shares storage with its source: %v", i, grid)
		}
	}
}

func TestInPlaceFailuresLeaveTheBufferUnchanged(t *testing.T) {
	xs := spare(1, 2, 3)
	for _, i := range []int64{-1, 4, math.MinInt64, math.MaxInt64} {
		if err := second(InsertInPlace(xs, i, 9, nil)); err != OutOfRange {
			t.Errorf("InsertInPlace at %d raised %v, want OutOfRange!", i, err)
		}
	}
	for _, i := range []int64{-1, 3, math.MinInt64, math.MaxInt64} {
		if err := second(RemoveAtInPlace(xs, i)); err != OutOfRange {
			t.Errorf("RemoveAtInPlace at %d raised %v, want OutOfRange!", i, err)
		}
	}
	if !equalInts(xs, []int64{1, 2, 3}) || xs[:4][3] != 0 {
		t.Errorf("a failed in-place operation changed the buffer: %v", xs[:4])
	}
}

func TestRemoveAtInPlaceClearsTheVacatedSlot(t *testing.T) {
	grid := make([][]int64, 3, 8)
	copy(grid, [][]int64{{1}, {2}, {3}})
	shorter := must(RemoveAtInPlace(grid, 1))
	if len(shorter) != 2 || shorter[0][0] != 1 || shorter[1][0] != 3 {
		t.Errorf("RemoveAtInPlace at 1 = %v, want [[1] [3]]", shorter)
	}
	if grid[2] != nil {
		t.Errorf("the vacated slot still holds %v", grid[2])
	}
}

func TestSliceInPlaceKeepsTheStorageAndReportsBoundsOutsideTheArray(t *testing.T) {
	xs := []int64{1, 2, 3, 4}
	part := must(SliceInPlace(xs, 1, 3))
	if !equalInts(part, []int64{2, 3}) || &part[0] != &xs[1] {
		t.Errorf("SliceInPlace(%v, 1, 3) = %v, want [2 3] over the same storage", xs, part)
	}
	for _, b := range [][2]int64{{-1, 2}, {3, 2}, {0, 5}} {
		if err := second(SliceInPlace(xs, b[0], b[1])); err != OutOfRange {
			t.Errorf("SliceInPlace from %d to %d raised %v, want OutOfRange!", b[0], b[1], err)
		}
	}
}

func TestSortInPlaceSortsTheStorageAndKeepsTheOrderOfEqualElements(t *testing.T) {
	xs := []int64{31, 12, 30, 11}
	tens := func(a, b int64) bool { return a/10 < b/10 }
	if got := SortInPlace(xs, tens); !equalInts(got, []int64{12, 11, 31, 30}) || &got[0] != &xs[0] {
		t.Errorf("SortInPlace by tens = %v, want [12 11 31 30] over the same storage", got)
	}
	ys := []int64{3, 1, 2}
	if got := SortUnstableInPlace(ys, func(a, b int64) bool { return a < b }); !equalInts(got, []int64{1, 2, 3}) || &got[0] != &ys[0] {
		t.Errorf("SortUnstableInPlace = %v, want [1 2 3] over the same storage", got)
	}
	pairs := [][]int64{{2}, {1}}
	if got := SortUnstableInPlace(pairs, func(a, b []int64) bool { return a[0] < b[0] }); got[0][0] != 1 || got[1][0] != 2 {
		t.Errorf("SortUnstableInPlace of arrays = %v, want [[1] [2]]", got)
	}
}

func TestRemoveInPlaceDeletesTheEntryFromTheDictionaryItself(t *testing.T) {
	m := map[int64]string{1: "a", 2: "b"}
	if got := RemoveInPlace(m, 1); len(got) != 1 || got[2] != "b" || len(m) != 1 {
		t.Errorf("RemoveInPlace of key 1 = %v, want the same dictionary holding only key 2", got)
	}
	if got := RemoveInPlace(m, 7); len(got) != 1 {
		t.Errorf("RemoveInPlace of an absent key = %v, want the dictionary unchanged", got)
	}
	keyed := map[string]Keyed[int64, string]{"1": {Key: 1, Val: "a"}, "2": {Key: 2, Val: "b"}}
	if got := RemoveKeyedInPlace(keyed, "1"); len(got) != 1 || got["2"].Val != "b" || len(keyed) != 1 {
		t.Errorf("RemoveKeyedInPlace of the projection of key 1 = %v, want the same dictionary holding only key 2", got)
	}
}

// equalInts reports whether a and b hold the same values in order.
func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
