// Package limits decides, right after the source check, whether the compiler
// implements every construct of a program, from one table of the lowering's and
// the backend's limits.
package limits

import (
	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/gen"
	"ritchie.dev/internal/lower"
)

// stage is one entry of the table: the stage whose limits it finds, by that stage's own predicates.
type stage struct {
	name string
	find func(*check.Program) []diagnostics.Record
}

var table = []stage{
	{"lowering", lower.Limits},
	{"backend", gen.Limits},
}

// Check returns a limit.unsupported-syntax diagnostic at each construct of a checked source program that a later stage does not implement, from the first stage of the table that finds one, so that no later stage meets a construct it cannot translate.
func Check(p *check.Program) []diagnostics.Record {
	for _, s := range table {
		if diags := s.find(p); len(diags) > 0 {
			return diags
		}
	}
	return nil
}
