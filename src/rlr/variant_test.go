package rlr

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func TestEachBlockCopyIsAFreshArrayWithoutSpareCapacity(t *testing.T) {
	xs := []int64{1, 2, 3}
	for name, out := range map[string][]int64{
		"AppendBlock":   must(AppendBlock(xs, 4)),
		"InsertBlock":   must(InsertBlock(xs, 1, 9)),
		"RemoveAtBlock": must(RemoveAtBlock(xs, 1)),
		"ConcatBlock":   must(ConcatBlock(xs, xs)),
		"Slice":         must(Slice(xs, 0, 3)),
	} {
		if cap(out) != len(out) {
			t.Errorf("%s gave count %d with capacity %d, want a capacity equal to the count", name, len(out), cap(out))
		}
		out[0] = 7
		if xs[0] != 1 {
			t.Fatalf("%s shares storage with its input", name)
		}
	}
}

// sortsAlike checks that the unstable sort of 1,000 elements drawn from 64 values gives the result of the stable sort and leaves its input as it was.
func sortsAlike[T any](t *testing.T, name string, value func(int64) T, less func(a, b T) bool, clone func(T) T) {
	t.Helper()
	xs := make([]T, 1000)
	v := int64(7)
	for i := range xs {
		v = (v*1103515245 + 12345) % 2147483648
		xs[i] = value(v >> 8 % 64)
	}
	before := CloneDeep(xs, clone)
	got, want := SortedUnstable(xs, less, clone), Sorted(xs, less, clone)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the unstable sort of %s differs from the stable sort", name)
	}
	if !reflect.DeepEqual(xs, before) {
		t.Errorf("the unstable sort of %s changed its input", name)
	}
}

func TestTheUnstableSortGivesTheResultOfTheStableSortForEveryTypeOfAnIndistinguishableOrder(t *testing.T) {
	sortsAlike(t, "Int8", func(v int64) int8 { return int8(v - 32) }, Less[int8], Same[int8])
	sortsAlike(t, "Int16", func(v int64) int16 { return int16(v - 32) }, Less[int16], Same[int16])
	sortsAlike(t, "Int32", func(v int64) int32 { return int32(v - 32) }, Less[int32], Same[int32])
	sortsAlike(t, "Int", func(v int64) int64 { return v - 32 }, Less[int64], Same[int64])
	sortsAlike(t, "Byte", func(v int64) uint8 { return uint8(v * 4) }, Less[uint8], Same[uint8])
	sortsAlike(t, "String", func(v int64) string { return strconv.FormatInt(v, 10) }, Less[string], Same[string])
	sortsAlike(t, "Date", func(v int64) Date { return DateFromDays(v - 32) }, LessDate, Same[Date])
	sortsAlike(t, "Time", func(v int64) Time { return Time{ns: v * nanosPerSecond} }, LessTime, Same[Time])
	sortsAlike(t, "Duration", func(v int64) Duration { return mustDuration(t, fmt.Sprintf("%dm", v)) }, LessDuration, Same[Duration])
	sortsAlike(t, "Byte[]", func(v int64) []byte { return []byte{byte(v % 4), byte(v)}[:1+v%2] }, LessBytes, Clone[byte])
}
