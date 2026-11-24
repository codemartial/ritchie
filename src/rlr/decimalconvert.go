// This file is the radix conversion between Decimal and the other numeric types.
package rlr

import (
	"math"
	"math/bits"
	"strconv"
)

// DecimalFromInt is the exact Decimal of an Int at exponent zero.
func DecimalFromInt(i int64) Decimal {
	return pack(i < 0, u128{lo: magnitude64(i)}, 0)
}

// magnitude64 is the absolute value of i as an unsigned integer.
func magnitude64(i int64) uint64 {
	if i < 0 {
		return uint64(-i)
	}
	return uint64(i)
}

// DecimalFromFloat converts the actual binary64 value of f to decimal128.
func DecimalFromFloat(f float64) (Decimal, Fault) {
	b := math.Float64bits(f)
	neg := b>>63 != 0
	exp := int(b>>52) & 0x7ff
	mant := b & (1<<52 - 1)
	switch {
	case exp == 0x7ff:
		return Decimal{}, OutOfRange
	case exp == 0 && mant == 0:
		return Decimal{}, NoFault
	case exp == 0:
		exp = 1
	default:
		mant |= 1 << 52
	}

	exp2 := exp - 1023 - 52
	tz := bits.TrailingZeros64(mant)
	mant >>= tz
	exp2 += tz
	if exp2 >= 0 {
		return integerToDecimal(neg, mant, exp2)
	}
	return fractionToDecimal(neg, mant, -exp2)
}

// integerToDecimal converts the integer mant times two to exp2.
func integerToDecimal(neg bool, mant uint64, exp2 int) (Decimal, Fault) {
	if bits.Len64(mant)+exp2 <= 256 {
		var c u256
		word, bit := exp2/64, exp2%64
		c[word] = mant << bit
		if bit != 0 && word+1 < len(c) {
			c[word+1] = mant >> (64 - bit)
		}
		return finish256(neg, c, 0, false)
	}
	var x wide
	x.setShifted(mant, exp2)
	return x.leadingDigits(neg)
}

// fractionToDecimal converts mant times five to k at exponent minus k.
func fractionToDecimal(neg bool, mant uint64, k int) (Decimal, Fault) {
	if k <= 87 {
		c := u256{mant}
		for j := k; j > 0; j -= 27 {
			c = mul256x64(c, pow5_64[min(j, 27)])
		}
		return finish256(neg, c, int32(-k), false)
	}

	digits := int(math.Log10(float64(mant))+float64(k)*log10of5) + 1
	s := digits - 36
	j := k - s
	var x wide
	x.setShifted(mant, 0)
	for ; j > 0; j -= 27 {
		x.mulWord(pow5_64[min(j, 27)])
	}
	q, sticky := x.shiftRight(s)
	return finish128(neg, q, int32(s-k), sticky)
}

const log10of5 = 0.69897000433601880478626110527551

var pow5_64 = func() (p [28]uint64) {
	p[0] = 1
	for i := 1; i < len(p); i++ {
		p[i] = p[i-1] * 5
	}
	return p
}()

// wide is an unsigned integer of up to twenty words.
type wide struct {
	w [20]uint64
	n int
}

// setShifted sets x to mant times two to shift.
func (x *wide) setShifted(mant uint64, shift int) {
	*x = wide{}
	word, bit := shift/64, shift%64
	x.w[word] = mant << bit
	x.n = word + 1
	if bit != 0 {
		if hi := mant >> (64 - bit); hi != 0 {
			x.w[word+1] = hi
			x.n = word + 2
		}
	}
}

// mulWord multiplies x by m in place.
func (x *wide) mulWord(m uint64) {
	var carry uint64
	for i := 0; i < x.n; i++ {
		h, l := bits.Mul64(x.w[i], m)
		var c uint64
		x.w[i], c = bits.Add64(l, carry, 0)
		carry = h + c
	}
	if carry != 0 {
		x.w[x.n] = carry
		x.n++
	}
}

// divWord divides x by the prepared divisor in place and reports the remainder.
func (x *wide) divWord(d wordDivisor) uint64 {
	s := d.s
	r := x.w[x.n-1] >> (64 - s)
	for i := x.n - 1; i >= 0; i-- {
		var lo uint64
		if i > 0 {
			lo = x.w[i-1] >> (64 - s)
		}
		x.w[i], r = d.step(r, x.w[i]<<s|lo)
	}
	for x.n > 0 && x.w[x.n-1] == 0 {
		x.n--
	}
	return r >> s
}

// shiftRight is x divided by two to s.
func (x *wide) shiftRight(s int) (u128, bool) {
	word, bit := s/64, s%64
	sticky := false
	for i := 0; i < word && i < x.n; i++ {
		sticky = sticky || x.w[i] != 0
	}
	at := func(i int) uint64 {
		if i < x.n {
			return x.w[i]
		}
		return 0
	}
	if bit == 0 {
		return u128{lo: at(word), hi: at(word + 1)}, sticky
	}
	sticky = sticky || at(word)&(1<<bit-1) != 0
	lo := at(word)>>bit | at(word+1)<<(64-bit)
	hi := at(word+1)>>bit | at(word+2)<<(64-bit)
	return u128{lo: lo, hi: hi}, sticky
}

// leadingDigits rounds x.
func (x *wide) leadingDigits(neg bool) (Decimal, Fault) {
	var chunks [24]uint64
	n := 0
	for x.n > 0 {
		chunks[n] = x.divWord(pow10div[19])
		n++
	}
	var c u256
	e := int32(0)
	sticky := false
	i := n - 1
	c[0] = chunks[i]
	taken := digits64(chunks[i])
	for i--; i >= 0; i-- {
		if taken+19 > 77 {
			break
		}
		c = mul256x64(c, pow10_64[19])
		var carry uint64
		c[0], carry = bits.Add64(c[0], chunks[i], 0)
		c[1], carry = bits.Add64(c[1], 0, carry)
		c[2], carry = bits.Add64(c[2], 0, carry)
		c[3] += carry
		taken += 19
	}
	for ; i >= 0; i-- {
		sticky = sticky || chunks[i] != 0
		e += 19
	}
	return finish256(neg, c, e, sticky)
}

// FloatFromDecimal converts the numerical value of d to binary64.
func FloatFromDecimal(d Decimal) (float64, Fault) {
	neg, c, e := d.unpack()
	if c.isZero() {
		return 0, NoFault
	}
	if c.hi == 0 && c.lo < 1<<53 {

		switch {
		case e >= 0 && e <= 22:
			return signed(neg, float64(c.lo)*exactPow10[e]), NoFault
		case e < 0 && e >= -22:
			return signed(neg, float64(c.lo)/exactPow10[-e]), NoFault
		case e > 22 && e <= 22+15:
			if scaled := c.lo * pow10_64[e-22]; scaled < 1<<53 && scaled/pow10_64[e-22] == c.lo {
				return signed(neg, float64(scaled)*exactPow10[22]), NoFault
			}
		}
	}
	var buf [48]byte
	text := appendCoefficient(buf[:0], c)
	text = append(text, 'e')
	text = strconv.AppendInt(text, int64(e), 10)
	f, err := strconv.ParseFloat(string(text), 64)
	if err != nil {

		return 0, OutOfRange
	}
	return signed(neg, f), NoFault
}

// signed applies the Decimal sign to a converted magnitude.
func signed(neg bool, f float64) float64 {
	if neg {
		return -f
	}
	return f
}

var exactPow10 = [23]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}
