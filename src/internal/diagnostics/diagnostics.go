// Package diagnostics defines the compiler's diagnostic records and their deterministic
// ordering and public encodings.
package diagnostics

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	SeverityError   = "error"
	SeverityFailure = "failure"
	SeverityNote    = "note"
)

const (
	InvalidUTF8            = "source.invalid-utf8"
	NonASCIIToken          = "source.non-ascii-token"
	UnclosedBlock          = "comment.unclosed-block"
	UnexpectedClass        = "name.unexpected-class"
	InvalidSigilBody       = "name.invalid-sigil-body"
	UnattachedSigil        = "name.unattached-sigil"
	ExcludedForm           = "syntax.excluded-form"
	ReservedForm           = "syntax.reserved-form"
	StrayContinuation      = "syntax.stray-continuation"
	UnclosedDelimiter      = "syntax.unclosed-delimiter"
	InvalidChain           = "syntax.invalid-chain"
	TernaryNesting         = "syntax.ternary-nesting"
	TernaryCoalescingMix   = "syntax.ternary-coalescing-mix"
	RepeatedQualifier      = "syntax.repeated-qualifier"
	DiscardedValue         = "syntax.discarded-value"
	UnexpectedToken        = "syntax.unexpected-token"
	LiteralInvalidInteger  = "literal.invalid-integer"
	LiteralInvalidFloat    = "literal.invalid-float"
	LiteralInvalidTemporal = "literal.invalid-temporal"
	LiteralInvalidString   = "literal.invalid-string"
	LiteralInvalidBytes    = "literal.invalid-byte-array"
	LiteralInvalidEscape   = "literal.invalid-escape"
	LiteralUnterminated    = "literal.unterminated"
	UnsupportedSyntax      = "limit.unsupported-syntax"
	UnsupportedMetadata    = "limit.unsupported-metadata"

	ProjectInvalidFile       = "project.invalid-file"
	ProjectUnknownField      = "project.unknown-field"
	ProjectInvalidName       = "project.invalid-name"
	ProjectInvalidModuleName = "project.invalid-module-name"
	ProjectInvalidRoot       = "project.invalid-root"
	ProjectDuplicateRoot     = "project.duplicate-root"
	ProjectMissingRoot       = "project.missing-root"
	ProjectEmptyRoot         = "project.empty-root"

	ModuleUnknownModule   = "module.unknown-module"
	ModuleUnknownMember   = "module.unknown-member"
	ModuleUnpublished     = "module.unpublished-member"
	ModuleNotValue        = "module.not-value"
	ModuleDuplicateDecl   = "module.duplicate-declaration"
	ModuleDependencyCycle = "module.dependency-cycle"

	MainInvalidSignature = "main.invalid-signature"
	MainMultipleEntry    = "main.multiple-entry"

	CallUnknownFunction        = "call.unknown-function"
	CallReceiverKind           = "call.receiver-kind"
	MethodReceiverNotOwned     = "method.receiver-not-owned"
	MethodDuplicateDeclaration = "method.duplicate-declaration"
	MemberUnknown              = "member.unknown"
	MemberDuplicateDeclaration = "member.duplicate-declaration"
	NameSelfOutsideMethod      = "name.self-outside-method"
	ConstantNonconstant        = "constant.nonconstant-expression"
	ConstantDependencyCycle    = "constant.dependency-cycle"

	NameDuplicateName     = "name.duplicate-name"
	NameUnknownName       = "name.unknown-name"
	NameReservedName      = "name.reserved-name"
	ParameterDefaultOrder = "parameter.default-order"
	ParameterMissingType  = "parameter.missing-type"

	VariableArity              = "variable.arity"
	VariableTypeDistribution   = "variable.type-distribution"
	VariableMissingInitializer = "variable.missing-initializer"
	VariableUseBeforeAssign    = "variable.use-before-assign"
	VariableUnread             = "variable.unread"
	ConstantReassignment       = "constant.reassignment"
	FunctionMissingReturn      = "function.missing-return"
	CallNotCallable            = "call.not-callable"
	FunctionResultArity        = "function.result-arity"
	TypeMismatch               = "type.mismatch"
	ExpressionArity            = "expression.arity"
	CallMixedArguments         = "call.mixed-arguments"
	CallUnknownArgument        = "call.unknown-argument"
	CallDuplicateArgument      = "call.duplicate-argument"
	CallExtraArgument          = "call.extra-argument"
	CallMissingArgument        = "call.missing-argument"
	ConstantOutOfRange         = "constant.out-of-range"
	ConstantDivisionByZero     = "constant.division-by-zero"
	OperatorInvalidOperands    = "operator.invalid-operands"
	IndexUnsupported           = "index.unsupported"
	ConstantInvalidBoundary    = "constant.invalid-string-boundary"
	ConstantMalformed          = "constant.malformed"
	ConstantMissingKey         = "constant.missing-key"
	ConstantLengthMismatch     = "constant.length-mismatch"
	TypeUnknown                = "type.unknown"
	TypeDependencyCycle        = "type.dependency-cycle"
	TypeContextRequired        = "type.context-required"
	TypeInvalidKey             = "type.invalid-key"
	TypeInvalidField           = "type.invalid-field"
	AssignmentInvalidTarget    = "assignment.invalid-target"
	AssignmentFallibleOperand  = "assignment.fallible-operand"
	NullableRepeated           = "nullable.repeated"
	NullableUnresolved         = "nullable.unresolved"

	AliasFormMismatch    = "alias.form-mismatch"
	AliasInvalidTarget   = "alias.invalid-target"
	AliasDependencyCycle = "alias.dependency-cycle"

	InterpolationUnmatchedBrace    = "interpolation.unmatched-brace"
	InterpolationUnterminated      = "interpolation.unterminated"
	InterpolationInvalidExpression = "interpolation.invalid-expression"
	InterpolationUnsupportedType   = "interpolation.unsupported-type"

	MatchEmpty            = "match.empty"
	MatchMissingCandidate = "match.missing-candidate"
	MatchArmAfterAny      = "match.arm-after-any"
	MatchMissingBody      = "match.missing-body"
	MatchInvalidArm       = "match.invalid-arm"
	MatchDeadArm          = "match.dead-arm"

	RangeInvalidContext = "range.invalid-context"
	ControlMissingLoop  = "control.missing-loop"
	LabelUnknown        = "label.unknown"
	LabelDuplicate      = "label.duplicate"
	LabelNonLoop        = "label.non-loop"

	ErrorImmutable           = "error.immutable"
	ErrorMixedReturn         = "error.mixed-return"
	ErrorResultPosition      = "error.result-position"
	ErrorCatalogConstruction = "error.catalog-construction"

	ServiceBodyNotEmpty  = "service.body-not-empty"
	ServiceSelf          = "service.self"
	ServiceBoundaryType  = "service.boundary-type"
	ContractUndeclared   = "contract.undeclared-error"
	ContractInvalidError = "contract.invalid-error"
	ContractUnraised     = "contract.unraised-error"

	HandleArmAfterAny             = "handle.arm-after-any"
	HandleAnyQualified            = "handle.any-qualified"
	HandleInvalidPattern          = "handle.invalid-pattern"
	HandleMissingBody             = "handle.missing-body"
	HandleIncompatibleReplacement = "handle.incompatible-replacement"
	HandleDeadArm                 = "handle.dead-arm"
	HandleNoOpReturn              = "handle.no-op-return"
	HandleNoOpStore               = "handle.no-op-store"
	ResumeOutsideHandler          = "control.resume-outside-handler"
	LabelUnavailable              = "label.unavailable"
	FutureMisplaced               = "future.misplaced-form"
)

// Pos represents one source position.
type Pos struct {
	Byte   int
	Line   int
	Column int
}

// Span represents a half-open physical byte range with derived display positions.
type Span struct {
	Start Pos
	End   Pos
}

// Record represents one diagnostic.
type Record struct {
	Reason       string
	Severity     string
	Path         string
	FileIndex    int
	Module       string
	CycleModules []string
	Span         Span
	Message      string
	Replacement  string
}

// Sentence puts diagnostic text in the sentence form required for public messages.
func Sentence(message string) string {
	lines := strings.Split(message, "\n")
	lines[0] = sentenceLine(lines[0])
	return strings.Join(lines, "\n")
}

// Code quotes a code token in diagnostic text.
func Code(text string) string {
	return strconv.Quote(text)
}

// sentenceLine puts one line in sentence form, splitting clauses, rewriting words, and capitalizing only in the prose around the code tokens that Code quotes, so that every code token keeps the exact text the source gives it.
func sentenceLine(message string) string {
	var parts []string
	for _, clause := range clauses(message) {
		segs := segments(strings.TrimSpace(clause))
		if len(segs) == 0 {
			continue
		}
		if first := &segs[0]; !first.code && strings.HasPrefix(first.text, "expected ") {
			first.text = "add " + strings.TrimPrefix(first.text, "expected ")
		}
		var out strings.Builder
		for i, seg := range segs {
			if seg.code {
				out.WriteString(seg.text)
				continue
			}
			text := prose.Replace(seg.text)
			if i == 0 {
				r, size := utf8.DecodeRuneInString(text)
				if unicode.IsLower(r) {
					text = string(unicode.ToUpper(r)) + text[size:]
				}
			}
			out.WriteString(text)
		}
		part := out.String()
		last := segs[len(segs)-1]
		if last.code || !strings.ContainsAny(part[len(part)-1:], ".!?") {
			part += "."
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

var prose = strings.NewReplacer(
	"Every ", "Each ",
	"every ", "each ",
	" may not ", " cannot ",
	" cannot stand as ", " cannot be ",
	" can be absent", " can be missing",
	" at least one ", " one or more ",
	" is not valid as ", " cannot be ",
	" must be the final arm", " must be the last arm",
	" has no producer yet", " has no producer",
	" Do not modify ", " Do not change ",
)

// segment is a run of a message that is either prose or one quoted code token.
type segment struct {
	text string
	code bool
}

// segments splits a clause into prose runs and the quoted code tokens that Code writes.
func segments(clause string) []segment {
	var out []segment
	for clause != "" {
		i := strings.IndexByte(clause, '"')
		if i < 0 {
			out = append(out, segment{text: clause})
			break
		}
		if i > 0 {
			out = append(out, segment{text: clause[:i]})
		}
		n := quoted(clause[i:])
		out = append(out, segment{text: clause[i : i+n], code: true})
		clause = clause[i+n:]
	}
	return out
}

// quoted returns the length of the Go-quoted string at the start of text, or the rest of the text when the quote does not close.
func quoted(text string) int {
	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(text)
}

// clauses splits a line at each clause separator outside a code token.
func clauses(line string) []string {
	var out []string
	from := 0
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"':
			i += quoted(line[i:]) - 1
		case strings.HasPrefix(line[i:], "; "):
			out = append(out, line[from:i])
			from = i + 2
			i++
		}
	}
	return append(out, line[from:])
}

func Sort(records []Record) {
	slices.SortStableFunc(records, func(a, b Record) int {
		return cmp.Or(
			cmp.Compare(a.FileIndex, b.FileIndex),
			cmp.Compare(a.Span.Start.Byte, b.Span.Start.Byte),
			cmp.Compare(a.Span.End.Byte, b.Span.End.Byte),
			strings.Compare(a.Reason, b.Reason))
	})
}

type jsonPos struct {
	Byte   int `json:"byte"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

type jsonSpan struct {
	Start jsonPos `json:"start"`
	End   jsonPos `json:"end"`
}

type jsonRecord struct {
	Reason       string   `json:"reason"`
	Severity     string   `json:"severity"`
	Path         string   `json:"path"`
	Module       string   `json:"module,omitempty"`
	CycleModules []string `json:"cycle_modules,omitempty"`
	Span         jsonSpan `json:"span"`
	Message      string   `json:"message"`
	Replacement  string   `json:"replacement,omitempty"`
}

func spanJSON(s Span) jsonSpan {
	return jsonSpan{Start: jsonPos(s.Start), End: jsonPos(s.End)}
}

// WriteJSONL writes each record as one JSON object per line.
func WriteJSONL(w io.Writer, records []Record) error {
	enc := json.NewEncoder(w)
	for _, r := range records {
		j := jsonRecord{
			Reason:       r.Reason,
			Severity:     r.Severity,
			Path:         r.Path,
			Module:       r.Module,
			CycleModules: r.CycleModules,
			Span:         spanJSON(r.Span),
			Message:      r.Message,
			Replacement:  r.Replacement,
		}
		if err := enc.Encode(j); err != nil {
			return err
		}
	}
	return nil
}

// WriteFailureJSON writes one invocation or compiler failure report as a single JSON object line.
func WriteFailureJSON(w io.Writer, message string) error {
	report := struct {
		Severity string `json:"severity"`
		Message  string `json:"message"`
	}{Severity: SeverityFailure, Message: Sentence(message)}
	return json.NewEncoder(w).Encode(report)
}

// WriteHuman writes records in a conventional path.
func WriteHuman(w io.Writer, records []Record) error {
	for _, r := range records {
		msg := r.Message
		if r.Replacement != "" {
			msg = fmt.Sprintf("%s. Use %s.", strings.TrimSuffix(msg, "."), Code(r.Replacement))
		}
		module := ""
		if r.Module != "" {
			module = " in module " + r.Module
		}
		_, err := fmt.Fprintf(w, "%s:%d:%d: %s: %s [%s]%s\n",
			r.Path, r.Span.Start.Line, r.Span.Start.Column, r.Severity, msg, r.Reason, module)
		if err != nil {
			return err
		}
	}
	return nil
}
