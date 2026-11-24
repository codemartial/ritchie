#import "../prelude.typ": horizontalrule, idx, secref

= Effects and Resolution
<chap-effects>
When work leaves the local frame, when its result is waited on, and how
parallelism arises. Remote reads -- calls of service methods and of
queries that cannot change remote state -- are the only work that
overlaps. This chapter fixes exactly when they do and where their failures
take effect.

== Remote operations
<effects-remote-operations>
#idx("remote operation")
These expression forms cross an effect boundary:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Operation],),
    table.hline(),
    [`create $Type { ... }`], [entity creation],
    [`$Type[id]`], [entity lookup],
    [`$Type[key: value] else create $Type { ... }`], [atomic entity
    get-or-create],
    [`ref.method(args)`], [entity method call],
    [`@Service.method(args)`], [service method call],
    [`%query(args)`], [query call],
  )]
  , kind: table
  )

\(Creation inside an entity method joins that invocation rather than
starting another remote operation.) A query call reads the read model
and is admitted wherever a service method
call is (#link(<chap-queries>)[Queries]). Everything else --
arithmetic, local calls, struct work, collection work -- is local and
completes in place.

A remote operation is the unit the rest of this file is about: it takes
real time, it can fail on the runtime channel, it carries a budget, and
it is the only thing that ever runs concurrently with the statements
around it.

== Results and order
<effects-the-sequential-meaning>
#idx("sequential execution")
#idx("overlap")
Ritchie programs execute as single-threaded logic with asynchronous
execution of external operations (eg. invocation of methods on
services, entities or projections). The runtime can optimise latency
by dispatching multiple executions in parallel. This section describes
the conditions of such overlapping operations.

A function reads as one statement after another. An operation is
#emph[earlier] than another when it precedes it in that reading.

A remote operation is a #emph[read] when its method cannot change remote
state and a #emph[mutation] otherwise. A #emph[remote read] is a service
method call or a query call whose method cannot change remote state. An
#emph[entity operation] is an entity method call, lookup, creation, or
get-or-create. A #emph[target] is an entity type or a service.

In every routine body, a remote read does not hold up the statements after
it, and the program waits where it uses the result. Every other remote
operation is dispatched and waited for at its statement
(#secref(<effects-overlap-and-its-limits>)).

== Futures
<effects-overlap-and-its-limits>
#idx("parallelism")
#idx("future")
The rules below apply in every routine body. The rules fix the result of a
routine. An implementation may dispatch a remote read earlier than the
rules say, and the result is the same.

+ #strong[A read returns a future.] A remote read is dispatched when the
  program reaches its statement. Its result is a #emph[future]: a value
  that is not yet known.
+ #strong[A move does not wait.] A #emph[move] puts a future into a
  local variable of the same type, stores it as an element of a local
  collection by a store that does not read any element value, or binds
  it to the variable of a loop over a local collection of futures.
+ #strong[Every other use waits.] A use that is not a move first waits
  for every future that the routine created before that future and has
  not yet waited for, in the order of creation, and then for that
  future. The program then has the value, or the first failure in that
  order takes effect at that point.
+ #strong[An effect waits for everything before it.] A mutation, every
  entity operation, an output, a wait on the clock
  (#secref(<stdlib-clock>)), a call of a local routine that can
  mutate, write output, or wait on the clock, a call of a function
  value, each statement that leaves the routine (its end, and a
  `return` of a value or of an error), and a failure of a local
  operation or of a local call that leaves the routine are
  #emph[effects]. An output is a call of `print`, and a call of `log`
  is not an effect (#secref(<builtins-diagnostics>)). Each effect first
  waits for every future that the
  routine has created and has not yet waited for, in the order of
  creation, and the first failure in that order takes effect. A
  mutation or an entity operation is then dispatched, and the program
  waits for it at its statement. A local routine can mutate, write
  output, or wait on the clock when its body does, or when the body of
  a local routine that it calls does; the output of a remote method
  is not the output of its caller.
+ #strong[A handler that supplies a known value belongs to the future.]
  On a statement with one read, a handler whose every arm supplies a known
  value, such as `handle any => v` or `handle Low! => v`, belongs to the
  future. A known value is a literal, including a collection literal of
  literals, or a variable that holds no future. A failed response that an
  arm selects makes the future resolve to the value that the arm has at the
  statement, and a failure that no arm selects stays the failure of the
  future. A call with any other handler is waited for at its statement,
  because the handler can exit, perform an effect, or compute.
+ #strong[An error that leaves the routine cancels what is in flight.] No
  future outlives its routine.

```ritchie
@Gauge {}
@Gauge.level(Int id) -> Int { r::Clock.sleep(50ms); return id }
@Gauge.trend(Int lv) -> Int { r::Clock.sleep(50ms); return lv * 2 }
@Gauge.note(Int id) -> Int { r::Clock.sleep(50ms); return id + 1 }

main() {
    Int a = @Gauge.level(1)     // dispatched at 0 ms
    Int c = @Gauge.note(1)      // dispatched at 0 ms
    Int b = @Gauge.trend(a)     // waits for a at 50 ms, then dispatched
    print(a + b + c)            // waits for b at 100 ms; prints 5
}
```

The program takes 100 ms, and the same statements in strict sequence take
150 ms.

== Demand points
<effects-demand-points>
#idx("demand point")
#idx("demand")
Every use of a future waits for it, except a move
(#secref(<effects-overlap-and-its-limits>)). The table lists the uses of a
future `f` by the rule that applies to them.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Moves], [Waits],),
    table.hline(),
    [`Int a = f` and `a = f` (a local variable) \
    `[f, g]`, `xs = append(xs, f)`, and `xs[i] = f` (an element of a local
    collection) \
    `for x in xs` over a local collection of futures (binds the loop
    variable) \
    a call statement whose result is unused (nothing uses the value)],
    [`Int? a = f` and any store that changes the type (converts the value) \
    `f + 1`, `f == 0`, `-f`, `f"{f}"`, and `a += f` (computes on the
    value) \
    `if f`, `match f`, and `for x in f` (decides control) \
    `f.field`, `f.method()`, `f[i]`, `xs[f]`, and `d[f]` (looks inside, or
    selects by the value) \
    an argument of a remote call or of a local routine, a field of a struct
    literal, `return f`, the response of a method, and capture by a closure
    (the value leaves the routine)],
  )]
  , kind: table
  )

An operation over a local collection of futures whose result does not
depend on any element value, such as `count(xs)`, does not use a
future and does not wait.

A future lives only in the local variables and local collections of the
routine that created it. Every argument is waited for before a remote operation
sends it, and every result is waited for before a method sends its
response. A pending representation does not cross a logical unit boundary.

=== Unused results
A remote operation may stand alone as a statement, and its result may go
unused. A declared variable that no expression reads is a compile
error for every initializer (#secref(<statements-variable-declarations>)).
A call statement whose result is unused is a move, so the program does not
wait for it at its statement. Its failure takes effect at the next wait of
rule 3 or of rule 4: the next use of a later future, the next effect, or
the end of the routine. An error that leaves the routine first cancels the
call.

```ritchie
@Pricing.quote(sku, qty)             // a move: nothing uses the result
Stock s = @Inventory.check(sku)      // dispatched with the quote
print(s.count)                       // waits for the quote, then for s
```

If the quote fails, its failure takes effect before `print` writes.

== Where failures take effect
<effects-failures-in-written-order>
#idx("abandonment")
A failure takes effect where the program waits for the future, whatever the
time at which its response arrives
(#secref(<effects-overlap-and-its-limits>)). A use and an effect wait for
the futures in the order of creation, so when several futures have failed,
the failure of the future that was created first takes effect. A call with a handler other than a known-value handler
is waited for at its statement, and a failure of a call with a known-value
handler does not take effect. The handlers that receive a failure are the
handlers that protect the statement of the failed operation
(#secref(<errors-fallback-and-nested-handling>)). A failure that takes
effect at a later statement therefore leaves the routine, and a handler of
that later statement does not receive it. No mutation written after the
failed operation is dispatched unless a recovery completes its statement.
A mutation in an arm body is an effect and waits as any effect does.

An arm refers only to variables defined before the earliest statement it
covers, and it sees their values as of that statement. The compiler accepts
a read in an arm only when every path reaching the arm has initialized the
variable.

How a no-op arm, `continue`, `break`, and `return` dispose of the recovered
statement, and which statements each handler protects, are specified in
#secref(<errors-block-and-loop-handling>). Every disposition keeps
completed work (#secref(<effects-scope-exit>)).

`Rejected!` carries arms like `Unavailable!` and has no effect. A no-op arm
covering `Unverified!` on a mutation that returns nothing is valid, because
the arm is the program's observation of the verdict.

== Scope exit
<effects-scope-exit>
A scope exits by its last statement, by `break`, by `continue`, by
`return`, or by an error leaving it. A statement that the exit skips does
not run, so it creates no future.

A statement that leaves the routine, by its end or by `return`, first
waits for every future that the routine has created and has not yet waited
for, in the order of creation, and the first failure in that order takes
effect in place of the exit (rule 4 of
#secref(<effects-overlap-and-its-limits>)). An error that leaves the
routine cancels every future of the routine that is in flight, and no
future outlives its routine (rule 6).

Nothing is left half applied: an entity invocation
either commits atomically or does not, and a committed step of an
abandoned sequence stays committed.

These guarantees exclude the effect of an `Unverified!` mutation. Such a
mutation may commit after its response, and neither its effect nor the time
of its effect is covered. Later mutations of the same target proceed after
a handled `Unverified!`, and a read after it is not conclusive about its
effect.

== The runtime failure channel
<effects-the-runtime-failure-channel>
#idx("Unavailable!", code: true)
#idx("Conflict!", code: true)
The system-owned runtime errors and their meanings are in
#secref(<errors-the-built-in-error-catalog>).

An operation checked as unable to mutate cannot yield `Unverified!` from
its own execution; `Conflict!` is confined to entity invocations.

On the terminal error of a bubbling chain the runtime records the
raising method and a rendered snapshot of its arguments
(#secref(<errors-raising-and-propagation>)). Arguments are rendered at
raise using their default display, with entity references represented by
their ids, and are retained as text rather than live values. The recorded
method's lexical module is the formatting module.

Caller-supplied argument values are not additionally redacted when
returned to that caller as diagnostic metadata. Default display follows
#link(<chap-types>)[Types]; field visibility does not suppress displayed
contents. History compaction follows the published module boundary in
#secref(<errors-raising-and-propagation>), independently of service-invocation
reporting. A service invocation within the same module introduces no
additional compaction boundary.

== Time budgets
<effects-time-budgets>
#idx("budget")
Every remote operation has a finite ambient budget, omitted from
signatures. An entry receives its configured budget when its execution
begins. Each remote operation receives a budget no longer than the
remaining budget of its caller, and the handler of the operation works
within that budget.

A remote operation that has no response when its budget ends settles
as `Cancelled!` for a read, and `Unverified!` for a mutation
(#secref(<errors-the-built-in-error-catalog>)). The program handles
that error at the operation like any other error. A remote operation
that would start after its budget is exhausted does not start, and it
observes `Cancelled!` if it is a read and `Unmodified!` if it is a
mutation.  Per-call budget syntax remains reserved.

== What remains sequential
<effects-what-remains-sequential>
For completeness, the forms that take no part in any of this:

- #strong[Entity method bodies.] A body performs no service calls, no
  query calls, and no foreign-entity work, and its same-type lookups and
  calls execute within the enclosing invocation
  (#secref(<entities-methods-and-transactions>)), so a body
  creates no future.
- #strong[Constant and module initialization.] Declaration-level
  initializers are local expressions, evaluated before the module serves
  anything (#link(<chap-declarations>)[Declarations]).
