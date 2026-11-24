package rlr

import (
	"slices"
	"testing"
)

func TestKeyedDictionaryRetainsFirstKey(t *testing.T) {
	m := DictOf(KeyOfDecimal, []Decimal{one("1.0"), one("1.00"), one("2")}, []string{"a", "b", "c"})
	if got := DictLength(m); got != 2 {
		t.Fatalf("DictLength = %d, want 2", got)
	}
	keys := KeysKeyed(m)
	slices.SortFunc(keys, func(a, b Decimal) int { return CompareDecimal(a, b) })
	if len(keys) != 2 || keys[0] != one("1.0") || keys[1] != one("2") {
		t.Errorf("KeysKeyed = %v, want [1.0 2]", keys)
	}
	if v, found := LookupKeyed(m, KeyOfDecimal(one("1.000"))); !found || v != "b" {
		t.Errorf("LookupKeyed(1.000) = %q, %v, want b, true", v, found)
	}
	if !Has(m, KeyOfDecimal(one("0.1E+1"))) || Has(m, KeyOfDecimal(one("3"))) {
		t.Error("Has does not follow numerical identity")
	}
	if CheckKey(m, KeyOfDecimal(one("3"))) != MissingKey || CheckKey(m, KeyOfDecimal(one("2.000"))) != NoFault {
		t.Error("CheckKey does not follow numerical identity")
	}
	StoreKeyed(m, KeyOfDecimal(one("1.0000")), one("1.0000"), "d")
	if got := GetKeyed(m, KeyOfDecimal(one("1")), Same[string]); !got.ok || got.value != "d" {
		t.Errorf("GetKeyed after store = %v, want d", got)
	}
	if k := KeysKeyed(RemoveKeyed(m, KeyOfDecimal(one("2.0")), Same[string])); len(k) != 1 || k[0] != one("1.0") {
		t.Errorf("RemoveKeyed keys = %v, want the retained 1.0", k)
	}
	ks, vs := UnzipKeyed(m, Same[string])
	if len(ks) != 2 || len(vs) != 2 {
		t.Errorf("UnzipKeyed = %v, %v", ks, vs)
	}
	zipped, fault := ZipKeyed([]Decimal{one("5.0"), one("5.00")}, []string{"x", "y"}, KeyOfDecimal, Same[string])
	if fault != NoFault || len(zipped) != 1 || zipped[KeyOfDecimal(one("5"))] != (Keyed[Decimal, string]{Key: one("5.0"), Val: "y"}) {
		t.Errorf("ZipKeyed = %v, %v", zipped, fault)
	}
	if _, fault := ZipKeyed([]Decimal{one("5.0")}, []string{}, KeyOfDecimal, Same[string]); fault != LengthMismatch {
		t.Errorf("ZipKeyed over unequal lengths = %v, want LengthMismatch", fault)
	}
	c := CloneKeyed(m, Same[string])
	StoreKeyed(c, KeyOfDecimal(one("2")), one("2"), "z")
	if v, _ := LookupKeyed(m, KeyOfDecimal(one("2"))); v != "z" {
		if v != "c" {
			t.Errorf("clone shares storage: original holds %q", v)
		}
	} else {
		t.Error("CloneKeyed shares storage with its source")
	}
	pairs := PairsKeyed(m)
	if len(pairs) != 2 {
		t.Errorf("PairsKeyed = %v", pairs)
	}
	display := EntriesKeyed(m, Plain[Decimal], Plain[string])
	if len(display.Keys) != 2 || len(display.Vals) != 2 {
		t.Errorf("EntriesKeyed = %v", display)
	}
	flat := DictOf(KeyOfDecimal, []Decimal{one("1")}, []Option[int64]{None[int64]()})
	if got := GetFlatKeyed(flat, KeyOfDecimal(one("1.0")), Same[Option[int64]]); got.ok {
		t.Errorf("GetFlatKeyed over a stored absence = %v, want absent", got)
	}
	if got := GetFlatKeyed(flat, KeyOfDecimal(one("7")), Same[Option[int64]]); got.ok {
		t.Errorf("GetFlatKeyed over a missing key = %v, want absent", got)
	}
}
