package builtin

import (
	"reflect"
	"testing"

	"ritchie.dev/rlr"
)

// ints returns an array of Int constants.
func ints(xs ...int64) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

var variantCases = map[string][]struct {
	args  []any
	want  any
	fault rlr.Fault
}{
	"append": {
		{[]any{ints(1, 2, 3), int64(4)}, ints(1, 2, 3, 4), rlr.NoFault},
		{[]any{ints(), int64(1)}, ints(1), rlr.NoFault},
	},
	"insert": {
		{[]any{ints(1, 2, 3), int64(0), int64(9)}, ints(9, 1, 2, 3), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(2), int64(9)}, ints(1, 2, 9, 3), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(3), int64(9)}, ints(1, 2, 3, 9), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(4), int64(9)}, []any(nil), rlr.OutOfRange},
		{[]any{ints(1, 2, 3), int64(-1), int64(9)}, []any(nil), rlr.OutOfRange},
	},
	"removeAt": {
		{[]any{ints(1, 2, 3), int64(0)}, ints(2, 3), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(1)}, ints(1, 3), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(2)}, ints(1, 2), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(3)}, []any(nil), rlr.OutOfRange},
		{[]any{ints(), int64(0)}, []any(nil), rlr.OutOfRange},
	},
	"concat": {
		{[]any{ints(1, 2), ints(3)}, ints(1, 2, 3), rlr.NoFault},
		{[]any{ints(), ints(3)}, ints(3), rlr.NoFault},
		{[]any{ints(), ints()}, ints(), rlr.NoFault},
	},
	"[:]": {
		{[]any{ints(1, 2, 3), int64(1), int64(3)}, ints(2, 3), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(0), int64(3)}, ints(1, 2, 3), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(3), int64(3)}, ints(), rlr.NoFault},
		{[]any{ints(1, 2, 3), int64(2), int64(1)}, []any(nil), rlr.OutOfRange},
		{[]any{ints(1, 2, 3), int64(0), int64(4)}, []any(nil), rlr.OutOfRange},
	},
	"sort": {
		{[]any{ints(3, 1, 2, 1)}, ints(1, 1, 2, 3), rlr.NoFault},
		{[]any{[]any{"b", "a", "c", "a"}}, []any{"a", "a", "b", "c"}, rlr.NoFault},
		{[]any{ints()}, ints(), rlr.NoFault},
	},
}

func TestEachVariantOfARowGivesTheResultsOfItsBaseFunction(t *testing.T) {
	for _, r := range Rows {
		for _, v := range []struct {
			variant Variant
			impl    any
		}{{BlockCopy, r.Block}, {UnstableSort, r.Unstable}} {
			if v.impl == nil {
				continue
			}
			cases := variantCases[r.Name]
			if len(cases) == 0 {
				t.Errorf("%s has a variant and no case", r.Signature)
			}
			_, extras := r.Function(v.variant)
			for _, c := range cases {
				base, baseFault, _ := r.Fold(c.args)
				if baseFault != c.fault || len(base) != 1 || !reflect.DeepEqual(base[0], c.want) {
					t.Errorf("the base function of %s over %v = %v with fault %v, want %v with fault %v", r.Signature, c.args, base, baseFault, c.want, c.fault)
				}
				got, fault, _ := apply(v.impl, extras, c.args)
				if fault != c.fault || len(got) != 1 || !reflect.DeepEqual(got[0], c.want) {
					t.Errorf("the variant of %s over %v = %v with fault %v, want %v with fault %v", r.Signature, c.args, got, fault, c.want, c.fault)
				}
			}
		}
	}
}
