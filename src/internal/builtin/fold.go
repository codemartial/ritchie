// This file applies a row's rlr function to constant values and converts literals to
// and from the values of rlr.
package builtin

import (
	"math"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"ritchie.dev/internal/syntax"
	"ritchie.dev/rlr"
)

var faultType = reflect.TypeOf(rlr.NoFault)

// rlrName returns the name in package rlr of a row's function without its type arguments, or the empty string for no function, and panics for a function outside rlr.
func rlrName(impl any) string {
	if impl == nil {
		return ""
	}
	full := runtime.FuncForPC(reflect.ValueOf(impl).Pointer()).Name()
	name, _, _ := strings.Cut(full, "[")
	rest, ok := strings.CutPrefix(name, "ritchie.dev/rlr.")
	if !ok || strings.Contains(rest, ".") {
		panic("builtin: a row maps to a function outside rlr: " + full)
	}
	return rest
}

// Fold applies the row's rlr function to constant arguments and returns its results and the fault it raised, or false when the row has no function to call.
func (r *Row) Fold(args []any) ([]any, rlr.Fault, bool) {
	return apply(r.Impl, r.Extra, args)
}

// apply applies an rlr function with extra parameters to constant arguments and returns its results and the fault it raised, or false when there is no function or it does not take the arguments.
func apply(impl any, extras []Extra, args []any) ([]any, rlr.Fault, bool) {
	if impl == nil {
		return nil, rlr.NoFault, false
	}
	fn := reflect.ValueOf(impl)
	ft := fn.Type()
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		var want reflect.Type
		if ft.IsVariadic() && i >= ft.NumIn()-1 {
			want = ft.In(ft.NumIn() - 1).Elem()
		} else if i < ft.NumIn() {
			want = ft.In(i)
		} else {
			return nil, rlr.NoFault, false
		}
		v, ok := convert(reflect.ValueOf(a), want)
		if !ok {
			return nil, rlr.NoFault, false
		}
		in[i] = v
	}
	for _, e := range extras {
		if e == Formatting {
			continue
		}
		if len(in) >= ft.NumIn() {
			return nil, rlr.NoFault, false
		}
		in = append(in, extra(e, ft.In(len(in))))
	}
	if !ft.IsVariadic() && len(in) != ft.NumIn() {
		return nil, rlr.NoFault, false
	}
	out := fn.Call(in)
	var results []any
	fault := rlr.NoFault
	for _, o := range out {
		if o.Type() == faultType {
			fault = o.Interface().(rlr.Fault)
			continue
		}
		results = append(results, o.Interface())
	}
	return results, fault, true
}

// convert converts a constant value to a parameter type, converting an array of constant values element by element.
func convert(v reflect.Value, want reflect.Type) (reflect.Value, bool) {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	switch {
	case !v.IsValid() && want.Kind() == reflect.Interface:
		return reflect.Zero(want), true
	case !v.IsValid():
		return v, false
	case v.Type().ConvertibleTo(want):
		return v.Convert(want), true
	case v.Kind() != reflect.Slice || want.Kind() != reflect.Slice:
		return v, false
	}
	out := reflect.MakeSlice(want, v.Len(), v.Len())
	for i := range v.Len() {
		e, ok := convert(v.Index(i), want.Elem())
		if !ok {
			return v, false
		}
		out.Index(i).Set(e)
	}
	return out, true
}

var sameConstant = reflect.ValueOf(func(v any) any { return v })

// extra returns the copy, equality, or order function of constant values as a value of a row's extra parameter type, where a copy of a constant is the constant itself and the copy function over values of any type is a Go function that is called without reflection.
func extra(e Extra, want reflect.Type) reflect.Value {
	relation := EqualValues
	switch e {
	case Clone:
		if want == sameConstant.Type() {
			return sameConstant
		}
		return reflect.MakeFunc(want, func(args []reflect.Value) []reflect.Value { return args })
	case Less:
		relation = lessValues
	}
	return reflect.MakeFunc(want, func(args []reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(relation(args[0].Interface(), args[1].Interface()))}
	})
}

// Equatable is a constant value in a form that the evaluator defines, which states its own equality.
type Equatable interface {
	Equal(other any) bool
}

// EqualValues reports whether two constant values of one type are equal under the equality of rlr: element-wise for arrays, where an array of Byte is held as bytes or as elements, by the equality function of a type whose equality is custom, and by value for every other constant.
func EqualValues(a, b any) bool {
	switch a := a.(type) {
	case Equatable:
		return a.Equal(b)
	case []any:
		if bytes, ok := b.([]byte); ok {
			return slices.EqualFunc(a, bytes, func(x any, y byte) bool { return x == y })
		}
		b, ok := b.([]any)
		return ok && rlr.EqualSlices(a, b, EqualValues)
	case []byte:
		if elems, ok := b.([]any); ok {
			return EqualValues(elems, a)
		}
		b, ok := b.([]byte)
		return ok && rlr.EqualBytes(a, b)
	case rlr.Decimal:
		b, ok := b.(rlr.Decimal)
		return ok && rlr.EqualDecimal(a, b)
	case rlr.DateTime:
		b, ok := b.(rlr.DateTime)
		return ok && rlr.EqualDateTime(a, b)
	}
	return widened(a) == widened(b)
}

// widened returns a signed integer constant as an int64, so that constants of different widths compare by their numbers.
func widened(v any) any {
	switch v := v.(type) {
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	}
	return v
}

// lessValues reports whether one constant value precedes another under the natural order of rlr.
func lessValues(a, b any) bool {
	b = widened(b)
	switch a := widened(a).(type) {
	case int64:
		return rlr.Less(a, b.(int64))
	case uint8:
		return rlr.Less(a, b.(uint8))
	case float64:
		return rlr.LessFloat(a, b.(float64))
	case string:
		return rlr.Less(a, b.(string))
	case []byte:
		return rlr.LessBytes(a, b.([]byte))
	case rlr.Decimal:
		return rlr.LessDecimal(a, b.(rlr.Decimal))
	case rlr.Date:
		return rlr.LessDate(a, b.(rlr.Date))
	case rlr.Time:
		return rlr.LessTime(a, b.(rlr.Time))
	case rlr.DateTime:
		return rlr.LessDateTime(a, b.(rlr.DateTime))
	case rlr.Duration:
		return rlr.LessDuration(a, b.(rlr.Duration))
	}
	return false
}

// Value returns the rlr value of a literal of a built-in type, where a negated literal is a negative value, or false when the literal has no value of that type.
func Value(x syntax.Expr, typ string) (any, bool) {
	negative := false
	if p, ok := x.(*syntax.Prim); ok && p.Op == "-" && len(p.Args) == 1 {
		negative = true
		x = p.Args[0]
	}
	lit, ok := x.(*syntax.Lit)
	if !ok {
		return nil, false
	}
	text := lit.Canonical()
	if negative {
		text = "-" + text
	}
	switch typ {
	case "Int", "Int8", "Int16", "Int32", "Int64":
		n, err := strconv.ParseInt(text, 10, 64)
		return n, err == nil && lit.Kind == syntax.IntLit
	case "Byte":
		n, err := strconv.ParseUint(text, 10, 8)
		return uint8(n), err == nil && lit.Kind == syntax.IntLit
	case "BitSet":
		n, err := strconv.ParseUint(text, 10, 64)
		return rlr.BitSet(n), err == nil && lit.Kind == syntax.IntLit
	case "Float":
		f, err := strconv.ParseFloat(text, 64)
		return f, err == nil && (lit.Kind == syntax.FloatLit || lit.Kind == syntax.IntLit)
	case "Decimal":
		d, f := rlr.ParseDecimal(text)
		return d, f == rlr.NoFault && (lit.Kind == syntax.FloatLit || lit.Kind == syntax.IntLit)
	case "Bool":
		return lit.Kind == syntax.KwTrue, lit.Kind == syntax.KwTrue || lit.Kind == syntax.KwFalse
	case "String":
		return string(lit.Bytes()), lit.Kind == syntax.StringLit || lit.Kind == syntax.RawStringLit
	case "Byte[]":
		return lit.Bytes(), lit.Kind == syntax.BytesLit || lit.Kind == syntax.RawBytesLit
	case "Duration":
		d, err := rlr.ParseDuration(strings.TrimPrefix(text, "-"))
		if negative {
			d = rlr.NegDuration(d)
		}
		return d, err == nil && lit.Kind == syntax.DurationLit
	case "Date":
		d, err := rlr.ParseDate(lit.Text)
		return d, err == nil && !negative && lit.Kind == syntax.DateLit
	case "Time":
		t, err := rlr.ParseTime(lit.Text)
		return t, err == nil && !negative && lit.Kind == syntax.TimeLit
	case "DateTime":
		dt, err := rlr.ParseDateTime(lit.Text)
		return dt, err == nil && !negative && lit.Kind == syntax.DateTimeLit
	}
	return nil, false
}

// Literal returns the literal tree of an rlr value at an origin, or nil for a value without a literal form.
func Literal(v any, o syntax.Origin) syntax.Expr {
	lit := func(kind syntax.Kind, text string) syntax.Expr {
		return &syntax.Lit{Origin: o, Kind: kind, Text: text}
	}
	negate := func(x syntax.Expr) syntax.Expr {
		return &syntax.Prim{Origin: o, Op: "-", Args: []syntax.Expr{x}}
	}
	switch v := v.(type) {
	case int64:
		if v < 0 {
			return negate(lit(syntax.IntLit, strconv.FormatUint(uint64(-v), 10)))
		}
		return lit(syntax.IntLit, strconv.FormatInt(v, 10))
	case int8:
		return Literal(int64(v), o)
	case int16:
		return Literal(int64(v), o)
	case int32:
		return Literal(int64(v), o)
	case uint8:
		return lit(syntax.IntLit, strconv.FormatUint(uint64(v), 10))
	case rlr.BitSet:
		return lit(syntax.IntLit, strconv.FormatUint(uint64(v), 10))
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil
		}
		text := strings.Replace(strconv.FormatFloat(math.Abs(v), 'g', -1, 64), "e+", "e", 1)
		if !strings.ContainsAny(text, ".e") {
			text += ".0"
		}
		if math.Signbit(v) {
			return negate(lit(syntax.FloatLit, text))
		}
		return lit(syntax.FloatLit, text)
	case rlr.Decimal:
		text := v.String()
		if !strings.ContainsAny(text, ".E") {
			text += "E0"
		}
		if strings.HasPrefix(text, "-") {
			return negate(lit(syntax.FloatLit, text[1:]))
		}
		return lit(syntax.FloatLit, text)
	case bool:
		if v {
			return lit(syntax.KwTrue, "true")
		}
		return lit(syntax.KwFalse, "false")
	case string:
		return lit(syntax.StringLit, syntax.QuoteString(v))
	case []byte:
		return lit(syntax.BytesLit, syntax.QuoteBytes(v))
	case rlr.Duration:
		text := v.String()
		if strings.HasPrefix(text, "-") {
			return negate(lit(syntax.DurationLit, text[1:]))
		}
		return lit(syntax.DurationLit, text)
	case rlr.Date:
		return lit(syntax.DateLit, v.String())
	case rlr.Time:
		return lit(syntax.TimeLit, v.String())
	case rlr.DateTime:
		return lit(syntax.DateTimeLit, v.String())
	}
	return nil
}
