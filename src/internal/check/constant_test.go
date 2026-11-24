// This file tests the constant evaluator: the law of constant admission, by which it
// executes the operations that the built-in table derives as constant-capable and no
// other operation, the value of each form of constant expression, and the diagnostic
// of each fault of one.
package check

import (
	"testing"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/syntax"
	"ritchie.dev/internal/testsrc"
)

// checked parses and checks a source set and returns the checker with the diagnostics of every stage.
func checked(files []testsrc.File) (*checker, []diagnostics.Record) {
	trees, diags := testsrc.Parse(files)
	if len(diags) > 0 {
		return nil, diags
	}
	c := newChecker(trees, false)
	if diags := c.collect(); len(diags) > 0 {
		return nil, diags
	}
	c.run()
	return c, append(c.faults, c.diags...)
}

const constantOperands = `Int8 TINY = 1
Int16 SHORT = 1
Int32 WIDE = 1
Int WHOLE = 1
Byte OCTET = 1
Float REAL = 1.5
Decimal MONEY = 1.5
String TEXT = "12"
Date DAY = d2025-01-31
Time CLOCK = t10:00:00
DateTime STAMP = 2026-09-23T01:00:00Z
Duration SPAN = 90m
Bool FLAG = true
Int[] LIST = [3, 1, 2]
String[] NAMES = ["a", "b", "c"]
[String: Int] TABLE = ["12": 1]
Byte[32] SEED = fill(OCTET, 32)
`

// operation returns the first built-in operation in the body of the routine sample of a checked program, outermost first.
func operation(p *Program) syntax.Expr {
	var found syntax.Expr
	for _, d := range p.Files[0].Decls {
		if fn, ok := d.(*syntax.Func); ok && fn.Name == "sample" {
			syntax.Inspect(fn.Body, func(n syntax.Node) bool {
				if x, ok := n.(syntax.Expr); ok && found == nil && p.Operation(x) != nil {
					found = x
				}
				return found == nil
			})
		}
	}
	return found
}

func TestTheEvaluatorExecutesExactlyTheMappingsThatTheTableDerivesAsConstantCapable(t *testing.T) {
	samples := []string{
		`sample() { print(WHOLE) }`,
		`sample() { log(WHOLE) }`,
		`sample(Error! failure) { r::report(failure) }`,
		`sample() -> String { return f"{WHOLE}" }`,
		`sample() { for v in range(TINY) { log(v) } }`,
		`sample() { for v in range(SHORT) { log(v) } }`,
		`sample() { for v in range(WIDE) { log(v) } }`,
		`sample() { for v in range(WHOLE) { log(v) } }`,
		`sample() { for v in range(TINY, TINY) { log(v) } }`,
		`sample() { for v in range(SHORT, SHORT) { log(v) } }`,
		`sample() { for v in range(WIDE, WIDE) { log(v) } }`,
		`sample() { for v in range(WHOLE, WHOLE) { log(v) } }`,
		`sample() { for v in range(TINY, TINY, TINY) { log(v) } }`,
		`sample() { for v in range(SHORT, SHORT, SHORT) { log(v) } }`,
		`sample() { for v in range(WIDE, WIDE, WIDE) { log(v) } }`,
		`sample() { for v in range(WHOLE, WHOLE, WHOLE) { log(v) } }`,
		`sample() { for v in range(MONEY, MONEY) { log(v) } }`,
		`sample() { for v in range(MONEY, MONEY, MONEY) { log(v) } }`,
		`sample() { for v in range(DAY, DAY) { log(v) } }`,
		`sample() { for v in range(DAY, DAY, SPAN) { log(v) } }`,
		`sample() { for v in range(CLOCK, CLOCK) { log(v) } }`,
		`sample() { for v in range(CLOCK, CLOCK, SPAN) { log(v) } }`,
		`sample() { for v in range(STAMP, STAMP) { log(v) } }`,
		`sample() { for v in range(STAMP, STAMP, SPAN) { log(v) } }`,
		`sample() { for v in range(SPAN, SPAN) { log(v) } }`,
		`sample() { for v in range(SPAN, SPAN, SPAN) { log(v) } }`,
		`sample() -> Int { return count(LIST) }`,
		`sample() -> Int { return count(TABLE) }`,
		`sample() -> Int { return count(TEXT) }`,
		`sample() -> Int { return count(b"12") }`,
		`sample() -> Bool { return contains(TEXT, TEXT) }`,
		`sample() -> Bool { return contains(b"12", b"12") }`,
		`sample() -> Bool { return contains(LIST, WHOLE) }`,
		`sample() -> (Int, Bool) { return indexOf(TEXT, TEXT) }`,
		`sample() -> (Int, Bool) { return indexOf(b"12", b"12") }`,
		`sample() -> Int? { return indexOf(LIST, WHOLE) }`,
		`sample() -> Int? { return first(LIST) }`,
		`sample() -> Int? { return last(LIST) }`,
		`sample() -> Int[] { return append([3, 1, 2], WHOLE) }`,
		`sample() -> Int[] { return insert([3, 1, 2], WHOLE, WHOLE) }`,
		`sample() -> Int[] { return removeAt([3, 1, 2], WHOLE) }`,
		`sample() -> Int[] { return concat([3, 1, 2], [3, 1, 2]) }`,
		`sample() -> Int[] { return reverse(LIST) }`,
		`sample() -> Int[] { return sort(LIST) }`,
		`sample() -> Int[] { return fill(WHOLE, WHOLE) }`,
		`sample() -> Bool { return has(TABLE, TEXT) }`,
		`sample() -> Int? { return TABLE.get(TEXT) }`,
		`sample() -> String[] { return keys(TABLE) }`,
		`sample() -> Int[] { return values(TABLE) }`,
		`sample() -> [String: Int] { return remove(TABLE, TEXT) }`,
		`sample() -> [String: Int] { return zip(NAMES, LIST) }`,
		`sample() -> (String[], Int[]) { return unzip(TABLE) }`,
		`sample() -> Bool { return isEmpty(TEXT) }`,
		`sample() -> Bool { return isEmpty(b"12") }`,
		`sample() -> Bool { return startsWith(TEXT, TEXT) }`,
		`sample() -> Bool { return startsWith(b"12", b"12") }`,
		`sample() -> Bool { return endsWith(TEXT, TEXT) }`,
		`sample() -> Bool { return endsWith(b"12", b"12") }`,
		`sample() -> String { return toUpper(TEXT) }`,
		`sample() -> String { return toLower(TEXT) }`,
		`sample() -> String { return trim(TEXT) }`,
		`sample() -> String { return replace(TEXT, TEXT, TEXT) }`,
		`sample() -> String[] { return split(TEXT, TEXT) }`,
		`sample() -> String { return join(NAMES, TEXT) }`,
		`sample() -> Bool { return TEXT.isRuneBoundary(WHOLE) }`,
		`sample() -> Int8 { return abs(TINY) }`,
		`sample() -> Int16 { return abs(SHORT) }`,
		`sample() -> Int32 { return abs(WIDE) }`,
		`sample() -> Int { return abs(WHOLE) }`,
		`sample() -> Float { return abs(REAL) }`,
		`sample() -> Decimal { return abs(MONEY) }`,
		`sample() -> Int8 { return min(TINY, TINY) }`,
		`sample() -> Int16 { return min(SHORT, SHORT) }`,
		`sample() -> Int32 { return min(WIDE, WIDE) }`,
		`sample() -> Int { return min(WHOLE, WHOLE) }`,
		`sample() -> Byte { return min(OCTET, OCTET) }`,
		`sample() -> Float { return min(REAL, REAL) }`,
		`sample() -> Decimal { return min(MONEY, MONEY) }`,
		`sample() -> String { return min(TEXT, TEXT) }`,
		`sample() -> Byte[] { return min(b"12", b"12") }`,
		`sample() -> Date { return min(DAY, DAY) }`,
		`sample() -> Time { return min(CLOCK, CLOCK) }`,
		`sample() -> DateTime { return min(STAMP, STAMP) }`,
		`sample() -> Duration { return min(SPAN, SPAN) }`,
		`sample() -> Int8 { return max(TINY, TINY) }`,
		`sample() -> Int16 { return max(SHORT, SHORT) }`,
		`sample() -> Int32 { return max(WIDE, WIDE) }`,
		`sample() -> Int { return max(WHOLE, WHOLE) }`,
		`sample() -> Byte { return max(OCTET, OCTET) }`,
		`sample() -> Float { return max(REAL, REAL) }`,
		`sample() -> Decimal { return max(MONEY, MONEY) }`,
		`sample() -> String { return max(TEXT, TEXT) }`,
		`sample() -> Byte[] { return max(b"12", b"12") }`,
		`sample() -> Date { return max(DAY, DAY) }`,
		`sample() -> Time { return max(CLOCK, CLOCK) }`,
		`sample() -> DateTime { return max(STAMP, STAMP) }`,
		`sample() -> Duration { return max(SPAN, SPAN) }`,
		`sample() -> Int8 { return -TINY }`,
		`sample() -> Int16 { return -SHORT }`,
		`sample() -> Int32 { return -WIDE }`,
		`sample() -> Int { return -WHOLE }`,
		`sample() -> Float { return -REAL }`,
		`sample() -> Decimal { return -MONEY }`,
		`sample() -> Duration { return -SPAN }`,
		`sample() -> Bool { return not FLAG }`,
		`sample() -> Int8 { return TINY + TINY }`,
		`sample() -> Int16 { return SHORT + SHORT }`,
		`sample() -> Int32 { return WIDE + WIDE }`,
		`sample() -> Int { return WHOLE + WHOLE }`,
		`sample() -> Float { return REAL + REAL }`,
		`sample() -> Decimal { return MONEY + MONEY }`,
		`sample() -> Time { return CLOCK + SPAN }`,
		`sample() -> DateTime { return STAMP + SPAN }`,
		`sample() -> Duration { return SPAN + SPAN }`,
		`sample() -> Int8 { return TINY - TINY }`,
		`sample() -> Int16 { return SHORT - SHORT }`,
		`sample() -> Int32 { return WIDE - WIDE }`,
		`sample() -> Int { return WHOLE - WHOLE }`,
		`sample() -> Float { return REAL - REAL }`,
		`sample() -> Decimal { return MONEY - MONEY }`,
		`sample() -> Time { return CLOCK - SPAN }`,
		`sample() -> Duration { return CLOCK - CLOCK }`,
		`sample() -> DateTime { return STAMP - SPAN }`,
		`sample() -> Duration { return STAMP - STAMP }`,
		`sample() -> Duration { return SPAN - SPAN }`,
		`sample() -> Int8 { return TINY * TINY }`,
		`sample() -> Int16 { return SHORT * SHORT }`,
		`sample() -> Int32 { return WIDE * WIDE }`,
		`sample() -> Int { return WHOLE * WHOLE }`,
		`sample() -> Float { return REAL * REAL }`,
		`sample() -> Decimal { return MONEY * MONEY }`,
		`sample() -> Duration { return SPAN * WHOLE }`,
		`sample() -> Duration { return WHOLE * SPAN }`,
		`sample() -> Int8 { return TINY / TINY }`,
		`sample() -> Int16 { return SHORT / SHORT }`,
		`sample() -> Int32 { return WIDE / WIDE }`,
		`sample() -> Int { return WHOLE / WHOLE }`,
		`sample() -> Float { return REAL / REAL }`,
		`sample() -> Decimal { return MONEY / MONEY }`,
		`sample() -> Duration { return SPAN / WHOLE }`,
		`sample() -> Float { return SPAN / SPAN }`,
		`sample() -> Int8 { return TINY % TINY }`,
		`sample() -> Int16 { return SHORT % SHORT }`,
		`sample() -> Int32 { return WIDE % WIDE }`,
		`sample() -> Int { return WHOLE % WHOLE }`,
		`sample() -> Float { return REAL % REAL }`,
		`sample() -> Decimal { return MONEY % MONEY }`,
		`sample() -> Bool { return MONEY == MONEY }`,
		`sample() -> Bool { return DAY == DAY }`,
		`sample() -> Bool { return CLOCK == CLOCK }`,
		`sample() -> Bool { return STAMP == STAMP }`,
		`sample() -> Bool { return SPAN == SPAN }`,
		`sample() -> Bool { return b"12" == b"12" }`,
		`sample() -> Bool { return WHOLE == WHOLE }`,
		`sample() -> Bool { return MONEY != MONEY }`,
		`sample() -> Bool { return DAY != DAY }`,
		`sample() -> Bool { return CLOCK != CLOCK }`,
		`sample() -> Bool { return STAMP != STAMP }`,
		`sample() -> Bool { return SPAN != SPAN }`,
		`sample() -> Bool { return b"12" != b"12" }`,
		`sample() -> Bool { return WHOLE != WHOLE }`,
		`sample() -> Int { return LIST[WHOLE] }`,
		`sample() -> Int { return TABLE[TEXT] }`,
		`sample() { Int[] xs = [1, 2]; xs[0] = WHOLE; log(xs) }`,
		`sample() { [String: Int] d = ["a": 1]; d["a"] = WHOLE; log(d) }`,
		`sample() -> Int[] { return LIST[WHOLE:WHOLE] }`,
		`sample() -> String { return TEXT[WHOLE:WHOLE] }`,
		`sample() -> Int { return Int.fromByte(OCTET) }`,
		`sample() -> Int { return Int.fromFloat(REAL) }`,
		`sample() -> Int { return Int.fromString(TEXT) }`,
		`sample() -> Int8 { return Int8.fromInt(WHOLE) }`,
		`sample() -> Int16 { return Int16.fromInt(WHOLE) }`,
		`sample() -> Int32 { return Int32.fromInt(WHOLE) }`,
		`sample() -> Float { return Float.fromInt(WHOLE) }`,
		`sample() -> Float { return Float.fromDecimal(MONEY) }`,
		`sample() -> Byte { return Byte.fromInt(WHOLE) }`,
		`sample() -> String { return String.fromBytes(b"12") }`,
		`sample(UUId id) -> String { return String.fromUUId(id) }`,
		`sample() -> Byte[] { return Byte[].fromString(TEXT) }`,
		`sample() -> Decimal { return Decimal.fromInt(WHOLE) }`,
		`sample() -> Decimal { return Decimal.fromFloat(REAL) }`,
		`sample() -> UUId { return UUId.fromString(TEXT) }`,
		`sample() -> DateTime { return DateTime.fromUnix(WHOLE) }`,
		`sample() -> DateTime { return DateTime.fromUnix(WHOLE, WHOLE) }`,
		`sample() -> Date { return DAY.addDays(WHOLE) }`,
		`sample() -> Date { return DAY.addWeeks(WHOLE) }`,
		`sample() -> Date { return DAY.addMonths(WHOLE) }`,
		`sample() -> Date { return DAY.addYears(WHOLE) }`,
		`sample() -> Int { return DAY.daysUntil(DAY) }`,
		`sample() -> Duration { return CLOCK.until(CLOCK) }`,
		`sample() -> Duration { return CLOCK.since(CLOCK) }`,
		`sample() -> DateTime { return STAMP.withOffset(SPAN) }`,
		`sample() -> Duration { return STAMP.offset() }`,
		`sample() -> (Int, Int) { return STAMP.unix() }`,
		`sample() -> Bool { return SPAN.highRes() }`,
		`sample() -> Bool { return SPAN.lowRes() }`,
		`sample() -> DateTime { return r::Clock.time() }`,
		`sample() -> r::Clock { return r::Clock.withOffset(SPAN) }`,
		`sample() { r::Clock.sleep(SPAN) }`,
		`sample(r::Clock clock) -> DateTime { return clock.time() }`,
		`sample(r::Clock clock) { clock.sleep(SPAN) }`,
		`sample(r::Clock clock) -> Duration { return clock.offset() }`,
		`sample() -> r::Random { return r::Random.new() }`,
		`sample() -> r::Random { return r::Random.seed(SEED) }`,
		`sample(r::Random source) -> BitSet { return source.bits() }`,
		`sample(r::Random source) -> Float { return source.ratio() }`,
		`sample(r::Random source) -> Int { return source.range(WHOLE, WHOLE) }`,
		`sample() -> Bool { return TINY < TINY }`,
		`sample() -> Bool { return SHORT < SHORT }`,
		`sample() -> Bool { return WIDE < WIDE }`,
		`sample() -> Bool { return WHOLE < WHOLE }`,
		`sample() -> Bool { return OCTET < OCTET }`,
		`sample() -> Bool { return REAL < REAL }`,
		`sample() -> Bool { return TEXT < TEXT }`,
		`sample() -> Bool { return b"12" < b"12" }`,
		`sample() -> Bool { return MONEY < MONEY }`,
		`sample() -> Bool { return DAY < DAY }`,
		`sample() -> Bool { return CLOCK < CLOCK }`,
		`sample() -> Bool { return STAMP < STAMP }`,
		`sample() -> Bool { return SPAN < SPAN }`,
		`sample() -> Bool { return TINY > TINY }`,
		`sample() -> Bool { return SHORT > SHORT }`,
		`sample() -> Bool { return WIDE > WIDE }`,
		`sample() -> Bool { return WHOLE > WHOLE }`,
		`sample() -> Bool { return OCTET > OCTET }`,
		`sample() -> Bool { return REAL > REAL }`,
		`sample() -> Bool { return TEXT > TEXT }`,
		`sample() -> Bool { return b"12" > b"12" }`,
		`sample() -> Bool { return MONEY > MONEY }`,
		`sample() -> Bool { return DAY > DAY }`,
		`sample() -> Bool { return CLOCK > CLOCK }`,
		`sample() -> Bool { return STAMP > STAMP }`,
		`sample() -> Bool { return SPAN > SPAN }`,
		`sample() -> Bool { return TINY <= TINY }`,
		`sample() -> Bool { return SHORT <= SHORT }`,
		`sample() -> Bool { return WIDE <= WIDE }`,
		`sample() -> Bool { return WHOLE <= WHOLE }`,
		`sample() -> Bool { return OCTET <= OCTET }`,
		`sample() -> Bool { return REAL <= REAL }`,
		`sample() -> Bool { return TEXT <= TEXT }`,
		`sample() -> Bool { return b"12" <= b"12" }`,
		`sample() -> Bool { return MONEY <= MONEY }`,
		`sample() -> Bool { return DAY <= DAY }`,
		`sample() -> Bool { return CLOCK <= CLOCK }`,
		`sample() -> Bool { return STAMP <= STAMP }`,
		`sample() -> Bool { return SPAN <= SPAN }`,
		`sample() -> Bool { return TINY >= TINY }`,
		`sample() -> Bool { return SHORT >= SHORT }`,
		`sample() -> Bool { return WIDE >= WIDE }`,
		`sample() -> Bool { return WHOLE >= WHOLE }`,
		`sample() -> Bool { return OCTET >= OCTET }`,
		`sample() -> Bool { return REAL >= REAL }`,
		`sample() -> Bool { return TEXT >= TEXT }`,
		`sample() -> Bool { return b"12" >= b"12" }`,
		`sample() -> Bool { return MONEY >= MONEY }`,
		`sample() -> Bool { return DAY >= DAY }`,
		`sample() -> Bool { return CLOCK >= CLOCK }`,
		`sample() -> Bool { return STAMP >= STAMP }`,
		`sample() -> Bool { return SPAN >= SPAN }`,
	}
	covered := map[*builtin.Row]bool{}
	for _, sample := range samples {
		c, diags := checked(one(constantOperands + sample + "\n"))
		if len(diags) > 0 {
			t.Errorf("%s: diagnostics %s", sample, testsrc.Describe(diags))
			continue
		}
		x := operation(c.p)
		if x == nil {
			t.Errorf("%s: the routine holds no built-in operation", sample)
			continue
		}
		row := c.p.Operation(x).Row
		covered[row] = true
		e := evaluation{c: c}
		if _, executed := e.eval(x); executed != row.Constant || c.defect != nil {
			t.Errorf("%s selects %s: the evaluator executes it = %v with the defect %v, and the table derives constant = %v", sample, row.Signature, executed, c.defect, row.Constant)
		}
	}
	for _, row := range builtin.Rows {
		switch row.Form {
		case builtin.Function, builtin.Operator, builtin.TypeMethod, builtin.Method:
			if !covered[row] {
				t.Errorf("no sample selects %s", row.Signature)
			}
		}
	}
}

func TestEachConstantExpressionEvaluatesToItsStatedValue(t *testing.T) {
	const prelude = "Color { RED, GREEN }\nPoint { Int x }\nInt BASE = 21\nString TAG = \"x\"\nByte[] DATA = b\"ab\"\ntwice(Int n) -> Int { return n * 2 }\n"
	tests := []struct {
		typ   string
		expr  string
		value string
	}{
		{"Int", "1 + 2 * 3", "7"},
		{"Int", "7 / 2", "3"},
		{"Int", "-7 / 2", "-3"},
		{"Int", "-7 % 3", "-1"},
		{"Int", "0x10 + 0b11", "19"},
		{"Int", "BASE * 2", "42"},
		{"Int", "-(-5)", "5"},
		{"Int8", "-128", "-128"},
		{"Int8", "-100 - 28", "-128"},
		{"Int64", "9223372036854775807", "9223372036854775807"},
		{"Byte", "0xff", "255"},
		{"Float", "1.5 * 2.0", "3.0"},
		{"Float", "0.1 + 0.2", "0.30000000000000004"},
		{"Float", "1.0 / 4.0", "0.25"},
		{"Float", "7.5 % 2.0", "1.5"},
		{"Float", "1e300 * 10.0", "1e301"},
		{"Float", "-0.0", "0.0"},
		{"Float", "-1.5", "-1.5"},
		{"Decimal", "9.99", "9.99"},
		{"Decimal", "-1.10", "-1.10"},
		{"Bool", "1 < 2 < 3", "true"},
		{"Bool", "3 > 2 > 2", "false"},
		{"Bool", "false and true", "false"},
		{"Bool", "not false or false", "true"},
		{"Bool", `"abc" < "abd"`, "true"},
		{"Bool", "Color.RED != Color.GREEN", "true"},
		{"Bool", "BASE == 21", "true"},
		{"String", `"a\tb"`, `"a\tb"`},
		{"String", `f"n={BASE + 1}, ok={true}, s={TAG}"`, `"n=22, ok=true, s=x"`},
		{"Int", "abs(-5)", "5"},
		{"Int", "max(1, 9, 4)", "9"},
		{"Int", "min(3, -2)", "-2"},
		{"Float", "abs(-2.5)", "2.5"},
		{"Int", `count("héllo")`, "6"},
		{"Int", "count([1, 2, 3])", "3"},
		{"Int?", "none ?? 3", "3"},
		{"Int?", "none", "none"},
		{"Int[]", "[1 + 1, BASE]", "[2, 21]"},
		{"[String: Int]", `["a": 1 + 1]`, `["a": 2]`},
		{"Color", "Color.GREEN", "Color.GREEN"},
		{"Color", ".RED", "Color.RED"},
		{"(Int) -> Int", "twice", "twice"},
		{"Duration", "90m", "1h30m"},
		{"Decimal", "1.20 + 3.400", "4.600"},
		{"Decimal", "abs(-2.50)", "2.50"},
		{"Decimal", "min(3.0, 1.00, 2.000)", "1.00"},
		{"Decimal", "Decimal.fromInt(7)", "7e0"},
		{"Float", "Float.fromDecimal(1.5)", "1.5"},
		{"Duration", "1d + 129m", "1d2h9m"},
		{"Duration", "1ms / 3", "333us333ns"},
		{"Float", "1s / 2s", "0.5"},
		{"Time", "t23:30:00 + 2h", "t01:30:00"},
		{"Duration", "t01:00:00 - t23:00:00", "-22h"},
		{"Date", "d2025-01-31.addMonths(2)", "d2025-03-31"},
		{"Bool", "(t23:00:00).until(t01:00:00) == 2h", "true"},
		{"Bool", "1h == 60m", "true"},
		{"Bool", "d2025-01-01 < d2025-01-02", "true"},
		{"DateTime", "2026-09-23T01:00:00Z.withOffset(10h)", "2026-09-23T11:00:00+10:00"},
		{"Int16", "Int16.fromInt(7) + Int16.fromInt(2)", "9"},
		{"Int8", "Int8.fromInt(-7) / Int8.fromInt(2)", "-3"},
		{"Int32", "max(Int32.fromInt(3), 2)", "3"},
		{"Int8[]", "fill(Int8.fromInt(0), 2)", "[0, 0]"},
		{"Int", "count(fill(Int32.fromInt(0), 2))", "2"},
		{"Int", "count(fill(Byte.fromInt(0), 300))", "300"},
		{"Bool", "contains([Int8.fromInt(3), 4], 3)", "true"},
		{"String", `f"{Decimal.fromInt(26) * 0.25}"`, `"6.50"`},
		{"String", `f"{min(Decimal.fromInt(26) * 0.25, 5.00)}"`, `"5.00"`},
		{"String", `f"{max(2.5, Decimal.fromInt(1))}"`, `"2.5"`},
		{"Bool", "startsWith(\"abc\", \"ab\")", "true"},
		{"String", "toUpper(\"abc\")", "\"ABC\""},
		{"String", "\"héllo\"[0:1]", "\"h\""},
		{"Bool", "\"héllo\".isRuneBoundary(2)", "false"},
		{"Byte", "Byte.fromInt(200)", "200"},
		{"String", "String.fromBytes(b\"ok\")", "\"ok\""},
		{"Color[]", "reverse([Color.RED, Color.GREEN])", "[Color.GREEN, Color.RED]"},
		{"Point?", "first([Point { 1 }])", "Point { 1 }"},
		{"Int?", "first([BASE, 2][0:0])", "none"},
		{"Bool", "[BASE, 2] == [21, 2]", "true"},
		{"Bool", "contains([Color.RED], Color.GREEN)", "false"},
		{"Int8", ".fromInt(5)", "5"},
		{"String", `f"{Color.GREEN} {[Color.RED]} {first([Point { 1 }])} {DATA}"`, `"GREEN [RED] Point { x: 1 } [97, 98]"`},
	}
	for _, tt := range tests {
		text := prelude + tt.typ + " VALUE = " + tt.expr + "\n"
		c, diags := checked(one(text))
		if len(diags) > 0 || c.defect != nil {
			t.Errorf("%s %s: diagnostics %s and the defect %v", tt.typ, tt.expr, testsrc.Describe(diags), c.defect)
			continue
		}
		k := c.p.Value(c.p.Table.Lookup("", "VALUE").Decl)
		if k == nil {
			t.Errorf("%s %s: the constant has no value", tt.typ, tt.expr)
			continue
		}
		if got := syntax.Print(k.Literal()); got != tt.value {
			t.Errorf("%s %s: got %s, want %s", tt.typ, tt.expr, got, tt.value)
		}
	}
}

func TestEachFaultOfAConstantExpressionIsRejectedWithItsStatedCodes(t *testing.T) {
	rows := []row{
		{"a constant initializer that names no constant is an unknown name",
			[]testsrc.File{{Text: "Int VALUE = MISSING\n"}},
			[]string{"name.unknown-name"}},
		{"a constant initializer that calls a function is not a constant expression",
			[]testsrc.File{{Text: "Int VALUE = make()\nmake() -> Int { return 0 }\n"}},
			[]string{"constant.nonconstant-expression"}},
		{"a parameter default that reads another parameter is not a constant expression",
			[]testsrc.File{{Text: "use(Int first, Int second = first) {}\n"}},
			[]string{"constant.nonconstant-expression"}},
		{"a field default that uses self is not a constant expression",
			[]testsrc.File{{Text: "Tool { Int size = self.size }\n"}},
			[]string{"constant.nonconstant-expression"}},
		{"constants that depend on each other form a cycle",
			[]testsrc.File{{Text: "Int FIRST = SECOND\nInt SECOND = THIRD\nInt THIRD = FIRST\n"}},
			[]string{"constant.dependency-cycle"}},
		{"a negative constant array length is out of range",
			[]testsrc.File{{Text: "Int SIZE = 0 - 1\nBox { Int[SIZE] xs }\n"}},
			[]string{"constant.out-of-range"}},
		{"a constant array length of a type other than a signed integer is a type mismatch",
			[]testsrc.File{{Text: "String SIZE = \"x\"\nBox { Int[SIZE] xs }\n"}},
			[]string{"type.mismatch"}},
		{"an array length that names no constant is an unknown name",
			[]testsrc.File{{Text: "Box { Int[SIZE] xs }\n"}},
			[]string{"name.unknown-name"}},
		{"an integer constant beyond its type is out of range",
			[]testsrc.File{{Text: "Int8 VALUE = 128\n"}},
			[]string{"constant.out-of-range"}},
		{"a constant is private to its module",
			[]testsrc.File{{Module: "lib", Path: "lib.ri", Text: "Int LIMIT = 1\n"}, {Module: "app", Path: "app.ri", Text: "Int LOCAL = lib::LIMIT\n"}},
			[]string{"module.unpublished-member"}},
		{"a constant Decimal division by zero is rejected",
			[]testsrc.File{{Text: "Decimal BAD = 1.0 / 0.0\n"}},
			[]string{"constant.division-by-zero"}},
		{"a constant Duration sum beyond the domain is out of range",
			[]testsrc.File{{Text: "Duration BAD = 4611686018427387903ms + 1ms\n"}},
			[]string{"constant.out-of-range"}},
		{"a constant Date addition beyond the year domain is out of range",
			[]testsrc.File{{Text: "Date BAD = (d+999999-12-31).addDays(1)\n"}},
			[]string{"constant.out-of-range"}},
		{"a constant slice bound inside a UTF-8 encoding is an invalid boundary",
			[]testsrc.File{{Text: "String BAD = \"héllo\"[0:2]\n"}},
			[]string{"constant.invalid-string-boundary"}},
		{"a constant conversion from an invalid byte array is malformed",
			[]testsrc.File{{Text: "String BAD = String.fromBytes(b\"\\xff\")\n"}},
			[]string{"constant.malformed"}},
		{"a constant whose fixed array type writes the constant as its length depends on itself",
			[]testsrc.File{{Text: "Int[SIZE] SIZE = [1]\n"}},
			[]string{"constant.dependency-cycle"}},
		{"a constant that a field type writes as its length is evaluated before the declarations after it are resolved",
			[]testsrc.File{{Text: "Grid { Int[SIZE] cells }\nTile { Int w = WIDTH }\nInt WIDTH = 2\nInt SIZE = count([Tile {}, Tile {}])\n"}},
			nil},
		{"an integer sum of literals beyond Int is out of range",
			[]testsrc.File{{Text: "main() { log(9223372036854775807 + 1) }\n"}},
			[]string{"constant.out-of-range"}},
		{"an integer literal beyond the type of a local constant is reported once",
			[]testsrc.File{{Text: "main() {\n    Int8 VALUE = 128\n    log(VALUE)\n}\n"}},
			[]string{"constant.out-of-range"}},
		{"a use of a local constant whose evaluation failed is not reported again",
			[]testsrc.File{{Text: "main() {\n    Int VALUE = 1 / 0\n    Int next = VALUE + 1\n    log(next)\n}\n"}},
			[]string{"constant.division-by-zero"}},
		{"a constant initializer that calls an operation with a result in an unspecified order is not a constant expression",
			[]testsrc.File{{Text: "String[] NAMES = keys([\"a\": 1])\n"}},
			[]string{"constant.nonconstant-expression"}},
		{"a formatted string with a dictionary in a slot is not a constant expression",
			[]testsrc.File{{Text: "[String: Int] AGES = [\"ada\": 36]\nString SHOWN = f\"{AGES}\"\n"}},
			[]string{"constant.nonconstant-expression"}},
		{"a constant initializer that calls an undeclared function is an unknown function",
			[]testsrc.File{{Text: "Int VALUE = missing()\n"}},
			[]string{"call.unknown-function"}},
	}
	for i := range rows {
		for j := range rows[i].files {
			if rows[i].files[j].Path == "" {
				rows[i].files[j].Path = "case.ri"
			}
		}
	}
	runTable(t, rows, sourceCodes)
}
