package syntax_test

import (
	"strings"
	"testing"

	"ritchie.dev/internal/syntax"
	"ritchie.dev/internal/testsrc"
)

// parsed returns the tree of one file with the description of its diagnostics.
func parsed(name, text string) (*syntax.File, string) {
	trees, diags := testsrc.Parse([]testsrc.File{{Path: name, Text: text}})
	if len(diags) > 0 || len(trees) == 0 {
		return nil, testsrc.Describe(diags)
	}
	return trees[0], ""
}

func TestANameOfTheWrongClassInADeclaredPositionIsReportedAtTheNameWithItsRule(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"main() {\n    for T in [1, 2] { print(T) }\n}\n", `name.unexpected-class at case.ri:2:9 A variable name starts with a lowercase letter or "_". The single uppercase letter "T" is a type parameter.`},
		{"main() {\n    for a, Item in [1: 2] { print(a) }\n}\n", `name.unexpected-class at case.ri:2:12 A variable name starts with a lowercase letter or "_". "Item" is a name of another class.`},
		{"main() {\n    for if in [1, 2] { print(1) }\n}\n", `syntax.unexpected-token at case.ri:2:9 "if" is a keyword and cannot name a variable.`},
		{"main() {\n    Int in = 1\n}\n", `syntax.unexpected-token at case.ri:2:9 "in" is a keyword and cannot name a variable.`},
		{"main() {\n    Int L = 1\n}\n", `name.unexpected-class at case.ri:2:9 A variable name starts with a lowercase letter or "_". The single uppercase letter "L" is a type parameter.`},
		{"DIGITS = \"0123\"\n", `syntax.unexpected-token at case.ri:1:1 Add the type of the constant before its name, as in "String DIGITS".`},
		{"LIMITS = [1, 2]\n", `syntax.unexpected-token at case.ri:1:1 Add the type of the constant before its name.`},
		{"Int limit = 4\n", `name.unexpected-class at case.ri:1:5 A module-level value is a constant, whose name is all uppercase, as in "LIMIT".`},
		{"Int N = 4\n", `name.unexpected-class at case.ri:1:5 A module-level value is a constant, whose name is all uppercase and has two or more characters. "N" is a name of another class.`},
		{"Int if = 4\n", `syntax.unexpected-token at case.ri:1:5 "if" is a keyword and cannot name a constant.`},
	}
	for _, tt := range tests {
		if _, diags := parsed("case.ri", tt.text); !strings.Contains(diags, tt.want) {
			t.Errorf("%q: diagnostics %s, want %s", tt.text, diags, tt.want)
		}
	}
}

func TestAConditionLoopAndATypedDeclarationKeepTheirForms(t *testing.T) {
	const text = "Int LIMIT = 4\n\nPt { Int x }\n\nmain() {\n    Pt p = Pt { x: 1 }\n    p.x = LIMIT\n    Int n = 0\n    for n < LIMIT { n += 1 }\n    for i, x in [1, 2] { print(i + x) }\n    for LIMIT > n { n += 1 }\n}\n"
	if _, diags := parsed("case.ri", text); diags != "" {
		t.Errorf("diagnostics %s", diags)
	}
}
