package monolith

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var persistentRoots = map[string]string{
	"rlr.Diag":       "the diagnostic sink",
	"rlr.Out":        "the output sink",
	"rlr.Exit":       "the exit function",
	"rlr.level":      "the reporting level, settled once at startup",
	"rlr.now":        "the installed clock",
	"rlr.entropy":    "the installed entropy source",
	"rlr.host":       "the installed target",
	"rlr.errorTypes": "the program's error-type metadata, registered once at initialization",
	"rlr.locations":  "the program's location metadata, registered once at initialization",
}

var runtimeShared = map[string]string{
	"rlr.scanners": "the pool of JSON decoders, each cleared of its input and pooled only after an input of at most 1 KiB",
	"rlr.encoders": "the pool of JSON encoders, each zeroed of its output and pooled only after an output of at most 4 KiB",
}

var immutableTables = map[string]bool{
	"rlr.catalog": true, "rlr.exactPow10": true, "rlr.pow5_64": true, "rlr.pow10_64": true,
	"rlr.pow10_128": true, "rlr.pow10_256": true, "rlr.pow10div": true,
	"rlr.minDay": true, "rlr.maxDay": true, "rlr.minUnixSec": true, "rlr.maxUnixSec": true,
	"rlr.durationUnits": true, "rlr.subordinateBound": true,
}

// placeRoot returns the root identifier of a written place.
func placeRoot(x ast.Expr) string {
	for {
		switch n := x.(type) {
		case *ast.Ident:
			return n.Name
		case *ast.IndexExpr:
			x = n.X
		case *ast.SelectorExpr:
			x = n.X
		case *ast.StarExpr:
			x = n.X
		case *ast.ParenExpr:
			x = n.X
		default:
			return ""
		}
	}
}

func TestRuntimeKeepsNoInvocationResidue(t *testing.T) {
	declared := map[string]bool{}
	for _, pkg := range []string{"rlr", "rlr/mesh", "monolith"} {
		paths, err := filepath.Glob(filepath.Join("..", pkg, "*.go"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("no sources for %s: %v", pkg, err)
		}
		fset := token.NewFileSet()
		var files []*ast.File
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		for _, file := range files {
			where := func(n ast.Node) string { return fset.Position(n.Pos()).String() }
			for _, imp := range file.Imports {
				if imp.Path.Value == `"unsafe"` || imp.Path.Value == `"C"` {
					t.Errorf("%s: imports %s", where(imp), imp.Path.Value)
				}
			}
			for _, decl := range file.Decls {
				gen, isGen := decl.(*ast.GenDecl)
				if !isGen || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						full := pkg + "." + name.Name
						declared[full] = true
						if persistentRoots[full] == "" && runtimeShared[full] == "" && !immutableTables[full] {
							t.Errorf("%s: package-level variable %s is not a persistent root, runtime-shared memory, or an immutable table", where(name), full)
						}
					}
				}
			}
			written := func(n ast.Node, x ast.Expr) {
				if root := placeRoot(x); immutableTables[pkg+"."+root] {
					t.Errorf("%s: the immutable table %s.%s is written or has its address taken", where(n), pkg, root)
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncDecl:

					if n.Recv == nil && n.Name.Name == "init" {
						return false
					}
				case *ast.AssignStmt:
					if n.Tok != token.DEFINE {
						for _, lhs := range n.Lhs {
							written(n, lhs)
						}
					}
				case *ast.IncDecStmt:
					written(n, n.X)
				case *ast.UnaryExpr:
					if n.Op == token.AND {
						written(n, n.X)
					}
				case *ast.SelectorExpr:
					if base, isIdent := n.X.(*ast.Ident); isIdent && base.Name == "runtime" && (n.Sel.Name == "SetFinalizer" || n.Sel.Name == "AddCleanup") {
						t.Errorf("%s: uses runtime.%s", where(n), n.Sel.Name)
					}
				}
				return true
			})
		}
	}
	for name := range persistentRoots {
		if !declared[name] {
			t.Errorf("the persistent root %s is not declared; remove it from the list", name)
		}
	}
	for name := range runtimeShared {
		if !declared[name] {
			t.Errorf("the runtime-shared memory %s is not declared; remove it from the list", name)
		}
	}
	for name := range immutableTables {
		if !declared[name] {
			t.Errorf("the immutable table %s is not declared; remove it from the list", name)
		}
	}
}
