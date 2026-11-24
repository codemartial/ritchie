package rlr

import "testing"

func TestStructValueDisplaysUnderTheFormattingModule(t *testing.T) {
	inner := StructValue{Module: "geo", Name: "Point", Fields: []Field{{"x", int64(1)}, {"y", int8(-2)}}}
	outer := StructValue{Module: "app", Name: "Box", Fields: []Field{
		{"at", inner}, {"label", "a \"b\"\n"}, {"tag", []byte{0, 'z'}}, {"ratio", 0.5}, {"on", true}, {"b", uint8(7)},
	}}
	tests := []struct {
		name   string
		value  StructValue
		module string
		want   string
	}{
		{"local", inner, "geo", `Point { x: 1, y: -2 }`},
		{"foreign", inner, "app", `geo::Point { x: 1, y: -2 }`},
		{"anonymous module", StructValue{Name: "Point", Fields: []Field{{"x", int64(1)}}}, "", `Point { x: 1 }`},
		{"empty", StructValue{Module: "geo", Name: "Empty"}, "geo", `Empty {}`},
		{"nested keeps the outer formatting module", outer, "app",
			`Box { at: geo::Point { x: 1, y: -2 }, label: "a \"b\"\n", tag: b"\x00z", ratio: 0.5, on: true, b: 7 }`},
		{"nested from a third module", outer, "cli",
			`app::Box { at: geo::Point { x: 1, y: -2 }, label: "a \"b\"\n", tag: b"\x00z", ratio: 0.5, on: true, b: 7 }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Display(tt.module); got != tt.want {
				t.Errorf("display = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestQuoteStringUsesTheLiteralEscapes(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", `""`},
		{"plain", `"plain"`},
		{"a\"b", `"a\"b"`},
		{"back\\slash", `"back\\slash"`},
		{"line\nfeed\ttab\rreturn", `"line\nfeed\ttab\rreturn"`},
		{"nul\x00", `"nul\0"`},
		{"bell\x07", `"bell\u{7}"`},
		{"del\x7f", `"del\u{7f}"`},
		{"héllo 😀", `"héllo 😀"`},
	}
	for _, tt := range tests {
		if got := QuoteString(tt.in); got != tt.want {
			t.Errorf("QuoteString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestLogAndFormatRenderStructsUnderTheirModule(t *testing.T) {
	out, _ := stubProcess(t)
	p := StructValue{Module: "geo", Name: "Point", Fields: []Field{{"s", "q"}}}
	Log("geo", p)
	Log("app", p)
	Log("app", "standalone \"text\"")
	Log("app", must(Format("geo", "<", p, "> ", "raw", " ", []byte("b"))))
	want := "Point { s: \"q\" }\ngeo::Point { s: \"q\" }\nstandalone \"text\"\n<Point { s: \"q\" }> raw b\"b\"\n"
	if got := out.String(); got != want {
		t.Errorf("sink output = %q, want %q", got, want)
	}
}
