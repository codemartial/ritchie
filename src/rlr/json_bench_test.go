package rlr

import (
	"strconv"
	"strings"
	"testing"
)

// benchReply is a service result with scalars of each JSON form, an array of structs, and a dictionary.
func benchReply(b *testing.B) any {
	b.Helper()
	price, _ := ParseDecimal("12.50")
	date, _ := ParseDate("d2024-03-05")
	items := make([]any, 32)
	for i := range items {
		items[i] = StructValue{Name: "Item", Fields: []Field{
			{Name: "id", Value: int64(i)},
			{Name: "label", Value: "item <" + strconv.Itoa(i) + "> & more"},
			{Name: "weight", Value: float64(i) * 1.25},
			{Name: "note", Value: Some("present")},
		}}
	}
	dict := DictValue{}
	for i := range 16 {
		dict.Keys = append(dict.Keys, "k"+strconv.Itoa(i))
		dict.Vals = append(dict.Vals, int64(i))
	}
	return StructValue{Name: "Order", Fields: []Field{
		{Name: "customer", Value: "Ada Lovelace"},
		{Name: "price", Value: price},
		{Name: "placed", Value: date},
		{Name: "items", Value: ArrayValue{Elems: items}},
		{Name: "totals", Value: dict},
		{Name: "flags", Value: []byte{1, 2, 3, 4}},
	}}
}

// benchRequest is a request body with the same content as benchReply.
func benchRequest() Raw {
	var items, entries []string
	for i := range 32 {
		n := strconv.Itoa(i)
		items = append(items, `{"id":`+n+`,"label":"item `+n+`","weight":`+strconv.FormatFloat(float64(i)*1.25, 'g', -1, 64)+`,"note":"present"}`)
	}
	for i := range 16 {
		n := strconv.Itoa(i)
		entries = append(entries, `{"key":"k`+n+`","value":`+n+`}`)
	}
	return Raw(`{"customer":"Ada Lovelace","price":12.50,"placed":"2024-03-05","items":[` + strings.Join(items, ",") +
		`],"totals":[` + strings.Join(entries, ",") + `],"flags":[1,2,3,4]}`)
}

type benchItem struct {
	id     int64
	label  string
	weight float64
	note   Option[string]
}

type benchOrder struct {
	customer string
	price    Decimal
	placed   Date
	items    []benchItem
	totals   map[string]int64
	flags    []byte
}

// decodeBenchItem mirrors the struct decoder that the backend generates.
func decodeBenchItem(in *Tokens) (v benchItem, err error) {
	err = DecodeObject(in, []string{"id", "label", "weight", "note"}, nil, func(i int) (err error) {
		switch i {
		case 0:
			v.id, err = DecodeInt[int64](in)
		case 1:
			v.label, err = DecodeString(in)
		case 2:
			v.weight, err = DecodeFloat(in)
		case 3:
			v.note, err = func(in *Tokens) (Option[string], error) {
				return DecodeOption(in, DecodeString)
			}(in)
		}
		return err
	})
	return v, err
}

// decodeBenchOrder mirrors the struct decoder that the backend generates for an order with nested values.
func decodeBenchOrder(in *Tokens) (v benchOrder, err error) {
	err = DecodeObject(in, []string{"customer", "price", "placed", "items", "totals", "flags"}, nil, func(i int) (err error) {
		switch i {
		case 0:
			v.customer, err = DecodeString(in)
		case 1:
			v.price, err = DecodeDecimal(in)
		case 2:
			v.placed, err = DecodeDate(in)
		case 3:
			v.items, err = func(in *Tokens) ([]benchItem, error) {
				return DecodeArray(in, decodeBenchItem)
			}(in)
		case 4:
			v.totals, err = func(in *Tokens) (map[string]int64, error) {
				return DecodeDict(in, DecodeString, DecodeInt[int64])
			}(in)
		case 5:
			v.flags, err = DecodeBytes(in)
		}
		return err
	})
	return v, err
}

func BenchmarkJSON(b *testing.B) {
	reply := benchReply(b)
	data, err := JSON(reply)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := JSON(reply); err != nil {
			b.Fatal(err)
		}
	}
}

// benchDecode times the decoding of body through d, the entry that a request body takes.
func benchDecode[T any](b *testing.B, body Raw, d Decoder[T]) {
	b.Helper()
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Decode(body, d); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	b.Run("order", func(b *testing.B) { benchDecode(b, benchRequest(), decodeBenchOrder) })
	b.Run("chain of 1000 nodes", func(b *testing.B) { benchDecode(b, chainBody(1000), decodeChainNode) })
	b.Run("chain of 10000 nodes", func(b *testing.B) { benchDecode(b, chainBody(10000), decodeChainNode) })
}
