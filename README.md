# Ritchie

Ritchie is a programming language for distributed applications. It
targets an abstract machine that models a distributed system rather
than a single computer. Thus Ritchie programs are decomposable at
deployment time, whether they are deployed as one monolithic process or
distributed across a cluster.

The abstract machine is documented in [RITE](docs/rite.md) (Reliable
Interaction with Typed Endpoints). RITE incorporates the conceptual
building blocks of distributed systems, such as stateless services,
transactional state, query views, event streams and a service mesh. It
also defines interaction modes between these components following
established reliability patterns.

Ritchie gives the syntactic form to RITE's concepts. Its compiler
provides the enforcement of the RITE model and ensures that even in a
monolithic form, the components are distinct and ready for deployment
into a distributed topology. The compiler lowers each program to a
small kernel language and generates Go, which it builds into a native
executable.

Ritchie is also designed to be a productive general-purpose
programming language for business applications. It includes built-in
types for decimal numbers, civil dates and times, timestamps, and
durations, among other conveniences. It has value semantics for local
types. It has sequential, single-threaded local execution while
providing concurrent invocation handling and automatic concurrency for
remote interactions to reduce latency. A remote read does not hold up
the statements after it, and the program waits for its result where
the value is used. Remote mutations run in written order, because they
change state that other units observe.

This repository holds the [compiler](src/), the [language
reference](docs/ritchie-reference/), the [RITE document](docs/rite.md),
and an [agent skill](skill/SKILL.md) for writing Ritchie.

## Status

Ritchie is in active, early-stage development. The complete, proposed
feature set is as documented in the
[reference](docs/ritchie-reference/).  The implemented feature set is
as documented in the [skill](skill/SKILL.md). Ritchie programs
currently compile into a modular monolith.

## A first program

```ritchie
Missing! { String item }

priceOf([String: Decimal] prices, String item) -> Decimal {
    Decimal? price = prices.get(item)
    if price {
        return price
    }
    return Missing! { item: item }
}

main() {
    [String: Decimal] prices = ["tea": 3.50, "cake": 4.25]
    Decimal total = priceOf(prices, "tea") + priceOf(prices, "cake")
    print(f"total {total}")
    Decimal extra = priceOf(prices, "scone") handle Missing! => 0.00
    print(f"extra {extra}")
}
```

`Missing!` is an error type, and returning one of its values raises it.
`handle` recovers the statement it follows, so the missing price becomes
`0.00`. `Decimal` is decimal arithmetic, so the prices add exactly. The
program prints:

```text
total 7.75
extra 0.00
```

## Building and running

Building the Ritchie compiler needs Go 1.27, and the compiler uses the
Go standard library only. The Ritchie compiler translates a program to
Go and builds the result with the `go` command, so `ritchie compile`
and `ritchie run` need Go 1.27 on the `PATH`. The executable that
`ritchie compile` creates is a normal binary that does not require the
Go toolchain.

```sh
go -C src test ./...
go -C src install ./cmd/ritchie
cp src/bin/rcc src/bin/rcrun "$(dirname "$(command -v ritchie)")"
rcrun program.ri
```

`go -C src install ./cmd/ritchie` installs the `ritchie` command, with
the subcommands `compile`, `run`, and `kernel print`. The scripts
`rcc` and `rcrun` under [`src/bin/`](src/bin/) run `ritchie compile` and `ritchie
run` from the directory that holds them, so they are copied beside
`ritchie`. `rcrun -` reads one source file from standard input, and
`rcrun --help` lists the options. `print` writes to standard output,
and `log` and error reports write to standard error.

## Documents

| Path | Contents |
|---|---|
| [`docs/ritchie-reference.pdf`](docs/ritchie-reference.pdf) | The Ritchie language reference |
| [`docs/ritchie-reference/`](docs/ritchie-reference/) | The Typst sources of the reference |
| [`docs/rite.md`](docs/rite.md) | RITE, the execution model: endpoints, invocation, outcomes, deadlines, and state ownership |
| [`skill/SKILL.md`](skill/SKILL.md) | The agent skill, with a compact guide to the language as this compiler accepts it, whose examples compile and run |
| [`src/doc.go`](src/doc.go) | The command reference, also shown by `go doc ./src` |

`typst compile --root docs docs/ritchie-reference/main.typ ritchie-reference.pdf`
renders the reference from its sources.

## Licence

The compiler is licensed under the [Functional Source License, Version
1.1, ALv2 Future License](LICENSE), and each release becomes available
under the Apache License, Version 2.0, two years after its publication.
