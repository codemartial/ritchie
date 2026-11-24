package rlr

import (
	"iter"
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// second returns the fault of a helper result.
func second[T any](_ T, f Fault) Fault { return f }

// must returns the value of a helper result expected to complete.
func must[T any](v T, f Fault) T {
	if f != NoFault {
		panic("helper reported " + f.Name() + " where completion was expected")
	}
	return v
}

func TestRenderBytesCoversEveryByteValue(t *testing.T) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	got := RenderBytes(all)
	if !strings.HasPrefix(got, `b"`) || !strings.HasSuffix(got, `"`) {
		t.Fatalf("rendering %q is not a quoted byte-array literal", got)
	}
	for _, c := range []byte(got) {
		if c < 0x20 || c > 0x7e {
			t.Errorf("rendering holds the non-printable byte %#x", c)
		}
	}
	tests := map[byte]string{
		0: `\x00`, '\n': `\n`, '\t': `\t`, '\r': `\r`, '\\': `\\`, '"': `\"`,
		'A': "A", ' ': " ", 0x7f: `\x7f`, 0xff: `\xff`, 0xe9: `\xe9`, 0x1f: `\x1f`,
	}
	for c, want := range tests {
		if single := RenderBytes([]byte{c}); single != `b"`+want+`"` {
			t.Errorf("byte %#x renders as %s, want b\"%s\"", c, single, want)
		}
	}
	if RenderBytes(nil) != `b""` {
		t.Errorf("empty array renders as %s", RenderBytes(nil))
	}
}

func TestRuneBoundaries(t *testing.T) {
	s := "héllo"
	want := map[int64]bool{-1: false, 0: true, 1: true, 2: false, 3: true, 4: true, 5: true, 6: true, 7: false}
	for i, w := range want {
		if got := IsRuneBoundary(s, i); got != w {
			t.Errorf("IsRuneBoundary(%q, %d) = %t, want %t", s, i, got, w)
		}
	}
	if !IsRuneBoundary("", 0) || IsRuneBoundary("", 1) {
		t.Error("the empty String has exactly one boundary, at 0")
	}
	four := "\U0001F600"
	for i := int64(1); i < 4; i++ {
		if IsRuneBoundary(four, i) {
			t.Errorf("interior byte %d of a four-byte scalar is a boundary", i)
		}
	}
}

func TestSlicesDistinguishBoundsFromBoundaries(t *testing.T) {
	s := "héllo"
	tests := []struct {
		lo, hi int64
		want   string
		err    Fault
	}{
		{0, 6, "héllo", NoFault},
		{0, 0, "", NoFault},
		{6, 6, "", NoFault},
		{1, 3, "é", NoFault},
		{3, 6, "llo", NoFault},
		{-1, 2, "", OutOfRange},
		{3, 1, "", OutOfRange},
		{0, 7, "", OutOfRange},
		{2, 7, "", OutOfRange},
		{0, 2, "", InvalidStringBoundary},
		{2, 3, "", InvalidStringBoundary},
		{2, 2, "", InvalidStringBoundary},
	}
	for _, tt := range tests {
		got, err := SliceString(s, tt.lo, tt.hi)
		if err != tt.err || got != tt.want {
			t.Errorf("SliceString(%q, %d, %d) = %q, %q; want %q, %q", s, tt.lo, tt.hi, got, err, tt.want, tt.err)
		}
	}
	b := []byte{1, 2, 3}
	sub := must(Slice(b, 1, 3))
	sub[0] = 9
	if b[1] != 2 {
		t.Error("a byte slice shares storage with its source")
	}
	if err := second(Slice(b, 0, 4)); err != OutOfRange {
		t.Errorf("out-of-range byte slice raised %v", err)
	}
	if err := second(Slice(b, 2, 1)); err != OutOfRange {
		t.Errorf("inverted byte slice raised %v", err)
	}
}

func TestIndexAndCheckRaiseBeforeTouchingTheArray(t *testing.T) {
	b := []byte{1, 2, 3}
	if must(index(b, 2)) != 3 {
		t.Error("index reads the wrong element")
	}
	for _, i := range []int64{-1, 3, math.MaxInt64, math.MinInt64} {
		if err := second(index(b, i)); err != OutOfRange {
			t.Errorf("index at %d raised %v", i, err)
		}
		if err := CheckIndex(b, i); err != OutOfRange {
			t.Errorf("CheckIndex at %d raised %v", i, err)
		}
	}
	if b[0] != 1 || b[1] != 2 || b[2] != 3 {
		t.Errorf("a failed check changed the array: %v", b)
	}
	if err := CheckIndex(b, 0); err != NoFault {
		t.Errorf("CheckIndex inside the array raised %v", err)
	}
	if err := second(index[uint8](nil, 0)); err != OutOfRange {
		t.Errorf("index on an empty array raised %v", err)
	}
}

func TestSequenceQueries(t *testing.T) {
	if Count("héllo") != 6 || Count([]byte{1, 2}) != 2 || Count("") != 0 {
		t.Error("Count is not the byte count")
	}
	if !IsEmpty("") || IsEmpty("a") || !IsEmpty([]byte{}) {
		t.Error("IsEmpty is not the zero-count test")
	}
	if !ContainsString("héllo", "él") || ContainsString("héllo", "z") || !ContainsString("x", "") {
		t.Error("ContainsString edges")
	}
	if !StartsWithString("héllo", "hé") || StartsWithString("héllo", "é") || !StartsWithString("", "") {
		t.Error("StartsWithString edges")
	}
	if !EndsWithString("héllo", "lo") || EndsWithString("héllo", "l") || !EndsWithString("a", "") {
		t.Error("EndsWithString edges")
	}
	if i, ok := IndexOfString("héllo", "llo"); i != 3 || !ok {
		t.Errorf("IndexOfString = %d, %t", i, ok)
	}
	if i, ok := IndexOfString("héllo", "z"); i != 0 || ok {
		t.Errorf("absent IndexOfString = %d, %t", i, ok)
	}
	if i, ok := IndexOfString("héllo", ""); i != 0 || !ok {
		t.Errorf("empty IndexOfString = %d, %t", i, ok)
	}
	if i, ok := IndexOfString("aab", "ab"); i != 1 || !ok {
		t.Errorf("IndexOfString first match = %d, %t", i, ok)
	}
	b := []byte{0, 0xff, 7}
	if !ContainsBytes(b, []byte{0xff}) || !StartsWithBytes(b, []byte{0}) || !EndsWithBytes(b, []byte{0xff, 7}) {
		t.Error("byte-array queries")
	}
	if i, ok := IndexOfBytes(b, []byte{7}); i != 2 || !ok {
		t.Errorf("IndexOfBytes = %d, %t", i, ok)
	}
	if i, ok := IndexOfBytes(b, nil); i != 0 || !ok {
		t.Errorf("empty IndexOfBytes = %d, %t", i, ok)
	}
	if i, ok := IndexOfBytes(b, []byte{1}); i != 0 || ok {
		t.Errorf("absent IndexOfBytes = %d, %t", i, ok)
	}
}

func TestBytewiseOrderIsUnsigned(t *testing.T) {
	if CompareBytes([]byte{0xff}, []byte{0x7f}) <= 0 {
		t.Error("0xff orders before 0x7f")
	}
	if CompareBytes([]byte("ab"), []byte("abc")) >= 0 || CompareBytes(nil, []byte{}) != 0 {
		t.Error("prefix and empty ordering")
	}
	if !(Min("Z", "a") == "Z" && Max("é", "z") == "é" && Min[uint8](200, 7) == 7 && Max[uint8](200, 255) == 255) {
		t.Error("Min and Max over String and Byte")
	}
	a, b := []byte{1}, []byte{2}
	least := MinBytes(a, b)
	least[0] = 9
	if a[0] != 1 {
		t.Error("MinBytes shares storage with its argument")
	}
	if greatest := MaxBytes(a, b); greatest[0] != 2 {
		t.Error("MaxBytes selects the wrong value")
	}
}

func TestStringFromBytesIsTheOneValidatingEntry(t *testing.T) {
	valid := [][]byte{[]byte(""), []byte("ascii"), []byte("héllo"), []byte("\U0001F600"), {0}}
	for _, b := range valid {
		if err := second(StringFromBytes(b)); err != NoFault {
			t.Errorf("valid %q raised %v", b, err)
		}
	}
	malformed := map[string][]byte{
		"stray continuation": {0x80},
		"truncated two-byte": {0xc3},
		"truncated three":    {0xe2, 0x82},
		"overlong":           {0xc0, 0xaf},
		"surrogate":          {0xed, 0xa0, 0x80},
		"above max scalar":   {0xf4, 0x90, 0x80, 0x80},
		"invalid lead":       {0xff},
		"valid then bad":     {'o', 'k', 0xff},
	}
	for name, b := range malformed {
		if err := second(StringFromBytes(b)); err != Malformed {
			t.Errorf("%s: raised %v, want Malformed!", name, err)
		}
	}
	if !utf8.ValidString(must(StringFromBytes(BytesFromString("héllo")))) {
		t.Error("round trip broke validity")
	}
	src := BytesFromString("ab")
	src[0] = 'z'
	if BytesFromString("ab")[0] != 'a' {
		t.Error("BytesFromString shares storage")
	}
}

func TestByteConversions(t *testing.T) {
	for _, i := range []int64{0, 1, 255} {
		if got := must(ByteFromInt(i)); int64(got) != i {
			t.Errorf("ByteFromInt(%d) = %d", i, got)
		}
	}
	for _, i := range []int64{-1, 256, math.MinInt64, math.MaxInt64} {
		if err := second(ByteFromInt(i)); err != OutOfRange {
			t.Errorf("ByteFromInt(%d) raised %v", i, err)
		}
	}
	if IntFromByte(255) != 255 || IntFromByte(0) != 0 {
		t.Error("IntFromByte")
	}
}

func TestFormatRendersEveryAdmittedType(t *testing.T) {
	got := must(Format("", "a ", int8(-1), " ", int16(2), " ", int32(3), " ", int64(4), " ", 1.5, " ", true, " ", uint8(200), " ", "s", " ", []byte{0, '"'}, " ", must(ParseDecimal("2.50")), ""))
	want := `a -1 2 3 4 1.5 true 200 s b"\x00\"" 2.50`
	if got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
	if must(Format("")) != "" {
		t.Error("an empty format is not the empty String")
	}
}

func TestCheckedLengthsWithSyntheticCounts(t *testing.T) {
	if must(addLength(1, 2)) != 3 || must(addLength(0, math.MaxInt64)) != math.MaxInt64 {
		t.Error("addLength sums")
	}
	for _, tt := range [][2]int64{{math.MaxInt64, 1}, {1, math.MaxInt64}, {-1, 0}, {0, -1}} {
		if err := second(addLength(tt[0], tt[1])); err != OutOfRange {
			t.Errorf("addLength(%d, %d) raised %v", tt[0], tt[1], err)
		}
	}
	if must(mulLength(3, 4)) != 12 || must(mulLength(0, math.MaxInt64)) != 0 || must(mulLength(math.MaxInt64, 1)) != math.MaxInt64 {
		t.Error("mulLength products")
	}
	for _, tt := range [][2]int64{{math.MaxInt64, 2}, {1 << 32, 1 << 32}, {-1, 1}} {
		if err := second(mulLength(tt[0], tt[1])); err != OutOfRange {
			t.Errorf("mulLength(%d, %d) raised %v", tt[0], tt[1], err)
		}
	}
	if must(replaceLength(10, 2, 3, 5)) != 14 || must(replaceLength(10, 2, 3, 1)) != 6 || must(replaceLength(4, 0, 0, 100)) != 4 {
		t.Error("replaceLength arithmetic")
	}
	if err := second(replaceLength(math.MaxInt64, 1, 0, 1)); err != OutOfRange {
		t.Errorf("replaceLength overflow raised %v", err)
	}
	if err := second(replaceLength(0, math.MaxInt64, 0, 2)); err != OutOfRange {
		t.Errorf("replaceLength product overflow raised %v", err)
	}
	lens := func(ns ...int64) iter.Seq[int64] { return slices.Values(ns) }
	if must(joinLength(lens(1, 2, 3), 3, 4)) != 14 || must(joinLength(lens(), 0, 4)) != 0 || must(joinLength(lens(5), 1, 4)) != 5 {
		t.Error("joinLength arithmetic")
	}
	if err := second(joinLength(lens(math.MaxInt64, 1), 2, 0)); err != OutOfRange {
		t.Errorf("joinLength overflow raised %v", err)
	}
	if err := second(joinLength(lens(0, 0, 0), 3, math.MaxInt64)); err != OutOfRange {
		t.Errorf("joinLength separator overflow raised %v", err)
	}
	if must(caseLength("straße", func(r rune) rune { return r })) != 7 {
		t.Error("caseLength sums encoded lengths")
	}
}

func TestTransformationsMatchTheStandardLibrary(t *testing.T) {
	inputs := []string{"", "straße", "ÀÉÎ ǅ", "i", "İ", "ſ", "\u00a0 a b \n", "\u2003x\u2003", "aaaa", "héy", "abc", "a::b", ",a,,b,", "\U0001F600x"}
	for _, s := range inputs {
		if must(ToUpper(s)) != strings.ToUpper(s) || must(ToLower(s)) != strings.ToLower(s) || Trim(s) != strings.TrimSpace(s) {
			t.Errorf("case or trim of %q differs from the standard library", s)
		}
		for _, sep := range []string{"", ",", ":", "a", "aa", "é", "::"} {
			if must(Join(must(Split(s, sep)), sep)) != s {
				t.Errorf("join(split(%q, %q)) does not round-trip", s, sep)
			}
			if sep == "" && s == "" {
				if got := must(Split(s, sep)); len(got) != 0 {
					t.Errorf("split of empty by empty = %q, want no pieces", got)
				}
				continue
			}
			if got, want := must(Split(s, sep)), strings.Split(s, sep); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Errorf("Split(%q, %q) = %q, want %q", s, sep, got, want)
			}
			if must(Replace(s, sep, "-")) != strings.ReplaceAll(s, sep, "-") {
				t.Errorf("Replace(%q, %q) differs from the standard library", s, sep)
			}
		}
	}
	if must(Replace("aaaa", "aa", "b")) != "bb" || must(Replace("aaa", "aa", "b")) != "ba" || must(Replace("héy", "", ".")) != ".h.é.y." || must(Replace("abc", "b", "")) != "ac" {
		t.Error("replacement edges")
	}
	if must(Join(nil, ",")) != "" || must(Join([]string{"solo"}, ",")) != "solo" || must(Join([]string{"a", "b"}, "")) != "ab" {
		t.Error("join edges")
	}
	if len(must(Split("", ","))) != 1 || strings.Join(must(Split("a", "a")), "|") != "|" {
		t.Error("split edges")
	}
	for _, s := range inputs {
		for _, out := range []string{must(ToUpper(s)), must(ToLower(s)), Trim(s), must(Replace(s, "a", "\U0001F600"))} {
			if !utf8.ValidString(out) {
				t.Errorf("result %q of %q is not valid UTF-8", out, s)
			}
		}
	}
}

func TestCloneIsIndependent(t *testing.T) {
	a := []byte{1, 2}
	b := Clone(a)
	b[0] = 9
	if a[0] != 1 {
		t.Error("Clone shares storage")
	}
	if got := Clone([]string(nil)); got == nil || len(got) != 0 {
		t.Error("Clone of an empty array is not an empty array")
	}
}

func TestLogRendersSequenceValues(t *testing.T) {
	out, _ := stubProcess(t)
	Log("", uint8(0))
	Log("", "héllo")
	Log("", []byte{0xff, 'a'})
	if got := out.String(); got != "0\nhéllo\nb\"\\xffa\"\n" {
		t.Errorf("sink output = %q", got)
	}
}

func TestRunReportsASequenceFaultOnce(t *testing.T) {
	installMetadata(t)
	out, exits := stubProcess(t)
	Run("app", func() error {
		Log("app", int64(1))
		s, f := SliceString("héllo", 0, 2)
		if f != NoFault {
			return Origin(f, 3, "")
		}
		Log("app", s)
		return nil
	})
	if got := out.String(); got != "1\nInvalidStringBoundary!\n  at app::main\n" {
		t.Errorf("sink output = %q", got)
	}
	if len(*exits) != 1 || (*exits)[0] != 1 {
		t.Errorf("exits = %v, want one exit with status 1", *exits)
	}
}
