#import "../prelude.typ": horizontalrule, idx, secref

= Standard Library
<chap-stdlib>
The modules that ship with the language, and the rule they are built
under: every local side effect enters through one module, so purity is
a property the compiler derives rather than an annotation the author
writes.

== Organisation
<stdlib-organisation>
The language provides its functionality in three forms. Built-in functions
(#link(<chap-builtins>)[Built-ins]) are always in scope and are pure
functions over owned values. The standard library is available through the
implicit module name `r`. Its types and operations use ordinary
qualification, such as `r::Clock.time()` and `r::Random.seed(seed)`. The
module name requires no import or application declaration. Its names and semantic
operation identities are known to the compiler. Built-in services (#secref(<stdlib-built-in-services>))
are provided by the runtime and follow service semantics without an
application declaring or deploying them.

== Clock
<stdlib-clock>
#idx("Clock", code: true)
`r::Clock.time() -> DateTime` returns the current UTC wall-clock reading. It
provides no monotonicity guarantee. Its precision and accuracy are properties
of the execution environment.

`r::Clock.withOffset(Duration offset) -> r::Clock` constructs a
clock value retaining `offset`, under the admission of DateTime `withOffset`
(#secref(<temporal-datetime-operations>)), and raises `OutOfRange!` for an
offset that DateTime `withOffset` does not admit.
`clock.time() -> DateTime` returns the reading `r::Clock.time()` would
return, retaining the clock's offset. A clock value has the capabilities of
an `r::Random` value (#secref(<stdlib-random>)): no equality, ordering, dictionary keys, or
constant evaluation, and `withOffset` is a runtime operation. It displays as
`r::Clock { offset: -04:00 }`, with `Z` for UTC. `clock.offset() -> Duration`
returns the retained offset.

`r::Clock.sleep(Duration duration)` and `clock.sleep(Duration duration)`
suspend the calling execution for at least `duration` and then return. A
`duration` that is zero or negative returns at once. The offset of a clock
value does not change its sleep. Other executions continue while one sleeps.

== Random
<stdlib-random>
#idx("Random", code: true)
The standard-library Random interface is:

```ritchie
r::Random.new() -> r::Random
r::Random.seed(Byte[32] seed) -> r::Random

random.bits() -> BitSet
random.ratio() -> Float
random.range(Int from, Int to) -> Int
```

`r::Random.new()` constructs a generator initialized from execution-environment
entropy. `r::Random.seed(seed)` constructs a reproducible generator using all
32 bytes in array order. The same seed and sequence of calls produce the same
results within one compiler/runtime release. Numeric sequences may change
between releases.

`bits()` returns 64 pseudorandom bits as a `BitSet`. `ratio()` returns a
pseudorandom `Float` in `[0.0, 1.0)`. For unequal endpoints,
`range(from, to)` is uniform over the integers starting at `from` and
proceeding toward, but excluding, `to`. Ascending and descending ranges are
both admitted, including every pair of `Int` endpoints. `range(x, x)` returns
`x` and does not affect subsequent draws.

`r::Random` supports neither equality nor ordering and is not
dictionary-key-capable. Its values are not constant-capable; in particular,
`r::Random.seed(...)` is a runtime operation even when its argument is
constant.

A generator constructed by `new()` displays as `r::Random {}`. A generator
constructed by `seed(seed)` displays as
`r::Random { seed: 0x` followed by the 64 lowercase hexadecimal digits of the
32 seed bytes in array order, then ` }`. Display records construction
provenance, does not reveal environment entropy, and is unchanged by draws. It
is descriptive and is not source construction syntax.

This chapter defines no durable or protocol representation for `r::Clock` or
`r::Random`.

== Impurity
<stdlib-impurity>
#idx("impure")
#idx("purity")
A call to a method of `r::Clock` or `r::Random` is a direct impurity root,
except `r::Clock.withOffset(...)`, `clock.offset()`, and
`r::Random.seed(...)`. `withOffset` and `seed` are runtime-only. A function is #emph[impure] when its body can reach a direct root, through any
chain of local calls. The compiler propagates the property through the call
graph; there is nothing to declare. Wrapping changes no classification.

An impure call is inadmissible where evaluation must be repeatable:

- #strong[Entity method bodies]
  (#secref(<entities-methods-and-transactions>)). A body re-executes on
  `Conflict!`, and every attempt must derive the same writes from the
  same arguments. Time or randomness a transaction needs is pinned by
  the caller and enters as a parameter.
- #strong[Declaration-level initializers]
  (#secref(<declarations-top-level-declarations>)). These evaluate
  before the module serves anything, and a value that differed per
  instance would not be a constant.

```ritchie
DateTime stamp = r::Clock.time()   // service code: fine
receipt = order.settle(stamp)      // the transaction receives time as a value
```

`print` and `log` (#secref(<builtins-diagnostics>)) stay admissible
everywhere: output is not program state, and a body that writes output
still derives the same writes.

With remote operations already fixed as the only non-local work
(#secref(<effects-remote-operations>)), this closes the account:
every result that can vary between runs enters through a direct local
impurity root or through a remote operation. Everything else is
deterministic.

== Derived modules
<stdlib-derived-modules>
Programmer-facing interfaces are ordinary standard modules built on the
primitive capabilities, and they carry the impurity of what they call. Their
catalogues are specified with the modules themselves; this chapter fixes only
the structure.

== Built-in services
<stdlib-built-in-services>
#idx("IO", code: true)
Real input and output -- files, byte streams, the world outside the
mesh -- is a built-in service, `@IO`, not a standard module, because
I/O has everything a remote operation has: it takes real time, it can
fail, and it runs under the ambient budget
(#secref(<effects-remote-operations>, <effects-time-budgets>)). A call on `@IO` behaves
as any service call -- a read returns a future, a call yields
`Unavailable!` when recovery is exhausted, and cannot appear in an
entity method body. The runtime provides it wherever code runs, with no
declaration and no placement of its own. Its method surface is reserved
and will be fixed alongside the stream syntax
(#secref(<lexical-reserved-for-future-use>)).
