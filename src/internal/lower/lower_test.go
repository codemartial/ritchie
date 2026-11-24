package lower

import (
	"strings"
	"testing"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
	"ritchie.dev/internal/testsrc"
)

// lowered parses and checks and lowers a source set and returns the kernel files with the diagnostics of any stage and the internal error of a defect.
func lowered(files []testsrc.File) ([]*syntax.File, []diagnostics.Record, error) {
	return loweredUnder(files, check.Options{}, false)
}

// loweredUnder lowers a source set under the declarations of a fixture and the schedule of a build.
func loweredUnder(files []testsrc.File, opts check.Options, sequential bool) ([]*syntax.File, []diagnostics.Record, error) {
	trees, diags := testsrc.Parse(files)
	if len(diags) > 0 {
		return nil, diags, nil
	}
	p, diags, err := check.Check(trees, opts)
	if err != nil || len(diags) > 0 {
		return nil, diags, err
	}
	if diags := Limits(p); len(diags) > 0 {
		return nil, diags, nil
	}
	k, diags, err := Lower(p, sequential)
	if err != nil || len(diags) > 0 {
		return nil, diags, err
	}
	return k.Files, nil, nil
}

func TestAnIntegralDecimalConstantLowersToALiteralThatChecksAgainWithItsQuantum(t *testing.T) {
	src := []testsrc.File{{Path: "case.ri", Text: "Decimal FIVE = Decimal.fromInt(5)\nmain() {\n    Decimal d = FIVE\n    log(d)\n}\n"}}
	got, diags, err := lowered(src)
	if err != nil || len(diags) > 0 {
		t.Fatalf("lowering: %v %s", err, testsrc.Describe(diags))
	}
	text := syntax.Print(got[0])
	if !strings.Contains(text, "Decimal d = 5e0\n") {
		t.Errorf("the constant is not written in exponent form:\n%s", text)
	}
	if _, diags, err := lowered([]testsrc.File{{Path: "case.ri", Text: text}}); err != nil || len(diags) > 0 {
		t.Errorf("the printed kernel program does not check: %v %s", err, testsrc.Describe(diags))
	}
}

var readCases = []struct {
	name    string
	program string
	want    string
}{
	{"a field before an argument whose rewrite hoists statements", `Bag {
    Int[] items
}

size(Int[] xs, Int k) -> Int {
    return count(xs) + k
}

main() {
    Bag b = Bag { items: [1, 2, 3] }
    Bool wide = count(b.items) > 2
    print(size(b.items, wide ? 1 : 2))
}
`, `Bag {
    Int[] items
}

size(Int[] xs, Int k) -> Int {
    return count(xs) + k
}

main() {
    Bag b = Bag { items: [1, 2, 3] }
    Bool wide = count(b.items) > 2
    Int t
    match {
        case wide => { t = 1 }
        any => { t = 2 }
    }
    print(size(b.items, t))
}
`},
	{"a field written as an argument by name after a call of an earlier parameter", `Bag {
    Int[] items
}

size(Int[] xs, Int k) -> Int {
    return count(xs) + k
}

next(Int k) -> Int {
    return k + 1
}

main() {
    Bag b = Bag { items: [1, 2, 3] }
    print(size(k: next(2), xs: b.items))
}
`, `Bag {
    Int[] items
}

size(Int[] xs, Int k) -> Int {
    return count(xs) + k
}

next(Int k) -> Int {
    return k + 1
}

main() {
    Bag b = Bag { items: [1, 2, 3] }
    Int t = next(2)
    print(size(b.items, t))
}
`},
}

func TestAnOperandThatNoLaterSiblingChangesIsReadWhereItIsUsed(t *testing.T) {
	for _, c := range readCases {
		got, diags, err := lowered([]testsrc.File{{Path: "case.ri", Text: c.program}})
		if err != nil || len(diags) > 0 {
			t.Errorf("%s: lowering: %v %s", c.name, err, testsrc.Describe(diags))
			continue
		}
		if text := syntax.Print(got[0]); text != c.want {
			t.Errorf("%s: the kernel text is\n%s\nwant\n%s", c.name, text, c.want)
		}
	}
}

var constantCases = []struct {
	name    string
	program string
	want    string
}{
	{"a module constant whose value is a collection, read by one element", `Int[] SQUARES = [0, 1, 4, 9, 16, 25, 36, 49, 64, 81]

square(Int n) -> Int {
    return SQUARES[n]
}

main() {
    print(square(7))
}
`, `Int[] SQUARES = [0, 1, 4, 9, 16, 25, 36, 49, 64, 81]

square(Int n) -> Int {
    return SQUARES[n]
}

main() {
    print(square(7))
}
`},
	{"a constant call whose result is a collection, and the count of one", `main() {
    Int[] a = fill(0, 4000000)
    print(count(a))
    print(count(fill(0, 4000000)))
}
`, `main() {
    Int[] a = fill(0, 4000000)
    print(count(a))
    print(4000000)
}
`},
	{"a constant built from another, a scalar constant, and a local constant", `Int[] TABLE = [1, 2, 3]
Int[] DOUBLED = concat(TABLE[:], TABLE[:])
Int LIMIT = count(DOUBLED)

main() {
    print(LIMIT)
    print(TABLE[0])
    Int[] LOCAL = [5, 6]
    Int[] ys = append(LOCAL[:], LIMIT)
    print(ys)
}
`, `Int[] TABLE = [1, 2, 3]

Int[] DOUBLED = concat(TABLE[:], TABLE[:])

main() {
    print(6)
    print(1)
    Int[] LOCAL = [5, 6]
    Int[] ys = append(LOCAL[:], 6)
    print(ys)
}
`},
	{"a method that can change its receiver on an element of a constant", `Tile {
    Int w
}

Tile.grow() {
    self.w = self.w + 1
}

Tile[] TILES = [Tile { w: 1 }, Tile { w: 2 }]

main() {
    Int i = 1
    TILES[i].grow()
    print(TILES[i].w)
}
`, `Tile {
    Int w
}

Tile.grow() {
    self.w = self.w + 1
}

Tile[] TILES = [Tile { w: 1 }, Tile { w: 2 }]

main() {
    Int i = 1
    Tile t = TILES[i]
    t.grow()
    print(TILES[i].w)
}
`},
	{"a nullable constant whose value is a collection as the left operand of ??", `Int[]? MAYBE = [1, 2]

main() {
    Int[] xs = MAYBE ?? [0]
    print(xs)
}
`, `Int[]? MAYBE = [1, 2]

main() {
    Int[]? t = MAYBE
    Int[] xs
    match {
        case t != none => { xs = t }
        any => { xs = [0] }
    }
    print(xs)
}
`},
	{"a constant whose initializer needs a statement in front of it", `Int[]? MAYBE = [1, 2]
Decimal HALF = 0.5
Int[] PICKED = MAYBE ?? [0]

main() {
    Decimal[] RATES = fill(HALF * 2.0, 2)
    print(RATES)
    print(PICKED)
}
`, `Int[]? MAYBE = [1, 2]

Int[] PICKED = [1, 2]

main() {
    Decimal[] RATES = [1.00, 1.00]
    print(RATES)
    print(PICKED)
}
`},
	{"a constant array of references to functions, which each use builds", `Step = (Int) -> Int

double(Int x) -> Int {
    return x * 2
}

Step[] STEPS = [double]

main() {
    Int v = 1
    for step in STEPS {
        v = step(v)
    }
    print(v)
}
`, `double(Int x) -> Int {
    return x * 2
}

main() {
    Int v = 1
    for step in [double] {
        v = step(v)
    }
    print(v)
}
`},
}

func TestAConstantWhoseValueIsACollectionStaysADeclarationAndAConstantCallWithSuchAResultStaysACall(t *testing.T) {
	for _, c := range constantCases {
		got, diags, err := lowered([]testsrc.File{{Path: "case.ri", Text: c.program}})
		if err != nil || len(diags) > 0 {
			t.Errorf("%s: lowering: %v %s", c.name, err, testsrc.Describe(diags))
			continue
		}
		text := syntax.Print(got[0])
		if text != c.want {
			t.Errorf("%s: the kernel text is\n%s\nwant\n%s", c.name, text, c.want)
		}
		again, diags, err := lowered([]testsrc.File{{Path: "case.ri", Text: text}})
		if err != nil || len(diags) > 0 {
			t.Errorf("%s: lowering the kernel text: %v %s", c.name, err, testsrc.Describe(diags))
			continue
		}
		if text := syntax.Print(again[0]); text != c.want {
			t.Errorf("%s: the kernel text lowers to\n%s\nwant itself", c.name, text)
		}
	}
}

const gaugeService = `@Gauge {}

@Gauge.level(Int id) -> Int {
    return id
}

@Gauge.list(Int id) -> Int[] {
    return [id]
}

@Gauge.note(Int id) {
    log(id)
}

`

// futureCase is a program with remote reads, the service methods that its fixture declares mutating, the schedule of its build, and the kernel text of its main routine and of the routines after it.
type futureCase struct {
	name, program string
	mutating      []string
	sequential    bool
	want          string
}

var futureCases = []futureCase{
	{"a read is a send whose future a local holds, and a join stands before the output that the sends reach", gaugeService + `main() {
    Int a = @Gauge.level(1)
    Int b = @Gauge.level(2)
    print(a + b)
}
`, nil, false, `main() {
    future<Int> a = send @Gauge.level(1)
    future<Int> b = send @Gauge.level(2)
    join
    print(wait a + wait b)
}
`},
	{"a use between two reads waits before the second read is sent", gaugeService + `main() {
    Int a = @Gauge.level(1)
    Int c = a + 0
    Int b = @Gauge.level(2)
    print(c + b)
}
`, nil, false, `main() {
    future<Int> a = send @Gauge.level(1)
    Int c = wait a + 0
    future<Int> b = send @Gauge.level(2)
    join
    print(c + wait b)
}
`},
	{"a handler that supplies or returns known values is the arms of its send, and a handler that runs code waits for its read at the statement", gaugeService + `main() {
    Int misses = 0
    Int a = @Gauge.level(2) handle Unavailable! => 0
    Int b = @Gauge.level(1) handle any => { return }
    Int c = @Gauge.level(2) handle any => {
        misses += 1
        resume 0
    }
    @Gauge.note(3) handle any => {}
    print(a + b + c + misses)
}
`, nil, false, `main() {
    Int misses = 0
    future<Int> a = send @Gauge.level(2) {
        case Unavailable! => 0
    }
    future<Int> b = send @Gauge.level(1) {
        any! => return
    }
    Int c
    match wait send @Gauge.level(2) {
        case Int v => { c = v }
        any! => {
            misses = misses + 1
            c = 0
        }
    }
    send @Gauge.note(3) {
        any! => {}
    }
    join
    print(wait a + wait b + c + misses)
}
`},
	{"a store of an element, an append, a collection literal, and a loop over a collection of futures move, count uses no future, and a read whose type the store changes waits", gaugeService + `main() {
    Int[] xs = [@Gauge.level(1), 2]
    xs = append(xs, @Gauge.level(3))
    xs[0] = @Gauge.level(4)
    Int second = xs[1]
    Int n = count(xs)
    Int sum = 0
    for x in xs {
        sum += x
    }
    Int[] whole = @Gauge.list(6)
    Int? maybe = @Gauge.level(7)
    print(sum + n + second + count(whole))
    print(maybe)
    print(xs)
}
`, nil, false, `main() {
    future<Int>[] xs = [send @Gauge.level(1), 2]
    xs = append(xs, send @Gauge.level(3))
    xs[0] = send @Gauge.level(4)
    future<Int> second = xs[1]
    Int n = count(xs)
    Int sum = 0
    for x in xs {
        sum = sum + wait x
    }
    future<Int[]> whole = send @Gauge.list(6)
    Int? maybe = wait send @Gauge.level(7)
    join
    print(sum + n + wait second + count(wait whole))
    print(maybe)
    print(wait xs)
}
`},
	{"two reads among the operands of one operation are both sent before the operation waits for the first", gaugeService + `main() {
    Int v = @Gauge.level(1) + @Gauge.level(2)
    print(v)
}
`, nil, false, `main() {
    future<Int> t = send @Gauge.level(1)
    future<Int> t1 = send @Gauge.level(2)
    Int v = wait t + wait t1
    join
    print(v)
}
`},
	{"the values of a dictionary hold futures and its keys hold none", gaugeService + `main() {
    [Int: Int] d = [1: @Gauge.level(1)]
    Int key = @Gauge.level(2)
    d[key] = @Gauge.level(3)
    Int sum = 0
    for k, v in d {
        sum += k + v
    }
    print(sum + count(d) + d[1])
}
`, nil, false, `main() {
    [Int: future<Int>] d = [1: send @Gauge.level(1)]
    future<Int> key = send @Gauge.level(2)
    d[wait key] = send @Gauge.level(3)
    Int sum = 0
    for k, v in d {
        Int t = k + wait v
        sum = sum + t
    }
    join
    print(sum + count(d) + wait d[1])
}
`},
	{"a call of log is no effect, so the routine does not wait before it", gaugeService + `main() {
    Int a = @Gauge.level(1)
    log("sent")
    print(a)
}
`, nil, false, `main() {
    future<Int> a = send @Gauge.level(1)
    log("sent")
    join
    print(wait a)
}
`},
	{"a wait on the clock is an effect, so a join stands before it", gaugeService + `main() {
    Int a = @Gauge.level(1)
    r::Clock.sleep(10ms)
    print(a)
}
`, nil, false, `main() {
    future<Int> a = send @Gauge.level(1)
    join
    r::Clock.sleep(10ms)
    print(wait a)
}
`},
	{"a call of a reader is a send, and one whose result the next statement computes on is waited for at its statement", gaugeService + `reading(Int id) -> Int {
    return @Gauge.level(id)
}

main() {
    Int r = reading(1)
    Int s = reading(2)
    Int t = s + 1
    print(r + t + reading(3))
}
`, nil, false, `reading(Int id) -> Int {
    return wait send @Gauge.level(id)
}

main() {
    future<Int> r = send reading(1)
    Int s = wait send reading(2)
    Int t = s + 1
    join
    print(wait r + t + reading(3))
}
`},
	{"a local that a closure reads holds no future, and the body of a closure holds its own", gaugeService + `main() {
    Int base = @Gauge.level(1)
    (Int) -> Int add = (Int n) => {
        Int a = @Gauge.level(n)
        Int b = @Gauge.level(base)
        return a + b
    }
    print(add(2))
}
`, nil, false, `main() {
    Int base = wait send @Gauge.level(1)
    (Int) -> Int add = (Int n) => {
        future<Int> a = send @Gauge.level(n)
        future<Int> b = send @Gauge.level(base)
        join
        return wait a + wait b
    }
    join
    print(add(2))
}
`},
	{"a future of a nullable value is waited for at its presence test and at each narrowed use", `@Index {}

@Index.find(Int id) -> Int? {
    return id
}

main() {
    Int? a = @Index.find(1)
    if a != none {
        print(a + 1)
    }
}
`, nil, false, `main() {
    future<Int?> a = send @Index.find(1)
    match {
        case wait a != none => {
            join
            print(wait a + 1)
        }
    }
    join
}
`},
	{"a mutation keeps its call and a join stands before it", gaugeService + `main() {
    Int a = @Gauge.level(1)
    @Gauge.note(a)
    @Gauge.note(2)
}
`, []string{"Gauge.note"}, false, `main() {
    future<Int> a = send @Gauge.level(1)
    join
    @Gauge.note(wait a)
    @Gauge.note(2)
}
`},
	{"a send whose call the fixture of the build makes a mutation lowers to the call", gaugeService + `main() {
    future<Int> a = send @Gauge.level(1)
    send @Gauge.note(2)
    join
    print(wait a)
}
`, []string{"Gauge.note"}, false, `main() {
    future<Int> a = send @Gauge.level(1)
    join
    @Gauge.note(2)
    join
    print(wait a)
}
`},
	{"the sequential schedule waits for every read at its place", gaugeService + `main() {
    Int a = @Gauge.level(1)
    Int b = @Gauge.level(2)
    @Gauge.note(3)
    print(a + b)
}
`, nil, true, `main() {
    Int a = wait send @Gauge.level(1)
    Int b = wait send @Gauge.level(2)
    wait send @Gauge.note(3)
    join
    print(a + b)
}
`},
}

func TestEachRuleOfFuturesWritesItsKernelFormsAndTheKernelTextLowersToItself(t *testing.T) {
	for _, c := range futureCases {
		opts := check.Options{Mutating: c.mutating}
		got, diags, err := loweredUnder([]testsrc.File{{Path: "case.ri", Text: c.program}}, opts, c.sequential)
		if err != nil || len(diags) > 0 {
			t.Errorf("%s: lowering: %v %s", c.name, err, testsrc.Describe(diags))
			continue
		}
		text := syntax.Print(got[0])
		if !strings.HasSuffix(text, "\n\n"+c.want) {
			t.Errorf("%s: the kernel text is\n%s\nwant its routines from main to be\n%s", c.name, text, c.want)
			continue
		}
		again, diags, err := loweredUnder([]testsrc.File{{Path: "case.ri", Text: text}}, opts, c.sequential)
		if err != nil || len(diags) > 0 {
			t.Errorf("%s: lowering the kernel text: %v %s", c.name, err, testsrc.Describe(diags))
			continue
		}
		if twice := syntax.Print(again[0]); twice != text {
			t.Errorf("%s: the kernel text lowers to\n%s\nwant itself", c.name, twice)
		}
	}
}
