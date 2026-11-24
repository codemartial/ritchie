package check

import (
	"slices"
	"testing"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
)

// reasons returns the codes of diagnostics.
func reasons(ds []diagnostics.Record) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Reason)
	}
	return out
}

func TestApplicationCodeConstructsACatalogErrorOnlyInADebugBuild(t *testing.T) {
	forms := map[string]func(name string) string{
		"a return":      func(name string) string { return "f() -> Int { return " + name + " }\nmain() { log(f()) }\n" },
		"a declaration": func(name string) string { return "main() {\n    Error! e = " + name + "\n    log(e)\n}\n" },
		"a service-method body": func(name string) string {
			return "@Svc {}\n@Svc.get() -> Int { return " + name + " }\nmain() { log(@Svc.get()) }\n"
		},
	}
	for _, r := range builtin.Rows {
		if r.Form != builtin.ErrorRow {
			continue
		}
		for form, program := range forms {
			files := one(program(r.Name))
			if got := reasons(checkWith(files, Options{})); !slices.Equal(got, []string{"error.catalog-construction"}) {
				t.Errorf("%s in %s: got %v, want [error.catalog-construction]", r.Name, form, got)
			}
			if got := reasons(checkWith(files, Options{Debug: true})); len(got) > 0 {
				t.Errorf("%s in %s under the debug option: got %v, want none", r.Name, form, got)
			}
		}
	}
}

func TestACaughtRuntimeErrorIsForwardedWithoutTheDebugOption(t *testing.T) {
	files := one("@Svc {}\n@Svc.get(Int n) -> Int { return n }\nrelay() -> Int {\n    return @Svc.get(1) handle {\n        Cancelled! e => return e\n    }\n}\nmain() { log(relay()) }\n")
	if got := reasons(checkWith(files, Options{})); len(got) > 0 {
		t.Errorf("got %v, want none", got)
	}
}
