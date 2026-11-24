// This file is the external text of the values that an adapter writes as strings, in
// the forms of the HTTP/JSON adapter of the Platform Reference and not in the literal
// grammar or the canonical display. Each type has one reader that reads its writer.
package rlr

import (
	"errors"
	"strconv"
	"strings"
)

// DecimalText is the external text of a Decimal: its display form, the General Decimal Arithmetic to-scientific-string form.
func DecimalText(d Decimal) string { return d.String() }

// DateText is the external text of a Date: an RFC 3339 full-date, or the ISO 8601 expanded form with a sign and six digits for a year outside 0000 through 9999.
func DateText(d Date) string { return strings.TrimPrefix(d.String(), "d") }

// ReadDate reads the external text of a Date.
func ReadDate(text string) (Date, error) {
	if strings.HasPrefix(text, "d") {
		return Date{}, errors.New("external Date text carries no d prefix")
	}
	return ParseDate("d" + text)
}

// TimeText is the external text of a Time: an RFC 3339 partial-time without a zero fraction or trailing fractional zeroes.
func TimeText(t Time) string { return strings.TrimPrefix(t.String(), "t") }

// ReadTime reads the external text of a Time, where a fraction of more than nine digits is beyond the Time domain.
func ReadTime(text string) (Time, error) {
	if strings.HasPrefix(text, "t") {
		return Time{}, errors.New("external Time text carries no t prefix")
	}
	return ParseTime("t" + text)
}

// DateTimeText is the external text of a DateTime: an RFC 3339 date-time with an uppercase T and Z and without a zero fraction or trailing fractional zeroes, which is its canonical display.
func DateTimeText(dt DateTime) string { return dt.String() }

// ReadDateTime reads an RFC 3339 date-time with an uppercase or lowercase t and z, truncating a fraction of more than nine digits after the ninth without carry and reporting that through truncated, where the unknown offset -00:00 and a local year outside 0000 through 9999 are beyond the DateTime domain.
func ReadDateTime(text string) (dt DateTime, truncated bool, err error) {
	b := []byte(text)
	if len(b) > 10 && b[10] == 't' {
		b[10] = 'T'
	}
	if n := len(b); n > 0 && b[n-1] == 'z' {
		b[n-1] = 'Z'
	}
	const fraction = len("YYYY-MM-DDTHH:MM:SS")
	if len(b) > fraction && b[fraction] == '.' {
		n := 0
		for fraction+1+n < len(b) && isDigit(b[fraction+1+n]) {
			n++
		}
		if n > 9 {
			b = append(b[:fraction+10], b[fraction+1+n:]...)
			truncated = true
		}
	}
	dt, err = ParseDateTime(string(b))
	return dt, truncated && err == nil, err
}

// DurationText is the external text of a Duration: a signed decimal number of seconds with at most nine fractional digits, without a zero fraction or trailing fractional zeroes, and the suffix s, which is the proto3 JSON form of google.protobuf.Duration.
func DurationText(d Duration) string {
	neg, mag := nanos(d)
	secs, frac := newWordDivisor(nanosPerSecond).div128(mag)
	var b []byte
	if neg {
		b = append(b, '-')
	}
	b = appendU128(b, secs)
	if frac != 0 {
		digits := strconv.FormatUint(frac+nanosPerSecond, 10)[1:]
		b = append(b, '.')
		b = append(b, strings.TrimRight(digits, "0")...)
	}
	return string(append(b, 's'))
}

// appendU128 writes an unsigned 128-bit value in decimal.
func appendU128(b []byte, x u128) []byte {
	if x.hi == 0 {
		return strconv.AppendUint(b, x.lo, 10)
	}
	return appendCoefficient(b, x)
}

// ReadDuration reads the external text of a Duration in the tier its magnitude selects, where a value that neither tier holds exactly, like a span beyond the nanosecond tier with a fraction of a millisecond, is beyond the Duration domain.
func ReadDuration(text string) (Duration, error) {
	body, ok := strings.CutSuffix(text, "s")
	if !ok {
		return Duration{}, errors.New("external Duration text ends with s")
	}
	neg := strings.HasPrefix(body, "-")
	if neg {
		body = body[1:]
	}
	whole, frac, _ := strings.Cut(body, ".")
	if whole == "" || strings.Contains(body, ".") && (frac == "" || len(frac) > 9) {
		return Duration{}, errors.New("external Duration text is whole seconds with from 1 through 9 fractional digits")
	}
	for _, part := range []string{whole, frac} {
		for i := 0; i < len(part); i++ {
			if !isDigit(part[i]) {
				return Duration{}, errors.New("external Duration text is decimal digits")
			}
		}
	}
	secs, ok := parseMagnitude(whole)
	if !ok {
		return Duration{}, errors.New("the magnitude exceeds the Duration domain")
	}
	overflow, mag := mulWord(secs, nanosPerSecond)
	if overflow {
		return Duration{}, errors.New("the magnitude exceeds the Duration domain")
	}
	if frac != "" {
		ns, _ := strconv.ParseUint(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
		var carry uint64
		if mag, carry = add128(mag, u128{lo: ns}); carry != 0 {
			return Duration{}, errors.New("the magnitude exceeds the Duration domain")
		}
	}
	if mag.hi != 0 || mag.lo > FineMaxNanos {
		if _, r := newWordDivisor(nanosPerMilli).div128(mag); r != 0 {
			return Duration{}, errors.New("a Duration beyond the nanosecond tier has whole milliseconds")
		}
	}
	d, fault := durationOf(neg && !mag.isZero(), mag)
	if fault != NoFault {
		return Duration{}, errors.New("the magnitude exceeds the Duration domain")
	}
	return d, nil
}
