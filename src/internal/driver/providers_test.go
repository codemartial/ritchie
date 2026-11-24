// This file checks the composition of the clock and entropy providers of built
// programs: the production defaults, deterministic replacement, and build identity.
package driver

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	clockProbe = "main() {\n    log(r::Clock.time())\n    log(r::Random.new().bits())\n}\n"
	sumProbe   = "main() {\n    log(1 + 1)\n}\n"
)

var (
	instant   = time.Date(2026, 9, 23, 9, 5, 2, 0, time.UTC)
	seed      = [32]byte([]byte("ritchie-g45-deterministic-seed!!"))
	otherSeed = [32]byte([]byte("ritchie-g45-deterministic-seed!?"))
)

// deterministic returns the source of a provider composition whose clock reads one instant and whose entropy is the ChaCha8 stream of one seed.
func deterministic(instant time.Time, seed [32]byte) []byte {
	return fmt.Appendf(nil, `package rlr

import (
	"math/rand/v2"
	"time"
)

func init() {
	instant := time.Unix(%d, 0).UTC()
	stream := rand.NewChaCha8(%#v)
	Install(func() time.Time { return instant }, stream.Uint64)
}
`, instant.Unix(), seed)
}

// probe builds and runs a program from standard input under a provider composition, returning its output and exit status.
func probe(t *testing.T, program string, providers []byte) (string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	h := Host{Name: "rcrun", Stdin: strings.NewReader(program), Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir(), Providers: providers}
	status := RunMain([]string{"-"}, h)
	return stdout.String() + stderr.String(), status
}

func TestTheProductionProvidersReadTheHostClockAndEntropy(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs generated programs")
	}
	out, status := probe(t, clockProbe, nil)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	year := 0
	if len(lines) == 2 && len(lines[0]) >= 4 {
		year, _ = strconv.Atoi(lines[0][:4])
	}
	if status != ExitOK || len(lines) != 2 || year < 2026 {
		t.Errorf("rcrun with production providers = status %d output %q, want status 0 and a clock reading of 2026 or later followed by one bit set", status, out)
	}
}

func TestDeterministicProvidersGiveTheConfiguredInstantAndSeedOnEveryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs generated programs")
	}
	table := []struct {
		name      string
		program   string
		providers []byte
		output    string
	}{
		{"the clock probe reads the configured instant and seed", clockProbe, deterministic(instant, seed), "2026-09-23T09:05:02Z\n0x50793a6856071bc1\n"},
		{"another seed gives other bits", clockProbe, deterministic(instant, otherSeed), "2026-09-23T09:05:02Z\n0xfcfc015654a69117\n"},
		{"another instant gives another reading", clockProbe, deterministic(instant.Add(time.Hour), seed), "2026-09-23T10:05:02Z\n0x50793a6856071bc1\n"},
		{"an effect-independent probe prints the same under production", sumProbe, nil, "2\n"},
		{"an effect-independent probe prints the same under deterministic providers", sumProbe, deterministic(instant, seed), "2\n"},
	}
	for _, c := range table {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for run := range 2 {
				out, status := probe(t, c.program, c.providers)
				if out != c.output || status != ExitOK {
					t.Errorf("run %d = status %d output %q, want status 0 output %q", run, status, out, c.output)
				}
			}
		})
	}
}

func TestAProviderChangeChangesTheBuiltExecutable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated programs")
	}
	built := func(providers []byte) []byte {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "probe.ri"), []byte(clockProbe), 0o644); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		h := Host{Name: "ritchie", Stdout: &stderr, Stderr: &stderr, Dir: dir, Providers: providers}
		if status := Main([]string{"probe.ri"}, h); status != ExitOK {
			t.Fatalf("ritchie probe.ri = status %d output %q, want status 0", status, stderr.String())
		}
		exe, err := os.ReadFile(filepath.Join(dir, "probe"))
		if err != nil {
			t.Fatal(err)
		}
		return exe
	}
	first := built(deterministic(instant, seed))
	again := built(deterministic(instant, seed))
	other := built(deterministic(instant, otherSeed))
	if !bytes.Equal(first, again) {
		t.Errorf("equal provider compositions built different executables")
	}
	if bytes.Equal(first, other) {
		t.Errorf("different seeds built the same executable")
	}
}
