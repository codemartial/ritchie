package syntax

import "testing"

func TestEqualHoldsExactlyUpToAConsistentRenamingOfLocalNames(t *testing.T) {
	tests := []struct {
		fact  string
		left  string
		right string
		equal bool
	}{
		{"locals may be renamed",
			"main() {\n    Int total = 1\n    log(total)\n}\n", "main() {\n    Int t = 1\n    log(t)\n}\n", true},
		{"the renaming must be consistent",
			"main() {\n    Int a = 1\n    Int b = 2\n    log(a)\n}\n", "main() {\n    Int a = 1\n    Int b = 2\n    log(b)\n}\n", false},
		{"two locals cannot be merged into one",
			"main() {\n    Int a = 1\n    Int b = 2\n    log(a)\n}\n", "main() {\n    Int a = 1\n    Int a = 2\n    log(a)\n}\n", false},
		{"a parameter is compared by its text",
			"f(Int x) {\n    log(x)\n}\n", "f(Int y) {\n    log(y)\n}\n", false},
		{"a module-level name is compared by its text",
			"Int AA = 1\n", "Int BB = 1\n", false},
		{"a field is compared by its text",
			"main() {\n    log(p.x)\n}\n", "main() {\n    log(p.y)\n}\n", false},
		{"a loop label may be renamed",
			"main() {\n    outer: for {\n        break outer\n    }\n}\n", "main() {\n    l: for {\n        break l\n    }\n}\n", true},
		{"an arm binding may be renamed",
			"main() {\n    match f() {\n        case Int v => { log(v) }\n    }\n}\n", "main() {\n    match f() {\n        case Int w => { log(w) }\n    }\n}\n", true},
		{"a closure parameter may be renamed",
			"main() {\n    g(x => x + 1)\n}\n", "main() {\n    g(y => y + 1)\n}\n", true},
		{"a local is renamed only inside its scope",
			"main() {\n    {\n        Int x = 1\n    }\n    log(x)\n}\n", "main() {\n    {\n        Int y = 1\n    }\n    log(x)\n}\n", true},
		{"literals are compared by value",
			"main() {\n    log(0xFF)\n}\n", "main() {\n    log(255)\n}\n", true},
		{"layout is ignored",
			"main() { log(1); log(2) }\n", "main() {\n\n    log(1)\n    log(2)\n}\n", true},
		{"comments are ignored",
			"main() {\n    log(1) // one\n}\n", "main() {\n    log(1)\n}\n", true},
		{"a braced arm body equals the bare statement",
			"main() {\n    match x {\n        case 1 => log(1)\n    }\n}\n", "main() {\n    match x {\n        case 1 => { log(1) }\n    }\n}\n", true},
		{"a different statement is a difference",
			"main() {\n    log(1)\n}\n", "main() {\n    log(2)\n}\n", false},
	}
	for _, tt := range tests {
		left, _ := parseText("left", tt.left)
		right, _ := parseText("right", tt.right)
		if got := Equal(left, right); got != tt.equal {
			t.Errorf("%s: Equal = %v, want %v\n%s\n%s", tt.fact, got, tt.equal, renamed(left), renamed(right))
		}
	}
}

func TestDifferenceReportsTheFirstDifferingLine(t *testing.T) {
	left, _ := parseText("left", "main() {\n    Int a = 1\n    log(a)\n    log(2)\n}\n")
	right, _ := parseText("right", "main() {\n    Int b = 1\n    log(b)\n    log(3)\n}\n")
	line, l, r := Difference(left, right)
	if line != 4 || l != "    log(2)" || r != "    log(3)" {
		t.Errorf("Difference = %d %q %q, want 4 %q %q", line, l, r, "    log(2)", "    log(3)")
	}
	if line, _, _ := Difference(left, left); line != 0 {
		t.Errorf("Difference of a tree with itself = %d, want 0", line)
	}
}
