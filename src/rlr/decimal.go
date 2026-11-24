package rlr

import (
	"math/bits"
	"strconv"
)

// Decimal is the finite decimal128 value.
type Decimal struct{ lo, hi uint64 }

const (
	DecimalPrecision = 34
	DecimalEmax      = 6144
	DecimalEmin      = -6143
	DecimalEtiny     = -6176
	DecimalEmaxQ     = 6111
)

const (
	decSignBit  = 1 << 63
	decExpShift = 49
	decExpMask  = 1<<14 - 1
	decCoefMask = 1<<decExpShift - 1
)

// MakeDecimal assembles a Decimal from a sign.
func MakeDecimal(neg bool, hi, lo uint64, exp int32) (Decimal, Fault) {
	c := u128{lo: lo, hi: hi}
	if hi&^decCoefMask != 0 || cmp128(c, pow10_128[34]) >= 0 || exp < DecimalEtiny || exp > DecimalEmaxQ {
		return Decimal{}, OutOfRange
	}
	return pack(neg, c, exp), NoFault
}

// Parts reports the sign.
func (d Decimal) Parts() (neg bool, hi, lo uint64, exp int32) {
	neg, c, exp := d.unpack()
	return neg, c.hi, c.lo, exp
}

// Bits is the packed representation of d as its high and low words.
func (d Decimal) Bits() (hi, lo uint64) { return d.hi, d.lo }

// DecimalFromBits is the Decimal whose packed representation is the given words.
func DecimalFromBits(hi, lo uint64) Decimal { return Decimal{lo: lo, hi: hi} }

// Negative reports whether d is below zero.
func (d Decimal) Negative() bool { return d.hi&decSignBit != 0 }

// Exponent is the quantum exponent of d.
func (d Decimal) Exponent() int32 { return unpackExponent(d.hi) }

// Coefficient is the decimal digits of the coefficient of d.
func (d Decimal) Coefficient() string {
	_, c, _ := d.unpack()
	var buf [40]byte
	return string(appendCoefficient(buf[:0], c))
}

func pack(neg bool, c u128, e int32) Decimal {
	hi := c.hi | uint64(uint32(e)&decExpMask)<<decExpShift
	if neg && !c.isZero() {
		hi |= decSignBit
	}
	return Decimal{lo: c.lo, hi: hi}
}

func (d Decimal) unpack() (neg bool, c u128, e int32) {
	return d.hi&decSignBit != 0, u128{lo: d.lo, hi: d.hi & decCoefMask}, unpackExponent(d.hi)
}

// unpackExponent sign-extends the 14-bit exponent field.
func unpackExponent(hi uint64) int32 {
	return int32(int16(uint16(hi>>decExpShift)<<2) >> 2)
}

// zeroDecimal is the zero with exponent e clamped into range.
func zeroDecimal(e int32) Decimal {
	if e < DecimalEtiny {
		e = DecimalEtiny
	} else if e > DecimalEmaxQ {
		e = DecimalEmaxQ
	}
	return pack(false, u128{}, e)
}

// NegDecimal is the negation of d.
func NegDecimal(d Decimal) Decimal {
	if d.lo|d.hi&decCoefMask == 0 {
		return d
	}
	return Decimal{lo: d.lo, hi: d.hi ^ decSignBit}
}

// AbsDecimal is the non-negative value of d with its exponent retained.
func AbsDecimal(d Decimal) Decimal {
	return Decimal{lo: d.lo, hi: d.hi &^ decSignBit}
}

// AddDecimal is the sum of a and b rounded to nearest.
func AddDecimal(a, b Decimal) (Decimal, Fault) { return addDecimal(a, b, false) }

// SubDecimal is the difference of a and b under the rules of AddDecimal.
func SubDecimal(a, b Decimal) (Decimal, Fault) { return addDecimal(a, b, true) }

func addDecimal(a, b Decimal, negateB bool) (Decimal, Fault) {
	na, ca, ea := a.unpack()
	nb, cb, eb := b.unpack()
	if negateB && !cb.isZero() {
		nb = !nb
	}
	if ea == eb && ca.hi|cb.hi == 0 {

		if na == nb {
			lo, carry := bits.Add64(ca.lo, cb.lo, 0)
			return pack(na, u128{lo: lo, hi: carry}, ea), NoFault
		}
		if ca.lo >= cb.lo {
			return pack(na, u128{lo: ca.lo - cb.lo}, ea), NoFault
		}
		return pack(nb, u128{lo: cb.lo - ca.lo}, ea), NoFault
	}
	if ea < eb {
		na, nb = nb, na
		ca, cb = cb, ca
		ea, eb = eb, ea
	}
	if ca.isZero() {
		return pack(nb, cb, eb), NoFault
	}
	d := int(ea - eb)
	if d == 0 {
		if na == nb {
			sum, _ := add128(ca, cb)
			return finish128(na, sum, eb, false)
		}
		if cmp128(ca, cb) >= 0 {
			diff, _ := sub128(ca, cb)
			return finish128(na, diff, eb, false)
		}
		diff, _ := sub128(cb, ca)
		return finish128(nb, diff, eb, false)
	}
	n := digits128(ca)
	if n+d <= 38 {

		p := mul128Low(ca, pow10_128[d])
		if na == nb {
			sum, _ := add128(p, cb)
			return finish128(na, sum, eb, false)
		}
		if cmp128(p, cb) >= 0 {
			diff, _ := sub128(p, cb)
			return finish128(na, diff, eb, false)
		}
		diff, _ := sub128(cb, p)
		return finish128(nb, diff, eb, false)
	}
	if n+d >= 69 {

		return pack(na, mul128Low(ca, pow10_128[34-n]), ea-int32(34-n)), NoFault
	}

	p := scale128(ca, d)
	q := u256{cb.lo, cb.hi}
	if na == nb {
		sum, _ := add256(p, q)
		return finish256(na, sum, eb, false)
	}
	diff, _ := sub256(p, q)
	return finish256(na, diff, eb, false)
}

// MulDecimal is the product of a and b rounded to nearest.
func MulDecimal(a, b Decimal) (Decimal, Fault) {
	na, ca, ea := a.unpack()
	nb, cb, eb := b.unpack()
	neg := na != nb
	e := ea + eb
	if ca.hi|cb.hi == 0 {
		hi, lo := bits.Mul64(ca.lo, cb.lo)
		p := u128{lo: lo, hi: hi}
		if e >= DecimalEtiny && e <= DecimalEmaxQ && cmp128(p, pow10_128[34]) < 0 {
			return pack(neg, p, e), NoFault
		}
		return finish128(neg, p, e, false)
	}
	return finish256(neg, mul128(ca, cb), e, false)
}

// DivDecimal is the quotient of a by b rounded to nearest.
func DivDecimal(a, b Decimal) (Decimal, Fault) {
	na, ca, ea := a.unpack()
	nb, cb, eb := b.unpack()
	if cb.isZero() {
		return Decimal{}, DivisionByZero
	}
	ideal := ea - eb
	if ca.isZero() {
		return zeroDecimal(ideal), NoFault
	}
	neg := na != nb
	if cb.hi == 0 {
		return divByWord(neg, ca, cb.lo, ideal)
	}

	s := 35 + digits128(cb) - digits128(ca)
	q, r := divmod256(scale128(ca, s), cb)
	quot := u128{lo: q[0], hi: q[1]}
	e := ideal - int32(s)
	if r.isZero() {
		e += int32(trailingZeros128(&quot, int(ideal-e)))
	}
	return finish128(neg, quot, e, !r.isZero())
}

// divByWord divides by a one-word divisor by long division in chunks of up to 19 digits.
func divByWord(neg bool, ca u128, cb uint64, ideal int32) (Decimal, Fault) {
	var q u128
	var r uint64
	var w wordDivisor
	if ca.hi == 0 {
		q.lo, r = ca.lo/cb, ca.lo%cb
	} else {
		w = newWordDivisor(cb)
		q, r = w.div128(ca)
	}
	if r == 0 {
		return finish128(neg, q, ideal, false)
	}
	if ca.hi == 0 {
		w = newWordDivisor(cb)
	}
	e := ideal
	n := 0
	if !q.isZero() {
		n = digits128(q)
	}

	r <<= w.s
	for {
		k := min(35-n, 19)
		hi, lo := bits.Mul64(r, pow10_64[k])
		var chunk uint64
		chunk, r = w.step(hi, lo)
		q = mulAdd128(q, pow10_64[k], chunk)
		e -= int32(k)
		if r == 0 {
			e += int32(trailingZeros128(&q, int(ideal-e)))
			return finish128(neg, q, e, false)
		}
		n = digits128(q)
		if n >= 35 {
			return finish128(neg, q, e, true)
		}
	}
}

// trailingZeros128 removes up to max trailing decimal zeros from q and reports how many it removed.
func trailingZeros128(q *u128, max int) int {
	k := 0
	if q.hi == 0 {
		v := q.lo
		for _, step := range [...]int{16, 8, 4, 2, 1} {
			if k+step <= max && v%pow10_64[step] == 0 {
				v /= pow10_64[step]
				k += step
			}
		}
		q.lo = v
		return k
	}
	for _, step := range [...]int{19, 16, 8, 4, 2, 1} {
		if k+step > max {
			continue
		}
		if qq, r := pow10div[step].div128(*q); r == 0 {
			*q = qq
			k += step
		}
	}
	return k
}

// RemDecimal is the remainder of a by b.
func RemDecimal(a, b Decimal) (Decimal, Fault) {
	na, ca, ea := a.unpack()
	_, cb, eb := b.unpack()
	if cb.isZero() {
		return Decimal{}, DivisionByZero
	}
	m := min(ea, eb)
	if ca.isZero() {
		return zeroDecimal(m), NoFault
	}
	if ea == eb && ca.hi|cb.hi == 0 {
		return pack(na, u128{lo: ca.lo % cb.lo}, m), NoFault
	}
	nA, nB := digits128(ca), digits128(cb)
	adjA, adjB := ea+int32(nA)-1, eb+int32(nB)-1
	if adjA < adjB {

		x := ca
		if ea > m {
			x = mul128Low(ca, pow10_128[ea-m])
		}
		return pack(na, x, m), NoFault
	}
	if adjA == adjB {

		x, y := ca, cb
		if ea > m {
			x = mul128Low(ca, pow10_128[ea-m])
		}
		if eb > m {
			y = mul128Low(cb, pow10_128[eb-m])
		}
		if cmp128(x, y) < 0 {
			return pack(na, x, m), NoFault
		}
		return pack(na, mod128(x, y), m), NoFault
	}
	var r u128
	if ea >= eb {
		d := int(ea - eb)
		switch {
		case nA+d <= 38:
			r = mod128(mul128Low(ca, pow10_128[d]), cb)
		case nA+d <= 77:
			r = mod256(scale128(ca, d), cb)
		case cb.hi == 0:

			x := bits.Rem64(ca.hi%cb.lo, ca.lo, cb.lo)
			r = u128{lo: mulmod64(x, powmod10Word(d, cb.lo), cb.lo)}
		default:
			r = mulmod128(mod128(ca, cb), powmod10(d, cb), cb)
		}
	} else {

		r = mod128(ca, mul128Low(cb, pow10_128[eb-ea]))
	}
	return pack(na, r, m), NoFault
}

// finish128 rounds a coefficient of any digit count at exponent e to the value set.
func finish128(neg bool, c u128, e int32, sticky bool) (Decimal, Fault) {
	n := digits128(c)
	drop := dropCount(n, e)
	if drop == 0 {
		return finishExact(neg, c, n, e)
	}
	if drop > n+1 {
		return zeroDecimal(DecimalEtiny), NoFault
	}
	q, guard, tail := shr10_128(c, drop)
	return roundInto(neg, q, e+int32(drop), guard, sticky || tail)
}

// finish256 is finish128 over a four-word coefficient.
func finish256(neg bool, c u256, e int32, sticky bool) (Decimal, Fault) {
	if c[2]|c[3] == 0 {
		return finish128(neg, u128{lo: c[0], hi: c[1]}, e, sticky)
	}
	n := digits256(c)
	drop := dropCount(n, e)
	if drop > n+1 {
		return zeroDecimal(DecimalEtiny), NoFault
	}
	q, guard, tail := shr10_256(c, drop)
	return roundInto(neg, u128{lo: q[0], hi: q[1]}, e+int32(drop), guard, sticky || tail)
}

// dropCount is the number of low digits an n-digit coefficient at exponent e sheds.
func dropCount(n int, e int32) int {
	drop := n - DecimalPrecision
	if under := DecimalEtiny - int(e); under > drop {
		drop = under
	}
	if drop < 0 {
		return 0
	}
	return drop
}

// roundInto applies ties-to-even rounding from the guard digit and sticky tail to a coefficient of at most 34 digits.
func roundInto(neg bool, c u128, e int32, guard int, sticky bool) (Decimal, Fault) {
	if guard > 5 || guard == 5 && (sticky || c.lo&1 == 1) {
		c, _ = add128(c, u128{lo: 1})
		if c == pow10_128[34] {
			c = pow10_128[33]
			e++
		}
	}
	return finishExact(neg, c, digits128(c), e)
}

// finishExact packs an n-digit coefficient of at most 34 digits at an exponent of at least the smallest quantum.
func finishExact(neg bool, c u128, n int, e int32) (Decimal, Fault) {
	if c.isZero() {
		return zeroDecimal(e), NoFault
	}
	if e+int32(n)-1 > DecimalEmax {
		return Decimal{}, OutOfRange
	}
	if e > DecimalEmaxQ {
		c = mul128Low(c, pow10_128[e-DecimalEmaxQ])
		e = DecimalEmaxQ
	}
	return pack(neg, c, e), NoFault
}

// ParseDecimal reads a fractional Decimal literal: no sign, because unary minus is an operator, and underscore separators between digits.
func ParseDecimal(text string) (Decimal, Fault) {
	c, e, sticky, ok := decimalDigits(text, true)
	if !ok {
		return Decimal{}, Malformed
	}
	return finish128(false, c, e, sticky)
}

// ReadDecimal reads a numeric string by the General Decimal Arithmetic to-number grammar for finite values, an optional sign, digits with an optional decimal point, and an optional exponent without separators, converts it by the Decimal literal rule, and reads -0 as zero because Decimal has one zero sign.
func ReadDecimal(text string) (Decimal, Fault) {
	neg := false
	if text != "" && (text[0] == '+' || text[0] == '-') {
		neg = text[0] == '-'
		text = text[1:]
	}
	c, e, sticky, ok := decimalDigits(text, false)
	if !ok {
		return Decimal{}, Malformed
	}
	return finish128(neg && !c.isZero(), c, e, sticky)
}

// decimalDigits reads an unsigned coefficient with an optional fraction and exponent, keeping up to 35 significant digits and reporting whether nonzero digits were dropped, with underscore separators between digits when separators is set, with a saturated exponent.
func decimalDigits(text string, separators bool) (c u128, e int32, sticky, ok bool) {
	n, dropped, frac := 0, 0, 0
	inFrac, seenDigit := false, false
	var exp int64
	i := 0
scan:
	for ; i < len(text); i++ {
		ch := text[i]
		switch {
		case ch >= '0' && ch <= '9':
			seenDigit = true
			if inFrac {
				frac++
			}
			if n == 0 && ch == '0' {
				continue
			}
			if n < 35 {
				c = mulAdd128(c, 10, uint64(ch-'0'))
				n++
			} else {
				if ch != '0' {
					sticky = true
				}
				dropped++
			}
		case ch == '_' && separators:
			if !separatorAt(text, i) {
				return u128{}, 0, false, false
			}
		case ch == '.':
			if inFrac {
				return u128{}, 0, false, false
			}
			inFrac = true
		case ch == 'e' || ch == 'E':
			var valid bool
			exp, valid = parseExponent(text[i+1:], separators)
			if !valid {
				return u128{}, 0, false, false
			}
			break scan
		default:
			return u128{}, 0, false, false
		}
	}
	if !seenDigit {
		return u128{}, 0, false, false
	}
	x := exp - int64(frac) + int64(dropped)
	x = min(max(x, -exponentSaturation), exponentSaturation)
	return c, int32(x), sticky, true
}

const exponentSaturation = 10_000_000

// parseExponent reads a signed exponent, with underscore separators between digits when separators is set.
func parseExponent(text string, separators bool) (int64, bool) {
	neg := false
	if text != "" && (text[0] == '+' || text[0] == '-') {
		neg = text[0] == '-'
		text = text[1:]
	}
	var v int64
	seen := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case ch >= '0' && ch <= '9':
			seen = true
			if v < exponentSaturation {
				v = v*10 + int64(ch-'0')
			}
		case ch == '_' && separators:
			if !separatorAt(text, i) {
				return 0, false
			}
		default:
			return 0, false
		}
	}
	if !seen {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// separatorAt reports whether the underscore at text[i] sits between two digits.
func separatorAt(text string, i int) bool {
	return i > 0 && isDecimalDigit(text[i-1]) && i+1 < len(text) && isDecimalDigit(text[i+1])
}

func isDecimalDigit(b byte) bool { return b >= '0' && b <= '9' }

// String is the cohort-preserving General Decimal Arithmetic to-scientific-string form.
func (d Decimal) String() string {
	var buf [48]byte
	return string(d.appendTo(buf[:0]))
}

func (d Decimal) appendTo(buf []byte) []byte {
	neg, c, e := d.unpack()
	if neg {
		buf = append(buf, '-')
	}
	start := len(buf)
	buf = appendCoefficient(buf, c)
	n := len(buf) - start
	adj := int(e) + n - 1
	if e <= 0 && adj >= -6 {
		if e == 0 {
			return buf
		}
		point := n + int(e)
		if point > 0 {
			buf = append(buf, 0)
			copy(buf[start+point+1:], buf[start+point:len(buf)-1])
			buf[start+point] = '.'
			return buf
		}

		k := 2 - point
		for range k {
			buf = append(buf, 0)
		}
		copy(buf[start+k:], buf[start:start+n])
		buf[start], buf[start+1] = '0', '.'
		for i := 2; i < k; i++ {
			buf[start+i] = '0'
		}
		return buf
	}
	if n > 1 {
		buf = append(buf, 0)
		copy(buf[start+2:], buf[start+1:len(buf)-1])
		buf[start+1] = '.'
	}
	buf = append(buf, 'E')
	if adj >= 0 {
		buf = append(buf, '+')
	} else {
		buf = append(buf, '-')
		adj = -adj
	}
	return strconv.AppendInt(buf, int64(adj), 10)
}

// appendCoefficient appends the decimal digits of c.
func appendCoefficient(buf []byte, c u128) []byte {
	if c.hi == 0 {
		return strconv.AppendUint(buf, c.lo, 10)
	}
	q, r := pow10div[19].div128(c)
	if q.hi != 0 {
		q2, r2 := pow10div[19].div128(q)
		buf = strconv.AppendUint(buf, q2.lo, 10)
		buf = appendPadded(buf, r2)
	} else {
		buf = strconv.AppendUint(buf, q.lo, 10)
	}
	return appendPadded(buf, r)
}

// appendPadded appends r.
func appendPadded(buf []byte, r uint64) []byte {
	const zeros = "0000000000000000000"
	buf = append(buf, zeros[digits64(r):]...)
	return strconv.AppendUint(buf, r, 10)
}

type u128 struct{ lo, hi uint64 }

type u256 [4]uint64

func (x u128) isZero() bool { return x.lo|x.hi == 0 }

func cmp128(a, b u128) int {
	switch {
	case a.hi != b.hi:
		if a.hi < b.hi {
			return -1
		}
		return 1
	case a.lo != b.lo:
		if a.lo < b.lo {
			return -1
		}
		return 1
	}
	return 0
}

func add128(a, b u128) (u128, uint64) {
	lo, c := bits.Add64(a.lo, b.lo, 0)
	hi, c := bits.Add64(a.hi, b.hi, c)
	return u128{lo: lo, hi: hi}, c
}

func sub128(a, b u128) (u128, uint64) {
	lo, b0 := bits.Sub64(a.lo, b.lo, 0)
	hi, b0 := bits.Sub64(a.hi, b.hi, b0)
	return u128{lo: lo, hi: hi}, b0
}

// mul128Low is the low 128 bits of a times b.
func mul128Low(a, b u128) u128 {
	hi, lo := bits.Mul64(a.lo, b.lo)
	return u128{lo: lo, hi: hi + a.lo*b.hi + a.hi*b.lo}
}

func mulAdd128(c u128, m, a uint64) u128 {
	hi, lo := bits.Mul64(c.lo, m)
	lo, carry := bits.Add64(lo, a, 0)
	return u128{lo: lo, hi: hi + c.hi*m + carry}
}

// mul128 is the full 256-bit product of a and b.
func mul128(a, b u128) u256 {
	h0, l0 := bits.Mul64(a.lo, b.lo)
	h1, l1 := bits.Mul64(a.lo, b.hi)
	h2, l2 := bits.Mul64(a.hi, b.lo)
	h3, l3 := bits.Mul64(a.hi, b.hi)
	var r u256
	r[0] = l0
	s1, c1 := bits.Add64(h0, l1, 0)
	s1, c2 := bits.Add64(s1, l2, 0)
	r[1] = s1
	s2, c3 := bits.Add64(h1, h2, c1)
	s2, c4 := bits.Add64(s2, l3, c2)
	r[2] = s2
	r[3] = h3 + c3 + c4
	return r
}

// wordDivisor is a one-word divisor prepared for division by reciprocal multiplication.
type wordDivisor struct {
	d, v uint64
	s    uint
}

func newWordDivisor(d uint64) wordDivisor {
	s := uint(bits.LeadingZeros64(d))
	dn := d << s
	v, _ := bits.Div64(^dn, ^uint64(0), dn)
	return wordDivisor{d: dn, v: v, s: s}
}

// step divides the two-word value u1.
func (w wordDivisor) step(u1, u0 uint64) (uint64, uint64) {
	q1, q0 := bits.Mul64(w.v, u1)
	var c uint64
	q0, c = bits.Add64(q0, u0, 0)
	q1, _ = bits.Add64(q1, u1, c)
	q1++
	r := u0 - q1*w.d
	if r > q0 {
		q1--
		r += w.d
	}
	if r >= w.d {
		q1++
		r -= w.d
	}
	return q1, r
}

// div128 divides a by the divisor.
func (w wordDivisor) div128(a u128) (u128, uint64) {
	s := w.s
	top := a.hi >> (64 - s)
	l1 := a.hi<<s | a.lo>>(64-s)
	l0 := a.lo << s
	var q u128
	q.hi, top = w.step(top, l1)
	q.lo, top = w.step(top, l0)
	return q, top >> s
}

// div256 divides a by the divisor.
func (w wordDivisor) div256(a u256) (u256, uint64) {
	s := w.s
	r := a[3] >> (64 - s)
	var q u256
	q[3], r = w.step(r, a[3]<<s|a[2]>>(64-s))
	q[2], r = w.step(r, a[2]<<s|a[1]>>(64-s))
	q[1], r = w.step(r, a[1]<<s|a[0]>>(64-s))
	q[0], r = w.step(r, a[0]<<s)
	return q, r >> s
}

// mod128 is a modulo a nonzero b.
func mod128(a, b u128) u128 {
	if b.hi == 0 {
		return u128{lo: bits.Rem64(a.hi%b.lo, a.lo, b.lo)}
	}
	if cmp128(a, b) < 0 {
		return a
	}
	_, r := divmod256(u256{a.lo, a.hi}, b)
	return r
}

// mod256 is a modulo a nonzero b.
func mod256(a u256, b u128) u128 {
	if b.hi == 0 {
		_, r := newWordDivisor(b.lo).div256(a)
		return u128{lo: r}
	}
	_, r := divmod256(a, b)
	return r
}

// mulmod128 is a times b modulo m for a and b already below m.
func mulmod128(a, b, m u128) u128 {
	return mod256(mul128(a, b), m)
}

// powmod10 is 10^d modulo m by binary exponentiation.
func powmod10(d int, m u128) u128 {
	if m == (u128{lo: 1}) {
		return u128{}
	}
	result := u128{lo: 1}
	base := mod128(u128{lo: 10}, m)
	for d > 0 {
		if d&1 == 1 {
			result = mulmod128(result, base, m)
		}
		d >>= 1
		if d > 0 {
			base = mulmod128(base, base, m)
		}
	}
	return result
}

// mulmod64 is a times b modulo m for a and b already below m.
func mulmod64(a, b, m uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return bits.Rem64(hi, lo, m)
}

// powmod10Word is 10^d modulo a one-word m by binary exponentiation.
func powmod10Word(d int, m uint64) uint64 {
	if m == 1 {
		return 0
	}
	result := uint64(1)
	base := 10 % m
	for d > 0 {
		if d&1 == 1 {
			result = mulmod64(result, base, m)
		}
		d >>= 1
		if d > 0 {
			base = mulmod64(base, base, m)
		}
	}
	return result
}

func cmp256(a, b u256) int {
	for i := 3; i >= 0; i-- {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func add256(a, b u256) (u256, uint64) {
	var r u256
	var c uint64
	for i := range r {
		r[i], c = bits.Add64(a[i], b[i], c)
	}
	return r, c
}

func sub256(a, b u256) (u256, uint64) {
	var r u256
	var borrow uint64
	for i := range r {
		r[i], borrow = bits.Sub64(a[i], b[i], borrow)
	}
	return r, borrow
}

// mul256x64 is a times a single word.
func mul256x64(a u256, m uint64) u256 {
	var r u256
	var carry uint64
	for i := range r {
		h, l := bits.Mul64(a[i], m)
		var c uint64
		r[i], c = bits.Add64(l, carry, 0)
		carry = h + c
	}
	return r
}

// scale128 is c times 10^k for k at most 76.
func scale128(c u128, k int) u256 {
	if k <= 38 {
		return mul128(c, pow10_128[k])
	}
	r := mul128(c, pow10_128[38])
	k -= 38
	for k > 19 {
		r = mul256x64(r, pow10_64[19])
		k -= 19
	}
	return mul256x64(r, pow10_64[k])
}

// divmod256 divides n by a two-word d whose high word is nonzero.
func divmod256(n u256, d u128) (u256, u128) {
	s := uint(bits.LeadingZeros64(d.hi))
	v1, v0 := d.hi, d.lo
	var u [5]uint64
	if s == 0 {
		copy(u[:4], n[:])
	} else {
		v1 = d.hi<<s | d.lo>>(64-s)
		v0 = d.lo << s
		u[4] = n[3] >> (64 - s)
		u[3] = n[3]<<s | n[2]>>(64-s)
		u[2] = n[2]<<s | n[1]>>(64-s)
		u[1] = n[1]<<s | n[0]>>(64-s)
		u[0] = n[0] << s
	}
	var q u256
	w := newWordDivisor(v1)
	for j := 2; j >= 0; j-- {
		var qhat, rhat uint64
		var overflow uint64
		if u[j+2] >= v1 {
			qhat = ^uint64(0)
			rhat, overflow = bits.Add64(u[j+1], v1, 0)
		} else {
			qhat, rhat = w.step(u[j+2], u[j+1])
		}
		for overflow == 0 {
			ph, pl := bits.Mul64(qhat, v0)
			if ph < rhat || ph == rhat && pl <= u[j] {
				break
			}
			qhat--
			rhat, overflow = bits.Add64(rhat, v1, 0)
		}
		p0h, p0l := bits.Mul64(qhat, v0)
		p1h, p1l := bits.Mul64(qhat, v1)
		mid, c := bits.Add64(p1l, p0h, 0)
		top := p1h + c
		var borrow uint64
		u[j], borrow = bits.Sub64(u[j], p0l, 0)
		u[j+1], borrow = bits.Sub64(u[j+1], mid, borrow)
		u[j+2], borrow = bits.Sub64(u[j+2], top, borrow)
		if borrow != 0 {
			qhat--
			var carry uint64
			u[j], carry = bits.Add64(u[j], v0, 0)
			u[j+1], carry = bits.Add64(u[j+1], v1, carry)
			u[j+2] += carry
		}
		q[j] = qhat
	}
	if s == 0 {
		return q, u128{lo: u[0], hi: u[1]}
	}
	return q, u128{lo: u[0]>>s | u[1]<<(64-s), hi: u[1] >> s}
}

// shr10_128 divides c by 10^k for k of at least one.
func shr10_128(c u128, k int) (u128, int, bool) {
	sticky := false
	for k > 19 {
		var r uint64
		c, r = pow10div[19].div128(c)
		sticky = sticky || r != 0
		k -= 19
	}
	q, r := pow10div[k].div128(c)
	return q, guardOf(r, k, &sticky), sticky
}

// shr10_256 is shr10_128 over four words.
func shr10_256(c u256, k int) (u256, int, bool) {
	sticky := false
	for k > 19 {
		if c[2]|c[3] == 0 {
			q, guard, tail := shr10_128(u128{lo: c[0], hi: c[1]}, k)
			return u256{q.lo, q.hi}, guard, sticky || tail
		}
		var r uint64
		c, r = pow10div[19].div256(c)
		sticky = sticky || r != 0
		k -= 19
	}
	q, r := pow10div[k].div256(c)
	return q, guardOf(r, k, &sticky), sticky
}

// guardOf splits the remainder of a division by 10^k into its leading digit.
func guardOf(r uint64, k int, sticky *bool) int {
	if k == 1 {
		return int(r)
	}
	guard, rest := pow10div[k-1].div128(u128{lo: r})
	*sticky = *sticky || rest != 0
	return int(guard.lo)
}

// digits64 is the decimal digit count of x.
func digits64(x uint64) int {
	if x == 0 {
		return 1
	}
	n := bits.Len64(x) * 1233 >> 12
	if x >= pow10_64[n] {
		n++
	}
	return n
}

// digits128 is the decimal digit count of x.
func digits128(x u128) int {
	if x.hi == 0 {
		return digits64(x.lo)
	}
	n := (64 + bits.Len64(x.hi)) * 1233 >> 12
	if cmp128(x, pow10_128[n]) >= 0 {
		n++
	}
	return n
}

// digits256 is the decimal digit count of x.
func digits256(x u256) int {
	if x[2]|x[3] == 0 {
		return digits128(u128{lo: x[0], hi: x[1]})
	}
	bl := 256 - bits.LeadingZeros64(x[3])
	if x[3] == 0 {
		bl = 192 - bits.LeadingZeros64(x[2])
	}
	n := bl * 1233 >> 12
	if cmp256(x, pow10_256[n]) >= 0 {
		n++
	}
	return n
}

var pow10_64 = [20]uint64{
	1, 10, 100, 1000, 10000, 100000, 1000000, 10000000, 100000000, 1000000000,
	10000000000, 100000000000, 1000000000000, 10000000000000, 100000000000000,
	1000000000000000, 10000000000000000, 100000000000000000, 1000000000000000000,
	10000000000000000000,
}

var (
	pow10_128 [39]u128
	pow10_256 [78]u256
	pow10div  [20]wordDivisor
)

func init() {
	for i := range pow10div {
		pow10div[i] = newWordDivisor(pow10_64[i])
	}
	pow10_128[0] = u128{lo: 1}
	for i := 1; i < len(pow10_128); i++ {
		pow10_128[i] = mulAdd128(pow10_128[i-1], 10, 0)
	}
	pow10_256[0] = u256{1}
	for i := 1; i < len(pow10_256); i++ {
		pow10_256[i] = mul256x64(pow10_256[i-1], 10)
	}
}
