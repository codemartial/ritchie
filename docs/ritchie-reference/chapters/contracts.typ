#import "../prelude.typ": horizontalrule, idx, secref

= Contracts
<chap-contracts>
A contract is a named set of method signatures, introduced by the
`contract` keyword and carrying no sigil. It serves three roles, each opt-in and
independently useful:

- #strong[Conformance.] Where a contract is in view, definitions of its
  method names must match its signatures. Conformance is structural.
- #strong[Bounds.] A generic body may require a type parameter to
  satisfy a contract, so the body is checked once at its definition.
- #strong[Provider declaration.] A type's author may declare
  conformance with `is`, getting it verified at the declaration and
  receiving the contract's default bodies.

== Declaring a contract
<contracts-declaring-a-contract>
#idx("contract")
A contract is written like ordinary types or their methods, except
that the type definition must be preceded by the keyword `contract`
and the parametrised types are denoted by single upper-case letters,
such as `A`, `B`, `C`, or most commonly, `T` for Type.

```ritchie
contract Labeled {
    T.label() -> String
}

contract Ordered {
    T.before(T other) -> Bool
}
```

Whether a contract method is instance-level or type-level follows the
usual rule: a method that uses `self` is instance-level, one that does
not is type-level (#link(<chap-declarations>)[Declarations]). A
contract method name does not start with `_`, since a private method
reaches no other module (#secref(<declarations-functions-and-methods>)).

A signature may use a second free type variable for a related type:

```ritchie
contract Conversion {
    T.from(V source) -> T      // V matches any source type
}
```

Tying the return to the receiver puts the contract's invariant in the
signature: every conforming `from` returns its own type.

== Conformance
<contracts-conformance>
#idx("conformance")
#idx("structural conformance")
A type conforms to a contract by having methods of the contracted
names and shapes. The receiver pins `T`, free variables such as `V`
match any type, and the rest of the signature must match exactly. No
declaration is needed for a type to be #emph[used] where a contract is
required (#secref(<contracts-bounds>)).

>>> NEEDS REVIEW
A contract additionally owns its method names within any module that
mentions it -- in a `where` bound, an `is` declaration, or by qualified
name. In such a module, every definition of a contracted name must
conform:

```ritchie
Money.from(Float f) -> Money     // conforms to Conversion
Money.from(Float f) -> String    // rejected where Conversion is in view
```

A module that does not mention the contract may define the same name with
any signature. That definition is simply not a contract member and is
invisible wherever the contract is required. There is at most one
definition per concrete signature in a program, so dispatch is
unambiguous.

<<< NEEDS REVIEW

>>> NEEDS REVIEW
=== Name-ownership and dispatch
The type checker verifies that a result fits its destination. It cannot
verify that a call is dispatched on the type the author meant:

```ritchie
String s = li.from(f)       // intended: l.from(f)
```

If some unrelated `Sku.from(Float) -> String` existed, the slip would
type-check. Under the contract no such definition can exist where the
contract is in view, so the misdispatch has no target and surfaces as an
error. The contract couples dispatch to the type check.
<<< NEEDS REVIEW

== Bounds
<contracts-bounds>
#idx("where", code: true)
#idx("is", code: true)
#idx("bound")
A generic body that calls contracted methods declares the requirement
with `where`. `is` reads \"satisfies\":

```ritchie
T.describe() -> String where T is Labeled {
    return f"<{self.label()}>"
}
```

Several contracts on one parameter separate with commas, and different
parameters with `and`:

```ritchie
pick(T a, T b) -> T where T is Ordered, Labeled { ... }
merge(T a, U b) -> U where T is Labeled and U is Numeric { ... }
```

The checking rule has two halves:

- #strong[At the definition.] A bounded parameter is opaque except for
  its promised interface: the methods of its bounds, plus the universal
  operations (assign, pass, `==`, `!=`). Any other use is an error in the
  generic itself.
- #strong[At the call.] A concrete type filling a bounded parameter must
  satisfy every bound -- structurally, with no declaration required of
  the type's author. A failure is reported at the call.

An unbounded parameter behaves as before: its requirements stay implicit
and are checked at each call (#secref(<types-generic-types>)).
Bounds are opt-in definition-time checking, not a replacement.

A bound names a fixed-signature contract -- `Labeled`, `Ordered`, one
written for exactly what the body needs. A family contract such as
`Conversion` (free `V`) is enforcement-only and is excluded from a
`where`: \"converts from something\" is not a callable capability. A
body that needs \"builds from a String\" names that:

```ritchie
contract Parseable {
    T.parse(String s) -> T
}

T.parsed(String s) -> T where T is Parseable {
    return T.parse(s)
}
```

Contracts take no type arguments and a bound is always the whole
contract.

== Provider declaration
<contracts-provider-declaration>
#idx("provider declaration")
A type's author may declare conformance at the top level, reusing `is`:

```ritchie
Money is Comparable
```

The declaration is opt-in. It changes nothing about where `Money` may be
used -- structural conformance already settles that -- and it does two
things at the spot where it stands:

- #strong[Verifies now.] The compiler checks `Money` against
  `Comparable` at the declaration, in the provider's own build, instead
  of at whatever distant call site first requires it. A drift between
  the type and the contract surfaces as the provider's error, on the
  provider's line.
- #strong[Grants the defaults.] The contract's default method bodies
  (#secref(<contracts-default-method-bodies>)) attach to `Money` here and only here.

The declaration lives in the module that owns the type and may name any
contract, qualified by its module.

== Default method bodies
<contracts-default-method-bodies>
#idx("default method")
A contract method may carry a body, derived from the others:

```ritchie
contract Comparable {
    T.cmp(T other) -> Int
    T.max(T other) -> T { return self.cmp(other) < 0 ? other : self }
    T.min(T other) -> T { return self.cmp(other) < 0 ? self : other }
}
```

A defaulted method is granted to a type only by a provider declaration
(#secref(<contracts-provider-declaration>)): `Money is Comparable` verifies `cmp` and gives `Money` a
`max` and `min` supplied by the contract, defined once, unable to drift. The type
may still define its own `max`, which must conform and then replaces the
default.

Structural conformance alone grants no methods. A granted method counts
toward conformance like any other method of the type, but only a
declaration brings it into existence.

Two provider declarations may not grant the same method name. Where a
second `is` would bring in a default whose name an earlier declaration
already granted, the second declaration is a compile error. The type
resolves it by defining the method itself, which then must conform to
every contract that names it. There is no ordering rule among contracts.

== Closed payload sums
<contracts-closed-payload-sums>
#idx("sum type")
#idx("closed sum")
The sum archetype has a constant form -- the enum -- and a payload
form: a closed alternation of existing types, declared with `=`:

```ritchie
Payment = Card | BankTransfer | StoreCredit
```

Rules:

- Two or more members, pairwise distinct types. The member type is the
  tag. Two arms needing the same representation get their own named
  types first.
- A member value widens to the sum implicitly -- always safe, no cast.
  There is no narrowing the other way.
- The sum exposes nothing of its members directly. `match` recovers a
  member, introducing the payload as a pattern variable written type-first. An ordinary match may cover
  any subset of members and may carry `any` for the remainder:

```ritchie
describe(Payment p) -> String {
    match p {
        case Card c          => return c.masked()
        case BankTransfer b  => return b.reference()
        case StoreCredit s   => return s.code()
    }
}
```

\(The `case Type name =>` arm tests the active member and introduces it as a pattern variable,
written type-first, for the arm's body: #secref(<statements-match>).)

Adding a member does not invalidate an ordinary match. A method defined
on the sum (by matching inside)
can conform the sum to any contract that shape fits, making `Payment[]`
a checked heterogeneous collection.

Sum members are value types and enums in this version. Entities do not
sum.

== Limits
<contracts-limits>
- #strong[No contract-typed values.] A variable, field, or collection
  element does not get typed by a contract -- there is no `Labeled x`
  holding \"some conformer\". Heterogeneity over a known member list is
  a sum (#secref(<contracts-closed-payload-sums>)); an open set of conformers in one collection -- the
  plugin-host shape -- waits on this mechanism and is deferred with it.
- No marker contracts: a contract with no methods has no shape to match.
- No associated types and no parameterized contracts.
- No mutation marking: neither contract signatures nor method bodies
  are classified by whether they change `self`. A contract-method call
  follows the uniform receiver rule -- `self` refers to the receiver
  when it is a mutand (#link(<chap-types>)[Types]) -- so a generic
  caller's variable can change through a contract method exactly as
  through a direct method call.
- No pre- or post-conditions: behaviour beyond what a signature states
  plays no part in conformance.
