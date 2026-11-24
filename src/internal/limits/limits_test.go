package limits

import (
	"slices"
	"testing"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/testsrc"
)

// limitsOf parses and checks one source file and returns the messages of the limits pass, or the diagnostics of an earlier stage.
func limitsOf(t *testing.T, text string) []string {
	t.Helper()
	files := []testsrc.File{{Path: "case.ri", Text: text}}
	trees, diags := testsrc.Parse(files)
	if len(diags) > 0 {
		t.Fatalf("parse: %s", testsrc.Describe(diags))
	}
	p, diags, err := check.Check(trees, check.Options{})
	if err != nil || len(diags) > 0 {
		t.Fatalf("check: %v %s", err, testsrc.Describe(diags))
	}
	var out []string
	for _, d := range Check(p) {
		if d.Reason != diagnostics.UnsupportedSyntax {
			t.Errorf("reason %s, want %s", d.Reason, diagnostics.UnsupportedSyntax)
		}
		out = append(out, diagnostics.Sentence(d.Message))
	}
	return out
}

func TestEachLimitOfALaterStageIsReportedAtTheSourceConstruct(t *testing.T) {
	tests := []struct {
		fact, text string
		want       []string
	}{
		{"a program that every stage implements has no limit", "main() { log(1) }\n", nil},
		{"a sum declaration is a limit of the lowering", "Choice = Int | String\nmain() { log(1) }\n", []string{"Sum declaration is not supported."}},
		{"a type without a Go type is a limit of the backend", "@Sv { }\n@Sv.f(UUId u) -> Int { return 1 }\n", []string{"The type UUId is not supported."}},
		{"a built-in call without an rlr function is a limit of the backend", "main() {\n    Decimal a = 1.0\n    Decimal b = 3.0\n    for d in range(a, b) { log(d) }\n}\n", []string{`"range" is not supported for arguments of these types.`}},
		{"a range over Date bounds is a limit of the backend", "main() {\n    for d in range(d2025-01-01, d2025-01-03) { log(d) }\n}\n", []string{`"range" is not supported for arguments of these types.`}},
		{"a range over Date bounds with a Duration step is a limit of the backend", "main() {\n    for d in range(d2025-01-01, d2025-01-03, 1d) { log(d) }\n}\n", []string{`"range" is not supported for arguments of these types.`}},
		{"a range over Time bounds with a Duration step is a limit of the backend", "main() {\n    for t in range(t10:00:00, t11:00:00, 30m) { log(t) }\n}\n", []string{`"range" is not supported for arguments of these types.`}},
		{"a range over DateTime bounds with a Duration step is a limit of the backend", "main() {\n    for t in range(2026-09-23T01:00:00Z, 2026-09-23T02:00:00Z, 30m) { log(t) }\n}\n", []string{`"range" is not supported for arguments of these types.`}},
	}
	for _, tt := range tests {
		if got := limitsOf(t, tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.fact, got, tt.want)
		}
	}
}
