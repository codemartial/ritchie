package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"
)

// rec returns an error record at fileIndex spanning bytes start to end of line 1.
func rec(fileIndex, start, end int, reason string) Record {
	return Record{
		Reason:    reason,
		Severity:  SeverityError,
		Path:      "p.ri",
		FileIndex: fileIndex,
		Span: Span{
			Start: Pos{Byte: start, Line: 1, Column: start + 1},
			End:   Pos{Byte: end, Line: 1, Column: end + 1},
		},
		Message: "m",
	}
}

func TestSortOrder(t *testing.T) {
	records := []Record{
		rec(1, 0, 1, "b.reason"),
		rec(0, 5, 6, "a.reason"),
		rec(0, 2, 9, "a.reason"),
		rec(0, 2, 3, "z.reason"),
		rec(0, 2, 3, "a.reason"),
	}
	Sort(records)
	type key struct {
		file, start, end int
		reason           string
	}
	var got []key
	for _, r := range records {
		got = append(got, key{r.FileIndex, r.Span.Start.Byte, r.Span.End.Byte, r.Reason})
	}
	want := []key{
		{0, 2, 3, "a.reason"},
		{0, 2, 3, "z.reason"},
		{0, 2, 9, "a.reason"},
		{0, 5, 6, "a.reason"},
		{1, 0, 1, "b.reason"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHumanForm(t *testing.T) {
	plain := rec(0, 0, 1, "syntax.reserved-form")
	plain.Message = Sentence(plain.Message)
	withRepl := rec(0, 2, 4, "syntax.excluded-form")
	withRepl.Message = Sentence(withRepl.Message)
	withRepl.Replacement = "and"
	inModule := rec(1, 5, 6, "name.unknown")
	inModule.Message = Sentence(inModule.Message)
	inModule.Path = "src/q.ri"
	inModule.Module = "bank"

	var b strings.Builder
	if err := WriteHuman(&b, []Record{plain, withRepl, inModule}); err != nil {
		t.Fatal(err)
	}
	want := "p.ri:1:1: error: M. [syntax.reserved-form]\n" +
		"p.ri:1:3: error: M. Use \"and\". [syntax.excluded-form]\n" +
		"src/q.ri:1:6: error: M. [name.unknown] in module bank\n"
	if b.String() != want {
		t.Errorf("human form =\n%s\nwant\n%s", b.String(), want)
	}
}

func TestSentence(t *testing.T) {
	tests := map[string]string{
		"a message":                   "A message.",
		"first clause; second clause": "First clause. Second clause.",
		`"x" is not valid`:            `"x" is not valid.`,
		`the literal "\"x\"" fails`:   `The literal "\"x\"" fails.`,
		"the ` token is invalid":      "The ` token is invalid.",
		`expected ")" to close`:       `Add ")" to close.`,
		"every value may not differ":  "Each value cannot differ.",
		"x cannot stand as y":         "X cannot be y.",
		"x beside y":                  "X beside y.",
		"message?":                    "Message?",
		"message\ndetail":             "Message.\ndetail",
		`"place" can raise Out!`:      `"place" can raise Out!`,
		`"f" lists X; "g" lists Y`:    `"f" lists X. "g" lists Y.`,
		`"every; x may not" is kept`:  `"every; x may not" is kept.`,
		`use "x"`:                     `Use "x".`,
	}
	for input, want := range tests {
		if got := Sentence(input); got != want {
			t.Errorf("Sentence(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCode(t *testing.T) {
	for input, want := range map[string]string{
		"x":   `"x"`,
		"<x>": `"<x>"`,
	} {
		if got := Code(input); got != want {
			t.Errorf("Code(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestJSONLShape(t *testing.T) {
	r := rec(0, 0, 1, "syntax.reserved-form")
	r.Replacement = ""
	withRepl := rec(0, 2, 4, "syntax.excluded-form")
	withRepl.Replacement = "and"
	cycle := rec(0, 5, 6, ModuleDependencyCycle)
	cycle.CycleModules = []string{"b", "a", "c"}

	var b strings.Builder
	if err := WriteJSONL(&b, []Record{r, withRepl, cycle}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want one JSON object per record", len(lines))
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 is not JSON: %v", err)
	}
	for _, field := range []string{"reason", "severity", "path", "span", "message"} {
		if _, present := first[field]; !present {
			t.Errorf("record lacks %q", field)
		}
	}
	if _, present := first["replacement"]; present {
		t.Error("replacement is present without a specified replacement")
	}
	if _, present := first["cycle_modules"]; present {
		t.Error("cycle_modules is present on a non-cycle diagnostic")
	}
	span := first["span"].(map[string]any)
	start := span["start"].(map[string]any)
	for _, field := range []string{"byte", "line", "column"} {
		if _, present := start[field]; !present {
			t.Errorf("span start lacks %q", field)
		}
	}

	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 2 is not JSON: %v", err)
	}
	if second["replacement"] != "and" {
		t.Errorf("replacement = %v, want and", second["replacement"])
	}
	if _, present := second["cycle_modules"]; present {
		t.Error("cycle_modules is present on a non-cycle diagnostic")
	}

	var third map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Fatalf("line 3 is not JSON: %v", err)
	}
	values, ok := third["cycle_modules"].([]any)
	if !ok {
		t.Fatalf("cycle_modules = %T, want a JSON array", third["cycle_modules"])
	}
	wantCycle := map[string]bool{"a": true, "b": true, "c": true}
	seenCycle := map[string]bool{}
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("cycle_modules contains %T, want strings", value)
		}
		if seenCycle[name] {
			t.Errorf("cycle_modules repeats %q", name)
		}
		seenCycle[name] = true
	}
	if len(seenCycle) != len(wantCycle) {
		t.Errorf("cycle_modules names = %v, want %v", seenCycle, wantCycle)
	}
	for name := range wantCycle {
		if !seenCycle[name] {
			t.Errorf("cycle_modules lacks %q", name)
		}
	}
}
