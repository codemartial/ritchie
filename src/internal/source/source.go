// Package source turns raw file bytes into a validated source object with physical byte
// spans and derived display positions.
package source

import (
	"bytes"
	"fmt"
	"sort"
	"unicode/utf8"

	"ritchie.dev/internal/diagnostics"
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// File is a validated source file.
type File struct {
	Path   string
	Index  int
	Module string
	Bytes  []byte
	Text   string
	BOM    bool

	lineStarts []int
}

// New validates data as complete UTF-8 and builds the file's line map.
func New(path string, index int, data []byte) (*File, *diagnostics.Record) {
	f := &File{Path: path, Index: index, Bytes: data, Text: string(data)}
	if bytes.HasPrefix(data, bom) {
		f.BOM = true
	}
	f.lineStarts = []int{0}
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			rec := f.record(diagnostics.InvalidUTF8, i, min(i+1, len(data)),
				fmt.Sprintf("malformed UTF-8 encoding at byte %d", i))
			return nil, &rec
		}
		if r == '\n' {
			f.lineStarts = append(f.lineStarts, i+1)
		}
		i += size
	}
	return f, nil
}

// ContentStart is the byte offset where source text begins.
func (f *File) ContentStart() int {
	if f.BOM {
		return len(bom)
	}
	return 0
}

// Pos derives the display position of a byte offset.
func (f *File) Pos(offset int) diagnostics.Pos {
	line := sort.Search(len(f.lineStarts), func(i int) bool {
		return f.lineStarts[i] > offset
	})
	start := f.lineStarts[line-1]
	if line == 1 && f.BOM && offset >= len(bom) {
		start = len(bom)
	}
	col := 1 + utf8.RuneCount(f.Bytes[start:min(offset, len(f.Bytes))])
	return diagnostics.Pos{Byte: offset, Line: line, Column: col}
}

// Span derives the display span of a half-open byte range.
func (f *File) Span(start, end int) diagnostics.Span {
	return diagnostics.Span{Start: f.Pos(start), End: f.Pos(end)}
}

// Record builds an error diagnostic for a byte range of this file.
func (f *File) Record(reason string, start, end int, message string) diagnostics.Record {
	return f.record(reason, start, end, message)
}

func (f *File) record(reason string, start, end int, message string) diagnostics.Record {
	return diagnostics.Record{
		Reason:    reason,
		Severity:  diagnostics.SeverityError,
		Path:      f.Path,
		FileIndex: f.Index,
		Module:    f.Module,
		Span:      f.Span(start, end),
		Message:   diagnostics.Sentence(message),
	}
}
