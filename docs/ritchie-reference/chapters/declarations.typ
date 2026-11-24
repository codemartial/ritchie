#import "../prelude.typ": horizontalrule, idx, secref

= Modules and Declarations
<chap-declarations>
What a module is, what may be declared at its top level, and what
crosses its boundary into other modules.

== Modules
<declarations-modules>
#idx("module")
A module is a cohesive ownership boundary: the code, state, and
published surface for one closely aligned purpose. At scale, a module is
the unit a single team can own, evolve, deploy, and reason about without
shared day-to-day control from another team. Small programs may have one
team owning many modules, but a healthy module should still read as one
purpose, not as a technical bucket.

A project gives each module an explicit name and maps it to a source-root
directory. Except for a subtree assigned to a separately named nested
root, every `.ri` file recursively beneath that root belongs to the
module and shares its namespace. Ordinary subdirectories organise files
without creating modules or changing visibility. Physical nesting grants
no privileged access between separately named modules.

The module name is a flat `VARIABLE` identifier. Contextual query words
remain identifiers outside their query positions and may be module
names. Directory names have no language meaning: moving or renaming a
root without changing the project mapping does not change its module.
Source files contain no module declaration.

Within each project, its metadata assigns every local module and every
selected dependency module one project-wide short name. Source in a
dependency resolves module names through the project that owns that
source, not through the consumer's module names. A dependency normally uses
the name declared by its publisher; a consumer may assign it another
name. Local module names, dependency module names, and implicit standard-module
names must be distinct within the project.

Code refers to another module's published declarations with `::`, e.g.
`crm::Customer`, `shop::@Checkout`, or `bank::$Account#`. There are no
import declarations or selective imports. A type alias shortens or
disambiguates a qualified name locally (#secref(<types-type-aliases>)). A module that mentions a contract -- in a `where` bound,
an `is` declaration, or by qualified name -- takes on its
name-ownership (#secref(<contracts-conformance>)).

A module name occupies the outermost name scope and is selected only as
the left side of `::`. An ordinary in-scope variable may have the
same name: a bare use and a following `.` select the ordinary variable, while
`name::` selects the module name. A module name is not itself a value.

A module depends on another when any of its declarations or expressions
refers to a declaration owned by the other module. The complete resolved
module-dependency graph is acyclic. A cycle is a compile error regardless
of whether its modules belong to the root project or to dependencies.

Each service type and entity type is a separate distribution unit.
Modules own names and access; they do not impose placement, scaling, or
failure boundaries. An entity type and its methods always live in one
module.

== Top-level declarations
<declarations-top-level-declarations>
#idx("declaration")
A module's top level holds declarations only:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Declaration], [Form],),
    table.hline(),
    [regular type or enum], [`Type { ... }`],
    [entity type], [`$Type { ... }`],
    [service type], [`@Type { }`],
    [error type], [`Type! { ... }` or bare `Type!`],
    [contract], [`contract Type { ... }`],
    [type alias], [`alias-name = type` (#secref(<types-type-aliases>))],
    [constant], [`Type NAME = value`],
    [function], [`name(params) [-> type] { ... }`],
    [method], [`Receiver.name(params) [-> sig] { ... }`],
  )]
  , kind: table
  )

Declaration order is insignificant. A name may be used above its
declaration within the module.

A constant declaration has the same compile-time semantics at module and
local scope (#secref(<statements-constant-declarations>)). Its
initializer is a constant expression, which the compiler evaluates to the
declared value. A raise, an evaluation failure, an out-of-range result, or
a dependency cycle among constants is a compile error.

Constant integer arithmetic is exact and each result must fit its
required type, unlike runtime integer arithmetic that wraps
(#secref(<types-primitive-value-types>)). Parameter defaults use the same
constant evaluator (#secref(<declarations-functions-and-methods>)).

== Functions and methods
<declarations-functions-and-methods>
#idx("function")
#idx("method")
#idx("receiver")
A free-standing function is module-private:

```ritchie
computeTax(String region, Decimal amount) -> Decimal { ... }
```

A method is declared on a receiver -- a regular type or enum, an entity
type, a service type, or a bounded type parameter
(#secref(<contracts-bounds>)). Its reach follows the
receiver's archetype (#secref(<declarations-visibility>)):

```ritchie
Dollar.rounded() -> Dollar { self.n = ... }  // instance: uses self
Dollar.code() -> String { return "AUD" }     // type-level: no self
$Account.charge(Decimal amount) -> Receipt | InsufficientFunds! { ... }
@Checkout.place(Cart cart) -> Confirmation | OutOfStock! { ... }
```

A method that uses `self` is instance-level and is called on a value.
One that does not use `self` is type-level and is called on the
type. The constructor convention `Type.new(...)` and the conversion
convention `Type.fromV(...)` are ordinary type-level methods. There is
no overloading: each method name on a receiver has one signature, and
distinct behaviours take distinct names. Fields and methods declared
by one type occupy one member namespace. No field and method may share
a name.

#idx("private method")
#idx("published method")
A method whose name starts with `_` is #emph[private]: it is callable
only within the declaring module, and it does not count toward
publishing its receiver (#secref(<declarations-visibility>)). Every other source-declared method
is #emph[published] and reaches other modules as the receiver's
archetype allows. The marker applies to methods of regular types,
enums, and services. An entity method name does not start with `_`.

```ritchie
Cart._resubtotal() { self.subtotal = total(self.lines) }   // private
Cart.add(Line l) {                                         // published
    self.lines = append(self.lines, l)
    self._resubtotal()
}
```

Types are closed. A method is declared only in the module that owns its
receiver type; no module adds methods to another module's type. To
extend a foreign type, declare a new type in the extending module that
holds it, and add methods to the new type.

=== Parameter defaults
#idx("parameter default")
A parameter may carry a default with `=`, and the defaulted parameters
form a trailing suffix of the parameter list. A default is a constant
expression (#secref(<declarations-top-level-declarations>)): it reads no other parameter, no `self`, and no
runtime state. A call that omits a defaulted parameter receives an
independent copy of the compiled default value, so an aggregate
default is not shared between invocations. A default naming a function
resolves in the declaring module, and the caller gains no access to
that function by name -- the value flows into the call, the name does
not. Omission at the call site:
#secref(<expressions-argument-lists>).

```ritchie
greet(String name, Int times = 1, String suffix = "!") { ... }
connect(String host, Int port = DEFAULT_PORT) { ... }
retry(Task t, (Int) -> Duration backoff = defaultBackoff) { ... }
```

== Visibility
<declarations-visibility>
#idx("visibility")
Visibility is fixed by archetype and declaration kind. There are no
access modifiers:

#figure(
  align(left)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Archetype], [Component], [Visibility],),
    table.hline(),
    [regular type, enum], [type and published methods], [any module if
    it has a published method, \ otherwise the declaring module only],
    [regular type, enum, service], [private methods (`_` prefix)], [the
    declaring module only],
    [regular type, enum], [fields or members], [the declaring module only],
    [module-level], [aliases (#secref(<types-type-aliases>)) and
    constants], [the declaring module only],
    [module-level], [free-standing functions and free-form queries],
    [the declaring module only],
    [error], [type], [any module],
    [entity], [type, references, methods, and lookup], [the declaring
    module only],
    [entity], [fields], [its own methods only],
    [entity], [identity `$T#`], [any module],
    [service], [type and published methods], [any module],
    [explicit projection], [type and query methods], [any module],
    [explicit projection], [columns], [any module, through a row],
    [implicit projection `%T`], [type, columns, and query methods], [the
    declaring module only],
    [contract], [declaration], [any module],
  )]
  , kind: table
  )

Only published methods (#secref(<declarations-functions-and-methods>)) count when publishing a regular
type or enum; private methods and language-defined built-in methods do
not. Entity
identity and lookup are detailed in #secref(<entities-identity>, <entities-lookup>, conj: "and")\; projection visibility is detailed in
#secref(<queries-visibility>).

== Entry point
<declarations-entry-point>
#idx("main", code: true)
#idx("entry point")
Every free-standing function named `main` has no parameters and no
return clause, whether it occurs in root or dependency source.

The root source set is the source selected directly by the project or
ad-hoc invocation, excluding dependency source. It contains zero or one
`main()`. When present, it is the entry point and the module that
declares it is the entry module. More than one root module declaring
`main()` is a compile error identifying every site. A `main()` in
dependency source is not an entry point for the consuming source set.
