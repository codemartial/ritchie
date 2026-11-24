package rlr

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestDictionaryLookupsDistinguishAbsenceFromStoredAbsence(t *testing.T) {
	m := map[string]int64{"a": 1}
	if !Has(m, "a") || Has(m, "b") || DictLength(m) != 1 || DictLength(map[string]int64{}) != 0 {
		t.Error("Has or DictLength misreport the entries")
	}
	if CheckKey(m, "a") != NoFault || CheckKey(m, "b") != MissingKey {
		t.Error("CheckKey does not raise MissingKey! for an absent key alone")
	}
	if v, present := Get(m, "a", Same[int64]).Get(); !present || v != 1 {
		t.Errorf("Get(a) = %v, %v", v, present)
	}
	if Get(m, "b", Same[int64]).IsSome() {
		t.Error("Get of an absent key is present")
	}
	nested := map[string][]int64{"a": {1}}
	got := Get(nested, "a", Clone[int64]).Unwrap()
	got[0] = 9
	if nested["a"][0] != 1 {
		t.Error("Get shares the stored value's storage")
	}
	flat := map[string]Option[int64]{"none": None[int64](), "some": Some[int64](4)}
	if GetFlat(flat, "none", Same[Option[int64]]).IsSome() || GetFlat(flat, "missing", Same[Option[int64]]).IsSome() {
		t.Error("GetFlat adds a nullable layer over a stored or missing absence")
	}
	if v := GetFlat(flat, "some", Same[Option[int64]]).Unwrap(); v != 4 {
		t.Errorf("GetFlat(some) = %d", v)
	}
	if !Has(flat, "none") {
		t.Error("a stored absence is not a member")
	}
}

func TestDictionaryEnumerationsAreIndependentAndAligned(t *testing.T) {
	m := map[string][]int64{"a": {1}, "b": {2}, "c": {3}}
	keys := Keys(m)
	values := Values(m, Clone[int64])
	if len(keys) != 3 || len(values) != 3 {
		t.Fatalf("keys %v values %v", keys, values)
	}
	values[0][0] = 99
	if m["a"][0] != 1 || m["b"][0] != 2 || m["c"][0] != 3 {
		t.Error("Values shares storage with the source")
	}
	ks, vs := Unzip(m, Clone[int64])
	if len(ks) != 3 || len(vs) != 3 {
		t.Fatalf("unzip %v %v", ks, vs)
	}
	for i, k := range ks {
		if m[k][0] != vs[i][0] {
			t.Errorf("unzip position %d pairs %s with %v", i, k, vs[i])
		}
	}
	vs[0][0] = 77
	if m[ks[0]][0] == 77 {
		t.Error("Unzip shares value storage with the source")
	}
	if Keys(map[string]int64{}) == nil || Values(map[string]int64{}, Same[int64]) == nil {
		t.Error("an empty enumeration is nil rather than an empty array")
	}
}

func TestRemoveAndCloneLeaveTheInputIndependent(t *testing.T) {
	m := map[string][]int64{"a": {1}, "b": {2}}
	less := Remove(m, "a", Clone[int64])
	if len(less) != 1 || len(m) != 2 || less["b"][0] != 2 {
		t.Errorf("Remove(a) = %v leaving %v", less, m)
	}
	less["b"][0] = 9
	if m["b"][0] != 2 {
		t.Error("Remove shares value storage with its input")
	}
	same := Remove(m, "zz", Clone[int64])
	if len(same) != 2 {
		t.Errorf("removing an absent key changed the contents: %v", same)
	}
	copied := CloneDict(m, Clone[int64])
	copied["a"][0] = 5
	copied["c"] = []int64{3}
	if m["a"][0] != 1 || len(m) != 2 {
		t.Error("CloneDict shares storage or entries with its input")
	}
	if CloneDict(map[string]int64{}, Same[int64]) == nil {
		t.Error("the clone of an empty dictionary is nil")
	}
}

func TestZipPairsPositionsAndReportsMismatch(t *testing.T) {
	m := must(Zip([]string{"a", "b", "a"}, []int64{1, 2, 3}, Same[int64]))
	if len(m) != 2 || m["a"] != 3 || m["b"] != 2 {
		t.Errorf("Zip = %v, want the last value under an equal key", m)
	}
	if err := second(Zip([]string{"a"}, []int64{}, Same[int64])); err != LengthMismatch {
		t.Errorf("Zip over unequal lengths raised %v", err)
	}
	if got := must(Zip([]string{}, []int64{}, Same[int64])); got == nil || len(got) != 0 {
		t.Errorf("Zip of empty inputs = %v", got)
	}
	vs := [][]int64{{1}}
	zipped := must(Zip([]string{"k"}, vs, Clone[int64]))
	zipped["k"][0] = 9
	if vs[0][0] != 1 {
		t.Error("Zip shares value storage with its input")
	}
}

func TestDictionaryDisplayEmbedsKeysAndValues(t *testing.T) {
	if got := Entries(map[string]int64{}, Plain[string], Plain[int64]).Display(""); got != "[:]" {
		t.Errorf("empty display = %q", got)
	}
	one := Entries(map[string]string{"k": "v"}, Plain[string], Plain[string]).Display("")
	if one != `["k": "v"]` {
		t.Errorf("one-entry display = %q", one)
	}
	form := Entries(map[int64]Option[int64]{1: None[int64](), 2: Some[int64](5)}, Plain[int64], Plain[Option[int64]])
	got := form.Display("")
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") || !strings.Contains(got, "1: none") || !strings.Contains(got, "2: 5") || strings.Count(got, ", ") != 1 {
		t.Errorf("two-entry display = %q", got)
	}
	nested := DictValue{Keys: []any{"a"}, Vals: []any{ArrayValue{Elems: []any{"x"}}}}
	if got := nested.Display(""); got != `["a": ["x"]]` {
		t.Errorf("nested display = %q", got)
	}
	if got := render(form, ""); got != form.Display("") {
		t.Error("a dictionary form does not render through its display")
	}
	keys := slices.Clone(form.Keys)
	if len(keys) != 2 {
		t.Errorf("Entries captured %d keys", len(keys))
	}
}

func TestDictionaryFaultsNameTheirCatalogErrors(t *testing.T) {
	if MissingKey.Name() != "MissingKey!" || LengthMismatch.Name() != "LengthMismatch!" {
		t.Errorf("faults name %q and %q", MissingKey.Name(), LengthMismatch.Name())
	}
	if Origin(MissingKey, 0, "").Display("") != "MissingKey!" {
		t.Error("an origin of MissingKey does not display as the catalog error")
	}
}

func TestUnzipAndEntriesPairEachKeyWithItsOwnValue(t *testing.T) {
	m := map[int64]string{}
	for i := range int64(64) {
		m[(i*37)%64] = string(rune('a' + i%26))
	}
	ks, vs := Unzip(m, Same[string])
	entries := Entries(m, Plain[int64], Plain[string])
	if len(ks) != len(m) || len(vs) != len(m) || len(entries.Keys) != len(m) {
		t.Fatalf("Unzip holds %d keys and %d values and Entries %d entries, want %d each", len(ks), len(vs), len(entries.Keys), len(m))
	}
	for i, k := range ks {
		if vs[i] != m[k] {
			t.Errorf("Unzip position %d pairs key %d with %q, want %q", i, k, vs[i], m[k])
		}
	}
	for i, k := range entries.Keys {
		if entries.Vals[i] != m[k.(int64)] {
			t.Errorf("Entries position %d pairs key %v with %v, want %q", i, k, entries.Vals[i], m[k.(int64)])
		}
	}
	keys := Keys(m)
	slices.Sort(keys)
	values := Values(m, Same[string])
	want := slices.Collect(maps.Values(m))
	slices.Sort(values)
	slices.Sort(want)
	if len(keys) != 64 || keys[0] != 0 || keys[63] != 63 || !slices.Equal(values, want) {
		t.Errorf("Keys and Values hold %v and %v, want the 64 keys and the values of m", keys, values)
	}
}
