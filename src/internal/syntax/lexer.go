// This file is the layout-aware scanner that turns one file into tokens.
package syntax

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/source"
	"ritchie.dev/rlr"
)

type lexer struct {
	file   *source.File
	src    string
	pos    int
	end    int
	tokens []Token
	diags  []diagnostics.Record
	stack  []byte
}

// Lex produces the tokens of a file with the diagnostics of its lexical faults.
func Lex(f *source.File) ([]Token, []diagnostics.Record) {
	return lexRange(f, f.ContentStart(), len(f.Text))
}

// lexRange lexes the half-open byte range of a file as a token sequence of its own.
func lexRange(f *source.File, start, end int) ([]Token, []diagnostics.Record) {
	l := &lexer{file: f, src: f.Text, pos: start, end: end}
	l.run()
	return l.tokens, l.diags
}

func (l *lexer) run() {
	for l.pos < l.end {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '\n' || c == ';':
			l.terminator(l.pos)
			l.pos++
		case c == '\\':
			l.continuation()
		case c == '/' && l.at(1) == '/':
			l.lineComment()
		case c == '/' && l.at(1) == '*':
			l.blockComment()
		case isWordStart(c):
			l.word()
		case isDigit(c):
			l.number()
		case c == '"' || c == '`':
			l.quoted(l.pos, 0)
		case c >= 0x80:
			r, n := utf8.DecodeRuneInString(l.src[l.pos:])
			l.report(diagnostics.NonASCIIToken, l.pos, l.pos+n,
				fmt.Sprintf("the character %q is not permitted outside string literals and comments", r))
			l.emit(Invalid, l.pos, l.pos+n)
			l.pos += n
		default:
			l.symbol()
		}
	}
	l.emit(EOF, l.end, l.end)
}

func (l *lexer) at(offset int) byte {
	if l.pos+offset < l.end {
		return l.src[l.pos+offset]
	}
	return 0
}

func (l *lexer) emit(k Kind, start, end int) {
	l.tokens = append(l.tokens, Token{Kind: k, Start: start, End: end, Text: l.src[start:end]})
}

func (l *lexer) report(reason string, start, end int, message string) {
	l.diags = append(l.diags, l.file.Record(reason, start, end, message))
}

// terminator produces one Newline token for a newline or semicolon at a depth where it is significant.
func (l *lexer) terminator(at int) {
	if n := len(l.stack); n > 0 && l.stack[n-1] != '{' {
		return
	}
	if n := len(l.tokens); n == 0 || l.tokens[n-1].Kind == Newline {
		return
	}
	l.emit(Newline, at, at+1)
}

// continuation drops a backslash that is the last non-layout character of its line and the newline after it.
func (l *lexer) continuation() {
	start := l.pos
	i := l.pos + 1
	for i < l.end && (l.src[i] == ' ' || l.src[i] == '\t' || l.src[i] == '\r') {
		i++
	}
	if i < l.end && l.src[i] != '\n' {
		l.report(diagnostics.StrayContinuation, start, start+1,
			"a line continuation backslash must be the last character of its line")
		l.emit(Invalid, start, start+1)
		l.pos = start + 1
		return
	}
	l.pos = min(i+1, l.end)
}

// LeadingComment returns the span of the text inside the first block comment of a file when only layout and line comments precede it, and false when there is no such closed comment.
func LeadingComment(f *source.File) (start, end int, ok bool) {
	l := &lexer{file: f, src: f.Text, pos: f.ContentStart(), end: len(f.Text)}
	for l.pos < l.end {
		switch c := l.src[l.pos]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.pos++
		case c == '/' && l.at(1) == '/':
			l.lineComment()
		case c == '/' && l.at(1) == '*':
			open := l.pos
			l.blockComment()
			if len(l.diags) > 0 {
				return 0, 0, false
			}
			return open + 2, l.pos - 2, true
		default:
			return 0, 0, false
		}
	}
	return 0, 0, false
}

func (l *lexer) lineComment() {
	for l.pos < l.end && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *lexer) blockComment() {
	start := l.pos
	depth := 0
	for l.pos < l.end {
		switch {
		case l.src[l.pos] == '/' && l.at(1) == '*':
			depth++
			l.pos += 2
		case l.src[l.pos] == '*' && l.at(1) == '/':
			depth--
			l.pos += 2
			if depth == 0 {
				return
			}
		default:
			l.pos++
		}
	}
	l.report(diagnostics.UnclosedBlock, start, start+2, "a block comment is not closed before the end of the file")
}

func isWordStart(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func isWordChar(c byte) bool  { return isWordStart(c) || isDigit(c) }
func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isHex(c byte) bool       { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// digitsAt reports whether n decimal digits start at offset i.
func (l *lexer) digitsAt(i, n int) bool {
	if i+n > l.end {
		return false
	}
	for k := range n {
		if !isDigit(l.src[i+k]) {
			return false
		}
	}
	return true
}

// span consumes characters of the set from pos and returns the end.
func (l *lexer) span(set string) int {
	i := l.pos
	for i < l.end && strings.IndexByte(set, l.src[i]) >= 0 {
		i++
	}
	return i
}

func (l *lexer) word() {
	start := l.pos
	c := l.src[start]
	switch {
	case c == 'd' && l.dateClaim(start+1):
		l.pos = start + 1
		l.pos = l.span("0123456789+-")
		l.temporal(DateLit, start)
		return
	case c == 't' && l.digitsAt(start+1, 2) && l.at(3) == ':' && isDigit(l.at(4)):
		l.pos = start + 1
		for l.pos < l.end && strings.IndexByte("0123456789:.", l.src[l.pos]) >= 0 && !l.methodDot() {
			l.pos++
		}
		l.temporal(TimeLit, start)
		return
	case (c == 'b' || c == 'f') && (l.at(1) == '"' || l.at(1) == '`'):
		l.pos++
		l.quoted(start, c)
		return
	}
	for l.pos < l.end && isWordChar(l.src[l.pos]) {
		l.pos++
	}
	text := l.src[start:l.pos]
	k, keyword := keywordKinds[text]
	if !keyword {
		k = ClassifyBare(text)
	}
	if (k == KwAny || k == TypeName) && l.at(0) == '!' && l.at(1) != '=' {
		l.pos++
		if k == KwAny {
			k = KwAnyError
		} else {
			k = ErrorType
		}
	}
	l.emit(k, start, l.pos)
}

// methodDot reports whether a dot followed by a name start is at pos, which ends a Time literal before a method call on it.
func (l *lexer) methodDot() bool { return l.at(0) == '.' && isWordStart(l.at(1)) }

// dateClaim reports whether an eligible year and the first hyphen of a Date follow offset i.
func (l *lexer) dateClaim(i int) bool {
	if l.digitsAt(i, 4) && i+4 < l.end && l.src[i+4] == '-' {
		return true
	}
	return i < l.end && (l.src[i] == '+' || l.src[i] == '-') && l.digitsAt(i+1, 6) && i+7 < l.end && l.src[i+7] == '-'
}

// dateTimeClaim reports whether a date followed by T starts at offset i, with the year n digits long.
func (l *lexer) dateTimeClaim(i, n int) bool {
	return l.digitsAt(i, n) && i+n < l.end && l.src[i+n] == '-' &&
		l.digitsAt(i+n+1, 2) && i+n+3 < l.end && l.src[i+n+3] == '-' &&
		l.digitsAt(i+n+4, 2) && i+n+6 < l.end && l.src[i+n+6] == 'T'
}

// dateTime consumes a DateTime candidate whose T is at offset t.
func (l *lexer) dateTime(start, t int) {
	l.pos = t + 1
	l.pos = l.span("0123456789:.")
	if l.at(0) == 'Z' {
		l.pos++
	} else if l.at(0) == '+' || l.at(0) == '-' {
		l.pos++
		l.pos = l.span("0123456789:")
	}
	l.temporal(DateTimeLit, start)
}

// temporal validates the temporal candidate from start to pos through the primitive table.
func (l *lexer) temporal(k Kind, start int) {
	text := l.src[start:l.pos]
	var err error
	switch k {
	case DateLit:
		_, err = rlr.ParseDate(text)
	case TimeLit:
		_, err = rlr.ParseTime(text)
	case DateTimeLit:
		_, err = rlr.ParseDateTime(text)
	case DurationLit:
		_, err = rlr.ParseDuration(text)
	}
	if err != nil {
		l.report(diagnostics.LiteralInvalidTemporal, start, l.pos, err.Error())
		l.emit(Invalid, start, l.pos)
		return
	}
	l.emit(k, start, l.pos)
}

func (l *lexer) number() {
	start := l.pos
	if l.dateTimeClaim(start, 4) {
		l.dateTime(start, start+10)
		return
	}
	for l.pos < l.end && isWordChar(l.src[l.pos]) {
		l.pos++
	}
	text := l.src[start:l.pos]
	if isDurationText(text) {
		l.temporal(DurationLit, start)
		return
	}
	prefixed := len(text) > 1 && text[0] == '0' && strings.IndexByte("xob", text[1]) >= 0
	if !prefixed {
		if l.at(0) == '.' && isDigit(l.at(1)) {
			l.pos++
			for l.pos < l.end && isWordChar(l.src[l.pos]) {
				l.pos++
			}
		}
		last := l.src[l.pos-1]
		if (last == 'e' || last == 'E') && (l.at(0) == '+' || l.at(0) == '-') {
			l.pos++
			for l.pos < l.end && isDigit(l.src[l.pos]) {
				l.pos++
			}
		}
		text = l.src[start:l.pos]
	}
	if !prefixed && strings.ContainsAny(text, ".eE") {
		if !isFloatText(text) {
			l.report(diagnostics.LiteralInvalidFloat, start, l.pos, "the float literal "+diagnostics.Code(text)+" is malformed")
			l.emit(Invalid, start, l.pos)
			return
		}
		l.emit(FloatLit, start, l.pos)
		return
	}
	if !isIntText(text) {
		l.report(diagnostics.LiteralInvalidInteger, start, l.pos, "the integer literal "+diagnostics.Code(text)+" is malformed")
		l.emit(Invalid, start, l.pos)
		return
	}
	l.emit(IntLit, start, l.pos)
}

// digitRun reports whether text is a non-empty run of digits of the class with single separators between digits.
func digitRun(text string, digit func(byte) bool) bool {
	if text == "" || text[0] == '_' || text[len(text)-1] == '_' {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] == '_' {
			if text[i-1] == '_' {
				return false
			}
			continue
		}
		if !digit(text[i]) {
			return false
		}
	}
	return true
}

func isIntText(text string) bool {
	if len(text) > 1 && text[0] == '0' {
		switch text[1] {
		case 'x':
			return digitRun(text[2:], isHex)
		case 'o':
			return digitRun(text[2:], func(c byte) bool { return c >= '0' && c <= '7' })
		case 'b':
			return digitRun(text[2:], func(c byte) bool { return c == '0' || c == '1' })
		}
	}
	return digitRun(text, isDigit)
}

func isFloatText(text string) bool {
	mantissa, exponent, hasExponent := strings.Cut(text, "e")
	if !hasExponent {
		mantissa, exponent, hasExponent = strings.Cut(text, "E")
	}
	whole, fraction, hasFraction := strings.Cut(mantissa, ".")
	if !digitRun(whole, isDigit) || hasFraction && !digitRun(fraction, isDigit) {
		return false
	}
	if hasExponent {
		if exponent != "" && (exponent[0] == '+' || exponent[0] == '-') {
			exponent = exponent[1:]
		}
		if !digitRun(exponent, isDigit) {
			return false
		}
	}
	return hasFraction || hasExponent
}

var durationUnits = []string{"d", "h", "m", "s", "ms", "us", "ns"}

// isDurationText reports whether text is one or more adjacent integer and unit segments.
func isDurationText(text string) bool {
	segments := 0
	for text != "" {
		n := 0
		for n < len(text) && (isDigit(text[n]) || text[n] == '_') {
			n++
		}
		if n == 0 || !digitRun(text[:n], isDigit) {
			return false
		}
		text = text[n:]
		unit := ""
		for _, u := range durationUnits {
			if strings.HasPrefix(text, u) && len(u) > len(unit) {
				unit = u
			}
		}
		if unit == "" {
			return false
		}
		text = text[len(unit):]
		segments++
	}
	return segments > 0
}

// quoted scans a string, byte array, or formatted literal whose prefix letter is prefix or zero.
func (l *lexer) quoted(start int, prefix byte) {
	quote := l.src[l.pos]
	raw := quote == '`'
	l.pos++
	kind := StringLit
	switch {
	case prefix == 'f':
		kind = FStringLit
	case prefix == 'b' && raw:
		kind = RawBytesLit
	case prefix == 'b':
		kind = BytesLit
	case raw:
		kind = RawStringLit
	}
	var parts []Part
	var text strings.Builder
	newlineAt := -1
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, Part{Text: text.String()})
			text.Reset()
		}
	}
	for l.pos < l.end {
		c := l.src[l.pos]
		switch {
		case c == quote:
			l.pos++
			if newlineAt >= 0 {
				l.report(diagnostics.LiteralInvalidString, start, l.pos, "a quoted literal cannot contain a raw newline; use \\n or a backtick literal")
				l.emit(Invalid, start, l.pos)
				return
			}
			flush()
			l.emit(kind, start, l.pos)
			if kind == FStringLit {
				l.tokens[len(l.tokens)-1].Parts = parts
			}
			return
		case c == '\n' && !raw:
			if newlineAt < 0 {
				newlineAt = l.pos
			}
			l.pos++
		case c == '\\' && !raw:
			value, n, err := decodeEscape(l.src[l.pos:l.end], prefix == 'b')
			if err != "" {
				l.report(diagnostics.LiteralInvalidEscape, l.pos, l.pos+n, err)
			} else {
				text.Write(value)
			}
			l.pos += n
		case c == '{' && kind == FStringLit:
			if l.at(1) == '{' {
				text.WriteByte('{')
				l.pos += 2
				continue
			}
			flush()
			if !l.slot(start, quote, &parts) {
				return
			}
		case c == '}' && kind == FStringLit:
			if l.at(1) == '}' {
				text.WriteByte('}')
				l.pos += 2
				continue
			}
			l.report(diagnostics.InterpolationUnmatchedBrace, l.pos, l.pos+1, "a closing brace outside a slot must be doubled")
			l.pos++
		default:
			text.WriteByte(c)
			l.pos++
		}
	}
	end := l.end
	if newlineAt >= 0 {
		end = newlineAt + 1
	}
	l.report(diagnostics.LiteralUnterminated, start, end, "the literal is not closed before the end of the file")
	l.emit(Invalid, start, end)
}

// slot scans one interpolation slot from its opening brace and reports whether the literal continues.
func (l *lexer) slot(start int, quote byte, parts *[]Part) bool {
	open := l.pos
	l.pos++
	depth := 1
	for l.pos < l.end {
		c := l.src[l.pos]
		switch {
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				*parts = append(*parts, Part{Slot: true, Start: open + 1, End: l.pos})
				l.pos++
				return true
			}
		case c == quote:
			l.report(diagnostics.InterpolationUnterminated, open, l.pos, "the slot is not closed before the literal ends")
			l.pos++
			l.emit(Invalid, start, l.pos)
			return false
		}
		l.pos++
	}
	l.report(diagnostics.InterpolationUnterminated, open, l.end, "the slot is not closed before the end of the file")
	l.emit(Invalid, start, l.end)
	return false
}

// decodeEscape decodes the escape at the start of s and returns its value, the length of its text, and a fault message.
func decodeEscape(s string, bytes bool) (value []byte, n int, err string) {
	if len(s) < 2 {
		return nil, len(s), "a backslash must begin an escape"
	}
	switch s[1] {
	case 'n':
		return []byte{'\n'}, 2, ""
	case 't':
		return []byte{'\t'}, 2, ""
	case 'r':
		return []byte{'\r'}, 2, ""
	case '\\':
		return []byte{'\\'}, 2, ""
	case '"':
		return []byte{'"'}, 2, ""
	case '0':
		return []byte{0}, 2, ""
	case 'x':
		n = 2
		for n < 4 && n < len(s) && isHex(s[n]) {
			n++
		}
		if !bytes {
			return nil, n, "the escape " + diagnostics.Code(s[:n]) + " is permitted only in a byte array literal"
		}
		if n != 4 {
			return nil, n, "the escape \\x requires exactly two hex digits"
		}
		var b byte
		for _, c := range s[2:4] {
			b = b<<4 | hexValue(byte(c))
		}
		return []byte{b}, 4, ""
	case 'u':
		if len(s) < 3 || s[2] != '{' {
			return nil, 2, "the escape \\u requires braces around the code point"
		}
		n = 3
		for n < len(s) && isHex(s[n]) {
			n++
		}
		digits := s[3:n]
		if n < len(s) && s[n] == '}' {
			n++
		}
		var r rune
		for _, c := range digits {
			r = r<<4 | rune(hexValue(byte(c)))
		}
		if digits == "" || len(digits) > 6 || s[n-1] != '}' || !utf8.ValidRune(r) {
			return nil, n, "the escape " + diagnostics.Code(s[:n]) + " does not name a Unicode scalar"
		}
		return utf8.AppendRune(nil, r), n, ""
	}
	_, size := utf8.DecodeRuneInString(s[1:])
	return nil, 1 + size, "the escape " + diagnostics.Code(s[:1+size]) + " is not defined"
}

func hexValue(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

var symbols = []struct {
	text string
	kind Kind
}{
	{"??=", CoalesceAssign}, {"...", Invalid},
	{"??", CoalesceOp}, {"==", EqEq}, {"!=", NotEq}, {"<=", LessEq}, {">=", GreaterEq},
	{"+=", PlusAssign}, {"-=", MinusAssign}, {"*=", StarAssign}, {"/=", SlashAssign},
	{"%=", PercentAssign}, {"::", ColonColon}, {"=>", FatArrow}, {"->", Arrow}, {":=", Define},
	{"..", Invalid}, {"~>", Invalid}, {"<~", Invalid}, {"<-", Invalid}, {"|>", Invalid},
	{"&&", Invalid}, {"||", Invalid}, {"++", Invalid}, {"--", Invalid},
	{"+", Plus}, {"-", Minus}, {"*", Star}, {"/", Slash}, {"%", Percent}, {"<", Less},
	{">", Greater}, {"=", Assign}, {"?", Question}, {":", Colon}, {"(", LParen}, {")", RParen},
	{"{", LBrace}, {"}", RBrace}, {"[", LBracket}, {"]", RBracket}, {",", Comma}, {".", Dot},
	{"#", Hash}, {"|", Bar}, {"!", Invalid}, {"&", Invalid}, {"~", Invalid},
}

var replacements = map[string]string{"&": "and", "&&": "and", "||": "or", "!": "not", "++": "+= 1", "--": "-= 1"}

func (l *lexer) symbol() {
	start := l.pos
	c := l.src[start]
	switch {
	case c == '$' || c == '@':
		l.sigil(c)
		return
	case c == '%' && l.at(1) != '=' && isWordStart(l.at(1)) && !l.lastEndsOperand():
		l.sigil(c)
		return
	case (c == '+' || c == '-') && l.dateTimeClaim(start+1, 6):
		l.dateTime(start, start+13)
		return
	}
	for _, s := range symbols {
		if !strings.HasPrefix(l.src[start:l.end], s.text) {
			continue
		}
		if s.text == "<-" && isOperandStart(l.at(2)) {
			continue
		}
		l.pos = start + len(s.text)
		if s.kind == Invalid {
			l.rejected(s.text, start)
			return
		}
		l.bracket(c)
		l.emit(s.kind, start, l.pos)
		return
	}
	l.pos++
	l.report(diagnostics.ExcludedForm, start, l.pos, fmt.Sprintf("the character %q is not part of Ritchie", c))
	l.emit(Invalid, start, l.pos)
}

func isOperandStart(c byte) bool {
	return isDigit(c) || isWordStart(c) || strings.IndexByte("([$@\"`", c) >= 0
}

func (l *lexer) lastEndsOperand() bool {
	n := len(l.tokens)
	return n > 0 && l.tokens[n-1].Kind.endsOperand()
}

// rejected reports a rejected or reserved symbol as one invalid token.
func (l *lexer) rejected(text string, start int) {
	if use, excluded := replacements[text]; excluded {
		rec := l.file.Record(diagnostics.ExcludedForm, start, l.pos, "the symbol "+diagnostics.Code(text)+" is not part of Ritchie")
		rec.Replacement = use
		l.diags = append(l.diags, rec)
	} else {
		l.report(diagnostics.ReservedForm, start, l.pos, "the symbol "+diagnostics.Code(text)+" is reserved for future use")
	}
	l.emit(Invalid, start, l.pos)
}

// bracket maintains the stack of open brackets that decides newline significance.
func (l *lexer) bracket(c byte) {
	switch c {
	case '(', '[', '{':
		l.stack = append(l.stack, c)
	case ')', ']', '}':
		var open byte
		switch c {
		case ')':
			open = '('
		case ']':
			open = '['
		case '}':
			open = '{'
		}
		for i, b := range slices.Backward(l.stack) {
			if b == open {
				l.stack = l.stack[:i]
				return
			}
		}
	}
}

func (l *lexer) sigil(c byte) {
	start := l.pos
	l.pos++
	if !isWordStart(l.at(0)) {
		l.report(diagnostics.UnattachedSigil, start, l.pos, fmt.Sprintf("the sigil %q must be attached to a name", c))
		l.emit(Invalid, start, l.pos)
		return
	}
	for l.pos < l.end && isWordChar(l.src[l.pos]) {
		l.pos++
	}
	body := l.src[start+1 : l.pos]
	class := ClassifyBare(body)
	if Keywords[body] {
		class = Invalid
	}
	kind := Invalid
	switch {
	case c == '$' && class == TypeName:
		kind = EntityType
	case c == '@' && class == TypeName:
		kind = ServiceType
	case c == '%' && class == TypeName:
		kind = ProjectionType
	case c == '%' && class == Variable:
		kind = QueryName
	}
	if kind == Invalid {
		l.report(diagnostics.InvalidSigilBody, start, l.pos, fmt.Sprintf("the sigil %q requires a type name body", c))
		l.emit(Invalid, start, l.pos)
		return
	}
	l.emit(kind, start, l.pos)
}
