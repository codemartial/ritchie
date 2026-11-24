package rlr

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// Sequence is the set of byte-addressed sequence representations.
type Sequence interface {
	~string | ~[]byte
}

// Count is the byte count of a sequence as an Int.
func Count[T Sequence](s T) int64 {
	return int64(len(s))
}

// IsEmpty reports whether a sequence has no bytes.
func IsEmpty[T Sequence](s T) bool {
	return len(s) == 0
}

// ContainsString reports whether part occurs in s.
func ContainsString(s, part string) bool { return strings.Contains(s, part) }

// ContainsBytes reports whether part occurs in b.
func ContainsBytes(b, part []byte) bool { return bytes.Contains(b, part) }

// StartsWithString reports whether s begins with prefix.
func StartsWithString(s, prefix string) bool { return strings.HasPrefix(s, prefix) }

// StartsWithBytes reports whether b begins with prefix.
func StartsWithBytes(b, prefix []byte) bool { return bytes.HasPrefix(b, prefix) }

// EndsWithString reports whether s ends with suffix.
func EndsWithString(s, suffix string) bool { return strings.HasSuffix(s, suffix) }

// EndsWithBytes reports whether b ends with suffix.
func EndsWithBytes(b, suffix []byte) bool { return bytes.HasSuffix(b, suffix) }

// IndexOfString is the byte position of the first occurrence of part in s and true.
func IndexOfString(s, part string) (int64, bool) { return found(strings.Index(s, part)) }

// IndexOfBytes is the byte position of the first occurrence of part in b and true.
func IndexOfBytes(b, part []byte) (int64, bool) { return found(bytes.Index(b, part)) }

// found converts a search result to the byte position and true or to zero and false when the search reported no position.
func found(i int) (int64, bool) {
	if i < 0 {
		return 0, false
	}
	return int64(i), true
}

// IsRuneBoundary reports whether byte position i of a String begins an encoded Unicode scalar or is the String's end.
func IsRuneBoundary(s string, i int64) bool {
	switch {
	case i < 0 || i > int64(len(s)):
		return false
	case i == int64(len(s)):
		return true
	}
	return s[i]&0xC0 != 0x80
}

// checkBounds validates a half-open range over n bytes.
func checkBounds(n, lo, hi int64) Fault {
	if lo < 0 || lo > hi || hi > n {
		return OutOfRange
	}
	return NoFault
}

// SliceString is the half-open byte slice s[lo.
func SliceString(s string, lo, hi int64) (string, Fault) {
	if f := checkBounds(int64(len(s)), lo, hi); f != NoFault {
		return "", f
	}
	if !IsRuneBoundary(s, lo) || !IsRuneBoundary(s, hi) {
		return "", InvalidStringBoundary
	}
	return s[lo:hi], NoFault
}

// StringFromBytes is the one entry through which bytes become a String.
func StringFromBytes(b []byte) (string, Fault) {
	if !utf8.Valid(b) {
		return "", Malformed
	}
	return string(b), NoFault
}

// BytesFromString is the UTF-8 encoding of a String as an independent byte array.
func BytesFromString(s string) []byte {
	return []byte(s)
}

// CompareBytes orders two byte arrays lexicographically by unsigned byte.
func CompareBytes(a, b []byte) int {
	return bytes.Compare(a, b)
}

// MinBytes is the least of one or more byte arrays under unsigned lexicographic order.
func MinBytes(xs ...[]byte) []byte {
	best := xs[0]
	for _, x := range xs[1:] {
		if CompareBytes(x, best) < 0 {
			best = x
		}
	}
	return Clone(best)
}

// MaxBytes is the greatest of one or more byte arrays under unsigned lexicographic order.
func MaxBytes(xs ...[]byte) []byte {
	best := xs[0]
	for _, x := range xs[1:] {
		if CompareBytes(x, best) > 0 {
			best = x
		}
	}
	return Clone(best)
}

// LessBytes is the natural ascending order of dynamic byte arrays.
func LessBytes(a, b []byte) bool { return bytes.Compare(a, b) < 0 }

// EqualBytes is the equality of two dynamic byte arrays.
func EqualBytes(a, b []byte) bool { return bytes.Equal(a, b) }
