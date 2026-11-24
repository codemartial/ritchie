// This file is the Ritchie HTTP/JSON mapping over the boundary values.
package rlr

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ritchie.dev/rlr/mesh"
)

// Raw is one undecoded JSON value.
type Raw = jsontext.Value

const (
	encoderLimit = 4 << 10
	decoderLimit = 1 << 10
)

// encoder is a JSON encoder that the runtime owns and lends to one encode at a time.
type encoder struct {
	out bytes.Buffer
	enc jsontext.Encoder
}

var encoders = sync.Pool{New: func() any { return new(encoder) }}

// JSON is the JSON text of a value given in its display form, written by a pooled encoder that is zeroed after use and returns to the pool only when its buffer holds at most encoderLimit bytes.
func JSON(v any) ([]byte, error) {
	e := encoders.Get().(*encoder)
	e.enc.Reset(&e.out)
	err := writeJSON(&e.enc, v)
	var data []byte
	if err == nil {
		data = bytes.Clone(bytes.TrimSuffix(e.out.Bytes(), []byte("\n")))
	}
	if e.release() {
		encoders.Put(e)
	}
	return data, err
}

// release zeroes the encoder's output and reports whether the encoder may return to the pool, which it may when its buffer holds at most encoderLimit bytes.
func (e *encoder) release() bool {
	e.out.Reset()
	if e.out.Cap() > encoderLimit {
		return false
	}
	spare := e.out.AvailableBuffer()
	clear(spare[:cap(spare)])
	e.enc.Reset(&e.out)
	return true
}

// writeJSON writes the JSON form of a display-form value.
func writeJSON(enc *jsontext.Encoder, v any) error {
	switch x := v.(type) {
	case nil:
		return enc.WriteToken(jsontext.Null)
	case bool:
		return enc.WriteToken(jsontext.Bool(x))
	case int8:
		return enc.WriteToken(jsontext.Int(int64(x)))
	case int16:
		return enc.WriteToken(jsontext.Int(int64(x)))
	case int32:
		return enc.WriteToken(jsontext.Int(int64(x)))
	case int64:
		return enc.WriteToken(jsontext.Int(x))
	case uint8:
		return enc.WriteToken(jsontext.Uint(uint64(x)))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return errors.New("a non-finite Float has no JSON form")
		}
		return enc.WriteToken(jsontext.Float(x))
	case string:
		return enc.WriteToken(jsontext.String(x))
	case Decimal:
		return enc.WriteToken(jsontext.String(DecimalText(x)))
	case Date:
		return enc.WriteToken(jsontext.String(DateText(x)))
	case Time:
		return enc.WriteToken(jsontext.String(TimeText(x)))
	case DateTime:
		return enc.WriteToken(jsontext.String(DateTimeText(x)))
	case Duration:
		return enc.WriteToken(jsontext.String(DurationText(x)))
	case BitSet:
		return enc.WriteToken(jsontext.String(x.String()))
	case EnumValue:
		return enc.WriteToken(jsontext.String(x.Name))
	case []byte:
		return writeArray(enc, len(x), func(i int) error { return enc.WriteToken(jsontext.Uint(uint64(x[i]))) })
	case optional:
		payload, present := x.payload()
		if !present {
			return enc.WriteToken(jsontext.Null)
		}
		return writeJSON(enc, payload)
	case StructValue:
		return writeFields(enc, x.Fields)
	case ArrayValue:
		return writeArray(enc, len(x.Elems), func(i int) error { return writeJSON(enc, x.Elems[i]) })
	case DictValue:
		return writeArray(enc, len(x.Keys), func(i int) error {
			return writeFields(enc, []Field{{Name: "key", Value: x.Keys[i]}, {Name: "value", Value: x.Vals[i]}})
		})
	case RitchieError:
		return writeFields(enc, []Field{{Name: errorTypes.at(x.typ).Name, Value: errorForm(x)}})
	case Formed:
		return writeJSON(enc, x.DisplayForm())
	}
	return errors.New("no JSON form for this value")
}

// writeArray writes a JSON array whose n elements elem writes in order.
func writeArray(enc *jsontext.Encoder, n int, elem func(i int) error) error {
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for i := range n {
		if err := elem(i); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndArray)
}

func writeFields(enc *jsontext.Encoder, fields []Field) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, f := range fields {
		if err := enc.WriteToken(jsontext.String(f.Name)); err != nil {
			return err
		}
		if err := writeJSON(enc, f.Value); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// errorForm is the display form of an error's declared fields.
func errorForm(e RitchieError) StructValue {
	if form := errorTypes.at(e.typ).Form; form != nil {
		return form(e.fields)
	}
	return StructValue{}
}

// Tokens is the JSON token stream of one text, which every decoder of that text reads in turn.
type Tokens = jsontext.Decoder

// Decoder reads the next JSON value of in into a Ritchie value of type T and retains no part of the text.
type Decoder[T any] func(in *Tokens) (T, error)

// scanner is a JSON decoder that the runtime owns and lends to one decode at a time.
type scanner struct {
	src bytes.Buffer
	dec jsontext.Decoder
}

var scanners = sync.Pool{New: func() any { return new(scanner) }}

// scan has read consume the one JSON value in raw from a pooled decoder over raw, rejects any text after the value, and clears the decoder, which returns to the pool only when raw holds at most decoderLimit bytes because the decoder keeps the member names it read.
func scan(raw Raw, read func(in *Tokens) error) error {
	s := scanners.Get().(*scanner)
	s.load(raw)
	err := read(&s.dec)
	if err == nil {
		if _, err = s.dec.ReadToken(); err == io.EOF {
			err = nil
		} else if err == nil {
			err = errors.New("text follows the JSON value")
		}
	}
	s.clear()
	if len(raw) <= decoderLimit {
		scanners.Put(s)
	}
	return err
}

// load points the decoder at raw, which it reads in place.
func (s *scanner) load(raw Raw) {
	s.src = *bytes.NewBuffer(raw)
	s.dec.Reset(&s.src)
}

// clear drops the decoder's references to its last input.
func (s *scanner) clear() {
	s.src = bytes.Buffer{}
	s.dec.Reset(&s.src)
}

// Decode reads the one JSON value in raw through d in a single pass and rejects any text after the value.
func Decode[T any](raw Raw, d Decoder[T]) (v T, err error) {
	err = scan(raw, func(in *Tokens) error {
		v, err = d(in)
		return err
	})
	return v, err
}

// begin reads the token that opens a JSON array or object, which what describes.
func begin(in *Tokens, open jsontext.Kind, what string) error {
	tok, err := in.ReadToken()
	if err == nil && tok.Kind() != open {
		err = errors.New("the JSON value is not " + what)
	}
	return err
}

// members reads a JSON object from in and hands the unquoted name of each member to visit, which reads the member's value from in and uses the name only before that read.
func members(in *Tokens, visit func(name []byte) error) error {
	if err := begin(in, jsontext.KindBeginObject, "an object"); err != nil {
		return err
	}
	for in.PeekKind() != jsontext.KindEndObject {
		quoted, err := in.ReadValue()
		if err != nil {
			return err
		}
		name, err := unquote(quoted)
		if err != nil {
			return err
		}
		if err := visit(name); err != nil {
			return err
		}
	}
	_, err := in.ReadToken()
	return err
}

// unquote is the content of a JSON string that a decoder has validated, as a subslice of quoted when it holds no escape.
func unquote(quoted Raw) ([]byte, error) {
	if bytes.IndexByte(quoted, '\\') < 0 {
		return quoted[1 : len(quoted)-1], nil
	}
	return jsontext.AppendUnquote(nil, quoted)
}

// scalar reads the next JSON token of in through accept, which accepts a token of the kind that what describes, and rejects a null.
func scalar[T any](in *Tokens, what string, accept func(tok jsontext.Token) (T, bool, error)) (T, error) {
	tok, err := in.ReadToken()
	if err != nil {
		var zero T
		return zero, err
	}
	v, ok, err := accept(tok)
	if !ok {
		if tok.Kind() == jsontext.KindNull {
			return v, errors.New("null is not a value of this type")
		}
		return v, errors.New("the JSON value is not " + what)
	}
	return v, err
}

// DecodeBool reads a JSON boolean.
func DecodeBool(in *Tokens) (bool, error) {
	return scalar(in, "a boolean", func(tok jsontext.Token) (bool, bool, error) {
		k := tok.Kind()
		return k == jsontext.KindTrue, k == jsontext.KindTrue || k == jsontext.KindFalse, nil
	})
}

// DecodeString reads a JSON string.
func DecodeString(in *Tokens) (string, error) {
	return scalar(in, "a string", func(tok jsontext.Token) (string, bool, error) {
		if tok.Kind() != jsontext.KindString {
			return "", false, nil
		}
		return tok.String(), true, nil
	})
}

// DecodeFloat reads a JSON number as the nearest Float and rejects a number beyond the Float range.
func DecodeFloat(in *Tokens) (float64, error) {
	return scalar(in, "a number", func(tok jsontext.Token) (float64, bool, error) {
		if tok.Kind() != jsontext.KindNumber {
			return 0, false, nil
		}
		f, err := tok.Float()
		return f, true, err
	})
}

// DecodeByte reads a JSON integer number within the Byte range.
func DecodeByte(in *Tokens) (uint8, error) {
	return scalar(in, "a number", func(tok jsontext.Token) (uint8, bool, error) {
		if tok.Kind() != jsontext.KindNumber {
			return 0, false, nil
		}
		n, err := tok.Uint()
		if err == nil && n > math.MaxUint8 {
			err = errors.New(tok.String() + " is outside the Byte range")
		}
		return uint8(n), true, err
	})
}

// DecodeInt reads a JSON integer number within the range of T.
func DecodeInt[T Integer](in *Tokens) (T, error) {
	return scalar(in, "a number", func(tok jsontext.Token) (T, bool, error) {
		if tok.Kind() != jsontext.KindNumber {
			return 0, false, nil
		}
		n, err := tok.Int()
		if err == nil && int64(T(n)) != n {
			err = errors.New(tok.String() + " is outside the range of the integer type")
		}
		return T(n), true, err
	})
}

// DecodeDecimal reads a JSON string or a JSON number as a Decimal: the string contents or the number token, read by ReadDecimal without a conversion through binary64.
func DecodeDecimal(in *Tokens) (Decimal, error) {
	text, err := scalar(in, "a string or a number", func(tok jsontext.Token) (string, bool, error) {
		k := tok.Kind()
		return tok.String(), k == jsontext.KindNumber || k == jsontext.KindString, nil
	})
	if err != nil {
		return Decimal{}, err
	}
	d, fault := ReadDecimal(text)
	if fault != NoFault {
		return Decimal{}, errors.New(text + " is not a Decimal value")
	}
	return d, nil
}

func decodeText[T any](in *Tokens, parse func(string) (T, error)) (T, error) {
	text, err := DecodeString(in)
	if err != nil {
		var zero T
		return zero, err
	}
	return parse(text)
}

// DecodeDate reads the external text of a Date.
func DecodeDate(in *Tokens) (Date, error) { return decodeText(in, ReadDate) }

// DecodeTime reads the external text of a Time.
func DecodeTime(in *Tokens) (Time, error) { return decodeText(in, ReadTime) }

// DecodeDuration reads the external text of a Duration.
func DecodeDuration(in *Tokens) (Duration, error) { return decodeText(in, ReadDuration) }

// DecodeDateTime reads the external text of a DateTime and reports on the diagnostic stream a fraction that it truncated after the ninth digit.
func DecodeDateTime(in *Tokens) (DateTime, error) {
	return decodeText(in, func(text string) (DateTime, error) {
		dt, truncated, err := ReadDateTime(text)
		if truncated {
			io.WriteString(Diag, "ritchie runtime: the DateTime "+strconv.Quote(text)+" has more than nine fractional digits and was truncated after the ninth\n")
		}
		return dt, err
	})
}

// DecodeEnum reads the member-name string of an enum, given the member names in declaration order, as the ordinal of the member.
func DecodeEnum[T ~uint32](in *Tokens, names []string) (T, error) {
	text, err := DecodeString(in)
	if err != nil {
		return 0, err
	}
	for i, n := range names {
		if n == text {
			return T(i), nil
		}
	}
	return 0, errors.New(strconv.Quote(text) + " is not a member of the enum")
}

// DecodeBitSet reads the display form of a BitSet.
func DecodeBitSet(in *Tokens) (BitSet, error) {
	return decodeText(in, func(text string) (BitSet, error) {
		digits, ok := strings.CutPrefix(text, "0x")
		if !ok || len(digits) != 16 {
			return 0, errors.New(text + " is not a BitSet value")
		}
		bits, err := strconv.ParseUint(digits, 16, 64)
		return BitSet(bits), err
	})
}

// DecodeBytes reads an array of integer numbers as a byte sequence.
func DecodeBytes(in *Tokens) ([]byte, error) { return DecodeArray(in, DecodeByte) }

// DecodeArray reads an array through the decoder of its elements.
func DecodeArray[T any](in *Tokens, elem Decoder[T]) ([]T, error) {
	if err := begin(in, jsontext.KindBeginArray, "an array"); err != nil {
		return nil, err
	}
	out := []T{}
	for in.PeekKind() != jsontext.KindEndArray {
		v, err := elem(in)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if _, err := in.ReadToken(); err != nil {
		return nil, err
	}
	return out, nil
}

// DecodeResults reads the results of a method as a response carries them: the one value of a method with one result, and otherwise an array of count values, each decoded by result.
func DecodeResults(in *Tokens, count int, result func(i int) error) error {
	if count == 1 {
		return result(0)
	}
	if err := begin(in, jsontext.KindBeginArray, "an array"); err != nil {
		return err
	}
	for i := range count {
		if err := result(i); err != nil {
			return err
		}
	}
	if in.PeekKind() != jsontext.KindEndArray {
		return errors.New("the array holds more than " + strconv.Itoa(count) + " elements")
	}
	_, err := in.ReadToken()
	return err
}

// DecodeFixed reads an array of exactly length elements through the decoder of its elements.
func DecodeFixed[T any](in *Tokens, length int, elem Decoder[T]) ([]T, error) {
	out, err := DecodeArray(in, elem)
	if err == nil && len(out) != length {
		err = errors.New("the array holds " + strconv.Itoa(len(out)) + " elements, not " + strconv.Itoa(length))
	}
	return out, err
}

// decodePairs reads an array of key and value objects, decodes the members of each object in the order of the text, and hands each decoded pair to put.
func decodePairs[K, V any](in *Tokens, key Decoder[K], value Decoder[V], put func(K, V)) error {
	if err := begin(in, jsontext.KindBeginArray, "an array"); err != nil {
		return err
	}
	for in.PeekKind() != jsontext.KindEndArray {
		var k K
		var v V
		var hasKey, hasValue bool
		err := members(in, func(name []byte) (err error) {
			switch string(name) {
			case "key":
				k, err = key(in)
				hasKey = true
			case "value":
				v, err = value(in)
				hasValue = true
			default:
				err = unknownProperty(string(name))
			}
			return err
		})
		if err != nil {
			return err
		}
		if !hasKey || !hasValue {
			return errors.New("a dictionary entry is an object with a key and a value")
		}
		put(k, v)
	}
	_, err := in.ReadToken()
	return err
}

// DecodeDict reads a dictionary through the decoders of its keys and values.
func DecodeDict[K comparable, V any](in *Tokens, key Decoder[K], value Decoder[V]) (map[K]V, error) {
	out := map[K]V{}
	err := decodePairs(in, key, value, func(k K, v V) { out[k] = v })
	return out, err
}

// DecodeKeyed is DecodeDict for a key type whose language equality is not Go's.
func DecodeKeyed[P comparable, K, V any](in *Tokens, project func(K) P, key Decoder[K], value Decoder[V]) (map[P]Keyed[K, V], error) {
	out := map[P]Keyed[K, V]{}
	err := decodePairs(in, key, value, func(k K, v V) { StoreKeyed(out, project(k), k, v) })
	return out, err
}

// DecodeOption reads a nullable value whose null is absence.
func DecodeOption[T any](in *Tokens, elem Decoder[T]) (Option[T], error) {
	if in.PeekKind() == jsontext.KindNull {
		_, err := in.ReadToken()
		return None[T](), err
	}
	v, err := elem(in)
	if err != nil {
		return None[T](), err
	}
	return Some(v), nil
}

// DecodeBox reads a nullable of a struct that contains itself, whose null is absence.
func DecodeBox[T any](in *Tokens, elem Decoder[T]) (Box[T], error) {
	if in.PeekKind() == jsontext.KindNull {
		_, err := in.ReadToken()
		return BoxNone[T](), err
	}
	v, err := elem(in)
	if err != nil {
		return BoxNone[T](), err
	}
	return BoxSome(v), nil
}

// DecodeObject reads a JSON object whose member names are among names: field reads the value of the member at each index as the text reaches it, a member outside names is rejected, and an absent member is an error unless optional marks its index.
func DecodeObject(in *Tokens, names []string, optional []bool, field func(i int) error) error {
	seen := make([]bool, len(names))
	err := members(in, func(name []byte) error {
		i := slices.IndexFunc(names, func(n string) bool { return n == string(name) })
		if i < 0 {
			return unknownProperty(string(name))
		}
		seen[i] = true
		return field(i)
	})
	if err != nil {
		return err
	}
	for i, name := range names {
		if !seen[i] && (optional == nil || !optional[i]) {
			return MissingProperty(name)
		}
	}
	return nil
}

func unknownProperty(name string) error { return errors.New("unknown property " + strconv.Quote(name)) }

// MissingProperty is the decoding error of an absent required property.
func MissingProperty(name string) error { return errors.New("missing property " + strconv.Quote(name)) }

// DecodeError reads the one-property object form of an error value.
func DecodeError(in *Tokens) (e RitchieError, err error) {
	seen := false
	err = members(in, func(name []byte) (err error) {
		if seen {
			return errors.New("an error value is an object with one property, its error name")
		}
		seen = true
		id, known := errorTypes.byName[string(name)]
		if !known {
			return errors.New("unknown error type " + string(name))
		}
		e, err = decodePayload(id, in)
		return err
	})
	if err == nil && !seen {
		err = errors.New("an error value is an object with one property, its error name")
	}
	if err != nil {
		return RitchieError{}, err
	}
	return e, nil
}

// DecodeErrorOf reads the one-property object form of an error value of the registered type id.
func DecodeErrorOf(in *Tokens, id ErrorTypeID) (RitchieError, error) {
	e, err := DecodeError(in)
	if err == nil && e.typ != id {
		err = errors.New("the error value is not a " + errorTypes.at(id).Name)
	}
	return e, err
}

// decodePayload reads the fields of an error value of the registered type id, and skips the value for a type without fields.
func decodePayload(id ErrorTypeID, in *Tokens) (RitchieError, error) {
	fields := errorTypes.at(id).Decode
	if fields == nil {
		return NewError(id, nil), in.SkipValue()
	}
	value, err := fields(in)
	if err != nil {
		return RitchieError{}, err
	}
	return NewError(id, value), nil
}

// WireError is the transport encoding of an error value by defining module and name with its fields in JSON form.
type WireError struct {
	Module string `json:"module"`
	Name   string `json:"name"`
	Fields Raw    `json:"fields,omitzero"`
}

// EncodeError is the transport encoding of e.
func EncodeError(e RitchieError) WireError {
	m := errorTypes.at(e.typ)
	w := WireError{Module: m.Module, Name: m.Name}
	if len(m.Fields) > 0 {
		w.Fields, _ = JSON(errorForm(e))
	}
	return w
}

// DecodeWireError recovers the error value of w under this program's registered types.
func DecodeWireError(w WireError) (RitchieError, bool) {
	id, known := errorTypes.byKey[typeKey{w.Module, w.Name}]
	if !known {
		return RitchieError{}, false
	}
	if errorTypes.at(id).Decode == nil {
		return NewError(id, nil), true
	}
	e, err := Decode(w.Fields, func(in *Tokens) (RitchieError, error) { return decodePayload(id, in) })
	return e, err == nil
}

const (
	problemType = "application/problem+json"
	jsonType    = "application/json"
	bodyLimit   = 1 << 20
	routePrefix = "/"
)

// statusOf is the HTTP status of a failure under the Ritchie HTTP/JSON mapping.
func statusOf(name string) int {
	switch name {
	case "Malformed!":
		return http.StatusBadRequest
	case "NotFound!":
		return http.StatusNotFound
	case "Duplicate!", "Conflict!", "Frozen!":
		return http.StatusConflict
	case "Unavailable!":
		return http.StatusServiceUnavailable
	case "Unmodified!", "Cancelled!":
		return http.StatusGatewayTimeout
	case "Unverified!", "Rejected!":
		return http.StatusInternalServerError
	}
	return http.StatusUnprocessableEntity
}

// problem is the failure body of an error under the mapping.
func problem(e RitchieError, module string) mesh.Reply {
	m := errorTypes.at(e.typ)
	status := statusOf(m.Name)
	fields := []Field{
		{Name: "type", Value: "urn:ritchie:error:" + strings.TrimSuffix(m.Name, "!")},
		{Name: "title", Value: m.Name},
		{Name: "status", Value: int64(status)},
		{Name: "detail", Value: e.Display(module)},
	}
	if len(m.Fields) > 0 {
		fields = append(fields, Field{Name: "ritchieFields", Value: errorForm(e)})
	}
	data, _ := JSON(StructValue{Fields: fields})
	return mesh.Reply{Status: status, ContentType: problemType, Body: data}
}

// expose starts the local exposure of every service method of the application on port.
func expose(rt *mesh.Runtime, app Application, budget time.Duration, port int) error {
	routes := map[string]mesh.Route{}
	for _, u := range app.Units {
		for _, m := range u.Methods {
			path := routePrefix + u.Route + "/" + strings.TrimPrefix(u.Locator.Unit, "@") + "/" + m.Name
			routes[path] = mesh.Route{Locator: u.Locator, Method: m.Name, Mutates: m.Mutates, Decode: decodeRequest(u.Module, m), Encode: encodeResponse(u.Module, m)}
		}
	}
	x := mesh.Exposure{Runtime: rt, Routes: routes, Budget: budget, BodyLimit: bodyLimit}
	return x.Serve(ServeAddress(port))
}

// decodeRequest reads a request body into the arguments of m or renders the Malformed! failure.
func decodeRequest(module string, m Method) func([]byte) ([]any, *mesh.Reply) {
	arguments := m.Decode
	return func(body []byte) ([]any, *mesh.Reply) {
		args, err := Decode(body, arguments)
		if err != nil {
			reply := problem(NewError(Malformed.TypeID(), nil), module)
			return nil, &reply
		}
		return args, nil
	}
}

// encodeResponse renders the response of an invocation of m.
func encodeResponse(module string, m Method) func(mesh.Response) mesh.Reply {
	mutates, encode := m.Mutates, m.Encode
	return func(r mesh.Response) mesh.Reply {
		if err := failureOf(r, mutates); err != nil {
			return problem(err.(RitchieError), module)
		}
		values := encode(r.AppResponse.Results)
		var body any
		switch len(values) {
		case 0:
			return mesh.Reply{Status: http.StatusNoContent}
		case 1:
			body = values[0]
		default:
			body = ArrayValue{Elems: values}
		}
		data, _ := JSON(body)
		return mesh.Reply{Status: http.StatusOK, ContentType: jsonType, Body: data}
	}
}
