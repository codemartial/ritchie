#import "../prelude.typ": horizontalrule, idx, secref, part-numbering

#set heading(numbering: part-numbering)

== Services -- Examples
<chap-services-examples>
Six worked examples for the rules in #link(<chap-services>)[Services].
The section number in each note points there. Examples share a module
unless a name is module-qualified.

=== Declaration
<services-examples-declaration>
```ritchie
@Pricing { }

@Pricing.quote(String sku, Int qty) -> Quote | UnknownSku! {
    ...
}
```

The body is empty by rule. Methods on the `@` type are the published
surface (#secref(<services-what-a-service-is>, <services-service-methods>)).

=== Method body and helpers
<services-examples-method-body-and-helpers>
```ritchie
discount(Int qty) -> Decimal {
    if qty >= 100 { return 0.15 }
    if qty >= 10  { return 0.05 }
    return 0.00
}

@Pricing.quote(String sku, Int qty) -> Quote | UnknownSku! {
    Decimal unit = listPrice(sku) handle {
        NotListed! => return UnknownSku!
    }
    return Quote.new(sku, qty, unit * Decimal.fromInt(qty) * (1.00 - discount(qty)))
}
```

The method computes from its arguments alone, and nothing survives the
call. `discount` and `listPrice` are private helpers; only the `@`
methods are callable from outside (#secref(<services-statelessness>, <services-service-methods>)).

=== Calling from client code
<services-examples-calling-from-client-code>
```ritchie
Confirmation conf = shop::@Checkout.place(cart) handle {
    OutOfStock!    => return backorder(cart)
    PaymentFailed! => return declined(cart)
}
return conf
```

A caller in another module names the service through its module and
handles the errors the method can raise. Anything unmatched bubbles (#secref(<services-calling-a-service>)).

=== Runtime failure
<services-examples-runtime-failure>
```ritchie
Quote q = @Pricing.quote(sku, qty) handle {
    Unavailable! => cachedQuote(sku, qty)
}
```

`Unavailable!` is system-defined, yet belongs to every service call's
reachable failures: the runtime exhausted its own recovery (#secref(<services-calling-a-service>)).

=== Service composing services
<services-examples-service-composing-services>
```ritchie
@Storefront.buy(Cart cart) -> Receipt | PaymentFailed! | OutOfStock! {
    Confirmation conf = @Checkout.place(cart)
    @Notify.send(cart.customerEmail, f"order {conf.code} placed")
    return Receipt.new(conf)
}
```

A service call from a service is the same form and semantics as from
anywhere else. `@Checkout.place`'s declared errors match
`@Storefront.buy`'s contract and bubble through. `@Notify.send`
declares no domain errors, so only the runtime channel can fail it, and
that channel needs no declaration (#secref(<services-calling-a-service>), #secref(<effects-the-runtime-failure-channel>)).

=== Budget exhaustion
<services-examples-budget-exhaustion>
```ritchie
Receipt r = @Storefront.buy(cart) handle {
    Unmodified! => return tryLater(cart)
    Unverified! => return pendingConfirmation(cart)
}
```

The call's time budget ran out. `Unmodified!` means the work did not
run; `Unverified!` means its outcome is unknown, so the caller can
assume neither completion nor absence of effect (#secref(<services-time-budgets>)).
