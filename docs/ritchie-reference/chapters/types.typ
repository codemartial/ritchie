#import "../prelude.typ": horizontalrule, idx, secref

= Type System
<chap-types>
What types Ritchie has, what they mean, and how they relate. Every type
belongs to one of eight archetypes, set apart by a few semantic axes the
chapter takes up first. The rest treats each type form in turn.

Declarations are type-first: a type precedes the name it applies to,
with no separator (`Int8 count`, `String host`, `add(Int a, Int b)`).
The case convention keeps the two apart -- a type is uppercase-led or
sigil-prefixed, a name is lowercase.

Ritchie initializes every variable explicitly. The compiler accepts a
read only when every path reaching it has initialized the variable. A
handler arm that completes a failed store without supplying values
leaves the variable without a value on its path
(#secref(<errors-block-and-loop-handling>)).
Struct and entity construction establishes every field.

A variable's type is fixed for its scope. Once `Int n` is introduced,
`n` stays an `Int` there and may be reassigned only to `Int` values. A
nested scope may #emph[shadow] the name with a fresh variable of any
type; that is a new, separate name, not a mutation of the original.

#horizontalrule

== Archetypes
<types-archetypes>
#idx("archetype")
Every Ritchie type belongs to an #strong[archetype]: a family that fixes a
coherent bundle of semantic defaults -- whether it shares observable
mutation, whether it has identity, whether it persists, who owns it,
how it mutates (#secref(<sec-semantic-axes>, cap: true)). The archetype is selected by the type's
written form (letter case and sigil).

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Archetype], [Written as], [In short],),
    table.hline(),
    [value], [built-in name], [immutable primitive (`Int`, `Float`,
    `Bool`, `Byte`, `String`)],
    [struct], [`Type { Type field }`], [local product value, compared by
    fields],
    [enum], [`Type { CONSTANT }`], [closed set of named constants],
    [collection], [`T[]`, `[K: V]`], [array or dict of elements],
    [entity], [`$Type { fields }`], [persistent record with identity],
    [service], [`@Type`], [stateless callable unit],
    [error], [`Type! { fields }`], [failure value carrying optional
    context],
    [function], [`(P) -> R`], [callable; local execution],
  )]
  , kind: table
  )

A plain `Type` name resolves to a value, struct, or enum by its
definition; the sigils `$ @ ! #` and the collection brackets pick the
rest. The semantic axes each archetype sets are in the next section.

#horizontalrule

== Semantic axes
<sec-semantic-axes>
#idx("semantic axes")
An archetype is a bundle of choices across seven axes:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Axis], [Options],),
    table.hline(),
    [Structure], [atom / product / sum / container],
    [Sharing], [immutable value / independent value / reference identity],
    [Identity], [structural / has-id],
    [Persistence], [ephemeral / persistent],
    [Ownership], [local value / logical unit],
    [Mutability], [immutable / owned-mutable / shared-mutable],
    [Scope], [local / remote],
  )]
  , kind: table
  )

How each archetype sets them:

#figure(
  align(center)[#table(
    columns: 8,
    align: (auto,auto,auto,auto,auto,auto,auto,auto,),
    table.header([Archetype], [Structure], [Sharing], [Identity], [Persist], [Ownership], [Mutability], [Scope],),
    table.hline(),
    [value], [atom], [immutable value], [structural], [ephemeral], [local value], [immutable], [local],
    [struct], [product], [independent value], [structural], [ephemeral], [local value], [owned], [local],
    [enum], [sum], [immutable value], [structural], [ephemeral], [local value], [immutable], [local],
    [collection], [container], [independent value], [structural], [either], [follows container], [owned], [local],
    [entity], [product], [reference identity], [has-id], [persistent], [logical unit], [shared], [remote],
    [service], [--], [--], [has-id], [--], [logical unit], [stateless], [remote],
    [error], [sum], [immutable value], [structural], [ephemeral], [local value], [immutable], [local],
    [function], [--], [--], [--], [ephemeral], [local value], [--], [local],
  )]
  , kind: table
  )

The axes that move together explain the family resemblances:

- #strong[struct vs entity] -- identity, sharing, persistence,
  ownership, and mutability all shift: a struct is a local value whose
  later mutations are observed only through the variable that owns it; an
  entity is a shared, persisted record owned by one logical unit and
  referenced by id. Its
  `shared` mutability means mutation is reachable through its public
  methods on the one identity, not that fields are assignable from
  outside -- they are private, and the entity runtime owns each instance's
  writes.
- #strong[enum vs struct] -- only structure differs (sum vs product).
- #strong[value vs struct] -- sharing and mutability differ (immutable
  value vs independent owned value).
- #strong[service vs function] -- same callable idea on a different
  scope axis (remote vs local).
- #strong[collection] -- persistence and ownership follow the container:
  a collection on its own is local and ephemeral, and one held in an
  entity field persists and is owned by the entity unit.

#horizontalrule

== Type equivalence
<types-type-equivalence>
#idx("nominal typing")
#idx("type equivalence")
Two separate questions, kept apart:

- #strong[Equivalence (assignability)] -- may a value of type `A` stand
  where `B` is expected.
- #strong[Value equality (`==`)] -- are two values of the same type
  equal.

#strong[Named types are nominal.] A `Type`, `$Type`, or `Type!` is
identified by its declaring module and declared name. A local declaring
module is identified by its explicit module name in its project. A
declaring module supplied by a publication is identified by the
publication identity and the module name declared by its publisher. A
consumer-side module name or rename changes only the qualifier used in that
project; it does not change type identity. Two distinct publication
identities therefore declare distinct types even when their module names,
declarations, and contents are identical.

Two structs with identical fields but different identities are different
types and do not interchange:

```ritchie
Point { Int x, Int y }
Coord { Int x, Int y }

useCoord(Coord c) -> Int { return c.x }

Point p = Point { 1, 2 }
useCoord(p)        // rejected: Point is not Coord
```

#strong[Anonymous composites are structural.] Arrays, dicts, function
types, and nullable types have no name, so they match by shape: every
`Int[]` is every other `Int[]`\; every `(Int) -> Int` is every other
`(Int) -> Int`. A composite built from named parts still compares its
parts nominally -- `Point[]` and `Coord[]` differ because `Point` and
`Coord` do.

#strong[Value equality] follows each archetype's identity axis: values,
structs, enums, and errors compare structurally (field by field);
entities compare by id (see #link(<chap-entities>)[Entities]). Equality
is defined within a single type after any permitted implicit conversion;
there is no other cross-type comparison.

#strong[Refinement.] The language's one subtype relation: every error
type refines `Error!`, the base error type, one level deep. An `E!`
value stands wherever `Error!` is expected, including an `any` arm's
error variable (#link(<chap-errors>)[Errors]). User types do not refine each
other, not even error types. No other subtyping exists, declared or
structural.

#horizontalrule

== Primitive (value) types
<types-primitive-value-types>
#idx("Int", code: true)
#idx("Int8", code: true)
#idx("Int16", code: true)
#idx("Int32", code: true)
#idx("Int64", code: true)
#idx("Float", code: true)
#idx("Bool", code: true)
#idx("Byte", code: true)
#idx("BitSet", code: true)
#idx("String", code: true)
#idx("value type")
#idx("primitive type")
Primitive scalars are the `value` archetype: immutable values with
structural equality.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Type], [Meaning],),
    table.hline(),
    [`Int8` `Int16` `Int32` `Int64`], [signed two's-complement
    integers],
    [`Int`], [alias for `Int64` -- the default integer],
    [`Float`], [64-bit IEEE-754 floating point],
    [`Bool`], [`true` or `false`],
    [`Byte`], [integral scalar from `0` through `255`, distinct from the
    integer types],
    [`BitSet`], [an unsigned 64-bit bit pattern, distinct from the integer
    types and `Byte`],
    [`String`], [immutable valid UTF-8 text],
    [`Decimal`], [exact base-10 number (see below)],
    [`Date`], [a civil date (year-month-day); no time, no zone],
    [`Time`], [a time of day; no date, no zone],
    [`DateTime`], [an instant with a retained numeric offset],
    [`Duration`], [an adaptive signed physical time span],
    [`UUId`], [opaque 128-bit identifier (see below)],
  )]
  , kind: table
  )

=== Numeric literals and conversion
An #strong[integer literal] takes the integer type required by its
context and defaults to `Int`. It does not take `Float` type from
context. Each literal operand and each constant integer operation is
evaluated exactly. Its result must be representable by the required
integer type; otherwise compilation fails. The context distributes
through arithmetic operators, unary minus, and the constant numeric
built-ins, and a typed operand of a comparison supplies the context of
an untyped literal operand beside it.

A unary minus directly on an integer literal participates in literal
range checking, so `Int8 m = -128` is admitted. Runtime integer
arithmetic uses the signed two's-complement operation of its type and
wraps modulo that type's width. `Int` is identical to `Int64`.

A #strong[fractional literal] (`3.14`, `6e2`) takes `Float` type unless
its context requires `Decimal`. A `Float` literal and each constant
`Float` operation is rounded to IEEE binary64 using round-to-nearest,
ties-to-even. A `Float` constant is finite and does not represent
negative zero. A result that is NaN or infinity is a compile error;
underflow to zero produces positive zero.

Runtime `Float` values include signed zero, infinities, and NaN.
Parentheses determine expression grouping, but do not require a rounded
intermediate value when an implementation uses a fused IEEE operation
with the same grouping.

The signed integer widths are ordered `Int8`, `Int16`, `Int32`,
`Int64`. A value of `IntN` converts implicitly to `IntM` exactly when
`N < M`; `Int` participates as `Int64`. This rule applies wherever a
value is required, including initialization, assignment, arguments,
returns, operators, and built-ins. It does not permit a typed constant
to convert to a narrower type based on its value.

No other implicit numeric coercion holds. Other numeric conversions use
a conversion function owned by the target and named for its source,
`T.fromV(v) -> T` -- there is no cast operator:

```ritchie
Float avg = Float.fromInt(total) / 3.0
Int   n   = Int.fromFloat(avg)
Int8  b   = .fromInt(n)           // target fixed by context; the leading dot drops it
Decimal price = 9.99              // literal adopts Decimal in context
```

Every `fromV` returns its own type `T`. An infallible conversion returns
`T` outright; a fallible one returns `T` over an `Error!` -- the
built-in numeric conversions raise `OutOfRange!` out of
range, which propagates like any failing call. A
type's module supplies the `fromX` conversions into it; where none
exists for a pair, conversion is a compile error. (Leading-dot form:
#link(<chap-expressions>)[Expressions]\; propagation:
#link(<chap-errors>)[Errors]\.)

=== Byte
A non-negative integer literal takes `Byte` type when an explicit
`Byte` context requires it, including a `Byte` operand of a comparison
or of `min` and `max`. Its exact value must be from 0 through 255; an
out-of-range literal is a compile error.

`Byte` values have structural equality and unsigned ordering. A `Byte`
does not convert implicitly to or from another type. `Byte.fromInt(i)`
returns the corresponding value or raises `OutOfRange!` when `i` is
outside 0 through 255. `Int.fromByte(b)` returns the corresponding
`Int`.

=== BitSet
A `BitSet` is a built-in nominal 64-bit bit-pattern value, distinct from every
integer type and from `Byte`.

A nonnegative integer literal takes `BitSet` type when an explicit `BitSet`
context requires it. Its exact value must be from `0` through `2^64 - 1`; an
out-of-range or negative literal is a compile error.

Equality compares all 64 bits exactly. `BitSet` is dictionary-key-capable. It
has no ordering, arithmetic, conversion, indexing, or bitwise operation. Its
default display is `0x` followed by exactly 16 lowercase hexadecimal digits,
including leading zeroes.

=== String and byte arrays
Every `String` value is valid UTF-8.
String equality and ordering compare the UTF-8 bytes, lexicographically
and unsigned. This makes equality exact and ordering independent of
locale or Unicode collation data. Immutable String values may share
storage, but storage identity is not observable.

`Byte[]` may contain any finite byte sequence, including encodings that
are not valid UTF-8. It has structural equality and unsigned
lexicographic ordering.

`Byte[].fromString(s)` returns the UTF-8 encoding of `s`.
`String.fromBytes(bs)` returns the decoded text when `bs` is valid UTF-8,
and raises `Malformed!` otherwise.

=== Decimal
#idx("Decimal", code: true)
A `Decimal` is a finite base-10 number with 34 significant digits and a
retained quantum. It denotes a signed `coefficient times 10^exponent`, where
the coefficient is from zero through `10^34 - 1` and the quantum exponent is
from `-6176` through `6111`. A nonzero value's adjusted exponent is its
quantum exponent plus its coefficient digit count minus one. The adjusted
exponent is at most `6144`; values below `-6143` are subnormal. The smallest
positive nonzero value is `1E-6176`; the largest is
`(10^34 - 1) times 10^6111`.

The quantum distinguishes cohorts such as `1.0` and `1.00`. Decimal has one
zero sign, so every zero is non-negative while retaining its quantum. It has
no NaN, infinity, arithmetic flags, or traps.

Decimal uses round-to-nearest with ties to an even last retained digit. Each
written operator rounds its result before that result participates in another
operation.

A Decimal literal retains its written quantum when representable and rounds
once when it exceeds 34 significant digits. A representable value whose
written exponent is too high is clamped without changing its magnitude.
Overflow after rounding is a compile error. Values below `1E-6176` round to a
subnormal value or zero without a diagnostic.

Decimal equality and ordering compare numerical values. Quantum does not
participate, so values such as `1.0` and `1.00` are equal and neither orders
before the other. Comparison is exact and does not round either operand.

There is no implicit conversion between Decimal and any other numeric type.
`Decimal.fromInt(i)` converts an `Int` exactly and is infallible.
`Decimal.fromFloat(f)` converts the actual IEEE binary64 value to decimal128,
rounding to nearest with ties to even. An exact binary-to-decimal conversion
prefers exponent zero. An inexact conversion uses the least possible exponent.
A NaN or infinity raises `OutOfRange!` because Decimal has no such value.

`Float.fromDecimal(d)` converts the numerical value to IEEE binary64, rounding
to nearest with ties to even. A magnitude above the largest finite Float raises
`OutOfRange!`; subnormal results and underflow to zero succeed. The sign of a
zero produced by underflow is unspecified. Decimal zero converts to positive
Float zero. No other explicit Decimal conversion is defined in this section.

=== Temporal values
`Date`, `Time`, `DateTime`, and `Duration` are distinct primitive values.
Their domains, literals, operations, display, and serialization information
are defined in #link(<chap-temporal>)[Temporal Types].

=== UUId
#idx("UUId", code: true)
An opaque 128-bit identifier with no literal form. Authors compare,
store, and pass `UUId` values. The
runtime mints one for every entity instance as its id, branded with the
entity type: the `id` read from a `$Customer` has type `$Customer#` (a
`#` suffix on the entity type), distinct from every other entity's id
type and from a bare `UUId`, and keys only `$Customer`'s lookup
(#link(<chap-entities>)[Entities]). The brand is a compile-time type
over the one `UUId` value, which round-trips through `String` via
`UUId.fromString(s)` -- raising `Malformed!` on text that does not
parse -- and `String.fromUUId(u)`.

=== Serialized information vs in-memory representation
The language fixes the information a value retains through serialization:
`Decimal` uses decimal128, temporal values retain the information defined in
#secref(<temporal-serialization>), and `UUId` uses its
canonical hyphenated hex string. A protocol mapping owns an encoding not fixed
elsewhere. In-memory representation and physical layout do not affect these language
semantics.

#horizontalrule

== Struct types
<types-struct-types>
#idx("struct")
A struct is a named product of fields (the `struct` archetype,
  #secref(<sec-semantic-axes>, cap: true)). Assigning, passing,
returning, or storing a struct gives the recipient a value whose later
mutations are not observed through the original. Declare it with a
`Type` name and a brace body of type-first fields:

```ritchie
User {
    String name
    Int    age = 18
    Bool   active = true
}
```

Required and defaulted fields may appear in any declaration order.
Every field name is unique. Fields and methods declared by the type occupy one
member namespace, so a field and method cannot share a name.

Fields are module private, thus reachable only inside the declaring
module. A field may carry a default with `=`\; a field without one
must be supplied at construction.

=== Defaults and nullability
A #emph[default] (`= ...`) makes a field omittable at construction. A
#emph[nullable] type (`T?`) lets the field hold no value.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Field], [At construction], [Holds],),
    table.hline(),
    [`Int age`], [required], [a number],
    [`Int age = 18`], [omittable -\> 18], [a number],
    [`Int? age`], [required], [a number, or `none`],
    [`Int? age = none`], [omittable -\> none], [a number, or `none`],
  )]
  , kind: table
  )

Defaults and nullability have no mutual dependency. For example, a
nullable field with no default is still required, so you must either
supply a value or `none`. (Using a `T?` and the `none` literal:
#secref(<types-nullable-types>).)

A field default is a constant expression evaluated during compilation
by the declaration-level constant evaluator. The field's declared type
supplies the expression's context, and names resolve in the struct's
declaring module. `self` and the struct's fields are not in scope. A
struct literal is constant when every supplied field value and every
selected default is constant. Field defaults participate in the
constant dependency graph; an evaluation failure, out-of-range result,
or dependency cycle is a compile error.

=== Construction
Inside the declaring module, a struct literal is `Type {... }`.
Fields in the initializer are either all-positional or all-named:

```ritchie
User { "bob", 30, true }              // positional, in field order
User { name: "bob", age: 30 }         // named; active omitted, takes its default
User { name: "bob" }                  // age and active omitted, take defaults
```

Positional initializers map to fields in declaration order; named
initializers pair each field with `:`, in any order. Either form may
omit only fields that have a default; every other field must be
supplied.

Each supplied positional expression at position `i` initializes the
field at declaration position `i`. Consequently, `Type {}` is only
accepted as an initializer if every field has a default value. A
positional initializer has no placeholder or comma-gap form. Use the
all-named form to omit an earlier defaulted field while supplying a
later field.

Fields are private, so the raw literal only works #emph[inside] the
type's module. From another module construction goes through whatever
type-level creation methods the author declares, by convention
`module::Type.new(...)`:

```ritchie
User u = accounts::User.new("bob", 30)
```

Distinct creation paths take distinct names (`User.fromEmail(...)`), and
there is no overloading. A type with no published methods is module-local.
A published type with no creation method cannot be constructed from
outside its module.

=== Field access
A struct field, `name`, is accessed by using the dot operator as
`.name`.
```ritchie
User u = User { name: "bob" }
String n = u.name
```

This field access is legal only in the declaring module. Other modules
observe a published struct through its methods.

=== Default display
A struct displays its contextual nominal name followed by every field
in declaration order as `Type { field: value, ... }`. A struct with no
fields displays as `Type {}`. Field values use their embedded default
display recursively. A String embedded in a struct uses its
double-quoted escaped literal form. Field visibility does not affect
display.

The formatting module is the lexical module containing the formatting
operation. A struct declared in that module displays as `Type`. A
foreign struct uses the shortest nominal reference available in that
module, such as `accounts::User`. A consumer-side module rename changes
the displayed qualifier. Every nested struct uses the formatting module
of the containing display.

For system-generated rendering associated with a method, the formatting
module is that method's lexical module.

=== Mutation and self
#idx("mutand")
A method's receiver is named `self`. Within an instance method, `self`
is a reference to the receiver: a read of `self` yields the receiver's
current value, and an assignment through `self` changes the receiver in
place. Signatures carry no mutation marking and the compiler performs
no mutability classification of method bodies.

A #emph[mutand] is a variable (#link(<chap-lexical>)[Lexical]), or a
mutand followed by a field selection, an index, or a dict key. A slice
denotes a value, as does every other non-mutand expression. When the
receiver is a mutand, `self` refers to it for the duration of the
call, so `a.reset()` changes `a`. When the receiver is
any other expression, `self` refers to a temporary holding the
expression's value, and the temporary is discarded when the call
completes.

```ritchie
a.reset()                 // self refers to a
a.point.reset()           // self refers to the field a.point
top = stacks[i].pop()     // self refers to the element stacks[i]
makeA().reset()           // self refers to a temporary
CONFIG.enable()           // ditto; a CONSTANT is not a mutand
```

`self` is a reference only within the method body. Returning, passing,
or capturing `self` copies the receiver's value at that point
(`return self` yields a value), so no other name aliases the
receiver.

A call that errors out leaves the receiver holding the value the body
had reached. A caller that needs the prior value copies it before the
call.

A promoted `embed` method takes the embedded field as `self`. Entity
methods act on shared transactional state through an entity reference
(#link(<chap-entities>)[Entities]). Contract methods follow the uniform
rule above (#link(<chap-contracts>)[Contracts]).

=== Embedded fields
#idx("embed", code: true)
A field marked `embed` is an ordinary named field whose type's methods
are promoted onto the struct as forwarding shorthand:

```ritchie
Bar {
    embed Foo foo
    String note
}
```

`bar.m(x)` calls `bar.foo.m(x)`, with `self` naming the `Foo` inside
`m`. Promotion reaches method lookup, not type identity: a `Bar` is not a
`Foo`, the two do not assign to each other, and a promoted method that
returns `Foo` returns the inner `Foo`. It is transitive, and reaches
declared methods only -- a type whose operations are free functions
promotes nothing. A method `Bar` defines itself takes precedence; the same method
promoted from two embedded fields is ambiguous until `Bar` defines it. A
promoted method satisfies a contract when its signature matches with the
self-type read as the field's type (#link(<chap-contracts>)[Contracts]).
`embed` applies to `struct` and `:=` fields, not to an entity or service.

=== Recursion
A struct may not contain itself by value, directly or through other
structs -- the compiler rejects the cycle. Recursion through a nullable,
a dynamic array, or a dict value is legal, and is how trees are written.
Equality and value operations
recurse and terminate, since independent value semantics admit no cyclic
values.

```ritchie
Category { String name, Category? parent }   // legal
Node { Int v, Node next }                    // compile error: by-value cycle
```

Each node of a recursive struct is an owned sub-value: `self.left` is a
#emph[part] of `self`, not a reference to a separate node. A recursive
method's receiver is therefore a field sub-place (`self.left`), not
`self` itself. A deep change is a change to that part of the whole
value, not to a separately identified node.

As a matter of idiomatic guidance, use value-return for structural or
recursive transforms (it needs no field narrowing), and in-place for
shallow changes.

```ritchie
Tree.inserted(Value v) -> Tree {
    if v.before(self.val) {
        Tree nl = self.left != none ? self.left.inserted(v) : Tree.new(v)
        return Tree { nl, self.right, self.val }
    }
    if self.val.before(v) {
        Tree nr = self.right != none ? self.right.inserted(v) : Tree.new(v)
        return Tree { self.left, nr, self.val }
    }
    return self                       // already present, unchanged
}
```

=== Invariants
#idx("invariant")
A struct type may define `__invariants() -> Bool`, a pure method that
reads `self` and writes nothing. The language holds it as a standing
guarantee: every live instance satisfies it. A struct that defines none
carries no such check.

#horizontalrule

== Enum types
<types-enum-types>
#idx("enum")
#idx("backing value")
An enum is the named form of the `sum` archetype: a closed set of named
constants, exactly one of which a value holds. It shares the
`Type { ... }` declaration shape with structs; the body decides which.
All-CONSTANT members make it an enum, lowercase fields make it a struct,
and a body is wholly one or the other. An empty body is a zero-field
struct -- an enum declares at least one member.

```ritchie
Suit { HEARTS, DIAMONDS, CLUBS, SPADES }
```

Within the declaring module, members are reached through the type:

```ritchie
Suit s = Suit.HEARTS
```

Members remain module-local when the enum is published. A type-level
method may return whichever members form the public API.

=== Backing values
A member may pin a constant value with `:`. Either every member is bare
or every member carries a value of one consistent type -- no mixing:

```ritchie
Status { ACTIVE: 1, SUSPENDED: 2, CLOSED: 3 }     // Int-backed
Role   { ADMIN: "admin", MEMBER: "member" }       // String-backed
```

The backing type is whatever the members' literals share, and all
members must agree on it.

=== Semantics
- #strong[No default member.] An enum value is always one of its members
  once set, but there is no implicit default -- a variable must be
  assigned before use.
- #strong[Closed]: an enum value holds one declared member. An ordinary
  `match` may cover any subset of members and may use `any` for the
  remainder. Adding a member does not invalidate an ordinary match.
- #strong[Nominal]: two enums with identical members but different names
  are different types and do not interchange.
- #strong[Immutable value, structural equality]: two enum values are
  equal iff they are the same member.

#horizontalrule

== Entity types
<types-entity-types>
An entity is the `$Type` archetype: a record with stable identity,
persisted and reached by id rather than
copied. It is declared like a struct, with the `$` sigil and a brace
body of fields:

```ritchie
$Account {
    String  owner
    Decimal balance
}
```

Where a struct is a local value, an entity is a persisted
record. The axes that move (#secref(<sec-semantic-axes>)): it has identity, it is
reference-only (every variable names the
one instance), it persists, its methods are reached by remote operation,
and its state is shared-mutable through methods.

=== Identity and lookup
Each instance has a stable id the runtime mints at creation -- not one
of the declared fields. Its type is `$Account#`, the entity's branded id
type (a `#` suffix on the entity type; #secref(<types-primitive-value-types>)). Look an instance up by
id with bracket syntax:

```ritchie
$Account[acctId]              // acctId is an $Account#
```

A lookup is a remote operation, not a local index -- it can fail,
raising `NotFound!` when no instance has that id and `Unavailable!` when
it cannot be performed -- so it is handled at the lookup site. There is
no auto-creation on the read path: a lookup that
finds nothing is a failure, not a fresh instance. Lookup is legal only
in the entity's declaring module. (Lookup results, creation, and the
persistence lifecycle: #link(<chap-entities>)[Entities]\.)

Unlike the entity type and its references, the identity type is public.
Another module may name it as `module::$Account#` and pass, return, store,
serialize, or compare its values without gaining access to `$Account`.

=== State and mutation
Fields are reachable only inside the entity's own methods, through any
reference of the entity's type (#link(<chap-entities>)[Entities]). They
hold serializable forms only -- values, structs, enums, collections of
these, and entity references. An entity's state changes only
through the entity's own methods, which mutate the one identity rather
than produce a new value (`shared` mutability). A variable names that
identity, not a private copy -- two variables holding the same id denote the
same instance. How concurrent observers see in-flight mutations
(consistency, transactions) is #link(<chap-entities>)[Entities].

=== Equality
Entities compare by id, not by fields (#secref(<types-type-equivalence>)): two references are
equal iff they name the same instance. Field-by-field comparison does
not apply.

Type-level only -- persistence durability, transactional method
semantics, lookup-failure handling, and the method and lifecycle rules
live in #link(<chap-entities>)[Entities].

#horizontalrule

== Error types
<types-error-types>
An error is the `Type!` archetype: a typed failure value, optionally
carrying context fields. Its declaration reuses the type-first field
syntax, with a trailing `!`:

```ritchie
Declined! { String reason }
Cancelled!
```

A fieldless error needs no body. An error declaration is distinct from a
struct: it has no methods, operators, embedding, invariants, or custom
matching or display behaviour. Its fields are immutable and read-only
wherever the exact error type is statically known, including outside the
declaring module.

Field names are unique. Required and defaulted fields may be interleaved.
A default is a typed constant resolved in the declaring module. Field
types are immutable owned data types that can be captured independently
and displayed canonically. Function types, services, entities, and error
types are rejected as fields, as are by-value cycles.

Construct one as the bare type or a literal -- `Cancelled!`,
`Declined! { "limit exceeded" }`. A bare `Type!` with no braces is
itself a complete error value only when the declaration has no fields.
A declaration whose fields all have defaults still uses `{}`. A literal
is entirely positional in declaration order or entirely named. Missing,
duplicate, unknown, excess, mixed, and mistyped fields are rejected. Raw
construction is available only in the declaring module, and capture makes
an owned immutable copy. The built-in catalog names
(#secref(<errors-the-built-in-error-catalog>)) are reserved: declaring an
error type with one of those names is a compile error.

=== Base type and history
Every error refines `Error!`, the base error type. A raised error exposes
the read-only `history` described in #secref(<errors-raising-and-propagation>). `history` is the sole reserved built-in member name on an error.
`cause`, `message`, `method`, and `args` have no special error meaning.

A handler matches only the current error type.
History is diagnostic data and does not participate in that match.

=== Errors vs absence
Two non-overlapping mechanisms:

- an #strong[error] (`E!`) marks a control-flow failure -- the
  operation could not produce its result, and the failure travels up the
  call path.
- a #strong[nullable] (`T?`, #secref(<types-nullable-types>)) marks structural absence --
  the value is present and well-formed, it simply holds nothing in that
  slot.

Reach for an error when something went wrong; reach for `T?` when
\"nothing\" is a normal, expected shape of the data.

Type-level only -- raising, propagation, re-expression, history, and the
`handle` forms live in #link(<chap-errors>)[Errors].

#horizontalrule

== Collection types
<types-collection-types>
#idx("array")
#idx("dict")
#idx("collection")
Collections are the `container` archetype: a `T[]` or `T[N]` array of
elements, or an `[K: V]` dict from keys to values. Like structs they are
owned local values with independent mutation semantics. Persistence and
ownership follow the container (#secref(<sec-semantic-axes>)): a collection on its own is
local and ephemeral, and one held in an entity field persists and is
owned by the entity unit.

=== Arrays
#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Meaning],),
    table.hline(),
    [`T[]`], [dynamic array of `T`, grows and shrinks],
    [`T[N]`], [fixed array of exactly `N` elements of `T`],
  )]
  , kind: table
  )

An array element may have any complete type that denotes a value.
Array postfixes apply from left to right: `Int[3][]` is a dynamic array
of fixed triples, while `Int[][3]` is a fixed triple of dynamic arrays.
In the canonical array type of a constant declaration, each dynamic occurrence is
instead a fixed cardinality inferred recursively from the constant initializer.
Array aliases participate after expansion. Every occurrence must have one
consistent length, and the variable's resolved type uses those fixed lengths.
An explicit `[N]` remains a checked cardinality. Bracket array literals, quoted
and raw byte-array literals, and `fill` participate; a byte-array cardinality is
its decoded or raw UTF-8 byte count. Inconsistent nested lengths are rejected.
This rule applies only to constant declarations: `[]` remains dynamic for variables,
parameters, fields, parameter defaults, and field defaults.

```ritchie
Int[] VALUES = [1, 2]               // resolved type Int[2]
Int[][] GRID = [[1, 2], [3, 4]]     // resolved type Int[2][2]
Byte[] DATA  = b"data points"       // resolved type Byte[11]
Int[3] TRIAD = fill(42, 3)          // count cross-checked against 3
```

The length `N` of a fixed array is a compile-time constant of signed
integer type whose value can widen to `Int`. It must be non-negative and
representable by `Int`.

A fixed array's size is part of its type: `Int[3]` and `Int[4]` are
different types and do not interchange, while every `Int[3]` matches
every other Int\[3\] (structural, #secref(<types-type-equivalence>)). A dynamic `T[]` and a
fixed `T[N]` are likewise distinct. Alias expansion precedes this
comparison, so identity is the canonical element type together with the
dynamic form or fixed length.

There are no implicit element values. An empty dynamic array is the
explicit literal `[]`, and a fixed `T[N]` value is initialized only by
a whole value containing exactly `N` initialized elements. `T[0]` is
already complete when initialized from `[]`.

=== Dicts
`[K: V]` maps keys to values, with keys distinct under value equality
(#secref(<types-type-equivalence>)). A key is a primitive other than `Float`, an enum, or a
struct whose fields recursively qualify. Aliases preserve eligibility.

A literal `[k: v, ...]` has one key type and one value type, inferred
from its context or its entries, and may end with a trailing comma.
`[:]` requires a dictionary context. Equal keys within a literal
overwrite values in written order while retaining the first key value.
A Decimal key uses numerical equality, so differing quantum does not create a
second entry. The same rule applies recursively to a qualifying struct key.
A literal of constant entries is constant.

`d[k]` yields `V` and raises `MissingKey!` for an absent key. Optional
lookup is `d.get(k)` (#secref(<builtins-dicts>)).
Assignment to `d[k]` inserts or replaces the entry; a missing
intermediate dictionary in the target path is not created. Path
evaluation and checks follow #secref(<expressions-assignment>).

A method receiver at a dictionary path is resolved after the arguments
evaluate, using path operands evaluated once before them. The entry
found then is the receiver, so a replacement entry under the key
becomes the receiver and a removed key raises `MissingKey!`.

Pair iteration visits each key present at loop entry exactly once, in
unspecified order, copying the key and value into its loop variables. Mutation
of the source during the traversal has no specified effect on the
remaining visits.

Display is `[key: value, ...]`, or `[:]` when empty, in unspecified
order. No order correspondence is guaranteed between separate
traversals, displays, or enumerations.

=== Composition and nullability

Collections may be nested. `Int[][]` is an array of arrays,
`[String: Int[]]` a dict to arrays. A trailing `?` attaches to the
element base, so `User?[]` is an array of nullable `User`.

=== Mutability
A collection is owned, like a struct: its owner mutates it in place (a
dynamic array grows, a dict gains entries), and handing it to another
variable yields an independent value, so later mutations through either
variable are not observed through the other.

#horizontalrule

== Nullable types
<types-nullable-types>
#idx("nullable")
#idx("null")
A nullable type `T?` is the one way to say a slot may hold nothing. It
is a checked option, not a null: a value of type `T?` is either a
present `T` or #emph[absent], and a plain `T` is always present. No null inhabits an arbitrary type; absence exists
only where the type carries the `?`.

The `?` attaches to the base type: `User?` is a nullable `User`. It
applies once -- there is no `T??`\; absence does not nest. The absent
value is written `none` -- `User? u = none` -- context-typed to
whichever `T?` it appears in.

=== The non-null guarantee
Because only `T?` can be absent, every `T` is a real `T`, used without a
check.

=== Narrowing
#idx("narrowing")
A `T?` cannot stand in for a `T`: you may not apply `T`'s operations to
a possibly-absent value. To obtain a `T`, you #emph[narrow], which
forces the absent case to be confronted -- either by handling the
present and absent cases separately, or by supplying a value to use when
absent. Narrowing requires absence to be resolved first.

```ritchie
Int? maybe = ...
Int sure = maybe ? maybe : 0      // supply a value for the absent case
// maybe + 1                       // rejected: maybe might be absent
```

Accessing a member of a nullable value requires its absence to be
resolved first, using the coalescing operator `??`, its assignment
`??=`, or narrowing -- which applies to a field-access path, not only
a bare name (#link(<chap-expressions>)[Expressions]).

=== Nullable integer widening
Signed-integer widening applies to nullable values: `IntN?` converts
implicitly to `IntM?` when `N < M`, with `Int` participating as `Int64`.
A present value widens; absence remains absent. This applies to
initialization, assignment, arguments, and returns. Coalescing retains its
exact-base-type requirement (#secref(<expressions-ternary-coalescing-and-narrowing>)).

=== Conversion cannot strip `?`
No conversion removes nullability: there is no `fromV` from a `T?` to a
`T`. There is no unchecked unwrap that could turn an absent value into a
`T` and fail at runtime -- removing the `?` is exactly what narrowing
does, with the absent case handled. (Conversion between value types:
#secref(<types-primitive-value-types>).)

=== Composition
`T?` composes with the other forms: `User?[]` is an array of nullable
users, `[String: Int?]` a dict whose values may be absent.

#horizontalrule

== Function types
<types-function-types>
#idx("function type")
The `function` archetype: a callable value, executed locally. A function
type names parameter types and a return type:

```ritchie
(Int, Int) -> Int           // two Ints, returns an Int
(String) -> Bool            // a String, returns a Bool
(Int)                       // an Int, returns nothing (void)
()                          // nothing in, nothing out
```

Only types appear in a function type, not parameter names. The arrow and
return type are omitted for a function that returns nothing.

=== Multiple returns
A function may return more than one value. Multiple return types are
parenthesized:

```ritchie
(String) -> (User, Bool)
```

The parens are required for multi-return, and distinguish it from a
single return. The body returns a value list, matched positionally
against the declared list (#secref(<statements-return-statements>)). A call of a multi-return function has the arity of its return list
(#secref(<expressions-what-an-expression-is>)). Its consumers are
the multi-variable declaration and the return statement of a function with the same
return list.

=== Functions as values
A function type is first-class: a function-typed value can be passed as
an argument, returned, assigned to a variable, or stored in a collection. An array of
function values is written through an alias -- with
`Step = (Int) -> Int`, the array type is `Step[]`\; there is no
parenthesized type composition. Which callables inhabit a function type
-- module-level functions, which do not capture, and lambdas, which
capture by value -- and their written forms are
#link(<chap-expressions>)[Expressions]\; at the type level they share
the one `(P) -> R` shape.

=== Equivalence and initialization
Function types are structural (#secref(<types-type-equivalence>)): every `(Int) -> Int` is every
other. There is no default function: a function-typed variable must be
assigned a function before it is used, which the compiler proves
statically.

=== Comparability and identity
Function values are not comparable: `==`, `!=`, the ordering
operators, and use as a comparable key are all rejected on a function
type. A named function is internally identified by its
module-qualified name and signature, with the declaring type included
for a method. A closure's identity is internally derived by the
compiler.

#horizontalrule

== Generic types
<types-generic-types>
#idx("generic")
#idx("type parameter")
A single uppercase letter (`T`, `U`, `K`, `V`) is a #strong[type
parameter]: a stand-in for some type the caller chooses. It is
introduced by its first appearance in a scope -- a declaration writes
no separate parameter list -- and refers to that same type everywhere
within it. A different letter is a different, independent type.
Multi-letter uppercase names are concrete types.

```ritchie
swap(T a, T b) -> (T, T) { return b, a }       // one T, chosen per call
Box { T value }                                // generic struct
map(T[] xs, (T) -> U f) -> U[] { ... }         // two parameters, T and U
```

A type parameter is #strong[lexically scoped]: a `T` inside a nested
function, method, or lambda refers to the nearest enclosing `T`, or is
introduced fresh if none encloses it. To get a separate type variable,
pick a separate letter.

=== Instantiation at the call
A generic function's letters are fixed at each call, from the arguments
alone, left to right: each letter takes the type of the first argument
that mentions it, and every later mention must match exactly. A letter
that appears only in the return type is a compile error.

A closure argument fixes a letter only when the closure's body is a
single expression whose type is known locally -- a literal, a
constructor, an f-string. A block-bodied closure fixes nothing; some
earlier argument must fix the letter instead.

```ritchie
map(T[] xs, (T) -> U f) -> U[] { ... }
String[] labels = map(counts, n => f"{n}")   // T from counts, U from the body
```

=== Generic structs and application
A struct whose body uses type parameters is a generic struct. Its
parameters are its distinct letters, in order of first appearance in the
body:

```ritchie
Pair { T first, U second }              // parameters T, U, in that order
```

After its declaration the type is always written #strong[applied]: the
name with one type argument per parameter, in angle brackets, in any
type position. Arguments match parameters positionally, and a wrong count is a
compile error.

```ritchie
Pair<String, Decimal> line = .new("SKU-12", 9.99)
Pair<Decimal, Decimal>[] quotes = []
splitName(String full) -> Pair<String, String>
```

An applied generic is nominal in its head and compares its arguments
recursively: every `Pair<Int, String>` matches every other, and
`Pair<Int, Int>` is a different type. There is no partial application
and no unapplied use.

Construction names the applied type like any introduction site: the
literal `Pair<Int, String> { 1, "first" }` inside the declaring module,
an exported creation method elsewhere.

Only structs take parameters. Entity, service, and error types do not,
an enum body has no letters, and the collections keep their own forms
(`T[]`, `T[N]`, `[K: V]`).

=== Methods on a generic struct
A method writes its receiver as an application over fresh letters, one
per parameter. The letters take the type parameters positionally and are in scope through
the whole method:

```ritchie
Pair<T, U>.swapped() -> Pair<U, T> {
    return Pair<U, T> { self.second, self.first }
}
```

Receiver arguments are single letters -- a method
is defined once, for every instantiation. A method may introduce further
letters of its own:

```ritchie
Pair<T, U>.mapFirst((T) -> V f) -> Pair<V, U> {
    return Pair<V, U> { f(self.first), self.second }
}
```

=== Unconstrained, structural
Ritchie has no traits, interfaces, or constraint declarations. A type
parameter is #strong[unconstrained]: a generic body may use any
operation on a `T`, and the operations it actually uses #emph[are] its
requirements. Those requirements are checked where a concrete type is
supplied -- at the call site -- and reported there:

```ritchie
sort(T[] xs) -> T[] { ... xs[i] < xs[j] ... }
sort(widgets)     // rejected at this call if Widget has no `<`
```

`==` and `!=` work on any `T`, because every Ritchie type has equality
(#secref(<types-type-equivalence>)). Everything else -- `<` and other ordering, `.field`,
`.method()`, `T.new(...)` -- works only if the supplied type provides
it. A type that does not is rejected at the call, not inside the
generic. A `where` clause may instead move those checks to the
generic's own definition; bounds are
#link(<chap-contracts>)[Contracts].

=== Passing operations as parameters
When a generic needs an operation a type might not have -- ordering,
arithmetic, a custom key -- it takes that operation as a function
parameter rather than demanding the type carry it:

```ritchie
sort(T[] xs, (T, T) -> Bool less) -> T[] { ... less(xs[i], xs[j]) ... }
sort(widgets, (Widget a, Widget b) -> Bool { return a.priority < b.priority })
```

#horizontalrule

== Type aliases
<types-type-aliases>
#idx("type alias")
An alias is an alternate module-private name for another type. It is a
top-level declaration with the alias name on the left of `=` and its
target type on the right:

```ritchie
AuthUser = auth::User
Scores   = [String: Int]
Handler  = (Request) -> Response
$Account# = ledgerkit::$Account#
```

The target may be any type, including a qualified type, an applied
generic, a collection, a nullable type, or a function type. An alias
takes no type parameters of its own.

The alias name follows the lexicographic form of its resolved target:
plain and composite value types use `Type`, entity references use
`$Type`, entity identities use `$Type#`, services use `@Type`,
projections use `%Type`, and errors use `Type!`. Thus `Account#` and
`AccountId` are invalid aliases of `ledgerkit::$Account#`. The valid
form is `$Account# = ledgerkit::$Account#`.

An alias creates no type identity and changes no property of its target.
Every use resolves as the target type, with the target's identity,
visibility, operations, conformance, ownership, and runtime
representation. An alias cannot be exported or named from another
module. A target from another module must itself be accessible at the
declaration.

Declarations are order-independent, and an alias may target another
alias. An alias target must resolve without revisiting an alias
declaration. Revisiting one is a compile error.

=== Distinct types
#idx("distinct type")
The `:=` form mints a new nominal value type over a representation,
distinct from it with no implicit conversion either way:

```ritchie
Email   := String
OrderId := UUId
Meters  := Decimal
```

`Email` and `String` do not interchange: neither assigns to the other,
and `Email` exposes none of `String`'s operations. `Email` defines its
own methods, conforms to contracts on its own terms, and may carry an
`__invariants()` gate (#secref(<types-struct-types>)). Construction and read-back go through
functions the author exports, by convention
`Email.fromString(String) -> Email | InvalidEmail!` and
`String.fromEmail(Email)`. A `:=` type takes its representation's
serialized form -- an `Email` is a `String` on the wire and disk.

Three related nominal declaration forms:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Meaning],),
    table.hline(),
    [`L := M`], [distinct nominal value type over the representation `M`],
    [`L { M field }`], [struct -- a product with a named field],
    [`L { embed M field }`], [struct that also promotes `M`'s methods
    (#secref(<types-struct-types>))],
  )]
  , kind: table
  )

#horizontalrule

== Explicit and inferred declarations
<types-explicit-and-inferred-declarations>
Every introduced name gets its type at the introduction site, either
from a written type or from one local typed source. An ordinary variable,
field, parameter, or constant writes its type before the name.

It applies even to results that could be inferred:

```ritchie
Int    n = compute()         // written, though compute returns Int
User   u = find(id)
Float  r = a / b
String s = ok ? "y" : "n"
```

A call result, field access, operator result, or ternary supplies the
value while the written type declares the name. Literals and constructors
also use the type-first form.

=== Locally inferred introductions
Two introductions infer their type from one local typed source:

- #strong[The for-loop variable.] `for item in accounts { ... }` gives
  `item` the element type of the iterated collection; the one lookup is
  that collection's type. (Loop forms:
  #link(<chap-statements>)[Statements]\.)

- #strong[A lambda parameter under a function-type pin.] When a lambda
  is the value of a variable or parameter whose written type is a
  function type, its parameter types come from that type; anywhere else
  a lambda writes its own parameter types. (Lambda forms:
  #link(<chap-expressions>)[Expressions]\.)

  ```ritchie
    (Int) -> Int f = x => x + 1      // x is Int, from the pin on the left
    map(xs, (Int n) => n * 2)        // no pin here, so n is written
  ```

Both forms derive the type from adjacent local context. Operations on a
generic `T` use the bounds described in #secref(<types-generic-types>).
