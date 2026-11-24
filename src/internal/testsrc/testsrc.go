// Package testsrc parses the source sets that compiler tests write inline.
package testsrc

import (
	"fmt"
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/source"
	"ritchie.dev/internal/syntax"
)

// File is one source file of a program with the module it belongs to.
type File struct {
	Module string
	Path   string
	Text   string
}

// Parse parses the files of a program into their trees, returning every lexical and syntax diagnostic.
func Parse(files []File) ([]*syntax.File, []diagnostics.Record) {
	var trees []*syntax.File
	var diags []diagnostics.Record
	for i, f := range files {
		src, rec := source.New(f.Path, i, []byte(f.Text))
		if rec != nil {
			diags = append(diags, *rec)
			continue
		}
		src.Module = f.Module
		tree, ds := syntax.Parse(src)
		diags = append(diags, ds...)
		trees = append(trees, tree)
	}
	return trees, diags
}

// Describe writes diagnostics for a failure message.
func Describe(diags []diagnostics.Record) string {
	var parts []string
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("%s at %s:%d:%d %s", d.Reason, d.Path, d.Span.Start.Line, d.Span.Start.Column, d.Message))
	}
	return strings.Join(parts, "; ")
}
