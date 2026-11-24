// This file is the sealed carrier of a declared Ritchie failure and its immutable
// program metadata.
package rlr

import (
	"io"
	"slices"
)

// ErrorTypeID indexes the registered error-type descriptors of one linked program.
type ErrorTypeID uint32

// LocationID indexes the registered location descriptors of one linked program.
type LocationID uint32

// ErrorFieldMeta describes one declared field of an error type.
type ErrorFieldMeta struct {
	Name     string
	TypeName string
}

// ErrorTypeMeta describes one declared error type.
type ErrorTypeMeta struct {
	Module string
	Name   string
	Fields []ErrorFieldMeta
	Render func(payload any) []string
	Form   func(payload any) StructValue
	Decode Decoder[any]
}

// LocationMeta describes one program location.
type LocationMeta struct {
	Revision string
	Module   string
	TypeName string
	Function string
}

// Fault is the outcome of a runtime helper that can fail, and names an error of the built-in catalog.
type Fault uint8

const (
	NoFault Fault = iota
	DivisionByZero
	OutOfRange
	InvalidStringBoundary
	Malformed
	MissingKey
	LengthMismatch
	Unavailable
	Rejected
	Unverified
	Conflict
	Unmodified
	Cancelled
	NotFound
	Duplicate
	Frozen
)

var catalog = [...]ErrorTypeMeta{
	DivisionByZero - 1:        {Name: "DivisionByZero!"},
	OutOfRange - 1:            {Name: "OutOfRange!"},
	InvalidStringBoundary - 1: {Name: "InvalidStringBoundary!"},
	Malformed - 1:             {Name: "Malformed!"},
	MissingKey - 1:            {Name: "MissingKey!"},
	LengthMismatch - 1:        {Name: "LengthMismatch!"},
	Unavailable - 1:           {Name: "Unavailable!"},
	Rejected - 1:              {Name: "Rejected!"},
	Unverified - 1:            {Name: "Unverified!"},
	Conflict - 1:              {Name: "Conflict!"},
	Unmodified - 1:            {Name: "Unmodified!"},
	Cancelled - 1:             {Name: "Cancelled!"},
	NotFound - 1:              {Name: "NotFound!"},
	Duplicate - 1:             {Name: "Duplicate!"},
	Frozen - 1:                {Name: "Frozen!"},
}

const FirstDeclared ErrorTypeID = ErrorTypeID(len(catalog))

// Faults lists every error of the built-in catalog.
func Faults() []Fault {
	out := make([]Fault, len(catalog))
	for i := range out {
		out[i] = Fault(i + 1)
	}
	return out
}

// Name is the catalog name of the error a fault raises.
func (f Fault) Name() string {
	if f == NoFault {
		return ""
	}
	return catalog[f-1].Name
}

// TypeID is the catalog type id of the error a fault raises.
func (f Fault) TypeID() ErrorTypeID { return ErrorTypeID(f - 1) }

// errorType is one registered error type.
type errorType struct {
	ErrorTypeMeta
	desc ErrorTypeDesc
}

// location is one registered location.
type location struct {
	LocationMeta
	desc ErrorLocationDesc
}

// typeKey identifies an error type by its defining module and name.
type typeKey struct {
	module, name string
}

// registry is the program's error types with their lookup indexes.
type registry struct {
	types  []errorType
	byName map[string]ErrorTypeID
	byKey  map[typeKey]ErrorTypeID
}

// at is the registered type with the given id.
func (r registry) at(id ErrorTypeID) errorType { return r.types[id] }

var (
	errorTypes = describeTypes(catalog[:])
	locations  []location
)

// RegisterErrorTypes installs the program's error-type descriptors after the catalog.
func RegisterErrorTypes(types []ErrorTypeMeta) {
	errorTypes = describeTypes(append(slices.Clone(catalog[:]), types...))
}

// RegisterLocations installs the program's location descriptors.
func RegisterLocations(locs []LocationMeta) {
	locations = make([]location, len(locs))
	for i, l := range locs {
		desc := ErrorLocationDesc{Revision: l.Revision, Module: l.Module, Function: l.Function}
		if l.TypeName != "" {
			desc.TypeName = Some(l.TypeName)
		}
		locations[i] = location{LocationMeta: l, desc: desc}
	}
}

// describeTypes pairs each type's metadata with its descriptor and indexes the types by name.
func describeTypes(types []ErrorTypeMeta) registry {
	out := registry{types: make([]errorType, len(types)), byName: map[string]ErrorTypeID{}, byKey: map[typeKey]ErrorTypeID{}}
	for i, m := range types {
		fields := make([]ErrorFieldDesc, len(m.Fields))
		for j, f := range m.Fields {
			fields[j] = ErrorFieldDesc{Name: f.Name, TypeName: f.TypeName}
		}
		out.types[i] = errorType{ErrorTypeMeta: m, desc: ErrorTypeDesc{Module: m.Module, Name: m.Name, Fields: fields}}
		id := ErrorTypeID(i)
		if _, taken := out.byName[m.Name]; !taken {
			out.byName[m.Name] = id
		}
		out.byKey[typeKey{m.Module, m.Name}] = id
	}
	return out
}

// ErrorStep is one completed propagation step.
type ErrorStep struct {
	typ       ErrorTypeID
	location  LocationID
	fieldVals []string
}

// trail is a persistent history.
type trail struct {
	ErrorStep
	prev *trail

	length int
}

// push is the trail extended by one most recent step.
func (t *trail) push(s ErrorStep) *trail {
	return &trail{ErrorStep: s, prev: t, length: t.count() + 1}
}

// count is the number of steps in the trail.
func (t *trail) count() int {
	if t == nil {
		return 0
	}
	return t.length
}

// list is the trail's steps as a fresh slice.
func (t *trail) list() []ErrorStep {
	out := make([]ErrorStep, t.count())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = t.ErrorStep
		t = t.prev
	}
	return out
}

// ErrorTypeDesc is the public descriptor of an error type.
type ErrorTypeDesc struct {
	Module string
	Name   string
	Fields []ErrorFieldDesc
}

// ErrorFieldDesc is the public descriptor of one declared error field.
type ErrorFieldDesc struct {
	Name     string
	TypeName string
}

// ErrorLocationDesc is the public descriptor of one program location.
type ErrorLocationDesc struct {
	Revision string
	Module   string
	TypeName Option[string]
	Function string
}

// DisplayForm is the default display of a step: the struct form of its three fields.
func (s ErrorStep) DisplayForm() StructValue {
	return StructValue{Name: "ErrorStep", Fields: []Field{
		{Name: "typ", Value: s.Type()}, {Name: "location", Value: s.Location()}, {Name: "fieldVals", Value: Elements(s.fieldVals, Plain[string])},
	}}
}

// DisplayForm is the default display of an error-type descriptor: the struct form of its three fields.
func (d ErrorTypeDesc) DisplayForm() StructValue {
	return StructValue{Name: "ErrorType", Fields: []Field{
		{Name: "module", Value: d.Module}, {Name: "name", Value: d.Name}, {Name: "fields", Value: Elements(d.Fields, Plain[ErrorFieldDesc])},
	}}
}

// DisplayForm is the default display of an error-field descriptor: the struct form of its two fields.
func (d ErrorFieldDesc) DisplayForm() StructValue {
	return StructValue{Name: "ErrorField", Fields: []Field{{Name: "name", Value: d.Name}, {Name: "typeName", Value: d.TypeName}}}
}

// DisplayForm is the default display of a location descriptor: the struct form of its four fields.
func (d ErrorLocationDesc) DisplayForm() StructValue {
	return StructValue{Name: "ErrorLocation", Fields: []Field{
		{Name: "revision", Value: d.Revision}, {Name: "module", Value: d.Module}, {Name: "typeName", Value: d.TypeName}, {Name: "function", Value: d.Function},
	}}
}

// EqualErrorStep reports whether two steps have one error type, one location, and equal field strings.
func EqualErrorStep(a, b ErrorStep) bool {
	return a.typ == b.typ && a.location == b.location && slices.Equal(a.fieldVals, b.fieldVals)
}

// EqualErrorType reports whether two error-type descriptors have equal fields.
func EqualErrorType(a, b ErrorTypeDesc) bool {
	return a.Module == b.Module && a.Name == b.Name && slices.Equal(a.Fields, b.Fields)
}

// EqualErrorField reports whether two error-field descriptors have equal fields.
func EqualErrorField(a, b ErrorFieldDesc) bool { return a == b }

// EqualErrorLocation reports whether two location descriptors have equal fields.
func EqualErrorLocation(a, b ErrorLocationDesc) bool { return a == b }

// Type is the step's nominal error-type descriptor.
func (s ErrorStep) Type() ErrorTypeDesc { return typeDesc(s.typ) }

// Location is the step's location descriptor.
func (s ErrorStep) Location() ErrorLocationDesc { return locationDesc(s.location) }

// FieldVals is the step's canonical field strings in declaration order.
func (s ErrorStep) FieldVals() []string { return slices.Clone(s.fieldVals) }

// typeDesc is the registered descriptor of an error type as an independent value.
func typeDesc(id ErrorTypeID) ErrorTypeDesc {
	desc := errorTypes.at(id).desc
	desc.Fields = slices.Clone(desc.Fields)
	return desc
}

// locationDesc is the registered descriptor of a location.
func locationDesc(id LocationID) ErrorLocationDesc { return locations[id].desc }

// RitchieError is the one generated value representing a declared Ritchie failure, where origin is the step of its history at which the error was raised, which a report states once as its raising routine.
type RitchieError struct {
	typ      ErrorTypeID
	fields   any
	location LocationID
	raised   bool
	history  *trail
	origin   *trail

	fieldVals []string

	raiser string

	arrived bool
}

// Error renders the error without a formatting module.
func (e RitchieError) Error() string { return RenderError(e, "") }

// NewError is an inert error value.
func NewError(typ ErrorTypeID, payload any) RitchieError {
	return RitchieError{typ: typ, fields: payload}
}

// Payload is the current error's typed payload record.
func Payload(e RitchieError) any { return e.fields }

// TypeIDOf is the current error's nominal type id.
func TypeIDOf(e RitchieError) ErrorTypeID { return e.typ }

// SameType reports whether two errors have one current nominal type.
func SameType(a, b RitchieError) bool { return a.typ == b.typ }

// EqualErrors reports whether two errors have one current nominal type and payloads equal under eq.
func EqualErrors[P any](a, b RitchieError, eq func(P, P) bool) bool {
	return a.typ == b.typ && eq(a.fields.(P), b.fields.(P))
}

// History is the error's completed prior steps.
func History(e RitchieError) []ErrorStep { return e.history.list() }

// Raise makes an error current at loc through an error-valued return.
func Raise(e RitchieError, loc LocationID, raiser string) RitchieError {
	if e.raised {
		return Bubble(e, loc)
	}
	e.location = loc
	e.raised = true
	e.raiser = raiser
	e.origin = nil
	return e
}

// Origin is the catalog error a failed built-in operation establishes at loc.
func Origin(f Fault, loc LocationID, raiser string) RitchieError {
	return Raise(NewError(f.TypeID(), nil), loc, raiser)
}

// Bubble records one propagation step at loc, where the first step after a raise is the error's origin.
func Bubble(e RitchieError, loc LocationID) RitchieError {
	e.history = e.history.push(currentStep(&e))
	if e.origin == nil {
		e.origin = e.history
	}
	e.location = loc
	return e
}

// Cross is the error as module receives it from a callee in another module.
func Cross(e RitchieError, module string) RitchieError {
	if from := locations[e.location].Module; from != module {
		e.history = compact(e.history, from)
	}
	return e
}

// ReplaceError installs next.
func ReplaceError(caught, next RitchieError, loc LocationID, raiser string) RitchieError {
	next.history = caught.history.push(currentStep(&caught))
	next.location = loc
	next.raised = true
	next.raiser = raiser
	next.origin = nil
	return next
}

// RecoveryRaise makes next current at loc through an error-valued return inside a handler arm whose caught error is caught.
func RecoveryRaise(caught, next RitchieError, loc LocationID, raiser string) RitchieError {
	if next.raised {
		return Bubble(next, loc)
	}
	return ReplaceError(caught, next, loc, raiser)
}

const (
	terminalLevel = 1
	serviceLevel  = 2
	recoveryLevel = 3
)

var level = terminalLevel

// ReportUnrecovered reports a failure no handler recovered while a recovery ran.
func ReportUnrecovered(module string, e RitchieError) {
	report(recoveryLevel, module, e)
}

// report makes the one reporting attempt of a disposition, writing the report only when the process reporting level reaches at and its history steps only at recoveryLevel.
func report(at int, module string, e RitchieError) {
	if level < at {
		return
	}
	io.WriteString(Diag, renderError(e, module, level >= recoveryLevel)+"\n")
}

// currentStep is the current error as a completed history step.
func currentStep(e *RitchieError) ErrorStep {
	return ErrorStep{typ: e.typ, location: e.location, fieldVals: fieldStrings(e)}
}

// compact removes the exited module's segment.
func compact(history *trail, module string) *trail {
	for history != nil && locations[history.location].Module == module {
		history = history.prev
	}
	return history
}

// fieldStrings is the canonical field strings of the current payload in declaration order.
func fieldStrings(e *RitchieError) []string {
	if e.fieldVals != nil {
		return e.fieldVals
	}
	m := errorTypes.at(e.typ)
	if m.Render == nil {
		return nil
	}
	e.fieldVals = m.Render(e.fields)
	return e.fieldVals
}
