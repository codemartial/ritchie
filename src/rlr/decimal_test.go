// This file tests the decimal kernel against an independent rational-arithmetic oracle.
package rlr

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// dec represents the oracle's value.
type dec struct {
	neg  bool
	coef *big.Int
	exp  int
}

var (
	bigTen      = big.NewInt(10)
	coefLimit   = new(big.Int).Exp(bigTen, big.NewInt(DecimalPrecision), nil)
	coefCarry   = new(big.Int).Exp(bigTen, big.NewInt(DecimalPrecision-1), nil)
	maxDecimal  = dec{coef: new(big.Int).Sub(coefLimit, big.NewInt(1)), exp: DecimalEmaxQ}
	minDecimal  = dec{coef: big.NewInt(1), exp: DecimalEtiny}
	minNormal   = dec{coef: big.NewInt(1), exp: DecimalEmin}
	maxSubnorm  = dec{coef: new(big.Int).Sub(coefCarry, big.NewInt(1)), exp: DecimalEtiny}
	powTenCache = map[int]*big.Int{}
)

// powTen returns a memoized power of ten.
func powTen(k int) *big.Int {
	if p, ok := powTenCache[k]; ok {
		return p
	}
	p := new(big.Int).Exp(bigTen, big.NewInt(int64(k)), nil)
	powTenCache[k] = p
	return p
}

// rat returns the exact rational value of d.
func (d dec) rat() *big.Rat {
	r := new(big.Rat).SetInt(d.coef)
	if d.exp >= 0 {
		r.Mul(r, new(big.Rat).SetInt(powTen(d.exp)))
	} else {
		r.Quo(r, new(big.Rat).SetInt(powTen(-d.exp)))
	}
	if d.neg {
		r.Neg(r)
	}
	return r
}

func (d dec) String() string {
	sign := ""
	if d.neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%sE%d", sign, d.coef, d.exp)
}

// toDecimal converts an oracle value to Decimal.
func toDecimal(t testing.TB, d dec) Decimal {
	t.Helper()
	if d.coef.Sign() < 0 || d.coef.Cmp(coefLimit) >= 0 || d.exp < DecimalEtiny || d.exp > DecimalEmaxQ {
		t.Fatalf("oracle value %s is outside the value set", d)
	}
	words := d.coef.Bits()
	var lo, hi uint64
	if len(words) > 0 {
		lo = uint64(words[0])
	}
	if len(words) > 1 {
		hi = uint64(words[1])
	}
	v, fault := MakeDecimal(d.neg, hi, lo, int32(d.exp))
	if fault != NoFault {
		t.Fatalf("MakeDecimal(%s) = %v", d, fault)
	}
	return v
}

func TestDecimalMakeChecksRange(t *testing.T) {
	maxHi, maxLo := uint64(0x1ED09BEAD87C0), uint64(0x378D8E63FFFFFFFF)
	valid := []struct {
		neg    bool
		hi, lo uint64
		exp    int32
	}{
		{false, 0, 0, 0}, {true, 0, 1, DecimalEtiny}, {false, maxHi, maxLo, DecimalEmaxQ},
	}
	for _, tt := range valid {
		d, fault := MakeDecimal(tt.neg, tt.hi, tt.lo, tt.exp)
		if fault != NoFault {
			t.Errorf("MakeDecimal(%v, %d, %d, %d) = %v", tt.neg, tt.hi, tt.lo, tt.exp, fault)
			continue
		}
		if neg, hi, lo, exp := d.Parts(); neg != tt.neg || hi != tt.hi || lo != tt.lo || exp != tt.exp {
			t.Errorf("Parts of MakeDecimal(%v, %d, %d, %d) = %v, %d, %d, %d", tt.neg, tt.hi, tt.lo, tt.exp, neg, hi, lo, exp)
		}
		if back := DecimalFromBits(d.Bits()); back != d {
			t.Errorf("DecimalFromBits(Bits) of %v = %v", d, back)
		}
	}
	invalid := []struct {
		hi, lo uint64
		exp    int32
	}{
		{maxHi, maxLo + 1, 0}, {1 << decExpShift, 0, 0}, {0, 1, DecimalEtiny - 1}, {0, 1, DecimalEmaxQ + 1}, {0, 1, 8192}, {0, 1, 20000},
	}
	for _, tt := range invalid {
		if _, fault := MakeDecimal(false, tt.hi, tt.lo, tt.exp); fault != OutOfRange {
			t.Errorf("MakeDecimal(false, %d, %d, %d) = %v, want OutOfRange", tt.hi, tt.lo, tt.exp, fault)
		}
	}
}

// fromDecimal converts a Decimal to an oracle value.
func fromDecimal(d Decimal) dec {
	neg, hi, lo, exp := d.Parts()
	coef := new(big.Int).SetUint64(hi)
	coef.Lsh(coef, 64)
	coef.Or(coef, new(big.Int).SetUint64(lo))
	return dec{neg: neg, coef: coef, exp: int(exp)}
}

func (d dec) equal(o dec) bool {
	return d.neg == o.neg && d.coef.Cmp(o.coef) == 0 && d.exp == o.exp
}

// oracleRound rounds an oracle value under decimal128 rules.
func oracleRound(v *big.Rat, ideal int) (dec, bool) {
	neg := v.Sign() < 0
	mag := new(big.Rat).Abs(v)
	if mag.Sign() == 0 {
		return dec{coef: new(big.Int), exp: clampExp(ideal)}, false
	}

	adj := len(mag.Num().String()) - len(mag.Denom().String())
	if scaledCmp(mag, adj) < 0 {
		adj--
	} else if scaledCmp(mag, adj+1) >= 0 {
		adj++
	}
	e := max(DecimalEtiny, adj-33)
	c := roundHalfEven(mag, e)
	if c.Cmp(coefLimit) == 0 {
		c = new(big.Int).Set(coefCarry)
		e++
	}
	exact := new(big.Rat).SetInt(c)
	exact.Mul(exact, ratPow(e))
	if exact.Cmp(mag) == 0 {
		var r big.Int
		for e < ideal {
			q, rem := new(big.Int).QuoRem(c, bigTen, &r)
			if rem.Sign() != 0 {
				break
			}
			c = q
			e++
		}
	}
	if c.Sign() == 0 {
		return dec{coef: c, exp: clampExp(e)}, false
	}
	if e+len(c.String())-1 > DecimalEmax {
		return dec{}, true
	}
	if e > DecimalEmaxQ {
		c = new(big.Int).Mul(c, powTen(e-DecimalEmaxQ))
		e = DecimalEmaxQ
	}
	return dec{neg: neg, coef: c, exp: e}, false
}

// clampExp limits an exponent to the decimal128 range.
func clampExp(e int) int {
	if e < DecimalEtiny {
		return DecimalEtiny
	}
	if e > DecimalEmaxQ {
		return DecimalEmaxQ
	}
	return e
}

var ratPowCache = map[int]*big.Rat{}

// ratPow returns a rational power of ten.
func ratPow(e int) *big.Rat {
	if r, ok := ratPowCache[e]; ok {
		return r
	}
	var r *big.Rat
	if e >= 0 {
		r = new(big.Rat).SetInt(powTen(e))
	} else {
		r = new(big.Rat).SetFrac(big.NewInt(1), powTen(-e))
	}
	ratPowCache[e] = r
	return r
}

// scaledCmp compares mag with 10^k.
func scaledCmp(mag *big.Rat, k int) int {
	return mag.Cmp(ratPow(k))
}

// roundHalfEven rounds a magnitude to the nearest even integer.
func roundHalfEven(mag *big.Rat, e int) *big.Int {
	x := new(big.Rat).Quo(mag, ratPow(e))
	q, _ := new(big.Int).Divide(x.Num(), x.Denom(), nil, big.Round)
	return q
}

// oracleAdd returns the exact sum rounded at the lesser exponent.
func oracleAdd(a, b dec) (dec, bool) {
	return oracleRound(new(big.Rat).Add(a.rat(), b.rat()), min(a.exp, b.exp))
}

// oracleSub returns the exact difference rounded at the lesser exponent.
func oracleSub(a, b dec) (dec, bool) {
	return oracleRound(new(big.Rat).Sub(a.rat(), b.rat()), min(a.exp, b.exp))
}

// oracleMul returns the exact product rounded at the summed exponent.
func oracleMul(a, b dec) (dec, bool) {
	return oracleRound(new(big.Rat).Mul(a.rat(), b.rat()), a.exp+b.exp)
}

// oracleDiv returns the exact quotient rounded at the difference of the exponents.
func oracleDiv(a, b dec) (dec, bool) {
	return oracleRound(new(big.Rat).Quo(a.rat(), b.rat()), a.exp-b.exp)
}

// oracleRem returns the remainder of truncating division.
func oracleRem(a, b dec) dec {
	q := new(big.Rat).Quo(a.rat(), b.rat())
	trunc, _ := new(big.Int).Divide(q.Num(), q.Denom(), nil, big.Trunc)
	r := new(big.Rat).Sub(a.rat(), new(big.Rat).Mul(b.rat(), new(big.Rat).SetInt(trunc)))
	m := min(a.exp, b.exp)
	scaled := new(big.Rat).Quo(r, ratPow(m))
	if !scaled.IsInt() {
		panic("remainder is not exact at the lesser exponent")
	}
	c := new(big.Int).Set(scaled.Num())
	neg := c.Sign() < 0
	c.Abs(c)
	if c.Cmp(coefLimit) >= 0 {
		panic("remainder exceeds 34 digits")
	}
	return dec{neg: neg && c.Sign() != 0, coef: c, exp: m}
}

// parseDec reads the oracle's own literal form.
func parseDec(t testing.TB, s string) dec {
	t.Helper()
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	mant, expText, hasExp := strings.Cut(strings.ToUpper(s), "E")
	exp := 0
	if hasExp {
		fmt.Sscan(expText, &exp)
	}
	intPart, fracPart, _ := strings.Cut(mant, ".")
	c, ok := new(big.Int).SetString(strings.ReplaceAll(intPart+fracPart, "_", ""), 10)
	if !ok {
		t.Fatalf("bad oracle literal %q", s)
	}
	d := dec{neg: neg && c.Sign() != 0, coef: c, exp: exp - len(strings.ReplaceAll(fracPart, "_", ""))}
	if d.exp > DecimalEmaxQ {

		d.coef, d.exp = new(big.Int).Mul(d.coef, powTen(d.exp-DecimalEmaxQ)), DecimalEmaxQ
	}
	return d
}

// randomDec draws a random oracle decimal.
func randomDec(r *rand.Rand) dec {
	var c *big.Int
	switch r.IntN(8) {
	case 0:
		c = new(big.Int).Sub(coefLimit, big.NewInt(1))
	case 1:
		c = new(big.Int).Set(powTen(r.IntN(34)))
	case 2:
		c = big.NewInt(int64(r.IntN(1000)))
	case 3:
		c = new(big.Int).SetUint64(r.Uint64())
		c.Mod(c, coefLimit)
	default:
		digits := 1 + r.IntN(34)
		var sb strings.Builder
		for range digits {
			sb.WriteByte(byte('0' + r.IntN(10)))
		}
		c, _ = new(big.Int).SetString(sb.String(), 10)
	}
	var e int
	switch r.IntN(6) {
	case 0:
		e = DecimalEtiny + r.IntN(DecimalEmaxQ-DecimalEtiny+1)
	case 1:
		e = r.IntN(11) - 5
	case 2:
		e = []int{DecimalEtiny, DecimalEmaxQ, DecimalEmin, DecimalEmin - 33, DecimalEmax - 33, 0}[r.IntN(6)]
	case 3:
		e = r.IntN(80) - 40
	default:
		e = r.IntN(200) - 100
	}
	return dec{neg: r.IntN(2) == 0 && c.Sign() != 0, coef: c, exp: e}
}

type binaryCase struct {
	name   string
	kernel func(a, b Decimal) (Decimal, Fault)
	oracle func(a, b dec) (dec, bool)
	zero   bool
}

var binaryCases = []binaryCase{
	{"add", AddDecimal, oracleAdd, false},
	{"sub", SubDecimal, oracleSub, false},
	{"mul", MulDecimal, oracleMul, false},
	{"div", DivDecimal, oracleDiv, true},
	{"rem", RemDecimal, func(a, b dec) (dec, bool) { return oracleRem(a, b), false }, true},
}

// checkBinary compares one kernel operation with its oracle on a pair.
func checkBinary(t testing.TB, op binaryCase, a, b dec) {
	t.Helper()
	got, fault := op.kernel(toDecimal(t, a), toDecimal(t, b))
	if op.zero && b.coef.Sign() == 0 {
		if fault != DivisionByZero {
			t.Errorf("%s(%s, %s) = %v, want DivisionByZero", op.name, a, b, fault)
		}
		return
	}
	want, overflow := op.oracle(a, b)
	if overflow {
		if fault != OutOfRange {
			t.Errorf("%s(%s, %s) = %s %v, want OutOfRange", op.name, a, b, fromDecimal(got), fault)
		}
		return
	}
	if fault != NoFault {
		t.Errorf("%s(%s, %s) = %v, want %s", op.name, a, b, fault, want)
		return
	}
	if g := fromDecimal(got); !g.equal(want) {
		t.Errorf("%s(%s, %s) = %s, want %s", op.name, a, b, g, want)
	}
}

func TestDecimalArithmeticVectors(t *testing.T) {
	tests := []struct{ op, a, b, want string }{

		{"add", "1.20", "3.400", "4.600"},
		{"mul", "1.20", "3.400", "4.08000"},
		{"div", "8.00", "2.0", "4.0"},
		{"rem", "1E+100", "3.0", "1.0"},

		{"add", "1.0", "-1.0", "0.0"},
		{"sub", "1.00", "1.0", "0.00"},
		{"add", "0E-3", "0E+2", "0.000"},
		{"add", "-0.0", "0.00", "0.00"},
		{"mul", "-1.0", "0", "0.0"},
		{"add", "1E+3", "0.001", "1000.001"},
		{"add", "123456789012345678901234567890.1234", "0.00005", "123456789012345678901234567890.1234"},
		{"add", "123456789012345678901234567890.1234", "0.00015", "123456789012345678901234567890.1236"},
		{"add", "9999999999999999999999999999999999", "1", "1.000000000000000000000000000000000E+34"},
		{"sub", "1E+40", "1", "1.000000000000000000000000000000000E+40"},
		{"sub", "1E+40", "5", "1.000000000000000000000000000000000E+40"},
		{"sub", "1E+40", "6000000", "9.999999999999999999999999999999994E+39"},
		{"add", "1E+100", "1E-100", "1.000000000000000000000000000000000E+100"},
		{"sub", "1E+6111", "1E-6176", "1.000000000000000000000000000000000E+6111"},
		{"add", "1.5", "2.5", "4.0"},
		{"add", "1E+2", "1E+3", "1.1E+3"},
		{"mul", "9999999999999999999999999999999999", "9999999999999999999999999999999999", "9.999999999999999999999999999999998E+67"},
		{"mul", "1.5", "1.5", "2.25"},
		{"mul", "1E+6111", "1E+10", "1.0000000000E+6121"},
		{"mul", "1E-6176", "0.1", "0E-6176"},
		{"mul", "1E-6176", "0.5", "0E-6176"},
		{"mul", "3E-6176", "0.5", "2E-6176"},
		{"mul", "1E-6100", "1E-70", "1E-6170"},
		{"mul", "1.234567890123456789012345678901234E-6143", "1E-10", "1.23456789012345678901235E-6153"},
		{"div", "1", "3", "0.3333333333333333333333333333333333"},
		{"div", "2", "3", "0.6666666666666666666666666666666667"},
		{"div", "1.0", "8", "0.125"},
		{"div", "1", "8", "0.125"},
		{"div", "1.00", "4", "0.25"},
		{"div", "12", "4", "3"},
		{"div", "1E+35", "1", "1E+35"},
		{"div", "1000000000000000000000000000000000", "0.01", "1.000000000000000000000000000000000E+35"},
		{"div", "1", "1E-40", "1E+40"},
		{"div", "0", "-5", "0"},
		{"div", "0E+3", "1E+2", "0E+1"},
		{"div", "-1", "1E+6111", "-1E-6111"},
		{"div", "1E-65", "1E+6111", "1E-6176"},
		{"div", "1E-65", "3E+6111", "0E-6176"},
		{"div", "2E-65", "3E+6111", "1E-6176"},
		{"div", "1E+6143", "0.1", "1.000000000000000000000000000000000E+6144"},
		{"div", "9999999999999999999999999999999999", "0.3333333333333333333333333333333333", "3E+34"},
		{"rem", "7", "3", "1"},
		{"rem", "-7", "3", "-1"},
		{"rem", "7", "-3", "1"},
		{"rem", "-7", "-3", "-1"},
		{"rem", "7.5", "2", "1.5"},
		{"rem", "2", "7.5", "2.0"},
		{"rem", "6", "3", "0"},
		{"rem", "-6.0", "3", "0.0"},
		{"rem", "1E+6111", "7", "6"},
		{"rem", "1E+6111", "9999999999999999999999999999999999", "10000000000000000000000000"},
		{"rem", "1E-6176", "1E+6111", "1E-6176"},
		{"rem", "1E+30", "1.5", "1.0"},
		{"rem", "123.456", "1E+2", "23.456"},
		{"rem", "1E+2", "123.456", "100.000"},
	}
	for _, tt := range tests {
		t.Run(tt.op+" "+tt.a+" "+tt.b, func(t *testing.T) {
			var op binaryCase
			for _, c := range binaryCases {
				if c.name == tt.op {
					op = c
				}
			}
			a, b := parseDec(t, tt.a), parseDec(t, tt.b)
			checkBinary(t, op, a, b)
			got, fault := op.kernel(toDecimal(t, a), toDecimal(t, b))
			if fault != NoFault {
				t.Fatalf("fault %v", fault)
			}
			if got.String() != tt.want {
				t.Errorf("display = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestDecimalFailures(t *testing.T) {
	tests := []struct {
		name string
		op   func(a, b Decimal) (Decimal, Fault)
		a, b string
		want Fault
	}{
		{"add overflow", AddDecimal, "9999999999999999999999999999999999E+6111", "1E+6111", OutOfRange},
		{"add carry overflow", AddDecimal, "9999999999999999999999999999999999E+6111", "5E+6110", OutOfRange},
		{"add below carry", AddDecimal, "9999999999999999999999999999999999E+6111", "4E+6110", NoFault},
		{"sub overflow", SubDecimal, "-9999999999999999999999999999999999E+6111", "1E+6111", OutOfRange},
		{"mul overflow", MulDecimal, "1E+6144", "10", OutOfRange},
		{"mul at the edge", MulDecimal, "9.999999999999999999999999999999999E+6143", "10", NoFault},
		{"div overflow", DivDecimal, "1E+6144", "0.01", OutOfRange},
		{"div by zero", DivDecimal, "1", "0", DivisionByZero},
		{"div zero by zero", DivDecimal, "0", "0.00", DivisionByZero},
		{"rem by zero", RemDecimal, "1", "0E+5", DivisionByZero},
		{"rem stays finite", RemDecimal, "9999999999999999999999999999999999E+6111", "1E-6176", NoFault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, fault := tt.op(toDecimal(t, parseDec(t, tt.a)), toDecimal(t, parseDec(t, tt.b)))
			if fault != tt.want {
				t.Errorf("fault = %v, want %v", fault, tt.want)
			}
		})
	}
}

func TestDecimalUnary(t *testing.T) {
	tests := []struct{ in, neg, abs string }{
		{"1.50", "-1.50", "1.50"},
		{"-1.50", "1.50", "1.50"},
		{"0.00", "0.00", "0.00"},
		{"-0E+5", "0E+5", "0E+5"},
		{"-9.999999999999999999999999999999999E+6144", "9.999999999999999999999999999999999E+6144", "9.999999999999999999999999999999999E+6144"},
		{"-1E-6176", "1E-6176", "1E-6176"},
	}
	for _, tt := range tests {
		d := toDecimal(t, parseDec(t, tt.in))
		if got := NegDecimal(d).String(); got != tt.neg {
			t.Errorf("-(%s) = %s, want %s", tt.in, got, tt.neg)
		}
		if got := AbsDecimal(d).String(); got != tt.abs {
			t.Errorf("abs(%s) = %s, want %s", tt.in, got, tt.abs)
		}
	}
	if (Decimal{}).String() != "0" || NegDecimal(Decimal{}).Negative() {
		t.Error("the zero value is not the non-negative Decimal zero")
	}
}

func TestDecimalDifferential(t *testing.T) {
	r := rand.New(rand.NewPCG(30, 2026))
	for i := range 20000 {
		a, b := randomDec(r), randomDec(r)
		for _, op := range binaryCases {
			checkBinary(t, op, a, b)
		}
		if t.Failed() && i > 50 {
			t.Fatal("stopping after repeated failures")
		}
	}
}

func TestDecimalEdgeGrid(t *testing.T) {
	edges := []dec{
		maxDecimal, minDecimal, minNormal, maxSubnorm,
		{coef: new(big.Int), exp: 0}, {coef: new(big.Int), exp: DecimalEtiny}, {coef: new(big.Int), exp: DecimalEmaxQ},
		{coef: big.NewInt(1), exp: 0}, {coef: big.NewInt(5), exp: -1}, {coef: big.NewInt(15), exp: -1},
		{coef: new(big.Int).Set(coefCarry), exp: 0}, {coef: new(big.Int).Set(coefCarry), exp: DecimalEmaxQ},
		{coef: new(big.Int).Sub(coefLimit, big.NewInt(1)), exp: 0},
		{coef: new(big.Int).Sub(coefLimit, big.NewInt(1)), exp: DecimalEtiny},
		{coef: big.NewInt(1), exp: DecimalEmaxQ}, {coef: big.NewInt(1), exp: DecimalEmax},
		{coef: big.NewInt(3), exp: 40}, {coef: big.NewInt(7), exp: -40},
	}
	for _, a := range edges {
		for _, b := range edges {
			for _, sa := range []bool{false, true} {
				for _, sb := range []bool{false, true} {
					x, y := a, b
					x.neg, y.neg = sa && x.coef.Sign() != 0, sb && y.coef.Sign() != 0
					if x.exp > DecimalEmaxQ {
						x.coef, x.exp = new(big.Int).Mul(x.coef, powTen(x.exp-DecimalEmaxQ)), DecimalEmaxQ
					}
					if y.exp > DecimalEmaxQ {
						y.coef, y.exp = new(big.Int).Mul(y.coef, powTen(y.exp-DecimalEmaxQ)), DecimalEmaxQ
					}
					for _, op := range binaryCases {
						checkBinary(t, op, x, y)
					}
				}
			}
		}
	}
}

func TestDecimalDisplay(t *testing.T) {
	tests := []struct{ in, want string }{
		{"123", "123"},
		{"-123", "-123"},
		{"123E+1", "1.23E+3"},
		{"123E+3", "1.23E+5"},
		{"123E-1", "12.3"},
		{"123E-5", "0.00123"},
		{"123E-10", "1.23E-8"},
		{"-123E-12", "-1.23E-10"},
		{"0", "0"},
		{"0E+2", "0E+2"},
		{"0E-2", "0.00"},
		{"0E-6", "0.000000"},
		{"0E-7", "0E-7"},
		{"1E-6", "0.000001"},
		{"1E-7", "1E-7"},
		{"5E-6", "0.000005"},
		{"50E-7", "0.0000050"},
		{"1E+1", "1E+1"},
		{"1E+6144", "1.000000000000000000000000000000000E+6144"},
		{"1E-6176", "1E-6176"},
		{"9999999999999999999999999999999999E+6111", "9.999999999999999999999999999999999E+6144"},
		{"1234567890123456789012345678901234E-40", "1.234567890123456789012345678901234E-7"},
		{"1234567890123456789012345678901234E-34", "0.1234567890123456789012345678901234"},
		{"1234567890123456789012345678901234E-33", "1.234567890123456789012345678901234"},
		{"1234567890123456789012345678901234E-39", "0.000001234567890123456789012345678901234"},
		{"1234567890123456789012345678901234", "1234567890123456789012345678901234"},
		{"12345", "12345"},
		{"1.20", "1.20"},
		{"4.600", "4.600"},
	}
	for _, tt := range tests {
		if got := toDecimal(t, parseDec(t, tt.in)).String(); got != tt.want {
			t.Errorf("%s displays as %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDecimalParse(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1.20", "1.20"},
		{"1_000.5", "1000.5"},
		{"6e2", "6E+2"},
		{"6E+2", "6E+2"},
		{"1.6e-19", "1.6E-19"},
		{"2e10", "2E+10"},
		{"0.0", "0.0"},
		{"00012.3400", "12.3400"},
		{"0e5", "0E+5"},
		{"0e7000", "0E+6111"},
		{"0e-7000", "0E-6176"},
		{"1e6111", "1E+6111"},
		{"1e6112", "1.0E+6112"},
		{"1e6144", "1.000000000000000000000000000000000E+6144"},
		{"1.5e6144", "1.500000000000000000000000000000000E+6144"},
		{"9999999999999999999999999999999999e6111", "9.999999999999999999999999999999999E+6144"},
		{"1e-6176", "1E-6176"},
		{"1e-6177", "0E-6176"},
		{"5e-6177", "0E-6176"},
		{"6e-6177", "1E-6176"},
		{"15e-6177", "2E-6176"},
		{"25e-6177", "2E-6176"},
		{"1e-6143", "1E-6143"},
		{"1.2345e-6175", "1.2E-6175"},
		{"1.2345e-6176", "1E-6176"},
		{"1234567890123456789012345678901234", "1234567890123456789012345678901234"},
		{"12345678901234567890123456789012345", "1.234567890123456789012345678901234E+34"},
		{"12345678901234567890123456789012345.5", "1.234567890123456789012345678901235E+34"},
		{"12345678901234567890123456789012344.5", "1.234567890123456789012345678901234E+34"},
		{"12345678901234567890123456789012345000000000e-9", "1.234567890123456789012345678901234E+34"},
		{"12345678901234567890123456789012345000001e-6", "1.234567890123456789012345678901235E+34"},
		{"99999999999999999999999999999999995", "1.000000000000000000000000000000000E+35"},
		{"0.000000000000000000000000000000000000001", "1E-39"},
		{"1.000", "1.000"},
		{"100", "100"},
		{"1e0", "1"},
		{"1e-0", "1"},
	}
	for _, tt := range tests {
		got, fault := ParseDecimal(tt.in)
		if fault != NoFault {
			t.Errorf("parse %q = %v", tt.in, fault)
			continue
		}
		if got.String() != tt.want {
			t.Errorf("parse %q = %q, want %q", tt.in, got.String(), tt.want)
		}
	}
	for _, in := range []string{"1e6145", "9999999999999999999999999999999999.5e6111", "1e99999999999", "1e10000000"} {
		if _, fault := ParseDecimal(in); fault != OutOfRange {
			t.Errorf("parse %q = %v, want OutOfRange", in, fault)
		}
	}
	for _, in := range []string{"", ".", "e5", "1e", "1.2.3", "1x", "1e+", "-1",
		"_", "_1", "1_", "1__2", "1_.2", "1._2", "1_e2", "1e_2", "1e+_2", "1e2_", "1e+_2_"} {
		if _, fault := ParseDecimal(in); fault != Malformed {
			t.Errorf("parse %q = %v, want Malformed", in, fault)
		}
	}
}

func TestDecimalParseAgreesWithOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(50, 54))
	for range 5000 {
		digits := 1 + r.IntN(60)
		var sb strings.Builder
		for range digits {
			sb.WriteByte(byte('0' + r.IntN(10)))
		}
		text := sb.String()
		point := r.IntN(digits + 1)
		if r.IntN(2) == 0 {
			text = text[:point] + "." + text[point:]
		}
		exp := 0
		if r.IntN(2) == 0 {
			exp = r.IntN(13000) - 6500
			text += fmt.Sprintf("e%d", exp)
		}
		if strings.HasPrefix(text, ".") {
			text = "0" + text
		}
		if strings.HasSuffix(text, ".") || strings.Contains(text, ".e") {
			text = strings.Replace(text, ".", ".0", 1)
		}
		d := parseDec(t, text)
		want, overflow := oracleRound(d.rat(), d.exp)
		got, fault := ParseDecimal(text)
		switch {
		case overflow:
			if fault != OutOfRange {
				t.Errorf("parse %q = %v, want OutOfRange", text, fault)
			}
		case fault != NoFault:
			t.Errorf("parse %q = %v", text, fault)
		case !fromDecimal(got).equal(want):
			t.Errorf("parse %q = %s, want %s", text, fromDecimal(got), want)
		}
	}
}

func TestDecimalDisplayRoundTrips(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	for range 5000 {
		d := randomDec(r)
		v := toDecimal(t, d)
		text := v.String()
		back, fault := ParseDecimal(strings.TrimPrefix(text, "-"))
		if fault != NoFault {
			t.Fatalf("display %q of %s does not parse: %v", text, d, fault)
		}
		if d.neg {
			back = NegDecimal(back)
		}
		if back != v {
			t.Errorf("%s displays as %q which parses to %s", d, text, fromDecimal(back))
		}
	}
}

func TestDecimalRoundedAndUnrounded(t *testing.T) {

	third, _ := DivDecimal(one("1"), one("3"))
	back, _ := MulDecimal(third, one("3"))
	if back.String() != "0.9999999999999999999999999999999999" {
		t.Errorf("(1/3)*3 = %s", back)
	}

	tiny, fault := MulDecimal(one("1E-6170"), one("1.23456789E-3"))
	if fault != NoFault || tiny.String() != "1.235E-6173" {
		t.Errorf("subnormal product = %s %v", tiny, fault)
	}

	up, _ := AddDecimal(one("1000000000000000000000000000000001"), one("0.5"))
	down, _ := AddDecimal(one("1000000000000000000000000000000000"), one("0.5"))
	if up.String() != "1000000000000000000000000000000002" || down.String() != "1000000000000000000000000000000000" {
		t.Errorf("ties = %s and %s", up, down)
	}
}

// one parses a decimal literal or panics.
func one(s string) Decimal {
	d, fault := ParseDecimal(s)
	if fault != NoFault {
		panic(s)
	}
	return d
}

func TestDigitCounts(t *testing.T) {
	for bl := 1; bl <= 256; bl++ {
		low := new(big.Int).Lsh(big.NewInt(1), uint(bl-1))
		high := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bl)), big.NewInt(1))
		for _, v := range []*big.Int{low, high} {
			want := len(v.String())
			words := v.Bits()
			var limbs [4]uint64
			for i, w := range words {
				limbs[i] = uint64(w)
			}
			if got := digits256(u256(limbs)); got != want {
				t.Errorf("digits256(%s) = %d, want %d", v, got, want)
			}
			if bl <= 128 {
				if got := digits128(u128{lo: limbs[0], hi: limbs[1]}); got != want {
					t.Errorf("digits128(%s) = %d, want %d", v, got, want)
				}
			}
			if bl <= 64 {
				if got := digits64(limbs[0]); got != want {
					t.Errorf("digits64(%s) = %d, want %d", v, got, want)
				}
			}
		}
	}
	if digits64(0) != 1 || digits128(u128{}) != 1 || digits256(u256{}) != 1 {
		t.Error("zero has one digit")
	}
}

func TestWideDivision(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	toBig := func(limbs []uint64) *big.Int {
		v := new(big.Int)
		for _, limb := range slices.Backward(limbs) {
			v.Lsh(v, 64)
			v.Or(v, new(big.Int).SetUint64(limb))
		}
		return v
	}
	for range 20000 {
		var n u256
		for j := range n {
			switch r.IntN(4) {
			case 0:
				n[j] = ^uint64(0)
			case 1:
				n[j] = 0
			default:
				n[j] = r.Uint64()
			}
		}
		d := u128{lo: r.Uint64(), hi: r.Uint64()}
		switch r.IntN(4) {
		case 0:
			d.hi = ^uint64(0)
		case 1:
			d.hi = 1
		case 2:
			d.hi = 1 << 63
		}
		if d.hi == 0 {
			d.hi = 1
		}
		q, rem := divmod256(n, d)
		bn, bd := toBig(n[:]), toBig([]uint64{d.lo, d.hi})
		wq, wr := new(big.Int).QuoRem(bn, bd, new(big.Int))
		if toBig(q[:]).Cmp(wq) != 0 || toBig([]uint64{rem.lo, rem.hi}).Cmp(wr) != 0 {
			t.Fatalf("divmod256(%s, %s) = %s r %s, want %s r %s", bn, bd, toBig(q[:]), toBig([]uint64{rem.lo, rem.hi}), wq, wr)
		}
		if d.lo != 0 {
			w := newWordDivisor(d.lo)
			q1, r1 := w.div256(n)
			wq1, wr1 := new(big.Int).QuoRem(bn, new(big.Int).SetUint64(d.lo), new(big.Int))
			if toBig(q1[:]).Cmp(wq1) != 0 || r1 != wr1.Uint64() {
				t.Fatalf("div256(%s, %d) = %s r %d, want %s r %s", bn, d.lo, toBig(q1[:]), r1, wq1, wr1)
			}
			half := u128{lo: n[0], hi: n[1]}
			q2, r2 := w.div128(half)
			bh := toBig([]uint64{half.lo, half.hi})
			wq2, wr2 := new(big.Int).QuoRem(bh, new(big.Int).SetUint64(d.lo), new(big.Int))
			if toBig([]uint64{q2.lo, q2.hi}).Cmp(wq2) != 0 || r2 != wr2.Uint64() {
				t.Fatalf("div128(%s, %d) = %s r %d, want %s r %s", bh, d.lo, toBig([]uint64{q2.lo, q2.hi}), r2, wq2, wr2)
			}
		}

		for _, small := range []uint64{1, 2, 3, 7, 10, 1000, pow10_64[19], uint64(r.IntN(1000)) + 1} {
			w := newWordDivisor(small)
			q3, r3 := w.div256(n)
			wq3, wr3 := new(big.Int).QuoRem(bn, new(big.Int).SetUint64(small), new(big.Int))
			if toBig(q3[:]).Cmp(wq3) != 0 || r3 != wr3.Uint64() {
				t.Fatalf("div256(%s, %d) = %s r %d, want %s r %s", bn, small, toBig(q3[:]), r3, wq3, wr3)
			}
		}
	}
}

func TestPowerTables(t *testing.T) {
	for i := range pow10_128 {
		want := powTen(i)
		got := fromDecimal(Decimal{lo: pow10_128[i].lo, hi: pow10_128[i].hi & decCoefMask}).coef
		if i <= 34 && got.Cmp(want) != 0 {
			t.Errorf("pow10_128[%d] = %s, want %s", i, got, want)
		}
	}
	last := pow10_256[len(pow10_256)-1]
	v := new(big.Int)
	for i := 3; i >= 0; i-- {
		v.Lsh(v, 64)
		v.Or(v, new(big.Int).SetUint64(last[i]))
	}
	if v.Cmp(powTen(77)) != 0 {
		t.Errorf("pow10_256[77] = %s", v)
	}
}

func TestDecimalLayoutInvariants(t *testing.T) {

	for _, e := range []int32{DecimalEtiny, DecimalEmaxQ, 0, -1, 1, -8192, 8191} {
		d := pack(true, u128{lo: 1}, e)
		if d.Exponent() != e || !d.Negative() {
			t.Errorf("exponent %d packs as %d", e, d.Exponent())
		}
	}
	if pack(true, u128{}, 5).Negative() {
		t.Error("a zero packs with a sign")
	}
	m := toDecimal(t, maxDecimal)
	if got := fromDecimal(m); !got.equal(maxDecimal) {
		t.Errorf("the largest value round-trips as %s", got)
	}
}

func FuzzDecimalArithmetic(f *testing.F) {
	f.Add(uint64(120), uint64(0), int32(-2), uint64(3400), uint64(0), int32(-3), byte(0))
	f.Add(uint64(1), uint64(0), int32(6111), uint64(1), uint64(0), int32(-6176), byte(1))
	f.Add(^uint64(0), uint64(0x1ed09bead87c0), int32(6111), uint64(1), uint64(0), int32(0), byte(2))
	f.Fuzz(func(t *testing.T, alo, ahi uint64, aexp int32, blo, bhi uint64, bexp int32, signs byte) {
		a := dec{neg: signs&1 != 0, coef: new(big.Int).SetUint64(ahi), exp: int(aexp)}
		a.coef.Lsh(a.coef, 64).Or(a.coef, new(big.Int).SetUint64(alo)).Mod(a.coef, coefLimit)
		b := dec{neg: signs&2 != 0, coef: new(big.Int).SetUint64(bhi), exp: int(bexp)}
		b.coef.Lsh(b.coef, 64).Or(b.coef, new(big.Int).SetUint64(blo)).Mod(b.coef, coefLimit)
		a.exp, b.exp = clampExp(a.exp), clampExp(b.exp)
		a.neg = a.neg && a.coef.Sign() != 0
		b.neg = b.neg && b.coef.Sign() != 0
		for _, op := range binaryCases {
			checkBinary(t, op, a, b)
		}
	})
}

var benchSink Decimal

// benchPair measures an operation across a decimal pair.
func benchPair(b *testing.B, x, y string, op func(a, b Decimal) (Decimal, Fault)) {
	b.Helper()
	p, q := one(x), one(y)
	b.ResetTimer()
	for b.Loop() {
		benchSink, _ = op(p, q)
	}
}

func BenchmarkDecimalAddMonetary(b *testing.B) { benchPair(b, "1234.56", "78.90", AddDecimal) }
func BenchmarkDecimalAddDelta2(b *testing.B)   { benchPair(b, "1234.5", "7.890", AddDecimal) }
func BenchmarkDecimalAddDelta34(b *testing.B) {
	benchPair(b, "1E+34", "1", AddDecimal)
}
func BenchmarkDecimalAddDelta40(b *testing.B) {
	benchPair(b, "1E+40", "1", AddDecimal)
}
func BenchmarkDecimalAddWideGap(b *testing.B) {
	benchPair(b, "1E+6000", "1E-6000", AddDecimal)
}
func BenchmarkDecimalSubCancel(b *testing.B) {
	benchPair(b, "1.000000000000000000000000000000001", "1.000000000000000000000000000000000", SubDecimal)
}
func BenchmarkDecimalMulSmall(b *testing.B) { benchPair(b, "1.20", "3.400", MulDecimal) }
func BenchmarkDecimalMul34x34(b *testing.B) {
	benchPair(b, "9999999999999999999999999999999999", "9999999999999999999999999999999999", MulDecimal)
}
func BenchmarkDecimalDivExactSmall(b *testing.B) { benchPair(b, "8.00", "2.0", DivDecimal) }
func BenchmarkDecimalDivExact(b *testing.B)      { benchPair(b, "1.0", "8", DivDecimal) }
func BenchmarkDecimalDivRecurring(b *testing.B)  { benchPair(b, "1", "3", DivDecimal) }
func BenchmarkDecimalDivWide(b *testing.B) {
	benchPair(b, "9999999999999999999999999999999999", "3333333333333333333333333333333333", DivDecimal)
}
func BenchmarkDecimalRemCommon(b *testing.B)  { benchPair(b, "1234.56", "7.89", RemDecimal) }
func BenchmarkDecimalRemNearby(b *testing.B)  { benchPair(b, "123456.789", "7", RemDecimal) }
func BenchmarkDecimalRemExtreme(b *testing.B) { benchPair(b, "1E+6111", "7.0", RemDecimal) }
func BenchmarkDecimalRemWide(b *testing.B) {
	benchPair(b, "1E+6111", "9999999999999999999999999999999999", RemDecimal)
}

func BenchmarkDecimalDisplay(b *testing.B) {
	d := one("1234567890123456789012345678901234E-20")
	var sink string
	for b.Loop() {
		sink = d.String()
	}
	_ = sink
}

func BenchmarkDecimalDisplayShort(b *testing.B) {
	d := one("4.600")
	var sink string
	for b.Loop() {
		sink = d.String()
	}
	_ = sink
}

func BenchmarkDecimalParse(b *testing.B) {
	for b.Loop() {
		benchSink, _ = ParseDecimal("1234567890.123456789012345678901234")
	}
}

func BenchmarkDecimalBulkSum(b *testing.B) {
	xs := make([]Decimal, 1000)
	for i := range xs {
		xs[i] = one(fmt.Sprintf("%d.%02d", i, i%100))
	}
	b.ResetTimer()
	for b.Loop() {
		var sum Decimal
		for _, x := range xs {
			sum, _ = AddDecimal(sum, x)
		}
		benchSink = sum
	}
}
