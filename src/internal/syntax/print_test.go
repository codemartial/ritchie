package syntax

import "testing"

func TestThePrinterWritesEachProgramInItsStatedCanonicalLayout(t *testing.T) {
	tests := []struct {
		fact  string
		input string
		want  string
	}{
		{"declarations are separated by one blank line and fields take one line each",
			"Box {}\nBox.open() {}\nPt { Int x, Int y = 1 }\n",
			"Box {}\n\nBox.open() {}\n\nPt {\n    Int x\n    Int y = 1\n}\n"},
		{"indentation is four spaces and braces open on the line of their construct",
			"main() {\n  if a { b() } else { c() }\n}\n",
			"main() {\n    if a {\n        b()\n    } else {\n        c()\n    }\n}\n"},
		{"an arm takes one line and a one-statement body is braced inline",
			"main() {\n    match x { case 1 => f(), any => { g()\n h() } }\n}\n",
			"main() {\n    match x {\n        case 1 => { f() }\n        any => {\n            g()\n            h()\n        }\n    }\n}\n"},
		{"a handler prints in brace form with a concise arm kept concise",
			"main() {\n    Int n = f() handle Err! => 0\n}\n",
			"main() {\n    Int n = f() handle {\n        Err! => 0\n    }\n}\n"},
		{"redundant parentheses are dropped and required ones are kept",
			"main() {\n    x = ((a + b)) * c - (d - e)\n    y = (a < b) < c\n    z = a == (not b)\n    w = -(-a)\n}\n",
			"main() {\n    x = (a + b) * c - (d - e)\n    y = (a < b) < c\n    z = a == (not b)\n    w = -(-a)\n}\n"},
		{"a coalescing chain keeps its right association",
			"main() {\n    x = a ?? (b ?? c)\n    y = (a ?? b) ?? c\n}\n",
			"main() {\n    x = a ?? b ?? c\n    y = (a ?? b) ?? c\n}\n"},
		{"same-type targets are written once",
			"main() {\n    Int a, Int b = 1, 2\n    Int c, String d = 3, \"d\"\n}\n",
			"main() {\n    Int a, b = 1, 2\n    Int c, String d = 3, \"d\"\n}\n"},
		{"literals print in canonical form",
			"main() {\n    log(0xFF, 1_0.5, `r`, 90m)\n}\n",
			"main() {\n    log(255, 10.5, \"r\", 1h30m)\n}\n"},
		{"a formatted string with a quoted slot prints in raw form",
			"main() {\n    log(f`{m[\"k\"]} {{x}}`)\n}\n",
			"main() {\n    log(f`{m[\"k\"]} {{x}}`)\n}\n"},
		{"an error without fields prints without braces and one with fields prints them",
			"Bad!\nmain() {\n    return Bad! {}\n}\n",
			"Bad!\n\nmain() {\n    return Bad!\n}\n"},
		{"a result list and a raise set print after the arrow",
			"f() -> (Int, Bool) | Err! {\n    return 1, true\n}\ng() -> none | Err! {}\n",
			"f() -> (Int, Bool) | Err! {\n    return 1, true\n}\n\ng() -> none | Err! {}\n"},
		{"a closure with one bare parameter prints without parentheses",
			"main() {\n    (Int) -> Int f = (x) => x\n    (Int, Int) -> Int g = (Int a, b) => { return a }\n}\n",
			"main() {\n    (Int) -> Int f = x => x\n    (Int, Int) -> Int g = (Int a, b) => {\n        return a\n    }\n}\n"},
		{"a chained assignment and a multi-target assignment keep their forms",
			"main() {\n    a = b = c\n    a, b = b, a\n    xs[i] += 1\n}\n",
			"main() {\n    a = b = c\n    a, b = b, a\n    xs[i] += 1\n}\n"},
		{"a labelled iterator loop and its break print with the label",
			"main() {\n    outer: for k, v in d {\n        break outer\n    }\n}\n",
			"main() {\n    outer: for k, v in d {\n        break outer\n    }\n}\n"},
		{"a query prints one clause per line",
			"%q(Int n) -> %Row { from $Ord o, %Row r where o.a == n and not o.b in r.c group by o.a select { s: sum(o.a), count(o.b) } having s > 1 sort o.a desc limit n }\n",
			"%q(Int n) -> %Row {\n    from $Ord o, %Row r\n    where o.a == n and not o.b in r.c\n    group by o.a\n    select {\n        s: sum(o.a)\n        count(o.b)\n    }\n    having s > 1\n    sort o.a desc\n    limit n\n}\n"},
		{"the kernel forms of a read in flight print without parentheses around their operands and a send prints its arms one to a line",
			"main() {\n    future<Int> a = send @Gauge.level(2) { case Low! => 0\n any! => return }\n    future<Int>[] xs = [a]\n    [String: future<Int>] d = [:]\n    send @Gauge.note(1) { any! => {} }\n    match wait send @Gauge.level(3) { case Int v => { xs = append(xs, send read(v)) } }\n    join\n    print((wait a) + wait xs[0] + count(wait d) + (wait xs)[0])\n    join(wait, send)\n}\n",
			"main() {\n    future<Int> a = send @Gauge.level(2) {\n        case Low! => 0\n        any! => return\n    }\n    future<Int>[] xs = [a]\n    [String: future<Int>] d = [:]\n    send @Gauge.note(1) {\n        any! => {}\n    }\n    match wait send @Gauge.level(3) {\n        case Int v => { xs = append(xs, send read(v)) }\n    }\n    join\n    print(wait a + wait xs[0] + count(wait d) + (wait xs)[0])\n    join(wait, send)\n}\n"},
		{"a type-member owner carries array suffixes",
			"main() {\n    b = Byte[].fromString(s)\n}\n",
			"main() {\n    b = Byte[].fromString(s)\n}\n"},
		{"an array length is an integer literal, a constant, or a qualified constant",
			"main() {\n    Int[3] a = x\n    Int[SIZE] b = x\n    Int[lib::SIZE][] c = x\n}\n",
			"main() {\n    Int[3] a = x\n    Int[SIZE] b = x\n    Int[lib::SIZE][] c = x\n}\n"},
		{"a dictionary type takes array suffixes",
			"main() {\n    [String: Int][] ds = []\n}\n",
			"main() {\n    [String: Int][] ds = []\n}\n"},
		{"a trailing comma in an array or dictionary literal is dropped",
			"main() {\n    a = [\n        1,\n        2,\n    ]\n    d = [\"k\": 1,]\n}\n",
			"main() {\n    a = [1, 2]\n    d = [\"k\": 1]\n}\n"},
		{"a bad statement prints as a placeholder",
			"main() {\n    x = )\n    log(1)\n}\n",
			"main() {\n    <invalid syntax>\n    log(1)\n}\n"},
	}
	for _, tt := range tests {
		f, diags := parseText("print", tt.input)
		if got := Print(f); got != tt.want {
			t.Errorf("%s:\n%s\nprints as\n%s\nwant\n%s\n%s", tt.fact, tt.input, got, tt.want, describe(diags))
		}
	}
}
