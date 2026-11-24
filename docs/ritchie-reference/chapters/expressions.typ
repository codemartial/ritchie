#import "../prelude.typ": horizontalrule, idx, secref

= Expressions
<chap-expressions>
Every expression form, what it evaluates to, and how the forms compose.
Precedence and associativity for the operators here are tabulated
in #link(<chap-precedence>)[Precedence].

#horizontalrule

== What an expression is
<expressions-what-an-expression-is>
#idx("expression")
#idx("arity")
#idx("result sequence")
An expression produces a #strong[result sequence] with an
#emph[arity]. Most forms produce exactly one result, a value of a
single type (#link(<chap-types>)[Types]). A call produces one result
per entry in its callee's return list. Each position an expression may
occupy determines the arities it accepts. A declaration initializer, a
condition, a call argument, a `return` value, a `match` subject, and
every operand require arity one. An expression statement accepts any
arity and discards the results. The consumers of arity greater than
one are the multi-variable declaration and the return statement that forwards a
call with the same return list
(#link(<chap-statements>)[Statements]). Multiple results are not a
tuple: no value holds them, they do not spread or flatten into an
argument list, and they do not pack into a collection. A method call's
effect on its receiver (#link(<chap-types>)[Types]) is not part of its
result sequence.

Operands evaluate left to right, in written order. Most operators
evaluate all their operands before combining them; the exceptions are
`and` and `or`, which short-circuit -- the right operand is evaluated
only when the left does not settle the result -- and the ternary
`c ? a : b`, which evaluates only the selected arm. For a call, the callee
or receiver is established once, explicit arguments evaluate once in written
left-to-right order including named arguments, omitted defaults are supplied,
and then the body runs.

Assignment is admitted only at statement level (#secref(<expressions-assignment>, cap: true)). A brace
block carries no value, including when it has a `handle` suffix
(#secref(<expressions-parenthesised-and-block-expressions>, <expressions-handle-suffix>, range: true, cap: true)).

#horizontalrule

== Literal expressions
<expressions-literal-expressions>
A literal denotes a constant value; its spelling is in
#link(<chap-lexical>)[Lexical], its type here. Numeric literals are
context-typed: an integer literal takes the integer type its context
requires and defaults to `Int`\; a non-negative integer literal may
instead take `Byte` from an explicit `Byte` context. A fractional
literal is `Float`, or `Decimal` where the context type is `Decimal`
(#link(<chap-types>)[Types]).
An integer literal does not acquire Decimal type from context.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Literal], [Example], [Type],),
    table.hline(),
    [integer], [`42`, `0xff`, `1_000`], [context int, default `Int`],
    [fractional], [`3.14`, `6e2`], [`Float`, or `Decimal` in context],
    [string], [`"hi\n"`], [`String`],
    [raw string], [#raw("`c:\\re`")], [`String`],
    [byte array], [`b"A\x00\xff"`], [`Byte[]`],
    [raw byte array], [#raw("b`A\\x00`")], [`Byte[]`],
    [f-string], [`f"x={n}"`], [`String`],
    [boolean], [`true`, `false`], [`Bool`],
    [duration], [`5s`, `500ms`], [`Duration`],
    [date / time], [`d2026-05-31`, `t17:50:00`], [`Date` / `Time`],
    [datetime], [`2026-05-31T17:50:00Z`], [`DateTime`],
    [absent], [`none`], [the `T?` of its context],
  )]
  , kind: table
  )

`none` is the absent value; it takes the nullable type of its context
and may not stand where no `T?` is expected
(#link(<chap-types>)[Types]).

An f-string interpolates each `{ ... }` slot. A slot holds an expression
restricted to ternary level: no top-level assignment and no `handle`
suffix. Each slot is formatted into the string, left to right.

```ritchie
String s = f"{user.name} has {count} items"
```

Each value has the following default display. An interpolation slot uses
the standalone form except where a compound value recursively requests
the embedded form.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Type], [Default display],),
    table.hline(),
    [signed integers], [canonical decimal, with a leading minus sign only
    for a negative value],
    [`Byte`], [unsigned canonical decimal],
    [`BitSet`], [`0x` followed by exactly 16 lowercase hexadecimal digits],
    [`Float`], [the shortest round-trip decimal and special forms defined
    in #secref(<builtins-numbers>)],
    [`Bool`], [`true` or `false`],
    [`String`], [its contents when standalone; a double-quoted escaped
    Ritchie String literal when embedded],
    [`Byte[]`], [a quoted `b"..."` literal that recreates the same bytes,
    using the byte escape rules below],
    [other arrays], [`[element, element]`, using each element's embedded
    display recursively; dynamic and fixed arrays use the same form],
    [`Decimal`], [the cohort-preserving decimal form defined below],
    [`Date`], [`YYYY-MM-DD`],
    [`Time`], [`HH:MM:SS` followed, when needed, by a fractional part of
    up to nine digits with trailing zeroes removed],
    [`DateTime`], [the DateTime literal form, using `Z` for UTC or the
    carried numeric offset and the Time fractional rule],
    [`Duration`], [a leading minus sign when negative followed by a
    canonical descending decomposition using `d`, `h`, `m`, `s`, `ms`,
    `us`, and `ns`; zero displays as `0s`],
    [`UUId`], [canonical lowercase hyphenated hexadecimal],
    [`r::Random`], [the construction-provenance form defined in
    #secref(<stdlib-random>)],
    [nullable], [a present value uses its contained value's default
        display in the same standalone or embedded context. An absent value
    displays as `none`],
    [struct], [the recursive form defined in
    #secref(<types-struct-types>)],
    [`ErrorStep`, `ErrorType`, `ErrorField`, `ErrorLocation`], [the struct
    form defined in #secref(<errors-the-error-value>)],
  )]
  , kind: table
  )

Decimal display uses the General Decimal Arithmetic `to-scientific-string`
form and preserves quantum. It uses ordinary notation when the quantum
exponent is at most zero and the adjusted exponent is at least `-6`;
otherwise it uses one leading digit, an optional decimal point, uppercase
`E`, and an explicitly signed adjusted exponent. It omits a leading plus sign.

Printable byte-array ASCII appears directly except quote and backslash;
newline, tab, carriage return, backslash, and quote use their short
escapes, and every other byte, including null, uses lowercase two-digit
`\xhh`. A formatted result whose byte count cannot be represented by
`Int` raises `OutOfRange!` before producing a String.

The exceptional byte-literal display belongs only to dynamic `Byte[]`.
A fixed `Byte[N]` uses the ordinary bracketed array form with numeric
Byte elements. Every empty general array, dynamic or fixed, displays as
`[]`.

#horizontalrule

== Name references
<expressions-name-references>
A bare name evaluates to the value of the local name it refers to; its type is the
name's declared type (#link(<chap-types>)[Types]).

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Refers to],),
    table.hline(),
    [`count`], [the nearest in-scope declaration of that name (a variable)],
    [`MAX_SIZE`], [a constant, module-level or local],
    [`self`], [the receiver of the enclosing method],
    [`apply`], [a declared function, as a value of its function type, when no variable in scope has the name],
  )]
  , kind: table
  )

`self` is in scope only inside a method body and names the instance the
method runs on; `self.field` reads one of its fields. A name with no
declaration in scope is a compile error.

A module name is not a value expression. It appears only to the left of
`::` in a module-qualified name, such as `bank::Account` or
`shop::@Checkout`. An ordinary variable with the same name is selected by a
bare use or by following `.`, while `name::` selects the module name.

#horizontalrule

== Operators
<expressions-operators>
#idx("operator")
Binary operators require both operands to share a type after permitted
implicit conversion (#link(<chap-types>)[Types]). When integer
operands have different widths, the narrower converts to the wider
before the operation. Arithmetic then produces the wider type. No
conversion propagates backward into an already grouped
subexpression. The one exception to the shared-type requirement is
temporal arithmetic, whose admitted signatures are tabulated in
#link(<chap-temporal>)[Temporal Types]. Precedence and associativity are
in #link(<chap-precedence>)[Precedence].

#figure(
  align(center)[#table(
    columns: 4,
    align: (auto,auto,auto,auto,),
    table.header([Group], [Operators], [Operands], [Result],),
    table.hline(),
    [arithmetic], [`+ - * / %`], [one numeric type], [that type],
    [comparison], [`== !=`], [one type], [`Bool`],
    [ordering], [`< > <= >=`], [one ordered type], [`Bool`],
    [logical], [`and or`], [`Bool`], [`Bool`],
  )]
  , kind: table
  )

`%` is the remainder. Between integers `/` truncates toward zero; on
`Float` and `Decimal` it is true division. `==` and `!=` apply to any
single comparable type, including `Bool`, and compare by that type's
identity rule. The
ordering operators require an ordered type (numbers, `Byte`, `String`,
`Byte[]`, and the temporal types). `String` defines no arithmetic
operator. The `and` and `or` operators use short-circuit
evaluation wherever they appear.

=== Arithmetic edges
- Between integers, `(a / b) * b + a % b == a` holds for every nonzero
  divisor, so the remainder takes the dividend's sign: `-7 % 3` is
  `-1`.
- Integer arithmetic, division included, wraps modulo the operand
  type's width.
- A zero integer divisor in a constant expression is a compile error.
  At runtime, a zero integer divisor raises `DivisionByZero!`.
- Decimal binary arithmetic requires two Decimal operands and returns Decimal.
  Addition and subtraction prefer the lesser operand exponent;
  multiplication prefers the sum of operand exponents; division prefers the
  left exponent minus the right exponent; and remainder prefers the lesser
  operand exponent. An exact division may extend toward lower exponents. Unary
  `-` retains the operand exponent. An exact result retains the preferred
  exponent where representable.
- Decimal `%` computes `q = trunc(x / y)` in exact arithmetic and
  `r = x - y*q`. A nonzero remainder has the dividend's sign. The operation
  applies across the full Decimal range even when the integer part of the
  mathematical quotient requires more than 34 decimal digits.
- A Decimal result above the maximum finite magnitude is a compile error in a
  constant expression and raises `OutOfRange!` at runtime. A zero Decimal
  divisor under `/` or `%` is a compile error in a constant expression and
  raises `DivisionByZero!` at runtime. Rounded, subnormal, clamped, and
  underflow results are values, not errors.
- Decimal underflow means an inexact result that is also subnormal. It can
  produce a nonzero value or zero and has no inspection operation.
- Runtime `Float` arithmetic, equality, and ordering follow IEEE
  binary64, so NaN is unequal to every value, itself included. A zero
  divisor under constant `Float` `/` or `%` is a compile error.
- `Float` `%` is the truncating remainder, which IEEE does not define:
  a finite dividend with an infinite divisor yields the dividend, and
  a zero divisor or any other non-finite operand yields NaN.

The unary operators:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Operator], [Operand], [Result],),
    table.hline(),
    [`-`], [numeric or `Duration`], [negation],
    [`not`], [`Bool`], [negation],
  )]
  , kind: table
  )

`not a and b` is `(not a) and b`\; `-a + b` is `(-a) + b`.
Boolean values support `==` and `!=`; they do not support ordering.

=== Chained comparison
#idx("chained comparison")
Two comparison operators may join three operands, `x OP y OP z`, meaning
`(x OP y) and (y OP z)` with `y` evaluated once:

```ritchie
0 <= i < n              // (0 <= i) and (i < n)
lo <= x <= hi
a == b == c             // (a == b) and (b == c)
```

The two operators must not contradict (see below), and a chain is only
accepted up to three operands:

- both ordering of one direction -- ascending `<`/`<=` or descending
  `>`/`>=`, strictness free to differ. `a < b > c` is rejected.
- or the same equality operator, `==`/`==` or `!=`/`!=`. `a == b != c`
  is rejected.

Ordering and equality do not mix in one chain. A longer conjunction is
written with `and`. Parenthesising opts out: `(a < b) < c` is read
strictly, left to right.

#horizontalrule

== Leading-dot member
<expressions-leading-dot-member>
#idx("leading-dot member")
A leading dot names a member of a type that is not written at the use
site: `.member` or `.member(args)`. It is allowed as the initializer
of an explicitly-typed declaration -- a variable, const, struct field
default, or parameter default -- where it resolves a type-level member
against the type written immediately to its left.

```ritchie
Suit s = .HEARTS                // same as:  Suit s = Suit.HEARTS
Int8 b = .fromInt(n)            // same as:  Int8 b = Int8.fromInt(n)
User u = .new("bob", 30)        // same as:  User u = User.new("bob", 30)
```

It resolves an enum member, a constructor, or a conversion. The type comes
only from the declaration immediately to its left. It is not inferred from a
call parameter, a return signature, or another variable. The leading
dot may not appear as a call argument, a return value, an assignment to
an existing variable, or nested in a larger expression.

#horizontalrule

== Field access and method calls
<expressions-field-access-and-method-calls>
#idx("field access")
#idx("method call")
A dot after a value expression or a type name selects one of its members.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Is],),
    table.hline(),
    [`expr.field`], [field access, value of the field's type],
    [`expr.method(args)`], [a method call with `expr` as receiver],
  )]
  , kind: table
  )

Fields are private to the declaring module
(#link(<chap-types>)[Types]).  Outside it, a value is reached only
through the methods its module exports. Method or field access can be
chained left to right: `a.b.c()`, `xs.first().name`.

Fields and methods form one member namespace. Selection first resolves
the member, which may be a field or a method. A following `(...)`
suffix denotes a method and requires the selected value to be
callable.

Value-member names are `VARIABLE` identifiers, so a value has no member named
with a type, constant, or sigilled-type token. Thus `account.balance` is value
member access, `bank::Account` is a module-qualified type, and `x.Account` is
ill-formed when `x` is a value.

Postfix selection, calling, indexing, and slicing compose from left to right.
The carrier of each suffix must have arity one and is evaluated once.

#horizontalrule

== Function calls
<expressions-function-calls>
A function call applies a function to an argument list (#secref(<expressions-argument-lists>, cap: true)):

```ritchie
f(x, y)                    // function in the current module
bank::f(x, y)              // the same function, inside module bank
Int8.fromInt(n)            // function associated with a type
accounts::User.new("bob")  // type member through module qualification
centre.distanceTo(point)   // instance method
```

A call applies a callable to an argument list. A free function is called as
`f(...)` or, within its declaring module, `module::f(...)`. A type-level method
is called on its type, and an instance method on its receiver. A function-typed
value is called by following it with `(...)`. Module qualification does not
change visibility.

Constructors and conversions are type-level methods. By convention,
`User.new(...)` constructs a `User`, while `Int8.fromInt(n)` converts to
`Int8` and may raise `OutOfRange!` (#link(<chap-types>)[Types]).

#horizontalrule

== Indexing and slicing
<expressions-indexing-and-slicing>
Brackets index or slice `String`, an array, or a dict. Sequence
positions are zero-based `Int` values.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Form], [On], [Yields],),
    table.hline(),
    [`s[lo:hi]`], [`String`], [a half-open byte slice, over
    `InvalidStringBoundary!` when an in-range bound splits UTF-8],
    [`arr[i]`], [dynamic or fixed array], [the element, over
    `OutOfRange!` if `i` is out of bounds],
    [`dict[key]`], [dict], [`V`, over `MissingKey!` if absent],
    [`arr[lo:hi]`], [dynamic or fixed array], [an independent dynamic
    sub-array `T[]`],
  )]
  , kind: table
  )

An array index is `Int`; a dict index has its key type. A negative index,
or an index at or beyond the element count, is invalid. An invalid index
that reaches execution raises `OutOfRange!`. Dictionary access follows
#secref(<types-collection-types>)\; optional lookup uses `get`
(#secref(<builtins-dicts>)).

Slice bounds are optional -- `xs[lo:]`, `xs[:hi]`, `xs[:]` -- with the
low bound inclusive and the high bound exclusive. Omitted bounds mean
zero and `count(xs)`. A negative bound, an inverted range, or a bound
beyond the byte or element count is invalid An invalid bound that reaches
execution raises `OutOfRange!`.

String positions and bounds count UTF-8 bytes. A String slice first
checks its numeric bounds, then requires each bound to lie between
encoded Unicode scalars. Failure of the second check raises
`InvalidStringBoundary!`. `s.isRuneBoundary(i)` reports whether a byte
position is such a boundary. Zero and `count(s)` are boundaries, and a
position outside that closed interval returns `false`. A single-index
String expression is rejected, so obtaining one byte from text requires
an explicit conversion to `Byte[]`.

#horizontalrule

== Construction, creation, and collection literals
<expressions-construction-creation-and-collection-literals>
A type-named brace literal constructs a local value. Entity creation is
the `create` expression.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Literal], [Constructs],),
    table.hline(),
    [`Point { 1, 2 }`], [a struct],
    [`create $Account { ... }`], [an entity
    (#link(<chap-entities>)[Entities])],
    [`Declined! { "x" }`], [an error],
  )]
  , kind: table
  )

Initializers choose one form: all-positional, in declaration order, or all-named
(`field: value`, any order). A field with a default may be
omitted (#link(<chap-types>)[Types]). A brace literal is available only
inside the declaring module; elsewhere a value comes from an exported
constructor (#secref(<expressions-function-calls>, cap: true)). Raw entity creation is likewise module-private
(#secref(<entities-creation>)). An error literal drops its
braces when it carries no fields: `Timeout!` is a complete error value.
A generic struct literal names the applied type:
`Pair<Int, String> { 1, "first" }` (#secref(<types-generic-types>)).

Collection literals use brackets:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Literal], [Is],),
    table.hline(),
    [`[a, b, c]`], [an array],
    [`[]`], [an empty array],
    [`[k1: v1, k2: v2]`], [a dict],
    [`[:]`], [an empty dict],
  )]
  , kind: table
  )

The first element's terminator separates the two: a `:` makes a dict, a
`,` or `]` makes an array. `[]` is the empty array, `[:]` the empty
dict.

An array literal may end with one optional trailing comma. Its entries
evaluate exactly once from left to right, and failure stops the entries
that follow. Every entry must match one resolved element type `T`. A
nonempty literal outside a fixed-array context produces `T[]`; a
required `T[]` context checks the same dynamic form, while a required
`T[N]` context requires exactly `N` entries. The literal is a constant
exactly when all its entries are constant.

The empty literal has no entry from which to obtain an element type, so
it requires an array context. It constructs `T[]` or `T[0]` according
to that context and is rejected under `T[N]` for every positive `N`.

#horizontalrule

== Entity lookup
<expressions-entity-lookup>
#idx("lookup")
`$Type[key]` looks up an entity by its id:

```ritchie
$Account[ownerId]
```

The lookup is a distributed fetch and can fail -- no such instance, or
the network -- so it yields the entity over an `Error!`, handled at the
lookup site. A key that matches nothing is a failure rather than a new
instance. Lookup is legal only in the entity's
declaring module (#link(<chap-entities>)[Entities]).

#horizontalrule

== Service call
<expressions-service-call>
`@Service.method(args)` calls a method on a service:

```ritchie
@Mailer.send(message)
```

The call runs over the network and yields the method's result, over an
`Error!` where it can fail (#link(<chap-services>)[Services]). Arguments
follow #secref(<expressions-argument-lists>, cap: true).

#horizontalrule

== Closures
<expressions-closures>
#idx("closure")
A closure is an inline function value written with `=>`. It captures the
variables it uses by value, copying them where it is created
(#link(<chap-types>)[Types]).

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Form], [Body], [Returns],),
    table.hline(),
    [`x => x + 1`], [one expression], [the expression],
    [`(x, y) => x + y`], [one expression], [the expression],
    [`() => f()`], [no parameters], [the expression],
    [`x => { ... return r }`], [a block], [by explicit `return`],
  )]
  , kind: table
  )

A block body returns nothing unless it runs `return`. Parameter types
are written, except where the closure is the value of a variable or
parameter whose type is a function type -- there the parameter types
come from that type:

```ritchie
(Int) -> Int inc = x => x + 1       // x is Int, from the left
map(xs, (Int n) => n * 2)           // written, no pin here
```

#horizontalrule

== Ternary, coalescing, and narrowing
<expressions-ternary-coalescing-and-narrowing>
#idx("ternary")
The ternary chooses between two expressions on a condition -- a `Bool`,
or a nullable tested for presence (below):

```ritchie
String s = ok ? "yes" : "no"
```

The condition and both arms require arity one. A required result type
contextualises both arms. Without one, equal arm types remain; differing
integer types use ordinary widening. An untyped integer literal adopts the
resolved type when representable, and literals default to `Int` if neither arm
fixes a type. Every other unequal pair is rejected. Both arms are resolved and
type-checked even though only one executes.

The ternary is runtime-only. It is not admitted in a module or local constant,
a field default, or a parameter default, including when all three operands are
otherwise constant.

A ternary cannot directly provide another ternary's condition or arm after
parentheses are removed. Thus `a ? b : (c ? d : e)` is rejected, while
`a ? f(c ? d : e) : g()` is accepted because the outer arm is a call. A
multi-way choice uses `if`/`else` or `match`
(#link(<chap-statements>)[Statements]).

=== Coalescing
#idx("coalescing")
`a ?? b` yields `a` when it is present and `b` when `a` is absent -- the
cheap way to confront a nullable. The left must be nullable (`T?`); a
non-nullable left is rejected, since its default would be dead. `b` is
evaluated only when `a` is absent.

```ritchie
Int n = score ?? 0          // score is Int?; n is Int
```

The result type follows `b`: a `b: T` resolves the absence (result `T`),
a `b: T?` keeps chaining (result `T?`). `b` matches `a`'s base type
exactly, and the integer widening of #secref(<types-primitive-value-types>) does not apply between the two operands. `??` associates right, so
`a ?? b ?? c` is the first present
of `a`, `b`, then `c`. It tests presence, not truth: a present `0`,
`false`, or `""` wins.

```ritchie
label = a ?? b ?? "x"       // first present of a, b, then "x"
```

`??` has lower precedence than arithmetic and higher precedence than comparison
(#link(<chap-precedence>)[Precedence]): `score ?? 0 < limit` is
`(score ?? 0) < limit`, and `m ?? n + 1` is `m ?? (n + 1)`. It is allowed
in f-string slots and as a nested operand generally.

`??` and the ternary may not be direct operands of one another after removing
parentheses -- both contend for `?`. Combine by assigning the intermediate to a
name, put one behind another expression such as a call, or use `if`/`else`
(a statement, which mixes with `??` freely). As siblings -- separate
arguments, fields, or elements -- they are fine; they just do not
compose.

```ritchie
Bool f = a ?? b
... f ? x : y                       // ok: intermediate named
if flag ?? false { ... }            // ok: ?? in an if condition
T r = a ?? (cond ? d : e)           // rejected: a ternary as a ?? operand
```

`x ??= b` is the matching coalescing assignment (#secref(<expressions-assignment>, cap: true)).

=== Narrowing a nullable
A `T?` cannot be used as a `T` until its absent case is confronted
(#link(<chap-types>)[Types]). A condition that tests a nullable narrows
it where it is known present. The recognised tests name the variable
directly:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Condition], [Narrows in],),
    table.hline(),
    [`x`], [the true branch],
    [`x != none`], [the true branch],
    [`x == none`], [the false branch],
  )]
  , kind: table
  )

```ritchie
Int? maybe = ...
Int sure = maybe ? maybe : 0        // in the true arm, maybe is Int
```

A test narrows a #emph[field-access path], not just a bare name. A path
`root.f1...fN` is narrowable when every intermediate is known present --
declared non-nullable, or already narrowed -- and only the terminal `fN`
is `T?`:

```ritchie
Int h = self.left != none ? self.left.height() : 0   // path narrowed in place
```

`self.left.right` is not narrowable on its own, since `self.left` is
itself `Node?`. It becomes narrowable inside `if self.left != none { ... }`.
Array and dictionary accesses (`xs[i]`, `dict[k]`) are not narrowed in
place. Assign them to a name first, which then narrows as a name.

A narrowing holds from the test until an assignment could change that
exact path. An instance-method call on a mutand may change it through `self`
(#link(<chap-types>)[Types]), so both written assignments and calls
count. An assignment to a proper prefix of the path (`self = ...`, or
the call `self.foo()`) ends the narrowing, since it replaces the value
holding the path. A written assignment to the path itself
(`self.left = ...`) ends it too, unless the stored value has a type that
is not nullable, in which case the path is present after the assignment. A
call on the narrowed path itself (`self.left.foo()`) leaves a value of
the path's present type, so it keeps the narrowing, as do
reads, type-level calls, and assignments or calls on a sibling
(`self.right.bar()`). A direct field assignment `self.X = ...`
invalidates only `self.X` and paths under it, not a sibling `self.Y`.

A recognised test also narrows in the right operand of `and`, and the
negated test narrows in the right operand of `or`; deep paths fall out of
this, with no extra rule:

```ritchie
if discount != none and discount > 0.0 { ... }            // discount narrowed on the right
if self.left != none and self.left.right != none { ... }  // self.left.right narrowed
```

When the branch a test selects cannot complete normally -- every path
through it returns, breaks, or continues -- the opposite narrowing
extends to the remainder of the enclosing block, for a name or a field
path alike (`if self.left == none { return }` leaves `self.left` present
after):

```ritchie
if discount == none { return }
apply(order, discount)              // discount is Decimal from here on
```

#horizontalrule

== Assignment
<expressions-assignment>
#idx("assignment")
Assignment updates an existing variable, field, or element:

```ritchie
count = count + 1
user.age = 30
items[i] = x
```

Assignment has the lowest precedence and associates right, so
`a = b = c` assigns `c` to `b`, then that value to `a`. It is admitted
only as a statement or within such a chain, and is rejected as an
operand of another expression.

Compound assignment combines an operation with the update:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Means],),
    table.hline(),
    [`x += y`], [`x = x + y`],
    [`x -= y`], [`x = x - y`],
    [`x *= y`], [`x = x * y`],
    [`x /= y`], [`x = x / y`],
    [`x %= y`], [`x = x % y`],
    [`x ??= y`], [`x = x ?? y`],
  )]
  , kind: table
  )

Compound assignment is a statement only: it may not appear as a
condition, a return value, a declaration initializer, or any other operand.
Its right side is a single expression -- compound forms do not chain.
For arithmetic compound assignment, the target place and its path components
are established once before the right side. After the right side completes,
the final numeric bounds check runs, the current value of that live place is
read, the arithmetic operation is applied, and the result is stored through
the established place.
If the right side mutates that place, arithmetic uses its updated value.
There is no `++` or `--`\; write `x += 1` and `x -= 1`.

`x ??= y` is the coalescing form: it requires a nullable assignable `x`
and assigns `y` only when `x` is absent (#secref(<expressions-ternary-coalescing-and-narrowing>, cap: true)). The target and its
path components are evaluated exactly once.

When `y` is non-nullable, successful completion establishes presence for a
target eligible for narrowing under #secref(<expressions-ternary-coalescing-and-narrowing>, cap: true), subject to that section's
invalidation rules. Indexed targets acquire no subsequent narrowing.

```ritchie
config.retries ??= 3        // set only if unset
```

An inline `handle` protects the assignment statement, including target
evaluation and the right side. Diversion performs no unfinished assignment
stores (#secref(<expressions-handle-suffix>, cap: true)).

```ritchie
total += @Tax.rate(region) handle { Unavailable! => total }   // keeps total on failure
total += @Tax.rate(region) handle { Unavailable! => 0.0 }     // sets total to 0.0 on failure
```

For a single array-element assignment, the mutand base and every place
segment through the final index evaluate once from left to right, then
the right side evaluates. An error while establishing the target stops
before the right side. Numeric bounds are checked only after the right
side succeeds: a right-side error performs no check or store, while a
completed right side followed by an invalid index raises `OutOfRange!`
and performs no store. A valid index stores an independent value of the
element type. An indexed call result or any other non-mutand base is not
an assignment target; a path rooted at a constant remains a constant
reassignment.

=== Multi-target assignment
A list of assignable targets may be stored from a positional value list:

```ritchie
a, b = b, a                       // swap two names
xs[i], xs[j] = xs[j], xs[i]       // swap two elements
self.x, self.y = self.y, self.x   // swap two fields
```

All right-hand values and all left-hand index and key subexpressions are
evaluated, and all required checks complete, before any store. The stores
then run left to right against live state. No store is visible to a read
in the same statement, so
`a, b = b, a` is a real swap with no temporary. The counts must match,
with no coercion, unless a single multi-return call fills the whole list
(`a, b = f()`) as the sole right-hand side. On a shared location the last
store wins. It is plain `=` only -- no multi-target compound form -- and
it does not chain (#link(<chap-statements>)[Statements]).

#horizontalrule

== Parenthesised and block expressions
<expressions-parenthesised-and-block-expressions>
Parentheses group a subexpression to override precedence --
`(a + b) * c` -- changing grouping only, not the value or type.

A brace block `{ ... }` carries no value. A `handle` suffix protects its
statements (#secref(<expressions-handle-suffix>, cap: true)). It does not make the block value-producing.

#horizontalrule

== Handle suffix
<expressions-handle-suffix>
A `handle` suffix protects the preceding statement or, when that statement
has bodies, each statement directly inside them. It does not protect a
header. It is not an operand-level value constructor. Recovery,
arm forms, and control rules are in #secref(<errors-inline-handle>, <errors-fallback-and-nested-handling>, range: true).

#horizontalrule

== Argument lists
<expressions-argument-lists>
A call's arguments are all-positional or all-named:

```ritchie
greet("Alice", 3)                   // positional, in parameter order
greet(name: "Alice", times: 3)      // named, any order
```

Positional arguments match parameters in order; named arguments pair each
with its parameter name, in any order. A parameter with a default may be
omitted (#secref(<declarations-functions-and-methods>)). The same
all-or-nothing rule governs struct and entity initializers (#secref(<expressions-construction-creation-and-collection-literals>, cap: true)).
