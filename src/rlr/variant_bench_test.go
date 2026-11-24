// This file measures each variant of an array function beside the base function
// that it replaces, over arrays of 1,024 elements.
package rlr

import (
	"strconv"
	"testing"
)

// benchInts returns 1,024 Int values in no order.
func benchInts() []int64 {
	xs := make([]int64, 1024)
	v := int64(7)
	for i := range xs {
		v = (v*1103515245 + 12345) % 2147483648
		xs[i] = v
	}
	return xs
}

// benchStrings returns the decimal texts of the values of benchInts.
func benchStrings() []string {
	xs := make([]string, 1024)
	for i, x := range benchInts() {
		xs[i] = strconv.FormatInt(x, 10)
	}
	return xs
}

// benchVariant times a base function and the variant that replaces it.
func benchVariant(b *testing.B, name string, base, variant func()) {
	b.Helper()
	for _, form := range []struct {
		name string
		fn   func()
	}{{"base", base}, {"variant", variant}} {
		b.Run(name+"/"+form.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				form.fn()
			}
		})
	}
}

func BenchmarkBlockCopy(b *testing.B) {
	xs := benchInts()
	same := Same[int64]
	benchVariant(b, "append", func() { Append(xs, 1, same) }, func() { AppendBlock(xs, 1) })
	benchVariant(b, "insert", func() { Insert(xs, 512, 1, same) }, func() { InsertBlock(xs, 512, 1) })
	benchVariant(b, "removeAt", func() { RemoveAt(xs, 512, same) }, func() { RemoveAtBlock(xs, 512) })
	benchVariant(b, "concat", func() { Concat(xs, xs, same) }, func() { ConcatBlock(xs, xs) })
	benchVariant(b, "slice", func() { SliceDeep(xs, 0, 1024, same) }, func() { Slice(xs, 0, 1024) })
	spare := make([]int64, 0, 128)
	grow := func(clone func(int64) int64) func() {
		return func() {
			out := spare
			for i := range int64(64) {
				out, _ = AppendInPlace(out, i, clone)
			}
		}
	}
	benchVariant(b, "append in place, 64 calls", grow(same), grow(nil))
}

func BenchmarkUnstableSort(b *testing.B) {
	xs, texts := benchInts(), benchStrings()
	benchVariant(b, "Int", func() { Sorted(xs, Less[int64], Same[int64]) }, func() { SortedUnstable(xs, Less[int64], Same[int64]) })
	benchVariant(b, "String", func() { Sorted(texts, Less[string], Same[string]) }, func() { SortedUnstable(texts, Less[string], Same[string]) })
}
