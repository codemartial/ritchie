#import "../prelude.typ": horizontalrule, idx, secref

= Temporal Types
<chap-temporal>
#idx("Date", code: true)
#idx("Time", code: true)
#idx("DateTime", code: true)
#idx("Duration", code: true)
#idx("temporal type")
`Date`, `Time`, `DateTime`, and `Duration` are distinct primitive value
types. `Date` and `Time` are civil coordinates, `DateTime` identifies an
instant while retaining a fixed numeric offset, and `Duration` is a signed
physical displacement. There is no implicit conversion among them.

Date orders by its Gregorian coordinate, Time by nanoseconds since midnight,
DateTime by its represented instant, and Duration by its signed physical
value. A DateTime's retained offset does not participate in ordering.

#horizontalrule

== Value domains
<temporal-domains>

=== Date
A `Date` is a year, month, and day in the proleptic Gregorian calendar. It
has no time, offset, or zone. Astronomical year numbering applies: year
`0000` is 1 BCE and year `-000001` is 2 BCE. The year domain is
-999,999 through +999,999.

A year is a leap year when it is divisible by 4, except that a year divisible
by 100 is not a leap year unless it is also divisible by 400. Month and day
must name an existing civil date; invalid fields do not normalize.

=== Time
A `Time` is an exact integer nanosecond count since the start of a civil day.
It has no date, offset, or zone. Its domain is 0 through
86,399,999,999,999 nanoseconds, from `t00:00:00` through
`t23:59:59.999999999`. Each modeled civil day contains exactly 86,400
seconds. Leap seconds and clock-smear behavior are outside this type.

=== DateTime
A `DateTime` identifies an instant by signed Unix epoch seconds and a
nanosecond fraction, and retains one numeric offset. Its local civil year is
restricted to `0000` through `9999`. The offset is local time minus UTC, has
whole-minute resolution, and ranges from `-14:00` through `+14:00`. At either
14-hour endpoint its minute field is zero.

A `DateTime` contains no named time zone or daylight-saving rule. Every valid
four-digit local date, `Time`, and admitted offset combination is in the
domain, including a local year endpoint whose UTC coordinate crosses into an
adjacent expanded year. A `Date` outside the four-digit year range cannot be
represented as a `DateTime`. Instant ordering uses the represented Unix
coordinate and is independent of the retained offset.

=== Duration
A `Duration` is a signed physical value independent of calendars and epochs.
One day is exactly 86,400 seconds. Every `Duration` is an eight-byte,
pointer-free value in one of two resolution tiers:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Tier], [Resolution], [Magnitude],),
    table.hline(),
    [fine], [1 nanosecond],
      [`0` through `4_611_686_018_426_999_999` ns],
    [coarse], [1 millisecond],
      [`4_611_686_018_427` through `4_611_686_018_427_387_903` ms],
  )]
  , kind: table
)

The signed domain is symmetric. There is one zero. Every magnitude in the
fine domain uses the fine tier; every larger admitted magnitude uses the
coarse tier. Conversion to the coarse tier truncates toward zero to a whole
millisecond. The tier boundary is therefore an exact millisecond. One scale
bit selects the tier and the other 63 bits carry its signed payload; the
extra negative endpoint of a two's-complement payload is outside the domain.

=== Comparison
Equality and ordering (#secref(<expressions-operators>))
use the identities stated at the head of this chapter. A Duration's tier
adds no distinction, so `1h == 60m`. Two DateTime values that identify one
instant are equal whatever their offsets; as a dictionary key, the first
equal DateTime retains its offset while a later write replaces the
associated value.

#horizontalrule

== Literals
<temporal-literals>
Temporal literals are constant values.

=== Date literals
A Date literal begins with `d` and has one of these year forms followed by
`-MM-DD`:

- Years `0000` through `9999` use exactly four unsigned digits.
- Positive years from 10,000 use `+` and exactly six digits.
- Negative years use `-` and exactly six digits.

Month and day use exactly two digits. A signed duplicate of the four-digit
nonnegative domain, negative zero, a year outside the Date domain, or an
invalid month or day is rejected.

```ritchie
d0000-01-01
d+010000-01-01
d-000001-12-31
d+999999-12-31
```

=== Time literals
A Time literal is `tHH:MM:SS` with an optional fractional second. Each main
field has exactly two digits. Hours range from `00` through `23`; minutes and
seconds range from `00` through `59`. `t24:00:00` and second `60` are rejected.

A fraction has one through nine digits. It is right-padded with zeroes to
nanosecond resolution. A source fraction with more than nine digits is
rejected, including when every excess digit is zero.

```ritchie
t00:00:00
t09:05:02.1
t23:59:59.999999999
```

=== DateTime literals
A DateTime literal joins an unprefixed four-digit date and time text with `T`,
followed by `Z` or a numeric offset `+HH:MM` or `-HH:MM`. Expanded years are
not admitted. Its fractional second follows the Time field and precision
rules. Offset minutes range from `00` through `59`; offset hours range from
`00` through `13`, with `14:00` as the only 14-hour form.
`Z` and `+00:00` create the same UTC value; UTC displays as `Z`. The form
`-00:00` is rejected. A nonzero admitted offset is retained.

```ritchie
2026-09-07T09:05:02Z
2026-09-07T19:05:02+10:00
0000-01-01T00:00:00+14:00
9999-12-31T23:59:59.999999999-14:00
```

=== Duration literals
A Duration literal contains one or more unsigned integer-and-unit segments.
The units, in descending order, are `d`, `h`, `m`, `s`, `ms`, `us`, and
`ns`. Segments are directly adjacent, units are distinct, and their order is
strictly descending. Each integer is decimal and admits the integer digit
separators from #link(<chap-lexical>)[Lexical]. A leading minus is unary
negation of the complete literal.

A one-segment literal has no mixed-radix component bound. In a compound
literal, every segment after the first is below its conventional bound:
hours below 24; minutes and seconds below 60; and milliseconds,
microseconds, and nanoseconds below 1,000. An omitted intermediate unit counts
as zero and does not enlarge a lower component's bound.

```ritchie
120m       // admitted single quantity
25h        // admitted single quantity
1d2h9m     // admitted compound value
1h60m      // rejected subordinate minute
1d129m     // rejected subordinate minute
1s1000ms   // rejected subordinate millisecond
```

The complete admitted magnitude is computed before the tier rule in
#secref(<temporal-domains>) is applied. A magnitude outside the
Duration domain is rejected.

=== Recognition and rejection
A temporal literal is one token: whitespace or a comment ends it, so
`d2026-05-31` is a Date, while `2026-05-31` is subtraction. A name such as
`d2026` or `t17` is an identifier, also where a colon follows it in a
by-name argument or a label, and a Time literal followed by `.` and a name
is the receiver of a method call. A DateTime literal with a signed
six-digit year is rejected.

A malformed temporal literal, with an invalid width, field, precision,
offset, unit order, subordinate bound, suffix, or range, produces one
compile diagnostic for the whole literal rather than one for each of its
parts. Exact diagnostic text is not specified, and rejected source produces
no artifact.

Bracket grammar retains its delimiters. For example, `items[10:20:30]` is
rejected as bracket or slice syntax; it is not a Time used as an index. The
prefixed form `items[t10:20:30]` is a Time used as an index expression and is
checked by the ordinary index rules. A Time may appear as an ordinary array
element.

#horizontalrule

== Canonical display
<temporal-display>
Temporal default display is ASCII and locale-independent. It does not depend
on the host zone. Each result is valid Ritchie source, is stable under
repeated formatting, and recreates the same value and Duration tier.

- Date begins with `d`, then uses its admitted four-digit or signed six-digit
  year form and two-digit month and day.
- Time begins with `t` and uses two-digit fields. It omits a zero fraction and otherwise removes
  trailing fractional zeroes.
- DateTime uses a four-digit year and joins its canonical local date and time
  fields without `d` or `t`, with `T` between them, followed by `Z` for UTC or
  its retained nonzero `+HH:MM` or `-HH:MM` offset.
- Duration zero is `0s`. Every other Duration uses one leading minus when
  negative and the largest applicable descending sequence of `d`, `h`, `m`,
  `s`, `ms`, `us`, and `ns`, omitting zero components. A coarse Duration has
  no microsecond or nanosecond component.

```ritchie
120m           // displays as 2h
1000000001ns   // displays as 1s1ns
```

A canonical display is not an alternative source rule. For example,
`1d + 129m` evaluates to a value displayed as `1d2h9m`, but the attached
compound literal `1d129m` is rejected.

#horizontalrule

== Date operations
<temporal-date-operations>
Date supplies these civil methods and has no primitive arithmetic operator:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Method], [Result], [Possible own failure],),
    table.hline(),
    [`addDays(Int)`], [`Date`], [`OutOfRange!`],
    [`addWeeks(Int)`], [`Date`], [`OutOfRange!`],
    [`addMonths(Int)`], [`Date`], [`OutOfRange!`],
    [`addYears(Int)`], [`Date`], [`OutOfRange!`],
    [`daysUntil(Date)`], [`Int`], [none],
  )]
  , kind: table
)

A negative addition argument moves backward; zero returns the receiver.
`addDays` and `addWeeks` use exact Gregorian day displacement.
`daysUntil` returns the signed Gregorian day count from the receiver to its
argument, positive when the argument is later.

`addMonths` computes the target proleptic year and month directly across year
zero, then limits the receiver's day to the last valid day of that target
month. `addYears` changes the astronomical year and applies the same day
rule. Each call applies this rule independently:

```ritchie
d2025-01-31.addMonths(2)              // d2025-03-31
d2025-01-31.addMonths(1).addMonths(1) // d2025-03-28
```

An addition whose target year lies outside the Date domain raises
`OutOfRange!`.

#horizontalrule

== Time operations
<temporal-time-operations>
Time admits the following operators and methods:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Form], [Result],),
    table.hline(),
    [`Time + Duration`, `Time - Duration`], [`Time`],
    [`Time - Time`], [`Duration`],
    [`until(Time)`, `since(Time)`], [`Duration`],
  )]
  , kind: table
)

Addition and subtraction by Duration are modulo exactly one modeled civil
day. Whole days and all other complete multiples of one day have no effect.
The operation retains nanosecond precision and is infallible, including for a
coarse or endpoint Duration.

`a - b` is the exact signed difference between the two same-day nanosecond
coordinates. Its result is strictly between `-1d` and `1d` and uses the fine
tier. `a.until(b)` is the forward cyclic distance from `a` to `b`;
`a.since(b)` is the forward cyclic distance from `b` to `a`. These method
results are in `[0d, 1d)` and use the fine tier.

All three difference forms are infallible and exact. For unequal values,
`a.until(b) == b.since(a)`, and opposing directional distances sum to `1d`.
Equal values produce `0s`.

```ritchie
t23:30:00 + 2h             // t01:30:00
t00:30:00 - 1h             // t23:30:00
t01:00:00 - t23:00:00      // -22h
t23:00:00.until(t01:00:00) // 2h
```

#horizontalrule

== DateTime operations
<temporal-datetime-operations>
`DateTime + Duration` and `DateTime - Duration` displace the represented
numeric instant. The result retains the DateTime operand's numeric offset and
converts the resulting instant to local fields under that offset. If those
fields have a local year outside `0000` through `9999`, the operation raises
`OutOfRange!`. It does not change the offset to obtain an in-range result or
consult zone rules.

`DateTime - DateTime` returns the elapsed Duration between the represented
instant coordinates. It ignores both retained offsets, is infallible, and may
lose sub-millisecond information only when the result magnitude requires the
coarse tier.

=== Offset editing
`withOffset(Duration offset)` returns the receiver's instant retaining
`offset` in place of the receiver's offset. An offset outside the domain of
#secref(<temporal-domains>), or a local year outside it under the new offset, raises
`OutOfRange!`. `offset()` returns the retained offset as a Duration, so
`v.withOffset(d).offset() == d` for every admitted `d`.

```ritchie
2026-09-23T01:00:00Z.withOffset(10h)      // 2026-09-23T11:00:00+10:00
9999-12-31T23:00:00Z.withOffset(2h)       // OutOfRange!
```

=== Unix conversion
The Unix origin is `1970-01-01T00:00:00Z`.

```ritchie
DateTime.fromUnix(Int seconds, Int nanoseconds = 0) -> DateTime
datetime.unix() -> (Int, Int)
```

`fromUnix` constructs the instant `origin + seconds × 1s + nanoseconds × 1ns`
with offset zero. `nanoseconds` must lie in `0` through `999_999_999` and the
local year must lie in the domain of #secref(<temporal-domains>)\; otherwise `OutOfRange!` is
raised. `unix()` returns the receiver's instant as the floor count of whole
seconds since the origin and the nanoseconds above it, in `0` through
`999_999_999`.

```ritchie
DateTime.fromUnix(-1, 999_999_999)  // 1969-12-31T23:59:59.999999999Z

Int seconds, nanoseconds = 1969-12-31T23:59:59.999999999Z.unix()  // -1, 999_999_999
```

#horizontalrule

== Duration operations
<temporal-duration-operations>
Duration admits exactly these primitive operators:

#figure(
  align(center)[#table(
    columns: 3,
    align: (auto,auto,auto,),
    table.header([Form], [Result], [Possible own failure],),
    table.hline(),
    [`Duration + Duration`, `Duration - Duration`], [`Duration`],
      [`OutOfRange!`],
    [`Duration * Int`, `Int * Duration`], [`Duration`],
      [`OutOfRange!`],
    [`Duration / Int`], [`Duration`], [`DivisionByZero!`],
    [`Duration / Duration`], [`Float`], [`DivisionByZero!`],
    [`-Duration`], [`Duration`], [none],
  )]
  , kind: table
)

No Date arithmetic operator, reversed Duration addition to Time or DateTime,
Time addition to Time, Float scaling, or Duration remainder exists.

Every Duration-producing operation computes its mathematical result before
selecting a tier. First truncate toward zero to a whole nanosecond and use the
fine tier when that integer fits. Otherwise truncate the exact result toward
zero to a whole millisecond and use the coarse tier when that integer fits.
A result that fits neither tier raises `OutOfRange!`. A
smaller-than-nanosecond result is the single zero. A coarse operand may
therefore produce a fine result.

For integer division, this rule is applied to the exact rational quotient:

```ritchie
 1ns / 2 // 0s
-1ns / 2 // 0s
 1ms / 3 // 333333ns
-1ms / 3 // -333333ns
```

`Duration / Duration` returns a Float ratio and does not select a Duration
tier. Magnitude reduction, including the loss or absorption of a
sub-millisecond contribution at a coarse magnitude, is a value rather than a
failure. It can make Duration arithmetic non-associative.

`highRes()` returns `true` exactly for the fine tier. `lowRes()` returns
`true` exactly for the coarse tier. Both methods are infallible and
constant-capable, and exactly one returns `true` for each value. The tier
reports current numeric resolution, not source-clock accuracy or measurement
provenance.

#horizontalrule

== Failure and evaluation
<temporal-failures>
Temporal operands evaluate once from left to right before their operation.
`OutOfRange!` is raised when a Date addition, DateTime displacement, Duration
sum, Duration difference, or Duration product cannot be represented by its
result type, and when a DateTime offset edit or Unix construction cannot be
represented as stated in #secref(<temporal-datetime-operations>). Division by a zero Int or zero Duration
raises `DivisionByZero!`. Time operations, DateTime difference, DateTime
`unix()`, Date `daysUntil`, Duration negation, and resolution inspection add
no failure of their own.

Every temporal operator, method, and type-level constructor in this chapter
is constant-capable: when its receiver and operands are constant
expressions, the operation is a constant expression with the same value or
failure as ordinary evaluation. A
known failure is a compile diagnostic and produces no artifact. A nonconstant
operation contributes the fixed error set stated in this chapter and uses
ordinary typed recovery
(#link(<chap-errors>)[Error Handling]). Quantization and absorption are
successful results.

#horizontalrule

== Compound assignment
<temporal-compound-assignment>
The admitted temporal compound assignments are:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Target], [Forms],),
    table.hline(),
    [`Time`], [`+= Duration`, `-= Duration`],
    [`DateTime`], [`+= Duration`, `-= Duration`],
    [`Duration`], [`+= Duration`, `-= Duration`, `*= Int`, `/= Int`],
  )]
  , kind: table
)

Other temporal compound forms are rejected because the corresponding
operator is absent or its result cannot be stored in the target type.

The ordinary compound-assignment contract applies
(#secref(<expressions-assignment>)). The target and its path
components are established once, then the right side evaluates. After any
required final target validation, the live target value is read, the
operation is performed, and its result is stored only after successful
completion. A right side that changes the target therefore affects the value
used by the operation. A failed operation leaves the target unchanged unless
ordinary recovery supplies a successful operation result.

#horizontalrule

== Serialization boundary
<temporal-serialization>
The language-level serialized information is:

#figure(
  align(center)[#table(
    columns: 2,
    align: (auto,auto,),
    table.header([Type], [Information],),
    table.hline(),
    [`Date`], [civil year, month, and day],
    [`Time`], [nanoseconds since midnight],
    [`DateTime`], [Unix seconds, nanoseconds, and retained offset minutes],
    [`Duration`], [resolution scale and signed payload],
  )]
  , kind: table
)
