// This file compares trees up to a consistent renaming of local names.
package syntax

import "strings"

// renamed prints a tree with locals, labels, arm bindings, and closure parameters replaced in order of first appearance.
func renamed(n Node) string {
	p := &printer{rename: true}
	p.reset()
	n.print(p)
	return p.b.String()
}

// Equal reports whether two trees are the same program up to a consistent renaming of local names.
func Equal(a, b Node) bool { return renamed(a) == renamed(b) }

// Difference reports the first differing line of the renamed prints, counted from one, or zero.
func Difference(a, b Node) (line int, left, right string) {
	as := strings.Split(renamed(a), "\n")
	bs := strings.Split(renamed(b), "\n")
	for i := 0; i < max(len(as), len(bs)); i++ {
		if i < len(as) {
			left = as[i]
		} else {
			left = ""
		}
		if i < len(bs) {
			right = bs[i]
		} else {
			right = ""
		}
		if left != right || i >= len(as) || i >= len(bs) {
			return i + 1, left, right
		}
	}
	return 0, "", ""
}
