#import "../prelude.typ": horizontalrule, idx, secref

= Statements
<chap-statements>
The statement forms, what each does, and how blocks scope the names they
introduce.

#horizontalrule

== Statements and separation
<statements-statements-and-separation>
A block runs its statements in written order. Statements are separated by
newlines or semicolons. Newline significance follows the bracket kind. Inside
`()` and `[ ]` newlines are insignificant, so a parenthesised expression or a
collection literal may wrap across lines. Inside `{ }` a `;` is interchangeable
with a newline, and either separates the items of a field, enum, initializer, or
arm list as `,` does.


```ritchie
Int total = (a +
    b + c)              // parens keep this as one statement
count = count + 1       // next statement
sum += 1; prod *= n     // two statements
```

A binary operator at the end of a line does not continue the
statement.  To split a long expression across lines, wrap it in
parentheses or use a trailing `\`.

The statement forms are: variable declaration, constant declaration, expression statement
(including assignment and calls), bare block, `return`, `if`/`else`,
`for`, labelled `for`, `break`, `continue`, `resume`, and `match`. Each has its
own section below.

#horizontalrule

== Blocks and scope
<statements-blocks-and-scope>
#idx("block")
#idx("scope")
A block `{ ... }` groups statements and introduces a scope. A name is
introduced after its variable declaration's initializer completes. The
initializer resolves names in the preceding scope, so an inner variable declaration's
initializer may read a shadowed outer variable. Names in a multi-variable declaration
are introduced together. Each introduced name remains visible through
the end of its enclosing block. A scope also fixes a value's lifetime.
The value lives until its block exits.

A name already introduced in the current scope cannot be introduced again in that
scope. A function's parameters are introduced in the body's outermost scope,
so a declaration there cannot reuse a parameter's name, while a nested block
may shadow it. An inner block may introduce a name that shadows an outer one,
and the inner declaration holds until the inner block ends.

```ritchie
Int x = 1
Int x = 2               // error: x already declared in this scope
{
    Int x = 3           // ok: inner block shadows the outer x
}                       // inner x ends here, outer x is 1 again
```

A bare block is itself a statement. It groups statements and creates a
scope around the included declarations. Statements do not reduce to a
value. An inline handler adds no block scope. An explicit handled block
retains this scope (#secref(<statements-handling-and-resumption>)).

#horizontalrule

== Variable declarations
<statements-variable-declarations>
#idx("variable declaration")
A variable declaration introduces one or more names, each with a type written before
it. Every target in an ordinary variable declaration carries a written
type. A statement that opens with a type-shaped token is a variable declaration. One
that opens with a value is an expression statement (#secref(<statements-expression-statements-and-assignment>, cap: true)).

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Form], [Syntax], [Means],),
    table.hline(),
    [declaration without an initializer], [`String host`], [name and type, no value yet],
    [declaration with an initializer], [`Int8 count = 63`], [name, type, and value],
    [same-type multi], [`Int x, y = 1, 2`], [one type, several names],
    [mixed multi], [`Int n, String s = f()`], [a type per name],
  )]
  , kind: table
  )

A declaration without an initializer uses a variable-case name and introduces the
name without a value. The compiler accepts a read when every path reaching
it has initialized the variable (#link(<chap-types>)[Types]). Every
multi-variable declaration has an `=`. A same-type list writes the type once and shares
it across the names. A mixed list writes a type before each name. The
right side is a value list, or a single call that produces one value per
target. The number of values must match the number of targets.

```ritchie
Decimal balance                     // assigned on a later line, before any read
Int8 count = 63
Int x, y = 1, 2                     // x and y are both Int
Int age, String name = parseRow(r)  // age from the first value, name from the second
User u = .new("bob", 30)            // leading-dot initializer (Expressions)
```

A variable declaration's initializer is any expression, or a leading-dot member
resolved against the declared type
(#link(<chap-expressions>)[Expressions]). When a recovery completes
without supplying values, the declaration introduces its names without
values (#secref(<errors-block-and-loop-handling>)). Multi-variable declarations commit their
names together after all evaluations and required checks complete.

Every declared variable is read by some expression in its scope. A variable
that no expression reads is a compile error (`variable.unread`), as is the
error variable `E! e` or `any e` of a handler arm that no expression of its
arm reads. A loop variable, a parameter, and the binding of a typed arm
`case T v` may stay unread. The check applies to a program that has no other
compile error.

#horizontalrule

== Constant declarations
<statements-constant-declarations>
#idx("constant")
A target written in constant case -- all uppercase, two or more
characters (#link(<chap-lexical>)[Lexical]) -- introduces a constant
declaration. Its mandatory initializer is a constant expression evaluated
during compilation (#secref(<declarations-top-level-declarations>)).
The name denotes that value throughout its scope. Assignment to the name
is a compile error.

```ritchie
Decimal TAX_RATE = 0.0825
Int MAX_LINE_ITEMS = 50

MAX_LINE_ITEMS = 60     // error: a constant cannot be reassigned
```

Constant case supplies the immutability marker. A constant declaration obeys
the same scope and same-scope uniqueness rules as any variable declaration (#secref(<statements-blocks-and-scope>, cap: true)).

#horizontalrule

== Expression statements and assignment
<statements-expression-statements-and-assignment>
An expression alone is a statement. It is evaluated and its result is
discarded. The forms used this way are calls and assignments.

```ritchie
cart.addItem(line)
@Inventory.reserve(sku, qty)
```

Assignment updates an existing variable, field, or element. Assignment
(`=`) and compound assignment (`+= -= *= /= %= ??=`) are statement-level
forms. Neither may appear as a condition, a declaration initializer, or any
other operand of a larger expression
(#link(<chap-expressions>)[Expressions]). When a recovery completes a
failed assignment without supplying values, a target variable has no
value afterwards. A no-op arm cannot complete a failed assignment to a
field, element, or entry (#secref(<errors-block-and-loop-handling>)).

```ritchie
order.total = subtotal + tax
order.lines[i] = line
payload[i] = byte
config.retries ??= 3             // assigns only when config.retries is absent
```

An index or key operand in an assignment target must not be able to fail.
An operand whose inferred raise set is not empty is a compile error
(#link(<errors-inferred-raise-sets>)[Inferred raise sets]). The validation of the target place
itself, such as the bounds check of its final index, can fail, and that
failure is a failure of the assignment.

```ritchie
counts[pos] = 0                  // yes: pos cannot fail
counts[slot(key)] = 0            // no: slot can fail
Int s = slot(key)                // do this
counts[s] = 0
```

A single `=` may also store a list of targets from a positional value
list -- the multi-target form. All right-hand values and all left-hand
index and key subexpressions are evaluated before any store; the stores
then run left to right, so a swap needs no temporary. The counts must
match (no coercion), unless a single multi-return call fills the whole
list as the sole right-hand side; on a shared location the last store
wins. It is plain `=` only and does not chain.

```ritchie
a, b = b, a                      // swap two names
xs[i], xs[j] = xs[j], xs[i]      // swap two elements
lo, hi = bounds()                // a single multi-return call fills both
```

The forms below are rejected because the assignment sits inside another
expression.

```ritchie
if total = subtotal { ... }      // no: an assignment is not a condition
Int n = (count += 1)             // no: not a declaration initializer
applyDiscount(total -= 10)       // no: not a call argument
```

There is no `++` or `--`. Write `count += 1`.

#horizontalrule

== Return statements
<statements-return-statements>
#idx("return", code: true)
A `return` ends the enclosing function or method. It may carry a
value. Its operands evaluate before the return takes effect. Recovery
of a failed return supplies the values that it returns. Diversion
cancels the unfinished return. A return in a handler arm still targets
the enclosing callable.

```ritchie
return order.total
return                  // a function with no declared return type
```

The returned value must match the function's declared return type
(#link(<chap-types>)[Types]). A return whose value is of an error type
is a raise, permitted regardless of the declared return type
(#secref(<errors-raising-and-propagation>)). A function with a return type
must not reach the end of its body. Every path that reaches completion
must do so through a matching `return`. A path that remains in an
infinite loop does not reach the end. A function with no return type
ends with a bare `return` or by reaching the end of its body.

A function declaring a multi-return list returns a matching value list,
comma-separated and matched positionally. A call of a function with the
same return list may stand as that list (#secref(<types-function-types>)).

```ritchie
parseRow(String r) -> (Int, String) {
    return 30, "bob"
}
```

The return value is an ordinary expression. It may not be an assignment
or compound assignment (#secref(<statements-expression-statements-and-assignment>, cap: true)). To return an updated value, assign
on one line and return on the next.

```ritchie
return total += tax     // no: an assignment is not a value
total += tax            // do this
return total
```

#horizontalrule

== If and else
<statements-if-and-else>
#idx("if", code: true)
An `if` runs a block when its condition holds. Further conditions chain
with `else if`, and a final `else` runs when none of them held.

```ritchie
if order.total > 100 {
    applyDiscount(order)
} else if order.total > 50 {
    addFreeShipping(order)
} else {
    chargeStandard(order)
}
```

The condition is a `Bool`, or a nullable tested for presence
(#link(<chap-expressions>)[Expressions]). No parentheses are required
around it, and the block braces are mandatory. The condition may not be
an assignment or compound assignment (#secref(<statements-expression-statements-and-assignment>, cap: true)).

A condition that tests a nullable narrows it to present inside the block
that runs when the test holds (#link(<chap-expressions>)[Expressions]).

```ritchie
if customer.coupon != none {
    redeem(customer.coupon)     // coupon is present here
}
```

For control-flow analysis, a compile-time Boolean constant contributes
only its evaluated path. Any other condition contributes both its true
and false paths. A variable is initialized after an `if` chain only when
every path reaching the following statement initialized it.

#horizontalrule

== For loops
<statements-for-loops>
#idx("for", code: true)
#idx("loop")
`for` is the only loop keyword. It has three forms.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Runs],),
    table.hline(),
    [`for { }`], [until a `break` or `return` leaves it],
    [`for cond { }`], [while `cond` holds],
    [`for x in iter { }`], [once for each element of `iter`, with `x`
    naming it],
  )]
  , kind: table
  )

```ritchie
for {                               // infinite
    Order? next = queue.pop()
    if next == none { break }
    process(next)
}

for remaining > 0 {                 // condition
    ship(order)
    remaining -= 1
}

for line in order.lines {           // iterator
    total += line.price
}
```

A `{` after `for` without any intermediate conditional expression
creates an infinite loop body.

In the iterator form, the type of `x` is not declared. It takes the
element type of `iter` (#link(<chap-types>)[Types]). The condition
form follows the same rules as an `if` condition (#secref(<statements-if-and-else>, cap: true)). The
condition must always be a `Bool` expression.

For an array source, the source expression is evaluated once before the
first iteration. The array selected by that evaluation and its element
count form the loop source. Assigning another array to a name used by
the expression does not redirect the loop. Iteration visits the selected
array's indices in increasing order from zero to one less than that
captured count.

At the start of each iteration, the loop variable is initialized with an
independent value copied from the element then stored at the current
index. Assigning or mutating the loop variable does not update the array
element. An element assignment made through the selected array before
its index is reached is observed when that iteration begins. Elements
added after the count was captured are not visited.

For control-flow analysis, `for {}` and a condition loop whose condition
is a compile-time `true` have no condition-false exit. A condition loop
whose condition is a compile-time `false` executes zero times. Any other
condition contributes both its true and false paths. An iterator loop
executes zero times when its source is empty.

An iterator may yield more than one value per step. The loop variables match
them positionally, one name per value, as a multi-variable declaration matches a
multi-value return (#secref(<statements-variable-declarations>, cap: true)); each loop variable stays typeless and takes its
value's type. A dict yields two values per step, the key then the
value.

```ritchie
for region, rate in taxRates {           // taxRates is [String: Decimal]
    log(f"{region}: {rate}")
}
```

Numeric ranges use the `range()` built-in rather than special syntax. A
`range(...)` call is a local counting source consumed directly by the
iterator form of `for`. It supplies successive loop values but is not
itself a value.

```ritchie
for month in range(1, 13) {         // 1 through 12
    sendStatement(account, month)
}
```

A variable is initialized after a loop only when every path reaching the
following statement initialized it. Those paths consist of the
applicable condition-false or source-exhaustion paths and every `break`
targeting that loop. A `return` exits the routine, a `break` exits its
target loop, and a `continue` transfers to its target loop's next
iteration. None of them proceeds to the following statement in its
current block.

#horizontalrule

== Labelled loops, break, and continue
<statements-labelled-loops-break-and-continue>
#idx("break", code: true)
#idx("continue", code: true)
#idx("label")
`break` leaves a loop. `continue` skips to its next iteration. With no
label, each acts on the innermost enclosing loop.

```ritchie
for line in order.lines {
    if line.cancelled { continue }      // skip this line
    if line.quantity == 0 { break }     // stop the loop
    post(line)
}
```

A loop may carry a label, written as a name and `:` before `for`.
`break label` and `continue label` then act on the labelled loop instead
of the innermost one. Only a loop can be labelled.

A label's scope is its labelled loop. A labelled `break` or `continue`
must name an enclosing labelled loop. The unlabelled forms target the
innermost enclosing loop and are invalid outside a loop. Labels with
overlapping scopes must have different names. A name may be reused after
its labelled loop ends. A label is written as a variable-case name.
Labels occupy a namespace of their own, so a label and a variable may
share a name without either hiding the other. An unused label is valid.
Inside a handler arm, only loops declared inside that arm are ordinary
transfer targets. Otherwise unlabelled transfers use the handler rules
(#secref(<errors-block-and-loop-handling>)). Labels outside the arm are
unavailable there.

```ritchie
scan: for order in orders {
    for line in order.lines {
        if line.sku == target {
            hit = order
            break scan                  // leaves both loops
        }
    }
}
```

#horizontalrule

== Match
<statements-match>
#idx("match", code: true)
#idx("pattern")
`match` is a control-flow statement. It selects at most one arm by a
subject value and runs that arm's body. A match body contains one or
more arms. Every arm introduces a local scope. Its body is one
statement or a brace block containing several statements.

```ritchie
match order.status {
    case PLACED => reserve(order)
    case SHIPPED => track(order)
    case DELIVERED => archive(order)
    case CANCELLED => refund(order)
}
```

The subject is evaluated once. Arms are tried from top to bottom. Each
candidate in an equality arm is an ordinary single-value expression
checked in the subject's type context. Candidates are evaluated from
left to right only when selection reaches them. The first candidate
equal to the captured subject selects its arm. No later candidate or
arm is evaluated. A failure while evaluating the subject or a reached
candidate propagates through the enclosing context.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Arm], [Matches when],),
    table.hline(),
    [`case v =>`], [the subject equals `v`],
    [`case v1, v2 =>`], [the subject equals any listed value],
    [`case predicate =>`], [the `Bool` predicate holds, with the subject
    in scope],
    [`case Type name =>`], [the subject's active sum member is `Type`,
    named `name`],
    [`any =>`], [nothing above matched],
  )]
  , kind: table
  )

A `case` arm contains one or more comma-separated candidates. An `any`
arm is optional, may occur once, and must be the final arm. Every arm
must have a body. A name declared in an arm is visible only within that
arm.

A `case` is an equality test when its pattern has the subject's type,
and a predicate when its pattern is a `Bool`. A predicate may name the
subject directly. Within a match over an enum subject, a bare CONSTANT
pattern resolves against the subject's enum type before any other
local name, so `case PLACED` over a `Status` subject names
`Status.PLACED`\; the qualified form is always valid. Over a `Bool`
subject every case pattern is an equality test -- predicate arms are
unavailable there, and a multi-way boolean choice is written with
`if`/`else`. Over a closed payload sum, an arm of the form
`case Type name` matches when the subject's active member is that type
and introduces a pattern variable of that type, written type-first, for the arm's block
(#link(<chap-contracts>)[Contracts]).

```ritchie
match response.code {
    case 200 => proceed()
    case 400 <= response.code < 500 => reportClient()
    any => reportServer()
}
```

An ordinary match need not cover every possible subject value. If no
arm matches, execution continues with the statement after the match.
An `any` arm handles every subject value not matched by an earlier arm.

#horizontalrule

== Handling and resumption
<statements-handling-and-resumption>
A `handle` suffix may follow any statement. What it protects and how an arm
disposes of the recovered statement, including the meaning of `continue`,
`break`, and `return`, are specified in
#secref(<errors-block-and-loop-handling>).

A handler containing exactly one arm may use the compact form from
#secref(<errors-inline-handle>) without braces around its arm list. This
applies uniformly to every statement that a handler can follow. Every handler
contains at least one arm.

`resume` is valid only inside a handler arm and supplies the values of the
recovered statement. An ordinary loop inside the arm does not change that destination.
A nested handler arm establishes its own. Brace blocks do not yield values.
The complete arm, scope, control, and fallback rules are in
#secref(<errors-inline-handle>, <errors-fallback-and-nested-handling>, range: true).
