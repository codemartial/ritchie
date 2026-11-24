#import "../prelude.typ": horizontalrule, idx, secref

= Entities
<chap-entities>
An entity is a persistent record with a stable identity whose methods
change its state one atomic step at a time. This chapter covers
identity, creation, lookup, the transactional contract those methods run
under, and how an entity differs from a struct. Orchestration across
several entities is in #link(<chap-services>)[Services]. How entity
operations are ordered with other remote work is in
#link(<chap-effects>)[Effects].

== What an entity is
<entities-what-an-entity-is>
#idx("entity")
#idx("persistence")
An entity is a persistent record with a stable identity, declared with
the `$` sigil and a brace body of type-first fields:

```ritchie
$Account {
    String  owner
    unique String email
    Decimal balance
}
```

A struct is a local value with independent mutation semantics. An
entity is a durable instance with an identity. Its state survives the
program that created it, without the need for explicit storage
declaration, data mapping layer, or save call.

An entity is the unit of consistency. Its methods are the only way its
state changes, and each method invocation is atomic (#secref(<entities-methods-and-transactions>)).
The entity type, its references, lookup, and methods are visible only in
the declaring module. Its identity type is the public cross-module
handle (#secref(<entities-identity>)).

== Identity
<entities-identity>
#idx("identity")
Every instance carries a built-in `id`, minted by the runtime at
creation. Its type is the entity's own id type `$Type#` -- a `#`
suffix on the entity type. It is not declared in the body, supplied in
initializers, or reassigned. It is readable on any reference:

```ritchie
$Customer# ref = customer.id    // on any reference
$Customer# own = self.id        // through self, inside a method
```

Identity is the basis of equality: two references are equal iff they
name the same instance (#secref(<types-type-equivalence>)).
Field-by-field comparison does not apply to entities.

`$Type#` is a public, scalar value type. Code in another module names
it with the entity's module qualifier and may pass, return, store,
serialize, and compare it. Identities of two entity types do not
interchange, and neither interchanges with `UUId`.  Possessing an
identity grants no entity lookup or method access.

Domain identifiers -- an email, an order number, a SKU -- are ordinary
entity fields, unlike identity. A domain identifier can change over an
instance's life without touching its identity. Entity fields may be
marked `unique` (#secref(<entities-unique-keys>)) to make them collision-proof across
instances of the same Entity type.

== Creation
<entities-creation>
#idx("creation")
Creation is always explicit (#secref(<entities-lookup>)).

Inside the declaring module, `create` constructs an entity instance:

```ritchie
$Account account = create $Account {
    owner: "bob"
    email: "bob@example.com"
    balance: 0.0
}
```

The initializer follows struct construction rules: all-positional or
all-named, defaults may be omitted, every other field must be supplied
(#secref(<types-struct-types>)). The expression's value is the
new reference, its `id` already minted. Initializer fields may contain
arbitrary well-typed expressions. Reusable creation input is an ordinary
author-defined struct.

A bare `$Type { ... }` is not an expression. >>>  Really? check this against helloworld.ri >>> There is no unidentified
entity value, implicit draft, or generated initializer type.

Cross-module creation goes through a public service owned by the entity's
module. The service creates the entity locally and returns its public
identity or another public value:

```ritchie
@Accounts.open(String owner, String email) -> $Account# {
    $Account account = create $Account {
        owner: owner, email: email, balance: 0.0
    }
    return account.id
}

bank::$Account# id = bank::@Accounts.open("bob", "bob@example.com")
```

A standalone creation is a remote operation: it commits before the
expression completes, and it can fail. A creation whose `unique`
fields collide with an existing instance raises `Duplicate!` (#secref(<entities-unique-keys>)). A creation the runtime cannot complete fails on the runtime
channel (#secref(<errors-runtime-failures>)). An instance, once
created, is only ever changed through its methods.

A `create` expression inside an entity method body, instance-level or
type-level, joins the invocation's transaction instead. The new
instance is buffered like any other write and becomes durable at the
invocation's commit; if the invocation aborts, nothing was created. Its
id is minted once per invocation, so transparent re-execution reuses it
(#secref(<entities-methods-and-transactions>)). A type-level creation method is one transaction over the
instances it creates and touches.

== Unique keys
<entities-unique-keys>
#idx("unique", code: true)
#idx("unique key")
A field marked `unique` is a domain key: the runtime guarantees no two
instances of the type hold the same value in that field.

```ritchie
$Product {
    unique String sku
    String        name
    Decimal       price
}
```

- At creation, a colliding `unique` value raises `Duplicate!`, a
  system-owned domain error precise to this case. Nothing is created.
- At mutation, assigning a colliding value to a `unique` field likewise
  raises `Duplicate!` and the method's transaction aborts (#secref(<entities-methods-and-transactions>)).
- `unique` marks a constraint, and it is also a transactional access
  path: the point lookup `$Type[key: value]` resolves an instance by its
  unique value (#secref(<entities-lookup>)). The language mints no finder method from it
  -- there is no `byEmail` -- the access path is the one lookup form.
  Bulk and non-unique querying is the read model's concern
  (#link(<chap-queries>)[Queries]).

Runtime-minted ids are unique, and field uniqueness applies per field, per
entity type, among instances that are not archived. Archiving an
instance may or may not release its unique values (#secref(<entities-lifecycle>)).

== Lookup
<entities-lookup>
#idx("lookup")
A lookup resolves one instance to a reference. It keys either by
identity or by a declared unique field:

```ritchie
$Account acct = $Account[payerId]          // by id (an $Account#)
$Account user = $Account[email: address]   // by a unique key
```

Both forms are module-private: a lookup is legal only in the entity's
declaring module. Other modules may hold a public identity but cannot
turn it, a bare id, or a known domain key into a reference.
`UUId.fromString` yields only a bare `UUId`. Cross-module interaction
goes through services, which accept
and return identities and other public values.

The unique-key form reads the same index that enforces the `unique`
constraint, so it resolves inside the transaction boundary with the cost
and contract of an id lookup. It names the unique field and the value to
match. The field must be declared `unique`. Identity is immutable. A
unique key is mutable and released by archiving, so the same key may
resolve to different instances over an entity's lifetime (#secref(<entities-lifecycle>)).

Both forms are remote operations and can fail on either channel
(#secref(<errors-two-channels>)):

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Failure], [Error], [Channel],),
    table.hline(),
    [no instance with that key], [`NotFound!`], [domain],
    [could not be performed], [`Unavailable!`], [runtime],
  )]
  , kind: table
  )

A lookup that finds nothing is a failure, not an absent value -- the
result type is `$Type`. There is no creation on the read
path: a missed lookup raises, it does not mint. Handle the miss where it
has meaning:

```ritchie
$Account acct = $Account[payerId] handle {
    NotFound! => return RejectedPayment! { reason: "unknown payer" }
}
```

=== Get or create
The two-step \"look up, create on `NotFound!`\" is not atomic: a
concurrent create between the miss and the create raises `Duplicate!`,
so correct code needs a retry. The `else` form does it in one atomic
step on the unique key:

```ritchie
$Customer c = $Customer[email: e] else create $Customer {
    name: n
    creditLimit: 500.0
}
```

The lookup and creation must name the same entity type. The lookup key
is injected into the initializer and cannot be restated. In positional
form, that field is omitted from the field order. The form keys on a
unique field.

The lookup key is evaluated first. Every initializer expression is then
evaluated in source order, even when the instance is found. If an
initializer expression fails, no lookup or creation begins. The creation
effect occurs only on a miss.

The atomic operation returns the existing reference on a hit and creates
an instance on a miss. A concurrent creation on the lookup key is the
found outcome, not `Duplicate!`. The operation contributes
`Duplicate!` only when another unique field can collide; a defaulted
unique field counts. Errors raised by initializer expressions are
unchanged.

An optional `then` block runs only when the logical operation creates
the instance:

```ritchie
$User user = $User[name: name] else create $User {} then {
    log(f"Hello new user {user.id}")
    return
}
log(f"Hello registered user {name}")
```

When the form initializes a variable, that variable is assigned and in
scope inside `then`. The block is skipped on a hit, a concurrent winner,
initializer failure, or creation failure. Its created outcome is stable
across transparent re-execution. Normal fallthrough continues after the
complete expression; exits and error propagation keep their ordinary
meaning.

In statement position, `then` consumes the outcome without a variable:

```ritchie
$User[name: name] else create $User {} then {
    log(f"Hello new user {name}")
}
```

`NotFound!` is not a get-or-create outcome. Outside an entity invocation the
atomic operation commits before `then`; a failure in `then` does not
undo a created instance. Inside an entity invocation, creation and
`then` remain in the enclosing transaction, and an escaping failure
aborts both. Runtime failures propagate on the runtime channel.

=== Candidates then verify
A lookup selects by one key. To act on a set chosen by a predicate --
every overdue invoice, say -- query the read model for
candidates, then act on each transactionally
(#link(<chap-queries>)[Queries]). A query is eventually consistent, so
the method it feeds re-checks its own precondition inside the
transaction and declines when the condition no longer holds. A stale
candidate costs a wasted call; the transactional precondition prevents
a wrong commit:

```ritchie
for r in %Invoice.overdue(today) {
    $Invoice[r.id].applyLateFee(today) handle { NotFound! => none }
}
```

An entity method cannot read a predicate-selected set of its own type
inside a transaction: the participant set is the instances it is handed
plus those it reaches by key (#secref(<entities-methods-and-transactions>)). Membership that must be
transactional is modelled as state -- a field holding the member
references -- and passed in as same-type participants.

== Methods and transactions
<entities-methods-and-transactions>
#idx("transaction")
#idx("atomicity")
An entity method is declared on the `$` type and runs against one
instance, named `self`:

```ritchie
$Account.withdraw(Decimal amount) -> Receipt | InsufficientFunds! {
    if amount > self.balance {
        return InsufficientFunds!
    }
    self.balance = self.balance - amount
    return Receipt.new(amount)
}
```

Methods are the entire mutation surface. Entity fields are reachable
only inside the entity's own methods, through any reference of the
entity's type -- `self` or a same-type participant. They are not
reachable from elsewhere in the declaring module, and not from other
modules. Every read or write of entity state therefore happens inside
some method's transaction.

Entity mutation is transactional state on a shared identity, orthogonal
to the value-mutation rule for structs (#link(<chap-types>)[Types]).

A method either uses `self` (an instance method, called on a reference)
or does not (a type-level method, such as a creation method, called on
the type). Like any function, an entity or service method may declare an
error set in its return signature (#secref(<errors-the-checked-error-contract>)).

=== The participant set
#idx("participant set")
A method may take same-type entities as parameters -- `$Account`,
`$Account[]`, `$Account?`. These are its declared participants. The body
may also reach further same-type instances by lookup. The invocation's
participant set is `self`, the declared participants, and every
same-type instance the body actually touches.

```ritchie
$Account.pay($Account to, Decimal amount) -> none | InsufficientFunds! {
    if amount > self.balance {
        return InsufficientFunds!
    }
    self.balance = self.balance - amount
    to.balance   = to.balance + amount
}
```

The signature documents the guaranteed minimum and lets a caller
estimate cost. The invocation discovers its complete participant set at
run time. A different-typed parameter is not a participant: it is an
opaque reference the body may hold; touching it is rejected (below).

=== Own-type touching only
An entity method body may touch -- read the state of, write the state
of, or call a method on -- entities of its own type only. Touching a
foreign-type instance, looking one up, or calling a service inside an
entity method body is a compile error. Cross-type work belongs to
services (#link(<chap-services>)[Services]). One type, one transactional
scope.

Within the declaring module, an entity field, method parameter, or
return type may name another entity type owned by that module, and the
body may store, pass, return, and compare such references. An untouched
reference does not join the transaction. An entity type from
another module cannot be named; cross-module state is identified by its
public identity and acted on through its owning service. Entity fields
hold serializable forms only -- values, structs, enums, collections of
these, and module-local entity references.

=== Same-type work joins the invocation
An in-body same-type lookup or same-type method call is not a remote
operation from the body's perspective. It executes within the enclosing
invocation: its reads and writes join the invocation's participant set
and write buffer, and there is no inner commit. An in-body lookup miss
raises `NotFound!`. The runtime channel
applies to the invocation as a whole, so transparent re-execution
re-derives every inner effect
exactly once.

=== No external effects
An entity method body performs no I/O, reads no clock, draws no
randomness, and calls no service. Its only effects are writes to its
participants' state. Any value from the outside world -- the current
time, a generated code, a rate -- enters as a parameter. This is what
lets the runtime re-execute a body safely: with the same arguments, the
same body derives the same writes. The compiler enforces it by
admitting no impure call in the body
(#secref(<stdlib-impurity>)).

=== The transactional contract
#idx("serializable")
Each invocation is one transaction over its participant set:

- #strong[Atomic.] All of the invocation's writes commit together, or
  none do. No caller, under any failure, observes partial effect.
- #strong[Serializable.] Concurrent invocations with overlapping
  participants behave as if run in some serial order. Anomalies where
  two guards each read the other's state and both proceed cannot occur.
- #strong[Consistent reads.] The first read of each participant pins its
  committed state for the rest of the invocation, overlaid with the
  invocation's own writes (a body reads what it has written).
- #strong[Non-blocking.] Invocations resolve conflicts without deadlock:
  conflicts surface as aborts, not waits, and the runtime re-executes the body
  transparently. The caller observes one outcome.
- #strong[Durable.] When the call returns success, the writes are
  durable.

=== Errors and abort
Returning an error is the abort. An error that escapes the body discards
every buffered write and surfaces to the caller. An error handled inside
the body is ordinary control flow -- the transaction continues and may
commit. There is no abort keyword and no rollback call.

Entity runtime failures follow #secref(<errors-the-built-in-error-catalog>).
If the runtime repeats one mutating invocation while recovering a lost response,
its transaction commits at most once. A successful repeat after a commit
returns the original result rather than one calculated from later state. A
separate call made by program execution is a new invocation.

=== Invariants
#idx("invariant")
An entity type may define `__invariants() -> Bool`, a pure method that
reads `self` and writes nothing. The runtime holds it as a standing
guarantee: every live instance satisfies it. An invocation whose commit
would leave any participant failing its `__invariants()` aborts,
committing nothing -- the same outcome as a returned error. An entity
that defines none carries no such check.

=== Narrowing the set deliberately
Passing a participant's #emph[values] instead of the participant keeps
it out of the transaction:

```ritchie
// Transactional over self and buddy.
$Doctor.goOffCall($Doctor buddy) { ... }

// Transactional over self only. Caller pre-read buddy state and owns
// its freshness.
$Doctor.goOffCallFast(Bool buddyOnCall) { ... }
```

Both are serializable over what is in the transaction. The trade is what
is in it, and it is visible in the signature. The entity author must
declare the narrower method and the module-local caller must choose it --
neither side can weaken the guarantee alone.

== References and mutation visibility
<entities-references-and-mutation-visibility>
Inside the declaring module, a variable of entity type holds a reference
to the one instance. Passing it, storing it in a collection, or returning
it within that module shares only the reference. Reads outside a method
go through methods too, so every observer sees some committed state of
the instance.

```ritchie
$Account[] flagged = [a, b, c]      // references, not copies
Bool same = a == flagged[0]         // true iff same id
```

== Entity vs struct
<entities-entity-vs-struct>
#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Aspect], [struct], [entity],),
    table.hline(),
    [identity], [none, compared by fields], [`$Type#`, compared by id],
    [sharing], [independent local value], [shared reference identity],
    [persistence], [ephemeral], [durable, implicit],
    [mutation], [by its owner, locally], [through methods,
    transactionally],
    [scope], [local], [remote],
    [failure], [cannot fail to exist], [lookup and methods can fail],
  )]
  , kind: table
  )

Model a thing as an entity when it has a life of its own -- referenced
from many places, mutated over time, worth finding again. Model it as a
struct when it is a value some other thing owns.

== Lifecycle
<entities-lifecycle>
#idx("lifecycle")
#idx("seal")
Every instance carries a lifecycle state: ACTIVE, FROZEN, or ARCHIVED.
The state is observed through its effects (the table below). Transitions
run only inside the entity's own methods, through the built-ins
`self.freeze()` and `self.archive()`, and commit atomically with the
method's other writes. The progression is one-way -- ACTIVE to FROZEN
to ARCHIVED, or ACTIVE straight to ARCHIVED. There is no way back.

#figure(
  align(center)[#table(
    columns: 4,
    align: (auto,auto,auto,auto,),
    table.header([State], [Reads], [Mutations], [Unique keys],),
    table.hline(),
    [ACTIVE], [normal], [normal], [held],
    [FROZEN], [normal], [rejected with `Frozen!`], [held],
    [ARCHIVED], [resolve for a while, then `NotFound!`], [rejected with
    `Frozen!`], [released],
  )]
  , kind: table
  )

#strong[FROZEN] seals the instance. Reads and lookups work, and the
instance joins transactions as a read-only participant. `Frozen!` is
raised dynamically, by the first write statement the invocation executes
against a FROZEN or ARCHIVED participant -- `self` or any other -- and
it aborts the invocation like any escaping error. An execution path
through a writing method that performs no write succeeds. `Frozen!`
is a catalog error (#secref(<errors-the-built-in-error-catalog>)), so it
escapes through the inferred raise set and no declared error set lists it.
A reversible hold (a suspended account, a disputed order) is not freezing
-- model that as ordinary domain state.

#strong[ARCHIVED] is end of life: immutable at once, unreadable
eventually. Lookups may keep resolving for an unspecified window, then
raise `NotFound!`. Its `unique` values are released immediately, so a
new instance may claim them (#secref(<entities-unique-keys>)). Physical removal happens behind
the unreadability veil, on operator-defined retention -- the language
does not expose it.

There is no deletion. What a domain means by \"gone\" is one of three
things: a domain state (reversible), FROZEN (sealed but readable), or
ARCHIVED (keys released, fading from view).
