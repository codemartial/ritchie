package rlr

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"
)

type pair struct {
	code int64
	note string
}

// registerPair registers one error type with a pair payload at the given position after the catalog, under module.
func registerPair(t *testing.T, module string, before int) {
	t.Helper()
	prevTypes, prevLocs := errorTypes, locations
	t.Cleanup(func() { errorTypes, locations = prevTypes, prevLocs })
	var metas []ErrorTypeMeta
	for i := range before {
		metas = append(metas, ErrorTypeMeta{Module: module, Name: "Filler" + string(rune('A'+i)) + "!"})
	}
	metas = append(metas, ErrorTypeMeta{Module: module, Name: "Pair!",
		Fields: []ErrorFieldMeta{{Name: "code", TypeName: "Int"}, {Name: "note", TypeName: "String"}},
		Render: func(p any) []string { v := p.(pair); return []string{render(v.code, module), QuoteString(v.note)} },
		Form: func(p any) StructValue {
			v := p.(pair)
			return StructValue{Module: module, Name: "Pair!", Fields: []Field{{Name: "code", Value: v.code}, {Name: "note", Value: v.note}}}
		},
		Decode: func(in *Tokens) (any, error) {
			var v pair
			err := DecodeObject(in, []string{"code", "note"}, nil, func(i int) (err error) {
				switch i {
				case 0:
					v.code, err = DecodeInt[int64](in)
				case 1:
					v.note, err = DecodeString(in)
				}
				return err
			})
			return v, err
		}})
	RegisterErrorTypes(metas)
	RegisterLocations(nil)
}

// optionOf is the decoder of a nullable through the decoder of its element.
func optionOf[T any](elem Decoder[T]) Decoder[Option[T]] {
	return func(in *Tokens) (Option[T], error) { return DecodeOption(in, elem) }
}

// arrayOf is the decoder of an array through the decoder of its elements.
func arrayOf[T any](elem Decoder[T]) Decoder[[]T] {
	return func(in *Tokens) ([]T, error) { return DecodeArray(in, elem) }
}

// dictOf is the decoder of a dictionary through the decoders of its keys and values.
func dictOf[K comparable, V any](key Decoder[K], value Decoder[V]) Decoder[map[K]V] {
	return func(in *Tokens) (map[K]V, error) { return DecodeDict(in, key, value) }
}

// membersOf is the decoder of an object with the given member names that keeps the text of each member's value.
func membersOf(names ...string) Decoder[map[string]string] {
	return func(in *Tokens) (map[string]string, error) {
		texts := map[string]string{}
		err := DecodeObject(in, names, nil, func(i int) error {
			v, err := in.ReadValue()
			texts[names[i]] = string(v)
			return err
		})
		return texts, err
	}
}

// chainNode mirrors the Go type that the backend emits for a struct with an Int field and a nullable field of the struct's own type.
type chainNode struct {
	v    int64
	next Box[chainNode]
}

// decodeChainNode mirrors the struct decoder that the backend generates for the struct of chainNode.
func decodeChainNode(in *Tokens) (v chainNode, err error) {
	err = DecodeObject(in, []string{"v", "next"}, nil, func(i int) (err error) {
		switch i {
		case 0:
			v.v, err = DecodeInt[int64](in)
		case 1:
			v.next, err = DecodeBox(in, decodeChainNode)
		}
		return err
	})
	return v, err
}

// chainBody is the JSON form of a chain of n nodes, nested n levels deep.
func chainBody(n int) Raw {
	return Raw(strings.Repeat(`{"v":1,"next":`, n-1) + `{"v":1,"next":null}` + strings.Repeat("}", n-1))
}

func TestDecodingAllocatesInProportionToTheNestingDepth(t *testing.T) {
	count := func(nodes int) float64 {
		body := chainBody(nodes)
		return testing.AllocsPerRun(5, func() {
			if _, err := Decode(body, decodeChainNode); err != nil {
				t.Fatal(err)
			}
		})
	}
	if shallow, deep := count(1000), count(2000); deep >= 3*shallow {
		t.Errorf("a chain of 2000 nodes allocates %.0f times and a chain of 1000 nodes %.0f times, want less than 3 times as many", deep, shallow)
	}
}

func TestDateTimeReductionsAreReportedInTheOrderOfTheBody(t *testing.T) {
	registerPair(t, "lib", 0)
	saved := Diag
	defer func() { Diag = saved }()
	read := decodeRequest("lib", Method{Decode: func(in *Tokens) ([]any, error) {
		var a, b DateTime
		err := DecodeObject(in, []string{"a", "b"}, nil, func(i int) (err error) {
			switch i {
			case 0:
				a, err = DecodeDateTime(in)
			case 1:
				b, err = DecodeDateTime(in)
			}
			return err
		})
		return []any{a, b}, err
	}})
	const (
		first  = `"2026-01-01T00:00:00.1111111111Z"`
		second = `"2026-01-01T00:00:00.2222222222Z"`
		whole  = `"2026-01-01T00:00:00Z"`
	)
	tests := []struct {
		body     string
		accepted bool
		reports  []string
	}{
		{`{"b":` + second + `,"a":` + first + `}`, true, []string{second, first}},
		{`{"a":` + first + `,"b":` + whole + `,"b":` + whole + `}`, false, []string{first}},
		{`{"b":5,"a":` + first + `}`, false, nil},
		{`{"a":` + first + `,"b":5}`, false, []string{first}},
	}
	for _, tt := range tests {
		var reports strings.Builder
		Diag = &reports
		_, malformed := read([]byte(tt.body))
		var want strings.Builder
		for _, text := range tt.reports {
			fmt.Fprintf(&want, "ritchie runtime: the DateTime %q has more than nine fractional digits and was truncated after the ninth\n", text[1:len(text)-1])
		}
		if accepted := malformed == nil; accepted != tt.accepted || reports.String() != want.String() {
			t.Errorf("%s: accepted %t with reports %q, want accepted %t with reports %q", tt.body, accepted, reports.String(), tt.accepted, want.String())
		}
	}
}

func TestWireErrorSurvivesADifferentLocalNumbering(t *testing.T) {
	registerPair(t, "lib", 0)
	sent := NewError(FirstDeclared, pair{code: 7, note: "x"})
	wire := EncodeError(sent)
	if wire.Module != "lib" || wire.Name != "Pair!" || string(wire.Fields) != `{"code":7,"note":"x"}` {
		t.Fatalf("wire = %+v", wire)
	}
	data, _ := json.Marshal(wire)
	registerPair(t, "lib", 3)
	var received WireError
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatal(err)
	}
	got, ok := DecodeWireError(received)
	if !ok || got.typ != FirstDeclared+3 || got.Display("lib") != `Pair! { code: 7, note: "x" }` {
		t.Errorf("decoded %v as %q (type %d), want Pair! at the receiver's own id %d", ok, got.Display("lib"), got.typ, FirstDeclared+3)
	}
	if _, ok := DecodeWireError(WireError{Module: "other", Name: "Pair!"}); ok {
		t.Error("an error of another module decoded under this module's type")
	}
}

func TestJSONFormsFollowTheMapping(t *testing.T) {
	registerPair(t, "lib", 0)
	d, _ := ParseDecimal("12.50")
	date, _ := ParseDate("d2024-03-05")
	dur, _ := ParseDuration("1h30m")
	e := NewError(FirstDeclared, pair{code: 1, note: "n"})
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"bool", true, "true"},
		{"int8", int8(-3), "-3"},
		{"byte", uint8(200), "200"},
		{"float", 1.5, "1.5"},
		{"string", "s", `"s"`},
		{"decimal", d, `"12.50"`},
		{"date", date, `"2024-03-05"`},
		{"duration", dur, `"5400s"`},
		{"bitset", BitSet(255), `"0x00000000000000ff"`},
		{"bytes", []byte{1, 2}, "[1,2]"},
		{"some", Some(int64(4)), "4"},
		{"none", None[int64](), "null"},
		{"absent box", BoxNone[int64](), "null"},
		{"present box", BoxSome(int64(4)), "4"},
		{"struct", StructValue{Name: "P", Fields: []Field{{Name: "x", Value: int64(1)}, {Name: "y", Value: "s"}}}, `{"x":1,"y":"s"}`},
		{"array", ArrayValue{Elems: []any{int64(1), int64(2)}}, "[1,2]"},
		{"dict", DictValue{Keys: []any{"k"}, Vals: []any{int64(1)}}, `[{"key":"k","value":1}]`},
		{"error", e, `{"Pair!":{"code":1,"note":"n"}}`},
	}
	for _, tt := range tests {
		data, err := JSON(tt.v)
		if err != nil || string(data) != tt.want {
			t.Errorf("%s: JSON = %s (%v), want %s", tt.name, data, err, tt.want)
		}
	}
}

func TestDecodersRejectNullAndOutOfRangeAndUnknown(t *testing.T) {
	registerPair(t, "lib", 0)
	if _, err := Decode(Raw("200"), DecodeInt[int8]); err == nil {
		t.Error("200 decoded as an Int8")
	}
	if _, err := Decode(Raw("1.5"), DecodeInt[int64]); err == nil {
		t.Error("1.5 decoded as an Int")
	}
	if _, err := Decode(Raw("null"), DecodeString); err == nil {
		t.Error("null decoded as a String")
	}
	if v, err := Decode(Raw("null"), optionOf(DecodeString)); err != nil || v.IsSome() {
		t.Errorf("null as a nullable String = %+v, %v", v, err)
	}
	if _, err := Decode(Raw(`{"a": 1, "b": 2}`), membersOf("a")); err == nil {
		t.Error("an unknown property was accepted")
	}
	dict, err := Decode(Raw(`[{"key": "k", "value": 3}]`), dictOf(DecodeString, DecodeInt[int64]))
	if err != nil || dict["k"] != 3 {
		t.Errorf("dictionary = %v, %v", dict, err)
	}
	keyed, err := Decode(Raw(`[{"key": 1.5, "value": "v"}]`), func(in *Tokens) (map[DecimalKey]Keyed[Decimal, string], error) {
		return DecodeKeyed(in, KeyOfDecimal, DecodeDecimal, DecodeString)
	})
	if err != nil || len(keyed) != 1 {
		t.Errorf("keyed dictionary = %v, %v", keyed, err)
	}
	bytes, err := Decode(Raw("[1, 255]"), DecodeBytes)
	if err != nil || string(bytes) != "\x01\xff" {
		t.Errorf("bytes = %v, %v", bytes, err)
	}
	if _, err := Decode(Raw("[256]"), DecodeBytes); err == nil {
		t.Error("256 decoded as a Byte")
	}
	got, err := Decode(Raw(`{"Pair!": {"code": 2, "note": "m"}}`), DecodeError)
	if err != nil || got.Display("lib") != `Pair! { code: 2, note: "m" }` {
		t.Errorf("error value = %q, %v", got.Display("lib"), err)
	}
	if _, err := Decode(Raw(`{"Pair!": {}, "Other!": {}}`), DecodeError); err == nil {
		t.Error("a two-property object decoded as an error")
	}
	if _, err := Decode(Raw(`{"Pair!": {"code": 2, "note": "m", "extra": 1}}`), DecodeError); err == nil {
		t.Error("an unknown error field was accepted")
	}
	bits, err := Decode(Raw(`"0x00000000000000ff"`), DecodeBitSet)
	if err != nil || bits != 255 {
		t.Errorf("bitset = %v, %v", bits, err)
	}
	if _, err := Decode(Raw(`"soon"`), DecodeDuration); err == nil {
		t.Error("soon decoded as a Duration")
	}
	_ = time.Second
}

func TestProblemBodiesFollowTheStatusTable(t *testing.T) {
	registerPair(t, "lib", 0)
	tests := []struct {
		fault  Fault
		status int
	}{{Malformed, 400}, {OutOfRange, 422}, {DivisionByZero, 422}, {Unavailable, 503}, {Rejected, 500}, {Unverified, 500}}
	for _, tt := range tests {
		reply := problem(NewError(tt.fault.TypeID(), nil), "lib")
		if reply.Status != tt.status || reply.ContentType != problemType {
			t.Errorf("%s: status %d %s, want %d problem+json", tt.fault.Name(), reply.Status, reply.ContentType, tt.status)
		}
	}
	reply := problem(NewError(FirstDeclared, pair{code: 1, note: "n"}), "lib")
	want := `{"type":"urn:ritchie:error:Pair","title":"Pair!","status":422,"detail":"Pair! { code: 1, note: \"n\" }","ritchieFields":{"code":1,"note":"n"}}`
	if reply.Status != 422 || string(reply.Body) != want {
		t.Errorf("declared error body = %d %s, want 422 %s", reply.Status, reply.Body, want)
	}
}

func TestJSONFormsUnderV2Defaults(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"html", "<a&b>", `"<a&b>"`},
		{"line separator", "a\u2028b", "\"a\u2028b\""},
		{"empty array", ArrayValue{}, "[]"},
		{"empty bytes", []byte(nil), "[]"},
		{"empty dict", DictValue{}, "[]"},
		{"empty struct", StructValue{}, "{}"},
	}
	for _, tt := range tests {
		data, err := JSON(tt.v)
		if err != nil || string(data) != tt.want {
			t.Errorf("%s: JSON = %s (%v), want %s", tt.name, data, err, tt.want)
		}
	}
	for _, v := range []any{"\xff", math.NaN(), math.Inf(1), StructValue{Fields: []Field{{Name: "x", Value: int64(1)}, {Name: "x", Value: int64(2)}}}} {
		if data, err := JSON(v); err == nil {
			t.Errorf("JSON(%#v) = %s, want an error", v, data)
		}
	}
}

func TestDecodersFollowV2Defaults(t *testing.T) {
	registerPair(t, "lib", 0)
	rejected := []struct {
		name string
		read func() error
	}{
		{"duplicate property", func() error { _, err := Decode(Raw(`{"a":1,"a":2}`), membersOf("a")); return err }},
		{"duplicate escaped property", func() error { _, err := Decode(Raw(`{"a":1,"\u0061":2}`), membersOf("a")); return err }},
		{"duplicate entry key", func() error {
			_, err := Decode(Raw(`[{"key":"k","key":"j","value":1}]`), dictOf(DecodeString, DecodeInt[int64]))
			return err
		}},
		{"duplicate error name", func() error {
			_, err := Decode(Raw(`{"Pair!":{"code":1,"note":"a"},"Pair!":{"code":2,"note":"b"}}`), DecodeError)
			return err
		}},
		{"invalid UTF-8 string", func() error { _, err := Decode(Raw("\"\xff\""), DecodeString); return err }},
		{"lone surrogate escape", func() error { _, err := Decode(Raw(`"\ud800"`), DecodeString); return err }},
		{"invalid UTF-8 property", func() error { _, err := Decode(Raw("{\"\xff\":1}"), membersOf("\uFFFD")); return err }},
		{"entry name case", func() error {
			_, err := Decode(Raw(`[{"Key":"k","Value":1}]`), dictOf(DecodeString, DecodeInt[int64]))
			return err
		}},
		{"property name case", func() error { _, err := Decode(Raw(`{"A":1}`), membersOf("a")); return err }},
		{"text after the value", func() error { _, err := Decode(Raw(`{"a":1} {}`), membersOf("a")); return err }},
		{"text after a number", func() error { _, err := Decode(Raw(`1.5 2`), DecodeDecimal); return err }},
		{"null after null", func() error { _, err := Decode(Raw(`null null`), optionOf(DecodeString)); return err }},
		{"null array", func() error { _, err := Decode(Raw(`null`), arrayOf(DecodeString)); return err }},
		{"null dictionary", func() error { _, err := Decode(Raw(`null`), dictOf(DecodeString, DecodeString)); return err }},
		{"decimal string with space", func() error { _, err := Decode(Raw(`" 1.5"`), DecodeDecimal); return err }},
		{"decimal string not a number", func() error { _, err := Decode(Raw(`"1_000"`), DecodeDecimal); return err }},
		{"decimal bool", func() error { _, err := Decode(Raw(`true`), DecodeDecimal); return err }},
	}
	for _, tt := range rejected {
		if err := tt.read(); err == nil {
			t.Errorf("%s: accepted, want an error", tt.name)
		}
	}
	scalars := map[string]func(Raw) error{
		"Bool":   func(r Raw) error { _, err := Decode(r, DecodeBool); return err },
		"String": func(r Raw) error { _, err := Decode(r, DecodeString); return err },
		"Float":  func(r Raw) error { _, err := Decode(r, DecodeFloat); return err },
		"Byte":   func(r Raw) error { _, err := Decode(r, DecodeByte); return err },
		"Int8":   func(r Raw) error { _, err := Decode(r, DecodeInt[int8]); return err },
		"Int":    func(r Raw) error { _, err := Decode(r, DecodeInt[int64]); return err },
	}
	for name, inputs := range map[string][]string{
		"Bool":   {`null`, `1`, `"true"`, `true false`, ``},
		"String": {`null`, `1`, `["s"]`, `"s" "t"`},
		"Float":  {`null`, `"1.5"`, `1e400`, `-1e400`, `true`},
		"Byte":   {`null`, `256`, `-1`, `-0`, `1.0`, `1e2`, `"1"`},
		"Int8":   {`null`, `128`, `-129`, `1.5`, `1e2`, `"1"`},
		"Int":    {`null`, `9223372036854775808`, `1.0`, `{}`},
	} {
		for _, in := range inputs {
			if err := scalars[name](Raw(in)); err == nil {
				t.Errorf("%s accepted %s, want an error", name, in)
			}
		}
	}
	for name, in := range map[string]string{"Bool": ` false `, "String": `"s"`, "Float": `-0`, "Byte": `255`, "Int8": `-128`, "Int": `-0`} {
		if err := scalars[name](Raw(in)); err != nil {
			t.Errorf("%s rejected %s: %v", name, in, err)
		}
	}
	if s, err := Decode(Raw(`"<a&b>\u2028"`), DecodeString); err != nil || s != "<a&b>\u2028" {
		t.Errorf("string = %q, %v", s, err)
	}
	for _, text := range []string{`1.5`, `"1.5"`, ` 1.5 `} {
		d, err := Decode(Raw(text), DecodeDecimal)
		if err != nil || d.String() != "1.5" {
			t.Errorf("Decimal %s = %s, %v, want 1.5", text, d.String(), err)
		}
	}
	props, err := Decode(Raw(` { "a" : [1, 2] , "b": "x" } `), membersOf("a", "b"))
	if err != nil || props["a"] != "[1, 2]" || props["b"] != `"x"` {
		t.Errorf("properties = %q, %v", props, err)
	}
	empty, err := Decode(Raw(`[]`), arrayOf(DecodeString))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("empty array = %#v, %v, want an empty non-nil slice", empty, err)
	}
}

func TestScannerReleasesItsInput(t *testing.T) {
	var s scanner
	s.load(Raw(`{"a":[1,2]}`))
	if _, err := s.dec.ReadValue(); err != nil {
		t.Fatalf("reading the loaded value: %v", err)
	}
	s.clear()
	if c := s.src.Cap(); c != 0 {
		t.Errorf("after clear the source buffer holds %d bytes of capacity, want 0", c)
	}
	if off := s.dec.InputOffset(); off != 0 {
		t.Errorf("after clear the decoder is at offset %d, want 0", off)
	}
	if tok, err := s.dec.ReadToken(); err != io.EOF {
		t.Errorf("after clear the decoder read %v, %v, want io.EOF", tok, err)
	}
	s.load(Raw(`"next"`))
	if tok, err := s.dec.ReadToken(); err != nil || tok.String() != "next" {
		t.Errorf("reloaded decoder read %v, %v, want the string next", tok, err)
	}
}

func TestEncoderReleasesItsOutput(t *testing.T) {
	var e encoder
	e.enc.Reset(&e.out)
	if err := writeJSON(&e.enc, "secret"); err != nil {
		t.Fatal(err)
	}
	if !e.release() {
		t.Fatal("an encoder with a small output was not released to the pool")
	}
	spare := e.out.AvailableBuffer()
	if spare = spare[:cap(spare)]; bytes.ContainsFunc(spare, func(r rune) bool { return r != 0 }) {
		t.Errorf("after release the buffer holds %q, want only zero bytes", spare)
	}
	if err := writeJSON(&e.enc, "next"); err != nil || e.out.String() != "\"next\"\n" {
		t.Errorf("reused encoder wrote %q, %v, want the string next", e.out.String(), err)
	}
	e.out.Reset()
	e.enc.Reset(&e.out)
	if err := writeJSON(&e.enc, strings.Repeat("x", 2*encoderLimit)); err != nil {
		t.Fatal(err)
	}
	if e.release() {
		t.Error("an encoder whose buffer exceeds the pooled limit was released to the pool")
	}
}

func TestJSONAboveThePooledLimit(t *testing.T) {
	long := strings.Repeat("y", 3*encoderLimit)
	for range 3 {
		data, err := JSON(ArrayValue{Elems: []any{long, "z"}})
		if err != nil || string(data) != `["`+long+`","z"]` {
			t.Fatalf("JSON of a long array = %d bytes, %v, want %d bytes", len(data), err, len(long)+9)
		}
		if data, err := JSON("short"); err != nil || string(data) != `"short"` {
			t.Fatalf("JSON after a long value = %s, %v", data, err)
		}
	}
}
