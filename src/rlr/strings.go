package rlr

import (
	"iter"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format renders a formatted String under the formatting module.
func Format(module string, parts ...any) (string, Fault) {
	rendered := make([]string, len(parts))
	total := int64(0)
	for i, part := range parts {
		rendered[i] = render(part, module)
		var f Fault
		if total, f = addLength(total, int64(len(rendered[i]))); f != NoFault {
			return "", f
		}
	}
	var out strings.Builder
	out.Grow(int(total))
	for _, r := range rendered {
		out.WriteString(r)
	}
	return out.String(), NoFault
}

// caseLength is the checked byte length of the simple case mapping of s under mapping.
func caseLength(s string, mapping func(rune) rune) (int64, Fault) {
	total := int64(0)
	for _, r := range s {
		var f Fault
		if total, f = addLength(total, int64(utf8.RuneLen(mapping(r)))); f != NoFault {
			return 0, f
		}
	}
	return total, NoFault
}

// ToUpper is the locale-independent simple uppercase mapping of a String.
func ToUpper(s string) (string, Fault) { return mapCase(s, unicode.ToUpper, strings.ToUpper) }

// ToLower is the locale-independent simple lowercase mapping of a String.
func ToLower(s string) (string, Fault) { return mapCase(s, unicode.ToLower, strings.ToLower) }

const caseSafe = math.MaxInt64 / utf8.UTFMax

// mapCase is the simple case mapping of s under convert after checking the mapped length when s is long enough to exceed the limit.
func mapCase(s string, mapping func(rune) rune, convert func(string) string) (string, Fault) {
	if len(s) > caseSafe {
		if _, f := caseLength(s, mapping); f != NoFault {
			return "", f
		}
	}
	return convert(s), NoFault
}

// Trim removes leading and trailing Unicode whitespace.
func Trim(s string) string {
	return strings.TrimSpace(s)
}

// replaceLength is the checked byte length of replacing count occurrences of an old-length pattern with a new-length one in s.
func replaceLength(sLen, count, oldLen, newLen int64) (int64, Fault) {
	if newLen >= oldLen {
		grown, f := mulLength(count, newLen-oldLen)
		if f != NoFault {
			return 0, f
		}
		return addLength(sLen, grown)
	}
	removed, f := mulLength(count, oldLen-newLen)
	if f != NoFault {
		return 0, f
	}
	return sLen - removed, NoFault
}

// Replace replaces every non-overlapping occurrence of old in s.
func Replace(s, old, new string) (string, Fault) {
	sLen, oldLen, newLen := int64(len(s)), int64(len(old)), int64(len(new))
	if _, f := replaceLength(sLen, sLen+1, oldLen, newLen); f != NoFault {
		if _, f := replaceLength(sLen, int64(strings.Count(s, old)), oldLen, newLen); f != NoFault {
			return "", f
		}
	}
	return strings.ReplaceAll(s, old, new), NoFault
}

// Split is the array of pieces around each occurrence of sep.
func Split(s, sep string) ([]string, Fault) {
	if s == "" && sep == "" {
		return []string{}, NoFault
	}
	return strings.Split(s, sep), NoFault
}

// joinLength is the checked byte length of joining count parts of the given lengths with a separator of length sepLen.
func joinLength(partLens iter.Seq[int64], count int, sepLen int64) (int64, Fault) {
	total := int64(0)
	for n := range partLens {
		var f Fault
		if total, f = addLength(total, n); f != NoFault {
			return 0, f
		}
	}
	if count > 1 {
		separators, f := mulLength(int64(count-1), sepLen)
		if f != NoFault {
			return 0, f
		}
		return addLength(total, separators)
	}
	return total, NoFault
}

// Join places sep between the elements of parts.
func Join(parts []string, sep string) (string, Fault) {
	lengths := func(yield func(int64) bool) {
		for _, p := range parts {
			if !yield(int64(len(p))) {
				return
			}
		}
	}
	if _, f := joinLength(lengths, len(parts), int64(len(sep))); f != NoFault {
		return "", f
	}
	return strings.Join(parts, sep), NoFault
}
