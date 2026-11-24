// This file is the comparison of the temporal types.
package rlr

import "cmp"

// CompareDate is the civil order of two Dates.
func CompareDate(a, b Date) int { return cmp.Compare(a.day, b.day) }

// EqualDate reports whether a and b are one civil date.
func EqualDate(a, b Date) bool { return a.day == b.day }

// LessDate is the ascending civil order.
func LessDate(a, b Date) bool { return a.day < b.day }

// CompareTime is the order of two Times within the civil day.
func CompareTime(a, b Time) int { return cmp.Compare(a.ns, b.ns) }

// EqualTime reports whether a and b are one nanosecond count.
func EqualTime(a, b Time) bool { return a.ns == b.ns }

// LessTime is the ascending order of Time.
func LessTime(a, b Time) bool { return a.ns < b.ns }

// CompareDateTime is the instant order of two DateTimes.
func CompareDateTime(a, b DateTime) int {
	if c := cmp.Compare(a.sec, b.sec); c != 0 {
		return c
	}
	return cmp.Compare(a.nsec, b.nsec)
}

// EqualDateTime reports whether a and b identify one instant.
func EqualDateTime(a, b DateTime) bool { return a.sec == b.sec && a.nsec == b.nsec }

// LessDateTime is the ascending instant order.
func LessDateTime(a, b DateTime) bool { return CompareDateTime(a, b) < 0 }

// DateTimeKey is the dictionary key projection of a DateTime.
type DateTimeKey struct {
	sec  int64
	nsec int32
}

// KeyOfDateTime is the key projection of dt.
func KeyOfDateTime(dt DateTime) DateTimeKey { return DateTimeKey{sec: dt.sec, nsec: dt.nsec} }

// DurationKey is the dictionary key projection of a Duration.
type DurationKey struct {
	neg bool
	mag u128
}

// KeyOfDuration is the key projection of d, its exact signed nanosecond span.
func KeyOfDuration(d Duration) DurationKey {
	neg, mag := nanos(d)
	return DurationKey{neg: neg, mag: mag}
}

// CompareDuration is the order of two Durations by exact physical span.
func CompareDuration(a, b Duration) int {
	aneg, amag := nanos(a)
	bneg, bmag := nanos(b)
	switch {
	case aneg && !bneg:
		return -1
	case bneg && !aneg:
		return 1
	case aneg:
		return cmp128(bmag, amag)
	}
	return cmp128(amag, bmag)
}

// EqualDuration reports whether a and b are one physical span.
func EqualDuration(a, b Duration) bool { return a.v == b.v }

// LessDuration is the ascending physical order.
func LessDuration(a, b Duration) bool { return CompareDuration(a, b) < 0 }

// MinUnder is the least of one or more values under less.
func MinUnder[T any](less func(a, b T) bool, xs ...T) T {
	best := xs[0]
	for _, x := range xs[1:] {
		if less(x, best) {
			best = x
		}
	}
	return best
}

// MaxUnder is the greatest of one or more values under less.
func MaxUnder[T any](less func(a, b T) bool, xs ...T) T {
	return MinUnder(func(a, b T) bool { return less(b, a) }, xs...)
}

// MinDate is the earliest of one or more dates.
func MinDate(xs ...Date) Date { return MinUnder(LessDate, xs...) }

// MaxDate is the latest of one or more dates.
func MaxDate(xs ...Date) Date { return MaxUnder(LessDate, xs...) }

// MinTime is the earliest of one or more times of day.
func MinTime(xs ...Time) Time { return MinUnder(LessTime, xs...) }

// MaxTime is the latest of one or more times of day.
func MaxTime(xs ...Time) Time { return MaxUnder(LessTime, xs...) }

// MinDateTime is the earliest of one or more instants.
func MinDateTime(xs ...DateTime) DateTime { return MinUnder(LessDateTime, xs...) }

// MaxDateTime is the latest of one or more instants.
func MaxDateTime(xs ...DateTime) DateTime { return MaxUnder(LessDateTime, xs...) }

// MinDuration is the shortest of one or more spans.
func MinDuration(xs ...Duration) Duration { return MinUnder(LessDuration, xs...) }

// MaxDuration is the longest of one or more spans.
func MaxDuration(xs ...Duration) Duration { return MaxUnder(LessDuration, xs...) }
