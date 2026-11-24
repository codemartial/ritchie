package rlr

import (
	"math"
	"testing"
)

func TestGeneralSlicesAreIndependentAndChecked(t *testing.T) {
	xs := []int64{1, 2, 3}
	sub := must(Slice(xs, 0, 2))
	sub[0] = 9
	if xs[0] != 1 || len(sub) != 2 {
		t.Errorf("Slice(%v, 0, 2) = %v and changed the source to %v", []int64{1, 2, 3}, sub, xs)
	}
	if got := must(Slice(xs, 3, 3)); len(got) != 0 || got == nil {
		t.Errorf("an empty slice at the end = %v, want an empty non-nil array", got)
	}
	for _, bounds := range [][2]int64{{-1, 1}, {2, 1}, {0, 4}, {math.MinInt64, 0}, {0, math.MaxInt64}} {
		if err := second(Slice(xs, bounds[0], bounds[1])); err != OutOfRange {
			t.Errorf("Slice bounds %v raised %v, want OutOfRange!", bounds, err)
		}
	}
	nested := [][]int64{{1}, {2}}
	deep := must(SliceDeep(nested, 0, 2, Clone[int64]))
	deep[0][0] = 9
	if nested[0][0] != 1 {
		t.Error("SliceDeep shares element storage with its source")
	}
	if err := second(SliceDeep(nested, 1, 0, Clone[int64])); err != OutOfRange {
		t.Errorf("inverted SliceDeep raised %v", err)
	}
	if Length(xs) != 3 || Length[int64](nil) != 0 {
		t.Error("Length does not report the element count")
	}
}

func TestCloneDeepCopiesEveryLevel(t *testing.T) {
	src := [][][]uint8{{{1, 2}}, {{3}}}
	copied := CloneDeep(src, func(x [][]uint8) [][]uint8 { return CloneDeep(x, Clone[uint8]) })
	copied[0][0][0] = 9
	copied[1] = nil
	if src[0][0][0] != 1 || src[1] == nil {
		t.Errorf("CloneDeep shares storage: source became %v", src)
	}
	if got := CloneDeep([]int64{}, func(x int64) int64 { return x }); got == nil || len(got) != 0 {
		t.Errorf("CloneDeep of an empty array = %v, want an empty non-nil array", got)
	}
}

func TestArrayValueDisplaysEmbeddedElements(t *testing.T) {
	tests := []struct {
		name  string
		value ArrayValue
		want  string
	}{
		{"empty", Elements([]int64{}, Plain[int64]), "[]"},
		{"integers", Elements([]int64{1, -2}, Plain[int64]), "[1, -2]"},
		{"fixed bytes are numeric", Elements([]uint8{0, 255}, Plain[uint8]), "[0, 255]"},
		{"strings embed quoted", Elements([]string{"a", "b\n"}, Plain[string]), `["a", "b\n"]`},
		{"dynamic byte arrays keep their literal form", Elements([][]byte{{0}, []byte("A")}, Plain[[]byte]), `[b"\x00", b"A"]`},
		{"nested", Elements([][]int64{{1}, {}}, func(x []int64) any { return Elements(x, Plain[int64]) }), "[[1], []]"},
		{"structs", Elements([]StructValue{{Module: "geo", Name: "P", Fields: []Field{{"x", int64(1)}}}}, Plain[StructValue]), "[geo::P { x: 1 }]"},
		{"floats and bools", Elements([]any{0.5, true}, Plain[any]), "[0.5, true]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Display("app"); got != tt.want {
				t.Errorf("display = %s, want %s", got, tt.want)
			}
		})
	}
	local := Elements([]StructValue{{Module: "geo", Name: "P"}}, Plain[StructValue])
	if got := local.Display("geo"); got != "[P {}]" {
		t.Errorf("display under the declaring module = %s, want [P {}]", got)
	}
	if got := must(Format("", "x=", Elements([]int64{1}, Plain[int64]))); got != "x=[1]" {
		t.Errorf("formatted array = %q, want x=[1]", got)
	}
}

func TestCheckIndexJudgesOnlyTheLiveContainer(t *testing.T) {
	xs := []int64{1, 2, 3}
	for _, i := range []int64{0, 1, 2} {
		if err := CheckIndex(xs, i); err != NoFault {
			t.Errorf("CheckIndex(%v, %d) raised %v", xs, i, err)
		}
	}
	for _, i := range []int64{-1, 3, math.MinInt64, math.MaxInt64} {
		if err := CheckIndex(xs, i); err != OutOfRange {
			t.Errorf("CheckIndex(%v, %d) raised %v, want OutOfRange!", xs, i, err)
		}
	}
	if err := CheckIndex[int64](nil, 0); err != OutOfRange {
		t.Errorf("CheckIndex over an empty array raised %v, want OutOfRange!", err)
	}
}
