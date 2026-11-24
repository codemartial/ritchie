package check

import (
	"slices"
	"strings"
	"testing"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/testsrc"
)

// row is one program of a diagnostics table with the sorted codes it is rejected with.
type row struct {
	fact  string
	files []testsrc.File
	codes []string
}

// one is a source set of one file.
func one(text string) []testsrc.File { return []testsrc.File{{Path: "case.ri", Text: text}} }

// runTable checks each row's codes against the codes that run reports for its program.
func runTable(t *testing.T, rows []row, run func([]testsrc.File) []string) {
	t.Helper()
	for _, tt := range rows {
		codes := run(tt.files)
		slices.Sort(codes)
		if !slices.Equal(codes, tt.codes) {
			t.Errorf("%s: got %v, want %v", tt.fact, codes, tt.codes)
		}
	}
}

// sourceCodes returns the codes the source check reports for a program.
func sourceCodes(files []testsrc.File) []string {
	var codes []string
	for _, d := range check(files) {
		codes = append(codes, d.Reason)
	}
	return codes
}

func TestEachSourceFaultOfNameTypeOrArityIsRejectedWithItsStatedCodes(t *testing.T) {
	rows := []row{
		{"a call of an undeclared function is an unknown function", one("caller() { missing() }\n"), []string{"call.unknown-function"}},
		{"a type-level method called through a value is a receiver kind fault", one("Tool {}\nTool.typeOnly() {}\nTool.valueCall() { self.typeOnly() }\n"), []string{"call.receiver-kind"}},
		{"an instance method called through its type is a receiver kind fault", one("Tool {}\nTool.instance() { self.instance() }\ncaller() { Tool.instance() }\n"), []string{"call.receiver-kind"}},
		{"a private method of another module is unpublished", []testsrc.File{{Module: "lib", Path: "lib/types.ri", Text: "Box {}\nBox.open() {}\nBox._hidden() {}\n"}, {Module: "app", Path: "app/use.ri", Text: "use() {\n    lib::Box._hidden()\n}\n"}}, []string{"module.unpublished-member"}},
		{"a positional value not assignable to its parameter is a type mismatch", one("need(Int one) {}\ncaller() { need(1 == 2) }\n"), []string{"type.mismatch"}},
		{"a value wider than the elements of the array argument of contains is a type mismatch", one("main() { Int8[] xs = [1]; Int v = 2; log(contains(xs, v)) }\n"), []string{"type.mismatch"}},
		{"a value wider than the elements of the array argument of indexOf is a type mismatch", one("main() { Int8[] xs = [1]; Int v = 2; log(indexOf(xs, v)) }\n"), []string{"type.mismatch"}},
		{"a value wider than the elements of the array argument of append is a type mismatch", one("main() { Int8[] xs = [1]; Int v = 2; log(append(xs, v)) }\n"), []string{"type.mismatch"}},
		{"a nullable value beside an array of its base type is a type mismatch", one("main() { Int[] xs = [1]; Int? v = 2; log(contains(xs, v)) }\n"), []string{"type.mismatch"}},
		{"a key wider than the keys of the dictionary argument of has is a type mismatch", one("main() { [Int8: String] d = [1: \"a\"]; Int k = 1; log(has(d, k)) }\n"), []string{"type.mismatch"}},
		{"a second array argument with narrower elements is a type mismatch", one("main() { Int[] xs = [1]; Int8[] ys = [2]; log(concat(xs, ys)) }\n"), []string{"type.mismatch"}},
		{"a second array argument with elements of a specific error type is a type mismatch", one("Denied!\nmain() { Error![] xs = [Denied!]; Denied![] ys = [Denied!]; log(count(concat(xs, ys))) }\n"), []string{"type.mismatch"}},
		{"an error value beside an array of Error! is accepted", one("Denied!\nmain() { Error![] xs = [Denied!]; log(count(append(xs, Denied!))) }\n"), nil},
		{"a value narrower than the elements of the array argument is accepted", one("main() { Int[] xs = [1]; Int8 v = 2; log(contains(xs, v)); log(append(xs, v)) }\n"), nil},
		{"a key narrower than the keys of the dictionary argument is accepted", one("main() { [Int: String] d = [1: \"a\"]; Int8 k = 1; log(has(d, k)) }\n"), nil},
		{"an array literal as the argument of a built-in takes the parameter type", one("main() { log(String.fromBytes([104, 105])) }\n"), nil},
		{"a call of fill as the argument of a built-in takes the parameter type", one("main() { log(r::Random.seed(fill(7, 32))) }\n"), nil},
		{"an array literal whose entry is outside the parameter type of a built-in is out of range", one("main() { log(String.fromBytes([104, 300])) }\n"), []string{"constant.out-of-range"}},
		{"Byte and Int do not interchange", one("main() { Byte value = 1; Int other = value; log(other) }\n"), []string{"type.mismatch"}},
		{"Byte has no arithmetic", one("main() { Byte a = 1; Byte b = 2; log(a + b) }\n"), []string{"operator.invalid-operands"}},
		{"an integer literal does not acquire Decimal type", one("main() { Decimal a = 1.0; log(a + 2) }\n"), []string{"operator.invalid-operands"}},
		{"a fractional literal beside a Decimal operand takes Decimal", one("main() { Decimal a = 1.0; log(a == 1.000); log(a + 2.5) }\n"), nil},
		{"Decimal and Float do not mix", one("main() { Decimal a = 1.0; Float b = 2.0; log(a + b) }\n"), []string{"operator.invalid-operands"}},
		{"Duration remainder has no mapping", one("main() { log(5s % 2s) }\n"), []string{"operator.invalid-operands"}},
		{"Time plus Duration is a Time", one("main() { Time t = t23:30:00 + 2h; Duration d = t01:00:00 - t23:00:00; log(t); log(d) }\n"), nil},
		{"a step of the type of Date bounds is a type mismatch", one("main() { for d in range(d2025-01-01, d2025-01-03, d2025-01-02) { log(d) } }\n"), []string{"type.mismatch"}},
		{"a return forwards the results of a method call", one("Box { Int n }\nBox.pair() -> (Int, Int) { return self.n, self.n }\nboth(Box b) -> (Int, Int) { return b.pair() }\nmain() { Int a, Int c = both(Box { n: 1 }); log(a + c) }\n"), nil},
		{"a return forwards the results of a built-in method call", one("parts(DateTime at) -> (Int, Int) { return at.unix() }\nmain() { Int a, Int c = parts(2026-09-23T01:00:00Z); log(a + c) }\n"), nil},
		{"a returned method call with fewer results than the routine is an arity fault", one("Box { Int n }\nBox.one() -> Int { return self.n }\nboth(Box b) -> (Int, Int) { return b.one() }\nmain() { Int a, Int c = both(Box { n: 1 }); log(a + c) }\n"), []string{"expression.arity"}},
		{"a zero-result call cannot supply an argument", one("zero() {}\ntake(Int value) {}\ncaller() { take(zero()) }\n"), []string{"expression.arity"}},
		{"a multi-result call cannot supply one argument", one("pair() -> (Int, Bool) { return 1, true }\ntake(Int value) {}\ncaller() { take(pair()) }\n"), []string{"expression.arity"}},
		{"an omitted required argument is reported at the call close", one("need(Int one) {}\ncaller() { need() }\n"), []string{"call.missing-argument"}},
		{"the first extra positional argument is reported", one("need(Int one) {}\ncaller() { need(1, 99, 100) }\n"), []string{"call.extra-argument"}},
		{"an error field cannot hold a function", one("Bad! { (Int) -> Int convert }\nmain() { log(1) }\n"), []string{"type.invalid-field"}},
		{"an error field cannot hold errors inside an array", one("Other!\nBad! { Other![] priors }\nmain() { log(1) }\n"), []string{"type.invalid-field"}},
		{"a mistyped match candidate is rejected", one("main() { match 1 { case \"one\" => log(1) } }\n"), []string{"type.mismatch"}},
		{"a negative literal index is out of range", one("main() { Int[] xs = [1]; log(xs[-1]) }\n"), []string{"constant.out-of-range"}},
		{"a negative constant index is out of range", one("Int BACK = -1\nmain() { Int[] xs = [1]; xs[BACK] = 2; log(xs) }\n"), []string{"constant.out-of-range"}},
		{"a constant index beyond a fixed length is out of range", one("main() { Int[2] xs = [1, 2]; log(xs[2]) }\n"), []string{"constant.out-of-range"}},
		{"a constant index within a fixed length is accepted", one("main() { Int[2] xs = [1, 2]; log(xs[1]) }\n"), nil},
		{"a negative slice bound is out of range", one("main() { log(\"abc\"[-1:2]) }\n"), []string{"constant.out-of-range"}},
		{"an inverted constant slice is out of range", one("main() { Int[] xs = [1, 2, 3]; log(xs[2:1]) }\n"), []string{"constant.out-of-range"}},
		{"a constant slice bound beyond a fixed length is out of range", one("main() { Int[2] xs = [1, 2]; log(xs[0:3]) }\n"), []string{"constant.out-of-range"}},
		{"an empty array literal requires a context", one("main(){[]}\n"), []string{"syntax.discarded-value"}},
		{"an empty array literal as an argument requires a context", one("main() { log([]) }\n"), []string{"type.context-required"}},
		{"a match without a catch-all leaves a completion path", one("choose(Int value) -> Int { match value { case 1 => return 10 } }\n"), []string{"function.missing-return"}},
		{"a deferred local cannot be read before assignment", one("main() {\n    Int pending\n    log(pending)\n}\n"), []string{"variable.use-before-assign"}},
		{"an assignment right side cannot read its uninitialized target", one("main() {\n    Int value\n    value = value + 1\n}\n"), []string{"variable.use-before-assign"}},
		{"a completing branch without assignment leaves the variable unassigned", one("choose(Bool flag) -> Int {\n    Int value\n    if flag { value = 4 }\n    return value\n}\n"), []string{"variable.use-before-assign"}},
		{"a constant-false loop contributes the pre-loop state", one("bad() -> Int {\n    Int value\n    for false { value = 5 }\n    return value\n}\n"), []string{"variable.use-before-assign"}},
		{"a break path without assignment prevents post-loop assignment", one("bad(Bool flag) -> Int {\n    Int value\n    for { if flag { value = 1 }; break }\n    return value\n}\n"), []string{"variable.use-before-assign"}},
		{"a compile-time true condition keeps only its path", one("good() -> Int {\n    Int value\n    if true { value = 4 } else { log(1) }\n    return value\n}\n"), nil},
		{"a resume value for a statement without results is incompatible", one("Fault!\nstop() { return Fault! }\nmain() { stop() handle { Fault! => resume 1 } }\n"), []string{"handle.incompatible-replacement"}},
		{"a bare resume on a declaration leaves the variable without a value", one("Fault!\nstop() -> Int { return Fault! }\nmain() { Int value = stop() handle { Fault! => resume }; log(value) }\n"), []string{"variable.use-before-assign"}},
		{"a short replacement list is incompatible", one("Fault!\nstop() -> (Int, String) { return Fault! }\nmain() { Int value, String text = stop() handle { Fault! => 1 }; log(value); log(text) }\n"), []string{"handle.incompatible-replacement"}},
		{"a mistyped replacement is incompatible", one("Fault!\nstop() -> Int { return Fault! }\nmain() { Int value = stop() handle { Fault! => \"bad\" }; log(value) }\n"), []string{"handle.incompatible-replacement"}},
		{"a value arm on a block whose failing statement has no results is incompatible", one("Fault!\nnothing() { return Fault! }\nnumber() -> Int { return Fault! }\nmain() { { nothing(); Int value = number(); log(value) } handle { any => 5 } }\n"), []string{"handle.incompatible-replacement"}},
		{"a value arm on a block recovers each failing statement that has results", one("Fault!\nnothing() { log(1); return Fault! }\nsmall() -> Int8 { log(2); return Fault! }\nwide() -> Int64 { log(3); return Fault! }\nmain() { { Int8 first = small(); Int64 second = wide(); log(first); log(second) } handle { any => 5 } }\n"), nil},
		{"a value arm on a failed condition is incompatible", one("Fault!\ncheck() -> Bool { log(1); return Fault! }\nmain() {\n    {\n        if check() { log(2) }\n        log(3)\n    } handle { Fault! => true }\n    log(4)\n}\n"), []string{"handle.incompatible-replacement"}},
		{"a no-op arm cannot recover a field store", one("Fault!\nfail() -> Int { return Fault! }\nBox { Int value }\nmain() {\n    Box box = Box { 1 }\n    box.value = fail() handle { Fault! => {} }\n    log(box.value)\n}\n"), []string{"handle.no-op-store"}},
		{"a no-op arm cannot recover a valued return", one("Fault!\nfail() -> Int { return Fault! }\nread() -> Int { return fail() handle { Fault! => {} } }\n"), []string{"function.missing-return", "handle.no-op-return"}},
		{"a variable that held a value before a block handler has none after a no-op arm", one("main() {\n    Int q = 0\n    {\n        q = 1 / q\n    } handle {\n        any => { log(1) }\n    }\n    log(q)\n}\n"), []string{"variable.use-before-assign"}},
		{"a variable that held a value before a loop handler has none after a no-op arm", one("main() {\n    Int q = 0\n    for i in [1] {\n        q = i / q\n    } handle {\n        any => { log(1) }\n    }\n    log(q)\n}\n"), []string{"variable.use-before-assign"}},
		{"a variable that a protected store gives its first value has none after a no-op arm", one("main() {\n    Int q\n    Int z = 0\n    {\n        q = 1 / z\n    } handle {\n        any => { log(1) }\n    }\n    log(q)\n}\n"), []string{"variable.use-before-assign"}},
		{"a later read in the block of a variable that a no-op arm leaves without a value is rejected", one("main() {\n    Int q\n    Int z = 0\n    {\n        q = 1 / z\n        log(q)\n    } handle {\n        any => { log(1) }\n    }\n}\n"), []string{"variable.use-before-assign"}},
		{"a variable that a block handler arm stores holds a value after the block", one("Down!\nquote(Int sku) -> Int | Down! {\n    if sku == 0 { return Down! }\n    return sku * 10\n}\nmain() {\n    Int price\n    {\n        price = quote(3)\n    } handle Down! => { price = 0 }\n    log(price)\n}\n"), nil},
		{"a later read in the block of a variable that the no-op arm stores is accepted", one("Down!\nquote(Int sku) -> Int | Down! {\n    if sku == 0 { return Down! }\n    return sku * 10\n}\nmain() {\n    Int price\n    {\n        price = quote(3)\n        log(price)\n    } handle Down! => { price = 0 }\n}\n"), nil},
		{"a read in a block handler arm sees the value that the variable held before the failed store", one("Down!\nquote(Int sku) -> Int | Down! {\n    if sku == 0 { return Down! }\n    return sku * 10\n}\nmain() {\n    Int price = 1\n    {\n        price = quote(3)\n    } handle Down! => { log(price); price = 0 }\n    log(price)\n}\n"), nil},
		{"a read after a block handler arm that only reads the variable is rejected", one("Down!\nquote(Int sku) -> Int | Down! {\n    if sku == 0 { return Down! }\n    return sku * 10\n}\nmain() {\n    Int price = 1\n    {\n        price = quote(3)\n    } handle Down! => { log(price) }\n    log(price)\n}\n"), []string{"variable.use-before-assign"}},
		{"a fallible index operand in an assignment target is rejected", one("Fault!\nslot(Int key) -> Int { if key < 0 { return Fault! }; return key }\nmain() {\n    Int[] counts = [1, 2]\n    counts[slot(0)] = 0\n    log(counts[0])\n}\n"), []string{"assignment.fallible-operand"}},
		{"a break in an arm outside a loop is rejected", one("Fault!\ncheck(Int k) { if k == 2 { return Fault! } }\nmain() {\n    {\n        check(2)\n        log(1)\n    } handle {\n        Fault! => break\n    }\n}\n"), []string{"control.missing-loop"}},
		{"a label outside the arm is not a transfer target", one("Fault!\nfail() -> Int { return Fault! }\nmain() { outer: for item in [1] { Int value = fail() handle { Fault! => break outer }; log(value) } }\n"), []string{"label.unavailable"}},
		{"resume outside an arm is rejected", one("main() { resume 1 }\n"), []string{"control.resume-outside-handler"}},
		{"an unrelated typed arm is dead", one("Missing!\nOther!\nread() -> Int { return Missing! }\nmain() { Int value = read() handle { Other! => 7 }; log(value) }\n"), []string{"handle.dead-arm"}},
		{"an opaque error value permits any typed arm", one("Missing!\nOther!\nrelay(Error! value) -> Int { return value }\nmain() { Int value = relay(Missing!) handle { Other! => 7 }; log(value) }\n"), nil},
		{"an alias arm after its canonical type is dead", one("Fault!\nAlias! = Fault!\nread() -> Int { return Fault! }\nmain() { Int value = read() handle { Fault! => 1; Alias! => 2 }; log(value) }\n"), []string{"handle.dead-arm"}},
		{"a recovery failure bypasses sister arms", one("Missing!\nBackupFailed!\nread() -> Int { return Missing! }\nbackup() -> Int { return BackupFailed! }\nmain() { Int value = read() handle { Missing! => backup(); BackupFailed! => 9 }; log(value) }\n"), []string{"handle.dead-arm"}},
		{"a recovery failure is available to an enclosing handler", one("Missing!\nBackupFailed!\nread() -> Int { return Missing! }\nbackup() -> Int { return BackupFailed! }\nmain() { { Int value = read() handle { Missing! => backup() }; log(value) } handle { BackupFailed! => 5 } }\n"), nil},
		{"the original error continues outward past a failed arm", one("Alpha!\nBeta!\nread() -> Int { return Alpha! }\nrepair() -> Int { return Beta! }\nmain() {\n    {\n        Int value = read() handle { Alpha! => repair() }\n        log(value)\n    } handle {\n        Alpha! => 7\n    }\n    log(8)\n}\n"), nil},
		{"a nullable path narrows inside its presence test", one("main() { Int? maybe = 3; if maybe != none { log(maybe + 1) } }\n"), nil},
		{"a nullable value is not used as its base type without narrowing", one("main() { Int? maybe = 3; log(maybe + 1) }\n"), []string{"operator.invalid-operands"}},
		{"a closure under a function-typed pin takes its parameter types", one("main() {\n    Int base = 10\n    (Int) -> Int addBase = x => x + base\n    log(addBase(5))\n}\n"), nil},
		{"a closure without a pin writes its parameter types", one("main() {\n    log(apply(x => x + 1))\n}\napply((Int) -> Int f) -> Int { return f(1) }\n"), nil},
		{"a bare closure parameter outside a pin has no type", one("main() {\n    Int[] xs = [1]\n    log(count(xs))\n    log((x => x)(1))\n}\n"), []string{"parameter.missing-type"}},
		{"a local constant folds through the evaluator", one("main() {\n    Int8 LIMIT = 100\n    Int8 x = LIMIT + 50\n    log(x)\n}\n"), []string{"constant.out-of-range"}},
		{"a constant temporal addition beyond the domain is rejected", one("Date BAD = (d+999999-12-31).addDays(1)\nmain() { log(BAD) }\n"), []string{"constant.out-of-range"}},
		{"a local variable that no statement reads is rejected", one("main() {\n    Int x = 1\n    x = 2\n}\n"), []string{"variable.unread"}},
		{"a handler arm binding that no statement reads is rejected", one("Bad!\nread() -> Int { return Bad! }\nmain() {\n    Int v = read() handle { Bad! e => 0 }\n    log(v)\n}\n"), []string{"variable.unread"}},
		{"a value arm on the validation of a plain element store is incompatible", one("main() {\n    Int[] a = [1, 2]\n    a[5] = 9 handle { OutOfRange! => 4 }\n    log(a)\n}\n"), []string{"handle.incompatible-replacement"}},
		{"a struct key with an array field is an invalid key", one("Bad { Int[] values }\nmain() { [Bad: Int] data = [Bad { [1] }: 2]; log(count(data)) }\n"), []string{"type.invalid-key"}},
		{"a struct key with a Float field is an invalid key", one("Bad { Float v }\nmain() { [Bad: Int] data = [:]; log(count(data)) }\n"), []string{"type.invalid-key"}},
		{"a Float key in the written type of a field is an invalid key", one("Box {\n    [Float: Int] d\n}\nmain() { log(1) }\n"), []string{"type.invalid-key"}},
		{"a Float key in the written type of a parameter is an invalid key", one("take([Float: Int] d) {}\nmain() { log(1) }\n"), []string{"type.invalid-key"}},
		{"a Float key in a written type initialized by zip is an invalid key", one("main() { Float[] ks = [1.0]; Int[] vs = [1]; [Float: Int] data = zip(ks, vs); log(count(data)) }\n"), []string{"type.invalid-key"}},
		{"a Float key that zip infers without a written type is an invalid key", one("main() { Float[] ks = [1.0]; Int[] vs = [1]; log(count(zip(ks, vs))) }\n"), []string{"type.invalid-key"}},
		{"a written Float key initialized by a literal is reported once", one("main() { [Float: Int] data = [1.0: 1]; log(count(data)) }\n"), []string{"type.invalid-key"}},
		{"an iterator loop over a value that is not an array, a dictionary, or a range is a type mismatch", one("main() {\n    for x in 5 { log(x) }\n}\n"), []string{"type.mismatch"}},
		{"an unread loop variable, parameter, and member arm binding are accepted", one("show(Int unused) {}\nmain() {\n    Int? maybe = 1\n    for x in [1] { show(1) }\n    match maybe {\n        case Int v => { log(0) }\n        any => { log(1) }\n    }\n}\n"), nil},
		{"an expression nested 900 levels deep is accepted", one("main() {\n    Int x = " + strings.Repeat("(", 900) + "1" + strings.Repeat(")", 900) + "\n    log(x)\n}\n"), nil},
		{"an expression nested deeper than 1000 levels is unsupported", one("main() {\n    Int x = " + strings.Repeat("(", 1000) + "1" + strings.Repeat(")", 1000) + "\n    log(x)\n}\n"), []string{"limit.unsupported-syntax"}},
	}
	runTable(t, rows, sourceCodes)
}

// kernelCodes returns the codes the kernel checks report for a kernel program, or internal for a typing fault.
func kernelCodes(files []testsrc.File) []string {
	diags, err := checkKernel(files)
	if err != nil {
		return []string{"internal"}
	}
	var codes []string
	for _, d := range diags {
		codes = append(codes, d.Reason)
	}
	return codes
}

func TestEachKernelProgramThatBreaksAKernelCheckIsRejectedWithItsStatedCode(t *testing.T) {
	rows := []row{
		{"a variable read before every path assigns it is rejected", one("main() {\n    Int value\n    match {\n        case count([1]) > 0 => { value = 1 }\n    }\n    log(value)\n}\n"), []string{"variable.use-before-assign"}},
		{"a variable assigned on every arm of a covering match is assigned", one("main() {\n    Int value\n    match {\n        case count([1]) > 0 => { value = 1 }\n        any => { value = 2 }\n    }\n    log(value)\n}\n"), nil},
		{"a variable assigned only inside a loop is unassigned after it", one("main() {\n    Int value\n    for {\n        match {\n            case count([1]) > 0 => { break }\n        }\n        value = 1\n    }\n    log(value)\n}\n"), []string{"variable.use-before-assign"}},
		{"a variable assigned only inside an iterator loop is unassigned after it", one("main() {\n    Int value\n    for x in [1] {\n        value = x\n    }\n    log(value)\n}\n"), []string{"variable.use-before-assign"}},
		{"an iterator loop raises what its source raises", one("Bad!\nOther!\nitems() -> Int[] {\n    return Bad!\n}\ntotal() -> Int | Other! {\n    Int t = 0\n    for x in items() {\n        t = t + x\n    }\n    return t\n}\n"), []string{"contract.undeclared-error", "contract.unraised-error"}},
		{"an iterator loop over an array, a dictionary, or a range raises nothing of its own, so a listed error cannot escape", one("Other!\ntotal(Int[] xs, [String: Int] d) -> Int | Other! {\n    Int t = 0\n    for x in xs {\n        t = t + x\n    }\n    for k, v in d {\n        t = t + v\n    }\n    for i in range(0, 3) {\n        t = t + i\n    }\n    return t\n}\n"), []string{"contract.unraised-error"}},
		{"a routine's declared set lists exactly its escaping domain errors", one("Bad!\nOther!\nparse() -> Int | Bad! {\n    return Other!\n}\nmain() {\n    log(parse())\n}\n"), []string{"contract.undeclared-error", "contract.unraised-error"}},
		{"a routine whose escaping set its declared set covers is accepted", one("Bad!\nparse() -> Int | Bad! {\n    return Bad!\n}\nmain() {\n    log(parse())\n}\n"), nil},
		{"a service method without a declared error set lets its errors escape", one("Bad!\nfail() -> Int {\n    return Bad!\n}\n@Svc {}\n@Svc.read() -> Int {\n    return fail()\n}\nmain() {\n    log(@Svc.read())\n}\n"), nil},
		{"an error arm that no outcome of the call selects is dead", one("Bad!\nOther!\nparse() -> Int {\n    return Bad!\n}\nmain() {\n    match parse() {\n        case Int v => { log(v) }\n        case Other! e => { log(0) }\n    }\n}\n"), []string{"match.dead-arm"}},
		{"an any arm after the member arm of the success type is dead", one("Bad!\nparse() -> Int {\n    return Bad!\n}\nmain() {\n    match parse() {\n        case Int v => { log(v) }\n        any => { log(0) }\n    }\n}\n"), []string{"match.dead-arm"}},
		{"an any arm after a member arm with several bindings is dead", one("Bad!\npair() -> (Int, String) {\n    return Bad!\n}\nmain() {\n    match pair() {\n        case Int n, String s => { log(n); log(s) }\n        any => { log(0) }\n        any! e => { log(1) }\n    }\n}\n"), []string{"match.dead-arm"}},
		{"a member arm with several bindings binds a nullable result without narrowing it", one("pair() -> (Int?, String) {\n    return 1, \"one\"\n}\nmain() {\n    match pair() {\n        case Int n, String s => { log(n); log(s) }\n    }\n}\n"), []string{"match.invalid-arm"}},
		{"an any! arm on a call that cannot fail is dead", one("parse() -> Int {\n    return 1\n}\nmain() {\n    match parse() {\n        case Int v => { log(v) }\n        any! e => { log(0) }\n    }\n}\n"), []string{"match.dead-arm"}},
		{"an outcome match whose arms select the outcomes is accepted", one("Bad!\nparse() -> Int {\n    return Bad!\n}\nmain() {\n    match parse() {\n        case Int v => { log(v) }\n        case Bad! e => { log(0) }\n    }\n}\n"), nil},
		{"a failure that no arm selects escapes the routine", one("Bad!\nOther!\nparse() -> Int {\n    return Bad!\n}\nwrap() -> Int | Other! {\n    match parse() {\n        case Int v => { return v }\n        case Other! e => { return e }\n    }\n    return 0\n}\n"), []string{"contract.undeclared-error", "match.dead-arm"}},
		{"the binding of an any! arm raises the opaque set, so a caller's any! arm can select", one("Bad!\nparse() -> Int {\n    return Bad!\n}\nwrap() -> Int {\n    match parse() {\n        case Int v => { return v }\n        any! e => { return e }\n    }\n}\nmain() {\n    match wrap() {\n        case Int v => { log(v) }\n        case Bad! b => { log(0) }\n        any! e => { log(1) }\n    }\n}\n"), nil},
		{"a call of a function value raises the opaque set, so its any! arm can select", one("apply((Int) -> Int f) {\n    match f(1) {\n        case Int v => { log(v) }\n        any! e => { log(0) }\n    }\n}\nmain() {\n    apply((Int x) => x + 1)\n}\n"), nil},
		{"a closure body is checked against the sets its own operations infer", one("one() -> Int {\n    return 1\n}\nmain() {\n    (Int) -> Int add = (Int x) => {\n        match one() {\n            case Int v => { return x + v }\n            any! e => { return x }\n        }\n    }\n    log(add(1))\n}\n"), []string{"match.dead-arm"}},
		{"a match over a nullable subject is not a kernel form", one("main() {\n    Int? t = 3\n    match t {\n        case Int v => { log(v) }\n        any => { log(0) }\n    }\n}\n"), []string{"internal"}},
		{"case none on a call that returns a value cannot select", one("parse() -> Int {\n    return 1\n}\nmain() {\n    match parse() {\n        case none => { log(0) }\n        any => { log(1) }\n    }\n}\n"), []string{"match.invalid-arm"}},
		{"a path has its value type in the arm of its presence test", one("label(String? nick, String name) -> String {\n    String r\n    match {\n        case nick != none => { r = nick }\n        any => { r = name }\n    }\n    return r\n}\nmain() {\n    log(label(none, \"anon\"))\n}\n"), nil},
		{"a path has its value type after a store of a value that is not nullable", one("main() {\n    Int? n = none\n    n = 3\n    log(n + 1)\n}\n"), nil},
		{"a loop head keeps a path that every edge back to it keeps present", one("main() {\n    Int? n = none\n    n = 1\n    for x in [1, 2] {\n        log(n + x)\n        n = x\n    }\n}\n"), nil},
		{"a loop head drops a path that an edge back to it leaves absent", one("main() {\n    Int? n = none\n    n = 1\n    for x in [1, 2] {\n        log(n + x)\n        n = none\n    }\n}\n"), []string{"internal"}},
		{"a closure body starts with no present path", one("main() {\n    Int? n = none\n    n = 1\n    () -> Int f = () => n + 1\n    log(f())\n}\n"), []string{"internal"}},
		{"a later candidate starts on the false edges of the earlier ones", one("main() {\n    Int? n = 2\n    match {\n        case n == none => { log(0) }\n        case n > 1 => { log(n) }\n    }\n}\n"), nil},
		{"a remote call result that a later statement demands is accepted", one("@Svc {}\n@Svc.read(String key) -> Int {\n    return 1\n}\nmain() {\n    Int used = @Svc.read(\"used\")\n    log(used)\n}\n"), nil},
		{"a kernel program with a type fault is an internal error", one("main() {\n    Int value = true\n}\n"), []string{"internal"}},
		{"a send as a whole value, as an element of a collection of futures, and as a statement is accepted with a join before each effect and exit", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    future<Int> a = send @Svc.read(1)\n    future<Int>[] xs = [a, send @Svc.read(2)]\n    xs = append(xs, send @Svc.read(3))\n    [String: future<Int>] d = [\"k\": xs[0]]\n    d[\"l\"] = 4\n    send @Svc.read(5) {\n        any! => 0\n    }\n    Int n = count(xs) + count(d)\n    for k, v in d {\n        n = n + wait v\n    }\n    join\n    print(wait a + n)\n    match wait send @Svc.read(6) {\n        case Int v => { n = v }\n    }\n    join\n}\n"), nil},
		{"an effect that a send reaches with no join between is rejected", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    future<Int> a = send @Svc.read(1)\n    print(wait a)\n}\n"), []string{"future.misplaced-form"}},
		{"the end of a routine that a send reaches with no join between is rejected", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    for k in [1, 2] {\n        send @Svc.read(k)\n    }\n}\n"), []string{"future.misplaced-form"}},
		{"a call of log after a send needs no join", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    send @Svc.read(1)\n    log(1)\n    join\n}\n"), nil},
		{"a future that an operation uses without a wait is a type fault", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    future<Int> a = send @Svc.read(1)\n    join\n    log(a + 1)\n}\n"), []string{"internal"}},
		{"an operation that reads the elements of a collection of futures is a type fault", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    future<Int>[] xs = [send @Svc.read(1)]\n    join\n    log(first(xs))\n}\n"), []string{"internal"}},
		{"a parameter of a future type is rejected", one("twice(future<Int> a) -> Int {\n    return 2\n}\nmain() {\n    log(twice(1))\n}\n"), []string{"future.misplaced-form"}},
		{"a send of a call of a routine that reaches no remote read is rejected", one("plain(Int k) -> Int {\n    return k\n}\nmain() {\n    future<Int> a = send plain(1)\n    join\n    log(wait a)\n}\n"), []string{"future.misplaced-form"}},
		{"a closure that reads a future of its routine is a type fault", one("@Svc {}\n@Svc.read(Int k) -> Int {\n    return k\n}\nmain() {\n    future<Int> a = send @Svc.read(1)\n    () -> Int f = () => wait a\n    join\n    log(f())\n}\n"), []string{"internal"}},
	}
	runTable(t, rows, kernelCodes)
}

func TestANarrowedUseWithoutAPresenceFactReportsThePathAndTheOriginThatEndedIt(t *testing.T) {
	rows := []struct {
		fact    string
		text    string
		code    string
		message string
	}{
		{"a join reports the edge that lacks the fact", "bad(Int? value, Bool pick) -> Int {\n    if pick { if value == none { return 0 } }\n    return value + 1\n}\n", "operator.invalid-operands", `"value" may be absent on the path where "pick" is false`},
		{"a store of a nullable value reports the store", "maybe() -> Int? { return none }\nmain() {\n    Int? n = 1\n    if n != none {\n        n = maybe()\n        log(n + 1)\n    }\n}\n", "operator.invalid-operands", `"n" may be absent after the store at line 5, column 9, whose value may be absent`},
		{"a struct method call on a prefix reports the call", "Leaf { Int n }\nTree { Leaf? left }\nTree.clear() { self.left = none }\nmain() {\n    Tree tree = Tree { Leaf { 1 } }\n    if tree.left {\n        tree.clear()\n        log(tree.left.n)\n    }\n}\n", "nullable.unresolved", `"tree.left" may be absent after the call of clear at line 7, column 9`},
		{"a loop back edge reports the edge and the store on it", "main() {\n    Int? n = none\n    n = 1\n    for x in [1, 2] {\n        log(n + x)\n        n = none\n    }\n}\n", "operator.invalid-operands", `"n" may be absent on the edge back to the head of the loop at line 4, column 5, after the store at line 6, column 9, whose value may be absent`},
		{"a path that no test establishes keeps the general message", "main() {\n    Int? n = none\n    log(n + 1)\n}\n", "operator.invalid-operands", `"n" is not known to hold a value`},
	}
	for _, tt := range rows {
		diags := check(one(tt.text))
		if len(diags) != 1 || diags[0].Reason != tt.code || !strings.Contains(diags[0].Message, tt.message) {
			t.Errorf("%s: got %v, want one %s containing %q", tt.fact, diags, tt.code, tt.message)
		}
	}
}

func TestEachDeclaredErrorSetIsCheckedAgainstTheInferredRaiseSet(t *testing.T) {
	const service = "@Svc {}\n"
	rows := []row{
		{"a free function whose declared set lists its escaping domain error is accepted", one("Oops!\nf(Int x) -> Int | Oops! {\n    if x > 1 { return Oops! }\n    return x\n}\nmain() { log(f(1)) }\n"), nil},
		{"a service method whose declared set lists its escaping domain error is accepted", one("Oops!\n" + service + "@Svc.f(Int x) -> Int | Oops! {\n    if x > 1 { return Oops! }\n    return x\n}\nmain() { log(@Svc.f(1)) }\n"), nil},
		{"a free function's listed error that cannot escape is unraised", one("Oops!\nf(Int x) -> Int | Oops! { return x }\nmain() { log(f(1)) }\n"), []string{"contract.unraised-error"}},
		{"a service method's listed error that cannot escape is unraised", one("Oops!\n" + service + "@Svc.f(Int x) -> Int | Oops! { return x }\nmain() { log(@Svc.f(1)) }\n"), []string{"contract.unraised-error"}},
		{"a free function's escaping domain error that its set omits is undeclared", one("Oops!\nOther!\nf(Int x) -> Int | Oops! {\n    if x > 1 { return Oops! }\n    return Other!\n}\nmain() { log(f(1)) }\n"), []string{"contract.undeclared-error"}},
		{"a service method's escaping domain error that its set omits is undeclared", one("Oops!\nOther!\n" + service + "@Svc.f(Int x) -> Int | Oops! {\n    if x > 1 { return Oops! }\n    return Other!\n}\nmain() { log(@Svc.f(1)) }\n"), []string{"contract.undeclared-error"}},
		{"a catalog error escapes a free function without being listed", one("Oops!\nf(Int x) -> Int | Oops! {\n    if x > 5 { return Oops! }\n    return 10 / x\n}\nmain() { log(f(1)) }\n"), nil},
		{"a catalog error escapes a service method without being listed", one("Oops!\n" + service + "@Svc.f(Int x) -> Int | Oops! {\n    if x > 5 { return Oops! }\n    return 10 / x\n}\nmain() { log(@Svc.f(1)) }\n"), nil},
		{"a free function's listed catalog error is invalid", one("Oops!\nf(Int x) -> Int | Oops! | OutOfRange! {\n    if x > 5 { return Oops! }\n    return 10 / x\n}\nmain() { log(f(1)) }\n"), []string{"contract.invalid-error"}},
		{"a service method's listed runtime error is invalid", one("Oops!\n" + service + "@Svc.f(Int x) -> Int | Oops! | Unavailable! {\n    if x > 5 { return Oops! }\n    return x\n}\nmain() { log(@Svc.f(1)) }\n"), []string{"contract.invalid-error"}},
		{"a free function whose raise set is opaque cannot carry a declared set", one("Oops!\nf((Int) -> Int g) -> Int | Oops! {\n    if g(1) > 1 { return Oops! }\n    return 1\n}\nmain() { log(f((Int x) => x)) }\n"), []string{"contract.undeclared-error"}},
		{"a caller sees the inferred set of a service method, not its declared set", one("Oops!\n" + service + "@Svc.f(Int x) -> Int | Oops! {\n    if x > 5 { return Oops! }\n    return 10 / x\n}\nmain() {\n    Int r = @Svc.f(0) handle DivisionByZero! => 9\n    log(r)\n}\n"), nil},
	}
	runTable(t, rows, sourceCodes)
}

func TestADiagnosticBeginningWithASourceNameQuotesItUnchanged(t *testing.T) {
	tests := []struct {
		fact, text, want string
	}{
		{"an undeclared error names the service method as written", "Out!\nOther!\n@Ord { }\n@Ord.place(Bool b) -> Int | Out! {\n    if b { return Out! }\n    return Other!\n}\nmain() { log(@Ord.place(true)) }\n", `"@Ord.place" can raise Other!, which its declared error set does not list.`},
		{"an undeclared error names a free function as written", "Oops!\nOther!\nf(Bool b) -> Int | Oops! {\n    if b { return Oops! }\n    return Other!\n}\nmain() { log(f(true)) }\n", `"f" can raise Other!, which its declared error set does not list.`},
		{"an unknown member keeps the case of the type and the member", "Pt { Int x }\nmain() { Pt p = Pt { 1 }; log(p.y) }\n", `"Pt" has no member "y".`},
	}
	for _, tt := range tests {
		var got []string
		for _, d := range check(one(tt.text)) {
			got = append(got, diagnostics.Sentence(d.Message))
		}
		if !slices.Contains(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.fact, got, tt.want)
		}
	}
}
