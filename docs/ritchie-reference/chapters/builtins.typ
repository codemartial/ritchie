#import "../prelude.typ": horizontalrule, idx, secref

= Built-ins
<chap-builtins>
== The built-in set
<builtins-the-built-in-set>
#idx("constant-capable operation")
#idx("eligible error set")
Ritchie provides a small set of always-in-scope functions for the values the
type system builds in: arrays, dicts, strings, numbers, ranges, and
diagnostics. A built-in uses ordinary call syntax and participates in
ordinary name resolution. It needs no import.

Array element assignment updates a slot; dictionary assignment inserts
or replaces an entry (#secref(<types-collection-types>)).
Collection transformations return owned values used to reassign the
collection, as each built-in operation specifies.

```ritchie
lines = append(lines, line)         // reassign: lines is now one longer
total = sum(map(lines, l => l.price))
```

A built-in name is predefined but not reserved. A local declaration of
the same name shadows the built-in within that declaration's scope
(#link(<chap-lexical>)[Lexical]).

Each built-in name denotes an operation. A type is accepted by a
built-in only when its operation has a natural meaning for the type.

An operation is #emph[constant-capable] when each of its operand and result
types admits constant values, it neither writes output nor is a direct
impurity root (#secref(<stdlib-impurity>)), its result does not depend on
an order that the language leaves unspecified, and every error of its
contract is in the eligible error set. The #emph[eligible error set] holds
the catalog errors that operations on values raise: `DivisionByZero!`,
`OutOfRange!`, `InvalidStringBoundary!`, `Malformed!`, `MissingKey!`, and
`LengthMismatch!` (#secref(<errors-the-built-in-error-catalog>)).

A call of a constant-capable operation whose arguments are constant
expressions is itself a constant expression and has the same result as
an ordinary call. An eligible error that its evaluation raises is a
compile error at that expression. Every other operation has only its
ordinary runtime form.

== Output and diagnostics
<builtins-diagnostics>
#idx("print", code: true)
#idx("log", code: true)
`print(value)` writes the value's default display form followed by a line
break to the program's output, then returns nothing. `log(value)` writes
the same text to the program's diagnostic output. Each accepts one argument
of any type. The calls of each write in execution order.

`print` and `log` are infallible. Failure to write does not raise an error
or change control flow. Evaluating the argument follows the ordinary
expression rules and may fail before the call.

```ritchie
print(order)                            // any value, a struct or a reference included
print(f"shipped {shipped} of {total}")  // combine values through an f-string
log(f"cache miss for {sku}")            // a record of what happened, not a result
```

To record several values together, interpolate them into an f-string
(#link(<chap-lexical>)[Lexical]).

== Ranges
<builtins-ranges>
#idx("range", code: true)
The predefined `range(...)` form is an iterator-source call valid only
immediately after `in` in an iterator `for` loop
(#link(<chap-statements>)[Statements]). The loop consumes it directly as
a local counting source. It does not produce a value that can be assigned to a name,
passed, returned, compared, displayed, or stored. Its operand expressions
are evaluated once, in written order, before iteration begins.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Supplies],),
    table.hline(),
    [`range(end)`], [integer types: `0` up to but not including `end`,
    with default step `1`],
    [`range(lo, hi)`], [`lo` up to but not including `hi`, with default
    step `1`],
    [`range(lo, hi, step)`], [`lo`, `lo + step`, `lo + 2*step`, \...,
    stopping before `hi`],
  )]
  , kind: table
  )

For an integer range, every typed operand must have the same integer
type. Untyped integer constants adopt that type. If no operand fixes a
type, they default to `Int`. Conflicting typed operands and constants
that the resolved type cannot represent are rejected. The implicit `0`
and `1` in `range(end)`, and the implicit `1` in `range(lo, hi)`, have
the resolved type. The loop variable also has that type.

For integer operands, `range(lo, hi, step)` supplies the mathematical
values `lo + k * step` for integers `k >= 0`. A positive step supplies a
value while it is less than `hi`. A negative step supplies a value while
it is greater than `hi`. A zero step, or a step whose direction moves
away from `hi`, supplies no values. The bound is excluded, so
`range(1, 13)` supplies `1` through `12`.

The successor after the final supplied value is not evaluated in the
fixed-width integer type. Integer range progression therefore does not
wrap or raise an overflow failure.

The bounds of a range share one rangeable type and the step has the
corresponding type below. Rangeable types use these default and step
types:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Type], [Default step], [Step type],),
    table.hline(),
    [integer types], [`1`], [the integer type],
    [`Decimal`], [`1.0`], [`Decimal`],
    [`Date`], [`1d`], [`Duration` (whole days)],
    [`Time`], [`1s`], [`Duration`],
    [`DateTime`], [`1s`], [`Duration`],
    [`Duration`], [`1s`], [`Duration`],
  )]
  , kind: table
  )

`Float` and `String` are not rangeable.

```ritchie
for slot in range(open, close, 30m) {   // open, close are DateTime; 30-minute slots
    publish(slot)
}
```

== Arrays
<builtins-arrays>
#idx("count", code: true)
#idx("contains", code: true)
#idx("indexOf", code: true)
#idx("first", code: true)
#idx("last", code: true)
#idx("append", code: true)
#idx("insert", code: true)
#idx("removeAt", code: true)
#idx("concat", code: true)
#idx("reverse", code: true)
#idx("sort", code: true)
#idx("sortBy", code: true)
#idx("fill", code: true)
#idx("map", code: true)
#idx("filter", code: true)
#idx("reduce", code: true)
#idx("find", code: true)
#idx("some", code: true)
#idx("all", code: true)
#idx("sum", code: true)
Array functions read or transform an array. Observers and folds accept
both `T[]` and `T[N]`. A cardinality-preserving transformation accepts
both forms and preserves a fixed cardinality: `reverse`, `sort`, and
`sortBy` return `T[N]` from `T[N]`, and `map` returns `U[N]`.
`append`, `insert`, `removeAt`, `concat`, and `filter` accept dynamic
arrays only because their result count may differ. Slice a fixed array
first, as `xs[:]`, when a dynamic input is wanted.

A transforming function returns a fresh array, which you reassign to grow
or shrink your own (#secref(<builtins-the-built-in-set>, cap: true)); it does not mutate its input. The
functions that take an operation accept a closure or function value
(#link(<chap-expressions>)[Expressions]).

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Function], [Returns], [Notes],),
    table.hline(),
    [`count(xs)`], [`Int`], [element count],
    [`contains(xs, v)`], [`Bool`], [membership by `==`],
    [`indexOf(xs, v)`], [`Int?`], [first position, `none` if absent],
    [`first(xs)`, `last(xs)`], [`T?` for `T` or `T?` elements],
    [`none` when empty],
    [`append(xs, v)`], [`T[]`], [`v` added at the end],
    [`insert(xs, i, v)`], [`T[]`], [`v` becomes element `i`;
    `OutOfRange!` unless `0 <= i <= count(xs)`],
    [`removeAt(xs, i)`], [`T[]`], [removes element `i` without reordering;
    `OutOfRange!` unless `0 <= i < count(xs)`],
    [`concat(xs, ys)`], [`T[]`], [`xs` followed by `ys`],
    [`reverse(xs)`], [`T[]` or `T[N]`], [reversed order; input form
    preserved],
    [`sort(xs)`], [`T[]` or `T[N]`], [`T` ordered; stable, ascending;
    input form preserved],
    [`sortBy(xs, key)`], [`T[]` or `T[N]`], [`key: (T) -> ordered`\;
    stable, ascending; input form preserved],
    [`map(xs, fn)`], [`U[]` or `U[N]`], [`fn: (T) -> U`; input form
    preserved],
    [`filter(xs, pred)`], [`T[]`], [`pred: (T) -> Bool`],
    [`reduce(xs, init, fn)`], [`U`], [left fold, `fn: (U, T) -> U`],
    [`find(xs, pred)`], [`T?`], [first match, `none` if none],
    [`some(xs, pred)`, `all(xs, pred)`], [`Bool`], [],
    [`sum(xs)`], [`T`], [`T` numeric; an empty array sums to zero],
    [`fill(v, n)`], [`T[]` or `T[N]`], [fresh array containing `n`
    owned copies of `v`; result context selects the form],
  )]
  , kind: table
  )

`first` and `last` return the element at the corresponding end, including
`none`. An empty array produces `none`. For non-nullable element type `T`,
the result type is `T?`; for nullable element type `T?`, the result type
remains `T?`.

`count` counts every element, including elements whose value is `none`.

For `insert`, elements before `i` retain their positions. The elements 
at `i` and thereafter, if any, move one position higher. Insertion at `count(xs)` is valid
and has the same result as `append(xs, v)`. For `removeAt`, elements before
`i` retain their positions and later elements move one position lower.

The `sort` function sorts in ascending order. It can be combined with
`reverse` for descending.

For Float elements, every NaN precedes every non-NaN, non-NaN values use
numeric ascending order, and negative and positive zero are equivalent for sorting.
Stability therefore preserves the relative order of signed zeros and of NaNs.
This sorting rule does not change Float equality or the comparison operators.

`fill(v, n)` evaluates `v` then `n`, each once, and returns a fresh array whose
`n` elements are independent owned copies of the completed value `v`. A
required `T[N]` result requires `n` to be a non-negative constant equal to
`N`; mismatch or a nonconstant count is a compile error. A required `T[]`
result is dynamic even when `n` is constant. With no fixed result context,
the dynamic mapping is selected. A negative constant count is a compile error;
a negative runtime count raises
`OutOfRange!`. Host allocation policy and resource exhaustion are not language
observations.

```ritchie
LineItem[] dueLines = filter(order.lines, l => l.balance > 0.00)
Decimal owed = sum(map(dueLines, l => l.balance))
LineItem? biggest = last(sortBy(order.lines, l => l.amount))
```

== Dicts
<builtins-dicts>
#idx("has", code: true)
#idx("get", code: true)
#idx("keys", code: true)
#idx("values", code: true)
#idx("remove", code: true)
#idx("zip", code: true)
#idx("unzip", code: true)
#figure(
  align(center)[#table(
    columns: 3,
    table.header([Function], [Returns], [Meaning]),
    [`count(d)`], [`Int`], [entry count],
    [`has(d, k)`], [`Bool`], [key membership, including stored `none`],
    [`d.get(k)`], [`V?`], [independent value, or `none` if missing],
    [`keys(d)`], [`K[]`], [independent keys],
    [`values(d)`], [`V[]`], [independent values],
    [`remove(d, k)`], [`[K: V]`], [independent dict without `k`;
      unchanged contents if absent],
    [`zip(keys, values)`], [`[K: V]`], [corresponding array entries],
    [`unzip(d)`], [`(K[], V[])`], [paired independent arrays],
  )], kind: table,
)

The displayed names are parameter names. `get` is a dictionary method;
the other forms are free built-ins. For nullable `V`, `get` retains one
nullable layer: missing and stored `none` are distinguished by `has`.

Each `zip` input independently accepts a fixed or dynamic array;
expected `[K: V]` supplies their element contexts. Unequal actual lengths
raise `LengthMismatch!`. Equal positions are processed in increasing
index order, with the last value for an equal key winning. Two empty
inputs produce an empty dictionary.

Array results are dynamic, subject to ordinary constant-declaration
cardinality inference. Enumeration order is unspecified. Separate
`keys` and `values` calls do not align; one `unzip` returns arrays of
length `count(d)` whose corresponding positions name the same entry.
These are ordinary multiple results. `count`, `has`, `get`, `remove`,
and `zip` are constant-capable under #secref(<builtins-the-built-in-set>). `keys`, `values`, and
`unzip` have only their runtime form, because a constant result would
fix an element order the language leaves unspecified. Only `zip` has an
own-operation error; operand failures follow the general rules.

== String and byte arrays
<builtins-string-and-byte-arrays>
#idx("isEmpty", code: true)
#idx("startsWith", code: true)
#idx("endsWith", code: true)
#idx("toUpper", code: true)
#idx("toLower", code: true)
#idx("trim", code: true)
#idx("replace", code: true)
#idx("split", code: true)
#idx("join", code: true)
Operations shared by multiple sequence types are free built-ins. Their
`String` and `Byte[]` forms have the same name and argument order; both
arguments of a two-sequence operation must have the same type. Counts
and positions use bytes.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Function], [Returns], [Notes],),
    table.hline(),
    [`count(value)`], [`Int`], [byte count],
    [`isEmpty(value)`], [`Bool`], [`count(value) == 0`],
    [`contains(value, part)`], [`Bool`], [`part` occurs in `value`],
    [`startsWith(value, prefix)`], [`Bool`], [prefix test],
    [`endsWith(value, suffix)`], [`Bool`], [suffix test],
    [`indexOf(value, part)`], [`(Int, Bool)`], [first byte position and
    `true`; `(0, false)` if absent],
  )]
  , kind: table
  )

The empty sequence is contained in, starts, and ends every value.
`indexOf(value, empty)` returns `(0, true)`. String matches begin at a
Unicode-scalar boundary because both operands are valid UTF-8.

String-only transformations return a new valid `String`:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Function], [Returns], [Notes],),
    table.hline(),
    [`toUpper(s)`, `toLower(s)`], [`String`], [simple Unicode case
    mapping, no locale],
    [`trim(s)`], [`String`], [leading and trailing Unicode whitespace
    removed],
    [`replace(s, old, new)`], [`String`], [every non-overlapping
    occurrence of `old`],
    [`split(s, sep)`], [`String[]`], [pieces around each `sep`],
    [`join(parts, sep)`], [`String`], [`parts` joined with `sep`],
  )]
  , kind: table
  )

Case conversion uses locale-independent simple Unicode mappings: each
input scalar maps to one output scalar. `trim` removes Unicode whitespace
from both ends.

`replace` chooses non-overlapping occurrences from left to right. An empty
`old` matches at the beginning and after each scalar. `split` keeps
empty pieces for a non-empty separator; an empty separator produces one
piece per scalar, or an empty array for empty input.

`join` places its separator only between elements. An empty array joins to
the empty String and a one-element array to that element. For every `s` and
`sep`, `join(split(s, sep), sep) == s`.

Arguments follow ordinary call evaluation. Results remain valid UTF-8. A
result size outside non-negative `Int` raises `OutOfRange!`.

Parsing text into a number or a `UUId` is a target-type conversion
(`Int.fromString`, raising `Malformed!`\; #link(<chap-types>)[Types]),
not a string function.

```ritchie
String[] parts = split("SKU-12-RED", "-")   // ["SKU", "12", "RED"]
String code = join(parts, "/")              // "SKU/12/RED"
Bool isRed = endsWith("SKU-12-RED", "RED")
```

== Numbers
<builtins-numbers>
#idx("abs", code: true)
#idx("min", code: true)
#idx("max", code: true)
#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Function], [Returns], [Notes],),
    table.hline(),
    [`abs(x)`], [same numeric type], [constant result is checked; runtime
    integer negation wraps; `Float` follows IEEE-754; `Decimal` is
    infallible],
    [`min(x, ...)`, `max(x, ...)`], [`T`], [the least or greatest of one
    or more ordered values],
  )]
  , kind: table
  )

`abs` takes exactly one numeric argument and returns that type. `min`
and `max` take one or more arguments. All arguments must be of the
same type, which is also the return type. Integer arguments of mixed
widths convert to the widest argument type, which also becomes the
return type.

A call to the predefined `abs`, `min`, or `max` is a constant expression
exactly when all its arguments are constant expressions. A shadowing
declaration retains ordinary runtime-call behavior. Constant results follow
the range and finite-value rules of their type.

Decimal `min` and `max` use numerical order. When two operands are numerically
equal and positive, `min` selects the operand with the lesser exponent and
`max` the operand with the greater exponent. For negative operands the choices
reverse. Decimal zero follows the positive rule. More than two arguments apply
this binary rule from left to right.

For a runtime integer value, `abs` uses that integer type's negation, so the
absolute value of its minimum value is the same minimum value. For `Float`,
`abs` clears the sign, including from negative zero and negative infinity.
`NaN` remains `NaN`.

For Decimal, `abs` is infallible, retains the operand exponent, and returns
the non-negative value of the operand. Decimal zero remains non-negative and
retains its quantum.

For `Float`, `min` and `max` propagate NaN from any argument. When the
selected result is zero, `min` returns negative zero if any zero argument is
negative, and `max` returns positive zero if any zero argument is positive.
Infinities participate as numeric extremes. Arguments are evaluated once,
from left to right.

`min` and `max` accept any ordered type, including strings and the
temporal types. Rounding, powers, and roots are standard-library
functions, not built-ins.

=== Float display
A `Float` is displayed as the shortest decimal representation that recovers
the same binary64 value. Scientific notation is used when the decimal
exponent is less than `-4` or at least `6`. It uses lowercase `e`, an
explicit exponent sign, and at least two exponent digits. Special values
display as `NaN`, `+Inf`, and `-Inf`. Negative zero displays as `-0`.
