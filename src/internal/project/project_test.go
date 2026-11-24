package project

import (
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"ritchie.dev/internal/diagnostics"
)

// readString reads data as the project metadata file.
func readString(t *testing.T, data string) (Project, []diagnostics.Record) {
	t.Helper()
	return Read("app.ritchie.toml", []byte(data))
}

func TestAcceptedNormalForm(t *testing.T) {

	p, diags := readString(t,
		"\xEF\xBB\xBF# leading comment\r\n"+
			"\r\n"+
			"[ modules ]  # spaced header\n"+
			"bank   = \"src/banking\"   # trailing comment\n"+
			"ledger = \"src/./ledger/\"\n"+
			"from   = \"depth/one/two\"\n"+
			"esc    = \"dir\\u0041\"\n")
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	if p.Name != "app" {
		t.Errorf("project name = %q, want app", p.Name)
	}
	want := []ModuleRoot{
		{Name: "bank", Root: "src/banking"},
		{Name: "ledger", Root: "src/ledger"},
		{Name: "from", Root: "depth/one/two"},
		{Name: "esc", Root: "dirA"},
	}
	if len(p.Modules) != len(want) {
		t.Fatalf("modules = %+v, want %d entries", p.Modules, len(want))
	}
	for i, w := range want {
		if p.Modules[i].Name != w.Name || p.Modules[i].Root != w.Root {
			t.Errorf("module %d = %+v, want %+v", i, p.Modules[i], w)
		}
	}
}

func TestEmptyProjectHasNoModules(t *testing.T) {
	p, diags := readString(t, "# nothing declared\n")
	if len(diags) != 0 || len(p.Modules) != 0 {
		t.Errorf("empty project: modules = %+v, diagnostics = %+v", p.Modules, diags)
	}
}

func TestRejectedMetadata(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		reason  string
		spanned string
	}{
		{"unknown table", "[tools]\n", diagnostics.ProjectUnknownField, "[tools]"},
		{"top-level field", "name = \"x\"\n", diagnostics.ProjectUnknownField, "name"},
		{"deps table", "[deps]\n", diagnostics.UnsupportedMetadata, "[deps]"},
		{"array of tables", "[[modules]]\n", diagnostics.UnsupportedMetadata, "[[modules]]"},
		{"dotted header", "[modules.sub]\n", diagnostics.UnsupportedMetadata, "[modules.sub]"},
		{"dotted key", "[modules]\na.b = \"src\"\n", diagnostics.UnsupportedMetadata, "a.b"},
		{"quoted key", "[modules]\n\"bank\" = \"src\"\n", diagnostics.UnsupportedMetadata, "\"bank\" = \"src\""},
		{"literal string", "[modules]\nbank = 'src'\n", diagnostics.UnsupportedMetadata, "'src'"},
		{"multiline string", "[modules]\nbank = \"\"\"\nsrc\n\"\"\"\n", diagnostics.UnsupportedMetadata, "\"\"\""},
		{"non-string root", "[modules]\nbank = 42\n", diagnostics.ProjectInvalidRoot, "42"},
		{"array root", "[modules]\nbank = [\"src\", 2]\n", diagnostics.ProjectInvalidRoot, "[\"src\", 2]"},
		{"inline table root", "[modules]\nbank = { dir = \"src\" }\n", diagnostics.ProjectInvalidRoot, "{ dir = \"src\" }"},
		{"duplicate key", "[modules]\nbank = \"a\"\nbank = \"b\"\n", diagnostics.ProjectInvalidFile, "bank"},
		{"duplicate table", "[modules]\n[modules]\n", diagnostics.ProjectInvalidFile, "[modules]"},
		{"missing equals", "[modules]\nbank \"src\"\n", diagnostics.ProjectInvalidFile, "\""},
		{"missing value", "[modules]\nbank =\n", diagnostics.ProjectInvalidFile, ""},
		{"unterminated string", "[modules]\nbank = \"src\n", diagnostics.ProjectInvalidFile, "\"src"},
		{"invalid escape", "[modules]\nbank = \"s\\qc\"\n", diagnostics.ProjectInvalidFile, "\\q"},
		{"content after value", "[modules]\nbank = \"a\" extra\n", diagnostics.ProjectInvalidFile, "extra"},
		{"malformed header", "[mod ules]\n", diagnostics.ProjectInvalidFile, "["},
		{"upper-case module name", "[modules]\nBank = \"src\"\n", diagnostics.ProjectInvalidModuleName, "Bank"},
		{"dashed module name", "[modules]\nbank-x = \"src\"\n", diagnostics.ProjectInvalidModuleName, "bank-x"},
		{"keyword module name", "[modules]\nreturn = \"src\"\n", diagnostics.ProjectInvalidModuleName, "return"},
		{"empty root", "[modules]\nbank = \"\"\n", diagnostics.ProjectInvalidRoot, "\"\""},
		{"absolute root", "[modules]\nbank = \"/src\"\n", diagnostics.ProjectInvalidRoot, "\"/src\""},
		{"escaping root", "[modules]\nbank = \"../out\"\n", diagnostics.ProjectInvalidRoot, "\"../out\""},
		{"inner escape", "[modules]\nbank = \"src/../../out\"\n", diagnostics.ProjectInvalidRoot, "\"src/../../out\""},
		{"duplicate root", "[modules]\nbank = \"src/x\"\nledger = \"src/./x\"\n", diagnostics.ProjectDuplicateRoot, "\"src/./x\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := readString(t, tt.src)
			if len(diags) == 0 {
				t.Fatalf("%q was accepted", tt.src)
			}
			d := diags[0]
			if d.Reason != tt.reason {
				t.Fatalf("reason = %s, want %s; all %+v", d.Reason, tt.reason, diags)
			}
			got := tt.src[d.Span.Start.Byte:d.Span.End.Byte]
			if got != tt.spanned {
				t.Errorf("span covers %q, want %q", got, tt.spanned)
			}
		})
	}
}

func TestHostAbsoluteRootsAreRejected(t *testing.T) {
	tested := 0
	for _, root := range []string{"/src", `C:\src`, `\\server\share`} {
		if !filepath.IsAbs(root) {
			continue
		}
		tested++
		src := "[modules]\nbank = " + strconv.Quote(root) + "\n"
		_, diags := readString(t, src)
		if len(diags) != 1 || diags[0].Reason != diagnostics.ProjectInvalidRoot {
			t.Errorf("root %q: diagnostics = %+v, want one %s",
				root, diags, diagnostics.ProjectInvalidRoot)
		}
	}
	if tested == 0 {
		t.Fatal("test has no absolute path form for this host")
	}
}

func TestContextualQueryWordIsAModuleName(t *testing.T) {
	p, diags := readString(t, "[modules]\nfrom = \"src\"\n")
	if len(diags) != 0 || len(p.Modules) != 1 || p.Modules[0].Name != "from" {
		t.Errorf("modules = %+v, diagnostics = %+v", p.Modules, diags)
	}
}

func TestProjectNameValidation(t *testing.T) {
	for _, base := range []string{"My.app.ritchie.toml", "9x.ritchie.toml", "self.ritchie.toml"} {
		_, diags := Read(base, []byte("[modules]\nbank = \"src\"\n"))
		if len(diags) == 0 || diags[0].Reason != diagnostics.ProjectInvalidName {
			t.Errorf("%s: diagnostics = %+v, want %s", base, diags, diagnostics.ProjectInvalidName)
			continue
		}
		span := diags[0].Span
		if span.Start.Byte != 0 || span.End.Byte != 0 {
			t.Errorf("%s: span = %+v, want zero length at the file start", base, span)
		}
	}
	_, diags := Read("nested/dir/payroll2.ritchie.toml", []byte(""))
	if len(diags) != 0 {
		t.Errorf("payroll2: diagnostics = %+v, want none", diags)
	}
}

func TestInvalidUTF8IsInvalidMetadata(t *testing.T) {
	_, diags := Read("app.ritchie.toml", []byte("[modules]\nbank = \"\xFF\"\n"))
	if len(diags) != 1 || diags[0].Reason != diagnostics.ProjectInvalidFile {
		t.Fatalf("diagnostics = %+v, want one %s", diags, diagnostics.ProjectInvalidFile)
	}
}

func TestDiagnosticsIdentifyTheProjectFile(t *testing.T) {
	_, diags := Read("dir/app.ritchie.toml", []byte("[tools]\n"))
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %+v, want one", diags)
	}
	if diags[0].Path != "dir/app.ritchie.toml" || diags[0].FileIndex != 0 || diags[0].Module != "" {
		t.Errorf("record = %+v, want the supplied project path at ordinal zero with no module", diags[0])
	}
}

// diagnosticsEqual compares two records with their cycle module lists in sorted order.
func diagnosticsEqual(a, b diagnostics.Record) bool {
	a.CycleModules = slices.Clone(a.CycleModules)
	b.CycleModules = slices.Clone(b.CycleModules)
	slices.Sort(a.CycleModules)
	slices.Sort(b.CycleModules)
	return reflect.DeepEqual(a, b)
}

func FuzzRead(f *testing.F) {
	f.Add([]byte("[modules]\nbank = \"src/banking\"\n"))
	f.Add([]byte("[deps]\ncsv = { }\n[modules]\na.b = 'x'\nbank = \"\"\"\nx\n\"\"\"\n"))
	f.Add([]byte("\xEF\xBB\xBF[modules]\r\nx = \"\\u0041\\U00000042\"\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p1, d1 := Read("f.ritchie.toml", data)
		p2, d2 := Read("f.ritchie.toml", data)
		if len(d1) != len(d2) || len(p1.Modules) != len(p2.Modules) {
			t.Fatalf("outcome is not deterministic: %+v vs %+v", d1, d2)
		}
		for i := range d1 {
			if !diagnosticsEqual(d1[i], d2[i]) {
				t.Fatalf("diagnostic %d differs between runs", i)
			}
		}
		if len(d1) == 0 {
			return
		}
		for _, d := range d1 {
			if !strings.HasPrefix(d.Reason, "project.") &&
				d.Reason != diagnostics.UnsupportedMetadata {
				t.Fatalf("metadata rejection carries reason %s", d.Reason)
			}
		}
	})
}
