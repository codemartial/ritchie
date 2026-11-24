package syntax

import (
	"slices"
	"strings"
	"testing"

	"ritchie.dev/internal/source"
)

// lexKinds returns the kinds of the tokens of a text.
func lexKinds(t *testing.T, text string) []Kind {
	t.Helper()
	f, rec := source.New("lex", 0, []byte(text))
	if rec != nil {
		t.Fatal(rec.Message)
	}
	toks, _ := Lex(f)
	var kinds []Kind
	for _, tok := range toks {
		kinds = append(kinds, tok.Kind)
	}
	return kinds
}

func TestTheLexerClassifiesEachInputAsItsStatedTokenSequence(t *testing.T) {
	tests := []struct {
		fact  string
		input string
		want  []Kind
	}{
		{"a bare word is sorted by case into its archetype", "x T PI Int", []Kind{Variable, TypeParam, Constant, TypeName, EOF}},
		{"a sigil before a type name forms one token", "$Account @Mailer\n%Order\n%byDay", []Kind{EntityType, ServiceType, Newline, ProjectionType, Newline, QueryName, EOF}},
		{"percent between operands is the operator even without spaces", "a%b", []Kind{Variable, Percent, Variable, EOF}},
		{"percent before an operand is the projection sigil", "x = %byDay", []Kind{Variable, Assign, QueryName, EOF}},
		{"an attached bang after a type is the error suffix", "NotFound!", []Kind{ErrorType, EOF}},
		{"a bang before equals is the inequality operator", "Int!=x", []Kind{TypeName, NotEq, Variable, EOF}},
		{"any with an attached bang is the remaining-failures arm head", "any! e", []Kind{KwAnyError, Variable, EOF}},
		{"a newline at depth zero is one token and a run collapses", "a\n\n\nb", []Kind{Variable, Newline, Variable, EOF}},
		{"a newline inside parentheses produces no token", "f(a,\nb)", []Kind{Variable, LParen, Variable, Comma, Variable, RParen, EOF}},
		{"a newline inside brackets produces no token", "[a,\nb]", []Kind{LBracket, Variable, Comma, Variable, RBracket, EOF}},
		{"a newline inside braces is a token", "{a\nb}", []Kind{LBrace, Variable, Newline, Variable, RBrace, EOF}},
		{"a semicolon is a newline token", "a; b", []Kind{Variable, Newline, Variable, EOF}},
		{"a trailing backslash suppresses the newline", "a \\\n+ b", []Kind{Variable, Plus, Variable, EOF}},
		{"a leading newline produces no token", "\n\na", []Kind{Variable, EOF}},
		{"a line comment ends at its newline", "a // c\nb", []Kind{Variable, Newline, Variable, EOF}},
		{"a block comment nests", "a /* x /* y */ z */ b", []Kind{Variable, Variable, EOF}},
		{"a date is claimed at its first hyphen", "d2026-05-31 d2026", []Kind{DateLit, Variable, EOF}},
		{"a signed six-digit year is a date", "d-000001-12-31", []Kind{DateLit, EOF}},
		{"a time is claimed at its first colon", "t09:00:00.5 t17", []Kind{TimeLit, Variable, EOF}},
		{"a datetime is claimed at the T", "2026-05-31T17:50:00Z 2026-05-31", []Kind{DateTimeLit, IntLit, Minus, IntLit, Minus, IntLit, EOF}},
		{"adjacent integer and unit segments form one duration", "1h30m 100ms 5s", []Kind{DurationLit, DurationLit, DurationLit, EOF}},
		{"an integer takes its radix prefix and separators", "0xFF_FF 1_000 0b1010 0o755", []Kind{IntLit, IntLit, IntLit, IntLit, EOF}},
		{"a point is a float point only when a digit follows", "1.5 1.abs", []Kind{FloatLit, IntLit, Dot, Variable, EOF}},
		{"an exponent makes a float", "2e10 1.6e-19", []Kind{FloatLit, FloatLit, EOF}},
		{"string forms carry their prefix", `"s" ` + "`r` " + `b"b" ` + "b`rb` " + `f"f" ` + "f`rf`", []Kind{StringLit, RawStringLit, BytesLit, RawBytesLit, FStringLit, FStringLit, EOF}},
		{"multi-character symbols are matched greedily", "??= ?? == => -> :: := <= >= != += %=", []Kind{CoalesceAssign, CoalesceOp, EqEq, FatArrow, Arrow, ColonColon, Define, LessEq, GreaterEq, NotEq, PlusAssign, PercentAssign, EOF}},
		{"two closing angles are two tokens", "Stack<Pair<Int, String>>", []Kind{TypeName, Less, TypeName, Less, TypeName, Comma, TypeName, Greater, Greater, EOF}},
		{"a rejected symbol is one invalid token", "a && b", []Kind{Variable, Invalid, Variable, EOF}},
		{"a reserved symbol is one invalid token", "a |> b", []Kind{Variable, Invalid, Variable, EOF}},
		{"less then minus before an operand is a comparison with negation", "a<-1", []Kind{Variable, Less, Minus, IntLit, EOF}},
		{"the hash is a token after an entity type", "$Order#", []Kind{EntityType, Hash, EOF}},
		{"a non-ASCII character outside a literal is invalid", "é", []Kind{Invalid, EOF}},
		{"a BOM is dropped", "\uFEFFa", []Kind{Variable, EOF}},
	}
	for _, tt := range tests {
		if got := lexKinds(t, tt.input); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %q lexes as %v, want %v", tt.fact, tt.input, got, tt.want)
		}
	}
}

func TestAFormattedStringTokenHoldsItsTextPartsAndSlotSpans(t *testing.T) {
	f, _ := source.New("lex", 0, []byte("f\"a{{b}} {x + 1} c\""))
	toks, diags := Lex(f)
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %s", describe(diags))
	}
	want := []Part{{Text: "a{b} "}, {Slot: true, Start: 10, End: 15}, {Text: " c"}}
	got := toks[0].Parts
	if len(got) != len(want) {
		t.Fatalf("parts = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("part %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if slot := strings.TrimSpace(f.Text[got[1].Start:got[1].End]); slot != "x + 1" {
		t.Errorf("slot text = %q, want %q", slot, "x + 1")
	}
}
