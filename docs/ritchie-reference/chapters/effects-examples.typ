#import "../prelude.typ": horizontalrule, idx, secref, part-numbering

#set heading(numbering: part-numbering)
== Effects -- Examples
<chap-effects-examples>
Worked examples for the rules in #link(<chap-effects>)[Effects]. The
section number in each note points there.

=== Independent reads overlap
<effects-examples-independent-reads-overlap>
```ritchie
Quote q = @Pricing.quote(sku, qty)
Stock s = @Inventory.check(sku)
return Offer.new(q, s)
```

Each read is dispatched where it is written, and neither statement waits
for the other. The `return` is an effect and waits for both. Total time
may be as short as the longer of the two
(#secref(<effects-overlap-and-its-limits>)).

=== A mutation waits for the reads before it
<effects-examples-a-mutation-waits-for-the-reads-before-it>
```ritchie
Quote q = @Pricing.quote(sku, qty)
@Ledger.record(sku, qty)
return q
```

A mutation is an effect, so it first waits for the quote. When the quote
fails, its failure takes effect and nothing is recorded. Otherwise the
mutation is dispatched after the quote has returned, and the program waits
for the mutation at its statement
(#secref(<effects-overlap-and-its-limits>)).

=== Written order on one target
<effects-examples-written-order-on-one-target>
```ritchie
$Account payer = $Account[customerId]
Receipt r = payer.charge(total)
Balance b = payer.balance()
return Summary.new(r, b)
```

Every entity operation is an effect, so the lookup, the charge, and the
balance call are each dispatched and waited for at their statements. The
balance call is dispatched after the charge has returned and observes it
(#secref(<effects-overlap-and-its-limits>)).

=== A move does not wait
<effects-examples-a-move-does-not-wait>
```ritchie
Quote q = @Pricing.quote(sku, qty)
Quote best = q                       // a move: no wait
Stock s = @Inventory.check(sku)      // dispatched with the quote
if best.total > limit {              // waits for the quote here
    return escalate(best, s)         // arguments of a local routine: wait for s
}
```

Storing the future in `best` moves it, as storing it in an element of a
local collection does. The field access in the condition is the first use that
waits (#secref(<effects-demand-points>)).

=== A handler that is not a known value waits at its statement
<effects-examples-a-handler-that-is-not-a-known-value>
```ritchie
for line in order.lines {
    @Tax.assess(line) handle {
        any => continue
    }
}
```

An arm that is not a known value can exit, perform an effect, or compute,
so each assessment is waited for at its statement and the next one is
dispatched after the previous one has settled. With a value handler such
as `Int n = @Tax.assess(line) handle any => 0`, the failed response makes
the future resolve to `0`, and the program does not wait at the statement
(#secref(<effects-overlap-and-its-limits>)).

=== continue skips the rest of the iteration
<effects-examples-continue-skips-the-iteration>
```ritchie
for order in orders {
    @Audit.record(order)
    @Ledger.post(order.total)
} handle {
    Unavailable! => continue
}
```

Both calls are mutations, and the program waits for each at its statement.
When an audit record fails, the posting for the same order is not
dispatched and the next order is processed. No two mutations are in flight
together
(#secref(<errors-block-and-loop-handling>, <effects-overlap-and-its-limits>)).

=== A return waits for the reads of its routine
<effects-examples-a-return-waits-for-the-reads-of-its-block>
```ritchie
@Inventory.check(sku)
return computeLocal(sku)
```

The call of `computeLocal` runs while the check is in flight, because it
is not an effect. The
`return` leaves the routine, so it first waits for the check. If the check
fails, its failure takes effect in place of the return
(#secref(<effects-scope-exit>)).

=== A loop that uses a result in its body runs in sequence
<effects-examples-a-loop-that-uses-a-result-runs-in-sequence>
```ritchie
@Gauge {}
@Gauge.level(Int id) -> Int { r::Clock.sleep(50ms); return id }

main() {
    Int sum = 0
    for id in [1, 2, 3] {
        Int lv = @Gauge.level(id)
        sum += lv               // waits in every iteration
    }
    print(sum)                  // 6, after 150 ms
}
```

The compound assignment uses `lv`, so each iteration waits for its read
before the next iteration dispatches
(#secref(<effects-overlap-and-its-limits>, <effects-demand-points>)).

=== A loop that collects futures fans out
<effects-examples-a-loop-that-collects-futures-fans-out>
```ritchie
@Gauge {}
@Gauge.level(Int id) -> Int { r::Clock.sleep(50ms); return id }

main() {
    Int sum = 0
    Int[] levels = []
    for id in [1, 2, 3] {
        levels = append(levels, @Gauge.level(id))   // three dispatches at 0 ms
    }
    for lv in levels {
        sum += lv                                   // waits in order
    }
    print(sum)                                      // 6, after 50 ms
}
```

The first loop moves each future into the local array, so it waits for
nothing. The second loop binds `lv` to each element and waits at the
compound assignment
(#secref(<effects-overlap-and-its-limits>, <effects-demand-points>)).

=== Conflict under contention
<effects-examples-conflict-under-contention>
```ritchie
$Account[from].pay($Account[to], amount) handle {
    Conflict! => return Busy! { account: from }
}
```

Concurrent payments touching the same accounts serialize. When contention
exhausts the budget, `Conflict!` is raised with state unchanged, a signal
to retry later rather than to inspect damage
(#secref(<effects-the-runtime-failure-channel>)).

=== Budget exhaustion
<effects-examples-budget-exhaustion>
```ritchie
Confirmation c = @Checkout.place(cart) handle {
    Unmodified! => return Queued.new(cart)    // did not run: resubmit
    Unverified! => return Pending.new(cart)   // may have run: reconcile
}
return c
```

When the budget ends the call before it attempts any modification, the arm
resubmits the cart. When the caller's wait ends without a response, the
order may exist, so the arm parks the cart for reconciliation instead of placing it again. Each
entity step inside was atomic, so nothing is half-applied (#secref(<effects-the-runtime-failure-channel>, <effects-time-budgets>)).
