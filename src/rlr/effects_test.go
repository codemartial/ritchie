package rlr

import (
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// installFixed installs a clock fixed at instant and an entropy source counting upward from start.
func installFixed(t *testing.T, instant time.Time, start uint64) {
	t.Helper()
	oldNow, oldEntropy := now, entropy
	next := start
	Install(func() time.Time { return instant }, func() uint64 {
		next++
		return next - 1
	})
	t.Cleanup(func() { now, entropy = oldNow, oldEntropy })
}

// seedOf returns a 32-byte seed of b.
func seedOf(b byte, i int, alt byte) []uint8 {
	seed := make([]uint8, 32)
	for k := range seed {
		seed[k] = b
	}
	if i >= 0 && i < 32 {
		seed[i] = alt
	}
	return seed
}

func TestNowReadsTheBoundClockInUTC(t *testing.T) {
	zone := time.FixedZone("east", 10*60*60)
	installFixed(t, time.Date(2026, 9, 23, 19, 5, 2, 500, zone), 0)
	if got := Now().String(); got != "2026-09-23T09:05:02.0000005Z" {
		t.Errorf("Now() = %s, want the UTC instant under offset zero", got)
	}
	installFixed(t, time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), 0)
	if got := Now().String(); got != "0000-01-01T00:00:00Z" {
		t.Errorf("Now() at the domain floor = %s", got)
	}
	installFixed(t, time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), 0)
	if got := Now().String(); got != "9999-12-31T23:59:59.999999999Z" {
		t.Errorf("Now() at the domain ceiling = %s", got)
	}
}

func TestNowOutsideTheDateTimeDomainInvalidatesTheRun(t *testing.T) {
	out, exits := stubProcess(t)
	installFixed(t, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), 0)
	Now()
	if len(*exits) != 1 || (*exits)[0] != 2 {
		t.Errorf("exits = %v, want one status 2", *exits)
	}
	if !strings.Contains(out.String(), "year 10000") {
		t.Errorf("diagnostic = %q, want the offending year", out.String())
	}
}

func TestBitSetDisplaysSixteenLowercaseHexDigits(t *testing.T) {
	for _, tt := range []struct {
		v    BitSet
		want string
	}{
		{0, "0x0000000000000000"},
		{0xff, "0x00000000000000ff"},
		{0xABCDEF, "0x0000000000abcdef"},
		{math.MaxUint64, "0xffffffffffffffff"},
		{1 << 63, "0x8000000000000000"},
	} {
		if got := tt.v.String(); got != tt.want {
			t.Errorf("BitSet(%d) displays %s, want %s", uint64(tt.v), got, tt.want)
		}
		if got := render(tt.v, ""); got != tt.want {
			t.Errorf("render(BitSet(%d)) = %s, want %s", uint64(tt.v), got, tt.want)
		}
	}
	if got := render(Elements([]BitSet{1, 2}, Plain[BitSet]), ""); got != "[0x0000000000000001, 0x0000000000000002]" {
		t.Errorf("array display = %s", got)
	}
}

func TestSeededGeneratorsReplayWithinARelease(t *testing.T) {
	a, b := SeedRandom(seedOf(7, -1, 0)), SeedRandom(seedOf(7, -1, 0))
	for i := range 64 {
		if x, y := Bits(&a), Bits(&b); x != y {
			t.Fatalf("draw %d: bits %s and %s differ under one seed", i, x, y)
		}
		if x, y := Ratio(&a), Ratio(&b); x != y {
			t.Fatalf("draw %d: ratios %v and %v differ under one seed", i, x, y)
		}
		if x, y := Range(&a, -5, 1000), Range(&b, -5, 1000); x != y {
			t.Fatalf("draw %d: ranges %d and %d differ under one seed", i, x, y)
		}
	}
}

func TestEverySeedByteParticipates(t *testing.T) {
	base := SeedRandom(seedOf(7, -1, 0))
	first := Bits(&base)
	for i := range 32 {
		g := SeedRandom(seedOf(7, i, 8))
		if Bits(&g) == first {
			t.Errorf("changing seed byte %d leaves the first draw at %s", i, first)
		}
	}

	forward, backward := make([]uint8, 32), make([]uint8, 32)
	for i := range forward {
		forward[i], backward[31-i] = uint8(i), uint8(i)
	}
	f, r := SeedRandom(forward), SeedRandom(backward)
	if Bits(&f) == Bits(&r) {
		t.Error("reversed seed bytes reproduce the first draw")
	}
}

func TestGeneratorCopiesContinueIndependently(t *testing.T) {
	g := SeedRandom(seedOf(3, -1, 0))
	copied := g
	first := Bits(&g)
	if got := Bits(&copied); got != first {
		t.Errorf("the copy's first draw is %s, want %s, the original's first draw", got, first)
	}
	second := Bits(&g)
	if got := Bits(&copied); got != second {
		t.Errorf("the copy's second draw is %s, want %s", got, second)
	}
	if first == second {
		t.Error("two consecutive draws agree")
	}
}

func TestDisplayRecordsProvenanceUnchangedByDraws(t *testing.T) {
	installFixed(t, time.Time{}, 12345)
	fresh := NewRandom()
	if got := fresh.Display(""); got != "r::Random {}" {
		t.Errorf("seedless display = %q", got)
	}
	Bits(&fresh)
	if got := render(fresh, ""); got != "r::Random {}" {
		t.Errorf("seedless display after a draw = %q", got)
	}
	seed := make([]uint8, 32)
	for i := range seed {
		seed[i] = uint8(i)
	}
	g := SeedRandom(seed)
	want := "r::Random { seed: 0x000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f }"
	if got := g.Display(""); got != want {
		t.Errorf("seeded display = %q, want %q", got, want)
	}
	Range(&g, 1, 100)
	Ratio(&g)
	if got := render(g, "m"); got != want {
		t.Errorf("seeded display after draws = %q, want %q", got, want)
	}
}

func TestSeedlessGeneratorsFollowTheBoundEntropy(t *testing.T) {
	installFixed(t, time.Time{}, 100)
	a := NewRandom()
	installFixed(t, time.Time{}, 100)
	b := NewRandom()
	installFixed(t, time.Time{}, 101)
	c := NewRandom()
	if x, y := Bits(&a), Bits(&b); x != y {
		t.Errorf("equal entropy streams start generators drawing %s and %s", x, y)
	}
	if x, y := Bits(&a), Bits(&c); x == y {
		t.Errorf("different entropy streams start generators drawing the same %s", x)
	}

	installFixed(t, time.Time{}, 0)
	NewRandom()
	if next := entropy(); next != 4 {
		t.Errorf("a seedless generator consumed %d entropy words, want 4", next)
	}
}

func TestRatioLiesInTheHalfOpenUnitInterval(t *testing.T) {
	g := SeedRandom(seedOf(9, -1, 0))
	var low, high bool
	for range 100000 {
		f := Ratio(&g)
		if f < 0 || f >= 1 || math.IsNaN(f) {
			t.Fatalf("ratio %v leaves [0, 1)", f)
		}
		low = low || f < 0.01
		high = high || f > 0.99
	}
	if !low || !high {
		t.Errorf("ratios reach neither end of the interval: low %v high %v", low, high)
	}
}

func TestRangeCoversEveryEndpointPair(t *testing.T) {
	g := SeedRandom(seedOf(11, -1, 0))
	const lo, hi = math.MinInt64, math.MaxInt64
	for _, tt := range []struct {
		from, to     int64
		min, max     int64
		name         string
		distinctions int
	}{
		{0, 4, 0, 3, "small ascending", 4},
		{4, 0, 1, 4, "small descending", 4},
		{-3, 4, -3, 3, "crossing zero", 7},
		{4, -3, -2, 4, "crossing zero descending", 7},
		{lo, hi, lo, hi - 1, "full ascending", 2},
		{hi, lo, lo + 1, hi, "full descending", 2},
		{lo, lo + 1, lo, lo, "one at the floor", 1},
		{hi, hi - 1, hi, hi, "one at the ceiling", 1},
		{hi - 1, hi, hi - 1, hi - 1, "one below the ceiling", 1},
		{lo + 1, lo, lo + 1, lo + 1, "one above the floor", 1},
		{lo, 0, lo, -1, "negative half", 2},
		{0, lo, lo + 1, 0, "negative half descending", 2},
	} {
		seen := map[int64]bool{}
		for range 2000 {
			n := Range(&g, tt.from, tt.to)
			if n < tt.min || n > tt.max {
				t.Fatalf("%s: range(%d, %d) = %d leaves [%d, %d]", tt.name, tt.from, tt.to, n, tt.min, tt.max)
			}
			seen[n] = true
		}
		if len(seen) < tt.distinctions {
			t.Errorf("%s: %d distinct values in 2000 draws, want at least %d", tt.name, len(seen), tt.distinctions)
		}
	}
}

func TestRangeWithEqualEndpointsConsumesNoDraw(t *testing.T) {
	a, b := SeedRandom(seedOf(5, -1, 0)), SeedRandom(seedOf(5, -1, 0))
	for _, x := range []int64{0, 7, -7, math.MinInt64, math.MaxInt64} {
		if got := Range(&a, x, x); got != x {
			t.Errorf("range(%d, %d) = %d", x, x, got)
		}
	}
	if x, y := Bits(&a), Bits(&b); x != y {
		t.Errorf("after equal-endpoint ranges the generator draws %s where its twin draws %s", x, y)
	}
}

func TestRangeIsUniformOverASmallSpan(t *testing.T) {
	g := SeedRandom(seedOf(13, -1, 0))
	counts := make([]int, 5)
	const draws = 50000
	for range draws {
		counts[Range(&g, 10, 15)-10]++
	}
	for v, c := range counts {
		if c < draws/5-1000 || c > draws/5+1000 {
			t.Errorf("value %d drawn %d times of %d; the span is not uniform", 10+v, c, draws)
		}
	}
}

func TestSleepWaitsAtLeastItsDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		Sleep(fineDuration(int64(20*time.Millisecond)), nil)
		if elapsed := time.Since(start); elapsed != 20*time.Millisecond {
			t.Errorf("sleep of 20ms took %v", elapsed)
		}
		start = time.Now()
		Sleep(fineDuration(-int64(time.Hour)), nil)
		Sleep(Duration{v: -3<<1 | 1}, nil)
		if elapsed := time.Since(start); elapsed != 0 {
			t.Errorf("negative sleeps took %v, want none", elapsed)
		}
	})
}
