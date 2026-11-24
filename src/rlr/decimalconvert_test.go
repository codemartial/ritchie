package rlr

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"testing"
)

// oracleFromFloat returns the exact conversion of f through rational arithmetic.
func oracleFromFloat(f float64) dec {
	v := new(big.Rat).SetFloat64(f)
	d, overflow := oracleRound(v, 0)
	if overflow {
		panic("a binary64 value overflowed decimal128")
	}
	return d
}

// oracleToFloat rounds an oracle rational to binary64.
func oracleToFloat(v *big.Rat) (float64, bool) {
	if v.Sign() == 0 {
		return 0, false
	}
	neg := v.Sign() < 0
	mag := new(big.Rat).Abs(v)

	q := mag.Num().BitLen() - mag.Denom().BitLen() - 53
	if q < -1074 {
		q = -1074
	}
	scale := func(q int) *big.Rat {
		if q >= 0 {
			return new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(q)))
		}
		return new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(-q)))
	}
	n := new(big.Rat).Quo(mag, scale(q))
	if n.Cmp(new(big.Rat).SetInt64(1<<53)) >= 0 {
		q++
		n.Quo(mag, scale(q))
	} else if n.Cmp(new(big.Rat).SetInt64(1<<52)) < 0 && q > -1074 {
		q--
		n.Quo(mag, scale(q))
	}
	floor := new(big.Int).Quo(n.Num(), n.Denom())
	rem := new(big.Rat).Sub(n, new(big.Rat).SetInt(floor))
	if c := rem.Cmp(big.NewRat(1, 2)); c > 0 || c == 0 && floor.Bit(0) == 1 {
		floor.Add(floor, big.NewInt(1))
	}
	if floor.BitLen() > 53 && q >= 971 || q > 971 {
		return 0, true
	}
	f := math.Ldexp(float64(floor.Int64()), q)
	if math.IsInf(f, 0) {
		return 0, true
	}
	if neg {
		f = -f
	}
	return f, false
}

func TestDecimalFromIntIsExact(t *testing.T) {
	for _, i := range []int64{0, 1, -1, 7, -7, 1 << 53, math.MaxInt64, math.MinInt64, 1000000} {
		want := dec{neg: i < 0, coef: new(big.Int).Abs(big.NewInt(i)), exp: 0}
		if got := fromDecimal(DecimalFromInt(i)); !got.equal(want) {
			t.Errorf("DecimalFromInt(%d) = %s, want %s", i, got, want)
		}
	}
}

func TestDecimalFromFloatVectors(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"}, {math.Copysign(0, -1), "0"}, {1, "1"}, {-1, "-1"}, {0.5, "0.5"}, {2, "2"}, {100, "100"},
		{0.1, "0.1000000000000000055511151231257827"},
		{0.25, "0.25"}, {1e22, "10000000000000000000000"}, {1e23, "99999999999999991611392"},
		{9007199254740992, "9007199254740992"}, {9007199254740993, "9007199254740992"},
		{math.MaxFloat64, "1.797693134862315708145274237317044E+308"},
		{math.SmallestNonzeroFloat64, "4.940656458412465441765687928682214E-324"},
		{2.2250738585072014e-308, "2.225073858507201383090232717332404E-308"},
		{2.2250738585072009e-308, "2.225073858507200889024586876085860E-308"},
		{1.5, "1.5"}, {-2.75, "-2.75"}, {123456789.125, "123456789.125"},
		{12.34, "12.33999999999999985789145284797996"},
		{math.Ldexp(1, 100), "1267650600228229401496703205376"},
		{math.Ldexp(1, 200), "1.606938044258990275541962092341163E+60"},
		{math.Ldexp(1, -100), "7.888609052210118054117285652827862E-31"},
		{math.Ldexp(3, -48), "1.065814103640150278806686401367188E-14"},
	}
	for _, tt := range tests {
		want := parseDec(t, tt.want)
		got, fault := DecimalFromFloat(tt.in)
		if fault != NoFault {
			t.Errorf("DecimalFromFloat(%g) = %v", tt.in, fault)
			continue
		}
		if g := fromDecimal(got); !g.equal(oracleFromFloat(tt.in)) || !g.equal(want) {
			t.Errorf("DecimalFromFloat(%g) = %s, want %s (oracle %s)", tt.in, g, tt.want, oracleFromFloat(tt.in))
		}
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, fault := DecimalFromFloat(f); fault != OutOfRange {
			t.Errorf("DecimalFromFloat(%g) = %v, want OutOfRange", f, fault)
		}
	}
}

func TestDecimalFromFloatDifferential(t *testing.T) {
	r := rand.New(rand.NewPCG(55, 3))
	check := func(f float64) {
		t.Helper()
		got, fault := DecimalFromFloat(f)
		if fault != NoFault {
			t.Errorf("DecimalFromFloat(%v) = %v", f, fault)
			return
		}
		if g, want := fromDecimal(got), oracleFromFloat(f); !g.equal(want) {
			t.Errorf("DecimalFromFloat(%v = %b) = %s, want %s", f, f, g, want)
		}
	}
	for exp := uint64(0); exp < 0x7ff; exp++ {
		for _, mant := range []uint64{0, 1, 1<<52 - 1, 1 << 51, r.Uint64() & (1<<52 - 1), r.Uint64() & (1<<52 - 1), 5, 1<<52 - 5} {
			if exp == 0 && mant == 0 {
				continue
			}
			for _, sign := range []uint64{0, 1} {
				check(math.Float64frombits(sign<<63 | exp<<52 | mant))
			}
		}
	}
	for e := -1074; e <= 1023; e++ {
		check(math.Ldexp(1, e))
		if e < 1023 {
			check(math.Ldexp(3, e))
		}
	}
	for p := -320; p <= 308; p++ {
		f, _ := strconv.ParseFloat("1e"+strconv.Itoa(p), 64)
		check(f)
		check(math.Nextafter(f, 0))
		check(math.Nextafter(f, math.Inf(1)))
	}
	for i := 0; i < 20000; i++ {
		f := math.Float64frombits(r.Uint64())
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		check(f)
	}
	for i := 0; i < 20000; i++ {
		check(float64(r.IntN(100000000)) / 100)
		check(r.Float64() * 1000)
	}
}

func TestFloatFromDecimalVectors(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"0", 0}, {"0.00", 0}, {"0E-6176", 0}, {"1", 1}, {"-1", -1}, {"0.5", 0.5}, {"0.1", 0.1},
		{"12.34", 12.34}, {"1E+22", 1e22}, {"1E+23", 1e23}, {"123456789012345678901234567890.5", 123456789012345678901234567890.5},
		{"9007199254740993", 9007199254740992}, {"9007199254740995", 9007199254740996},
		{"9007199254740993.000000000000000001", 9007199254740994},
		{"1.7976931348623157E+308", math.MaxFloat64}, {"1.7976931348623158E+308", math.MaxFloat64},
		{"4.9E-324", math.SmallestNonzeroFloat64}, {"2.4703282292062328E-324", math.SmallestNonzeroFloat64},
		{"2.4703282292062327E-324", 0}, {"-2.4703282292062327E-324", math.Copysign(0, -1)},
		{"1E-6176", 0}, {"-1E-6176", math.Copysign(0, -1)}, {"1E-400", 0},
		{"2.2250738585072014E-308", 2.2250738585072014e-308}, {"2.2250738585072011E-308", 2.225073858507201e-308},
		{"0.1000000000000000055511151231257827", 0.1},
		{"1234567890123456789012345678901234E+6", 1.234567890123456789012345678901234e39},
	}
	for _, tt := range tests {
		got, fault := FloatFromDecimal(toDecimal(t, parseDec(t, tt.in)))
		if fault != NoFault {
			t.Errorf("FloatFromDecimal(%s) = %v", tt.in, fault)
			continue
		}
		if math.Float64bits(got) != math.Float64bits(tt.want) {
			t.Errorf("FloatFromDecimal(%s) = %v (%b), want %v (%b)", tt.in, got, got, tt.want, tt.want)
		}
	}
	for _, s := range []string{"1.797693134862315808E+308", "1E+309", "9.999999999999999999999999999999999E+6144", "-1E+400", "1E+6111"} {
		if _, fault := FloatFromDecimal(toDecimal(t, parseDec(t, s))); fault != OutOfRange {
			t.Errorf("FloatFromDecimal(%s) = %v, want OutOfRange", s, fault)
		}
	}
}

func TestFloatFromDecimalDifferential(t *testing.T) {
	r := rand.New(rand.NewPCG(56, 4))
	check := func(d dec) {
		t.Helper()
		want, overflow := oracleToFloat(d.rat())
		got, fault := FloatFromDecimal(toDecimal(t, d))
		switch {
		case overflow && fault != OutOfRange:
			t.Errorf("FloatFromDecimal(%s) = %v, %v, want OutOfRange", d, got, fault)
		case !overflow && fault != NoFault:
			t.Errorf("FloatFromDecimal(%s) = %v, want %v", d, fault, want)
		case !overflow && math.Float64bits(got) != math.Float64bits(want):
			t.Errorf("FloatFromDecimal(%s) = %v (%b), want %v (%b)", d, got, got, want, want)
		}
	}

	for i := 0; i < 5000; i++ {
		f := math.Float64frombits(r.Uint64())
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		exact := oracleFromFloat(f)
		if len(exact.coef.String()) <= DecimalPrecision {
			check(exact)
		}
		next := math.Nextafter(f, math.Inf(1))
		if math.IsInf(next, 0) {
			continue
		}
		mid := new(big.Rat).Add(new(big.Rat).SetFloat64(f), new(big.Rat).SetFloat64(next))
		mid.Quo(mid, big.NewRat(2, 1))
		if m, overflow := oracleRound(mid, 0); !overflow && len(m.coef.String()) <= DecimalPrecision {
			check(m)
			above := dec{neg: m.neg, coef: new(big.Int).Add(m.coef, big.NewInt(1)), exp: m.exp}
			below := dec{neg: m.neg, coef: new(big.Int).Sub(m.coef, big.NewInt(1)), exp: m.exp}
			if above.coef.Cmp(coefLimit) < 0 {
				check(above)
			}
			check(below)
		}
	}

	for i := 0; i < 20000; i++ {
		d := randomDec(r)
		switch r.IntN(4) {
		case 0:
			d.exp = r.IntN(700) - 360
		case 1:
			d.exp = r.IntN(60) - 30
		}
		if d.exp < DecimalEtiny {
			d.exp = DecimalEtiny
		}
		if d.exp > DecimalEmaxQ {
			d.exp = DecimalEmaxQ
		}
		check(d)
		check(dec{neg: !d.neg, coef: d.coef, exp: d.exp})
	}
	for _, s := range []string{"1.7976931348623157E+308", "1.797693134862315807937E+308", "1.797693134862315807938E+308",
		"2.2250738585072014E-308", "4.9406564584124654E-324", "2.4703282292062327E-324", "2.4703282292062328E-324",
		"1E-6176", "9.999999999999999999999999999999999E+6144"} {
		check(parseDec(t, s))
	}
}

// FuzzDecimalFromFloat exercises arbitrary bit patterns convert as the rational oracle does.
func FuzzDecimalFromFloat(f *testing.F) {
	f.Add(uint64(0x3FB999999999999A))
	f.Add(uint64(1))
	f.Add(uint64(0x7FEFFFFFFFFFFFFF))
	f.Fuzz(func(t *testing.T, bits uint64) {
		x := math.Float64frombits(bits)
		got, fault := DecimalFromFloat(x)
		if math.IsNaN(x) || math.IsInf(x, 0) {
			if fault != OutOfRange {
				t.Fatalf("DecimalFromFloat(%v) = %v, want OutOfRange", x, fault)
			}
			return
		}
		if fault != NoFault {
			t.Fatalf("DecimalFromFloat(%v) = %v", x, fault)
		}
		if g, want := fromDecimal(got), oracleFromFloat(x); !g.equal(want) {
			t.Fatalf("DecimalFromFloat(%v) = %s, want %s", x, g, want)
		}
	})
}

// FuzzFloatFromDecimal exercises arbitrary coefficients and exponents convert as the rational oracle does.
func FuzzFloatFromDecimal(f *testing.F) {
	f.Add(uint64(0), uint64(1234), int32(-2))
	f.Add(uint64(0x1ED09BEAD87C0), uint64(0x378D8E63FFFFFFFF), int32(6111))
	f.Fuzz(func(t *testing.T, hi, lo uint64, exp int32) {
		d, fault := MakeDecimal(false, hi, lo, exp)
		if fault != NoFault {
			return
		}
		want, overflow := oracleToFloat(fromDecimal(d).rat())
		got, fault := FloatFromDecimal(d)
		switch {
		case overflow != (fault == OutOfRange):
			t.Fatalf("FloatFromDecimal(%s) = %v, %v; oracle overflow %v", d, got, fault, overflow)
		case !overflow && math.Float64bits(got) != math.Float64bits(want):
			t.Fatalf("FloatFromDecimal(%s) = %v, want %v", d, got, want)
		}
	})
}

// strconvDecimalFromFloat returns the standard-library candidate kernel.
func strconvDecimalFromFloat(f float64) (Decimal, Fault) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Decimal{}, OutOfRange
	}
	var buf [64]byte
	text := strconv.AppendFloat(buf[:0], math.Abs(f), 'e', DecimalPrecision-1, 64)
	d, fault := ParseDecimal(string(text))
	if f < 0 {
		d = NegDecimal(d)
	}
	return d, fault
}

// strconvFloatFromDecimal returns the standard-library candidate kernel without the exact fast path.
func strconvFloatFromDecimal(d Decimal) (float64, Fault) {
	f, err := strconv.ParseFloat(d.String(), 64)
	if err != nil {
		return 0, OutOfRange
	}
	return f, NoFault
}

func TestConversionCandidatesAgree(t *testing.T) {
	r := rand.New(rand.NewPCG(57, 5))
	for i := 0; i < 5000; i++ {
		f := math.Float64frombits(r.Uint64())
		if math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		got, _ := DecimalFromFloat(f)
		alt, _ := strconvDecimalFromFloat(f)
		if len(fromDecimal(got).coef.String()) == DecimalPrecision && got != alt {
			t.Errorf("DecimalFromFloat(%v) = %s, strconv candidate %s", f, got, alt)
		}
		d := randomDec(r)
		d.exp = r.IntN(700) - 360
		x, fx := FloatFromDecimal(toDecimal(t, d))
		y, fy := strconvFloatFromDecimal(toDecimal(t, d))
		if fx != fy || math.Float64bits(x) != math.Float64bits(y) {
			t.Errorf("FloatFromDecimal(%s) = %v %v, strconv candidate %v %v", d, x, fx, y, fy)
		}
	}
}

var conversionInputs = []struct {
	name string
	f    float64
	d    string
}{
	{"Monetary", 12.34, "12.34"},
	{"Integer", 1234567, "1234567"},
	{"PowerOfTwo", math.Ldexp(1, 60), "1152921504606846976"},
	{"Inexact34", 0.1, "1.000000000000000055511151231257827E-34"},
	{"Large", 1.5e300, "1.5E+300"},
	{"MaxFloat", math.MaxFloat64, "1.7976931348623157E+308"},
	{"Small", 1.5e-300, "1.5E-300"},
	{"Subnormal", math.SmallestNonzeroFloat64, "4.9E-324"},
	{"NormalBoundary", 2.2250738585072014e-308, "2.2250738585072014E-308"},
	{"Digits34", 1234.5678, "1234567890123456789012345678901234E-30"},
}

func BenchmarkDecimalFromFloat(b *testing.B) {
	for _, in := range conversionInputs {
		b.Run(in.name, func(b *testing.B) {
			for b.Loop() {
				DecimalFromFloat(in.f)
			}
		})
		b.Run(in.name+"/strconv", func(b *testing.B) {
			for b.Loop() {
				strconvDecimalFromFloat(in.f)
			}
		})
	}
}

func BenchmarkFloatFromDecimal(b *testing.B) {
	for _, in := range conversionInputs {
		d := one(in.d)
		b.Run(in.name, func(b *testing.B) {
			for b.Loop() {
				FloatFromDecimal(d)
			}
		})
		b.Run(in.name+"/strconv", func(b *testing.B) {
			for b.Loop() {
				strconvFloatFromDecimal(d)
			}
		})
	}
	overflow := one("1E+400")
	b.Run("Overflow", func(b *testing.B) {
		for b.Loop() {
			FloatFromDecimal(overflow)
		}
	})
}

func BenchmarkConversionBulk(b *testing.B) {
	r := rand.New(rand.NewPCG(58, 6))
	fs := make([]float64, 1000)
	ds := make([]Decimal, 1000)
	for i := range fs {
		fs[i] = float64(r.IntN(100000000)) / 100
		ds[i], _ = DecimalFromFloat(fs[i])
	}
	b.Run("FromFloat", func(b *testing.B) {
		for b.Loop() {
			for _, f := range fs {
				DecimalFromFloat(f)
			}
		}
	})
	b.Run("ToFloat", func(b *testing.B) {
		for b.Loop() {
			for _, d := range ds {
				FloatFromDecimal(d)
			}
		}
	})
	b.Run("RandomFromFloat", func(b *testing.B) {
		rs := make([]float64, 1000)
		for i := range rs {
			for {
				rs[i] = math.Float64frombits(r.Uint64())
				if !math.IsNaN(rs[i]) && !math.IsInf(rs[i], 0) {
					break
				}
			}
		}
		for b.Loop() {
			for _, f := range rs {
				DecimalFromFloat(f)
			}
		}
	})
}
