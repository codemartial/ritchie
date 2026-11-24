// This file finds, in a checked source program, the types and built-in calls that the
// backend does not map, by the same predicates that generation uses.
package gen

import (
	"fmt"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// Limits returns a source diagnostic for each type without a Go type and each built-in call without an rlr function in a checked source program, at the first construct that uses it.
func Limits(p *check.Program) []diagnostics.Record {
	e := &emitter{p: p, t: p.Table, decoding: map[*resolve.Entry]bool{}}
	seen := map[string]bool{}
	var out []diagnostics.Record
	typ := func(n syntax.Node, t syntax.Type) {
		if t == nil || seen[syntax.Print(t)] || e.service(t) {
			return
		}
		seen[syntax.Print(t)] = true
		before := len(e.diags)
		e.at = n
		e.goType(t)
		if len(e.diags) > before {
			e.diags = e.diags[:before]
			o := n.Where()
			out = append(out, o.File.Record(diagnostics.UnsupportedSyntax, o.Start, o.End, fmt.Sprintf("the type %s is not supported", syntax.Print(t))))
		}
	}
	row := func(x syntax.Expr) {
		op := p.Operation(x)
		if op == nil {
			return
		}
		if name, _ := e.rowFunction(op); name == "" {
			o := x.Where()
			out = append(out, o.File.Record(diagnostics.UnsupportedSyntax, o.Start, o.End, fmt.Sprintf("%s is not supported for arguments of these types", diagnostics.Code(op.Row.Name))))
		}
	}
	for _, f := range p.Files {
		syntax.Inspect(f, func(n syntax.Node) bool {
			switch n := n.(type) {
			case *syntax.Call:
				row(n)
			case *syntax.MethodCall:
				row(n)
			}
			if x, ok := n.(syntax.Expr); ok {
				typ(n, p.TypeOf(x))
				for _, t := range p.Results(x) {
					typ(n, t)
				}
			}
			if t := p.LocalType(n); t != nil {
				typ(n, t)
			}
			if w, ok := n.(syntax.Type); ok {
				typ(n, p.Table.Type(w))
			}
			return true
		})
	}
	diagnostics.Sort(out)
	return out
}

// service reports whether a type names a service, which a program uses only as the receiver of a remote call and which has no Go value.
func (e *emitter) service(t syntax.Type) bool {
	en := e.entry(t)
	return en != nil && en.Kind == resolve.ServiceKind
}
