// Package lower rewrites a checked source program into a kernel program by the rules of
// RITCHIE-MIN.md: phase A applies every rewrite but the handler lowering and leaves
// block handlers, raise-set inference runs over that program, and phase B lowers the
// handlers to outcome matches.
package lower

import (
	"fmt"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
)

// Lower lowers a checked source program to a kernel program under the schedule of the build, which is sequential when every read is waited for at its place, types the kernel program in silent mode, and returns it with its types and raise sets, the table of operand modes, the table of the loops whose reads run ahead, which is empty under the sequential schedule, the diagnostics of the kernel check, and the internal error of a defect, which includes a construct that the lowering does not implement, because the limits pass rejects such a program first.
func Lower(p *check.Program, sequential bool) (*check.Program, []diagnostics.Record, error) {
	k, limits, err := run(p, sequential)
	if err != nil {
		return nil, nil, err
	}
	if len(limits) > 0 {
		return nil, nil, internal("the lowering met a construct that the limits pass did not reject", limits)
	}
	out, diags, err := check.CheckKernel(k, p.Options)
	if err != nil {
		return nil, nil, err
	}
	out.Modes = out.Ownership()
	if !sequential {
		out.Ahead = out.RunAhead()
	}
	return out, append(diags, out.Flights()...), nil
}

// Limits returns a source diagnostic for each construct of a checked source program that the lowering does not implement, which the limits pass reports before lowering.
func Limits(p *check.Program) []diagnostics.Record {
	_, limits, _ := run(p, false)
	return limits
}

// run applies both phases and returns the kernel files, or the constructs without a rule, or the internal error of a defect.
func run(p *check.Program, sequential bool) ([]*syntax.File, []diagnostics.Record, error) {
	a := &lowerer{p: p, routines: map[syntax.Expr]bool{}, inline: map[*syntax.Handled]bool{}, whole: map[syntax.Expr]bool{}}
	var files []*syntax.File
	for _, f := range p.Files {
		files = append(files, a.file(f))
	}
	if len(a.diags) > 0 {
		diagnostics.Sort(a.diags)
		return nil, a.diags, nil
	}
	pA, diags, err := check.CheckKernel(files, p.Options)
	if err != nil {
		return nil, nil, fmt.Errorf("%w, which is the program after phase A", err)
	}
	if len(diags) > 0 {
		return nil, nil, internal("the program after phase A does not check", diags)
	}
	b := &outcomes{p: pA, fl: newFlights(pA, sequential, a.inline)}
	var kernel []*syntax.File
	for _, f := range pA.Files {
		kernel = append(kernel, b.file(f))
	}
	if len(b.diags) > 0 {
		diagnostics.Sort(b.diags)
		return nil, b.diags, nil
	}
	return kernel, nil, nil
}

// internal is the error of a lowered program that a later stage rejects.
func internal(what string, diags []diagnostics.Record) error {
	var parts []string
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("%s at %s:%d:%d: %s", d.Reason, d.Path, d.Span.Start.Line, d.Span.Start.Column, d.Message))
	}
	return fmt.Errorf("internal error: %s: %v", what, parts)
}

// unsupported reports a form that parses but has no rule yet, at its origin and with its construct's name.
func unsupported(diags *[]diagnostics.Record, n syntax.Node) {
	o := n.Where()
	*diags = append(*diags, o.File.Record(diagnostics.UnsupportedSyntax, o.Start, o.End, fmt.Sprintf("%s is not supported", o.Construct)))
}
