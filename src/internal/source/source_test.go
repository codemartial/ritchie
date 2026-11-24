package source

import (
	"bytes"
	"testing"

	"ritchie.dev/internal/diagnostics"
)

// mustNew loads data as a source file that must be accepted.
func mustNew(t *testing.T, data string) *File {
	t.Helper()
	f, rec := New("t.ri", 0, []byte(data))
	if rec != nil {
		t.Fatalf("New() rejected valid input: %+v", *rec)
	}
	return f
}

func TestValidationRejectsFirstMalformedByte(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		byte int
	}{
		{"leading invalid byte", []byte{0xFF, 'a'}, 0},
		{"truncated sequence", []byte("ab\xC3"), 2},
		{"bad continuation", []byte("a\xC3\x28b"), 1},
		{"surrogate half", []byte("x\xED\xA0\x80"), 1},
		{"overlong encoding", []byte("\xC0\xAF"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rec := New("t.ri", 0, tt.data)
			if f != nil || rec == nil {
				t.Fatalf("New() accepted malformed UTF-8")
			}
			if rec.Reason != diagnostics.InvalidUTF8 {
				t.Errorf("reason = %q, want %q", rec.Reason, diagnostics.InvalidUTF8)
			}
			if rec.Span.Start.Byte != tt.byte {
				t.Errorf("start byte = %d, want %d", rec.Span.Start.Byte, tt.byte)
			}
			if rec.Span.End.Byte != tt.byte+1 {
				t.Errorf("end byte = %d, want half-open %d", rec.Span.End.Byte, tt.byte+1)
			}
		})
	}
}

func TestValidationHappensBeforeAnyScan(t *testing.T) {

	f, rec := New("t.ri", 0, []byte("Email = String\n\xFF"))
	if f != nil || rec == nil {
		t.Fatal("New() accepted a file with a late malformed byte")
	}
}

func TestLeadingBOMIsMetadata(t *testing.T) {
	f := mustNew(t, "\xEF\xBB\xBFmain")
	if !f.BOM {
		t.Error("BOM = false, want true")
	}
	if f.ContentStart() != 3 {
		t.Errorf("ContentStart() = %d, want 3", f.ContentStart())
	}

	pos := f.Pos(3)
	if pos.Line != 1 || pos.Column != 1 {
		t.Errorf("Pos(3) = line %d column %d, want line 1 column 1", pos.Line, pos.Column)
	}
}

func TestNoBOMKeepsContentStartZero(t *testing.T) {
	f := mustNew(t, "main")
	if f.BOM || f.ContentStart() != 0 {
		t.Errorf("BOM = %v ContentStart = %d, want false 0", f.BOM, f.ContentStart())
	}
}

func TestExactScalarPreservation(t *testing.T) {

	data := "// café café STRASSE straße\n"
	f := mustNew(t, data)
	if !bytes.Equal(f.Bytes, []byte(data)) {
		t.Error("source bytes were altered by decoding")
	}
}

func TestPositionDerivation(t *testing.T) {
	f := mustNew(t, "ab\ncdéf\ng")
	tests := []struct {
		offset, line, column int
	}{
		{0, 1, 1},
		{1, 1, 2},
		{2, 1, 3},
		{3, 2, 1},
		{5, 2, 3},
		{7, 2, 4},
		{9, 3, 1},
		{10, 3, 2},
	}
	for _, tt := range tests {
		pos := f.Pos(tt.offset)
		if pos.Line != tt.line || pos.Column != tt.column {
			t.Errorf("Pos(%d) = %d:%d, want %d:%d",
				tt.offset, pos.Line, pos.Column, tt.line, tt.column)
		}
	}
}

func TestBOMFirstLineColumns(t *testing.T) {
	f := mustNew(t, "\xEF\xBB\xBFab\ncd")
	if pos := f.Pos(4); pos.Line != 1 || pos.Column != 2 {
		t.Errorf("Pos(4) = %d:%d, want 1:2", pos.Line, pos.Column)
	}

	if pos := f.Pos(6); pos.Line != 2 || pos.Column != 1 {
		t.Errorf("Pos(6) = %d:%d, want 2:1", pos.Line, pos.Column)
	}
}

func TestLaterFEFFIsSourceText(t *testing.T) {

	f := mustNew(t, "ab\uFEFFcd")
	if f.BOM {
		t.Error("BOM = true for a file without a leading BOM")
	}
	if !bytes.Contains(f.Bytes, []byte("\uFEFF")) {
		t.Error("interior U+FEFF was stripped")
	}
}
