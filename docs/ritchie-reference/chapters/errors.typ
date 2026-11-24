#import "../prelude.typ": horizontalrule, idx, secref

= Error Handling
<chap-errors>
How a Ritchie program signals failure and recovers from it: raising and
propagation, the `handle` forms, declared error sets, error history, and the closed runtime-failure channel
beneath them all. The error types themselves are declared in
#link(<chap-types>)[Types].

== Two channels
<errors-two-channels>
#idx("error channel")
Failures occur on two distinct channels.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Channel], [Domain error], [Runtime failure],),
    table.hline(),
    [What it is], [an application verdict: the operation ran and rejected], [a mechanism condition: the operation did not run, or its outcome is unknown (timeout, connection loss)],
    [Who acts on it], [the program, through a handler], [the runtime first (retry, failover); the program only after],
    [Reaches a `handle` arm], [yes], [only after recovery is exhausted],
  )]
  , kind: table
  )

The runtime can attempt to recover from runtime failures on its
own. When recovery is exhausted the failure surfaces as a
system-defined error type (#secref(<errors-runtime-failures>)),
matchable in `handle` like any other error. The application author can
then take a suitable domain-level action. Having to issue retries,
manage backoffs, rate-limiting or other such measures are not required
and not recommended.

#horizontalrule

== Raising and propagation
<errors-raising-and-propagation>
#idx("raise")
#idx("propagation")
An error can be raised by constructing its corresponding `Error!`
value and returning it. An error value can be a bare fieldless
`Type!`, a literal `Type! { ... }`, or an error value read from a variable.

```ritchie
$Account.withdraw(Decimal amount) -> Receipt | InsufficientFunds! {
    if amount > self.balance {
        return InsufficientFunds!
    }
    self.balance = self.balance - amount
    return Receipt.new(amount)
}
```

Each fallible operation has an immediate implicit error
check. Without recovery, execution is sequential fail-fast: a failed
operation stops the unfinished statement and bubbles to the caller. A
handler may recover the operation before propagation (#secref(<errors-inline-handle>, <errors-fallback-and-nested-handling>, range: true)).

```ritchie
priceLine(Line line) -> Money {
    Money base = unitPrice(line.sku) // if this errors, the next two statements don't run
    Money taxed = addTax(base, line.region)
    return scale(taxed, line.qty)
}
```

A method or field chain short-circuits on the first failure: in
`order.reserve().charge().ship()`, a failure in `reserve()` skips
`charge()` and `ship()`.

A `return` containing exactly one error-valued expression raises it,
regardless of the function's success schema. Success values and an error
cannot share one return. Error types are excluded from the success slot
of a signature, so `-> NotFound!` as a success form is a compile error.
The raise location is the return statement, not the construction site.

Propagation is bare and history-preserving. A bubbling error keeps its current
type and declared field values unchanged. Bubbling performs no conversion. The
failed operation is its propagation location. Re-expression occurs only by
returning a different error from inside a handle arm
(#secref(<errors-re-expression-and-history>)). Handling precedes propagation:
add a propagation step only when the decision to bubble is made.

=== The error value
<errors-the-error-value>
Every error type refines `Error!`, the static type of an `any` arm's error
variable (#secref(<errors-inline-handle>)). `Error!` is not constructed or
listed in a declared error set (#secref(<errors-the-checked-error-contract>)).
The current error retains its nominal type, declared typed fields, and location.
A typed arm may read those fields. The error variable of an `any` arm is opaque
apart from `history`. It may be forwarded, stored, passed, or logged.

`history` is an immutable iterable `ErrorStep[]`. Its read-only descriptor
types are:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Type], [Fields],),
    table.hline(),
    [`ErrorStep`], [`ErrorType typ`, `ErrorLocation location`,
    `String[] fieldVals`],
    [`ErrorType`], [`String module`, `String name`,
    `ErrorField[] fields`],
    [`ErrorField`], [`String name`, `String typeName`],
    [`ErrorLocation`], [`String revision`, `String module`,
    `String? typeName`, `String function`],
  )]
  , kind: table
  )

These read-only descriptors have no literal form. The default display of
a descriptor is the struct form (#secref(<types-struct-types>)) of its
type name and its fields in the order above, and two descriptors of one
type are equal when their fields are equal. `ErrorType` identifies
the defining module and nominal error name. Its fields align with
`fieldVals` in declaration order. `ErrorLocation` identifies the revision,
module, optional enclosing type, and function. Each `fieldVals` entry uses
its field type's canonical diagnostic representation in the error's
declaring module. Its original value type cannot be recovered.

An inert error value has empty history. Its first error-valued return
establishes the current location without adding a historical step. A
previously raised error retains its history when stored, passed, or
returned later.

History runs from the deepest step to the most recent step. Reverse
iteration starts immediately below the current error and
walks inward. Within a module it contains each propagation step. At a
published module boundary, steps added inside that module are removed and
deeper boundary steps remain. A program may compare or branch on its
descriptors, but an `ErrorStep` is not an `Error!`: it cannot be raised,
matched as a typed error, or used to recover typed payload values.

Errors compare by current nominal type and declared field values. Current
location and history do not participate in equality. Default diagnostic
rendering includes the current type and fields, current location, the
routine that raised the current error, and every history step except the
step at which that routine raised it. Its layout and explanatory text are non-normative.

#horizontalrule

== Declared error sets
<errors-the-checked-error-contract>
#idx("declared error set")
#idx("error set")
A function's raise set is inferred, as
#link(<errors-inferred-raise-sets>)[Inferred raise sets] below states.
Any function or method may also declare an error set in its return
signature. A declared error set records the program-declared errors that can
escape the function, and the compiler checks the record against the inferred
set. It has no other effect: callers, handlers, and the runtime use the
inferred raise set.

A declared error set is written in the return signature after the
success form, as one or more error types joined by `|`:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Success], [Errors], [Signature],),
    table.hline(),
    [one value], [none], [`-> Receipt`],
    [one value], [yes], [`-> Receipt | InsufficientFunds!`],
    [two values], [yes], [`-> (Receipt, Stamp) | Locked! | Closed!`],
    [no value], [none], [omit `->`],
    [no value], [yes], [`-> none | Posted! | Declined!`],
  )]
  , kind: table
  )

The success outcome always leads the signature: a type, a parenthesised
value list, or `none` for a method that returns nothing but can fail.
Each error keeps its `!`. The grammar is
`success-form { "|" error-type }`.

Rules:

- A declared error set lists only error types that the program declares.
  Listing a catalog error (#secref(<errors-the-built-in-error-catalog>)) is a
  compile error.
- When a function declares an error set, it lists each program-declared error
  that can escape the function, and each listed error can escape it. An
  unlisted escaping program-declared error and a listed error that cannot
  escape are compile errors. Catalog errors escape without being listed.
- A function whose inferred raise set has incomplete knowledge, because it can
  raise an `Error!` value whose nominal type is unknown, cannot be checked
  against a declared error set, so a declared error set on such a function is
  a compile error.
- A contract method declares no error set (#link(<chap-contracts>)[Contracts]),
  because a required contract method has no body and therefore no inferred
  raise set to check a declared set against.

=== Inferred raise sets
<errors-inferred-raise-sets>
A raise set describes the nominal errors an operation or function may
propagate under the rules below. An ordinary function needs neither an
error declaration nor a handler for each inferred error. Unhandled
errors use the fallback in #secref(<errors-fallback-and-nested-handling>).

Constructing, copying, storing, or passing an error value does not raise
it. An explicit error return contributes the possible nominal types of
the returned value. A concrete error type supplies that nominal type.
Local variables and copies retain known types, and control-flow joins
combine alternatives. A typed error variable supplies the matched
type; an `any` error variable supplies the remaining possibilities that select
that arm. A value of opaque `Error!` whose nominal type is unknown,
including an ordinary function parameter, contributes incomplete
knowledge when raised. Incomplete knowledge is distinct from an empty
set. Ordinary argument values do not specialise a function's raise set.

An expression includes failures of its evaluated operands and its own
operation. A local call contributes its callee's inferred escaping
errors. Calls are treated as possibly successful for local reachability;
inference does not prove from a callee's body that following code cannot
execute. Recursive re-entry introduces no additional error source: stop
descending that path, while retaining the dependency through which the
cycle's errors propagate. The resulting sets are independent of
traversal order and require no recursive unrolling.

Use the local control-flow rules of #link(<chap-statements>)[Statements].
A compile-time Boolean condition selects its evaluated path; other
conditions contribute both paths. Explicit transfers end their current
paths. Apply the corresponding evaluation rules to short-circuit and
conditional expressions (#link(<chap-expressions>)[Expressions]).
Iterator loops retain their zero-iteration and exhaustion paths; constant
range cardinality supplies no additional reachability proof. No further
dead-code, termination, argument-value, or range analysis is required.
Optimisation does not change the inferred set or handler validity.

Each nonconstant built-in operation contributes a fixed set for its
selected type mapping, using the errors defined by its contract in
#link(<chap-types>)[Types], #link(<chap-expressions>)[Expressions], and
#link(<chap-builtins>)[Built-ins]. Do not narrow that set using the values
of particular operands. Thus nonconstant integer division contributes
`DivisionByZero!` even with a constant nonzero divisor. A successful
constant expression contributes no runtime failure of its own; an
invalid constant expression retains its compile-time diagnostic and
cannot be recovered by a handler. Operand failures remain included.

Handling transforms these possibilities according to
#secref(<errors-inline-handle>, <errors-fallback-and-nested-handling>,
range: true).  Successful resumption, diversion, or a successful
non-error return consumes the original error. An explicit error return
propagates the returned value's possible types, replacing or
forwarding the original as specified in
#secref(<errors-re-expression-and-history>). An unmatched error
propagates outward.

A failure while evaluating recovery is available to the lexically
enclosing handlers specified in
#secref(<errors-fallback-and-nested-handling>). If unrecovered, it is
reported separately, the recovery attempt ends, and handling of the
original error continues outward. Such a secondary failure does not
join the enclosing function's escaping raise set. A completed explicit
error return from an arm does contribute its returned types. A failure
while evaluating that return's operands is still a recovery failure.
Consequently the errors available to an enclosing handler can differ
from those escaping the function.

=== Dead-arm guard
A `handle` arm is a compile error when no failure of the operations it
protects can select it, under the selection rules of
#secref(<errors-inline-handle>, <errors-fallback-and-nested-handling>,
range: true) and the raise sets of
#link(<errors-inferred-raise-sets>)[Inferred raise sets]. Where that
information is incomplete, the arm is live.

The guard judges a handler as if its protected region were entered, so a
handler inside code that cannot be reached keeps its arms. In the
following program the arm is live, although the `if` that encloses it
cannot be entered:

```ritchie
Bad!

f() -> Int { return Bad! }

main() {
    if false {
        Int x = f() handle Bad! => 0
        log(x)
    }
    log(1)
}
```

#horizontalrule

== Inline handle
<errors-inline-handle>
#idx("handle", code: true)
An inline `handle` protects one statement without adding a lexical scope.
Every arm abandons the rest of the recovered statement
(#secref(<errors-block-and-loop-handling>)). An arm that supplies values
completes that statement with those values as its results.

```ritchie
Int n = readCount() + 1 handle Malformed! => 0
```

A handler contains at least one arm. Its arm list is enclosed in `{ ... }`.
When the list contains exactly one arm, its list braces may be omitted. No
newline or semicolon may separate `handle` from the compact arm pattern. The
compact handler ends at the ordinary statement boundary after its arm body. A
brace following `=>` is the arm's statement body. Multi-arm lists require
braces; braces do not admit an empty list. Braced and compact forms have
identical scope, selection, recovery, fallback, and control behavior.

If `readCount()` fails here, `n` becomes `0` and the addition does not run.

A statement's results are the values it produces:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Statement], [Results],),
    table.hline(),
    [`Int n = f()`], [the value of `n`],
    [`x = f()`, `a, b = f()`], [the values stored],
    [`total += f()`], [the new value of `total`],
    [`return f()`], [the values returned],
    [`resume f()`], [the values supplied],
    [any other statement], [none],
  )]
  , kind: table
  )

An arm supplies values with a concise list, such as `=> 0`, or with
`resume` and a value list, such as `resume 0`. The values must match the
results of the recovered statement in number, order, and type. The
recovery in a brace arm runs statements and may resume, divert, or
return explicitly.

The compiler rejects an arm that supplies values when a statement that
the arm can recover has no results, because that statement has no
destination for the values. A `resume` without values completes the
recovered statement without supplying values, as a no-op arm does
(#secref(<errors-block-and-loop-handling>)).

When several remote operations of one statement fail, the statement fails
with one of those failures, and which one is unspecified
(#secref(<effects-failures-in-written-order>)).

An arm pattern is `ErrorType! [caught]` or `any [caught]`.
Brackets here mark optional parts. An arm selects by error type only. A typed
error variable has the matched nominal error type. An `any` error variable has type `Error!`. Each arm has its own lexical
scope. Only the current error participates in selection, not its history.

```ritchie
Empty!
Negative! { Int value }

readCount(Int raw) -> Int {
    if raw == 0 { return Empty! }
    if raw < 0 { return Negative! { raw } }
    return raw
}

main() {
    Int n, m = readCount(3), readCount(-2) handle {
        Negative! e => 0, e.value
        Empty!      => 1, 1
    }
    log(n + m)
}
```

Here `readCount(-2)` fails with `Negative!`, and the arm that matches that
type supplies both results, reading the field `value` of the caught error. The
program writes `-2`. The arm for `Empty!` is live, because `readCount` can
raise `Empty!` and an argument value does not narrow its raise set
(#link(<errors-inferred-raise-sets>)[Inferred raise sets]).

Aliases use canonical identities. Selection prefers a typed arm for the
current error, then `any`, regardless of typed-arm order. Duplicate canonical
patterns are invalid. `any` is optional, unique, and final.

Each replacement must match the result types of every statement that
its arm can recover. Recovery discards the results of the other
operations of the recovered statement and does not repeat them. Their
completed effects remain.  All evaluations and required checks of a
multi-variable declaration or multi-assignment complete before any
destination is committed.

#horizontalrule

== Block and loop handling
<errors-block-and-loop-handling>
#idx("handled block")
#idx("handle block")
#idx("protected statement")
A `handle` suffix may follow any statement. A handler on a statement
without a body protects that statement. A handler on a block, a loop,
or an `if` protects each statement directly inside its bodies.  A
handler on a `match` protects each case as a whole. A handler does not
protect a loop header, an `if` condition, or a `match` subject. A
protected block retains its ordinary scope.

A handler on a loop acts as a handler on a block that contains the loop
body, so its arms can read the loop variables.

```ritchie
for line in readLines() {
    post(line)
} handle {
    Malformed! => continue
}
```

Here the handler protects `post(line)` and does not protect
`readLines()`. A failure of `readLines()` passes to the enclosing handler.
To protect the header as well, put the loop in a handled block:

```ritchie
{
    for line in readLines() {
        post(line)
    } handle {
        Malformed! => continue
    }
} handle {
    any => { log("stopped") }
}
```

The outer handler protects the loop as a whole. It recovers a failure of
`readLines()` and a failure of `post(line)` that the inner handler does not
select.

The failed statement is the innermost statement that contains the failed
operation. The recovered statement is the protected statement, among those
of the selecting handler, that contains the failed statement
(#secref(<errors-fallback-and-nested-handling>)). The handler recovers it as
a whole, and the arm and recovery rules of #secref(<errors-inline-handle>)
apply to it. A block, a loop, an `if`, and a `match` have no results.

A no-op recovery completes without supplying values, diverting, or returning,
such as `{ log("skipped") }`. After a no-op arm, the recovered statement
counts as complete and the statements after it run
(#secref(<effects-failures-in-written-order>)).

After a no-op arm, each variable that the recovered statement can store has
no value, even if it held one before. Reading it before a later store is a
compile error.

The compiler rejects a no-op arm that can be selected when the recovered
statement:

- can store a field, an element, or a dictionary entry, including a method
  call that can change its receiver; or
- is a `return` that supplies values.

`break` and `continue` in an arm act on a loop. In an arm of a loop handler,
`break` ends that loop and `continue` starts its next iteration. In an arm
of any other handler, they act on the nearest loop outside the statement
that precedes the handler. The compiler rejects them when no loop is
outside the statement.

```ritchie
for item in items {
    Price p = @Pricing.quote(item)
    total += p.amount
} handle {
    Unavailable! => continue
}
```

When a quote fails, the rest of that iteration is abandoned, `total` is
unchanged for that item, and the next iteration runs. With a no-op arm
such as `Unavailable! => { log("skipped") }`, the declaration of `p`
completes as a no-op, `p` has no value on that path, and the compiler
rejects `total += p.amount`.

An ordinary loop declared inside an arm has ordinary `break`, `continue`,
and label targets. Otherwise those keywords use the handler targets above.
A label outside the arm is not a transfer target. `resume` targets its
lexically enclosing handler arm, including from a loop inside that arm.
`return` exits the enclosing callable after evaluating its operands.

Diversion skips unfinished evaluation and stores. Completed effects remain.
A variable use is valid only if every reaching path supplies its value.
Remote work and exits obey #secref(<effects-scope-exit>).

#horizontalrule

== Fallback and nested handling
<errors-fallback-and-nested-handling>
A failure is offered to the handlers that protect a statement
containing the failed statement. The handler nearest in scope to the
failed statement receives it first. A handler whose arms do not select the
failure passes it to the handler at the next higher scope. The handler
that selects the failure recovers the statement that it protects. A
failure that no handler selects bubbles to the caller. A final
`any` overrides this fallback for errors not selected by a more
specific arm.

A failure inside recovery logic is offered to the handlers outside the
handler that runs it. A handler there that selects the new failure
recovers the statement that it protects, as it does for any other
failure. When no handler of the routine selects the new failure, the
new failure is reported with its own history, and the handler itself
fails with the same error that it was attempting to recover from. This
error may be recovered by another handler in an outer scope, and the
recovery then continues at the statement being recovered. For example:

```ritchie
Unreadable!
Unrepairable!

read() -> Int {
    return Unreadable!
}

repair() -> Int {
    return Unrepairable!
}

main() {
    {
        Int n = read() handle Unreadable! => repair()
        log(n)
    } handle Unreadable! => 0
    log("done")
}
```

Here `read()` fails with `Unreadable!`, and the inner handler calls
`repair()`, which fails with `Unrepairable!`. No handler of `main`
selects `Unrepairable!`, so it is reported with its own history. The
inner handler then fails with `Unreadable!`, and the block handler
recovers the declaration of `n` with `0`. The program reports
`Unrepairable!` and then writes `0` and `done`. With
`handle Unrepairable! => 1` as the block handler, the block handler
selects the new failure and recovers the declaration of `n` with `1`, so
nothing is reported and the program writes `1` and `done`.

An error returned explicitly from a recovery follows
#secref(<errors-re-expression-and-history>).

=== Exhaustive handle
#idx("exhaustive", code: true)
A `handle exhaustive` block drops the fallback: it must match every
domain error the handled work can raise, and no `any` arm is allowed. An
unmatched domain error is a compile error, not a bubble. The runtime
channel stays exempt -- its types may be matched or left to bubble. Adding an error to the
handled work's raise set then turns every `handle exhaustive` of it into a
compile error -- the closed-world guarantee a total `match` gives over a
sum.

```ritchie
{
    reserve(order.item, order.qty)
    charge(order.customer, order.total)
} handle exhaustive {
    OutOfStock!        => return cancel(order)
    InsufficientFunds! => return decline(order)
}
```

#horizontalrule

== Re-expression and history
<errors-re-expression-and-history>
#idx("error history")
#idx("re-expression")
An inert error returned outside a handle arm starts a new chain. Implicit
bubbling preserves the active chain. An explicit error return selects the
error to bubble as follows.
Returning the caught error bubbles it. Returning a different error
re-expresses the failure:

```ritchie
@Orders.place(Cart cart) -> Confirmation | OrderFailed! {
    Receipt r = @Payment.charge(cart.customer, cart.total) handle {
        any => return OrderFailed!
    }
    return confirm(cart, r)
}
```

The returned `OrderFailed!` becomes the typed current error at that return
location. Its history is the caught error's history followed by the caught
current error. An upstream handler matches `OrderFailed!`. It may iterate
the history but cannot match or re-raise its steps.

The new current error above is inert before that return. Returning an
error that was already raised propagates its own history instead. The
caught error is then handled, and the two histories do not combine. A
stored copy of the caught error retains the caught history and bubbles it.

Each implicit bubble likewise adds the prior current error as a step and
makes the same type and fields current at the implicit propagation location.
Returning a caught error held in an error variable does the same at the explicit return
location. A failure while evaluating return operands follows #secref(<errors-fallback-and-nested-handling>).
It has not completed the explicit return. Once execution has left the
handle arm, returning an inert error starts a new chain. Returning a
previously raised error preserves the history it already carries.

#horizontalrule

== Runtime failures
<errors-runtime-failures>
#idx("runtime channel")
System-owned error names, meanings, and construction restrictions are in
#secref(<errors-the-built-in-error-catalog>). The runtime may recover a failed remote operation before the
program observes it. When recovery is exhausted, the program receives the
applicable system error. Once `Unverified!` is determined for an invocation,
the runtime makes no further automatic attempt of it. An explicit later call
by the program is a new invocation. An invocation that is checked as unable
to mutate and that ends without a verdict yields `Cancelled!` when the
budget ended it and `Unavailable!` otherwise.

The program matches an exhausted mechanism failure in `handle` like any
other error:

```ritchie
Quote q = @Pricing.quote(sku) handle {
    Unavailable! => cachedQuote(sku)     // runtime gave up reaching the service
}
```

#horizontalrule

== Unhandled errors
<errors-terminal-disposition>
An error no arm recovers returns to the originating caller, frame by
frame, until a handler takes it or it reaches the operation's boundary.

Each service invocation logs its escaping error once, then returns that
error as its outcome. The local report includes the history retained
inside the invocation, before outgoing module-boundary compaction
(#secref(<errors-raising-and-propagation>)). The caller receives the compacted error and may recover
it or propagate it further. A runtime error keeps its type when it escapes a
service invocation, except that a runtime error confirming relevant state
unaltered reaches the caller as `Unverified!` when the invocation may have
altered relevant state. A caller's recovery does not undo the
service's report. A failure passing through several service invocations
can therefore produce one report at each invocation.

A failure inside a recovery attempt can also end unhandled, as specified
in #secref(<errors-fallback-and-nested-handling>). Each report belongs to the invocation or recovery path
ending there.

An error escaping `main` is reported once on the program's diagnostic
output and the process exits with status `1`. The report identifies the
terminal error type exactly once. Statements following the failed
operation do not execute. Additional diagnostic detail belongs to the
same report.

Reporting is infallible, like #secref(<builtins-diagnostics>).
Each report requires one reporting attempt, not guaranteed delivery to
a failed diagnostic destination. A write failure leaves the error
outcome, history, process status, and further handling unchanged; it
does not raise another error or trigger recursive reporting.

The platform selects which of the reports in this chapter a program
writes, and whether a report shows the error's history steps. A report
that the platform does not select is not written, and the error's
outcome, history, and the process status do not change.

#horizontalrule

== Recover, bubble, or log
<errors-recover-bubble-or-log>
At each disposition, choose one action: recover the error, bubble it, or log
it where its path terminates (#secref(<errors-fallback-and-nested-handling>, <errors-terminal-disposition>, conj: "and")). A service invocation's
report and its caller's handling of the returned outcome are separate
dispositions. Logging an unrecovered secondary failure ends that
failure's path. Continuing outward with the original error is a separate
disposition.

An explicit error return is bubbling. Returning a fresh error from an arm
re-expresses the active failure while preserving its history. Returning an
already-raised error selects that error's existing chain (#secref(<errors-re-expression-and-history>)).

An error type must be actionable: a handler must be able to respond to it
differently from another. Information that needs no distinct response
belongs in fields or telemetry.

#horizontalrule

== The built-in error catalog
<errors-the-built-in-error-catalog>
#idx("Unavailable!", code: true)
#idx("Rejected!", code: true)
#idx("Unverified!", code: true)
#idx("Conflict!", code: true)
#idx("Unmodified!", code: true)
#idx("Cancelled!", code: true)
#idx("NotFound!", code: true)
#idx("Duplicate!", code: true)
#idx("Frozen!", code: true)
#idx("Malformed!", code: true)
#idx("OutOfRange!", code: true)
#idx("InvalidStringBoundary!", code: true)
#idx("DivisionByZero!", code: true)
#idx("MissingKey!", code: true)
#idx("LengthMismatch!", code: true)
#idx("error catalog")
The language supplies a closed set of error types:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Error], [Raised by],),
    table.hline(),
    table.cell(colspan: 2)[#emph[Runtime errors]],
    [`Unavailable!`], [recovery ended without a result; relevant
    state confirmed unaltered],
    [`Rejected!`], [the runtime rejected the invocation for a code
    or configuration defect; relevant state unaltered],
    [`Unverified!`], [a mutation-capable flow may have altered
    relevant state; final effect unestablished],
    [`Conflict!`], [an entity invocation exhausted contention;
    state unchanged],
    [`Unmodified!`], [the budget ended a mutation-capable call
    before it attempted any modification; relevant state unaltered],
    [`Cancelled!`], [the budget ended a call that cannot mutate
    before it gave a result; relevant state unaltered],
    table.hline(),
    [`NotFound!`], [an entity lookup that matches no instance],
    [`Duplicate!`], [a unique-key collision at creation or mutation],
    [`Frozen!`], [a mutating method on a sealed instance],
    [`Malformed!`], [a text-to-value conversion that cannot
    parse its input, or invalid UTF-8 supplied to `String.fromBytes`],
    [`MissingKey!`], [checked dictionary access to an absent key],
    [`LengthMismatch!`], [`zip` with unequal input lengths],
    [`OutOfRange!`], [a sequence index or slice out of bounds;
    a conversion, temporal result, or `Decimal` result the target type
    cannot hold],
    [`InvalidStringBoundary!`], [an in-range String slice
    bound that divides a UTF-8 encoding],
    [`DivisionByZero!`], [integer or `Decimal` `/` or `%`, or
    `Duration` division, with a zero divisor],
  )]
  , kind: table
  )

Application code cannot construct a catalog error, and a declared error
set does not list one (#secref(<errors-the-checked-error-contract>)).

The runtime errors are the mechanism channel
(#secref(<errors-two-channels>, <errors-runtime-failures>)). They are
system-defined, globally reserved and unqualified. A remote call can raise
each of them in addition to the callee's inferred raise set.

The catalog names are reserved. A user declaration of any of them is a
compile error. System errors have no typed fields of their own.

A statement that fails in a collection value read or `zip` call recovers
like any other failed statement (#secref(<errors-inline-handle>)). A failed
validation of a mutable place, whether an array element or a dictionary
entry and including a method receiver path, permits only diversion or
return. Recovery does not implicitly create an entry or retry the operation.
