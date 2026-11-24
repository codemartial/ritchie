package rlr

import (
	"io"
	"slices"
	"strconv"
	"strings"
)

// Formed is a value whose default display is composed from parts the renderer walks itself.
type Formed interface {
	DisplayForm() StructValue
}

// pending is one unit of display work.
type pending struct {
	text     string
	value    any
	isValue  bool
	embedded bool
}

// render maps a value to its default display form under the formatting module.
func render(v any, module string) string {
	var out strings.Builder
	renderInto(&out, v, module, false)
	return out.String()
}

// renderEmbedded maps a value to its display form inside a compound value.
func renderEmbedded(v any, module string) string {
	var out strings.Builder
	renderInto(&out, v, module, true)
	return out.String()
}

// renderInto writes the display of v under the formatting module.
func renderInto(out *strings.Builder, v any, module string, embedded bool) {
	stack := []pending{{value: v, isValue: true, embedded: embedded}}
	push := func(items ...pending) {
		for _, item := range slices.Backward(items) {
			stack = append(stack, item)
		}
	}
	for len(stack) > 0 {
		w := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !w.isValue {
			out.WriteString(w.text)
			continue
		}
		switch x := w.value.(type) {
		case string:
			if w.embedded {
				out.WriteString(QuoteString(x))
			} else {
				out.WriteString(x)
			}
		case optional:
			payload, present := x.payload()
			if !present {
				out.WriteString(absentDisplay)
				continue
			}
			push(pending{value: payload, isValue: true, embedded: w.embedded})
		case Formed:
			push(pending{value: x.DisplayForm(), isValue: true, embedded: w.embedded})
		case EnumValue:
			if x.Value == nil {
				out.WriteString(x.Name)
				continue
			}
			push(pending{value: x.Value, isValue: true, embedded: w.embedded})
		case StructValue:
			writeQualified(out, x.Module, x.Name, module)
			if len(x.Fields) == 0 {
				out.WriteString(" {}")
				continue
			}
			out.WriteString(" { ")
			items := make([]pending, 0, 2*len(x.Fields)+1)
			for i, f := range x.Fields {
				label := f.Name + ": "
				if i > 0 {
					label = ", " + label
				}
				items = append(items, pending{text: label}, pending{value: f.Value, isValue: true, embedded: true})
			}
			push(append(items, pending{text: " }"})...)
		case ArrayValue:
			out.WriteByte('[')
			items := make([]pending, 0, 2*len(x.Elems)+1)
			for i, el := range x.Elems {
				if i > 0 {
					items = append(items, pending{text: ", "})
				}
				items = append(items, pending{value: el, isValue: true, embedded: true})
			}
			push(append(items, pending{text: "]"})...)
		case DictValue:
			if len(x.Keys) == 0 {
				out.WriteString("[:]")
				continue
			}
			out.WriteByte('[')
			items := make([]pending, 0, 4*len(x.Keys)+1)
			for i, k := range x.Keys {
				if i > 0 {
					items = append(items, pending{text: ", "})
				}
				items = append(items,
					pending{value: k, isValue: true, embedded: true},
					pending{text: ": "},
					pending{value: x.Vals[i], isValue: true, embedded: true})
			}
			push(append(items, pending{text: "]"})...)
		case Displayer:
			out.WriteString(x.Display(module))
		default:
			text, displayable := renderScalar(x)
			if !displayable {
				io.WriteString(Diag, "ritchie runtime: unrenderable value\n")
				Exit(2)
			}
			out.WriteString(text)
		}
	}
}

// qualifiedName is a declared name under the formatting module.
func qualifiedName(declaring, name, module string) string {
	if declaring != module && declaring != "" {
		return declaring + "::" + name
	}
	return name
}

// writeQualified writes qualifiedName to out.
func writeQualified(out *strings.Builder, declaring, name, module string) {
	out.WriteString(qualifiedName(declaring, name, module))
}

// renderScalar maps a scalar value to its display form and reports whether the value is one.
func renderScalar(v any) (string, bool) {
	switch v := v.(type) {
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case Decimal:
		return v.String(), true
	case Date:
		return v.String(), true
	case Time:
		return v.String(), true
	case DateTime:
		return v.String(), true
	case Duration:
		return v.String(), true
	case BitSet:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case []byte:
		return RenderBytes(v), true
	}
	return "", false
}

// EnumValue is the display form of an enum member, which renders as its backing value when Value holds one and as its member name otherwise, and whose JSON form is always the member name.
type EnumValue struct {
	Name  string
	Value any
}

// Displayer is a value that renders its own default display under the formatting module of the containing display.
type Displayer interface {
	Display(module string) string
}

// StructValue is the display form of one struct value.
type StructValue struct {
	Module string
	Name   string
	Fields []Field
}

// Field is one displayed field.
type Field struct {
	Name  string
	Value any
}

// Display renders a struct as its contextual nominal name followed by every field in declaration order.
func (s StructValue) Display(module string) string { return render(s, module) }

// ArrayValue is the display form of a general array.
type ArrayValue struct {
	Elems []any
}

// Elements is the display form of an array.
func Elements[T any](xs []T, embed func(T) any) ArrayValue {
	out := ArrayValue{Elems: make([]any, len(xs))}
	for i, x := range xs {
		out.Elems[i] = embed(x)
	}
	return out
}

// Plain is the embedding of an element whose display form is the value itself.
func Plain[T any](x T) any { return x }

// Display renders an array as its bracketed elements.
func (a ArrayValue) Display(module string) string { return render(a, module) }

// DictValue is the display form of a dictionary.
type DictValue struct {
	Keys []any
	Vals []any
}

// Entries is the display form of a dictionary, whose entries follow one traversal of m.
func Entries[K comparable, V any](m map[K]V, embedKey func(K) any, embedValue func(V) any) DictValue {
	out := DictValue{Keys: make([]any, 0, len(m)), Vals: make([]any, 0, len(m))}
	for k, v := range m {
		out.Keys = append(out.Keys, embedKey(k))
		out.Vals = append(out.Vals, embedValue(v))
	}
	return out
}

// Display renders a dictionary as its bracketed entries.
func (d DictValue) Display(module string) string { return render(d, module) }

// Argument is one parameter of a raising routine and its value at raise.
type Argument struct {
	Name  string
	Value any
}

// Opaque is the display form of an argument value whose type has no default display.
type Opaque string

// Display renders the stand-in text.
func (o Opaque) Display(string) string { return "<" + string(o) + ">" }

// QuoteString renders a String as the double-quoted Ritchie literal that denotes it.
func QuoteString(s string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, c := range s {
		switch {
		case c == '\n':
			out.WriteString(`\n`)
		case c == '\t':
			out.WriteString(`\t`)
		case c == '\r':
			out.WriteString(`\r`)
		case c == '\\':
			out.WriteString(`\\`)
		case c == '"':
			out.WriteString(`\"`)
		case c == 0:
			out.WriteString(`\0`)
		case c < 0x20 || c == 0x7f:
			out.WriteString(`\u{`)
			out.WriteString(strconv.FormatInt(int64(c), 16))
			out.WriteByte('}')
		default:
			out.WriteRune(c)
		}
	}
	out.WriteByte('"')
	return out.String()
}

// RenderBytes is the display form of a byte array.
func RenderBytes(b []byte) string {
	var out strings.Builder
	out.WriteString("b\"")
	for _, c := range b {
		switch {
		case c == '\n':
			out.WriteString(`\n`)
		case c == '\t':
			out.WriteString(`\t`)
		case c == '\r':
			out.WriteString(`\r`)
		case c == '\\':
			out.WriteString(`\\`)
		case c == '"':
			out.WriteString(`\"`)
		case c >= 0x20 && c <= 0x7e:
			out.WriteByte(c)
		default:
			const hex = "0123456789abcdef"
			out.WriteString(`\x`)
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0xf])
		}
	}
	out.WriteByte('"')
	return out.String()
}

// CanonicalField renders one error field value canonically under the error's declaring module.
func CanonicalField(v any, module string) string {
	return renderEmbedded(v, module)
}

// Snapshot renders the raising routine at loc with its arguments as the terminal argument snapshot.
func Snapshot(loc LocationID, args ...Argument) string {
	l := locations[loc]
	var out strings.Builder
	if l.TypeName != "" {
		out.WriteString(l.TypeName)
		out.WriteByte('.')
	}
	out.WriteString(l.Function)
	out.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(a.Name)
		out.WriteString(": ")
		out.WriteString(renderEmbedded(a.Value, l.Module))
	}
	out.WriteByte(')')
	return out.String()
}

// Display renders the error value's default display form under the formatting module.
func (e RitchieError) Display(module string) string {
	m := errorTypes.at(e.typ)
	var out strings.Builder
	out.WriteString(qualifiedName(m.Module, m.Name, module))
	vals := fieldStrings(&e)
	if len(vals) == 0 {
		return out.String()
	}
	out.WriteString(" { ")
	for i, v := range vals {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(m.Fields[i].Name)
		out.WriteString(": ")
		out.WriteString(v)
	}
	out.WriteString(" }")
	return out.String()
}

// RenderError is the default terminal rendering of an error.
func RenderError(e RitchieError, module string) string {
	return renderError(e, module, true)
}

func renderError(e RitchieError, module string, traceback bool) string {
	var out strings.Builder
	out.WriteString(e.Display(module))
	if e.raised {
		out.WriteString("\n  at ")
		out.WriteString(locationText(e.location))
	}
	if e.raiser != "" {
		out.WriteString("\n  raised by ")
		out.WriteString(e.raiser)
	}
	if traceback {
		for c := e.history; c != nil; c = c.prev {
			if c == e.origin && e.raiser != "" {
				continue
			}
			out.WriteString("\n  via ")
			out.WriteString(stepText(c.ErrorStep, module))
		}
	}
	return out.String()
}

// stepText renders one history step.
func stepText(s ErrorStep, module string) string {
	m := errorTypes.at(s.typ)
	var out strings.Builder
	out.WriteString(qualifiedName(m.Module, m.Name, module))
	if len(s.fieldVals) > 0 {
		out.WriteString(" [")
		out.WriteString(strings.Join(s.fieldVals, ", "))
		out.WriteString("]")
	}
	out.WriteString(" at ")
	out.WriteString(locationText(s.location))
	return out.String()
}

// RoutineName is the name of a routine as the source writes it, like "@Ord.place", "Ct.bump", or "f", qualified by its module when it has one, which the compiler's diagnostics and the runtime report of an escaping error both use.
func RoutineName(module, owner, function string) string {
	name := function
	if owner != "" {
		name = owner + "." + function
	}
	if module != "" {
		name = module + "::" + name
	}
	return name
}

// locationText renders one location descriptor.
func locationText(id LocationID) string {
	l := locations[id]
	var out strings.Builder
	out.WriteString(RoutineName(l.Module, l.TypeName, l.Function))
	if l.Revision != "" {
		out.WriteString(" @ ")
		out.WriteString(l.Revision)
	}
	return out.String()
}
