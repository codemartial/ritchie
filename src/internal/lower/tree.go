// This file rebuilds expressions and types: copies of literal trees at a new origin,
// expressions with sub-expressions replaced, and the written form of a canonical type.
package lower

import (
	"fmt"

	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// rebuild returns x with every sub-expression that f maps replaced, copying only the path from x to each replacement, and a closure body is rebuilt by g when g is given.
func rebuild(x syntax.Expr, f func(syntax.Expr) (syntax.Expr, bool), g func(*syntax.Closure) *syntax.Closure) syntax.Expr {
	if x == nil {
		return nil
	}
	if y, ok := f(x); ok {
		return y
	}
	sub := func(y syntax.Expr) syntax.Expr { return rebuild(y, f, g) }
	exprs := func(ys []syntax.Expr) ([]syntax.Expr, bool) {
		changed := false
		out := make([]syntax.Expr, len(ys))
		for i, y := range ys {
			out[i] = sub(y)
			changed = changed || out[i] != y
		}
		return out, changed
	}
	args := func(as []*syntax.Arg) ([]*syntax.Arg, bool) {
		changed := false
		out := make([]*syntax.Arg, len(as))
		for i, a := range as {
			v := sub(a.Value)
			if v == a.Value {
				out[i] = a
				continue
			}
			changed = true
			out[i] = &syntax.Arg{Origin: a.Origin, Name: a.Name, Value: v}
		}
		return out, changed
	}
	switch x := x.(type) {
	case *syntax.Lit, *syntax.Name, *syntax.Self:
		return x
	case *syntax.Prim:
		if ys, changed := exprs(x.Args); changed {
			return &syntax.Prim{Origin: x.Origin, Op: x.Op, Args: ys}
		}
		return x
	case *syntax.Format:
		changed := false
		parts := make([]syntax.FormatPart, len(x.Parts))
		for i, p := range x.Parts {
			parts[i] = p
			if p.Slot != nil {
				parts[i].Slot = sub(p.Slot)
				changed = changed || parts[i].Slot != p.Slot
			}
		}
		if changed {
			return &syntax.Format{Origin: x.Origin, Parts: parts}
		}
		return x
	case *syntax.Call:
		callee := sub(x.Callee)
		as, changed := args(x.Args)
		if changed || callee != x.Callee {
			return &syntax.Call{Origin: x.Origin, Callee: callee, Args: as, Kind: x.Kind}
		}
		return x
	case *syntax.MethodCall:
		recv := sub(x.Receiver)
		as, changed := args(x.Args)
		if changed || recv != x.Receiver {
			return &syntax.MethodCall{Origin: x.Origin, Receiver: recv, Name: x.Name, Args: as, Kind: x.Kind}
		}
		return x
	case *syntax.ServiceCall:
		if as, changed := args(x.Args); changed {
			return &syntax.ServiceCall{Origin: x.Origin, Service: x.Service, Name: x.Name, Args: as}
		}
		return x
	case *syntax.Closure:
		if g != nil {
			return g(x)
		}
		return x
	case *syntax.Construct:
		if as, changed := args(x.Args); changed {
			return &syntax.Construct{Origin: x.Origin, Type: x.Type, Args: as}
		}
		return x
	case *syntax.ArrayLit:
		if ys, changed := exprs(x.Elems); changed {
			return &syntax.ArrayLit{Origin: x.Origin, Elems: ys}
		}
		return x
	case *syntax.DictLit:
		changed := false
		entries := make([]*syntax.DictEntry, len(x.Entries))
		for i, e := range x.Entries {
			k, v := sub(e.Key), sub(e.Value)
			if k == e.Key && v == e.Value {
				entries[i] = e
				continue
			}
			changed = true
			entries[i] = &syntax.DictEntry{Origin: e.Origin, Key: k, Value: v}
		}
		if changed {
			return &syntax.DictLit{Origin: x.Origin, Entries: entries}
		}
		return x
	case *syntax.FieldRead:
		if y := sub(x.X); y != x.X {
			return &syntax.FieldRead{Origin: x.Origin, X: y, Name: x.Name}
		}
		return x
	case *syntax.IndexRead:
		y, i := sub(x.X), sub(x.Index)
		if y != x.X || i != x.Index {
			return &syntax.IndexRead{Origin: x.Origin, X: y, Index: i}
		}
		return x
	case *syntax.Slice:
		y, lo, hi := sub(x.X), sub(x.Lo), sub(x.Hi)
		if y != x.X || lo != x.Lo || hi != x.Hi {
			return &syntax.Slice{Origin: x.Origin, X: y, Lo: lo, Hi: hi}
		}
		return x
	case *syntax.Logical:
		l, r := sub(x.Left), sub(x.Right)
		if l != x.Left || r != x.Right {
			return &syntax.Logical{Origin: x.Origin, Op: x.Op, Left: l, Right: r}
		}
		return x
	case *syntax.Send:
		if y := sub(x.X); y != x.X {
			return &syntax.Send{Origin: x.Origin, X: y, Arms: x.Arms}
		}
		return x
	case *syntax.Wait:
		if y := sub(x.X); y != x.X {
			return &syntax.Wait{Origin: x.Origin, X: y}
		}
		return x
	}
	panic(fmt.Sprintf("lower: unexpected expression %T", x))
}

// reorigin returns a deep copy of a literal tree, as the constant evaluator yields it, at the origin of the expression it replaces.
func reorigin(x syntax.Expr, o syntax.Origin) syntax.Expr {
	switch x := x.(type) {
	case *syntax.Lit:
		return &syntax.Lit{Origin: o, Kind: x.Kind, Text: x.Text}
	case *syntax.Name:
		return &syntax.Name{Origin: o, Module: x.Module, Owner: x.Owner, Name: x.Name}
	case *syntax.Prim:
		args := make([]syntax.Expr, len(x.Args))
		for i, a := range x.Args {
			args[i] = reorigin(a, o)
		}
		return &syntax.Prim{Origin: o, Op: x.Op, Args: args}
	case *syntax.ArrayLit:
		elems := make([]syntax.Expr, len(x.Elems))
		for i, e := range x.Elems {
			elems[i] = reorigin(e, o)
		}
		return &syntax.ArrayLit{Origin: o, Elems: elems}
	case *syntax.DictLit:
		entries := make([]*syntax.DictEntry, len(x.Entries))
		for i, e := range x.Entries {
			entries[i] = &syntax.DictEntry{Origin: o, Key: reorigin(e.Key, o), Value: reorigin(e.Value, o)}
		}
		return &syntax.DictLit{Origin: o, Entries: entries}
	case *syntax.Construct:
		args := make([]*syntax.Arg, len(x.Args))
		for i, a := range x.Args {
			args[i] = &syntax.Arg{Origin: o, Name: a.Name, Value: reorigin(a.Value, o)}
		}
		return &syntax.Construct{Origin: o, Type: x.Type, Args: args}
	}
	panic(fmt.Sprintf("lower: unexpected constant value %T", x))
}

// erase returns a written type in which every alias name is replaced by its target and every constant length by its value, as the canonical type writes them, and the type itself when it names neither.
func (l *lowerer) erase(t syntax.Type) syntax.Type {
	if t == nil || !l.indirect(t) {
		return t
	}
	canonical, _ := l.p.Table.ResolveType(t, l.module)
	if canonical == nil {
		return t
	}
	return written(canonical, l.module, t.Where())
}

// indirect reports whether a written type names an alias or a constant length.
func (l *lowerer) indirect(t syntax.Type) bool {
	found := false
	syntax.Inspect(t, func(n syntax.Node) bool {
		if a, ok := n.(*syntax.Array); ok {
			_, named := a.Length.(*syntax.Name)
			found = found || named
		}
		if nt, ok := n.(*syntax.Named); ok {
			var e *resolve.Entry
			if nt.Module == "" {
				e = l.p.Table.Scope(l.module, nt.Name)
			} else {
				e = l.p.Table.Lookup(nt.Module, nt.Name)
			}
			found = found || e != nil && e.Kind == resolve.AliasKind
		}
		return !found
	})
	return found
}

// written returns the form of a canonical type that a kernel program writes in a module: the module's own qualifier is omitted and every node carries the given origin.
func written(t syntax.Type, module string, o syntax.Origin) syntax.Type {
	switch t := t.(type) {
	case nil:
		return nil
	case *syntax.Named:
		args := make([]syntax.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = written(a, module, o)
		}
		m := t.Module
		if m == module {
			m = ""
		}
		return &syntax.Named{Origin: o, Module: m, Name: t.Name, Args: args}
	case *syntax.Array:
		var length syntax.Expr
		if t.Length != nil {
			length = reorigin(t.Length, o)
		}
		return &syntax.Array{Origin: o, Elem: written(t.Elem, module, o), Length: length}
	case *syntax.Dict:
		return &syntax.Dict{Origin: o, Key: written(t.Key, module, o), Value: written(t.Value, module, o)}
	case *syntax.Nullable:
		return &syntax.Nullable{Origin: o, Elem: written(t.Elem, module, o)}
	case *syntax.Future:
		return &syntax.Future{Origin: o, Elem: written(t.Elem, module, o)}
	case *syntax.FuncType:
		ft := &syntax.FuncType{Origin: o}
		for _, p := range t.Params {
			ft.Params = append(ft.Params, written(p, module, o))
		}
		for _, r := range t.Results {
			ft.Results = append(ft.Results, written(r, module, o))
		}
		return ft
	}
	panic(fmt.Sprintf("lower: unexpected type %T", t))
}

// name returns a reference to a local name at an origin.
func name(o syntax.Origin, n string) *syntax.Name {
	return &syntax.Name{Origin: o, Name: n}
}

// decl returns a declaration of one variable without a value.
func decl(o syntax.Origin, t syntax.Type, n string) *syntax.VarDecl {
	return &syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: t, Name: n}}}
}

// store returns an assignment of one value to one target.
func store(o syntax.Origin, target, value syntax.Expr) *syntax.Store {
	return &syntax.Store{Origin: o, Targets: []syntax.Expr{target}, Values: []syntax.Expr{value}}
}

// block returns a block of statements at an origin.
func block(o syntax.Origin, stmts []syntax.Stmt) *syntax.Block {
	return &syntax.Block{Origin: o, Stmts: stmts}
}

// rebuilt returns a block with the statements of b replaced.
func rebuilt(b *syntax.Block, stmts []syntax.Stmt) *syntax.Block {
	return &syntax.Block{Origin: b.Origin, Stmts: stmts}
}

// caseArm returns a case arm with one candidate.
func caseArm(o syntax.Origin, candidate syntax.Expr, body syntax.Stmt) *syntax.Arm {
	return &syntax.Arm{Origin: o, Kind: syntax.CaseArm, Patterns: []syntax.Expr{candidate}, Body: body}
}

// anyArm returns a catch-all arm.
func anyArm(o syntax.Origin, body syntax.Stmt) *syntax.Arm {
	return &syntax.Arm{Origin: o, Kind: syntax.AnyArm, Body: body}
}

// memberArm returns a member arm that binds the value of a nullable.
func memberArm(o syntax.Origin, t syntax.Type, binding string, body syntax.Stmt) *syntax.Arm {
	return &syntax.Arm{Origin: o, Kind: syntax.TypedArm, Type: t, Binding: binding, Body: body}
}

// isLit reports whether an expression is a literal.
func isLit(x syntax.Expr) bool {
	_, ok := x.(*syntax.Lit)
	return ok
}

// isName reports whether an expression is a plain name.
func isName(x syntax.Expr) bool {
	n, ok := x.(*syntax.Name)
	return ok && n.Owner == nil
}

// pathOf returns the text of a narrowable path, a name or self followed by fields, or the empty string.
func pathOf(x syntax.Expr) string {
	switch x := x.(type) {
	case *syntax.Name:
		if x.Module == "" && x.Owner == nil {
			return x.Name
		}
	case *syntax.Self:
		return "self"
	case *syntax.FieldRead:
		if p := pathOf(x.X); p != "" {
			return p + "." + x.Name
		}
	}
	return ""
}

// setBool returns the store of a Boolean keyword into a local.
func setBool(o syntax.Origin, local, value string) syntax.Stmt {
	kind := syntax.KwFalse
	if value == "true" {
		kind = syntax.KwTrue
	}
	return store(o, name(o, local), &syntax.Lit{Origin: o, Kind: kind, Text: value})
}

// named returns a built-in type by name at an origin.
func named(o syntax.Origin, n string) *syntax.Named {
	return &syntax.Named{Origin: o, Name: n}
}
