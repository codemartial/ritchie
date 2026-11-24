#import "../prelude.typ": horizontalrule, idx, secref

= Projections and Queries
<chap-queries>
A program's read model: projections -- the named row shapes the runtime
derives from committed entity state -- and the queries that read them. Both carry the `%` sigil. Identity reads and
mutation are in #link(<chap-entities>)[Entities].

== The read model
<queries-the-read-model>
#idx("read model")
The `%` sigil marks derived reads, as `$` marks transactional entity
access. A projection, `%Order`, is a row shape over committed state. A
query, `%bigWest()`, reads one or more sources and returns rows of a
projection. The sigil tells the reader the consistency model: everything
under `%` is read-only and eventually consistent, and a result reflects
some recent committed state of each source, possibly lagging the latest
writes. Everything under `$` is transactional.

Building a query is local and cannot fail. Calling it is one remote
operation that returns one result, consumed by iteration. A delivered
row is a local struct, and its columns read as ordinary field access. A
query reads only derived data, so entity methods remain
the only mutation surface and entity field privacy is untouched.

== Visibility
<queries-visibility>
Within the module that owns an entity, the entity's full schema is
visible. Module code queries any field of its own entities with no
declaration: an entity type is a legal source (#secref(<queries-queries-and-clauses>)), and the
implicit projection `%T` covers its fields (#secref(<queries-projections>)).

```ritchie
// orders module -- no projection declaration is needed to query own entities
%openOrders() -> %Order {
    from  $Order o
    where o.status == OrderStatus.OPEN
}
```

Across modules, entity fields are not visible and entities are not
queryable. A module shares a view of its state by declaring and
exporting a projection, a named row shape over its entities. A query in
another module names that projection as its source. Projection column
names are independent of the storage field names, so the owner renames
internal fields without breaking consumers.

```ritchie
// orders module: export a row shape over its orders
%OrderLine {
    from   $Order o
    select { number: o.number, total: o.total }
}

// billing module: name the exported projection as a source
%bigOrders(Decimal floor) -> orders::%OrderLine {
    from  orders::%OrderLine l
    where l.total > floor
}
```

The two query forms divide publication. A query method on a projection
is declared by the projection's owner and is callable from any module.
A free-form query is private to the module that declares it, like a free
function. A consumer composes its own free-form queries over any
projection it can see, while the owner curates the named read API.

A projection may expose an identity column of type `$Type#`. The
identity lets a consumer correlate rows or pass an instance's identity
back to an owner service; it does not expose the entity reference,
lookup, or methods. An explicit projection's type and columns are public.

```ritchie
// invoiceId identifies the entity without granting entity access.
%InvoiceSummary {
    from   $Invoice i
    select {
        invoiceId: i.id,
        number:  i.number,
        balance: i.total - i.paid,
        dueOn:   i.dueOn
    }
}

// a consumer asks the owning module to act
billing::@Invoices.applyLateFee(r.invoiceId, today)
```

Visibility is a compile-time rule and does not affect consistency. Every
query is eventually consistent, whether it reads the owner's full
schema or an exported projection. Within the owner, identity reads --
`$Type[id]`, `$Type[key: value]`, and entity methods -- are transactional
(#link(<chap-entities>)[Entities]). In production environments,
serving queries typically adds no conflict and no latency to entity
transactions. The `unique` constraint is the exception, committing
atomically with the write.

== Projections
<queries-projections>
#idx("projection")
A projection is a record type used to query entity data in bulk. A
projection name is capitalised, like `%Order`. A query returns a
projection (#secref(<queries-queries-and-clauses>)), and the caller iterates the result to read its
rows, one record at a time.

=== The implicit projection
Every entity implies a projection. For an entity `$Order`, the
projection `%Order` exists in the owning module with no declaration. Its
columns are the entity's declared fields plus an `id` column of type
`$Order#`, the entity's identity (#secref(<entities-identity>)). The implicit projection is pure data, carries no reference
column, and is private to the owning module.
The owner reaches an instance from a row with `$Order[row.id]`, paying
one lookup per instance it acts on. An owner that wants to act in bulk,
or to share rows, declares an explicit projection.

=== Sourced projections
A sourced projection derives its rows from a source. Its body holds
exactly two clauses, `from` and `select`. The `from` clause names the
source and introduces a source variable that ranges over the
source's rows. The `select` clause derives and names each column from
that source variable, and a column's type is the type of the expression that
derives it.

```ritchie
%InvoiceSummary {
    from   $Invoice i
    select {
        invoiceId: i.id,
        number:  i.number,
        balance: i.total - i.paid,
        dueOn:   i.dueOn
    }
}
```

A select entry is a source-variable field path or arithmetic over numeric field
paths (#secref(<queries-select-and-rows>)). An entity source variable itself and any other expression of
entity-reference type are not projection columns. Select `i.id` to
publish the scalar identity instead. A sourced projection has no
`where`, `sort`, `limit`, or parameters. Its extent is every row of the
source, mapped. Filtering is a query's work (#secref(<queries-queries-and-clauses>)).

A sourced projection may source another projection. The source chain
must be acyclic.

=== Bare projections
A bare projection declares typed columns and nothing else. It is a row
shape with no dataset behind it. A query with a `select` clause names a
bare projection as its return type and builds its rows (#secref(<queries-select-and-rows>)). A
bare projection is excluded from `from` clauses.

```ritchie
%Exposure {
    $Customer# customerId
    Decimal   total
}
```

A projection declaration is a definition, not storage. The runtime
promises no dataset behind it, and a program cannot observe whether one
exists. Derived data that must persist is written into entities (#secref(<queries-boundaries>)).

=== Projection methods
A projection method is a query over the projection's extent. Its body
is a clause block, and it returns the receiver's rows, so the return
type is not written.

```ritchie
// reads the receiver's extent, returns %InvoiceSummary rows
%InvoiceSummary.overdue(Date today) {
    from  %InvoiceSummary v
    where v.dueOn < today
    sort  v.dueOn
}
```

A query method on a bare projection carries a `select` clause that
builds the receiver's rows, since a bare projection has no extent of
its own.

== Queries and clauses
<queries-queries-and-clauses>
#idx("select", code: true)
#idx("query")
#idx("clause")
A query reads one or more sources and returns the rows of a projection.
It comes in two forms. A free-form query stands alone, is private to its
module, and writes its return type. A method-form query is declared on a
projection and returns the receiver's rows, so it leaves the return
type unwritten. In both forms the parameter list holds exactly what
callers pass.

```ritchie
// free form: module-private, return type written
%bigWest(Decimal floor) -> %Order {
    from  $Order o
    where o.region == "West" and o.total > floor
    sort  o.total desc
    limit 50
}

// method form: the same body declared on a projection. The receiver
// is the return type, so it is not written.
%Order.bigWest(Decimal floor) { ... }
```

The body of a query is a block of clauses, not statements. `from` comes
first and is required. The rest are optional and appear at most once, in
this order, which is also the execution order.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Clause], [Role],),
    table.hline(),
    [`from`], [declare the sources, one variable per source (#secref(<queries-execution-and-failure>) for
    joins)],
    [`where`], [keep the rows that satisfy a predicate (#secref(<queries-predicates>))],
    [`group by`], [form groups by one or more keys (#secref(<queries-aggregates-and-grouping>))],
    [`select`], [derive and name the result columns (#secref(<queries-select-and-rows>))],
    [`having`], [keep the groups that satisfy a predicate (#secref(<queries-aggregates-and-grouping>))],
    [`sort`], [order the result by one or more keys, `asc` (default) or
    `desc`],
    [`limit`], [cap the result count],
  )]
  , kind: table
  )

A body with no `where` returns every row of the source. A body with no
`select` returns the source's row shape, so a query that sources
`$Order` and writes no `select` returns `%Order` rows. A query that
builds a different row shape, that groups, or that joins carries a
`select` clause (#secref(<queries-select-and-rows>, <queries-aggregates-and-grouping>, <queries-joins>)).

Calling a query runs it as one remote operation (#secref(<queries-execution-and-failure>)).

== Predicates
<queries-predicates>
#idx("predicate")
The `where` clause holds one boolean predicate, built from the forms
below. The `having` clause uses the same forms over the columns that
`select` built (#secref(<queries-aggregates-and-grouping>)).

Operands:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Operand], [Examples],),
    table.hline(),
    [source-variable field path], [`o.total`, `o.address.city`, `o.customer`],
    [parameter], [`floor`, `c`],
    [constant in scope], [`FREE_SHIP_FLOOR`],
    [literal], [`100.0`, `"West"`, `d2026-01-01`, `30d`, `none`],
    [enum member], [`OrderStatus.OPEN`],
  )]
  , kind: table
  )

Combinators:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Rule],),
    table.hline(),
    [`==` `!=`], [both operands share one type, references compare by
    identity],
    [`<` `>` `<=` `>=`], [one ordered type: the numbers, `String`, the
    temporal types],
    [chained], [`lo <= o.total <= hi`, the rule of
    #link(<chap-expressions>)[Expressions]],
    [`in`], [left operand of type `T`, right a `T[]` parameter or
    constant],
    [`and` `or` `not`], [combine predicates],
  )]
  , kind: table
  )

Operands mix freely within a comparison, under one rule: both sides
share a single type after the integer widening of
#secref(<types-primitive-value-types>). The same widening admits a
narrower integer left operand of `in` against the element type of the
right operand. A field compares against a parameter, against another
field (`i.paid < i.total`), against a literal, or for membership
against a `T[]` (`i.region in regions`).

Inside the owning module, a reference-typed operand compares against a
reference of its own type, so `o.customer == cust` holds when `cust` is
a `$Customer` naming the same instance. A source variable over an entity source
is itself a reference operand (`o == target`). Public projections carry
the corresponding `$Customer#` identity instead. Identity values compare
only with the same branded identity type; identities of different entity
types and bare `UUId` values do not interchange.

A nullable field admits `== none` and `!= none`. Any other comparison on
an absent field is false. No narrowing applies inside a query.

A comparison operand may be arithmetic over numeric field paths,
parameters, and literals, typed as in `select` (#secref(<queries-select-and-rows>)) -- so
`i.paid < i.total * 0.9` is a predicate. Arithmetic that faults --
division by zero or `Decimal` overflow -- fails the query (#secref(<queries-execution-and-failure>)).
A predicate otherwise holds the comparisons and combinators above and nothing else:
no function or method calls, no query calls, no ternary, and no
`handle`. A comparand the engine cannot derive -- a clock read, a
call -- enters as a parameter, which is why `today` below is passed in
rather than read from a clock.

```ritchie
%Invoice.overdue(Date today) {
    from  $Invoice i
    where i.dueOn < today and i.paid < i.total
    sort  i.dueOn
}
```

== Select and rows
<queries-select-and-rows>
#idx("row")
A query with a `select` clause builds its own rows. Its return type
names a projection, and `select` builds one row per result in literal
form, positional or named, with the type name omitted. The return type
pins the row shape, through the declared return of a free form and
through the receiver of a method form.

```ritchie
%westExposure(Decimal floor) -> %Exposure {
    from   $Order o
    where  o.region == "West" and o.total > floor
    select { o.customer.id, o.total }
    sort   total desc
}
```

A select entry is a source-variable field path, an aggregate (#secref(<queries-aggregates-and-grouping>)), or
arithmetic over numeric field paths, parameters, and literals. An entry
of entity-reference type is rejected as a projection column. Selecting
the reference's `id` produces its public identity value instead
(#secref(<queries-visibility>)).

```ritchie
select { o.sku, o.price * o.quantity }
```

A delivered row is a local struct. Its columns read as ordinary field
access. A `select` query reads values in bulk; acting on an identified
instance is a separate call to a service owned by that entity's module.

=== Sort and order
`sort` orders the result by one or more keys, each `asc` (the default)
or `desc`. A sort key follows the result shape: with a `select` clause
it names a row column (`sort total desc`), and without one it names a
source-variable field path (`sort o.total desc`).

A sort key is of an ordered type: the numbers, `String`, `Bool`, and the
temporal types. `Bool` orders `false` before `true`. Strings order by
Unicode code point, with no locale collation. An entity reference or
identity is not a sort key. A nullable key places absent values after every present
value, ascending and descending alike.

Order is set by the `sort` clause. Without a `sort`, result order is
unspecified and may differ between executions. A `sort` orders the
result by its keys, and where those keys tie, the order breaks on the
result's primary key when it has one -- an entity-sourced row's
identity, or a grouped row's group keys -- so a sorted query over such
rows is deterministic. A result with no primary key, such as a
bare-projection `select` without an id column, leaves remaining ties in
unspecified order. Name enough `sort` keys to be unique where a total
order matters.

== Aggregates and grouping
<queries-aggregates-and-grouping>
#idx("aggregate")
#idx("grouping")
An aggregate folds many rows into one value. There are five, and each
takes one argument. Aggregates appear only in `select` entries.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Aggregate], [Argument], [Result],),
    table.hline(),
    [`count`], [the source variable], [`Int`, the number of rows],
    [`sum`], [a numeric field path], [the argument's type],
    [`avg`], [a `Decimal` or `Float` field path], [the argument's
    type],
    [`min`, `max`], [an ordered field path], [the argument's type],
  )]
  , kind: table
  )

An integer average is written as `sum` over `count`, with the division
in consuming code.

A `group by` clause forms groups. Its keys are source-variable field paths or
time buckets (below). A group key requires total equality, so `Float`,
and any struct containing one, is rejected as a key, the same rule as a
dict key. With `group by` present, every `select` entry is a group key
or an aggregate, and the result has one row per group. Without
`group by`, an aggregate query treats the whole source as one group and
returns a one-row result. An empty source returns an empty result.

A `having` clause filters groups. Its predicate names the columns that
`select` built, with the combinators of `where` (#secref(<queries-predicates>)) over those
columns, parameters, constants, and literals. An aggregate is not named
in `having` directly: give it a column name in `select` and filter on
that name.

The full clause order is `from`, `where`, `group by`, `select`,
`having`, `sort`, `limit`. The written order is the execution order:
filter the rows, form the groups, build one row per group, filter the
groups.

```ritchie
%RegionRevenue {
    String  region
    Int     orders
    Decimal revenue
}

%regionRevenue(Decimal floor) -> %RegionRevenue {
    from   $Order o
    where  o.status == OrderStatus.FILLED
    group  by o.region
    select { region: o.region, orders: count(o), revenue: sum(o.total) }
    having revenue > floor
    sort   revenue desc
}
```

=== Time buckets
#idx("time bucket")
A time bucket is a computed group key. `bucket(field, step)` takes a
temporal field path and a `Duration` step, and groups the rows whose
field falls in the same step-aligned interval. A bucket aligns to the
temporal type's epoch rather than calendar boundaries such as month
starts. The bucket's value is the interval's start, in the field's
temporal type. A bucket appears in `group by` and, written the same way,
as a `select` key.

```ritchie
%DailyVolume {
    Date day
    Int  orders
}

%dailyVolume(Date since) -> %DailyVolume {
    from   $Order o
    where  o.placedAt >= since
    group  by bucket(o.placedAt, 1d)
    select { day: bucket(o.placedAt, 1d), orders: count(o) }
    sort   day
}
```

There is no per-group selection, such as the top rows of each group. A
consumer iterates the grouped result and runs a limited query per key in
plain code.

== Joins
<queries-joins>
#idx("join")
A query may read several sources at once. The `from` clause then introduces
one variable per source, comma-separated, and any source that is legal
alone is legal here, so own entities, visible projections, and mixtures
of the two all combine. A multi-source body requires a `select` clause,
because no single source's row shape can stand as the result.

The join condition lives in `where`. An equality between operands of two
different source variables is a join edge. Both kinds of equality serve: a
reference equality joins along an identity edge, following entity
composition, and a value equality joins rows that agree on a shared
type, such as a region code. A value-equality edge needs total equality,
so `Float`, and any struct containing one, is rejected as an edge. The
remaining predicate forms filter rows as in any query (#secref(<queries-predicates>)). Each
source is read at its own snapshot (#secref(<queries-execution-and-failure>)), so a join may pair rows
from independently selected snapshots.

Joins are inner. A result row exists for each combination of source rows
that satisfies the predicate, and a source row with no counterpart
contributes nothing. There are no outer forms; inner joins omit
unmatched rows rather than adding absent columns.

Every source must be joined. The `where` clause carries at least one
equality linking each source variable to another, and a body that leaves a source
unlinked does not compile.

Grouping, `having`, `sort`, and `limit` compose over the joined rows. A
group key may come from any source variable, and a source variable over an entity source
may itself stand as a group key, which groups by identity.

```ritchie
%customerTotals() -> %Exposure {
    from   $Customer c, $Order o
    where  o.customer == c and o.status == OrderStatus.FILLED
    group  by c
    select { customerId: c.id, total: sum(o.total) }
    sort   total desc
}
```

Outside the owning module, the same join is written over exported
projections, and identity columns carry the edge
(`v.customerId == u.customerId`).

== Execution and failure
<queries-execution-and-failure>
Calling a query runs it as one remote operation under the effects model
(#link(<chap-effects>)[Effects]), admitted wherever a service call is.
An entity method body cannot call a query, by the no-external-effects
rule. A query joins no transaction, so `Conflict!` is not a query outcome.

The caller consumes a result by iteration. The call stands in the
iterator position of a `for` loop, and rows arrive as the loop advances,
so a result has no size ceiling. There is no whole-result value: an
author who wants an array fills one from the loop.

```ritchie
Decimal exposure = 0.0
for r in %Order.bigWest(100.0) {
    exposure += r.total
} handle { Unavailable! => return }
```

A result reads each source at one snapshot, a consistent cut of that
source's committed state fixed when the call starts. Every row of one
source reflects that cut, so a concurrent write causes neither duplicate
nor missing rows within the scan. Each source has its own snapshot, so a
join may pair rows from cuts that did not coexist
(#secref(<queries-joins>)).

The snapshot is held while the caller iterates, under a consumption
deadline. An iterator advanced past the deadline raises `Cancelled!`,
since the query itself has not mutated relevant state. The deadline is
configured outside the program.

A query declares no error set. Its own runtime failures confirm
relevant state unaltered (#secref(<errors-the-built-in-error-catalog>)). A failure
reaches the enclosing handled block whether raised before the first row or
partway through iteration. Nothing is silently truncated: a consumer
sees every row or a failure, and an empty result is a value, not a
failure.

A row is data. The owner may look up `$T[row.id]`; another module passes
the identity to an owner service. The acting entity method re-verifies
its precondition because the row is
eventually consistent (#link(<chap-entities>)[Entities], candidates then
verify). Each such method invocation is its own transaction: work
committed by earlier loop iterations persists when a later one fails,
since no transaction surrounds the loop. A consumer that wants several
mutations in one atomic step batches the participants into one method
call.

```ritchie
$Invoice[] due = []
for r in %Invoice.overdue(today) {
    due.append($Invoice[r.id])
} handle { Unavailable! => return }
$Invoice.applyLateFees(due, today)        // one transaction over the batch
```

== Boundaries
<queries-boundaries>
The read model is derived from committed entity state and delivered as
rows. Several capabilities sit outside it.

A query reads once. There is no continuous form that watches a
projection and delivers rows as new commits arrive.

Time-series analysis is not covered. Relative-time comparison, such as
this hour against the same hour a week earlier, windowed rate and
aggregation over time, and the statistical aggregates beyond the five
here, have no form in the read model.

Per-row ordered computation is not expressible. A predicate or select
entry reads one row in isolation, with no access to a neighbouring row,
so sessionization and running totals cannot be written as a query. Work
that needs ordered, stateful processing is done where state lives -- in
entity methods, or as derived entities written at ingest.

Durable derived data is an entity. A query is a definition over live
state with no storage of its own (#secref(<queries-projections>)). A derived result that must
persist is written into entities, which persist by the ordinary rules
and stay under their author's lifecycle control.
