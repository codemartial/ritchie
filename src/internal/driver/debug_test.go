// This file checks the debug option, which admits catalog errors in application code.
package driver

import (
	"bytes"
	"strings"
	"testing"
)

const catalogConstruction = "f() -> Int { return Unverified! }\n\nmain() { log(f()) }\n"

// runWith builds and runs a program from standard input with the given options, returning its output and exit status.
func runWith(t *testing.T, program string, options ...string) (string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	h := Host{Name: "rcrun", Stdin: strings.NewReader(program), Stdout: &stdout, Stderr: &stderr, Dir: t.TempDir()}
	status := RunMain(append(options, "-"), h)
	return stdout.String() + report(stderr.String()), status
}

// report returns the diagnostic stream without the indented location and history lines of error reports, which the tests do not state.
func report(stderr string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(stderr, "\n") {
		if !strings.HasPrefix(line, "  ") {
			out.WriteString(line)
		}
	}
	return out.String()
}

func TestACatalogErrorConstructionIsRejectedWithoutTheDebugOption(t *testing.T) {
	out, status := runWith(t, catalogConstruction)
	if status != ExitRejected || !strings.Contains(out, "[error.catalog-construction]") {
		t.Errorf("rcrun - = status %d output %q, want status %d and error.catalog-construction", status, out, ExitRejected)
	}
}

func TestADebugBuildRaisesTheCatalogErrorThatItConstructs(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs generated programs")
	}
	out, status := runWith(t, catalogConstruction, DebugOption)
	if want := "Unverified!\n"; out != want || status != 1 {
		t.Errorf("rcrun %s - = status %d output %q, want status 1 output %q", DebugOption, status, out, want)
	}
}
