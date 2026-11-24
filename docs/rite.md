# RITE

*Reliable Interaction with Typed Endpoints*

**Version 1.0**

Tahir Hashmi  
19 October 2026

Copyright © 2026 Tahir Hashmi.

> This document is licensed under the
> [Creative Commons Attribution-NoDerivatives 4.0 International License](https://creativecommons.org/licenses/by-nd/4.0/).

An architectural style and execution model for distributed applications.

RITE is an architectural style and abstract execution model for
distributed applications. It combines established reliability techniques
into a unified, typed application model in which state ownership,
invariant boundaries, and interaction behaviour are specified together.

Each component has an explicit contract, owns its state and invariants,
and communicates through well-defined interaction modes that determine
data consistency and failure semantics. Every interaction has a stable
identity, a finite budget, and a classifiable outcome. These rules make
mutations safe to retry within a defined window, keep invariants within
one owner, and make effects attributable to their causes.

By placing these rules in the application model, RITE enables static
compatibility checks and consistent runtime behaviour across component
relationships. Its semantics do not depend on process layout, placement,
replication, or storage mechanisms. Version 1.0 covers one datacenter or
a cluster of well-connected datacenters. Multi-region interaction is
outside its scope.

# 1 Problem Space

## 1.1 Domain

Modern enterprise applications are built as collections of services,
each owning a piece of the system's state and exposing logic to
manipulate it or behaviour over it. Integration between these services,
however, is invariably ad hoc, without a view of the *whole application*
and how its pieces fit.

Over the past few decades, since the rise and fall of models like CORBA,
significant progress has been made in understanding distributed
interaction and making it reliable. Most of these advances address a
narrow slice of the problem. Developers and architects remain
responsible for assembling these solutions into a coherent application
and defining consistent integration practices for it.

One of the consequences of this pattern is the need for developers to
become "full cycle" engineers. Unlike other engineering disciplines
where design, construction and operation are typically separate
specialisations, "full cycle engineers" spend an inordinate amount of
effort understanding both the problem domain and the delivery
environment.

A coherent computing model for distributed applications would replace
that irregularity with a uniform integration vocabulary. That would
let developers reason about the whole system rather than one component
at a time, and operators be more effective at managing a fixed pattern
of workloads with well-defined properties.

Developing such a model would allow us to convert several runtime
failures into code level safety checks. It would also allow for more
capable runtimes that offload reliability concerns from the application
domain and allow cleaner separation of responsibilities between
developers and infrastructure operators.

## 1.2 Definitions

As indicated by the title, this model is built on three concepts,
defined next.

**Reliable.** Every interaction has a classifiable outcome: it completed,
it did not complete, or its result is unknown. No interaction fails
silently, and no interaction ends in a state beyond the three classes
defined in §4.1.

**Interaction.** Communication between units uses a small set of defined
modes with known consistency and failure semantics. Protocols and
consistency follow from the chosen interaction mode, rather than being
assembled ad hoc.

**Typed Endpoints.** The distributed system units, called Endpoints, are
types in the application's type system. An Endpoint's contract consists
of its methods, their request and result types, the typed errors each
method may return, the Event types it publishes or emits, and the
interaction mode of each method, which fixes its consistency and failure
semantics. Contract compatibility is statically checkable, so
integration failures are found before deployment rather than during
deployment or at runtime.

## 1.3 Properties

RITE aims to make distributed applications reliable, observable,
evolvable, debuggable, resource bounded, and easier to reason about. Its
constraints isolate failures and state ownership, classify interaction
outcomes, preserve causal lineage, make mutations retryable within the
configured duplicate suppression window, and bound synchronous work.
Typed interfaces and explicit interaction modes reduce coupling and
per-integration decisions. These qualities arise from the constraints in
Section 7.

# 2 Foundations

## 2.1 Endpoint

An **Endpoint** is an independent, replicated, network addressable unit
with an explicit contract. It is the unit of ownership, failure
isolation, and interface.

An Endpoint is not necessarily a deployment unit, code organisation
unit, or team boundary. These often align but are not definitional.

## 2.2 Locator

An Endpoint is addressed by a **Locator**: a stable name that survives
restarts, deploys, and scaling. How the name resolves to physical
infrastructure is an implementation detail.

> **Example.** A Locator may consist of a tenant, a namespace, a kind,
> and a name.

A Locator is the stable part of an Endpoint's versioned address. It must
not embed network coordinates, regions, shards, or versions.

## 2.3 Kinds of Endpoint

There are four kinds of Endpoint.

A **Service** owns no durable state. It performs stateless computation
and may invoke other Endpoints. It may enforce contract level validation
(schema, auth, policy).

An **Entity** owns Entity instances, which hold durable state. It
defines their schema, methods, and per instance invariants. Internal
storage structure is unconstrained.

A **Stream** owns a durable, ordered sequence of immutable Events of one
type, which may be a sum type. Publishers append Events by Invocation,
and consumers receive them by Activation (§6).

A **Collection** owns a read-only view derived from the committed state
of one or more Entity Endpoints. It maintains its view from their CDC
Streams (§3.5). The purpose of a Collection is to serve queries that
span many instances, including joins or aggregates.

Section 3.1 defines Entity instances and Events.

> **Example.** A payments system might consist of:
>
> - `Account` – an Entity. Each instance represents one account. Per
>   instance invariant: balance must not go negative. Methods:
>   `debit(amount)`, `credit(amount)`.
>
> - `Payment` – a Service. Orchestrates transfers between `Account`
>   instances. Each debit or credit is an independent atomic RPC on one
>   instance. The implementation of `Payment` determines the order of
>   the debit and the credit, and the recovery of a transfer in which
>   one of them fails.

## 2.4 Interaction

An **interaction** is one application intent between two Endpoints. Its
identity covers the first delivery attempt and every retry. A separate
submission is a new interaction with a new identity.

Each attempt of an interaction carries an **Envelope** – a structured
message conveying at least the interaction's identity, source Endpoint,
destination Endpoint, and an ordering context where ordering matters.

The identity of an Envelope issued while processing another derives from
the identity of its cause. A retry of the cause therefore reissues the
same identities for the same downstream interactions, and the lineage of
every Envelope can be reconstructed.

Identity provides correlation for tracing and idempotency for
deduplication.

## 2.5 Abstract Machine

A RITE system at runtime is a population of Endpoints. Entities,
Collections, and Streams own durable state. Services do not. Every
observable cross-Endpoint effect results from processing an Envelope.

Each Endpoint owns its state, reads its own clock, and schedules its own
work. Cross-Endpoint work happens only through Invocations or
Activations.

The runtime is the part of the abstract machine that carries
interactions between Endpoints and keeps the state those interactions
require. It is a role, which an implementation may realise in any form.

The abstract machine has two interaction modes.

- In **Invocation** (§5), an originator sends one request to one
  destination Endpoint, which handles it as one logical operation with
  one outcome. In RPC mode, the originator waits for the outcome. In
  Messaging mode, it does not.

- In **Activation** (§6), the runtime delivers one Event from a Stream
  to one consumer Endpoint, which processes it. Each activation has its
  own outcome, and the runtime records each consumer's progress
  separately. Publication into a Stream is an Invocation on the owning
  Stream (§6.2).

Figure 1 shows how Services, Entities, Streams, and Collections
interact.

![Endpoint kinds and the interactions between them](rite-diagrams/abstract-machine.svg)

*Figure 1: Endpoint kinds and the interactions between them*

An implementation conforms to RITE when no observable behaviour that it
lets programs exhibit is such that the abstract machine excludes it.

## 2.6 The Invariant Rule

Each invariant must be enforced within one Endpoint over that Endpoint's
own state. All state that one invariant relates therefore belongs to a
single Endpoint, and each Endpoint is the consistency boundary for the
state it owns.

## 2.7 Lifecycle

An Endpoint may fail at any time and later recover. After recovery, its
durable state reflects every committed change.

While an Endpoint is failed, it must keep its own state consistent. An
invocation that arrives while the Endpoint is failed receives
`ERR_REQUEST` (§4.1) within its inbound budget. An invocation in flight
when the Endpoint fails receives `ERR_REQUEST` or `ERR_RESPONSE` under
the rules of §5.2 and §5.7.

> **Example.** An implementation moves an Endpoint through **start**
> (recover state if any, accept traffic), **run** (handle requests),
> **fail** (stop accepting traffic), **recover** (restart, replay), and
> **scale** (add capacity).

# 3 Stateful Endpoints

This section defines the state forms, ownership, and consistency rules,
including their behaviour under network partition.

## 3.1 State Forms

**Entity instance.** Each instance is a uniquely identified, mutable
record with a version that advances on each committed write (§5.3).

**Event.** An immutable typed record of a fact, with a durable identity.
Entity instances represent mutable current state, while Events represent
immutable history. Entity state changes become Events on the Entity's
CDC Stream (§3.5), and a successful Entity mutation may emit further
Events (§5.3). Telemetry, device measurements, and external system
Events enter Streams through publication (§6.2).

## 3.2 Sharding

An Entity may shard its instances for scale. All shards share the same
Locator, interface, invariant rules, and contract.

> **Example.** Each shard holds the instances selected by a routing key,
> such as a hash or range over Entity identity.

An Entity's transactional scope extends across all its shards.
Cross-shard coordination within the Entity is internal.

A Stream may shard Event storage and processing for scale. Publishers
and consumers address the Stream, not its internal shards. Sharding must
preserve the contracts for Event durability, opaque positions,
retention, consumer progress, and acceptance order within each ordering
context. Different ordering contexts may progress independently.

## 3.3 Ownership

- Each Entity instance must have exactly one writer at a time (C2,
  §7.2).

- All mutable business state must be recorded as Entity instances.

## 3.4 Consistency by Endpoint Kind

Consistency is determined by Endpoint kind and ownership.

**Entity.** State modifications through Entity methods must be atomic.

**Collection.** Reads are eventually consistent. A Collection builds its
view from CDC Events, with optional projection, indexing, joins, or
aggregation, and the Collection may lag behind the latest writes to the
underlying Entity instances. An application can write through methods
over Entity instances whose identities are found in the Collection's
result set.

**Stream.** Events are immutable. Ordering and retention follow §6.1.

## 3.5 CDC

Every Entity has a **Change Data Capture** (CDC) Stream, which is a
Stream provisioned by the runtime. The runtime publishes each committed
mutation of the Entity's instances as an Event on it, in commit order
for each instance. The runtime is the only publisher to a CDC Stream.

Figure 2 shows one Entity and two Collections derived from it. Each dot
is a writer, and every instance hangs from exactly one writer (§3.3).
Each CDC entry names an instance and the version its commit produced.
Each Activation leaves the CDC Stream at its consumer's position (§6.1),
so the two Collections progress independently.

![Entity ownership, change capture, and derived Collections](rite-diagrams/state.svg)

*Figure 2: Entity ownership, change capture, and derived Collections*

## 3.6 Schema Evolution

These rules apply for schemas of Invocation payloads and Events.

- Additive changes (new optional fields, new Event types) are safe.
  Existing consumers must ignore unknown fields and unknown Event types.

- Removing or renaming fields breaks consumers. Changing the semantics
  of existing fields may break consumers undetectably.

- A breaking change introduces a new interface version under the same
  Locator (§2.2).

- An interface version remains compatible with its predecessor for a
  configured period.

> **Example.** An implementation checks compatibility at build time and
> fails the build on an incompatibility. Each breaking change comes with
> a migration plan.

The model prescribes no mechanism for Entity schema evolution.

## 3.7 Ownership Transfer

When changing Endpoint boundaries, sharding, or ownership:

- At all times, each Entity instance must have exactly one writer (C2,
  §7.2).

- During migration, reads may be served from old or new interfaces.

> **Example.** A migration plan includes a reconciliation step that
> verifies completeness after the transfer.

## 3.8 Behaviour Under Network Partition

A network partition is a condition where some Endpoints cannot exchange
Envelopes with others within the deadline budget.

- **Entity invocations.** During a partition, the runtime must not allow
  two sides to claim ownership of the same instance. If ownership cannot
  be unambiguously determined, the instance is unavailable on both sides
  until the partition heals. An Invocation that the runtime does not
  dispatch because the Entity is unavailable returns `ERR_REQUEST`, and
  state at the destination remains unchanged. An Invocation that may have
  reached the Entity before it became unavailable is classified under
  §4.1. In
  CAP terms, Entity calls are CP: consistency is preserved at the cost
  of availability under partition.

- **Collection reads.** Collections (§2.3) are derived from Entity state
  via projection, indexing, joins, or aggregation. They may be served
  from any node holding a copy of the projection and may lag behind the
  latest Entity writes. Reads remain available on the partitioned side
  in exchange for the lag. In CAP terms, Collection reads are AP:
  availability is preserved at the cost of staleness.

- **Activations and Streams.** A partition between a consumer and the
  Stream pauses delivery to that consumer. When connectivity returns,
  delivery resumes from the consumer's durable position. If retention
  has removed the next Event, the runtime reports a retention gap
  (§6.1). A partition between a publisher and the Stream is handled as a
  failed publication under the three outcome model (§6.2).

- **Multi-Entity operations.** A Service coordinating multiple Entities
  observes each call's result and follows its chosen coordination
  strategy. C3 (§7.3) prevents the unavailability of one Entity from
  violating another Entity's local invariants.

# 4 Interaction Modes and Semantics

Endpoints interact in two modes. In Invocation, an originator sends a
request to a destination Endpoint, and in Activation, the runtime
delivers a Stream Event to a consumer Endpoint. Invocation and
Activation share the outcome, identity, and deadline rules defined here.
Sections 5 and 6 define the rules specific to each mode.

## 4.1 Three Outcomes

Every cross-Endpoint interaction produces exactly one of three outcomes:

- *Completed* – a verdict reached the originator: a positive result or
  an `ERROR` (an application domain rejection). The method contract
  determines any committed state change.

- *Request not completed* (`ERR_REQUEST`) – no verdict reached the
  originator, and state at the destination is unchanged. Causes vary:
  the request did not reach the destination, was rejected before
  invocation, or could not be completed by the runtime.

- *Outcome unknown* (`ERR_RESPONSE`) – the originator does not know
  whether the request ran or what its state effect was.

`ERROR` is the application domain verdict. It is typically the handler's
typed rejection (e.g., `InsufficientFunds`, `ItemOutOfStock`,
`NotFound`) and may also be produced by the runtime when it acts in the
application's namespace – for example, looking up an entity by an
identifier the application owns and finding none. The runtime passes
`ERROR` through to the originator for resolution.

`ERR_REQUEST` and `ERR_RESPONSE` are runtime domain signals. The runtime
detects and classifies them, and it may attempt recovery before
reporting them. The application sees them only when the runtime's
recovery cannot resolve the condition.

> **Example.** Recovery uses retries, failover, reconnection, or circuit
> recovery, chosen according to the cause.

**Classification under uncertainty.** A timeout may mean the destination
has failed, is overloaded, is partitioned, or that the response was lost
in transit. A failed interaction is classified as `ERR_RESPONSE` unless
there is evidence that its outcome is `ERROR` or `ERR_REQUEST`.

Figure 3 marks the outcome of a failure at each step of RPC and
Messaging Invocation (§5.2) and of Activation (§6.4).

![Outcome classification in each interaction mode](rite-diagrams/outcomes.svg)

*Figure 3: Outcome classification in each interaction mode*

## 4.2 Identity and Idempotency

State mutating cross-Endpoint interactions must be idempotent on the
interaction's identity (C5, §7.5).

The destination recognises the identity and applies the operation at
most once per interaction. A repeat returns the same outcome as the
first completed operation. A repeat that arrives while an earlier
attempt is in progress does not wait for it and receives `ERR_RESPONSE`.
Deduplication scope and retention are operational policies.

The guarantee is bounded by the configured retention window. A retry
whose identity has aged out of the deduplication window must be expired
and return `ERR_RESPONSE`.

In both the above situations, an implementation may return `ERR_REQUEST`
instead of `ERR_RESPONSE` when it has sufficient evidence that the
interaction had no external effect.

## 4.3 Deadline and Budget

Every cross-Endpoint interaction has a deadline: a cutoff by which the
call must produce one of the three outcomes (§4.1). Budget is the time
remaining until the deadline.

In an RPC, the destination Endpoint knows the remaining budget and uses
it to manage its work. A Messaging Invocation's deadline covers enqueue,
and a publication's deadline covers acceptance by the Stream (§6.2). In
Activation mode, each attempt has its own deadline (§6.4).

When processing one call causes an Endpoint to issue further calls
downstream, the downstream deadline must not exceed the inbound one. An
Endpoint may divide its remaining budget across multiple downstream
calls but cannot extend the budget.

Each Endpoint classifies deadlines against its own clock. An originator
classifies an expired call from its own observations (§4.1), so a
destination that measures the same deadline differently leaves that
classification correct.

> **Example.** The remaining budget travels with a request as a
> duration, which the destination measures from the request's arrival on
> a local monotonic clock.

When the deadline expires, the call's outcome is classified per mode
(§5, §6): `ERR_REQUEST` if no externally visible effect could have
occurred, `ERR_RESPONSE` if any might have.

Figure 4 follows a call chain in which Service A invokes Service B,
which invokes an Entity. The first attempt of A's call ends in
`ERR_REQUEST`, and the runtime retries it with the same identity. The
second attempt reaches B, which calls the Entity. The runtime reattempts
B's call within that call's deadline, which falls before A's. The second
attempt on the Entity commits, but its response is lost, and the
deadline expires before a further attempt can obtain the stored outcome
(§4.2). B therefore receives `ERR_RESPONSE`, which it returns to A
before A's deadline.

![Runtime retries and deadline expiry along a call chain](rite-diagrams/deadline.svg)

*Figure 4: Runtime retries and deadline expiry along a call chain*

# 5 Invocation Mode

## 5.1 RPC and Messaging

**RPC** returns the destination method's outcome to the originator
within the deadline. It covers both state mutating requests and reads.
The distinction between commands and queries is a property of the
invoked method, not a separate submode.

**Messaging** returns an acknowledgement once the runtime has accepted
durable custody, but the processing outcome does not return in band.
Custody lasts until the destination processes the message, and the
message need not be retained afterwards. Capacity exhaustion rejects
the Invocation before acceptance. Settlement and abandonment of a
message follow the rules for activation attempts (§6.4, §6.5).

> **Example.** A Service sends a message to another Service together
> with the Locator of a callback Endpoint, and the receiving Service
> reports the processing outcome by invoking that Endpoint.

## 5.2 Envelope and Outcome Specialisation

In Invocation mode, the Envelope's destination is an Endpoint capable of
handling individual invocations. The Envelope's ordering context
constrains order only for publication (§6.1). Other invocations are
ordered only by the originator awaiting each outcome (§5.5).

`ERR_REQUEST` vs `ERR_RESPONSE` classification:

- **RPC.** If the request was definitely not delivered to the
  destination boundary, `ERR_REQUEST`. If the request may have been
  delivered but the response was not received (timeout, connection lost
  during the response), `ERR_RESPONSE`.

- **Messaging.** If the runtime did not accept custody, `ERR_REQUEST`.
  If custody may have been accepted but the originator received no
  acknowledgement, `ERR_RESPONSE`. Acknowledged custody is Completed
  (§5.1).

When classification cannot be determined, default to `ERR_RESPONSE`.

## 5.3 Entity Invocation

An Entity invocation is the unit of atomic state change for the
Entity's instances. Its **participant set** contains the instances
that the invocation reads or writes, and it may grow during execution.

> **Example.** A language declares the initial participants in the
> method signature, as parameters of the same type that hold one entity,
> a list, or an optional entity. Further participants join when the body
> looks up entities of the same type.

**Atomic snapshot.** The invocation observes a consistent snapshot of
its participants. Every read of an entity in the participant set
returns one committed version, fixed for the duration of the
invocation, overlaid with any writes the invocation has issued so far
(read-your-own-writes).

**Atomic commit.** The invocation's writes are atomic across its
participants. Either every write becomes visible together, with each
modified entity's version advancing, or none do. The runtime publishes
the committed mutations as Events on the CDC Stream (§3.5).

**Emission.** A successful Entity mutation may emit Events to Streams,
and the runtime publishes each emitted Event (§6.2).

**Serialisability.** Concurrent invocations whose participant sets
overlap are serialisable. There is some serial order in which they could
have been executed that yields the same observable result.

**No external effects.** An Entity invocation makes no outgoing network
calls. The body manipulates only its participants' state, and emissions
take effect only after the commit.

**Outcomes.** The invocation produces one of the three outcomes of
§4.1.  A Completed outcome carries either the body's return value,
with writes committed, or an `ERROR` (the body's typed rejection),
with no writes committed. Contention that ends the invocation before
commit produces `ERR_REQUEST`. Idempotency on identity (§4.2) makes
any retried invocation observe the same outcome as a single completed
execution.

## 5.4 Service Invocation

A Service invocation executes business logic. It can also perform
stateless orchestration by issuing outgoing requests to other
Endpoints and composing their results. Each outgoing request is itself
an invocation on its destination, carrying its own identity and a
deadline carved from the remaining budget (§4.3).

An `ERROR` from a downstream invocation is observable to the Service
body and may be handled or propagated.

## 5.5 Concurrency

RITE defines no implicit ordering between invocations. A runtime may
execute them concurrently, including invocations sent to the same
Endpoint.

Entity invocations with overlapping participant sets remain serialisable
(§5.3).

A Service can establish an order by waiting for one invocation's outcome
before issuing the next. Activations for one consumer proceed
sequentially within an ordering context (§6.5).

## 5.6 Composition

When an operation spans multiple Entity Endpoints, coordination is
explicitly handled by the application (C3, §7.3). Each Entity invocation
is atomic over its participant set. A committed Entity invocation's
effects persist regardless of what happens to the Service invocation
that initiated it.

The order of steps, the resolution of partial completion when a step
fails, the recovery of incomplete operations after a Service crash,
and the choice between durable or ephemeral progress tracking are all
application design concerns.

## 5.7 Expiry

An invocation's deadline is the end of its allowance on the
destination's clock. The caller takes the outcome from a response that
arrives in time, and otherwise from its own evidence under §4.1:

- **Entity invocation.** Invocations on Entity methods make no outgoing
  calls, and their effects are confined to a single atomic commit on
  the participant set. An entity method invocation does not issue its
  commit after its deadline. The caller receives `ERR_REQUEST` when it
  has evidence under §4.1 that no commit was issued, such as a response
  reporting that the deadline passed before the commit, and
  `ERR_RESPONSE` when no response arrives in time.

- **Service invocation.** A Service settles each of its outgoing calls
  at its own deadline, and the Service forms its response from those outcomes. If
  the caller receives no response in time, it classifies from what it sent:
  `ERR_REQUEST` if the Service cannot mutate, `ERR_RESPONSE`
  otherwise.

# 6 Activation Mode

In Activation, the runtime delivers one Stream Event to one consumer
Endpoint, which processes it. The runtime owns delivery, recovery,
settlement, and consumer progress. No application caller waits for an
activation outcome.

## 6.1 Positions, Ordering, and Retention

Each accepted Event receives an opaque position. Positions identify
consumer progress and allow lag to be measured against the Stream's
current end. Two Events in one ordering context keep the order in
which the Stream accepted them. Independent ordering contexts have no
relative ordering guarantee and may make progress independently.

Retention is independent of consumer progress. If retention removes
Events before a consumer processes them, the runtime reports a
retention gap.

## 6.2 Publication

Publication is an Invocation (§5) on the Stream. The runtime performs
the operation and produces one of the three outcomes (§4.1).

- *Completed* – the Event and its Stream position are durable.

- `ERR_REQUEST` – the Stream rejected the Event before acceptance.
  Exhausted capacity produces this outcome.

- `ERR_RESPONSE` – the Stream may have accepted the Event, but the
  publisher did not receive confirmation.

Each publication has an identity. Repeating a publication with the
same identity within the configured duplicate suppression window adds
at most one Event. A retry outside that window may add a
duplicate. The publisher attaches an ordering context when ordering
matters.

## 6.3 Consumers

One Stream-to-consumer relationship has one durable consumer position.
Separate consumer Endpoints provide independent progress over the same
Stream.

Interface compatibility between a consumer Endpoint and the Stream it
consumes from is statically derivable from declared types. A consumer
must accept the Stream's Event type or, for a sum type, at least one
of its variants, in which case Events of other variants do not
activate the consumer. An incompatible pairing cannot be deployed.

A new consumer begins with the first Event published after the
consumer becomes active. A restarting consumer resumes from its
durable position.  A consumer may also request that the runtime replay
retained Events from an earlier position. Each replayed Event starts a
new activation with a new identity (§2.4) and keeps its own durable
identity.

## 6.4 Activation Outcomes

An activation attempt is accepted when consumer processing may have
begun. Settlement records the final status and advances the consumer
position.

- *Completed* – the consumer completed processing with a result or an
  `ERROR`. An `ERROR` is terminal.

- `ERR_REQUEST` – processing did not begin.

- `ERR_RESPONSE` – processing may have begun, but its result is unknown.

An Event that matches the consumer's declared type but whose payload the
consumer cannot read produces `ERR_REQUEST` without reattempt, and
settlement advances the consumer position past it.

Each attempt has a deadline.

## 6.5 Recovery and Progress

The runtime may abandon an activation whose processing may have begun.
Abandonment settles the activation, advances the consumer position, and
produces an operational failure.

Activations within one ordering context proceed sequentially for one
consumer. Activations in independent contexts may proceed concurrently.

> **Example.** An implementation reattempts technical failures within a
> finite configured recovery limit and abandons the activation once the
> limit is exhausted. It quarantines an abandoned or rejected Event with
> its diagnostic context, and a quarantine failure does not prevent
> settlement or progress.

## 6.6 Effects and Repeated Processing

The runtime owns consumer position, activation attempts, recovery,
redelivery, and settlement state. This bookkeeping is opaque to
application logic.

An activation may invoke other Endpoints through ordinary Invocation.
Those downstream effects do not commit atomically with consumer
progress. A failed or uncertain attempt may therefore cause the handler
to run again after a downstream effect has committed.

Retries of one activation retain its identity. When repeated mutation
must be suppressed, the destination of a downstream invocation applies
the identity and duplicate suppression rules of §4.2.

# 7 Rationale

This section gives the reason for each constraint and its cost. For each
of C2 to C6, it also describes a design that keeps every other
constraint and still fails, which shows that the constraint is
necessary. The method follows Fielding's *REST* dissertation (Fielding
2000), which justifies each constraint by the effect of removing it.

## 7.1 C1 – Premise

**Endpoints are independent failure domains.** The model treats each
Endpoint as a unit that can fail while other Endpoints continue to run.
Deployment determines which Endpoints a single process or hardware fault
affects.

C1 alone permits designs in which a component reads or writes another's
state, an invariant spans components, interaction semantics are ad hoc,
a mutation is not idempotent, and a call waits without limit. Each of C2
to C6 excludes one of these designs.

## 7.2 C2 – Single Ownership

Every piece of durable state has exactly one owning unit at a time and
is not shared. Another unit acts on it by invoking the owner's
behaviour, not by accessing state directly. C2 provides Debuggability,
because every state change has one responsible unit, and Evolvability,
because no unit depends on another's storage.

Each cross-unit access may require a network round trip. A unit may
cache responses or replicate datasets to reduce read latency or load.
The owner is not responsible for the validity of those copies. A
consumer may build a replica from the owner's CDC Stream (§3.5) or from
a Collection (§2.3). The consumer owns the replica's validity
requirements.

A design without C2 lets Endpoints write to a shared database. The
schemas of the Endpoints are then coupled. A schema change needs
coordination across units, and ownership and incident attribution become
ambiguous.

## 7.3 C3 – The Invariant Rule

Each invariant must be enforced within one unit over that unit's own
state. A need for a transaction that spans Endpoints indicates that an
invariant crosses the chosen unit boundary. C2 makes local enforcement
possible, and the rule removes coordination from invariant enforcement.
C3 provides Debuggability, because a violated invariant has one
responsible unit, and Reasoning simplicity, because developers reason
about an invariant within one unit.

This rule excludes some natural decompositions, in which each domain
noun becomes its own unit. Separate `Account` and `Ledger` units, for
example, cannot enforce the invariant that each balance equals the sum
of its ledger entries. The designer must merge the units or accept and
design for eventual consistency, making the boundary a design
commitment.

A design without C3 enforces an invariant across two units with
two-phase commit. An unavailable participant can then prevent commit,
and prepared participants can block while the coordinator is
unavailable. Transaction latency includes the coordination rounds.

## 7.4 C4 – Explicit Interaction Modes

All communication between units uses a small set of defined modes with
known consistency and coupling properties. The visible mode determines
consistency instead of negotiating it at runtime. C4 provides Reasoning
simplicity, because the mode determines consistency and failure
semantics, and Observability, because every interaction has common
semantics.

Independent failure domains make each interaction cross a failure
boundary, so the modes account for partial failure. They offer no custom
consistency levels. A mismatch requires restructuring the design rather
than extending the interaction model.

A design without C4 gives each call site its own protocol, with its own
timeout, retry, and consistency behaviour. The behaviour of the
application under partial failure then differs from one interaction to
the next. Reasoning about the application requires study of every
integration, and the developer attention involved grows with the number
of integrations rather than staying bounded by a small, fixed set of
modes.

## 7.5 C5 – Idempotent Mutations

Every state mutating operation must be idempotent on its identity. Calls
across independent failure domains can fail, and C4 identifies unknown
outcomes. C5 provides Reliability, because a retry after `ERR_RESPONSE`
is safe, and a mutation can be replayed. It also gives mutation a
uniform retry strategy.

Some operations are naturally idempotent, such as "set balance to 100."
"Increment balance by 10" is not. A design without C5 applies "increment
balance by 10" on every delivery. A retry after `ERR_RESPONSE` can then
duplicate a payment or an inventory decrement, and manual intervention
is the only safe response.

## 7.6 C6 – Time-Bounded Calls

Every cross-Endpoint interaction must carry an explicit, finite budget
for its synchronous part. The budget propagates through a call chain,
and a caller must not allocate more than what remains. Budget
exhaustion fails the operation with a classified outcome. C4
identifies which interactions carry budgets. C6 provides Resource
boundedness, because no call holds caller resources indefinitely, and
Reliability, because a slow Endpoint cannot stall the Endpoints that
call it.

Callers must allocate time, and long running work must use asynchronous
messaging or streaming.

A design without C6 lets a call wait without limit. A slow C then blocks
B, then A, then A's caller, so one callee can block every caller in a
chain of arbitrary depth. Per site timeouts added later do not provide
budget accounting across the chain. Caller resources then remain
dependent on the callee despite the separate failure domains.

## 7.7 Conclusion

Sections 7.2 to 7.6 each describe a design that keeps every other
constraint and still fails. None of C2 to C6 is therefore redundant, and
removing any one of them weakens the model.

Together, the constraints confine an Endpoint's failure to that
Endpoint. Other Endpoints observe the failure only as classified
outcomes (§4.1). C2 prevents one Endpoint from corrupting another's
state, and each Endpoint keeps its own state consistent through failure
(§2.7). C5 makes an `ERR_RESPONSE` safely retryable, and C6 bounds how
long a slow callee can hold a caller.

# 8 Inspiration

RITE draws on decades of field experience in building and operating
large scale distributed backends for consumer facing online products,
such as social media, e-commerce, and gaming. The following sources
inspired its foundations.

RITE is designed for service oriented architecture, which Amazon adopted
and popularised in the early 2000s (Gray 2006), and for microservices
(Lewis and Fowler 2014). Both decompose an application into
independently owned services, each with its own data and an explicit
interface. An Endpoint is such a unit, and C1 and C2 (§7.1, §7.2) make
its failure isolation and its ownership of state explicit. Both styles,
however, leave interaction semantics to each integration (§1.1), while
RITE defines them once for every integration (§4 to §6).

RPC Invocation keeps the call model of the *RPC* lineage (White 1976;
Birrell and Nelson 1984) without its location transparency. The outcome
classes of §4.1 keep partial failure visible to the originator, as Waldo
et al. (1994) argue a distributed system must. *gRPC*'s initial design
included per call deadlines, and some implementations propagate them
through call chains.

The runtime role (§2.5) is inspired by service meshes, in particular
*Linkerd* (Buoyant 2016), which separate operational concerns from
application code. RITE does not, however, require a service mesh to
fulfil the role.

An Entity follows the key-value store, as popularised by *memcached*
(Fitzpatrick 2004) and later *NoSQL* systems, with RITE giving each
value a user defined type. A type with methods owns its mutation, as an
abstract data type does (Liskov and Zilles 1974), so the method becomes
the boundary of each transaction (§5.3). Entity methods resemble the
commands of *Redis* (Sanfilippo 2009), which mutate a typed value
atomically on the server, except that the application defines them.

Collections follow materialised views and the read models of *CQRS*
(§2.3). CDC Streams follow the change data capture pattern (Das et
al. 2012) and carry the committed writes from which such derived views
can be maintained (§3.5).

Activation uses the durable log consumer model exemplified by *Kafka*
(Kreps, Narkhede, Rao 2011).

RITE leaves the coordination of multistep operations to the application
(§5.6), where established patterns apply and RITE's guarantees simplify
their use. The transactional outbox commits a state change together with
the message that announces it, and Entity emission provides that
guarantee within the model (§5.3). Orchestrators built on state machines
with durable states, such as *Flux* (Flipkart 2017), record each
operation's progress so that it survives a crash of the coordinating
Service.

# 9 Vocabulary

## 9.1 Structural Terms

| Term | RITE definition |
| --- | --- |
| Endpoint | An independent, replicated, network addressable unit of ownership, failure isolation, and interface. |
| Entity | An Endpoint that owns Entity instances and their per instance invariants. |
| Service | A stateless Endpoint that performs computation and orchestration. |
| Collection | An Endpoint that owns a read-only view derived from the committed state of one or more Entity Endpoints. |
| Stream | An Endpoint that owns a durable, ordered sequence of immutable Events. |
| Method | A member of an Endpoint's contract, with its request and result types, its typed `ERROR`s, and its interaction mode. |
| Locator | The stable logical name of an Endpoint, independent of network location, sharding, placement, and interface version. |
| Envelope | A structured message that conveys at least a cross-Endpoint interaction's identity, source, destination, and optional ordering context. |
| Runtime | The role in the abstract machine that carries interactions between Endpoints and keeps the state those interactions require. |
| Entity instance | A mutable typed record, with a durable identity. |
| Event | An immutable typed record of a fact, with a durable identity. |
| CDC Stream | A Stream to which the runtime alone publishes the committed mutations from an Entity Endpoint, in per instance commit order. |

## 9.2 Interaction Terms

| Term | RITE definition |
| --- | --- |
| Interaction | One application intent between two Endpoints, covering the first attempt and every retry. |
| Identity | The identifier of an interaction, reused for retries and used to derive downstream interaction identities. |
| Invocation | An interaction in which an originator sends one request to one destination Endpoint, which handles it as one logical operation with one outcome. |
| RPC | An Invocation whose originator waits for the outcome. |
| Messaging | An Invocation for which the runtime accepts durable custody and returns an acknowledgement to the originator. |
| Publication | An Invocation that submits one Event to a Stream. |
| Activation | An interaction in which the runtime delivers one Stream Event to one consumer Endpoint, with an outcome that controls consumer progress. |
| Participant set | The Entity instances read or written by one Entity invocation. |
| Deadline | The cutoff by which a cross-Endpoint interaction must produce one of the three outcomes. |
| Budget | The time remaining until a deadline expires. |
| Ordering context | The scope within which a Stream preserves Event acceptance order and a consumer processes Activations sequentially. |
| Consumer | A Stream-to-consumer relationship with one durable consumer position. |
| Consumer position | The durable position recording a consumer's progress through a Stream. |
| Settlement | The end of processing for an Invocation or Activation, together with the record of its final status. |
| Retention | The period for which durable data is retained within the application. |
| Completed | An outcome stating that a verdict, a result or `ERROR`, reached the originator. |
| `ERROR` | An application domain rejection returned by a completed interaction, whether produced by the handler or the runtime in the application namespace. |
| `ERR_REQUEST` | A runtime outcome stating that no verdict reached the originator and destination state is unchanged. |
| `ERR_RESPONSE` | A runtime outcome stating that the originator does not know whether the request ran or what state effect it had. |

# 10 References

- Fielding, Roy Thomas. 2000. "Architectural Styles and the Design of Network-based Software Architectures." Doctoral dissertation, University of California, Irvine.
- Gray, Jim. 2006. "A Conversation with Werner Vogels." *ACM Queue*, May 2006. https://doi.org/10.1145/1142055.1142065
- Lewis, James, and Martin Fowler. 2014. "Microservices." https://martinfowler.com/articles/microservices.html
- White, James E. 1976. "A High-Level Framework for Network-Based Resource Sharing." In *Proceedings of the AFIPS National Computer Conference*, 561-570. Also RFC 707.
- Birrell, Andrew D., and Bruce Jay Nelson. 1984. "Implementing Remote Procedure Calls." *ACM Transactions on Computer Systems* 2 (1): 39-59.
- Waldo, Jim, Geoff Wyant, Ann Wollrath, and Sam Kendall. 1994. "A Note on Distributed Computing." Technical Report SMLI TR-94-29, Sun Microsystems Laboratories, November 1994.
- Buoyant. 2016. *Linkerd*. https://linkerd.io
- Fitzpatrick, Brad. 2004. "Distributed Caching with Memcached." *Linux Journal* 124 (August 2004).
- Liskov, Barbara, and Stephen Zilles. 1974. "Programming with Abstract Data Types." *ACM SIGPLAN Notices* 9 (4): 50-59.
- Sanfilippo, Salvatore. 2009. *Redis*. https://redis.io
- Das, Shirshanka, et al. 2012. "All Aboard the Databus!: LinkedIn's Scalable Consistent Change Data Capture Platform." In *Proceedings of the Third ACM Symposium on Cloud Computing (SoCC '12)*.
- Kreps, Jay, Neha Narkhede, and Jun Rao. 2011. "Kafka: A Distributed Messaging System for Log Processing." In *NetDB '11*, Athens.
- Flipkart. 2017. *Flux*. https://github.com/flipkart-incubator/flux
