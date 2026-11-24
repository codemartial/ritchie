// This file implements the standard library's effect kernel.
package rlr

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"math/rand/v2"
	"strconv"
	"time"

	"ritchie.dev/rlr/mesh"
)

var (
	now     func() time.Time
	entropy func() uint64
)

// Install sets the clock and entropy sources of the process.
func Install(clock func() time.Time, source func() uint64) {
	now, entropy = clock, source
}

func Now() DateTime { return reading(0) }

// Sleep suspends the caller for at least d and returns at once for a d that is not positive, where the cancellation of the task whose body the caller runs, or the cancel signal of the caller of its invocation, ends the body at the sleep when the runtime contains host faults and ends only the sleep otherwise.
func Sleep(d Duration, inv Invocation) {
	var span time.Duration
	switch {
	case !d.Coarse():
		span = time.Duration(d.Payload())
	case d.Payload() > 0:
		span = math.MaxInt64
	}
	cancelled, stopped := inv.cancelled(), inv.stopped()
	if (cancelled == nil && stopped == nil) || span <= 0 {
		time.Sleep(span)
		return
	}
	timer := time.NewTimer(span)
	defer timer.Stop()
	select {
	case <-timer.C:
		return
	case <-cancelled:
	case <-stopped:
	}
	if inv.inv != nil && inv.inv.Contains() {
		panic(mesh.Halt{})
	}
}

// Clock represents r.
type Clock struct{ offset int16 }

func ClockWithOffset(offset Duration) (Clock, Fault) {
	minutes, ok := offsetMinutes(offset)
	if !ok {
		return Clock{}, OutOfRange
	}
	return Clock{offset: int16(minutes)}, NoFault
}

// ClockSleep suspends the caller for at least d through a clock value, whose offset does not change the span.
func ClockSleep(c Clock, d Duration, inv Invocation) { Sleep(d, inv) }

func ClockTime(c Clock) DateTime { return reading(c.offset) }

func ClockOffset(c Clock) Duration { return fineDuration(int64(c.offset) * nanosPerMinute) }

// reading returns the bound clock's instant under offset.
func reading(offset int16) DateTime {
	t := now().UTC()
	dt := DateTime{sec: t.Unix(), nsec: int32(t.Nanosecond()), offset: offset}
	y := int64(t.Year())
	if y >= MinYear && y <= MaxYear {

		y = localYear(dt)
	}
	if y < MinDateTimeYear || y > MaxDateTimeYear {
		io.WriteString(Diag, "ritchie runtime: the bound clock reads year "+strconv.FormatInt(y, 10)+", outside the DateTime domain\n")
		Exit(2)
	}
	return dt
}

// Display returns the descriptive display of a clock.
func (c Clock) Display(string) string {
	return string(appendOffset([]byte("r::Clock { offset: "), c.offset)) + " }"
}

// BitSet represents the nominal 64-bit bit pattern.
type BitSet uint64

// String returns the fixed-width display of a BitSet.
func (b BitSet) String() string {
	digits := strconv.FormatUint(uint64(b), 16)
	return "0x" + hexPadding[len(digits):] + digits
}

const hexPadding = "0000000000000000"

// Random represents r.
type Random struct {
	state  rand.ChaCha8
	seeded bool
	seed   [32]byte
}

func NewRandom() Random {
	var seed [32]byte
	for i := 0; i < len(seed); i += 8 {
		binary.LittleEndian.PutUint64(seed[i:], entropy())
	}
	var g Random
	g.state.Seed(seed)
	return g
}

func SeedRandom(seed []uint8) Random {
	g := Random{seeded: true}
	copy(g.seed[:], seed)
	g.state.Seed(g.seed)
	return g
}

func Bits(g *Random) BitSet { return BitSet(g.state.Uint64()) }

func Ratio(g *Random) float64 { return rand.New(&g.state).Float64() }

func Range(g *Random, from, to int64) int64 {
	if from == to {
		return from
	}

	if from < to {
		span := uint64(to) - uint64(from)
		return int64(uint64(from) + rand.New(&g.state).Uint64N(span))
	}
	span := uint64(from) - uint64(to)
	return int64(uint64(from) - rand.New(&g.state).Uint64N(span))
}

// Display returns the provenance display of a generator.
func (g Random) Display(string) string {
	if !g.seeded {
		return "r::Random {}"
	}
	return "r::Random { seed: 0x" + hex.EncodeToString(g.seed[:]) + " }"
}
