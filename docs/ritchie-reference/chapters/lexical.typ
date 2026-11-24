#import "../prelude.typ": horizontalrule, idx, secref, section-numbering, subsection-numbering

= Lexical Specification
<chap-lexical>
How Ritchie source text is divided into tokens: the spellings of
identifiers, keywords, literals, operators, and delimiters, and the
whitespace, newline, and comment rules that surround them.

#horizontalrule

== Source text
<lexical-source-text>
- #strong[Encoding.] Source is a well-formed UTF-8 byte sequence,
  decoded without Unicode normalization. A single UTF-8 BOM at byte
  offset zero is accepted as an encoding signature and is not part of
  the source text. U+FEFF elsewhere is an ordinary source character.
- #strong[Identifiers are ASCII.] Identifier and keyword characters are
  drawn from `[A-Za-z0-9_]` plus any archetype sigils such as `$ @ %` (leading)
  or `!` (trailing). Non-ASCII characters appear only inside string
  literals and comments. This keeps the case-based archetype rules
  (#secref(<lexical-identifiers-and-archetypes>, cap: true)) unambiguous.
- #strong[Case is significant] everywhere: in keywords, in identifiers,
  and in the letter-case rules that assign an identifier to its
  archetype.

#horizontalrule

== Whitespace and layout
<lexical-whitespace-and-layout>
#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Character], [Role],),
    table.hline(),
    [space, tab, `\r`], [layout only; separates tokens, otherwise
    ignored],
    [newline (`\n`)], [a token; significance depends on bracket depth],
  )]
  , kind: table
  )

Whitespace is not required between tokens that are already
distinguishable (`a+b` and `a + b` lex identically), but it is
required for separating two adjacent word tokens (`for x` is two
tokens; `forx` is one).

#horizontalrule

== Lines and Terminators
<lexical-lines-and-terminators>
A newline is a token whose significance depends on the type of
enclosing brackets, if any.

- #strong[At bracket depth zero] a newline terminates a statement or a
  declaration.
- #strong[Inside `(` `)` and `[` `]`] newlines are insignificant. An
  expression, argument list, or collection literal may span as many
  lines as desired.
- #strong[Inside `{ }`] a newline terminates a statement
  in a block. In field, enum, initializer, and arm lists it is
  interchangeable with `,`.
- #strong[Runs collapse.] Any run of consecutive newlines (including
  blank lines) is a single terminator. Leading and trailing blank lines
  are harmless.

A `;` is accepted as a statement separator for placing multiple
statements on the same physical line. It is also accepted, but not
recommended, as a trailing punctuation for a sole statement on a line.

A trailing `\` on a line signifies a continuation for the present
statement over to the next line. Trailing here implies that it is the
last non-whitespace character on the line.

#horizontalrule

== Comments
<lexical-comments>
#idx("comment")
#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Rule],),
    table.hline(),
    [`// ...`], [line comment; runs to the end of the line],
    [`/* ... */`], [block comment; #strong[nests]\; may span lines],
  )]
  , kind: table
  )

Block comments can be nested. Every `/*` must be matched by a later
`*/`, so a region that already contains a block comment can be
commented out wholesale.

```ritchie
/* disable this path:
   /* legacy branch, already commented */
   handleLegacy()
*/
```

A block comment that is not closed is a lexical error, even though it
may be the final text of a source file. Comment delimiters and
non-newline contents are layout and do not produce tokens. The newline
ending a line comment and every newline inside a block comment remain
governed by #secref(<lexical-lines-and-terminators>, cap: true).

#horizontalrule

== Token classes
<lexical-token-classes>
Every non-layout, non-comment character belongs to exactly one token:

- #strong[Identifiers] sorted into eight archetypes (#secref(<lexical-identifiers-and-archetypes>, cap: true))
- #strong[Keywords] (#secref(<lexical-keywords>, cap: true))
- #strong[Literals]: integer, float, string, backtick string, f-string,
  boolean, duration, date, time, datetime (#secref(<lexical-literals>, cap: true))
- #strong[Operators and delimiters] (#secref(<lexical-operators-and-delimiters>, cap: true))

#horizontalrule

== Identifiers and archetypes
<lexical-identifiers-and-archetypes>
#idx("identifier")
#idx("sigil")
An identifier is sorted into one of eight #strong[archetypes] by its
leading sigil and its letter case. The sigil (if any) is recognised
first; the remaining word is then classified by case alone.

=== Bare identifiers (no sigil)
A bare word matches `[A-Za-z_][A-Za-z0-9_]*` and is classified by:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Archetype], [Rule], [Examples],),
    table.hline(),
    [`VARIABLE`], [starts with a lowercase letter or `_`], [`count`,
    `userName`, `_tmp`],
    [`TYPE_PARAM`], [a single uppercase letter], [`T`, `U`, `K`],
    [`CONSTANT`], [uppercase-led, 2+ chars, no lowercase letter], [`PI`,
    `MAX_SIZE`, `P0`],
    [`TYPE`], [uppercase-led, 2+ chars, at least one lowercase], [`Int`,
    `Point`, `HttpClient`],
  )]
  , kind: table
  )

The four rules are mutually exclusive and total over the bare-identifier
pattern. The discriminator is purely letter case:

- First character lowercase or `_` gives a `VARIABLE`.
- A lone uppercase letter is always a `TYPE_PARAM` (so there are no
  single-letter constants; use two or more characters).
- An uppercase-led word with no lowercase letter is a `CONSTANT`\;
  digits and `_` are allowed (`MAX_BUFFER_LEN`, `HTTP2`).
- An uppercase-led word containing any lowercase letter is a `TYPE`.

#strong[Conventions.] Variables are `lowerCamelCase` or `snake_case`.
Types are `PascalCase` with no underscores. Constants are
`SCREAMING_SNAKE_CASE`.

=== Sigil archetypes
Sigils mark the distributed-system archetypes. Three are prefixes that
classify the type; one is a suffix that marks the failure archetype.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Archetype], [Form], [Examples],),
    table.hline(),
    [`ENTITY_TYPE`], [`$` + `TYPE`], [`$Account`, `$Order`],
    [`SERVICE_TYPE`], [`@` + `TYPE`], [`@PaymentGateway`, `@Mailer`],
    [`PROJECTION_TYPE`], [`%` + `TYPE`], [`%Order`, `%InvoiceSummary`],
    [`QUERY`], [`%` + `VARIABLE`], [`%byDay`, `%regionRevenue`],
    [`ERROR_TYPE`], [`TYPE` + `!`], [`NotFound!`],
  )]
  , kind: table
  )

The `$`, `@`, and `%` prefixes attach directly to their bodies. `$` and
`@` require a `TYPE` body. `%` requires either a `TYPE` or a `VARIABLE`
body. The `!` suffix attaches directly to a `TYPE` body. Each body
follows the corresponding bare-identifier spelling and case rules. A
prefix names what kind of thing a type is. The suffix marks an outcome
mode (`!` for failure, paired with the `?` of a nullable type).

The projection sigil `%` is the eighth archetype and the one prefix
that takes either case. Its two forms are two readings of the read
model: `%` + `TYPE` is a projection, a derived row shape (`%Order`);
`%` + `VARIABLE` is a query, a derived read that yields such rows. A
query is invoked like a function -- `%byDay(args)`, or
`%Order.byDay(args)` on a projection -- and the parentheses and
arguments are the grammar's call form, the same one functions use. An
author reads both forms as projections, and the case tells a row
definition from a read of it. The token classes `PROJECTION_TYPE` and
`QUERY` are used to distinguish them in the grammar.

`$`, `@`, and `!` are not operators on their own; `%` is also the modulo
operator:

- `$` and `@` carry meaning only as archetype prefixes immediately
  before a `TYPE` body.
- `%` is the projection prefix when it leads an operand immediately
  before a `TYPE` or `VARIABLE` body (`%Order`, `%byDay`), and the modulo
  operator when it falls between two operands (`a % b`). Whitespace does
  not affect this split, thus `a%b` is `a % b`. Parse position resolves
  the two, the same split that lets `-` serve as negation and subtraction
  (#secref(<lexical-operators-and-delimiters>, cap: true)).
- `!` is meaningful only as the operator `!=`, or as the `ERROR_TYPE`
  suffix directly after a `TYPE`. A `!` immediately followed by `=` is
  always `!=`. A `!` after a `TYPE` and not followed by `=` is the
  error suffix. Logical negation is the `not` keyword rather than `!`.

#horizontalrule

== Keywords
<lexical-keywords>
#idx("keyword")
_Keywords_ are reserved words and thus not usable as identifiers:

```ritchie
and        any        break      case       contract   continue
create     else       embed      exhaustive false      for
handle     if         in         is         match      none
not        or         resume     return     self       then
true       unique     where
```

`true` and `false` are the boolean literals; `none` is the absent-value
literal. `and`, `or`, and `not` are the boolean operators (Ritchie has no
`&&`, `||`, or a negation `!` operator -- see #secref(<lexical-excluded-forms>, cap: true)). All other
words are control-flow, declaration, or archetype keywords defined in
#link(<chap-grammar>)[the grammar].

=== Contextual query vocabulary
A second set of words is meaningful only inside a `%` clause body, the
body of a projection or query declaration
(#link(<chap-queries>)[Queries]). There they introduce clauses and
aggregates. Everywhere else they are ordinary identifiers, so a variable
named `count`, `from`, or `limit` is legal outside a clause body, and a
program that writes no query sees no new keywords.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Role], [Words],),
    table.hline(),
    [clause], [`from`, `select`, `group`, `by`, `having`, `sort`,
    `limit`],
    [sort order], [`asc`, `desc`],
    [aggregate], [`count`, `sum`, `avg`, `min`, `max`, `bucket`],
  )]
  , kind: table
  )

`where` also appears in a clause body but stays a reserved keyword
(above), because it serves the contract bound as well
(#link(<chap-contracts>)[Contracts]). The grammar tells the two
positions apart. The `in` predicate operator is likewise the reserved
`in` keyword. The contextual words are recognised only where the grammar
expects a clause keyword or a select entry.

=== Predefined built-in names
The built-in functions (#link(<chap-builtins>)[Built-ins]) -- `range`,
`log`, `count`, `map`, and the rest -- are ordinary identifiers, not
reserved words. They are declared in every scope, need no import, and a
local declaration of the same name shadows the built-in within its scope.
Several of them (`count`, `sum`, `min`, `max`) spell the same word as a
query aggregate above; inside a `%` clause body the word is the
aggregate, and elsewhere it names the built-in function.

#horizontalrule

#set heading(numbering: subsection-numbering)
== Literals
<lexical-literals>
#idx("literal")
=== Integer
<lexical-integer>
Decimal by default, with three alternate radixes. A single `_` may sit
between two digits as a grouping separator; it may not lead, trail, or
sit adjacent to the radix prefix.

#figure(
  align(center)[#table(
    columns: 4,
    align: (auto,auto,auto,auto,),
    table.header([Radix], [Prefix], [Digits], [Example],),
    table.hline(),
    [decimal], [none], [`0`-`9`], [`1_000_000`],
    [hex], [`0x`], [`0`-`9 a`-`f A`-`F`], [`0xFF_FF`],
    [octal], [`0o`], [`0`-`7`], [`0o755`],
    [binary], [`0b`], [`0 1`], [`0b1010_0001`],
  )]
  , kind: table
  )

Radix prefixes are lowercase (`0x`, not `0X`); hex digits may be either
case.

=== Float
<lexical-float>
A float is decimal only and is distinguished from an integer by a
fractional part, an exponent, or both:

```ritchie
3.14159          // digits "." digits
3.141_59         // separators allowed in each digit run
6.022e23         // exponent, no sign
1.6e-19          // signed exponent
2e10             // exponent with no fractional part
```

Structure: an integer part, an optional `.` followed by a fractional
part, and an optional exponent (`e` or `E`, an optional `+`/`-`, then
digits). At least one of the fractional part or the exponent must be
present, otherwise the token is an integer. A `.` is a float point only
when a digit follows it, so `x.field` is field access, not a malformed
float. `_` separators follow the same between-digits rule as integers.

=== Quoted String and byte arrays
<lexical-quoted-string-and-byte-arrays>
A double-quoted literal produces `String`. A `b` immediately before the
opening quote produces `Byte[]`. Neither form may contain a raw newline;
multi-line contents use a backtick literal (8.4) or explicit `\n`.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Escape], [Meaning],),
    table.hline(),
    [`\n`], [newline],
    [`\t`], [tab],
    [`\r`], [carriage return],
    [`\\`], [backslash],
    [`\"`], [double quote],
    [`\0`], [null; exactly one character follows the backslash, so a
    digit after it is ordinary text],
    [`\u{ ... }`], [Unicode scalar by hex code point, 1-6 hex digits],
    [`\xHH`], [one byte by exactly two hex digits; byte arrays only],
  )]
  , kind: table
  )

`\u{ ... }` lets source stay ASCII while embedding any Unicode scalar.
In a byte-array literal, source characters and Unicode escapes contribute
their UTF-8 encoding, while `\xHH` contributes exactly one byte. Thus
`b"A\x00\xff"` has three bytes. `\xHH` is rejected in String because it
could violate the String validity invariant. An invalid scalar,
malformed escape, unterminated literal, or raw newline is a lexical
error.

=== Backtick String and byte arrays (raw)
<lexical-backtick-string-and-byte-arrays-raw>
#idx("raw string")
A #raw("`")-delimited literal is a raw String; a leading `b` makes it a
raw byte array. No escapes are processed and newlines are literal:

```ritchie
path: `C:\Users\ritchie`    // backslashes are literal
sql:  `SELECT *
       FROM accounts`       // spans lines
Byte[] header = b`RI\x00`   // four source characters after RI
```

Raw String contains its source text. A raw byte array contains the UTF-8
bytes of its source contents; `\x00` in the example is four bytes, not an
escape. Only a closing backtick ends either form. An unterminated form
is a lexical error.

=== F-string (interpolated)
<lexical-f-string-interpolated>
#idx("f-string")
#idx("string interpolation")
An `f` prefix on either String quote style produces an interpolated
String. A `{ ... }` slot holds an expression evaluated and inserted at
that position:

```ritchie
f"Hello {name}, you have {count(inbox)} messages"
f`path is {root}/{leaf}`
```

- `f"..."` processes the String escapes from 8.3. #raw("f`...`") is raw,
  like a backtick String, except for its interpolation slots.
- `{{` and `}}` contribute literal braces outside a slot.
- Interpolation slots nest braces: the slot ends at the brace that
  balances its opener, so `{ point{x: 1} }` is one slot.
- Slot text is taken verbatim between its braces and no escape is
  processed inside it. A double quote inside a slot ends a quoted
  f-string, so a slot that needs a quoted String literal uses the raw
  form, where only a backtick ends the literal.
- Each slot is a restricted expression (no top-level assignment, no
  `handle` suffix); see `interpolation-expr` in
  #link(<chap-grammar>)[the grammar].

An unterminated f-string, or an unterminated `{ ... }` slot, is a
lexical error. Byte arrays have no formatted-literal form.

=== Boolean
<lexical-boolean>
`true` and `false` are keywords (#secref(<lexical-keywords>, cap: true)) and the only boolean
literals.

=== Duration
<lexical-duration>
A Duration literal is one or more directly adjacent `integer unit` segments.
Its units are:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Unit], [Meaning],),
    table.hline(),
    [`ns`], [nanoseconds],
    [`us`], [microseconds],
    [`ms`], [milliseconds],
    [`s`], [seconds],
    [`m`], [minutes],
    [`h`], [hours],
    [`d`], [days (exactly 86 400 s)],
  )]
  , kind: table
  )

```ritchie
5s
100ms
1h30m
2d12h
```

Each segment is a decimal integer (digit separators allowed) followed
immediately by a unit. Units are distinct and descend. A single segment has
no mixed-radix component bound. After the first segment, hours are below 24,
minutes and seconds below 60, and each subsecond component below 1,000. The
longest unit matches first, so `100ms` is 100 milliseconds, not `100m` then
`s`. Complete semantics are in #link(<temporal-literals>)[Temporal Types].

=== Date
<lexical-date>
A Date literal begins with `d`. It then uses a four-digit unsigned year from
`0000` through `9999`, or a signed six-digit expanded year, followed by
two-digit month and day:

```ritchie
d0000-01-01
d2026-05-31
d+010000-01-01
d-000001-12-31
```

Positive expanded years begin at `+010000`; negative years begin at
`-000001`. Signed duplicates of the four-digit nonnegative range and negative
zero are rejected. An attached `d`, eligible year, and first date-separator
hyphen claim a Date candidate. Before that hyphen, forms such as `d2026` remain
ordinary identifiers. Complete domains and validation are in
#link(<temporal-literals>)[Temporal Types].

=== Time
<lexical-time>
A Time literal is `tHH:MM:SS`, all fields two-digit, with optional
fractional seconds, denoting a time of day with no date and no zone:

```ritchie
t17:50:00
t09:00:00.5
t23:59:59.999999999
```

The seconds field is mandatory. An attached `t`, two-digit hour, and first
colon claim a Time candidate; before that colon, forms such as `t17` remain
ordinary identifiers. A fraction has one through nine digits. Complete
domains and validation are in #link(<temporal-literals>)[Temporal Types].

=== DateTime
<lexical-datetime>
A DateTime literal joins an unprefixed four-digit date and time text with `T`,
followed by a zone -- `Z` for UTC or a numeric offset `+HH:MM` / `-HH:MM`:

```ritchie
2026-05-31T17:50:00Z
2026-05-31T17:50:00.676767676+10:00
0000-01-01T00:00:00+14:00
9999-12-31T23:59:59.999999999-14:00
```

The `T` is required. It claims an attached four-digit or signed-six-digit
date-like candidate; a signed-six-digit year is then rejected because expanded
years belong only to Date. The offset is required and is either fixed numeric
text or `Z`; a named zone is not a DateTime literal component. Fractional
seconds follow the Time rule. Complete domains and validation are in
#link(<temporal-literals>)[Temporal Types].

#horizontalrule

#set heading(numbering: section-numbering)
== Operators and delimiters
<lexical-operators-and-delimiters>
=== Operators
#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Group], [Tokens],),
    table.hline(),
    [arithmetic], [`+` `-` `*` `/` `%`],
    [comparison], [`==` `!=` `<` `>` `<=` `>=`],
    [coalescing], [`??`],
    [assignment], [`=` `+=` `-=` `*=` `/=` `%=` `??=`],
    [ternary], [`?` `:`],
  )]
  , kind: table
  )

Boolean operators are the keywords `and`, `or`, `not`, not symbols.

=== Delimiters and punctuation
#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Token], [Use],),
    table.hline(),
    [`(` `)`], [call, grouping, parameter and closure lists],
    [`{` `}`], [blocks, struct/entity/error/enum bodies, handle arms],
    [`[` `]`], [indexing, slicing, array and dict literals, array
    types],
    [`,`], [list separator],
    [`:`], [named init, enum value, dict entry, dict-type key, ternary
    else, slice, loop label],
    [`.`], [member selection from a value or type],
    [`::`], [module qualification],
    [`=>`], [closure body, match arm, handle arm],
    [`->`], [function return type (follows the parameter list)],
    [`:=`], [distinct-type declaration: `Email := String` mints a nominal
    type over a representation (#link(<chap-types>)[Types])],
    [`#`], [entity id-type former: `$Order#` is the id type of `$Order`
    (#link(<chap-types>)[Types])],
    [`|`], [separates the error types in a method's return contract
    (#link(<chap-errors>)[Errors])],
    [`~>`], [reserved for future use (#secref(<lexical-reserved-for-future-use>, cap: true))],
    [`;`], [optional statement separator],
  )]
  , kind: table
  )

Multi-character operators and punctuation are matched greedily: `<=`, `->`,
`=>`, `:=`, `::`, `~>`, `??`, and `??=` are each one token. `??=` is the
three-character
coalescing assignment, not `??` then `=`; `x??y` is one `??`, not `?`
then `?`. `:` plays several pairing roles, the exact role
being resolved by the grammar from surrounding context, not by the
lexer.

In type position `<` and `>` also delimit a type-argument list --
`Pair<Int, String>` -- resolved by parse position from the comparison
operators, the same split that lets `-` serve as both negation and
subtraction. There is no `<<` or `>>` token, so a nested application
(`Stack<Pair<Int, String>>`) closes with two ordinary `>` tokens.

#horizontalrule

== Excluded forms
<lexical-excluded-forms>
Ritchie deliberately omits some operators common to C-family languages.
These are recognised and rejected; the table gives the form to use
instead.

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Written], [Rejected because], [Use instead],),
    table.hline(),
    [`&` `&&`], [no symbolic boolean AND], [`and`],
    [`||`], [no symbolic boolean OR], [`or`],
    [`!expr`], [`!` is not logical negation], [`not expr`],
    [`++` `--`], [no increment/decrement operators], [`x += 1`,
    `x -= 1`],
  )]
  , kind: table
  )

A single `|` separates the error types in a method's return contract
(#link(<chap-errors>)[Errors]); `||` is rejected -- use `or`.

#horizontalrule

== Reserved for future use
<lexical-reserved-for-future-use>
These symbols are recognised by the language but carry no meaning yet.
They are reserved so a later feature can adopt them without
accidentally breaking user code.

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Reserved], [Likely role (hint)],),
    table.hline(),
      [`~` (prefix)], [a unary operator in the complement / negate /
    approximate family.],
      [`~>`], [directed flow: a pipeline stage, an effect or boundary
          crossing, or a state transition. Distinct from `->` (function
          return) and `=>` (maps-to).],
      [`<~`], [inbound counterpart to `~>`.],
      [`<-`], [receive, assign-from],
      [`|>`], [pipe-forward: feed a value as the input of the next call.],
      [`..` `...`], [ranges (`lo..hi`) and spread / rest.],
  )]
  , kind: table
  )

Exact tokenisation of the reserved multi-character and prefix forms is
fixed when each feature lands. In particular, `<-` must be disambiguated
from `<` followed by a unary `-`, by parse position (prefix/operand vs
infix) -- the same split that already lets `-` serve as both negation
and subtraction, and that now separates the `%` projection sigil from
binary `%` (#secref(<lexical-identifiers-and-archetypes>, cap: true)).
