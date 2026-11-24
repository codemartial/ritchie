package rlr

import (
	"slices"
)

// Clone is an independent copy of an array value.
func Clone[T any](xs []T) []T {
	out := make([]T, len(xs))
	copy(out, xs)
	return out
}

// Length is the element count of an array as an Int.
func Length[T any](xs []T) int64 {
	return int64(len(xs))
}

// Slice is the half-open element slice xs[lo.
func Slice[T any](xs []T, lo, hi int64) ([]T, Fault) {
	if f := checkBounds(int64(len(xs)), lo, hi); f != NoFault {
		return nil, f
	}
	return Clone(xs[lo:hi]), NoFault
}

// SliceDeep is Slice over elements that need clone to become independent.
func SliceDeep[T any](xs []T, lo, hi int64, clone func(T) T) ([]T, Fault) {
	if f := checkBounds(int64(len(xs)), lo, hi); f != NoFault {
		return nil, f
	}
	return CloneDeep(xs[lo:hi], clone), NoFault
}

// CloneDeep is an independent copy of an array whose elements need clone to become independent.
func CloneDeep[T any](xs []T, clone func(T) T) []T {
	out := make([]T, len(xs))
	for i, x := range xs {
		out[i] = clone(x)
	}
	return out
}

// CheckIndex validates position i over the array.
func CheckIndex[T any](xs []T, i int64) Fault {
	if i < 0 || i >= int64(len(xs)) {
		return OutOfRange
	}
	return NoFault
}

// index is the element at position i.
func index[T any](xs []T, i int64) (T, Fault) {
	if f := CheckIndex(xs, i); f != NoFault {
		var zero T
		return zero, f
	}
	return xs[i], NoFault
}

// Same is the clone of a value Go copies whole.
func Same[T any](x T) T { return x }

// Equal is the equality of two values Go compares with its own operator.
func Equal[T comparable](a, b T) bool { return a == b }

// EqualSlices reports elementwise equality of two arrays under eq.
func EqualSlices[T any](a, b []T, eq func(T, T) bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !eq(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Contains reports whether v occurs in xs under the element equality eq.
func Contains[T any](xs []T, v T, eq func(T, T) bool) bool {
	for _, x := range xs {
		if eq(x, v) {
			return true
		}
	}
	return false
}

// IndexOf is the lowest index at which v occurs in xs under eq.
func IndexOf[T any](xs []T, v T, eq func(T, T) bool) Option[int64] {
	for i, x := range xs {
		if eq(x, v) {
			return Some(int64(i))
		}
	}
	return None[int64]()
}

// First is the independent copy of the first element.
func First[T any](xs []T, clone func(T) T) Option[T] {
	if len(xs) == 0 {
		return None[T]()
	}
	return Some(clone(xs[0]))
}

// Last is the independent copy of the last element.
func Last[T any](xs []T, clone func(T) T) Option[T] {
	if len(xs) == 0 {
		return None[T]()
	}
	return Some(clone(xs[len(xs)-1]))
}

// FirstFlat is the independent copy of the first element of an array of nullable elements.
func FirstFlat[T any](xs []Option[T], clone func(Option[T]) Option[T]) Option[T] {
	if len(xs) == 0 {
		return None[T]()
	}
	return clone(xs[0])
}

// LastFlat is the independent copy of the last element of an array of nullable elements.
func LastFlat[T any](xs []Option[T], clone func(Option[T]) Option[T]) Option[T] {
	if len(xs) == 0 {
		return None[T]()
	}
	return clone(xs[len(xs)-1])
}

// Append is the fresh dynamic array holding independent copies of xs followed by v.
func Append[T any](xs []T, v T, clone func(T) T) ([]T, Fault) {
	n, f := addLength(int64(len(xs)), 1)
	if f != NoFault {
		return nil, f
	}
	out := make([]T, 0, n)
	for _, x := range xs {
		out = append(out, clone(x))
	}
	return append(out, clone(v)), NoFault
}

// AppendBlock is Append over elements that share no storage, which it copies as one block.
func AppendBlock[T any](xs []T, v T) ([]T, Fault) {
	n, f := addLength(int64(len(xs)), 1)
	if f != NoFault {
		return nil, f
	}
	out := make([]T, n)
	copy(out, xs)
	out[len(xs)] = v
	return out, NoFault
}

// Insert is the fresh dynamic array in which v occupies position i.
func Insert[T any](xs []T, i int64, v T, clone func(T) T) ([]T, Fault) {
	if i < 0 || i > int64(len(xs)) {
		return nil, OutOfRange
	}
	n, f := addLength(int64(len(xs)), 1)
	if f != NoFault {
		return nil, f
	}
	out := make([]T, 0, n)
	for _, x := range xs[:i] {
		out = append(out, clone(x))
	}
	out = append(out, clone(v))
	for _, x := range xs[i:] {
		out = append(out, clone(x))
	}
	return out, NoFault
}

// InsertBlock is Insert over elements that share no storage, which it copies as two blocks.
func InsertBlock[T any](xs []T, i int64, v T) ([]T, Fault) {
	if i < 0 || i > int64(len(xs)) {
		return nil, OutOfRange
	}
	n, f := addLength(int64(len(xs)), 1)
	if f != NoFault {
		return nil, f
	}
	out := make([]T, n)
	copy(out, xs[:i])
	out[i] = v
	copy(out[i+1:], xs[i:])
	return out, NoFault
}

// RemoveAt is the fresh dynamic array holding the elements before i followed by the elements after it.
func RemoveAt[T any](xs []T, i int64, clone func(T) T) ([]T, Fault) {
	if f := CheckIndex(xs, i); f != NoFault {
		return nil, f
	}
	out := make([]T, 0, len(xs)-1)
	for _, x := range xs[:i] {
		out = append(out, clone(x))
	}
	for _, x := range xs[i+1:] {
		out = append(out, clone(x))
	}
	return out, NoFault
}

// RemoveAtBlock is RemoveAt over elements that share no storage, which it copies as two blocks.
func RemoveAtBlock[T any](xs []T, i int64) ([]T, Fault) {
	if f := CheckIndex(xs, i); f != NoFault {
		return nil, f
	}
	out := make([]T, len(xs)-1)
	copy(out, xs[:i])
	copy(out[i:], xs[i+1:])
	return out, NoFault
}

// Concat is the fresh dynamic array holding independent copies of xs followed by those of ys.
func Concat[T any](xs, ys []T, clone func(T) T) ([]T, Fault) {
	n, f := addLength(int64(len(xs)), int64(len(ys)))
	if f != NoFault {
		return nil, f
	}
	out := make([]T, 0, n)
	for _, x := range xs {
		out = append(out, clone(x))
	}
	for _, y := range ys {
		out = append(out, clone(y))
	}
	return out, NoFault
}

// ConcatBlock is Concat over elements that share no storage, which it copies as two blocks.
func ConcatBlock[T any](xs, ys []T) ([]T, Fault) {
	n, f := addLength(int64(len(xs)), int64(len(ys)))
	if f != NoFault {
		return nil, f
	}
	out := make([]T, n)
	copy(out, xs)
	copy(out[len(xs):], ys)
	return out, NoFault
}

// Reverse is the fresh array holding independent copies of xs in the opposite order.
func Reverse[T any](xs []T, clone func(T) T) []T {
	out := make([]T, len(xs))
	for i, x := range xs {
		out[len(xs)-1-i] = clone(x)
	}
	return out
}

// Sorted is the fresh array holding independent copies of xs in stable ascending order under less.
func Sorted[T any](xs []T, less func(a, b T) bool, clone func(T) T) []T {
	out := CloneDeep(xs, clone)
	slices.SortStableFunc(out, comparison(less))
	return out
}

// SortedUnstable is Sorted under a natural order less that treats only identical values as equal, so that the order of equal elements shows in no result and the sort need not keep it.
func SortedUnstable[T any](xs []T, less func(a, b T) bool, clone func(T) T) []T {
	out := CloneDeep(xs, clone)
	if !sortOrdered(out) {
		slices.SortFunc(out, comparison(less))
	}
	return out
}

// sortOrdered sorts xs in place by Go's own order and reports true when Go's order of the element type is the natural order of the Ritchie type that the element type represents.
func sortOrdered[T any](xs []T) bool {
	switch xs := any(xs).(type) {
	case []int8:
		slices.Sort(xs)
	case []int16:
		slices.Sort(xs)
	case []int32:
		slices.Sort(xs)
	case []int64:
		slices.Sort(xs)
	case []uint8:
		slices.Sort(xs)
	case []string:
		slices.Sort(xs)
	default:
		return false
	}
	return true
}

// comparison is the three-way comparison of the strict order less.
func comparison[T any](less func(a, b T) bool) func(a, b T) int {
	return func(a, b T) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	}
}

// Fill is the fresh dynamic array holding n independent copies of v.
func Fill[T any](v T, n int64, clone func(T) T) ([]T, Fault) {
	if n < 0 {
		return nil, OutOfRange
	}
	out := make([]T, n)
	for i := range out {
		out[i] = clone(v)
	}
	return out, NoFault
}

// cloned is the independent copy of one element under clone, which is nil for an element type that shares no storage.
func cloned[T any](v T, clone func(T) T) T {
	if clone == nil {
		return v
	}
	return clone(v)
}

// AppendInPlace is xs extended by an independent copy of v in the storage of xs, which the caller reads no more.
func AppendInPlace[T any](xs []T, v T, clone func(T) T) ([]T, Fault) {
	if _, f := addLength(int64(len(xs)), 1); f != NoFault {
		return xs, f
	}
	return append(xs, cloned(v, clone)), NoFault
}

// InsertInPlace is xs with an independent copy of v at position i and the elements from i on moved one position higher, in the storage of xs, which the caller reads no more.
func InsertInPlace[T any](xs []T, i int64, v T, clone func(T) T) ([]T, Fault) {
	if i < 0 || i > int64(len(xs)) {
		return xs, OutOfRange
	}
	if _, f := addLength(int64(len(xs)), 1); f != NoFault {
		return xs, f
	}
	return slices.Insert(xs, int(i), cloned(v, clone)), NoFault
}

// RemoveAtInPlace is xs without the element at i, in the storage of xs, which the caller reads no more.
func RemoveAtInPlace[T any](xs []T, i int64) ([]T, Fault) {
	if f := CheckIndex(xs, i); f != NoFault {
		return xs, f
	}
	return slices.Delete(xs, int(i), int(i)+1), NoFault
}

// ConcatInPlace is xs extended by independent copies of the elements of ys in the storage of xs, which the caller reads no more.
func ConcatInPlace[T any](xs, ys []T, clone func(T) T) ([]T, Fault) {
	if _, f := addLength(int64(len(xs)), int64(len(ys))); f != NoFault {
		return xs, f
	}
	if clone == nil {
		return append(xs, ys...), NoFault
	}
	xs = slices.Grow(xs, len(ys))
	for _, y := range ys {
		xs = append(xs, clone(y))
	}
	return xs, NoFault
}

// ReverseInPlace is xs with its elements in the opposite order, in the storage of xs, which the caller reads no more.
func ReverseInPlace[T any](xs []T) []T {
	slices.Reverse(xs)
	return xs
}

// SliceInPlace is the half-open element slice xs[lo:hi] in the storage of xs, which the caller reads no more.
func SliceInPlace[T any](xs []T, lo, hi int64) ([]T, Fault) {
	if f := checkBounds(int64(len(xs)), lo, hi); f != NoFault {
		return xs, f
	}
	return xs[lo:hi], NoFault
}

// SortInPlace is xs in stable ascending order under less, in the storage of xs, which the caller reads no more.
func SortInPlace[T any](xs []T, less func(a, b T) bool) []T {
	slices.SortStableFunc(xs, comparison(less))
	return xs
}

// SortUnstableInPlace is SortInPlace under a natural order less that treats only identical values as equal.
func SortUnstableInPlace[T any](xs []T, less func(a, b T) bool) []T {
	if !sortOrdered(xs) {
		slices.SortFunc(xs, comparison(less))
	}
	return xs
}
