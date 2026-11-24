#import "../prelude.typ": horizontalrule, idx, secref, part-numbering

#set heading(numbering: part-numbering)
== Entities -- Examples
<chap-entities-examples>
Twelve worked examples for the rules in
#link(<chap-entities>)[Entities]. The section number in each note
points there. Examples 10 to 12 drive entities from service methods
(#link(<chap-services>)[Services]).

=== Declaration
<entities-examples-declaration>
```ritchie
$Customer {
    String  name
    unique String email
    Decimal creditLimit = 500.00
}
```

A persistent type with a runtime-minted `id` (implicit rather than declared), one
unique domain key, and a defaulted field (#secref(<entities-what-an-entity-is>, <entities-identity>, <entities-unique-keys>)).

=== Creation inside the module
<entities-examples-creation-inside-the-module>
```ritchie
$Customer c = create $Customer { name: "Ada", email: "ada@example.com" }
log(c.id)                       // id exists the moment creation returns
```

The expression commits durably before the variable receives the reference.
`creditLimit` takes its default (#secref(<entities-creation>)).

=== Exported creation method
<entities-examples-exported-creation-method>
```ritchie
$Customer.new(String name, String email) -> $Customer {
    return create $Customer { name: name, email: email }
}

$Customer c = crm::$Customer.new("Ada", "ada@example.com") handle {
    Duplicate! => return $Customer[existingId]
}
return c
```

Outside the module raw creation is unavailable, so creation goes through
the exported method. A colliding email raises `Duplicate!` (#secref(<entities-creation>, <entities-unique-keys>)).

=== Lookup
<entities-examples-lookup>
```ritchie
$Order order = $Order[orderId]
order.confirm()
```

A remote fetch that raises `NotFound!` when no such instance exists and
`Unavailable!` when it cannot be performed. Unhandled, either bubbles
before `confirm` runs (#secref(<entities-lookup>)).

=== Failed lookup, handled
<entities-examples-failed-lookup-handled>
```ritchie
$Order order = $Order[orderId] handle {
    NotFound! => return Declined! { reason: "unknown order" }
}
order.confirm()
```

The miss is re-expressed in the caller's vocabulary at the lookup site.
`Unavailable!` falls through the inserted fallback and bubbles (#secref(<entities-lookup>)).

=== Get or create
<entities-examples-get-or-create>
```ritchie
$Customer c = $Customer[email: email] else create $Customer {
    name: name
} then {
    log(f"created {c.id}")
}
```

The key is injected, the operation returns a reference on either
outcome, and `then` runs only for creation. `c` is assigned inside the
continuation (#secref(<entities-lookup>)).

=== Method with mutation
<entities-examples-method-with-mutation>
```ritchie
$Order.applyDiscount(Decimal rate) -> none | Closed! {
    if self.status == Status.CLOSED {
        return Closed!
    }
    self.discount = self.total * rate
    self.total    = self.total - self.discount
}
```

Both writes commit together or not at all. The error return is the
abort: nothing is written when `Closed!` surfaces (#secref(<entities-methods-and-transactions>)).

=== Same-type participants
<entities-examples-same-type-participants>
```ritchie
$Account.pay($Account to, Decimal amount) -> none | InsufficientFunds! {
    if amount > self.balance {
        return InsufficientFunds!
    }
    self.balance = self.balance - amount
    to.balance   = to.balance + amount
}

$Account[from].pay($Account[to], 25.00) handle {
    Conflict! => return retryLater(from, to)
}
```

One transaction over `self` and `to`: serializable, atomic,
non-blocking. Contention beyond the budget surfaces as `Conflict!` with
state unchanged (#secref(<entities-methods-and-transactions>)).

=== Entities in collections
<entities-examples-entities-in-collections>
```ritchie
$Account[] audited = [primary, reserve, escrow]
for acct in audited {
    acct.assessFee(monthlyFee)
}
Bool same = audited[0] == primary       // true: id equality
```

The array holds references, so `assessFee` mutates the one instance each
element names, and equality is by id (#secref(<entities-references-and-mutation-visibility>, <entities-entity-vs-struct>)).

=== Creation from a service
<entities-examples-creation-from-a-service>
```ritchie
@Onboarding.register(String name, String email) -> UUId {
    $Customer c = $Customer.new(name, email)
    return c.id
}
```

The service drives creation and returns the identity (#secref(<entities-creation>),
#secref(<services-services-and-entities>)).

=== Lookup and method call from a service
<entities-examples-lookup-and-method-call-from-a-service>
```ritchie
@Support.creditGoodwill(UUId customerId, Decimal amount) -> none | LimitExceeded! {
    $Customer c = $Customer[customerId]
    c.credit(amount)
}
```

Both the lookup and the method call are remote operations. Each failure
either matches the declared contract and bubbles, or is handled in the
body (#secref(<entities-lookup>), #secref(<services-calling-a-service>, <services-services-and-entities>)).

=== Multiple entity types in one method
<entities-examples-multiple-entity-types-in-one-method>
```ritchie
@Fulfilment.ship(UUId orderId) -> Tracking | NotPacked! {
    $Order order = $Order[orderId]
    Shipment s = order.releaseForShipping()
    $Courier courier = $Courier[s.courierId]
    return courier.book(s)
}
```

Cross-type orchestration is exactly what a service is for -- each
entity call is atomic on its own type, and the service sequences them
(#secref(<entities-methods-and-transactions>), #secref(<services-services-and-entities>)).
