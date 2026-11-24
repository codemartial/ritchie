// This file implements the kernel commands: print writes the lowered kernel program
// as canonical Ritchie text, and compare judges a lowered source tree against a
// kernel tree by Equal.
package driver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/project"
	"ritchie.dev/internal/source"
	"ritchie.dev/internal/syntax"
)

// print lowers the selected source set and writes each kernel file, preceded by its path when there are several.
func (inv *invocation) print() int {
	set, kernel, status := inv.lowered()
	if set == nil {
		return status
	}
	for _, f := range kernel.Files {
		if len(kernel.Files) > 1 {
			fmt.Fprintf(inv.h.Stdout, "// %s\n", f.File.Path)
		}
		fmt.Fprint(inv.h.Stdout, syntax.Print(f))
	}
	return ExitOK
}

// compare lowers the given inputs within the source tree, or the whole tree when none are given, and reports whether each lowered file is Equal to the file at the same path of the kernel tree, printing the first difference otherwise.
func (inv *invocation) compare(kernelDir, sourceDir string, inputs []string) int {
	src := &invocation{h: inv.h, debug: inv.debug, inputs: inputs}
	src.h.Dir = inv.path(sourceDir)
	manifests, _ := filepath.Glob(filepath.Join(src.h.Dir, "*"+project.Suffix))
	switch {
	case len(inputs) > 0:
	case len(manifests) == 1:
		src.inputs = []string{filepath.Base(manifests[0])}
	default:
		files, err := riFiles(src.h.Dir)
		if err != nil {
			return inv.failure(err.Error())
		}
		src.inputs = files
	}
	set, kernel, status := src.lowered()
	if set == nil {
		return status
	}
	equal := true
	for i, lowered := range kernel.Files {
		path := set.files[i].Path
		data, err := os.ReadFile(filepath.Join(inv.path(kernelDir), filepath.FromSlash(path)))
		if err != nil {
			fmt.Fprintf(inv.h.Stdout, "%s: no kernel version: %v\n", path, err)
			equal = false
			continue
		}
		f, rec := source.New(path, i, data)
		if rec != nil {
			return inv.reject([]diagnostics.Record{*rec})
		}
		f.Module = set.files[i].Module
		want, diags := syntax.Parse(f)
		if len(diags) > 0 {
			return inv.reject(diags)
		}
		if !syntax.Equal(lowered, want) {
			line, left, right := syntax.Difference(lowered, want)
			fmt.Fprintf(inv.h.Stdout, "%s:%d: lowered %q, kernel %q\n", path, line, left, right)
			equal = false
		}
	}
	if !equal {
		return ExitRejected
	}
	return ExitOK
}

// riFiles lists the .ri files under a directory by their slash-separated relative paths, in lexical order.
func riFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".ri" {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	if len(out) == 0 && err == nil {
		err = fmt.Errorf("%s holds no .ri file", dir)
	}
	return out, err
}
