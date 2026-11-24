package rlr

import (
	"iter"
	"math"
	"strconv"
)

// Integer is the set of Ritchie integer representations.
type Integer interface {
	~int8 | ~int16 | ~int32 | ~int64
}

// Div is integer division.
func Div[T Integer](a, b T) (T, Fault) {
	if b == 0 {
		return 0, DivisionByZero
	}
	return a / b, NoFault
}

// Rem is the integer remainder.
func Rem[T Integer](a, b T) (T, Fault) {
	if b == 0 {
		return 0, DivisionByZero
	}
	return a % b, NoFault
}

// RemFloat is the Float truncating remainder.
func RemFloat(a, b float64) float64 {
	return math.Mod(a, b)
}

// Upto is the integer range from zero toward end with step one, which excludes end.
func Upto[T Integer](end T) iter.Seq[T] { return Progression(0, end, 1) }

// Between is the integer range from lo toward hi with step one, which excludes hi.
func Between[T Integer](lo, hi T) iter.Seq[T] { return Progression(lo, hi, 1) }

// Progression is the integer range lo, lo + step, lo + 2*step, and so on, which supplies a value while a positive step keeps it below hi or a negative step keeps it above hi, so that a zero step or a step away from hi supplies none, and which does not compute the successor of the last value in T, so that the range neither wraps nor overflows.
func Progression[T Integer](lo, hi, step T) iter.Seq[T] {
	return func(yield func(T) bool) {
		if !(step > 0 && lo < hi || step < 0 && lo > hi) {
			return
		}
		cur := lo
		for yield(cur) {
			var more bool
			if cur, more = advance(cur, hi, step); !more {
				return
			}
		}
	}
}

// advance returns the value after cur in a progression toward hi and whether the range supplies it, comparing the distance to hi with the step in unsigned 64-bit arithmetic.
func advance[T Integer](cur, hi, step T) (T, bool) {
	if step > 0 {
		remaining := uint64(int64(hi)) - uint64(int64(cur))
		if remaining > uint64(int64(step)) {
			return cur + step, true
		}
		return cur, false
	}
	remaining := uint64(int64(cur)) - uint64(int64(hi))
	if remaining > uint64(-int64(step)) {
		return cur + step, true
	}
	return cur, false
}

// Abs is the integer absolute value under the type's negation.
func Abs[T Integer](x T) T {
	if x < 0 {
		return -x
	}
	return x
}

// AbsFloat clears the sign of a Float.
func AbsFloat(x float64) float64 {
	return math.Float64frombits(math.Float64bits(x) &^ (1 << 63))
}

// Ordered is the set of Ritchie value representations Go orders with its own operators.
type Ordered interface {
	Integer | ~uint8 | ~string
}

// Min is the least of one or more ordered values.
func Min[T Ordered](xs ...T) T {
	best := xs[0]
	for _, x := range xs[1:] {
		if x < best {
			best = x
		}
	}
	return best
}

// Max is the greatest of one or more ordered values.
func Max[T Ordered](xs ...T) T {
	best := xs[0]
	for _, x := range xs[1:] {
		if x > best {
			best = x
		}
	}
	return best
}

// MinFloat is the least of one or more Floats.
func MinFloat(xs ...float64) float64 {
	best := xs[0]
	for _, x := range xs[1:] {
		switch {
		case best != best:
		case x != x, x < best:
			best = x
		case x == best && math.Signbit(x):
			best = x
		}
	}
	return best
}

// MaxFloat is the greatest of one or more Floats.
func MaxFloat(xs ...float64) float64 {
	best := xs[0]
	for _, x := range xs[1:] {
		switch {
		case best != best:
		case x != x, x > best:
			best = x
		case x == best && !math.Signbit(x):
			best = x
		}
	}
	return best
}

// ByteFromInt converts an integer to Byte.
func ByteFromInt(i int64) (uint8, Fault) {
	if i < 0 || i > 255 {
		return 0, OutOfRange
	}
	return uint8(i), NoFault
}

// IntFromByte converts a Byte to the corresponding Int.
func IntFromByte(b uint8) int64 {
	return int64(b)
}

// Less is the natural ascending order of the values Go orders with its own operator.
func Less[T Ordered](a, b T) bool { return a < b }

// LessFloat is the natural sort order of Float.
func LessFloat(a, b float64) bool {
	if a != a {
		return b == b
	}
	return b == b && a < b
}

// addLength is the checked total of two non-negative byte counts.
func addLength(total, n int64) (int64, Fault) {
	if n < 0 || total < 0 || total > math.MaxInt64-n {
		return 0, OutOfRange
	}
	return total + n, NoFault
}

// mulLength is the checked product of two non-negative counts.
func mulLength(a, b int64) (int64, Fault) {
	if a < 0 || b < 0 {
		return 0, OutOfRange
	}
	if a == 0 || b == 0 {
		return 0, NoFault
	}
	if a > math.MaxInt64/b {
		return 0, OutOfRange
	}
	return a * b, NoFault
}

// IntFromFloat converts a Float to Int by truncation toward zero, raising OutOfRange! for a value outside Int and for NaN.
func IntFromFloat(f float64) (int64, Fault) {
	t := math.Trunc(f)
	if !(t >= math.MinInt64 && t < math.MaxInt64) {
		return 0, OutOfRange
	}
	return int64(t), NoFault
}

// IntFromString parses decimal integer text with an optional sign, raising Malformed! for text that is not such a numeral or lies outside Int.
func IntFromString(s string) (int64, Fault) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, Malformed
	}
	return n, NoFault
}

// narrow converts an Int to a narrower integer type, raising OutOfRange! for a value the type does not hold.
func narrow[T Integer](i int64) (T, Fault) {
	if int64(T(i)) != i {
		return 0, OutOfRange
	}
	return T(i), NoFault
}

// Int8FromInt converts an Int to Int8.
func Int8FromInt(i int64) (int8, Fault) { return narrow[int8](i) }

// Int16FromInt converts an Int to Int16.
func Int16FromInt(i int64) (int16, Fault) { return narrow[int16](i) }

// Int32FromInt converts an Int to Int32.
func Int32FromInt(i int64) (int32, Fault) { return narrow[int32](i) }

// FloatFromInt converts an Int to the nearest Float, with ties to even.
func FloatFromInt(i int64) float64 { return float64(i) }
