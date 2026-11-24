// This file parses the signature text of a row into its parameter and result types.
package builtin

import (
	"fmt"
	"strings"

	"ritchie.dev/internal/syntax"
)

// signatureParser reads one signature text.
type signatureParser struct {
	text string
	pos  int
}

// parseSignature fills the parameter and result types of a row from its signature text.
func parseSignature(r *Row) {
	p := &signatureParser{text: r.Signature}
	if i := strings.IndexByte(p.text, '('); i >= 0 && r.Form != Field {
		p.pos = i
		p.expect('(')
		for !p.at(')') {
			t := p.typ()
			if p.has("...") {
				r.Variadic = true
			}
			r.Params = append(r.Params, t)
			if p.skip(); p.pos < len(p.text) && p.text[p.pos] >= 'a' && p.text[p.pos] <= 'z' {
				r.Names = append(r.Names, p.word())
			}
			if !p.has(",") {
				break
			}
		}
		p.expect(')')
	} else {
		p.pos = strings.Index(p.text, " ->")
		if p.pos < 0 {
			p.pos = len(p.text)
		}
	}
	p.skip()
	if p.pos == len(p.text) {
		r.ResultNone = true
		return
	}
	p.expectWord("->")
	if p.at('(') {
		p.expect('(')
		for !p.at(')') {
			r.Results = append(r.Results, p.typ())
			if !p.has(",") {
				break
			}
		}
		p.expect(')')
		return
	}
	r.Results = []syntax.Type{p.typ()}
}

func (p *signatureParser) skip() {
	for p.pos < len(p.text) && p.text[p.pos] == ' ' {
		p.pos++
	}
}

func (p *signatureParser) at(c byte) bool {
	p.skip()
	return p.pos < len(p.text) && p.text[p.pos] == c
}

func (p *signatureParser) has(s string) bool {
	p.skip()
	if strings.HasPrefix(p.text[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *signatureParser) expect(c byte) {
	if !p.at(c) {
		panic(fmt.Sprintf("builtin: expected %q in signature %q", c, p.text))
	}
	p.pos++
}

func (p *signatureParser) expectWord(s string) {
	if !p.has(s) {
		panic(fmt.Sprintf("builtin: expected %q in signature %q", s, p.text))
	}
}

// word reads a run of name characters.
func (p *signatureParser) word() string {
	p.skip()
	start := p.pos
	for p.pos < len(p.text) {
		c := p.text[p.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		panic(fmt.Sprintf("builtin: expected a type in signature %q", p.text))
	}
	return p.text[start:p.pos]
}

// typ parses one type: a letter, a named type, a dictionary, or a function type, with nullable and array suffixes.
func (p *signatureParser) typ() syntax.Type {
	var t syntax.Type
	switch {
	case p.at('['):
		p.expect('[')
		key := p.typ()
		p.expect(':')
		value := p.typ()
		p.expect(']')
		t = &syntax.Dict{Key: key, Value: value}
	case p.at('('):
		p.expect('(')
		var params []syntax.Type
		for !p.at(')') {
			params = append(params, p.typ())
			if !p.has(",") {
				break
			}
		}
		p.expect(')')
		var results []syntax.Type
		if p.has("->") {
			results = []syntax.Type{p.typ()}
		}
		t = &syntax.FuncType{Params: params, Results: results}
	default:
		name := p.word()
		module := ""
		if p.has("::") {
			module, name = name, p.word()
		}
		if p.has("!") {
			name += "!"
		}
		t = &syntax.Named{Module: module, Name: name}
	}
	for {
		switch {
		case p.at('?'):
			p.expect('?')
			t = &syntax.Nullable{Elem: t}
		case p.at('['):
			p.expect('[')
			var length syntax.Expr
			if !p.at(']') {
				w := p.word()
				if w[0] >= '0' && w[0] <= '9' {
					length = &syntax.Lit{Kind: syntax.IntLit, Text: w}
				} else {
					length = &syntax.Name{Name: w}
				}
			}
			p.expect(']')
			t = &syntax.Array{Elem: t, Length: length}
		default:
			return t
		}
	}
}
