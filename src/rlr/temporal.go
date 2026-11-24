// This file is the temporal kernel.
package rlr

import (
	"math/big"
	"math/bits"
)

// Date is a proleptic-Gregorian civil date.
type Date struct{ day int32 }

const (
	MinYear = -999_999
	MaxYear = 999_999
)

var (
	minDay = daysFromCivil(MinYear, 1, 1)
	maxDay = daysFromCivil(MaxYear, 12, 31)
)

// Time is an exact nanosecond count since the start of a civil day.
type Time struct{ ns int64 }

const NanosPerDay = 86_400 * nanosPerSecond

const (
	nanosPerSecond = 1_000_000_000
	nanosPerMilli  = 1_000_000
	secondsPerDay  = 86_400
)

// DateTime is an instant.
type DateTime struct {
	sec    int64
	nsec   int32
	offset int16
}

const (
	MinDateTimeYear = 0
	MaxDateTimeYear = 9999
	MaxOffset       = 14 * 60
)

// Duration represents a signed physical span.
type Duration struct{ v int64 }

const (
	FineMaxNanos    = 4_611_686_018_426_999_999
	CoarseMinMillis = 4_611_686_018_427
	CoarseMaxMillis = 4_611_686_018_427_387_903
)

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// daysFromCivil is the day count of a valid civil date.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// civilFromDays is the civil date of a day count.
func civilFromDays(z int64) (y, m, d int64) {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = doy - (153*mp+2)/5 + 1
	m = mp + 3
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

// IsLeapYear reports the Gregorian leap rule for an astronomical year.
func IsLeapYear(y int64) bool {
	return y%4 == 0 && (y%100 != 0 || y%400 == 0)
}

// DaysInMonth is the number of days of month m in year y.
func DaysInMonth(y, m int64) int64 {
	switch m {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if IsLeapYear(y) {
			return 29
		}
		return 28
	}
	return 0
}

// MakeDate is the Date of civil fields.
func MakeDate(y, m, d int64) (Date, bool) {
	if y < MinYear || y > MaxYear || m < 1 || m > 12 || d < 1 || d > DaysInMonth(y, m) {
		return Date{}, false
	}
	return Date{day: int32(daysFromCivil(y, m, d))}, true
}

// DateFromDays is the Date of a day count the compiler has already checked against the domain.
func DateFromDays(n int64) Date { return Date{day: int32(n)} }

// Days is the signed day count of the Date from 1970-01-01.
func (d Date) Days() int64 { return int64(d.day) }

// Civil is the year.
func (d Date) Civil() (y, m, day int64) { return civilFromDays(int64(d.day)) }

// dateOfDays checks a day count against the Date domain.
func dateOfDays(n int64) (Date, Fault) {
	if n < minDay || n > maxDay {
		return Date{}, OutOfRange
	}
	return Date{day: int32(n)}, NoFault
}

// AddDays displaces a Date by n Gregorian days.
func AddDays(d Date, n int64) (Date, Fault) {
	sum, overflow := addChecked(int64(d.day), n)
	if overflow {
		return Date{}, OutOfRange
	}
	return dateOfDays(sum)
}

// AddWeeks displaces a Date by n weeks of seven days.
func AddWeeks(d Date, n int64) (Date, Fault) {
	hi, lo := bits.Mul64(magnitude64(n), 7)
	if hi != 0 || lo > 1<<62 {

		return Date{}, OutOfRange
	}
	weeks := int64(lo)
	if n < 0 {
		weeks = -weeks
	}
	return AddDays(d, weeks)
}

// AddMonths moves a Date to the month n months away.
func AddMonths(d Date, n int64) (Date, Fault) {
	y, m, day := d.Civil()

	if n > 12*(MaxYear-MinYear+1) || n < -12*(MaxYear-MinYear+1) {
		return Date{}, OutOfRange
	}
	total := y*12 + (m - 1) + n
	ty, tm := floorDiv(total, 12), total-floorDiv(total, 12)*12+1
	return clampedDate(ty, tm, day)
}

// AddYears moves a Date to the same month n years away and limits the day to the last valid day of that month.
func AddYears(d Date, n int64) (Date, Fault) {
	y, m, day := d.Civil()
	if n > MaxYear-MinYear+1 || n < -(MaxYear-MinYear+1) {
		return Date{}, OutOfRange
	}
	return clampedDate(y+n, m, day)
}

// clampedDate is the Date of a target year and month with the day limited to the month's last valid day.
func clampedDate(y, m, day int64) (Date, Fault) {
	if y < MinYear || y > MaxYear {
		return Date{}, OutOfRange
	}
	if last := DaysInMonth(y, m); day > last {
		day = last
	}
	return Date{day: int32(daysFromCivil(y, m, day))}, NoFault
}

// DaysUntil is the signed Gregorian day count from a to b.
func DaysUntil(a, b Date) int64 { return int64(b.day) - int64(a.day) }

// MakeTime is the Time of civil fields.
func MakeTime(h, m, s, ns int64) (Time, bool) {
	if h < 0 || h > 23 || m < 0 || m > 59 || s < 0 || s > 59 || ns < 0 || ns >= nanosPerSecond {
		return Time{}, false
	}
	return Time{ns: ((h*60+m)*60+s)*nanosPerSecond + ns}, true
}

// TimeFromNanos is the Time of a day-nanosecond count the compiler has already checked against the domain.
func TimeFromNanos(n int64) Time { return Time{ns: n} }

// Nanos is the nanosecond count of the Time since the start of its day.
func (t Time) Nanos() int64 { return t.ns }

// Fields is the hour.
func (t Time) Fields() (h, m, s, ns int64) {
	ns = t.ns % nanosPerSecond
	sec := t.ns / nanosPerSecond
	return sec / 3600, sec / 60 % 60, sec % 60, ns
}

// dayNanos reduces a Duration modulo one day to a signed nanosecond count of magnitude below one day.
func dayNanos(d Duration) int64 {
	neg, mag := nanos(d)
	r := int64(mod128(mag, u128{lo: NanosPerDay}).lo)
	if neg {
		return -r
	}
	return r
}

// AddTime displaces a Time by a Duration modulo one civil day.
func AddTime(t Time, d Duration) Time {
	ns := (t.ns + dayNanos(d)) % NanosPerDay
	if ns < 0 {
		ns += NanosPerDay
	}
	return Time{ns: ns}
}

// SubTime displaces a Time backwards by a Duration modulo one civil day.
func SubTime(t Time, d Duration) Time { return AddTime(t, NegDuration(d)) }

// TimeDifference is the exact signed same-day coordinate difference a - b.
func TimeDifference(a, b Time) Duration { return fineDuration(a.ns - b.ns) }

// Until is the forward cyclic distance from a to b.
func Until(a, b Time) Duration {
	diff := b.ns - a.ns
	if diff < 0 {
		diff += NanosPerDay
	}
	return fineDuration(diff)
}

func Since(a, b Time) Duration { return Until(b, a) }

// MakeDateTime is the DateTime of local civil fields under an offset in minutes.
func MakeDateTime(y, mo, d, h, mi, s, ns, offset int64) (DateTime, bool) {
	if y < MinDateTimeYear || y > MaxDateTimeYear || offset < -MaxOffset || offset > MaxOffset {
		return DateTime{}, false
	}
	date, ok := MakeDate(y, mo, d)
	if !ok {
		return DateTime{}, false
	}
	if _, ok := MakeTime(h, mi, s, ns); !ok {
		return DateTime{}, false
	}
	local := date.Days()*secondsPerDay + (h*60+mi)*60 + s
	return DateTime{sec: local - offset*60, nsec: int32(ns), offset: int16(offset)}, true
}

// DateTimeFromParts is the DateTime of Unix seconds.
func DateTimeFromParts(sec, nsec, offset int64) DateTime {
	return DateTime{sec: sec, nsec: int32(nsec), offset: int16(offset)}
}

// Unix is the instant of the DateTime as Unix seconds and nanoseconds.
func (dt DateTime) Unix() (sec, nsec int64) { return dt.sec, int64(dt.nsec) }

// Offset is the retained offset of the DateTime in minutes east of UTC.
func (dt DateTime) Offset() int64 { return int64(dt.offset) }

// Local is the civil date and time of the DateTime under its retained offset.
func (dt DateTime) Local() (Date, Time) {
	local := dt.sec + int64(dt.offset)*60
	days := floorDiv(local, secondsPerDay)
	sod := local - days*secondsPerDay
	return Date{day: int32(days)}, Time{ns: sod*nanosPerSecond + int64(dt.nsec)}
}

// dateTimeOf is the DateTime of an instant span under an offset.
func dateTimeOf(neg bool, mag u128, offset int16) (DateTime, Fault) {

	q, r := newWordDivisor(nanosPerSecond).div128(mag)
	sec, nsec := int64(q.lo), int64(r)
	if neg {
		sec = -sec
		if nsec != 0 {
			sec--
			nsec = nanosPerSecond - nsec
		}
	}
	return inDomain(DateTime{sec: sec, nsec: int32(nsec), offset: offset})
}

// inDomain is dt when its local year lies in the four-digit domain.
func inDomain(dt DateTime) (DateTime, Fault) {
	if y := localYear(dt); y < MinDateTimeYear || y > MaxDateTimeYear {
		return DateTime{}, OutOfRange
	}
	return dt, NoFault
}

// localYear is the civil year of dt under its retained offset.
func localYear(dt DateTime) int64 {
	date, _ := dt.Local()
	y, _, _ := date.Civil()
	return y
}

const nanosPerMinute = 60 * nanosPerSecond

var (
	minUnixSec = daysFromCivil(MinDateTimeYear, 1, 1) * secondsPerDay
	maxUnixSec = (daysFromCivil(MaxDateTimeYear, 12, 31)+1)*secondsPerDay - 1
)

// offsetMinutes is the whole-minute count of an admissible offset.
func offsetMinutes(d Duration) (int64, bool) {
	neg, mag := nanos(d)
	if mag.hi != 0 || mag.lo > MaxOffset*nanosPerMinute || mag.lo%nanosPerMinute != 0 {
		return 0, false
	}
	return count(neg, mag.lo/nanosPerMinute), true
}

// WithOffset is the instant of dt retaining offset in place of its own.
func WithOffset(dt DateTime, offset Duration) (DateTime, Fault) {
	minutes, ok := offsetMinutes(offset)
	if !ok {
		return DateTime{}, OutOfRange
	}
	dt.offset = int16(minutes)
	return inDomain(dt)
}

// ScaleDuration is the exact product of an integer and a Duration under the tier rule.
func ScaleDuration(n int64, d Duration) (Duration, Fault) { return MulDuration(d, n) }

// DateTimeFromSeconds is the instant sec whole seconds after the Unix origin under offset zero.
func DateTimeFromSeconds(sec int64) (DateTime, Fault) { return DateTimeFromUnix(sec, 0) }

// DateTimeFromUnix is the instant sec seconds and nsec nanoseconds after the Unix origin under offset zero.
func DateTimeFromUnix(sec, nsec int64) (DateTime, Fault) {
	if nsec < 0 || nsec >= nanosPerSecond || sec < minUnixSec || sec > maxUnixSec {
		return DateTime{}, OutOfRange
	}
	return DateTime{sec: sec, nsec: int32(nsec)}, NoFault
}

// Offset is the retained offset of dt as a Duration.
func Offset(dt DateTime) Duration { return fineDuration(int64(dt.offset) * nanosPerMinute) }

// Unix is the instant of dt as the floor of its Unix seconds and the nanoseconds above them.
func Unix(dt DateTime) (sec, nsec int64) { return dt.Unix() }

// instant is the DateTime's instant as a signed nanosecond span.
func instant(dt DateTime) (neg bool, mag u128) {
	neg = dt.sec < 0
	hi, lo := bits.Mul64(magnitude64(dt.sec), nanosPerSecond)
	mag = u128{lo: lo, hi: hi}
	if neg {
		mag, _ = sub128(mag, u128{lo: uint64(dt.nsec)})
	} else {
		mag, _ = add128(mag, u128{lo: uint64(dt.nsec)})
	}
	return neg, mag
}

// AddDateTime displaces a DateTime's instant by a Duration and retains its offset.
func AddDateTime(dt DateTime, d Duration) (DateTime, Fault) {
	ineg, imag := instant(dt)
	dneg, dmag := nanos(d)
	neg, mag := addSpans(ineg, imag, dneg, dmag)
	return dateTimeOf(neg, mag, dt.offset)
}

// SubDateTime displaces a DateTime's instant backwards by a Duration and retains its offset.
func SubDateTime(dt DateTime, d Duration) (DateTime, Fault) {
	return AddDateTime(dt, NegDuration(d))
}

// DateTimeDifference is the elapsed Duration between the instants of a and b.
func DateTimeDifference(a, b DateTime) Duration {
	aneg, amag := instant(a)
	bneg, bmag := instant(b)
	d, _ := durationOf(addSpans(aneg, amag, !bneg, bmag))
	return d
}

// DurationFromBits is the Duration of a carrier value the compiler has already checked.
func DurationFromBits(v int64) Duration { return Duration{v: v} }

// Bits is the carrier value of the Duration.
func (d Duration) Bits() int64 { return d.v }

// Coarse reports whether the Duration is in the millisecond tier.
func (d Duration) Coarse() bool { return d.v&1 != 0 }

// Payload is the signed count of the Duration in its tier's unit.
func (d Duration) Payload() int64 { return d.v >> 1 }

// HighRes reports whether the Duration is in the nanosecond tier.
func HighRes(d Duration) bool { return !d.Coarse() }

// LowRes reports whether the Duration is in the millisecond tier.
func LowRes(d Duration) bool { return d.Coarse() }

// fineDuration is the fine-tier Duration of a nanosecond count inside the fine domain.
func fineDuration(ns int64) Duration { return Duration{v: ns << 1} }

// nanos is the exact nanosecond span of a Duration.
func nanos(d Duration) (neg bool, mag u128) {
	p := d.Payload()
	neg = p < 0
	m := magnitude64(p)
	if d.Coarse() {
		hi, lo := bits.Mul64(m, nanosPerMilli)
		return neg, u128{lo: lo, hi: hi}
	}
	return neg, u128{lo: m}
}

// durationOf selects the tier of an exact nanosecond span.
func durationOf(neg bool, mag u128) (Duration, Fault) {
	if mag.hi == 0 && mag.lo <= FineMaxNanos {
		return Duration{v: count(neg, mag.lo) << 1}, NoFault
	}
	ms, _ := newWordDivisor(nanosPerMilli).div128(mag)
	if ms.hi != 0 || ms.lo > CoarseMaxMillis {
		return Duration{}, OutOfRange
	}
	return Duration{v: count(neg, ms.lo)<<1 | 1}, NoFault
}

// count applies a sign to a magnitude below 2^63.
func count(neg bool, m uint64) int64 {
	if neg {
		return -int64(m)
	}
	return int64(m)
}

// addChecked is a + b with an overflow report.
func addChecked(a, b int64) (int64, bool) {
	sum := a + b
	return sum, (a >= 0) == (b >= 0) && (sum >= 0) != (a >= 0)
}

// addSpans adds two signed spans whose magnitudes are far below 2^127.
func addSpans(aneg bool, a u128, bneg bool, b u128) (bool, u128) {
	if aneg == bneg {
		sum, _ := add128(a, b)
		return aneg && !sum.isZero(), sum
	}
	if cmp128(a, b) >= 0 {
		diff, _ := sub128(a, b)
		return aneg && !diff.isZero(), diff
	}
	diff, _ := sub128(b, a)
	return bneg, diff
}

// AddDuration is the sum of two Durations under the tier rule.
func AddDuration(a, b Duration) (Duration, Fault) {
	aneg, amag := nanos(a)
	bneg, bmag := nanos(b)
	return durationOf(addSpans(aneg, amag, bneg, bmag))
}

// SubDuration is the difference of two Durations under the tier rule.
func SubDuration(a, b Duration) (Duration, Fault) {
	return AddDuration(a, NegDuration(b))
}

// NegDuration is the negation of a Duration.
func NegDuration(d Duration) Duration {
	return Duration{v: (-d.Payload())<<1 | d.v&1}
}

// MulDuration is the exact product of a Duration and an integer under the tier rule.
func MulDuration(d Duration, n int64) (Duration, Fault) {
	neg, mag := nanos(d)
	overflow, product := mulWord(mag, magnitude64(n))
	if overflow {
		return Duration{}, OutOfRange
	}
	return durationOf(neg != (n < 0), product)
}

// DivDuration is the exact rational quotient of a Duration by an integer.
func DivDuration(d Duration, n int64) (Duration, Fault) {
	if n == 0 {
		return Duration{}, DivisionByZero
	}
	neg, mag := nanos(d)
	m := magnitude64(n)
	var q u128
	if mag.hi == 0 {
		q.lo = mag.lo / m
	} else {
		q, _ = newWordDivisor(m).div128(mag)
	}
	return durationOf(neg != (n < 0), q)
}

const exactFloatNanos = 1 << 53

// RatioDuration is the Float ratio of two Durations, the binary64 value nearest the exact quotient: two spans of at most exactFloatNanos nanoseconds are both binary64 values and divide as Floats, which rounds the exact quotient once, and every other pair divides as a rational.
func RatioDuration(a, b Duration) (float64, Fault) {
	if b.Payload() == 0 {
		return 0, DivisionByZero
	}
	aNeg, aMag := nanos(a)
	bNeg, bMag := nanos(b)
	if aMag.hi == 0 && bMag.hi == 0 && aMag.lo <= exactFloatNanos && bMag.lo <= exactFloatNanos {
		ratio := float64(aMag.lo) / float64(bMag.lo)
		if aNeg != bNeg && aMag.lo != 0 {
			ratio = -ratio
		}
		return ratio, NoFault
	}
	ratio, _ := new(big.Rat).SetFrac(bigSpan(aNeg, aMag), bigSpan(bNeg, bMag)).Float64()
	return ratio, NoFault
}

// bigSpan is a signed span as an arbitrary-precision integer.
func bigSpan(neg bool, mag u128) *big.Int {
	out := new(big.Int).SetUint64(mag.hi)
	out.Lsh(out, 64)
	out.Or(out, new(big.Int).SetUint64(mag.lo))
	if neg {
		out.Neg(out)
	}
	return out
}
