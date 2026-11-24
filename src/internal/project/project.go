// Package project reads Ritchie project metadata and computes deterministic module
// membership over an injected filesystem.
package project

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/source"
	"ritchie.dev/internal/syntax"
)

const Suffix = ".ritchie.toml"

const FileOrdinal = 0

// Span represents a half-open byte range into the project file.
type Span struct {
	Start, End int
}

// Project represents the validated result of reading a project file.
type Project struct {
	Name    string
	Modules []ModuleRoot

	file *source.File
}

// ModuleRoot represents one [modules] entry.
type ModuleRoot struct {
	Name               string
	Root               string
	NameSpan, RootSpan Span
}

// Read validates a project file's content.
func Read(projectPath string, data []byte) (Project, []diagnostics.Record) {
	f, rec := source.New(projectPath, FileOrdinal, data)
	if rec != nil {
		bad := *rec
		bad.Reason = diagnostics.ProjectInvalidFile
		bad.Message = diagnostics.Sentence("the project metadata does not use UTF-8")
		return Project{}, []diagnostics.Record{bad}
	}
	r := &reader{file: f, data: f.Bytes}
	stem := strings.TrimSuffix(filepath.Base(projectPath), Suffix)
	if !isVariableName(stem) {
		r.report(diagnostics.ProjectInvalidName, 0, 0,
			fmt.Sprintf("project name %q is not a VARIABLE identifier", stem))
	}
	r.run()
	if len(r.diags) > 0 {
		return Project{}, r.diags
	}
	return Project{Name: stem, Modules: r.modules, file: f}, nil
}

func (p Project) record(reason string, span Span, message string) diagnostics.Record {
	return p.file.Record(reason, span.Start, span.End, message)
}

// isVariableName reports whether s is an instance of the language's VARIABLE production.
func isVariableName(s string) bool {
	if s == "" || syntax.Keywords[s] {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return syntax.ClassifyBare(s) == syntax.Variable
}

type reader struct {
	file    *source.File
	data    []byte
	pos     int
	diags   []diagnostics.Record
	modules []ModuleRoot

	seenModules bool
	seenKeys    map[string]bool
	rootsByNorm map[string]bool
}

// report records the first diagnostic.
func (r *reader) report(reason string, start, end int, message string) {
	if len(r.diags) == 0 {
		r.diags = append(r.diags, r.file.Record(reason, start, end, message))
	}
}

func (r *reader) run() {
	r.seenKeys = map[string]bool{}
	r.rootsByNorm = map[string]bool{}
	r.pos = r.file.ContentStart()
	for r.pos < len(r.data) && len(r.diags) == 0 {
		le := r.lineEnd()
		r.line(r.pos, le)
		r.pos = le + 1
	}
}

// lineEnd returns the offset of the next newline.
func (r *reader) lineEnd() int {
	for i := r.pos; i < len(r.data); i++ {
		if r.data[i] == '\n' {
			return i
		}
	}
	return len(r.data)
}

// line handles one physical line [ls.
func (r *reader) line(ls, le int) {
	if le > ls && r.data[le-1] == '\r' {
		le--
	}
	i := skipBlank(r.data, ls, le)
	if i == le {
		return
	}
	switch r.data[i] {
	case '#':
		r.comment(i, le)
	case '[':
		r.header(i, le)
	default:
		r.entry(i, le)
	}
}

// comment rejects the control characters other than tab that TOML 1.0.0 forbids inside a comment.
func (r *reader) comment(i, le int) {
	for j := i; j < le; j++ {
		if c := r.data[j]; (c < 0x20 && c != '\t') || c == 0x7F {
			r.report(diagnostics.ProjectInvalidFile, j, j+1,
				"control character in a comment")
			return
		}
	}
}

func (r *reader) trailing(i, le int) {
	i = skipBlank(r.data, i, le)
	if i == le {
		return
	}
	if r.data[i] == '#' {
		r.comment(i, le)
		return
	}
	r.report(diagnostics.ProjectInvalidFile, i, le,
		"the entry has text after its value")
}

func (r *reader) header(i, le int) {
	start := i
	if i+1 < le && r.data[i+1] == '[' {
		r.report(diagnostics.UnsupportedMetadata, start, le,
			"Ritchie does not support an array of tables in project metadata")
		return
	}
	j := skipBlank(r.data, i+1, le)
	ks := j
	for j < le && isBareKeyByte(r.data[j]) {
		j++
	}
	if j == ks {
		r.report(diagnostics.ProjectInvalidFile, start, min(start+1, le),
			"this table header does not follow the project grammar")
		return
	}
	name := string(r.data[ks:j])
	j = skipBlank(r.data, j, le)
	if j < le && r.data[j] == '.' {
		r.report(diagnostics.UnsupportedMetadata, start, le,
			"Ritchie does not support a table header that contains \".\" in project metadata")
		return
	}
	if j == le || r.data[j] != ']' {
		r.report(diagnostics.ProjectInvalidFile, start, min(start+1, le),
			"this table header does not follow the project grammar")
		return
	}
	hdrEnd := j + 1
	switch name {
	case "modules":
		if r.seenModules {
			r.report(diagnostics.ProjectInvalidFile, start, hdrEnd,
				"the project file defines the [modules] table 2 times")
			return
		}
		r.seenModules = true
		r.trailing(hdrEnd, le)
	case "deps":
		r.report(diagnostics.UnsupportedMetadata, start, hdrEnd,
			"Ritchie does not support the [deps] table in project metadata")
	default:
		r.report(diagnostics.ProjectUnknownField, start, hdrEnd,
			fmt.Sprintf("this platform defines no [%s] table", name))
	}
}

// entry handles one key-value line at the top level or inside [modules].
func (r *reader) entry(i, le int) {
	ks := i
	if c := r.data[i]; c == '"' || c == '\'' {
		r.report(diagnostics.UnsupportedMetadata, ks, le,
			"Ritchie does not support a key in quotes in project metadata")
		return
	}
	j := i
	for j < le && isBareKeyByte(r.data[j]) {
		j++
	}
	if j == ks {
		r.report(diagnostics.ProjectInvalidFile, ks, min(ks+1, le),
			"this entry does not follow the project grammar")
		return
	}
	key := string(r.data[ks:j])
	ke := j
	j = skipBlank(r.data, j, le)
	if j < le && r.data[j] == '.' {
		end := j
		for end < le && r.data[end] != '=' {
			end++
		}
		for end > ks && isBlank(r.data[end-1]) {
			end--
		}
		r.report(diagnostics.UnsupportedMetadata, ks, end,
			"Ritchie does not support a key that contains \".\" in project metadata")
		return
	}
	if j == le || r.data[j] != '=' {
		r.report(diagnostics.ProjectInvalidFile, min(j, le-1), min(j+1, le),
			fmt.Sprintf("put %q after the key", "="))
		return
	}
	if !r.seenModules {
		r.report(diagnostics.ProjectUnknownField, ks, ke,
			fmt.Sprintf("this platform defines no field %q at the project file root", key))
		return
	}
	if r.seenKeys[key] {
		r.report(diagnostics.ProjectInvalidFile, ks, ke,
			fmt.Sprintf("the project file defines key %q 2 times", key))
		return
	}
	r.seenKeys[key] = true
	if !isVariableName(key) {
		r.report(diagnostics.ProjectInvalidModuleName, ks, ke,
			fmt.Sprintf("module name %q is not a VARIABLE identifier", key))
		return
	}
	j = skipBlank(r.data, j+1, le)
	if j == le {
		r.report(diagnostics.ProjectInvalidFile, le, le, "add a value")
		return
	}
	vs := j
	value, ve, ok := r.value(vs, le)
	if !ok {
		return
	}
	r.moduleRoot(key, value, Span{ks, ke}, Span{vs, ve})
	r.trailing(ve, le)
}

// value parses the accepted basic-string value form and classifies every other value start.
func (r *reader) value(vs, le int) (value string, ve int, ok bool) {
	switch c := r.data[vs]; c {
	case '"':
		if vs+2 < le && r.data[vs+1] == '"' && r.data[vs+2] == '"' {
			r.report(diagnostics.UnsupportedMetadata, vs, le,
				"Ritchie does not support a string that contains a newline in project metadata")
			return "", 0, false
		}
		return r.basicString(vs, le)
	case '\'':
		if vs+2 < le && r.data[vs+1] == '\'' && r.data[vs+2] == '\'' {
			r.report(diagnostics.UnsupportedMetadata, vs, le,
				"Ritchie does not support a literal string that contains a newline in project metadata")
			return "", 0, false
		}
		end := vs + 1
		for end < le && r.data[end] != '\'' {
			end++
		}
		if end == le {
			r.report(diagnostics.ProjectInvalidFile, vs, le,
				"this literal string has no quote at its end")
			return "", 0, false
		}
		r.report(diagnostics.UnsupportedMetadata, vs, end+1,
			"Ritchie does not support a literal string in project metadata")
		return "", 0, false
	default:
		if (c < 0x20 && c != '\t') || c == 0x7F {
			r.report(diagnostics.ProjectInvalidFile, vs, vs+1,
				"control character in the value")
			return "", 0, false
		}
		ve := r.nonStringEnd(vs, le)
		r.report(diagnostics.ProjectInvalidRoot, vs, ve,
			"a module root must be a basic string")
		return "", 0, false
	}
}

// nonStringEnd returns the end of a non-string value for its span.
func (r *reader) nonStringEnd(vs, le int) int {
	if c := r.data[vs]; c == '[' || c == '{' {
		depth := 0
		for j := vs; j < le; j++ {
			switch r.data[j] {
			case '[', '{':
				depth++
			case ']', '}':
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return le
	}
	j := vs
	for j < le && !isBlank(r.data[j]) && r.data[j] != '#' {
		j++
	}
	return j
}

// basicString decodes a single-line TOML 1.0.0 basic string.
func (r *reader) basicString(vs, le int) (string, int, bool) {
	var b strings.Builder
	i := vs + 1
	for i < le {
		c := r.data[i]
		switch {
		case c == '"':
			return b.String(), i + 1, true
		case c == '\\':
			seq, size, ok := decodeEscape(r.data[i:le])
			if !ok {
				r.report(diagnostics.ProjectInvalidFile, i, min(i+2, le),
					"this basic string contains a sequence that is not permitted")
				return "", 0, false
			}
			b.WriteString(seq)
			i += size
		case (c < 0x20 && c != '\t') || c == 0x7F:
			r.report(diagnostics.ProjectInvalidFile, i, i+1,
				"control character in a basic string")
			return "", 0, false
		default:
			b.WriteByte(c)
			i++
		}
	}
	r.report(diagnostics.ProjectInvalidFile, vs, le,
		"this basic string has no quote at its end")
	return "", 0, false
}

// decodeEscape decodes one backslash escape at the start of data.
func decodeEscape(data []byte) (string, int, bool) {
	if len(data) < 2 {
		return "", 0, false
	}
	switch data[1] {
	case 'b':
		return "\b", 2, true
	case 't':
		return "\t", 2, true
	case 'n':
		return "\n", 2, true
	case 'f':
		return "\f", 2, true
	case 'r':
		return "\r", 2, true
	case '"':
		return `"`, 2, true
	case '\\':
		return `\`, 2, true
	case 'u':
		return decodeUnicodeEscape(data, 4)
	case 'U':
		return decodeUnicodeEscape(data, 8)
	}
	return "", 0, false
}

func decodeUnicodeEscape(data []byte, digits int) (string, int, bool) {
	if len(data) < 2+digits {
		return "", 0, false
	}
	var v rune
	for _, c := range data[2 : 2+digits] {
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			v = v<<4 | rune(c-'A'+10)
		default:
			return "", 0, false
		}
	}
	if !utf8.ValidRune(v) {
		return "", 0, false
	}
	return string(v), 2 + digits, true
}

// moduleRoot validates one [modules] entry's root.
func (r *reader) moduleRoot(key, root string, nameSpan, rootSpan Span) {
	if root == "" {
		r.report(diagnostics.ProjectInvalidRoot, rootSpan.Start, rootSpan.End,
			"a module root must not be empty and must start from the project directory")
		return
	}
	norm := path.Clean(root)
	switch {
	case filepath.IsAbs(root) || strings.HasPrefix(norm, "/"):
		r.report(diagnostics.ProjectInvalidRoot, rootSpan.Start, rootSpan.End,
			fmt.Sprintf("%q must start from the project directory, not the filesystem root", root))
		return
	case norm == ".." || strings.HasPrefix(norm, "../"):
		r.report(diagnostics.ProjectInvalidRoot, rootSpan.Start, rootSpan.End,
			fmt.Sprintf("%q is above the project directory", root))
		return
	case norm != "." && !fs.ValidPath(norm):
		r.report(diagnostics.ProjectInvalidRoot, rootSpan.Start, rootSpan.End,
			fmt.Sprintf("%q does not follow the project path grammar", root))
		return
	}
	if r.rootsByNorm[norm] {
		r.report(diagnostics.ProjectDuplicateRoot, rootSpan.Start, rootSpan.End,
			fmt.Sprintf("2 roots produce path %q after Ritchie cleans the paths", norm))
		return
	}
	r.rootsByNorm[norm] = true
	r.modules = append(r.modules, ModuleRoot{
		Name: key, Root: norm, NameSpan: nameSpan, RootSpan: rootSpan,
	})
}

func isBareKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '-'
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

func skipBlank(data []byte, i, end int) int {
	for i < end && isBlank(data[i]) {
		i++
	}
	return i
}
