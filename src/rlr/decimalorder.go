// This file is the numerical order of Decimal values.
package rlr

// CompareDecimal is the numerical order of a and b.
func CompareDecimal(a, b Decimal) int {
	if a == b {
		return 0
	}
	an, ac, ae := a.unpack()
	bn, bc, be := b.unpack()
	az, bz := ac.isZero(), bc.isZero()
	switch {
	case az && bz:
		return 0
	case az:
		if bn {
			return 1
		}
		return -1
	case bz:
		if an {
			return -1
		}
		return 1
	case an != bn:
		if an {
			return -1
		}
		return 1
	}
	c := compareMagnitude(ac, ae, bc, be)
	if an {
		return -c
	}
	return c
}

// compareMagnitude orders two nonzero magnitudes.
func compareMagnitude(ac u128, ae int32, bc u128, be int32) int {
	if ae == be {
		return cmp128(ac, bc)
	}
	adjA, adjB := int(ae)+digits128(ac), int(be)+digits128(bc)
	switch {
	case adjA < adjB:
		return -1
	case adjA > adjB:
		return 1
	case ae > be:
		return cmp128(mul128Low(ac, pow10_128[ae-be]), bc)
	}
	return cmp128(ac, mul128Low(bc, pow10_128[be-ae]))
}

// EqualDecimal reports whether a and b denote one number.
func EqualDecimal(a, b Decimal) bool { return a == b || CompareDecimal(a, b) == 0 }

// LessDecimal is the natural ascending order of Decimal.
func LessDecimal(a, b Decimal) bool { return CompareDecimal(a, b) < 0 }

// MinDecimal is the least of one or more Decimals under the General Decimal Arithmetic rule.
func MinDecimal(xs ...Decimal) Decimal { return extremeDecimals(xs, true) }

// MaxDecimal is the greatest of one or more Decimals.
func MaxDecimal(xs ...Decimal) Decimal { return extremeDecimals(xs, false) }

// extremeDecimals is the least or greatest of one or more Decimals.
func extremeDecimals(xs []Decimal, min bool) Decimal {
	best := xs[0]
	for _, x := range xs[1:] {
		best = extremeDecimal(best, x, min)
	}
	return best
}

// extremeDecimal is the operand the binary min or max rule keeps between the value selected so far and the next candidate.
func extremeDecimal(best, x Decimal, min bool) Decimal {
	switch c := CompareDecimal(x, best); {
	case c < 0 && min, c > 0 && !min:
		return x
	case c != 0:
		return best
	}
	xe, be := x.Exponent(), best.Exponent()
	if xe == be {
		return best
	}
	wantLesser := min
	if x.Negative() {
		wantLesser = !wantLesser
	}
	if (xe < be) == wantLesser {
		return x
	}
	return best
}

// DecimalKey is the dictionary key projection of a Decimal.
type DecimalKey struct{ lo, hi uint64 }

// KeyOfDecimal is the key projection of d.
func KeyOfDecimal(d Decimal) DecimalKey {
	neg, c, e := d.unpack()
	if c.isZero() {
		return DecimalKey{}
	}
	if (c.lo%10+c.hi%10*6)%10 != 0 {
		return DecimalKey(d)
	}
	if c.hi == 0 {

		for c.lo%10 == 0 {
			c.lo /= 10
			e++
		}
	} else {
		e += int32(trailingZeros128(&c, DecimalPrecision))
	}
	return DecimalKey(pack(neg, c, e))
}
