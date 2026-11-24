// This file is the fresh-name rule: a rule supplies a hint, which is used as it is when
// no name declared or used in the routine is the hint, and otherwise with the smallest
// numeric suffix that makes it distinct from every such name.
package lower

import (
	"strconv"

	"ritchie.dev/internal/syntax"
)

// names collects every name declared or used in a routine, labels included.
func names(fn syntax.Node) map[string]bool {
	out := map[string]bool{}
	syntax.Inspect(fn, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Param:
			out[n.Name] = true
		case *syntax.VarTarget:
			out[n.Name] = true
		case *syntax.ConstDecl:
			out[n.Name] = true
		case *syntax.IterFor:
			out[n.Label] = true
			for _, v := range n.Vars {
				out[v] = true
			}
		case *syntax.For:
			out[n.Label] = true
		case *syntax.CondFor:
			out[n.Label] = true
		case *syntax.Break:
			out[n.Label] = true
		case *syntax.Continue:
			out[n.Label] = true
		case *syntax.Arm:
			out[n.Binding] = true
		case *syntax.Name:
			if n.Module == "" && n.Owner == nil {
				out[n.Name] = true
			}
		}
		return true
	})
	delete(out, "")
	return out
}

// fresh returns the hint, or the hint with the smallest numeric suffix, that no name of the routine uses, and reserves it.
func fresh(taken map[string]bool, hint string) string {
	name := hint
	for i := 1; taken[name]; i++ {
		name = hint + strconv.Itoa(i)
	}
	taken[name] = true
	return name
}
