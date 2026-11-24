// This file canonicalises literals to one text per value, so that the printer and
// equality see values.
package syntax

import (
	"fmt"
	"math/big"
	"strings"

	"ritchie.dev/rlr"
)

// Canonical is the canonical source text of the literal's value.
func (l *Lit) Canonical() string {
	switch l.Kind {
	case IntLit:
		return canonicalInt(l.Text)
	case FloatLit:
		return canonicalFloat(l.Text)
	case StringLit, RawStringLit:
		return QuoteString(string(literalBytes(l.Kind, l.Text)))
	case BytesLit, RawBytesLit:
		return QuoteBytes(literalBytes(l.Kind, l.Text))
	case DurationLit:
		d, err := rlr.ParseDuration(l.Text)
		if err == nil {
			return d.String()
		}
	case DateLit:
		d, err := rlr.ParseDate(l.Text)
		if err == nil {
			return d.String()
		}
	case TimeLit:
		t, err := rlr.ParseTime(l.Text)
		if err == nil {
			return t.String()
		}
	case DateTimeLit:
		dt, err := rlr.ParseDateTime(l.Text)
		if err == nil {
			return dt.String()
		}
	}
	return l.Text
}

// Bytes is the decoded value of a string or byte array literal.
func (l *Lit) Bytes() []byte { return literalBytes(l.Kind, l.Text) }

// canonicalInt writes an integer literal in decimal without separators.
func canonicalInt(text string) string {
	text = strings.ReplaceAll(text, "_", "")
	if len(text) > 1 && text[0] == '0' {
		base := 0
		switch text[1] {
		case 'x':
			base = 16
		case 'o':
			base = 8
		case 'b':
			base = 2
		}
		if base != 0 {
			if v, ok := new(big.Int).SetString(text[2:], base); ok {
				return v.String()
			}
			return text
		}
	}
	return strings.TrimLeft(text[:len(text)-1], "0") + text[len(text)-1:]
}

// canonicalFloat removes separators, leading zeros, and the exponent sign and case, keeping the written digits.
func canonicalFloat(text string) string {
	text = strings.ReplaceAll(text, "_", "")
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(text), "e")
	whole, fraction, hasFraction := strings.Cut(mantissa, ".")
	whole = strings.TrimLeft(whole[:len(whole)-1], "0") + whole[len(whole)-1:]
	out := whole
	if hasFraction {
		out += "." + fraction
	}
	if hasExponent {
		negative := strings.HasPrefix(exponent, "-")
		exponent = strings.TrimLeft(exponent, "+-")
		exponent = strings.TrimLeft(exponent[:len(exponent)-1], "0") + exponent[len(exponent)-1:]
		if negative && exponent != "0" {
			exponent = "-" + exponent
		}
		out += "e" + exponent
	}
	return out
}

// literalBytes decodes the value of a string or byte array literal from its source text.
func literalBytes(kind Kind, text string) []byte {
	raw := kind == RawStringLit || kind == RawBytesLit
	bytes := kind == BytesLit || kind == RawBytesLit
	open := strings.IndexAny(text, "\"`")
	body := text[open+1 : len(text)-1]
	if raw {
		return []byte(body)
	}
	var out []byte
	for i := 0; i < len(body); {
		if body[i] != '\\' {
			out = append(out, body[i])
			i++
			continue
		}
		value, n, err := decodeEscape(body[i:], bytes)
		if err == "" {
			out = append(out, value...)
		}
		i += n
	}
	return out
}

// QuoteString writes a string value as a quoted literal with the fewest escapes.
func QuoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		writeEscaped(&b, r)
	}
	b.WriteByte('"')
	return b.String()
}

func writeEscaped(b *strings.Builder, r rune) {
	switch r {
	case '\n':
		b.WriteString(`\n`)
	case '\t':
		b.WriteString(`\t`)
	case '\r':
		b.WriteString(`\r`)
	case '\\':
		b.WriteString(`\\`)
	case '"':
		b.WriteString(`\"`)
	case 0:
		b.WriteString(`\0`)
	default:
		if r < 0x20 || r == 0x7F {
			fmt.Fprintf(b, `\u{%x}`, r)
			return
		}
		b.WriteRune(r)
	}
}

// QuoteBytes writes a byte array value as a quoted literal with hex escapes for bytes outside printable ASCII.
func QuoteBytes(data []byte) string {
	var b strings.Builder
	b.WriteString(`b"`)
	for _, c := range data {
		switch {
		case c == '\n' || c == '\t' || c == '\r' || c == '\\' || c == '"' || c == 0:
			writeEscaped(&b, rune(c))
		case c >= 0x20 && c < 0x7F:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// formatText writes one text part of a formatted string, doubling braces, with escapes unless raw.
func formatText(s string, raw bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '{':
			b.WriteString("{{")
		case r == '}':
			b.WriteString("}}")
		case raw:
			b.WriteRune(r)
		default:
			writeEscaped(&b, r)
		}
	}
	return b.String()
}
