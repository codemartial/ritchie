package syntax

import (
	"fmt"
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/source"
)

// parseText parses a text as one file.
func parseText(name, text string) (*File, []diagnostics.Record) {
	f, rec := source.New(name, 0, []byte(text))
	if rec != nil {
		return nil, []diagnostics.Record{*rec}
	}
	return Parse(f)
}

// describe renders diagnostics with their codes and positions.
func describe(diags []diagnostics.Record) string {
	var parts []string
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("%s at %d:%d %s", d.Reason, d.Span.Start.Line, d.Span.Start.Column, d.Message))
	}
	return strings.Join(parts, "; ")
}
