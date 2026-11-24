package check

import (
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/testsrc"
)

// check parses and checks a source set in reporting mode, returning every diagnostic.
func check(files []testsrc.File) []diagnostics.Record { return checkWith(files, Options{}) }

// checkWith parses and checks a source set in reporting mode under the given options, returning every diagnostic, where an internal error fails the test through a diagnostic of its own.
func checkWith(files []testsrc.File, opts Options) []diagnostics.Record {
	trees, diags := testsrc.Parse(files)
	if len(diags) > 0 {
		return diags
	}
	_, ds, err := Check(trees, opts)
	if err != nil {
		ds = append(ds, diagnostics.Record{Reason: "internal", Message: err.Error()})
	}
	return ds
}

// checkKernel parses and checks a kernel program in silent mode, returning the kernel-check diagnostics, with those of the forms of a read in flight, and the internal error of a typing fault.
func checkKernel(files []testsrc.File) ([]diagnostics.Record, error) {
	trees, diags := testsrc.Parse(files)
	if len(diags) > 0 {
		return diags, nil
	}
	p, ds, err := CheckKernel(trees, Options{})
	if err != nil {
		return ds, err
	}
	return append(ds, p.Flights()...), nil
}
