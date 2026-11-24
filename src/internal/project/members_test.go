package project

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"ritchie.dev/internal/diagnostics"
)

// readValid reads metadata as the project file.
func readValid(t *testing.T, metadata string) Project {
	t.Helper()
	p, diags := Read("app.ritchie.toml", []byte(metadata))
	if len(diags) != 0 {
		t.Fatalf("metadata diagnostics = %+v, want none", diags)
	}
	return p
}

// file returns an in-memory source file holding one alias declaration.
func file() *fstest.MapFile { return &fstest.MapFile{Data: []byte("Email = String\n")} }

func TestMembershipAssignsDeepestRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"app.ritchie.toml":      file(),
		"src/outer/a.ri":        file(),
		"src/outer/sub/deep.ri": file(),
		"src/outer/inner/b.ri":  file(),
		"src/outer/inner/c.ri":  file(),
	}
	p := readValid(t, "[modules]\nouter = \"src/outer\"\ninner = \"src/outer/inner\"\n")
	set, diags := Members(fsys, p)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	want := []ModuleFiles{
		{Name: "inner", Files: []string{"src/outer/inner/b.ri", "src/outer/inner/c.ri"}},
		{Name: "outer", Files: []string{"src/outer/a.ri", "src/outer/sub/deep.ri"}},
	}
	if len(set.Modules) != len(want) {
		t.Fatalf("modules = %+v, want %+v", set.Modules, want)
	}
	for i, w := range want {
		got := set.Modules[i]
		if got.Name != w.Name || len(got.Files) != len(w.Files) {
			t.Fatalf("module %d = %+v, want %+v", i, got, w)
		}
		for j, path := range w.Files {
			if got.Files[j] != path {
				t.Errorf("module %s file %d = %q, want %q", w.Name, j, got.Files[j], path)
			}
		}
	}
}

func TestOrphansAndNestedProjectsAreOutside(t *testing.T) {
	fsys := fstest.MapFS{
		"app.ritchie.toml":                   file(),
		"orphan.ri":                          file(),
		"src/bank/a.ri":                      file(),
		"src/bank/vendored/own.ritchie.toml": file(),
		"src/bank/vendored/inside.ri":        file(),
	}
	p := readValid(t, "[modules]\nbank = \"src/bank\"\n")
	set, diags := Members(fsys, p)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	if len(set.Modules) != 1 || len(set.Modules[0].Files) != 1 ||
		set.Modules[0].Files[0] != "src/bank/a.ri" {
		t.Errorf("membership = %+v, want only src/bank/a.ri", set.Modules)
	}
}

func TestOnlyRegularProjectFilesCreateNestedBoundaries(t *testing.T) {
	fsys := fstest.MapFS{
		"app.ritchie.toml":                                 file(),
		"src/bank/a.ri":                                    file(),
		"src/bank/vendor/inside.ri":                        file(),
		"src/bank/vendor/link.ritchie.toml":                &fstest.MapFile{Mode: fs.ModeSymlink},
		"src/bank/vendor/directory.ritchie.toml/nested.ri": file(),
	}
	p := readValid(t, "[modules]\nbank = \"src/bank\"\n")
	set, diags := Members(fsys, p)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	want := []string{
		"src/bank/a.ri",
		"src/bank/vendor/directory.ritchie.toml/nested.ri",
		"src/bank/vendor/inside.ri",
	}
	if len(set.Modules) != 1 || !slices.Equal(set.Modules[0].Files, want) {
		t.Errorf("membership = %+v, want %v", set.Modules, want)
	}
}

func TestDeclaredRootMayCarryItsOwnProjectFile(t *testing.T) {

	fsys := fstest.MapFS{
		"app.ritchie.toml":                   file(),
		"vendor/lib/lib.ritchie.toml":        file(),
		"vendor/lib/a.ri":                    file(),
		"vendor/lib/sub/b.ri":                file(),
		"vendor/lib/deeper/own.ritchie.toml": file(),
		"vendor/lib/deeper/hidden.ri":        file(),
	}
	p := readValid(t, "[modules]\nlib = \"vendor/lib\"\n")
	set, diags := Members(fsys, p)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	want := []string{"vendor/lib/a.ri", "vendor/lib/sub/b.ri"}
	if len(set.Modules) != 1 || !slices.Equal(set.Modules[0].Files, want) {
		t.Errorf("membership = %+v, want %v", set.Modules, want)
	}
}

func TestProjectDirectoryMayBeARoot(t *testing.T) {

	fsys := fstest.MapFS{
		"app.ritchie.toml": file(),
		"a.ri":             file(),
		"sub/b.ri":         file(),
	}
	p := readValid(t, "[modules]\nbank = \".\"\n")
	set, diags := Members(fsys, p)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	if len(set.Modules) != 1 || len(set.Modules[0].Files) != 2 {
		t.Errorf("membership = %+v, want a.ri and sub/b.ri", set.Modules)
	}
}

func TestMissingAndEmptyRoots(t *testing.T) {
	fsys := fstest.MapFS{
		"app.ritchie.toml":      file(),
		"src/bank/a.ri":         file(),
		"src/empty/note":        file(),
		"src/hollow/inner/b.ri": file(),
	}
	tests := []struct {
		name     string
		metadata string
		reason   string
	}{
		{"missing root", "[modules]\nbank = \"src/gone\"\n", diagnostics.ProjectMissingRoot},
		{"root is a file", "[modules]\nbank = \"src/bank/a.ri\"\n", diagnostics.ProjectMissingRoot},
		{"no ri files", "[modules]\nbank = \"src/empty\"\n", diagnostics.ProjectEmptyRoot},
		{"empty after subtraction", "[modules]\nouter = \"src/hollow\"\ninner = \"src/hollow/inner\"\n", diagnostics.ProjectEmptyRoot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := readValid(t, tt.metadata)
			_, diags := Members(fsys, p)
			if len(diags) != 1 || diags[0].Reason != tt.reason {
				t.Errorf("diagnostics = %+v, want one %s", diags, tt.reason)
			}
		})
	}
}

func TestSymbolicLinksAreNotFollowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "aside"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeMember := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeMember("app.ritchie.toml", "[modules]\nbank = \"src\"\n")
	writeMember(filepath.Join("src", "real.ri"), "Email = String\n")
	writeMember("outside.ri", "Email = String\n")
	if err := os.Symlink(filepath.Join(dir, "outside.ri"),
		filepath.Join(dir, "src", "linked.ri")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "src", "aside"),
		filepath.Join(dir, "src", "door")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p := readValid(t, "[modules]\nbank = \"src\"\n")
	set, diags := Members(os.DirFS(dir), p)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diags)
	}
	if len(set.Modules) != 1 || len(set.Modules[0].Files) != 1 ||
		set.Modules[0].Files[0] != "src/real.ri" {
		t.Errorf("membership = %+v, want only src/real.ri", set.Modules)
	}
}
