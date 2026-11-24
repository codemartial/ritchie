#import "../prelude.typ": horizontalrule, idx, secref, part-numbering

#set heading(numbering: part-numbering)

== Error Handling -- Examples
<chap-errors-examples>
Ten worked examples for the rules in #link(<chap-errors>)[Errors]. The
section number in each note points there.

=== Simple propagation
<errors-examples-simple-propagation>
```ritchie
priceOrder(Order order) -> Money {
    Money sub = subtotal(order.lines)       // a raise here bubbles bare
    Money tax = salesTax(sub, order.region)
    return sub + tax
}
```

If `subtotal` or `salesTax` raises, the error short-circuits the rest and
returns to the caller unchanged. The raise set is inferred
(#secref(<errors-raising-and-propagation>, <errors-the-checked-error-contract>)).

=== Single-error inline handle
<errors-examples-single-error-inline-handle>
```ritchie
Decimal rate = lookupRate(order.region) handle RateUnavailable! => 0.0
Money tax = order.subtotal * rate
```

The handled expression yields either a rate or `RateUnavailable!`. The
arm substitutes `0.0`, which is assigned to `rate`, and execution continues.
Any other error bubbles via the inserted fallback (#secref(<errors-inline-handle>, <errors-fallback-and-nested-handling>)).

=== Multiple-error inline handle
<errors-examples-multiple-error-inline-handle>
```ritchie
Shipment s = book(order) handle {
    NoCarrier!  => book(order, fallbackCarrier)
    AddressBad! => return flagForReview(order)
    any e       => return e
}
```

Three arms: recover with a substitute, divert with `return`, or bubble
the rest bare. An arm names the matched error type-first when it needs
it (`any e`), narrowed to the arm's type (#secref(<errors-inline-handle>)).

=== Block-level handle
<errors-examples-block-level-handle>
```ritchie
{
    reserve(order.item, order.qty)
    charge(order.customer, order.total)
    ship(order)
} handle {
    OutOfStock!        => return cancel(order)
    InsufficientFunds! => return decline(order)
    any                => return abort(order)
}
```

The three statements share a handler. Here every arm `return`s, exiting
the function. These statements have no results, so no arm can supply
values for them. With the handler on an enclosing block,
`{ { ... } } handle { ... }`, the handler would protect the inner block as
a whole, so a no-op arm would end the work at the first failure and keep the
completed work (#secref(<errors-block-and-loop-handling>)).

=== Loop-level handle
<errors-examples-loop-level-handle>
```ritchie
for line in order.lines {
    applyDiscount(line)
} handle {
    any => return
}
```

These local calls execute in sequence. `return` exits the function on
the first error. `continue` would instead go on with the next line
(#secref(<errors-block-and-loop-handling>)).

=== Nested handle
<errors-examples-nested-handle>
```ritchie
{
    Money sub = subtotal(order.lines)
    Decimal rate = lookupRate(order.region) handle { RateUnavailable! => 0.0 }
    Money total = sub + sub * rate
    record(order, total)
} handle {
    EmptyOrder! => return reject(order)
    any         => return abort(order)
}
```

The inline `handle` recovers `rate` locally. `EmptyOrder!` from
`subtotal`, and anything else, falls to the block-level `handle`
(#secref(<errors-inline-handle>, <errors-block-and-loop-handling>)).

=== Error with context fields
<errors-examples-error-with-context-fields>
```ritchie
LimitExceeded! { Money attempted, Money ceiling }

$Account.withdraw(Money amount) -> Receipt | LimitExceeded! {
    if amount > self.dailyCeiling {
        return LimitExceeded! { attempted: amount, ceiling: self.dailyCeiling }
    }
    self.balance = self.balance - amount
    return Receipt.new(amount)
}
```

Fields carry structured detail a handler reads after matching the type.
Branching is still on `LimitExceeded!`\; the fields inform the recovery,
not the match (#secref(<errors-raising-and-propagation>), #secref(<types-error-types>)).

=== Declared error set and the dead-arm guard
<errors-examples-checked-contract-and-the-dead-arm-guard>
```ritchie
$Invoice.settle(Payment p) -> Receipt | AlreadyPaid! | Underpaid! {
    ...
}

Receipt r = $Invoice.settle(p) handle {
    Underpaid! => requestBalance(p)
    Overpaid!  => refundDifference(p)    // compile error: settle cannot raise Overpaid!
}
```

`settle` declares its two errors, and the compiler checks that both can
escape it. The `Overpaid!` arm is rejected at compile time -- the inferred
raise set of the call does not contain it, so the arm is dead.
`AlreadyPaid!`, unmatched, bubbles via the fallback
(#secref(<errors-the-checked-error-contract>, <errors-fallback-and-nested-handling>)).

=== Re-expression at a domain crossing
<errors-examples-re-expression-at-a-domain-crossing>
```ritchie
@Orders.place(Cart cart) -> Confirmation | OrderFailed! {
    Receipt r = @Payment.charge(cart.customer, cart.total) handle {
        any => return OrderFailed!
    }
    return confirm(cart, r)
}
```

`@Payment` and `@Orders` are different domains. The payment service's
declared error becomes the active error in the handler. Returning
`OrderFailed!` re-expresses it while preserving every prior step in
history. Upstream handlers branch on the typed current `OrderFailed!`
and may iterate its diagnostic history (#secref(<errors-re-expression-and-history>)).

=== Runtime failure
<errors-examples-runtime-failure>
```ritchie
Quote q = @Pricing.quote(sku) handle {
    Unavailable! => cachedQuote(sku)
}
```

`Unavailable!` is system-owned, surfacing only after the runtime
exhausted its own retries against `@Pricing`. The author declares it
nowhere and matches it like a domain error. A mutation behind such a
call is deduplicated by the runtime on the interaction's identity, so a
retry does not double-apply (#secref(<errors-runtime-failures>), #link(<chap-effects>)[Effects]).
