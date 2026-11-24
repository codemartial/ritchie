package project

import (
	"strings"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/source"
	"ritchie.dev/internal/syntax"
)

// Embedded reports the unsupported dependency block of an ad-hoc source file, which is the leading block comment when its first non-blank line is [deps], or nil when the file carries none.
func Embedded(f *source.File) *diagnostics.Record {
	at, end, ok := syntax.LeadingComment(f)
	if !ok {
		return nil
	}
	for line := range strings.Lines(f.Text[at:end]) {
		content := strings.TrimSpace(line)
		if content == "" {
			at += len(line)
			continue
		}
		if content != "[deps]" {
			return nil
		}
		start := at + strings.Index(line, content)
		rec := f.Record(diagnostics.UnsupportedMetadata, start, start+len(content), "Ritchie does not support the [deps] table in embedded metadata")
		return &rec
	}
	return nil
}
