package syntax

import "testing"

func TestEachLiteralCanonicalisesToItsStatedText(t *testing.T) {
	tests := []struct {
		fact string
		kind Kind
		text string
		want string
	}{
		{"a decimal integer drops its separators", IntLit, "1_000_000", "1000000"},
		{"a hex integer is written in decimal", IntLit, "0xFF", "255"},
		{"an octal integer is written in decimal", IntLit, "0o755", "493"},
		{"a binary integer is written in decimal", IntLit, "0b1010_0001", "161"},
		{"leading zeros are dropped", IntLit, "007", "7"},
		{"zero stays zero", IntLit, "0", "0"},
		{"an integer beyond 64 bits keeps its digits", IntLit, "0x1_0000_0000_0000_0000", "18446744073709551616"},
		{"a float drops its separators", FloatLit, "3.141_59", "3.14159"},
		{"a float keeps its trailing fraction zeros", FloatLit, "1.10", "1.10"},
		{"an exponent is lowercase without a plus", FloatLit, "6.022E+23", "6.022e23"},
		{"an exponent keeps its minus", FloatLit, "1.6e-19", "1.6e-19"},
		{"an exponent drops its leading zeros", FloatLit, "2e010", "2e10"},
		{"a negative zero exponent is zero", FloatLit, "2e-0", "2e0"},
		{"a float drops leading zeros of its whole part", FloatLit, "01.5", "1.5"},
		{"a string keeps its letter escapes", StringLit, `"a\nb\t\"\\\0"`, `"a\nb\t\"\\\0"`},
		{"a unicode escape becomes the character", StringLit, `"\u{41}"`, `"A"`},
		{"a non-ASCII character stays literal", StringLit, `"é"`, `"é"`},
		{"a control character becomes a unicode escape", StringLit, `"\u{1}"`, `"\u{1}"`},
		{"a raw string becomes a quoted string", RawStringLit, "`a\\b\n\"c\"`", `"a\\b\n\"c\""`},
		{"a byte array keeps printable ASCII and escapes the rest", BytesLit, `b"A\x00\xff"`, `b"A\0\xff"`},
		{"a raw byte array contributes its UTF-8 bytes", RawBytesLit, "b`é`", `b"\xc3\xa9"`},
		{"a byte array unicode escape contributes UTF-8 bytes", BytesLit, `b"\u{e9}"`, `b"\xc3\xa9"`},
		{"a duration is written in descending units", DurationLit, "90m", "1h30m"},
		{"a duration drops zero segments", DurationLit, "1h0m", "1h"},
		{"a date keeps its form", DateLit, "d2026-05-31", "d2026-05-31"},
		{"a time drops trailing fraction zeros", TimeLit, "t09:00:00.500", "t09:00:00.5"},
		{"a datetime at offset zero is written with Z", DateTimeLit, "2026-05-31T17:50:00+00:00", "2026-05-31T17:50:00Z"},
		{"a datetime keeps its offset", DateTimeLit, "2026-05-31T17:50:00.5+10:00", "2026-05-31T17:50:00.5+10:00"},
		{"true is itself", KwTrue, "true", "true"},
		{"none is itself", KwNone, "none", "none"},
	}
	for _, tt := range tests {
		l := &Lit{Kind: tt.kind, Text: tt.text}
		if got := l.Canonical(); got != tt.want {
			t.Errorf("%s: %s %s canonicalises to %s, want %s", tt.fact, tt.kind, tt.text, got, tt.want)
		}
	}
}
