package rlr

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

// cohortOf returns an equivalent Decimal at an adjusted exponent.
func cohortOf(t testing.TB, d dec, k int) dec {
	t.Helper()
	return dec{neg: d.neg, coef: new(big.Int).Mul(d.coef, powTen(k)), exp: d.exp - k}
}

func TestDecimalCompareVectors(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.00", 0}, {"1.00", "1.0", 0}, {"0", "0.000", 0}, {"0E-6176", "0E+6111", 0},
		{"-1.0", "-1.00", 0}, {"1E+2", "100", 0}, {"100.0", "1.000E+2", 0},
		{"1.0", "1.01", -1}, {"1.01", "1.0", 1}, {"-1.0", "1.0", -1}, {"-1.0", "-2.0", 1},
		{"0", "1E-6176", -1}, {"0", "-1E-6176", 1}, {"-0.00", "0", 0},
		{"9.999999999999999999999999999999999E+6144", "9.999999999999999999999999999999998E+6144", 1},
		{"1E-6176", "2E-6176", -1}, {"1E-6143", "9.99E-6144", 1},
		{"1E+6111", "9.99E+6110", 1}, {"123456789012345678901234567890123.4", "1234567890123456789012345678901234E-1", 0},
		{"1234567890123456789012345678901234", "1234567890123456789012345678901235", -1},
		{"1E+33", "999999999999999999999999999999999.9", 1}, {"1E+34", "9999999999999999999999999999999999", 1},
		{"5", "5.000000000000000000000000000000001", -1}, {"0.5", "5E-1", 0}, {"-5E-1", "-0.5", 0},
	}
	for _, tt := range tests {
		a, b := toDecimal(t, parseDec(t, tt.a)), toDecimal(t, parseDec(t, tt.b))
		if got := CompareDecimal(a, b); got != tt.want {
			t.Errorf("CompareDecimal(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := CompareDecimal(b, a); got != -tt.want {
			t.Errorf("CompareDecimal(%s, %s) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
		if got := EqualDecimal(a, b); got != (tt.want == 0) {
			t.Errorf("EqualDecimal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want == 0)
		}
		if got := LessDecimal(a, b); got != (tt.want < 0) {
			t.Errorf("LessDecimal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want < 0)
		}
		ka, kb := KeyOfDecimal(a), KeyOfDecimal(b)
		if (ka == kb) != (tt.want == 0) {
			t.Errorf("KeyOfDecimal(%s) == KeyOfDecimal(%s) is %v, want %v", tt.a, tt.b, ka == kb, tt.want == 0)
		}
	}
}

func TestDecimalCompareDifferential(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 1))
	check := func(a, b dec) {
		t.Helper()
		da, db := toDecimal(t, a), toDecimal(t, b)
		want := a.rat().Cmp(b.rat())
		if got := CompareDecimal(da, db); got != want {
			t.Errorf("CompareDecimal(%s, %s) = %d, want %d", a, b, got, want)
		}
		if got := EqualDecimal(da, db); got != (want == 0) {
			t.Errorf("EqualDecimal(%s, %s) = %v, want %v", a, b, got, want == 0)
		}
		if got := KeyOfDecimal(da) == KeyOfDecimal(db); got != (want == 0) {
			t.Errorf("keys of %s and %s equal = %v, want %v", a, b, got, want == 0)
		}
	}
	for i := 0; i < 20000; i++ {
		a, b := randomDec(r), randomDec(r)
		check(a, b)
		check(a, a)

		if k := DecimalPrecision - len(a.coef.String()); k > 0 && a.exp-k >= DecimalEtiny {
			check(a, cohortOf(t, a, r.IntN(k)+1))
		}

		next := dec{neg: a.neg, coef: new(big.Int).Add(a.coef, big.NewInt(1)), exp: a.exp}
		if next.coef.Cmp(coefLimit) < 0 {
			check(a, next)
			if k := DecimalPrecision - len(next.coef.String()); k > 0 && next.exp-k >= DecimalEtiny {
				check(a, cohortOf(t, next, k))
			}
		}

		check(a, dec{neg: !a.neg, coef: a.coef, exp: a.exp})
		check(a, dec{coef: new(big.Int), exp: b.exp})
	}
}

func TestDecimalMinMax(t *testing.T) {
	tests := []struct {
		args     []string
		min, max string
	}{
		{[]string{"1.0", "1.00"}, "1.00", "1.0"},
		{[]string{"1.00", "1.0"}, "1.00", "1.0"},
		{[]string{"-1.0", "-1.00"}, "-1.0", "-1.00"},
		{[]string{"-1.00", "-1.0"}, "-1.0", "-1.00"},
		{[]string{"0.0", "0.00"}, "0.00", "0.0"},
		{[]string{"0E+3", "0"}, "0", "0E+3"},
		{[]string{"1.5"}, "1.5", "1.5"},
		{[]string{"1.5", "-2", "3"}, "-2", "3"},
		{[]string{"3", "1.5", "-2"}, "-2", "3"},
		{[]string{"1.00", "1.0", "1.000"}, "1.000", "1.0"},
		{[]string{"1.0", "1.000", "1.00"}, "1.000", "1.0"},
		{[]string{"1E+2", "100", "100.0"}, "100.0", "1E+2"},
		{[]string{"-1E+2", "-100", "-100.0"}, "-1E+2", "-100.0"},
		{[]string{"9.999999999999999999999999999999999E+6144", "1E-6176"}, "1E-6176", "9.999999999999999999999999999999999E+6144"},
		{[]string{"0", "-1E-6176", "1E-6176"}, "-1E-6176", "1E-6176"},
	}
	for _, tt := range tests {
		args := make([]Decimal, len(tt.args))
		for i, a := range tt.args {
			args[i] = toDecimal(t, parseDec(t, a))
		}
		if got := MinDecimal(args...); !fromDecimal(got).equal(parseDec(t, tt.min)) {
			t.Errorf("MinDecimal(%v) = %s, want %s", tt.args, got, tt.min)
		}
		if got := MaxDecimal(args...); !fromDecimal(got).equal(parseDec(t, tt.max)) {
			t.Errorf("MaxDecimal(%v) = %s, want %s", tt.args, got, tt.max)
		}
	}
}

func TestDecimalMinMaxDifferential(t *testing.T) {
	r := rand.New(rand.NewPCG(32, 1))
	for i := 0; i < 5000; i++ {
		n := r.IntN(4) + 1
		vals := make([]dec, n)
		args := make([]Decimal, n)
		for j := range vals {
			vals[j] = randomDec(r)
			if j > 0 && r.IntN(3) == 0 {
				if k := DecimalPrecision - len(vals[0].coef.String()); k > 0 && vals[0].exp-k >= DecimalEtiny {
					vals[j] = cohortOf(t, vals[0], r.IntN(k)+1)
				}
			}
			args[j] = toDecimal(t, vals[j])
		}
		for _, min := range []bool{true, false} {
			var got Decimal
			if min {
				got = MinDecimal(args...)
			} else {
				got = MaxDecimal(args...)
			}
			extreme := vals[0].rat()
			for _, v := range vals[1:] {
				if c := v.rat().Cmp(extreme); c < 0 && min || c > 0 && !min {
					extreme = v.rat()
				}
			}
			g := fromDecimal(got)
			if g.rat().Cmp(extreme) != 0 {
				t.Fatalf("min=%v over %v = %s, not the extreme value", min, vals, g)
			}
			for _, v := range vals {
				if v.rat().Cmp(extreme) != 0 {
					continue
				}
				wantLesser := min
				if v.neg {
					wantLesser = !wantLesser
				}
				if wantLesser && v.exp < g.exp || !wantLesser && v.exp > g.exp {
					t.Fatalf("min=%v over %v = %s, but %s has the preferred exponent", min, vals, g, v)
				}
			}
		}
	}
}

func TestDecimalKeyProjection(t *testing.T) {
	for _, s := range []string{"0", "0.00", "0E+6111", "0E-6176"} {
		if k := KeyOfDecimal(toDecimal(t, parseDec(t, s))); k != (DecimalKey{}) {
			t.Errorf("KeyOfDecimal(%s) = %v, want the zero key", s, k)
		}
	}
	tests := []struct {
		in, coef string
		exp      int
	}{
		{"1.00", "1", 0}, {"100", "1", 2}, {"1.0E+2", "1", 2}, {"120.0", "12", 1}, {"-0.50", "-5", -1},
		{"1000000000000000000000000000000000E+6111", "1", 6144}, {"1.000000000000000000000000000000000", "1", 0},
		{"12345678901234567890123456789012.00", "12345678901234567890123456789012", 0},
	}
	for _, tt := range tests {
		got := KeyOfDecimal(toDecimal(t, parseDec(t, tt.in)))
		coef, _ := new(big.Int).SetString(tt.coef, 10)
		want := dec{neg: coef.Sign() < 0, coef: coef.Abs(coef), exp: tt.exp}
		if g := fromDecimal(Decimal(got)); !g.equal(want) {
			t.Errorf("KeyOfDecimal(%s) = %s, want %s", tt.in, g, want)
		}
		if want.exp <= DecimalEmaxQ && got != KeyOfDecimal(toDecimal(t, want)) {
			t.Errorf("KeyOfDecimal(%s) differs from the projection of %s", tt.in, want)
		}
	}
	if d := toDecimal(t, parseDec(t, "1.5")); Decimal(KeyOfDecimal(d)) != d {
		t.Error("a reduced value's key is not its own bits")
	}
}

func BenchmarkDecimalCompareCohort(b *testing.B) {
	x, y := one("1.20"), one("1.2000")
	for b.Loop() {
		CompareDecimal(x, y)
	}
}

func BenchmarkDecimalCompareOrdered(b *testing.B) {
	x, y := one("1234.56"), one("1234.57")
	for b.Loop() {
		CompareDecimal(x, y)
	}
}

func BenchmarkDecimalEqualGap(b *testing.B) {
	x, y := one("1E+6111"), one("9.99E+6110")
	for b.Loop() {
		EqualDecimal(x, y)
	}
}

func BenchmarkDecimalKey(b *testing.B) {
	x := one("1234.5600")
	for b.Loop() {
		KeyOfDecimal(x)
	}
}

func BenchmarkDecimalMin(b *testing.B) {
	x, y, z := one("1.20"), one("1.2000"), NegDecimal(one("3.5"))
	for b.Loop() {
		MinDecimal(x, y, z)
	}
}
