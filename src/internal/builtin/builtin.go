// Package builtin is the one table of built-ins: each row holds a built-in's name and
// form, its signature with element and key types as letters, its fault set, and the rlr
// function it maps to, each type row states the capabilities of its type, and the table
// derives from them whether constant evaluation admits a mapping.
package builtin

import (
	"reflect"
	"slices"
	"strings"

	"ritchie.dev/internal/syntax"
	"ritchie.dev/rlr"
)

// Form is how a built-in is written at its use: as a type, as an error type of the catalog, as a free call, as an operator, index, or slice, as a method called on its type, as a method called on a value, or as a read-only member of a value.
type Form uint8

const (
	TypeRow Form = iota
	ErrorRow
	Function
	Operator
	TypeMethod
	Method
	Field
)

// Tier is the catalog tier of an error row, where the runtime tier is the group of errors that every remote call can raise and no catalog error of any tier is listed in a declared error set.
type Tier uint8

const (
	NoTier Tier = iota
	Runtime
)

// Extra is a parameter of a row's rlr function that the row's signature lacks: the formatting module, which comes first, or a key projection, copy, equality, or order function, or the invocation of the calling routine, which follow the arguments.
type Extra uint8

const (
	Formatting Extra = iota
	Clone
	Equal
	Less
	Projection
	Caller
)

// Variant is an implementation of a row that the classes of its element type select: the base implementation, which serves every type and which constant evaluation calls, the block copy for an element type of the flat copy class, or the unstable sort for an element type of an indistinguishable order.
type Variant uint8

const (
	Base Variant = iota
	BlockCopy
	UnstableSort
)

// Row is one built-in, whose Capabilities a type row states, whose Eligible marks an error row of the eligible error set, which holds the errors of operations on values that the evaluation of a constant expression reports as compile errors, whose Effect marks a row that writes output or waits on the clock and is therefore an effect, before which a routine waits for every read in flight, whose Futures marks a row that reads no element value of its collection, because it stores an argument as an element or its result depends on no element value, so that the collection can hold futures as its elements, whose Standalone marks a direct impurity root, whose result depends on when its call runs and whose call the generator therefore writes as a Go statement of its own, so that the call runs at its place in the order of evaluation, whose Iterator marks a row that is the source of an iterator loop and yields the values of its result type to the loop alone, whose Unordered marks a row whose result holds the entries of a dictionary in an order that the language leaves unspecified, whose Keyed, FlatKeyed, and KeyedExtra serve a dictionary with projected keys, KeyedExtra defaulting to Extra, whose Block is the function of its block copy, which takes no copy function, whose Unstable is the function of its unstable sort, whose InPlace is the function that works in the storage of its first argument and takes the extra parameters of InPlaceExtra, which the generator calls in place of the function of the variant when the call consumes that argument, whose InPlaceKeyed and InPlaceUnstable are that function over a dictionary with projected keys and under the unstable sort, whose Keeps marks a row whose in-place function leaves every element of its first argument as it was, so that the argument still holds its value when a later operation of the statement fails, whose Go, FlatGo, KeyedGo, FlatKeyedGo, BlockGo, UnstableGo, InPlaceGo, InPlaceKeyedGo, and InPlaceUnstableGo name its functions in package rlr, whose Names are the parameter names its signature gives, whose Inputs list the receiver of a method before the parameters, and whose Constant marks a mapping that constant evaluation admits, all derived once by the table.
type Row struct {
	Form            Form
	Name            string
	Signature       string
	Faults          []string
	Impl            any
	Flat            any
	Extra           []Extra
	Keyed           any
	FlatKeyed       any
	KeyedExtra      []Extra
	Block           any
	Unstable        any
	InPlace         any
	InPlaceExtra    []Extra
	InPlaceKeyed    any
	InPlaceUnstable any
	Keeps           bool
	Ref             bool
	Standalone      bool
	Effect          bool
	Futures         bool
	Iterator        bool
	Unordered       bool
	Eligible        bool
	Tier            Tier
	Capabilities    Capabilities

	Go                string
	FlatGo            string
	KeyedGo           string
	FlatKeyedGo       string
	BlockGo           string
	UnstableGo        string
	InPlaceGo         string
	InPlaceKeyedGo    string
	InPlaceUnstableGo string
	Owner             string
	Member            string
	Params            []syntax.Type
	Names             []string
	Inputs            []syntax.Type
	Variadic          bool
	Results           []syntax.Type
	ResultNone        bool
	Constant          bool
}

var Rows = []*Row{
	{Form: TypeRow, Name: "Int", Capabilities: ordinal},
	{Form: TypeRow, Name: "Int8", Capabilities: ordinal},
	{Form: TypeRow, Name: "Int16", Capabilities: ordinal},
	{Form: TypeRow, Name: "Int32", Capabilities: ordinal},
	{Form: TypeRow, Name: "Int64", Capabilities: ordinal},
	{Form: TypeRow, Name: "Float", Capabilities: Capabilities{Flat, Native, Distinguishable, true}},
	{Form: TypeRow, Name: "Bool", Capabilities: nominal},
	{Form: TypeRow, Name: "Byte", Capabilities: ordinal},
	{Form: TypeRow, Name: "BitSet", Capabilities: nominal},
	{Form: TypeRow, Name: "String", Capabilities: ordinal},
	{Form: TypeRow, Name: "Decimal", Capabilities: Capabilities{Flat, Custom, Distinguishable, true}},
	{Form: TypeRow, Name: "Date", Capabilities: ordinal},
	{Form: TypeRow, Name: "Time", Capabilities: ordinal},
	{Form: TypeRow, Name: "DateTime", Capabilities: Capabilities{Flat, Custom, Distinguishable, true}},
	{Form: TypeRow, Name: "Duration", Capabilities: ordinal},
	{Form: TypeRow, Name: "UUId", Capabilities: opaque},
	{Form: TypeRow, Name: "Error!", Capabilities: record},
	{Form: TypeRow, Name: "ErrorStep", Capabilities: record},
	{Form: TypeRow, Name: "ErrorType", Capabilities: record},
	{Form: TypeRow, Name: "ErrorField", Capabilities: record},
	{Form: TypeRow, Name: "ErrorLocation", Capabilities: record},
	{Form: TypeRow, Name: "r::Clock", Capabilities: opaque},
	{Form: TypeRow, Name: "r::Random", Capabilities: opaque},

	{Form: ErrorRow, Name: "Unavailable!", Tier: Runtime},
	{Form: ErrorRow, Name: "Rejected!", Tier: Runtime},
	{Form: ErrorRow, Name: "Unverified!", Tier: Runtime},
	{Form: ErrorRow, Name: "Conflict!", Tier: Runtime},
	{Form: ErrorRow, Name: "Unmodified!", Tier: Runtime},
	{Form: ErrorRow, Name: "Cancelled!", Tier: Runtime},
	{Form: ErrorRow, Name: "NotFound!"},
	{Form: ErrorRow, Name: "Duplicate!"},
	{Form: ErrorRow, Name: "Frozen!"},
	{Form: ErrorRow, Name: "Malformed!", Eligible: true},
	{Form: ErrorRow, Name: "LengthMismatch!", Eligible: true},
	{Form: ErrorRow, Name: "MissingKey!", Eligible: true},
	{Form: ErrorRow, Name: "OutOfRange!", Eligible: true},
	{Form: ErrorRow, Name: "InvalidStringBoundary!", Eligible: true},
	{Form: ErrorRow, Name: "DivisionByZero!", Eligible: true},

	{Form: Field, Name: "Error!.history", Signature: "Error!.history -> ErrorStep[]"},
	{Form: Field, Name: "ErrorStep.typ", Signature: "ErrorStep.typ -> ErrorType"},
	{Form: Field, Name: "ErrorStep.location", Signature: "ErrorStep.location -> ErrorLocation"},
	{Form: Field, Name: "ErrorStep.fieldVals", Signature: "ErrorStep.fieldVals -> String[]"},
	{Form: Field, Name: "ErrorType.module", Signature: "ErrorType.module -> String"},
	{Form: Field, Name: "ErrorType.name", Signature: "ErrorType.name -> String"},
	{Form: Field, Name: "ErrorType.fields", Signature: "ErrorType.fields -> ErrorField[]"},
	{Form: Field, Name: "ErrorField.name", Signature: "ErrorField.name -> String"},
	{Form: Field, Name: "ErrorField.typeName", Signature: "ErrorField.typeName -> String"},
	{Form: Field, Name: "ErrorLocation.revision", Signature: "ErrorLocation.revision -> String"},
	{Form: Field, Name: "ErrorLocation.module", Signature: "ErrorLocation.module -> String"},
	{Form: Field, Name: "ErrorLocation.typeName", Signature: "ErrorLocation.typeName -> String?"},
	{Form: Field, Name: "ErrorLocation.function", Signature: "ErrorLocation.function -> String"},

	{Form: Function, Name: "print", Signature: "print(T)", Impl: rlr.Print, Extra: formatting, Effect: true},
	{Form: Function, Name: "log", Signature: "log(T)", Impl: rlr.Log, Extra: formatting},
	{Form: Function, Name: "r::report", Signature: "r::report(Error!)", Impl: rlr.ReportUnrecovered, Extra: formatting},
	{Form: Function, Name: "format", Signature: "format(T...) -> String", Faults: outOfRange, Impl: rlr.Format, Extra: formatting},

	{Form: Function, Name: "range", Signature: "range(Int8) -> Int8", Impl: rlr.Upto[int8], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int16) -> Int16", Impl: rlr.Upto[int16], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int32) -> Int32", Impl: rlr.Upto[int32], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int) -> Int", Impl: rlr.Upto[int64], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int8, Int8) -> Int8", Impl: rlr.Between[int8], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int16, Int16) -> Int16", Impl: rlr.Between[int16], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int32, Int32) -> Int32", Impl: rlr.Between[int32], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int, Int) -> Int", Impl: rlr.Between[int64], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int8, Int8, Int8) -> Int8", Impl: rlr.Progression[int8], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int16, Int16, Int16) -> Int16", Impl: rlr.Progression[int16], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int32, Int32, Int32) -> Int32", Impl: rlr.Progression[int32], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Int, Int, Int) -> Int", Impl: rlr.Progression[int64], Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Decimal, Decimal) -> Decimal", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Decimal, Decimal, Decimal) -> Decimal", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Date, Date) -> Date", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Date, Date, Duration) -> Date", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Time, Time) -> Time", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Time, Time, Duration) -> Time", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(DateTime, DateTime) -> DateTime", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(DateTime, DateTime, Duration) -> DateTime", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Duration, Duration) -> Duration", Iterator: true},
	{Form: Function, Name: "range", Signature: "range(Duration, Duration, Duration) -> Duration", Iterator: true},

	{Form: Function, Name: "count", Signature: "count(String) -> Int", Impl: rlr.Count[string]},
	{Form: Function, Name: "count", Signature: "count(Byte[]) -> Int", Impl: rlr.Count[[]byte]},
	{Form: Function, Name: "count", Signature: "count(T[N]) -> Int", Impl: rlr.Length[any], Futures: true},
	{Form: Function, Name: "count", Signature: "count([K: V]) -> Int", Impl: rlr.DictLength[int64, any], Keyed: rlr.DictLength[int64, any], Futures: true},
	{Form: Function, Name: "contains", Signature: "contains(String, String) -> Bool", Impl: rlr.ContainsString},
	{Form: Function, Name: "contains", Signature: "contains(Byte[], Byte[]) -> Bool", Impl: rlr.ContainsBytes},
	{Form: Function, Name: "contains", Signature: "contains(T[N], T) -> Bool", Impl: rlr.Contains[any], Extra: equal},
	{Form: Function, Name: "indexOf", Signature: "indexOf(String, String) -> (Int, Bool)", Impl: rlr.IndexOfString},
	{Form: Function, Name: "indexOf", Signature: "indexOf(Byte[], Byte[]) -> (Int, Bool)", Impl: rlr.IndexOfBytes},
	{Form: Function, Name: "indexOf", Signature: "indexOf(T[N], T) -> Int?", Impl: rlr.IndexOf[any], Extra: equal},
	{Form: Function, Name: "first", Signature: "first(T[N]) -> T?", Impl: rlr.First[any], Flat: rlr.FirstFlat[any], Extra: clone},
	{Form: Function, Name: "last", Signature: "last(T[N]) -> T?", Impl: rlr.Last[any], Flat: rlr.LastFlat[any], Extra: clone},
	{Form: Function, Name: "append", Signature: "append(T[], T) -> T[]", Faults: outOfRange, Impl: rlr.Append[any], Block: rlr.AppendBlock[any], InPlace: rlr.AppendInPlace[any], InPlaceExtra: clone, Keeps: true, Extra: clone, Futures: true},
	{Form: Function, Name: "insert", Signature: "insert(T[], Int, T) -> T[]", Faults: outOfRange, Impl: rlr.Insert[any], Block: rlr.InsertBlock[any], InPlace: rlr.InsertInPlace[any], InPlaceExtra: clone, Extra: clone},
	{Form: Function, Name: "removeAt", Signature: "removeAt(T[], Int) -> T[]", Faults: outOfRange, Impl: rlr.RemoveAt[any], Block: rlr.RemoveAtBlock[any], InPlace: rlr.RemoveAtInPlace[any], Extra: clone},
	{Form: Function, Name: "concat", Signature: "concat(T[], T[]) -> T[]", Faults: outOfRange, Impl: rlr.Concat[any], Block: rlr.ConcatBlock[any], InPlace: rlr.ConcatInPlace[any], InPlaceExtra: clone, Keeps: true, Extra: clone},
	{Form: Function, Name: "reverse", Signature: "reverse(T[N]) -> T[N]", Impl: rlr.Reverse[any], InPlace: rlr.ReverseInPlace[any], Extra: clone},
	{Form: Function, Name: "sort", Signature: "sort(T[N]) -> T[N]", Impl: rlr.Sorted[any], Unstable: rlr.SortedUnstable[any], InPlace: rlr.SortInPlace[any], InPlaceUnstable: rlr.SortUnstableInPlace[any], InPlaceExtra: []Extra{Less}, Extra: []Extra{Less, Clone}},
	{Form: Function, Name: "fill", Signature: "fill(T, Int) -> T[N]", Faults: outOfRange, Impl: rlr.Fill[any], Extra: clone},

	{Form: Function, Name: "has", Signature: "has([K: V] d, K k) -> Bool", Impl: rlr.Has[int64, any], Keyed: rlr.Has[int64, any]},
	{Form: Method, Name: "[K: V].get", Signature: "[K: V].get(K k) -> V?", Impl: rlr.Get[int64, any], Flat: rlr.GetFlat[int64, any], Extra: clone, Keyed: rlr.GetKeyed[int64, any, any], FlatKeyed: rlr.GetFlatKeyed[int64, any, any]},
	{Form: Function, Name: "keys", Signature: "keys([K: V] d) -> K[]", Impl: rlr.Keys[int64, any], Keyed: rlr.KeysKeyed[int64, any, any], Unordered: true},
	{Form: Function, Name: "values", Signature: "values([K: V] d) -> V[]", Impl: rlr.Values[int64, any], Extra: clone, Keyed: rlr.ValuesKeyed[int64, any, any], Unordered: true},
	{Form: Function, Name: "remove", Signature: "remove([K: V] d, K k) -> [K: V]", Impl: rlr.Remove[int64, any], Extra: clone, Keyed: rlr.RemoveKeyed[int64, any, any], InPlace: rlr.RemoveInPlace[int64, any], InPlaceKeyed: rlr.RemoveKeyedInPlace[int64, any, any]},
	{Form: Function, Name: "zip", Signature: "zip(K[N] keys, V[M] values) -> [K: V]", Faults: []string{"LengthMismatch!"}, Impl: rlr.Zip[int64, any], Extra: clone, Keyed: rlr.ZipKeyed[int64, any, any], KeyedExtra: []Extra{Projection, Clone}},
	{Form: Function, Name: "unzip", Signature: "unzip([K: V] d) -> (K[], V[])", Impl: rlr.Unzip[int64, any], Extra: clone, Keyed: rlr.UnzipKeyed[int64, any, any], Unordered: true},

	{Form: Function, Name: "isEmpty", Signature: "isEmpty(String) -> Bool", Impl: rlr.IsEmpty[string]},
	{Form: Function, Name: "isEmpty", Signature: "isEmpty(Byte[]) -> Bool", Impl: rlr.IsEmpty[[]byte]},
	{Form: Function, Name: "startsWith", Signature: "startsWith(String, String) -> Bool", Impl: rlr.StartsWithString},
	{Form: Function, Name: "startsWith", Signature: "startsWith(Byte[], Byte[]) -> Bool", Impl: rlr.StartsWithBytes},
	{Form: Function, Name: "endsWith", Signature: "endsWith(String, String) -> Bool", Impl: rlr.EndsWithString},
	{Form: Function, Name: "endsWith", Signature: "endsWith(Byte[], Byte[]) -> Bool", Impl: rlr.EndsWithBytes},
	{Form: Function, Name: "toUpper", Signature: "toUpper(String) -> String", Faults: outOfRange, Impl: rlr.ToUpper},
	{Form: Function, Name: "toLower", Signature: "toLower(String) -> String", Faults: outOfRange, Impl: rlr.ToLower},
	{Form: Function, Name: "trim", Signature: "trim(String) -> String", Impl: rlr.Trim},
	{Form: Function, Name: "replace", Signature: "replace(String, String, String) -> String", Faults: outOfRange, Impl: rlr.Replace},
	{Form: Function, Name: "split", Signature: "split(String, String) -> String[]", Faults: outOfRange, Impl: rlr.Split},
	{Form: Function, Name: "join", Signature: "join(String[N], String) -> String", Faults: outOfRange, Impl: rlr.Join},
	{Form: Method, Name: "String.isRuneBoundary", Signature: "String.isRuneBoundary(Int) -> Bool", Impl: rlr.IsRuneBoundary},

	{Form: Function, Name: "abs", Signature: "abs(Int8) -> Int8", Impl: rlr.Abs[int8]},
	{Form: Function, Name: "abs", Signature: "abs(Int16) -> Int16", Impl: rlr.Abs[int16]},
	{Form: Function, Name: "abs", Signature: "abs(Int32) -> Int32", Impl: rlr.Abs[int32]},
	{Form: Function, Name: "abs", Signature: "abs(Int) -> Int", Impl: rlr.Abs[int64]},
	{Form: Function, Name: "abs", Signature: "abs(Float) -> Float", Impl: rlr.AbsFloat},
	{Form: Function, Name: "abs", Signature: "abs(Decimal) -> Decimal", Impl: rlr.AbsDecimal},
	{Form: Function, Name: "min", Signature: "min(Int8...) -> Int8", Impl: rlr.Min[int8]},
	{Form: Function, Name: "min", Signature: "min(Int16...) -> Int16", Impl: rlr.Min[int16]},
	{Form: Function, Name: "min", Signature: "min(Int32...) -> Int32", Impl: rlr.Min[int32]},
	{Form: Function, Name: "min", Signature: "min(Int...) -> Int", Impl: rlr.Min[int64]},
	{Form: Function, Name: "min", Signature: "min(Byte...) -> Byte", Impl: rlr.Min[uint8]},
	{Form: Function, Name: "min", Signature: "min(Float...) -> Float", Impl: rlr.MinFloat},
	{Form: Function, Name: "min", Signature: "min(Decimal...) -> Decimal", Impl: rlr.MinDecimal},
	{Form: Function, Name: "min", Signature: "min(String...) -> String", Impl: rlr.Min[string]},
	{Form: Function, Name: "min", Signature: "min(Byte[]...) -> Byte[]", Impl: rlr.MinBytes},
	{Form: Function, Name: "min", Signature: "min(Date...) -> Date", Impl: rlr.MinDate},
	{Form: Function, Name: "min", Signature: "min(Time...) -> Time", Impl: rlr.MinTime},
	{Form: Function, Name: "min", Signature: "min(DateTime...) -> DateTime", Impl: rlr.MinDateTime},
	{Form: Function, Name: "min", Signature: "min(Duration...) -> Duration", Impl: rlr.MinDuration},
	{Form: Function, Name: "max", Signature: "max(Int8...) -> Int8", Impl: rlr.Max[int8]},
	{Form: Function, Name: "max", Signature: "max(Int16...) -> Int16", Impl: rlr.Max[int16]},
	{Form: Function, Name: "max", Signature: "max(Int32...) -> Int32", Impl: rlr.Max[int32]},
	{Form: Function, Name: "max", Signature: "max(Int...) -> Int", Impl: rlr.Max[int64]},
	{Form: Function, Name: "max", Signature: "max(Byte...) -> Byte", Impl: rlr.Max[uint8]},
	{Form: Function, Name: "max", Signature: "max(Float...) -> Float", Impl: rlr.MaxFloat},
	{Form: Function, Name: "max", Signature: "max(Decimal...) -> Decimal", Impl: rlr.MaxDecimal},
	{Form: Function, Name: "max", Signature: "max(String...) -> String", Impl: rlr.Max[string]},
	{Form: Function, Name: "max", Signature: "max(Byte[]...) -> Byte[]", Impl: rlr.MaxBytes},
	{Form: Function, Name: "max", Signature: "max(Date...) -> Date", Impl: rlr.MaxDate},
	{Form: Function, Name: "max", Signature: "max(Time...) -> Time", Impl: rlr.MaxTime},
	{Form: Function, Name: "max", Signature: "max(DateTime...) -> DateTime", Impl: rlr.MaxDateTime},
	{Form: Function, Name: "max", Signature: "max(Duration...) -> Duration", Impl: rlr.MaxDuration},

	{Form: Operator, Name: "-", Signature: "-(Int8) -> Int8"},
	{Form: Operator, Name: "-", Signature: "-(Int16) -> Int16"},
	{Form: Operator, Name: "-", Signature: "-(Int32) -> Int32"},
	{Form: Operator, Name: "-", Signature: "-(Int) -> Int"},
	{Form: Operator, Name: "-", Signature: "-(Float) -> Float"},
	{Form: Operator, Name: "-", Signature: "-(Decimal) -> Decimal", Impl: rlr.NegDecimal},
	{Form: Operator, Name: "-", Signature: "-(Duration) -> Duration", Impl: rlr.NegDuration},
	{Form: Operator, Name: "not", Signature: "not(Bool) -> Bool"},

	{Form: Operator, Name: "+", Signature: "+(Int8, Int8) -> Int8"},
	{Form: Operator, Name: "+", Signature: "+(Int16, Int16) -> Int16"},
	{Form: Operator, Name: "+", Signature: "+(Int32, Int32) -> Int32"},
	{Form: Operator, Name: "+", Signature: "+(Int, Int) -> Int"},
	{Form: Operator, Name: "+", Signature: "+(Float, Float) -> Float"},
	{Form: Operator, Name: "+", Signature: "+(Decimal, Decimal) -> Decimal", Faults: outOfRange, Impl: rlr.AddDecimal},
	{Form: Operator, Name: "+", Signature: "+(Time, Duration) -> Time", Impl: rlr.AddTime},
	{Form: Operator, Name: "+", Signature: "+(DateTime, Duration) -> DateTime", Faults: outOfRange, Impl: rlr.AddDateTime},
	{Form: Operator, Name: "+", Signature: "+(Duration, Duration) -> Duration", Faults: outOfRange, Impl: rlr.AddDuration},
	{Form: Operator, Name: "-", Signature: "-(Int8, Int8) -> Int8"},
	{Form: Operator, Name: "-", Signature: "-(Int16, Int16) -> Int16"},
	{Form: Operator, Name: "-", Signature: "-(Int32, Int32) -> Int32"},
	{Form: Operator, Name: "-", Signature: "-(Int, Int) -> Int"},
	{Form: Operator, Name: "-", Signature: "-(Float, Float) -> Float"},
	{Form: Operator, Name: "-", Signature: "-(Decimal, Decimal) -> Decimal", Faults: outOfRange, Impl: rlr.SubDecimal},
	{Form: Operator, Name: "-", Signature: "-(Time, Duration) -> Time", Impl: rlr.SubTime},
	{Form: Operator, Name: "-", Signature: "-(Time, Time) -> Duration", Impl: rlr.TimeDifference},
	{Form: Operator, Name: "-", Signature: "-(DateTime, Duration) -> DateTime", Faults: outOfRange, Impl: rlr.SubDateTime},
	{Form: Operator, Name: "-", Signature: "-(DateTime, DateTime) -> Duration", Impl: rlr.DateTimeDifference},
	{Form: Operator, Name: "-", Signature: "-(Duration, Duration) -> Duration", Faults: outOfRange, Impl: rlr.SubDuration},
	{Form: Operator, Name: "*", Signature: "*(Int8, Int8) -> Int8"},
	{Form: Operator, Name: "*", Signature: "*(Int16, Int16) -> Int16"},
	{Form: Operator, Name: "*", Signature: "*(Int32, Int32) -> Int32"},
	{Form: Operator, Name: "*", Signature: "*(Int, Int) -> Int"},
	{Form: Operator, Name: "*", Signature: "*(Float, Float) -> Float"},
	{Form: Operator, Name: "*", Signature: "*(Decimal, Decimal) -> Decimal", Faults: outOfRange, Impl: rlr.MulDecimal},
	{Form: Operator, Name: "*", Signature: "*(Duration, Int) -> Duration", Faults: outOfRange, Impl: rlr.MulDuration},
	{Form: Operator, Name: "*", Signature: "*(Int, Duration) -> Duration", Faults: outOfRange, Impl: rlr.ScaleDuration},
	{Form: Operator, Name: "/", Signature: "/(Int8, Int8) -> Int8", Faults: divisionByZero, Impl: rlr.Div[int8]},
	{Form: Operator, Name: "/", Signature: "/(Int16, Int16) -> Int16", Faults: divisionByZero, Impl: rlr.Div[int16]},
	{Form: Operator, Name: "/", Signature: "/(Int32, Int32) -> Int32", Faults: divisionByZero, Impl: rlr.Div[int32]},
	{Form: Operator, Name: "/", Signature: "/(Int, Int) -> Int", Faults: divisionByZero, Impl: rlr.Div[int64]},
	{Form: Operator, Name: "/", Signature: "/(Float, Float) -> Float"},
	{Form: Operator, Name: "/", Signature: "/(Decimal, Decimal) -> Decimal", Faults: []string{"DivisionByZero!", "OutOfRange!"}, Impl: rlr.DivDecimal},
	{Form: Operator, Name: "/", Signature: "/(Duration, Int) -> Duration", Faults: divisionByZero, Impl: rlr.DivDuration},
	{Form: Operator, Name: "/", Signature: "/(Duration, Duration) -> Float", Faults: divisionByZero, Impl: rlr.RatioDuration},
	{Form: Operator, Name: "%", Signature: "%(Int8, Int8) -> Int8", Faults: divisionByZero, Impl: rlr.Rem[int8]},
	{Form: Operator, Name: "%", Signature: "%(Int16, Int16) -> Int16", Faults: divisionByZero, Impl: rlr.Rem[int16]},
	{Form: Operator, Name: "%", Signature: "%(Int32, Int32) -> Int32", Faults: divisionByZero, Impl: rlr.Rem[int32]},
	{Form: Operator, Name: "%", Signature: "%(Int, Int) -> Int", Faults: divisionByZero, Impl: rlr.Rem[int64]},
	{Form: Operator, Name: "%", Signature: "%(Float, Float) -> Float", Impl: rlr.RemFloat},
	{Form: Operator, Name: "%", Signature: "%(Decimal, Decimal) -> Decimal", Faults: []string{"DivisionByZero!", "OutOfRange!"}, Impl: rlr.RemDecimal},

	{Form: Operator, Name: "==", Signature: "==(Decimal, Decimal) -> Bool", Impl: rlr.EqualDecimal},
	{Form: Operator, Name: "==", Signature: "==(Date, Date) -> Bool", Impl: rlr.EqualDate},
	{Form: Operator, Name: "==", Signature: "==(Time, Time) -> Bool", Impl: rlr.EqualTime},
	{Form: Operator, Name: "==", Signature: "==(DateTime, DateTime) -> Bool", Impl: rlr.EqualDateTime},
	{Form: Operator, Name: "==", Signature: "==(Duration, Duration) -> Bool", Impl: rlr.EqualDuration},
	{Form: Operator, Name: "==", Signature: "==(Byte[], Byte[]) -> Bool", Impl: rlr.EqualBytes},
	{Form: Operator, Name: "==", Signature: "==(T, T) -> Bool"},
	{Form: Operator, Name: "!=", Signature: "!=(Decimal, Decimal) -> Bool", Impl: rlr.EqualDecimal},
	{Form: Operator, Name: "!=", Signature: "!=(Date, Date) -> Bool", Impl: rlr.EqualDate},
	{Form: Operator, Name: "!=", Signature: "!=(Time, Time) -> Bool", Impl: rlr.EqualTime},
	{Form: Operator, Name: "!=", Signature: "!=(DateTime, DateTime) -> Bool", Impl: rlr.EqualDateTime},
	{Form: Operator, Name: "!=", Signature: "!=(Duration, Duration) -> Bool", Impl: rlr.EqualDuration},
	{Form: Operator, Name: "!=", Signature: "!=(Byte[], Byte[]) -> Bool", Impl: rlr.EqualBytes},
	{Form: Operator, Name: "!=", Signature: "!=(T, T) -> Bool"},

	{Form: Operator, Name: "[]", Signature: "[](T[N], Int) -> T", Faults: outOfRange, Futures: true},
	{Form: Operator, Name: "[]", Signature: "[]([K: V], K) -> V", Faults: []string{"MissingKey!"}, Futures: true},
	{Form: Operator, Name: "[]=", Signature: "[]=(T[N], Int, T)", Faults: outOfRange, Futures: true},
	{Form: Operator, Name: "[]=", Signature: "[]=([K: V], K, V)", Futures: true},
	{Form: Operator, Name: "[:]", Signature: "[:](T[N], Int, Int) -> T[]", Faults: outOfRange, Impl: rlr.SliceDeep[any], Block: rlr.Slice[any], InPlace: rlr.SliceInPlace[any], Keeps: true, Extra: clone},
	{Form: Operator, Name: "[:]", Signature: "[:](String, Int, Int) -> String", Faults: []string{"OutOfRange!", "InvalidStringBoundary!"}, Impl: rlr.SliceString},

	{Form: TypeMethod, Name: "Int.fromByte", Signature: "Int.fromByte(Byte) -> Int", Impl: rlr.IntFromByte},
	{Form: TypeMethod, Name: "Int.fromFloat", Signature: "Int.fromFloat(Float) -> Int", Faults: outOfRange, Impl: rlr.IntFromFloat},
	{Form: TypeMethod, Name: "Int.fromString", Signature: "Int.fromString(String) -> Int", Faults: []string{"Malformed!"}, Impl: rlr.IntFromString},
	{Form: TypeMethod, Name: "Int8.fromInt", Signature: "Int8.fromInt(Int) -> Int8", Faults: outOfRange, Impl: rlr.Int8FromInt},
	{Form: TypeMethod, Name: "Int16.fromInt", Signature: "Int16.fromInt(Int) -> Int16", Faults: outOfRange, Impl: rlr.Int16FromInt},
	{Form: TypeMethod, Name: "Int32.fromInt", Signature: "Int32.fromInt(Int) -> Int32", Faults: outOfRange, Impl: rlr.Int32FromInt},
	{Form: TypeMethod, Name: "Float.fromInt", Signature: "Float.fromInt(Int) -> Float", Impl: rlr.FloatFromInt},
	{Form: TypeMethod, Name: "Float.fromDecimal", Signature: "Float.fromDecimal(Decimal) -> Float", Faults: outOfRange, Impl: rlr.FloatFromDecimal},
	{Form: TypeMethod, Name: "Byte.fromInt", Signature: "Byte.fromInt(Int) -> Byte", Faults: outOfRange, Impl: rlr.ByteFromInt},
	{Form: TypeMethod, Name: "String.fromBytes", Signature: "String.fromBytes(Byte[]) -> String", Faults: []string{"Malformed!"}, Impl: rlr.StringFromBytes},
	{Form: TypeMethod, Name: "String.fromUUId", Signature: "String.fromUUId(UUId) -> String"},
	{Form: TypeMethod, Name: "Byte[].fromString", Signature: "Byte[].fromString(String) -> Byte[]", Impl: rlr.BytesFromString},
	{Form: TypeMethod, Name: "Decimal.fromInt", Signature: "Decimal.fromInt(Int) -> Decimal", Impl: rlr.DecimalFromInt},
	{Form: TypeMethod, Name: "Decimal.fromFloat", Signature: "Decimal.fromFloat(Float) -> Decimal", Faults: outOfRange, Impl: rlr.DecimalFromFloat},
	{Form: TypeMethod, Name: "UUId.fromString", Signature: "UUId.fromString(String) -> UUId", Faults: []string{"Malformed!"}},
	{Form: TypeMethod, Name: "DateTime.fromUnix", Signature: "DateTime.fromUnix(Int seconds) -> DateTime", Faults: outOfRange, Impl: rlr.DateTimeFromSeconds},
	{Form: TypeMethod, Name: "DateTime.fromUnix", Signature: "DateTime.fromUnix(Int seconds, Int nanoseconds) -> DateTime", Faults: outOfRange, Impl: rlr.DateTimeFromUnix},

	{Form: Method, Name: "Date.addDays", Signature: "Date.addDays(Int) -> Date", Faults: outOfRange, Impl: rlr.AddDays},
	{Form: Method, Name: "Date.addWeeks", Signature: "Date.addWeeks(Int) -> Date", Faults: outOfRange, Impl: rlr.AddWeeks},
	{Form: Method, Name: "Date.addMonths", Signature: "Date.addMonths(Int) -> Date", Faults: outOfRange, Impl: rlr.AddMonths},
	{Form: Method, Name: "Date.addYears", Signature: "Date.addYears(Int) -> Date", Faults: outOfRange, Impl: rlr.AddYears},
	{Form: Method, Name: "Date.daysUntil", Signature: "Date.daysUntil(Date) -> Int", Impl: rlr.DaysUntil},
	{Form: Method, Name: "Time.until", Signature: "Time.until(Time) -> Duration", Impl: rlr.Until},
	{Form: Method, Name: "Time.since", Signature: "Time.since(Time) -> Duration", Impl: rlr.Since},
	{Form: Method, Name: "DateTime.withOffset", Signature: "DateTime.withOffset(Duration offset) -> DateTime", Faults: outOfRange, Impl: rlr.WithOffset},
	{Form: Method, Name: "DateTime.offset", Signature: "DateTime.offset() -> Duration", Impl: rlr.Offset},
	{Form: Method, Name: "DateTime.unix", Signature: "DateTime.unix() -> (Int, Int)", Impl: rlr.Unix},
	{Form: Method, Name: "Duration.highRes", Signature: "Duration.highRes() -> Bool", Impl: rlr.HighRes},
	{Form: Method, Name: "Duration.lowRes", Signature: "Duration.lowRes() -> Bool", Impl: rlr.LowRes},

	{Form: TypeMethod, Name: "r::Clock.time", Signature: "r::Clock.time() -> DateTime", Impl: rlr.Now, Standalone: true},
	{Form: TypeMethod, Name: "r::Clock.withOffset", Signature: "r::Clock.withOffset(Duration offset) -> r::Clock", Faults: outOfRange, Impl: rlr.ClockWithOffset},
	{Form: TypeMethod, Name: "r::Clock.sleep", Signature: "r::Clock.sleep(Duration)", Impl: rlr.Sleep, Extra: []Extra{Caller}, Standalone: true, Effect: true},
	{Form: Method, Name: "r::Clock.time", Signature: "r::Clock.time() -> DateTime", Impl: rlr.ClockTime, Standalone: true},
	{Form: Method, Name: "r::Clock.sleep", Signature: "r::Clock.sleep(Duration)", Impl: rlr.ClockSleep, Extra: []Extra{Caller}, Standalone: true, Effect: true},
	{Form: Method, Name: "r::Clock.offset", Signature: "r::Clock.offset() -> Duration", Impl: rlr.ClockOffset},
	{Form: TypeMethod, Name: "r::Random.new", Signature: "r::Random.new() -> r::Random", Impl: rlr.NewRandom, Standalone: true},
	{Form: TypeMethod, Name: "r::Random.seed", Signature: "r::Random.seed(Byte[32]) -> r::Random", Impl: rlr.SeedRandom},
	{Form: Method, Name: "r::Random.bits", Signature: "r::Random.bits() -> BitSet", Impl: rlr.Bits, Ref: true, Standalone: true},
	{Form: Method, Name: "r::Random.ratio", Signature: "r::Random.ratio() -> Float", Impl: rlr.Ratio, Ref: true, Standalone: true},
	{Form: Method, Name: "r::Random.range", Signature: "r::Random.range(Int from, Int to) -> Int", Impl: rlr.Range, Ref: true, Standalone: true},
}

var (
	outOfRange     = []string{"OutOfRange!"}
	divisionByZero = []string{"DivisionByZero!"}
	formatting     = []Extra{Formatting}
	clone          = []Extra{Clone}
	equal          = []Extra{Equal}
)

var orderings = []struct {
	typ  string
	impl any
}{
	{"Int8", nil}, {"Int16", nil}, {"Int32", nil}, {"Int", nil}, {"Byte", nil}, {"Float", nil}, {"String", nil},
	{"Byte[]", rlr.CompareBytes}, {"Decimal", rlr.CompareDecimal}, {"Date", rlr.CompareDate},
	{"Time", rlr.CompareTime}, {"DateTime", rlr.CompareDateTime}, {"Duration", rlr.CompareDuration},
}

func init() {
	for _, op := range []string{"<", ">", "<=", ">="} {
		for _, o := range orderings {
			Rows = append(Rows, &Row{Form: Operator, Name: op, Signature: op + "(" + o.typ + ", " + o.typ + ") -> Bool", Impl: o.impl})
		}
	}
	for _, r := range Rows {
		if r.Signature != "" {
			parseSignature(r)
		}
		r.Go, r.FlatGo = rlrName(r.Impl), rlrName(r.Flat)
		r.KeyedGo, r.FlatKeyedGo = rlrName(r.Keyed), rlrName(r.FlatKeyed)
		r.BlockGo, r.UnstableGo, r.InPlaceGo = rlrName(r.Block), rlrName(r.Unstable), rlrName(r.InPlace)
		r.InPlaceKeyedGo, r.InPlaceUnstableGo = rlrName(r.InPlaceKeyed), rlrName(r.InPlaceUnstable)
		if r.KeyedExtra == nil {
			r.KeyedExtra = r.Extra
		}
		byName[nameKey{r.Form, r.Name}] = append(byName[nameKey{r.Form, r.Name}], r)
		if r.Form == TypeMethod || r.Form == Method || r.Form == Field {
			i := strings.LastIndexByte(r.Name, '.')
			r.Owner, r.Member = r.Name[:i], r.Name[i+1:]
			byOwner[r.Owner] = append(byOwner[r.Owner], r)
		}
		r.Inputs = r.Params
		if r.Form == Method {
			r.Inputs = append([]syntax.Type{(&signatureParser{text: r.Owner}).typ()}, r.Params...)
		}
	}
	for _, r := range Rows {
		if r.Form == TypeRow && r.Capabilities.Copy == 0 {
			panic("builtin: the type " + r.Name + " states no capabilities")
		}
		r.Constant = r.constant()
	}
}

// Select returns the variant of the row for an element type of a copy class and an order class.
func (r *Row) Select(c Copy, o Order) Variant {
	switch {
	case r.Block != nil && c == Flat:
		return BlockCopy
	case r.Unstable != nil && o == Indistinguishable:
		return UnstableSort
	}
	return Base
}

// Function returns the name in package rlr of the function of a variant of the row and the extra parameters of that function.
func (r *Row) Function(v Variant) (string, []Extra) {
	switch v {
	case BlockCopy:
		return r.BlockGo, slices.DeleteFunc(slices.Clone(r.Extra), func(e Extra) bool { return e == Clone })
	case UnstableSort:
		return r.UnstableGo, r.Extra
	}
	return r.Go, r.Extra
}

// InPlaceFunction returns the name of the in-place function of the row for a variant, over a dictionary with projected keys when keyed is set, and its extra parameters.
func (r *Row) InPlaceFunction(v Variant, keyed bool) (string, []Extra) {
	switch {
	case keyed:
		return r.InPlaceKeyedGo, r.InPlaceExtra
	case v == UnstableSort:
		return r.InPlaceUnstableGo, r.InPlaceExtra
	}
	return r.InPlaceGo, r.InPlaceExtra
}

// nameKey identifies the rows of one form and name.
type nameKey struct {
	form Form
	name string
}

var (
	byName  = map[nameKey][]*Row{}
	byOwner = map[string][]*Row{}
)

// Lookup returns the rows of a form with a name, in table order.
func Lookup(form Form, name string) []*Row { return byName[nameKey{form, name}] }

// Members returns the method and field rows of an owner type, written as the rows write it.
func Members(owner string) []*Row { return byOwner[owner] }

// Names returns the parameter names of the row that names the most parameters, whose names the rows of fewer parameters share as a prefix, or nil.
func Names(rows []*Row) []string {
	var names []string
	for _, r := range rows {
		if len(r.Names) > len(names) {
			names = r.Names
		}
	}
	return names
}

// KeyProjection is a built-in key type whose Go equality is not its Ritchie equality: Impl is the rlr function that projects a key to a comparable value, Go names it, Type names the type of that value in package rlr, and KeyProjections holds one per such type by name.
type KeyProjection struct {
	Impl any
	Go   string
	Type string
}

var KeyProjections = map[string]*KeyProjection{
	"Decimal":  {Impl: rlr.KeyOfDecimal},
	"DateTime": {Impl: rlr.KeyOfDateTime},
	"Duration": {Impl: rlr.KeyOfDuration},
}

func init() {
	for _, p := range KeyProjections {
		p.Go, p.Type = rlrName(p.Impl), reflect.TypeOf(p.Impl).Out(0).Name()
	}
}

// ProjectKey returns the projection of a constant key of a projected key type, or nil for a key of any other type.
func ProjectKey(v any) any {
	for _, p := range KeyProjections {
		fn := reflect.ValueOf(p.Impl)
		if fn.Type().In(0) == reflect.TypeOf(v) {
			return fn.Call([]reflect.Value{reflect.ValueOf(v)})[0].Interface()
		}
	}
	return nil
}

// Module reports whether a module name is a standard module.
func Module(name string) bool { return name == "r" }

// Letter returns the letter of a signature type that stands for any type, or false.
func Letter(t syntax.Type) (string, bool) {
	n, ok := t.(*syntax.Named)
	if !ok || n.Module != "" || len(n.Name) != 1 || n.Name[0] < 'A' || n.Name[0] > 'Z' {
		return "", false
	}
	return n.Name, true
}

// Cardinality returns the letter of a signature array length that stands for either array form, or false.
func Cardinality(length syntax.Expr) (string, bool) {
	n, ok := length.(*syntax.Name)
	if !ok {
		return "", false
	}
	return n.Name, true
}
