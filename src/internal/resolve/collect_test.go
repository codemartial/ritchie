package resolve

import (
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/testsrc"
)

// collect parses the files of a source set and collects them, where no fixed array type writes a constant as its length, returning every diagnostic.
func collect(files []testsrc.File) []diagnostics.Record {
	trees, diags := testsrc.Parse(files)
	_, ds := Collect(trees, nil)
	return append(diags, ds...)
}
