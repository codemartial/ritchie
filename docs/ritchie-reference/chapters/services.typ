#import "../prelude.typ": horizontalrule, idx, secref

= Services
<chap-services>
A service is a stateless, remotely callable unit -- a published set of
methods that orchestrate entities, call other services, and reach the
outside world. This chapter covers declaration, statelessness, logical
invocation, and time budgets. The error behavior a service presents is
in #link(<chap-errors>)[Errors]. How its calls overlap is in
#link(<chap-effects>)[Effects].

== What a service is
<services-what-a-service-is>
#idx("service")
A service is a published, remotely callable unit with no state of its
own, declared with the `@` sigil:

```ritchie
@Billing { }
```

The body is always empty -- a service has no fields, because it has no
state. Its substance is its methods, declared at module level on the `@`
type:

```ritchie
@Billing.invoice($Order# orderId) -> Invoice | Closed! {
    $Order order = $Order[orderId]
    OrderSummary s = order.summarize()
    Decimal tax = computeTax(s.region, s.total)
    return Invoice.new(s, tax)
}
```

A service is where impure, cross-cutting work lives: orchestrating
entities of different types, calling other services, and reaching the
outside world. Everything an entity method is forbidden, a service
method may do within the entity types its module owns. Work owned by
another module is reached through that module's service methods.

== Statelessness
<services-statelessness>
#idx("statelessness")
A service owns no state between calls. Its methods do not use `self`
because there is no service instance.

== Service methods
<services-service-methods>
#idx("service method")
A service method is declared like any method, with the `@` type as
receiver. Like any function, it may declare an error set in its return
signature (#secref(<errors-the-checked-error-contract>)):

```ritchie
@Checkout.place(Cart cart) -> Confirmation | OutOfStock! | PaymentFailed! {
    ...
}
```

- A declared error set is checked as on any function. A caller handles the
  method's inferred raise set together with the runtime errors of a remote
  call (#secref(<services-calling-a-service>)).
- Parameters and results are public values, including entity identities.
  They cross a logical service boundary, so every parameter and result type must
  be visible to any module and have value semantics: values, structs, enums,
  collections of these, errors, and entity identities. Function types and
  entity references do not cross a service boundary. The check is recursive -- through
  fields, type arguments, dict values, and nullable wrappers -- and
  applies at the method declaration.
- Free-standing functions in the module are private helpers. The
  service methods are the module's published callable surface
  (#link(<chap-declarations>)[Declarations]).

== Calling a service
<services-calling-a-service>
`@Service.method(args)` invokes the method, from any module:

```ritchie
Confirmation c = @Checkout.place(cart) handle {
    OutOfStock! => return suggestAlternatives(cart)
}
```

`@Service.method(args)` is a remote operation. It returns the method's
typed result, an error of the method's inferred raise set, or a runtime error
under #secref(<errors-the-built-in-error-catalog>). Reporting of an escaping error follows
#secref(<errors-terminal-disposition>).

== Services and entities
<services-services-and-entities>
#idx("orchestration")
Services and entities are independent: a service is not \"the service
of\" any entity type, and one service commonly orchestrates several:

```ritchie
@Checkout.place(Cart cart) -> Confirmation | OutOfStock! | PaymentFailed! {
    $Inventory inv = $Inventory[cart.warehouseId]
    inv.reserve(cart.items)
    $Account payer = $Account[cart.customerId]
    Receipt? r = payer.charge(cart.total) handle InsufficientFunds! => none
    if r == none { return PaymentFailed! }
    return Confirmation.new(cart, r)
}
```

The division of labour:

- #strong[Entity methods] are the leaf transactions. Each is atomic over
  its own-type participants.
- #strong[Service methods] compose those transactions. A service method
  is #emph[not] a transaction: each entity call it makes commits or
  aborts on its own, and a committed call stays committed whatever
  happens to the service method afterwards.

== Logical boundary
<services-logical-boundary>
#idx("distribution")
The service name identifies the logical destination without an address in
source code. A plain function call stays within the calling unit.

== Time budgets
<services-time-budgets>
#idx("time budget")
#idx("budget")
Service calls use the ambient budget in #secref(<effects-time-budgets>). Per-call budget syntax is reserved for a later pass.

== Gateway
<services-gateway>
#idx("Gateway")
`@Gateway` is the built-in application singleton that publishes service
methods as external routes. It is not a service type and cannot be
declared, aliased, or given methods.

Every declared service method belongs to the application's internal
route registry. An internal registration adds optional properties to
that entry; it does not decide whether the method is generated or
deployed:

```ritchie
@Bank.checkReserves() -> Decimal {
    return 1_000_000.00
}

@Gateway.internal(
    method: @Bank.checkReserves,
    budget: 1s,
)
```

An external registration adds the method to the external route
registry and creates an external adapter:

```ritchie
@Gateway.external(
    method: @Bank.openAccount,
    budget: 1500ms,
)
```

Only service methods may be registered. Entity methods, projections,
queries, and free functions are not Gateway routes. One method may
have both an internal and an external registration; properties on the
two entries are independent. `budget` is optional and must be a positive
`Duration` when present.

The service method remains directly callable through the private Ritchie
service protocol after external registration. The external route has no
corresponding Ritchie method, so an internal Ritchie call cannot be routed
through the Gateway.

== Service vs function
<services-service-vs-function>
#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Aspect], [function], [service method],),
    table.hline(),
    [reach], [private to its module], [callable from anywhere],
    [execution], [local, in the caller's frame], [remote, in the
    service's unit],
    [errors], [raise set inferred], [raise set inferred, plus the
    runtime errors of a remote call],
    [time], [unbounded], [budgeted],
    [state], [sees caller's values], [no caller-local variables or
    service-owned state],
    [failure mode], [fails with its caller], [separate call outcome,
    caller handles],
  )]
  , kind: table
  )

Reach for a function to factor logic within a module. Reach for a
service method to publish behaviour, cross entity types, or touch the
world.
