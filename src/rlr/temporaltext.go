// This file is the text boundary of the temporal kernel.
package rlr

import (
	"errors"
	"math/bits"
	"strconv"
	"strings"
)

// ParseDate reads a Date literal.
func ParseDate(text string) (Date, error) {
	if !strings.HasPrefix(text, "d") {
		return Date{}, errors.New("a Date literal begins with d")
	}
	y, rest, err := readYear(text[1:], true)
	if err != nil {
		return Date{}, err
	}
	m, d, rest, err := readMonthDay(rest)
	if err != nil {
		return Date{}, err
	}
	if rest != "" {
		return Date{}, errors.New("unexpected text after the day")
	}
	date, ok := MakeDate(y, m, d)
	if !ok {
		return Date{}, errors.New("this month and day do not identify a date")
	}
	return date, nil
}

// readYear reads the year at the start of text.
func readYear(text string, expanded bool) (int64, string, error) {
	if text != "" && (text[0] == '+' || text[0] == '-') {
		if !expanded {
			return 0, "", errors.New("only Date permits a year with a sign")
		}
		digits, rest, ok := cutDigits(text[1:], 6)
		if !ok {
			return 0, "", errors.New("a year with a sign requires 6 digits")
		}
		y, _ := strconv.ParseInt(digits, 10, 64)
		if y <= 9999 {
			if text[0] == '+' || y == 0 {
				return 0, "", errors.New("write a year through 9999 with 4 digits and no sign")
			}
		}
		if text[0] == '-' {
			y = -y
		}
		return y, rest, nil
	}
	digits, rest, ok := cutDigits(text, 4)
	if !ok {
		return 0, "", errors.New("a year is exactly four digits")
	}
	y, _ := strconv.ParseInt(digits, 10, 64)
	return y, rest, nil
}

// readMonthDay reads `-MM-DD` at the start of text.
func readMonthDay(text string) (m, d int64, rest string, err error) {
	if !strings.HasPrefix(text, "-") {
		return 0, 0, "", errors.New("a hyphen separates the year and month")
	}
	month, rest, ok := cutDigits(text[1:], 2)
	if !ok {
		return 0, 0, "", errors.New("a month is exactly two digits")
	}
	if !strings.HasPrefix(rest, "-") {
		return 0, 0, "", errors.New("a hyphen separates the month and day")
	}
	day, rest, ok := cutDigits(rest[1:], 2)
	if !ok {
		return 0, 0, "", errors.New("a day is exactly two digits")
	}
	m, _ = strconv.ParseInt(month, 10, 64)
	d, _ = strconv.ParseInt(day, 10, 64)
	return m, d, rest, nil
}

// ParseTime reads a Time literal.
func ParseTime(text string) (Time, error) {
	if !strings.HasPrefix(text, "t") {
		return Time{}, errors.New("a Time literal begins with t")
	}
	t, rest, err := readTime(text[1:])
	if err != nil {
		return Time{}, err
	}
	if rest != "" {
		return Time{}, errors.New("unexpected text after the seconds")
	}
	return t, nil
}

// readTime reads a time value from text.
func readTime(text string) (Time, string, error) {
	var fields [3]int64
	rest := text
	for i := range fields {
		if i > 0 {
			if !strings.HasPrefix(rest, ":") {
				return Time{}, "", errors.New("a colon separates the hour, minute, and second")
			}
			rest = rest[1:]
		}
		digits, after, ok := cutDigits(rest, 2)
		if !ok {
			return Time{}, "", errors.New("the hour, minute, and second are exactly two digits each")
		}
		fields[i], _ = strconv.ParseInt(digits, 10, 64)
		rest = after
	}
	var ns int64
	if strings.HasPrefix(rest, ".") {
		n := 0
		for n < len(rest)-1 && isDigit(rest[1+n]) {
			n++
		}
		if n == 0 || n > 9 {
			return Time{}, "", errors.New("a fraction requires from 1 through 9 digits")
		}
		ns, _ = strconv.ParseInt(rest[1:1+n]+strings.Repeat("0", 9-n), 10, 64)
		rest = rest[1+n:]
	}
	t, ok := MakeTime(fields[0], fields[1], fields[2], ns)
	if !ok {
		return Time{}, "", errors.New("the hour must be less than 24; the minute and second must be less than 60")
	}
	return t, rest, nil
}

// ParseDateTime reads a DateTime literal.
func ParseDateTime(text string) (DateTime, error) {
	y, rest, err := readYear(text, false)
	if err != nil {
		return DateTime{}, err
	}
	mo, d, rest, err := readMonthDay(rest)
	if err != nil {
		return DateTime{}, err
	}
	if !strings.HasPrefix(rest, "T") {
		return DateTime{}, errors.New("T joins the date and time")
	}
	t, rest, err := readTime(rest[1:])
	if err != nil {
		return DateTime{}, err
	}
	offset, err := readOffset(rest)
	if err != nil {
		return DateTime{}, err
	}
	h, mi, s, ns := t.Fields()
	dt, ok := MakeDateTime(y, mo, d, h, mi, s, ns, offset)
	if !ok {
		return DateTime{}, errors.New("the month and day do not name a civil date")
	}
	return dt, nil
}

// readOffset reads the whole zone suffix.
func readOffset(text string) (int64, error) {
	if text == "Z" {
		return 0, nil
	}
	if text == "" || (text[0] != '+' && text[0] != '-') {
		return 0, errors.New("a DateTime requires Z or an offset with digits at its end")
	}
	hours, rest, ok := cutDigits(text[1:], 2)
	if !ok || !strings.HasPrefix(rest, ":") {
		return 0, errors.New("an offset contains a sign and HH:MM")
	}
	minutes, rest, ok := cutDigits(rest[1:], 2)
	if !ok || rest != "" {
		return 0, errors.New("an offset contains a sign and HH:MM")
	}
	h, _ := strconv.ParseInt(hours, 10, 64)
	m, _ := strconv.ParseInt(minutes, 10, 64)
	if m > 59 || h > 14 || h == 14 && m != 0 {
		return 0, errors.New("an offset must be from -14:00 through +14:00, with minutes less than 60")
	}
	offset := h*60 + m
	if text[0] == '-' {
		if offset == 0 {
			return 0, errors.New("-00:00 is not an offset; UTC is Z or +00:00")
		}
		offset = -offset
	}
	return offset, nil
}

var durationUnits = []struct {
	name  string
	nanos uint64
}{
	{"d", NanosPerDay}, {"h", 3600 * nanosPerSecond}, {"m", 60 * nanosPerSecond},
	{"s", nanosPerSecond}, {"ms", nanosPerMilli}, {"us", 1000}, {"ns", 1},
}

var subordinateBound = map[string]uint64{"h": 24, "m": 60, "s": 60, "ms": 1000, "us": 1000, "ns": 1000}

// ParseDuration reads an unsigned Duration literal.
func ParseDuration(text string) (Duration, error) {
	var total u128
	rest := text
	last := -1
	for rest != "" {
		digits, after, ok := cutDigitRun(rest)
		if !ok {
			return Duration{}, errors.New("each segment is a decimal integer followed by a unit")
		}
		unit := -1
		for i, u := range durationUnits {
			if strings.HasPrefix(after, u.name) && (unit < 0 || len(u.name) > len(durationUnits[unit].name)) {
				unit = i
			}
		}
		if unit < 0 {
			return Duration{}, errors.New("each segment is a decimal integer followed by d, h, m, s, ms, us, or ns")
		}
		if unit <= last {
			return Duration{}, errors.New("units are distinct and descend from d through ns")
		}
		value, ok := parseMagnitude(digits)
		if !ok {
			return Duration{}, errors.New("the magnitude exceeds the Duration domain")
		}
		if last >= 0 {
			if bound := subordinateBound[durationUnits[unit].name]; value.hi != 0 || value.lo >= bound {
				return Duration{}, errors.New("the segment value exceeds its limit")
			}
		}
		h1, l1 := mulWord(value, durationUnits[unit].nanos)
		if h1 {
			return Duration{}, errors.New("the magnitude exceeds the Duration domain")
		}
		var carry uint64
		if total, carry = add128(total, l1); carry != 0 {
			return Duration{}, errors.New("the magnitude exceeds the Duration domain")
		}
		last = unit
		rest = after[len(durationUnits[unit].name):]
	}
	if last < 0 {
		return Duration{}, errors.New("a Duration literal requires one or more segments")
	}
	d, fault := durationOf(false, total)
	if fault != NoFault {
		return Duration{}, errors.New("the magnitude exceeds the Duration domain")
	}
	return d, nil
}

// mulWord is a times m with an overflow report.
func mulWord(a u128, m uint64) (overflow bool, product u128) {
	h1, l1 := bits.Mul64(a.lo, m)
	h2, l2 := bits.Mul64(a.hi, m)
	hi, carry := bits.Add64(h1, l2, 0)
	return h2 != 0 || carry != 0, u128{lo: l1, hi: hi}
}

// parseMagnitude reads a decimal digit run with separators exactly into a bounded integer.
func parseMagnitude(digits string) (u128, bool) {
	var out u128
	for i := 0; i < len(digits); i++ {
		if digits[i] == '_' {
			continue
		}
		overflow, next := mulWord(out, 10)
		if overflow {
			return u128{}, false
		}
		var carry uint64
		if out, carry = add128(next, u128{lo: uint64(digits[i] - '0')}); carry != 0 {
			return u128{}, false
		}
	}
	return out, true
}

// cutDigits cuts exactly n decimal digits from the start of text.
func cutDigits(text string, n int) (digits, rest string, ok bool) {
	if len(text) < n {
		return "", "", false
	}
	for i := 0; i < n; i++ {
		if !isDigit(text[i]) {
			return "", "", false
		}
	}
	return text[:n], text[n:], true
}

// cutDigitRun cuts a decimal digit run with single separators strictly between two digits from the start of text.
func cutDigitRun(text string) (digits, rest string, ok bool) {
	n := 0
	for n < len(text) && (isDigit(text[n]) || text[n] == '_') {
		n++
	}
	if n == 0 || text[0] == '_' || text[n-1] == '_' || strings.Contains(text[:n], "__") {
		return "", "", false
	}
	return text[:n], text[n:], true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// String is the canonical display of the Date.
func (d Date) String() string {
	y, m, day := d.Civil()
	var b []byte
	b = append(b, 'd')
	b = appendYear(b, y)
	b = append(b, '-')
	b = appendWidth(b, m, 2)
	b = append(b, '-')
	return string(appendWidth(b, day, 2))
}

// appendYear writes a year in its literal form.
func appendYear(b []byte, y int64) []byte {
	switch {
	case y < 0:
		return appendWidth(append(b, '-'), -y, 6)
	case y > 9999:
		return appendWidth(append(b, '+'), y, 6)
	}
	return appendWidth(b, y, 4)
}

// appendWidth writes a nonnegative value zero-padded to width digits.
func appendWidth(b []byte, n int64, width int) []byte {
	digits := strconv.FormatInt(n, 10)
	for i := len(digits); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, digits...)
}

// String is the canonical display of the Time.
func (t Time) String() string {
	return string(t.appendFields(append([]byte(nil), 't')))
}

// appendFields writes the HH.
func (t Time) appendFields(b []byte) []byte {
	h, m, s, ns := t.Fields()
	b = appendWidth(b, h, 2)
	b = append(b, ':')
	b = appendWidth(b, m, 2)
	b = append(b, ':')
	b = appendWidth(b, s, 2)
	if ns != 0 {
		b = append(b, '.')
		b = appendWidth(b, ns, 9)
		for b[len(b)-1] == '0' {
			b = b[:len(b)-1]
		}
	}
	return b
}

// String is the canonical display of the DateTime.
func (dt DateTime) String() string {
	date, t := dt.Local()
	y, m, d := date.Civil()
	var b []byte
	b = appendWidth(b, y, 4)
	b = append(b, '-')
	b = appendWidth(b, m, 2)
	b = append(b, '-')
	b = appendWidth(b, d, 2)
	b = append(b, 'T')
	b = t.appendFields(b)
	return string(appendOffset(b, dt.offset))
}

// appendOffset appends an offset in minutes in its display form.
func appendOffset(b []byte, minutes int16) []byte {
	if minutes == 0 {
		return append(b, 'Z')
	}
	offset := int64(minutes)
	if offset < 0 {
		b = append(b, '-')
		offset = -offset
	} else {
		b = append(b, '+')
	}
	b = appendWidth(b, offset/60, 2)
	b = append(b, ':')
	return appendWidth(b, offset%60, 2)
}

// String is the canonical display of the Duration.
func (d Duration) String() string {
	p := d.Payload()
	if p == 0 {
		return "0s"
	}
	var b []byte
	if p < 0 {
		b = append(b, '-')
	}
	rest := magnitude64(p)
	units := durationUnits

	scale := uint64(1)
	if d.Coarse() {
		units = units[:5]
		scale = nanosPerMilli
	}
	for _, u := range units {
		unit := u.nanos / scale
		if n := rest / unit; n != 0 {
			b = strconv.AppendUint(b, n, 10)
			b = append(b, u.name...)
			rest -= n * unit
		}
	}
	return string(b)
}
