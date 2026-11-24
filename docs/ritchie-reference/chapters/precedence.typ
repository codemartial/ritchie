#import "../prelude.typ": horizontalrule, idx, part-numbering

#set heading(numbering: part-numbering)
== Operator Precedence
<chap-precedence>
Operator precedence from highest to lowest is as follows,
along with associativity. The expression forms themselves are in
#link(<chap-expressions>)[Expressions].

#figure(
  align(center)[#table(
    columns: 4,
    align: (auto,auto,auto,auto,),
    table.header([Level], [Form], [Operators], [Associativity],),
    table.hline(),
    [1], [postfix], [`.` `(...)` `[...]`], [left],
    [2], [prefix], [`-`], [right],
    [3], [multiplicative], [`* / %`], [left],
    [4], [additive], [`+ -`], [left],
    [5], [coalescing], [`??`], [right; nullable left],
    [6], [ordering], [`< > <= >=`], [left (see chaining)],
    [7], [equality], [`== !=`], [left (see chaining)],
    [8], [negation], [`not`], [right],
    [9], [logical and], [`and`], [left],
    [10], [logical or], [`or`], [left],
    [11], [ternary], [`? :`], [non-associative],
    [12], [assignment], [`= += -= *= /= %= ??=`], [right, statement-level],
  )]
  , kind: table
  )

Level 1 (postfix) is field access, method and function calls, indexing,
and slicing. Level 2 (prefix `-`) has higher precedence than every binary
operator, so `-a * b` is `(-a) * b`.

Reading down the table, each level has lower precedence than the one
above: `a + b * c` is `a + (b * c)`\; `a < b and c < d` is
`(a < b) and (c < d)`\; `a or b and c` is `a or (b and c)`.

#strong[Chaining.] Levels 6 and 7 carry one exception to
left-association: a run of two comparison operators over three operands
is a single chained comparison, not a left-nested pair
(#link(<chap-expressions>)[Expressions]). `(a < b) < c` parenthesises
out of it.

#strong[Negation] (`not`) has lower precedence than comparison and higher
precedence than `and`: `not a == b` is `not (a == b)`, `not a < b < c` negates
the whole chain, and `not a and b` is `(not a) and b`. It associates
right, so `not not a` is accepted. It may not stand as the operand of
a tighter operator: `a == not b` is rejected. Write `a == (not b)`.

#strong[Coalescing] (`??`) sits below comparison and above additive, so
`score ?? 0 < limit` is `(score ?? 0) < limit` and `m ?? n + 1` is
`m ?? (n + 1)`. It associates right (`a ?? b ?? c` is `a ?? (b ?? c)`)
and its left must be nullable. `??` and the ternary may not be direct operands
of one another after parentheses are removed
(#link(<chap-expressions>)[Expressions]).

#strong[Ternary] is non-associative. A ternary may not be the condition or an
arm of another ternary after parentheses are removed, so
`a ? b : (c ? d : e)` is rejected. Containment behind another expression,
as in `a ? f(c ? d : e) : g()`, is accepted
(#link(<chap-expressions>)[Expressions]).

#strong[Assignment] (`=`) and #strong[compound assignment]
(`+= -= *= /= %= ??=`) appear only at statement level. Simple `=` associates
right within a chain (`a = b = c`); compound forms do not chain.
Multi-target assignment (`a, b = b, a`) is also statement-level
(#link(<chap-statements>)[Statements]).

#strong[Leading-dot member] (`.name`, `.name(args)`) is a
declaration-initializer form resolved against the declared type
(#link(<chap-expressions>)[Expressions]), not an operator in this table.

There is no cast operator; type conversion is an ordinary call
(#link(<chap-expressions>)[Expressions], #link(<chap-types>)[Types]).
