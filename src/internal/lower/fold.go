// This file is the phase A fold: a scoped walk over each routine body that rewrites
// derived nodes whose children are already kernel forms, hoisting the statements a
// rewrite needs in front of the statement being lowered.
package lower

import (
	"fmt"

	"ritchie.dev/internal/check"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// lowerer is the state of phase A over one routine, where routines holds the lowered calls of routines, which are the calls that reach no built-in, inline holds the handlers that phase A leaves for a handler written on one statement, and whole holds the values of the declarations and stores, whose elements a collection literal can move into a local.
type lowerer struct {
	p         *check.Program
	module    string
	names     map[string]bool
	scope     *scope
	results   []syntax.Type
	pre       []syntax.Stmt
	protected int
	routines  map[syntax.Expr]bool
	inline    map[*syntax.Handled]bool
	whole     map[syntax.Expr]bool
	diags     []diagnostics.Record
	stopped   bool
}

// scope is one lexical scope of a body: the names declared in it with their types.
type scope struct {
	parent *scope
	types  map[string]syntax.Type
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, types: map[string]syntax.Type{}}
}

func (l *lowerer) push() { l.scope = newScope(l.scope) }
func (l *lowerer) pop()  { l.scope = l.scope.parent }

// declare records a name declared in the current scope.
func (l *lowerer) declare(n string, t syntax.Type) { l.scope.types[n] = t }

// fresh returns a name that no name of the routine uses, from a hint.
func (l *lowerer) fresh(hint string) string { return fresh(l.names, hint) }

// written returns the written form of a canonical type at an origin.
func (l *lowerer) written(t syntax.Type, o syntax.Origin) syntax.Type { return written(t, l.module, o) }

// unsupported reports a form without a rule and stops the lowering of the routine.
func (l *lowerer) unsupported(n syntax.Node) {
	unsupported(&l.diags, n)
	l.stopped = true
}

// hoist places a statement in front of the statement being lowered.
func (l *lowerer) hoist(s syntax.Stmt) { l.pre = append(l.pre, s) }

// sub lowers an expression with its own prelude, which the caller places where the expression is evaluated.
func (l *lowerer) sub(f func() syntax.Expr) ([]syntax.Stmt, syntax.Expr) {
	saved := l.pre
	l.pre = nil
	x := f()
	pre := l.pre
	l.pre = saved
	return pre, x
}

// file lowers the declarations of one file.
func (l *lowerer) file(f *syntax.File) *syntax.File {
	l.module = f.File.Module
	out := &syntax.File{Origin: f.Origin}
	for _, d := range f.Decls {
		if k := l.decl(d); k != nil {
			out.Decls = append(out.Decls, k)
		}
	}
	return out
}

// decl lowers one declaration, where aliases and the constants with a scalar value vanish and the declarations of units not yet replayed are unsupported.
func (l *lowerer) decl(d syntax.Decl) syntax.Decl {
	switch d := d.(type) {
	case *syntax.Func:
		return l.routine(d)
	case *syntax.Struct:
		return &syntax.Struct{Origin: d.Origin, Name: d.Name, Fields: l.fields(d.Fields)}
	case *syntax.Error:
		return &syntax.Error{Origin: d.Origin, Name: d.Name, Fields: l.fields(d.Fields)}
	case *syntax.Enum, *syntax.Service:
		return d
	case *syntax.Const:
		if k := l.moduleConstant(d); k != nil {
			return k
		}
		return nil
	case *syntax.Alias:
		return nil
	case *syntax.Entity, *syntax.Projection, *syntax.Query,
		*syntax.Sum, *syntax.Distinct, *syntax.Contract, *syntax.Conformance:
		l.unsupported(d)
		return nil
	}
	panic(fmt.Sprintf("lower: unexpected declaration %T", d))
}

// fields returns fields without their defaults, which constructions supply.
func (l *lowerer) fields(fs []*syntax.Field) []*syntax.Field {
	out := make([]*syntax.Field, len(fs))
	for i, f := range fs {
		out[i] = &syntax.Field{Origin: f.Origin, Embed: f.Embed, Unique: f.Unique, Type: l.erase(f.Type), Name: f.Name}
	}
	return out
}

// routine lowers the body of a function or method: its declared error set is erased, because the source check alone reads it, and its parameters lose their defaults, which callers supply, except on a service method, whose defaults belong to its published interface.
func (l *lowerer) routine(fn *syntax.Func) *syntax.Func {
	out := &syntax.Func{Origin: fn.Origin, Receiver: l.erase(fn.Receiver), Name: fn.Name, ResultNone: fn.ResultNone, Bounds: fn.Bounds}
	for _, r := range fn.Results {
		out.Results = append(out.Results, l.erase(r))
	}
	published := false
	if e := l.p.Table.Of(fn); e != nil {
		published = e.Routine == resolve.ServiceMethod
	}
	for _, p := range fn.Params {
		param := &syntax.Param{Origin: p.Origin, Type: l.erase(p.Type), Name: p.Name}
		if published && p.Default != nil {
			param.Default = l.p.Default(p)
		}
		out.Params = append(out.Params, param)
	}
	if fn.Body == nil {
		return out
	}
	l.names = names(fn)
	l.stopped = false
	l.scope = newScope(nil)
	l.results = nil
	for _, p := range fn.Params {
		l.declare(p.Name, l.p.LocalType(p))
	}
	for _, r := range fn.Results {
		l.results = append(l.results, l.p.Table.Type(r))
	}
	out.Body = l.block(fn.Body)
	return out
}

// block lowers a block in a scope of its own.
func (l *lowerer) block(b *syntax.Block) *syntax.Block {
	l.push()
	defer l.pop()
	return rebuilt(b, l.stmts(b.Stmts))
}

// stmts lowers a statement list.
func (l *lowerer) stmts(list []syntax.Stmt) []syntax.Stmt {
	var out []syntax.Stmt
	for _, s := range list {
		if l.stopped {
			break
		}
		out = append(out, l.stmt(s)...)
	}
	return out
}

// stmt lowers one statement into the statements that replace it, hoisted statements first.
func (l *lowerer) stmt(s syntax.Stmt) []syntax.Stmt {
	saved := l.pre
	l.pre = nil
	defer func() { l.pre = saved }()
	out := l.form(s)
	return append(l.pre, out...)
}

// form lowers one statement by the rule of its form.
func (l *lowerer) form(s syntax.Stmt) []syntax.Stmt {
	var out []syntax.Stmt
	switch s := s.(type) {
	case *syntax.VarDecl:
		if l.p.Value(s.Targets[0]) == nil {
			out = l.varDecl(s)
		} else {
			out = l.constants(s)
		}
	case *syntax.ConstDecl:
		out = l.constDecl(s)
	case *syntax.Store:
		out = l.store(s)
	case *syntax.CompoundStore:
		out = l.compoundStore(s)
	case *syntax.ExprStmt:
		out = []syntax.Stmt{&syntax.ExprStmt{Origin: s.Origin, X: l.expr(s.X, nil)}}
	case *syntax.For, *syntax.CondFor, *syntax.IterFor:
		out = l.loop(s, nil)
	case *syntax.Break:
		n := *s
		out = []syntax.Stmt{&n}
	case *syntax.Join:
		out = []syntax.Stmt{s}
	case *syntax.Continue:
		n := *s
		out = []syntax.Stmt{&n}
	case *syntax.Return:
		wants := l.results
		if len(s.Values) != len(wants) {
			wants = make([]syntax.Type, len(s.Values))
		}
		out = []syntax.Stmt{&syntax.Return{Origin: s.Origin, Values: l.demanded(s.Values, wants)}}
	case *syntax.Resume:
		out = []syntax.Stmt{&syntax.Resume{Origin: s.Origin, Values: l.values(s.Values, nil)}}
	case *syntax.Block:
		out = []syntax.Stmt{l.block(s)}
	case *syntax.If:
		out = l.ifChain(s)
	case *syntax.Match:
		out = l.match(s)
	case *syntax.Handled:
		out = l.handled(s)
	default:
		panic(fmt.Sprintf("lower: unexpected statement %T", s))
	}
	return out
}

// loop lowers a loop, with its body replaced by the protected body that a loop handler moves onto it when one is given.
func (l *lowerer) loop(s syntax.Stmt, body *syntax.Block) []syntax.Stmt {
	switch s := s.(type) {
	case *syntax.For:
		if body == nil {
			body = s.Body
		}
		return []syntax.Stmt{&syntax.For{Origin: s.Origin, Label: s.Label, Body: l.block(body)}}
	case *syntax.CondFor:
		if body == nil {
			body = s.Body
		}
		return l.condFor(s, body)
	case *syntax.IterFor:
		if body == nil {
			body = s.Body
		}
		return l.iterFor(s, body)
	}
	panic(fmt.Sprintf("lower: unexpected loop %T", s))
}

// body lowers the body of an arm, a bare statement or a block, into one statement.
func (l *lowerer) body(s syntax.Stmt) syntax.Stmt {
	if b, ok := s.(*syntax.Block); ok {
		return l.block(b)
	}
	l.push()
	defer l.pop()
	out := l.stmt(s)
	if len(out) == 1 {
		return out[0]
	}
	return block(s.Where(), out)
}

// values lowers a value list under the types of its targets: one call of the targets' arity, or one value per target.
func (l *lowerer) values(values []syntax.Expr, wants []syntax.Type) []syntax.Expr {
	if len(values) != len(wants) {
		wants = make([]syntax.Type, len(values))
	}
	for _, v := range values {
		l.whole[v] = true
	}
	return l.operands(values, wants)
}

// elements lowers the elements of a collection literal, which a literal that is the whole value of a declaration or a store can move, and which any other literal uses.
func (l *lowerer) elements(x syntax.Expr, xs []syntax.Expr, wants []syntax.Type) []syntax.Expr {
	if l.whole[x] {
		return l.operands(xs, wants)
	}
	return l.demanded(xs, wants)
}

// varDecl lowers a declaration, and a single target whose initializer is a ternary is stored by the arms of the ternary's match.
func (l *lowerer) varDecl(s *syntax.VarDecl) []syntax.Stmt {
	targets := make([]*syntax.VarTarget, len(s.Targets))
	types := make([]syntax.Type, len(s.Targets))
	for i, t := range s.Targets {
		targets[i] = &syntax.VarTarget{Origin: t.Origin, Type: l.erase(t.Type), Name: t.Name}
		types[i] = l.p.LocalType(t)
	}
	if len(s.Values) == 1 && len(s.Targets) == 1 {
		if tern, ok := s.Values[0].(*syntax.Ternary); ok {
			o := s.Origin
			m := l.ternaryMatch(tern, types[0], func(v syntax.Expr) syntax.Stmt { return store(o, name(o, targets[0].Name), v) })
			l.declare(targets[0].Name, types[0])
			return append([]syntax.Stmt{&syntax.VarDecl{Origin: o, Targets: targets}}, m...)
		}
		if c, ok := s.Values[0].(*syntax.Coalesce); ok {
			o := s.Origin
			m := l.coalesceMatch(c, func(v syntax.Expr) syntax.Stmt { return store(o, name(o, targets[0].Name), v) })
			l.declare(targets[0].Name, types[0])
			return []syntax.Stmt{&syntax.VarDecl{Origin: o, Targets: targets}, m}
		}
	}
	values := l.values(s.Values, types)
	for i, t := range targets {
		l.declare(t.Name, types[i])
	}
	return []syntax.Stmt{&syntax.VarDecl{Origin: s.Origin, Targets: targets, Values: values}}
}

// store lowers an assignment, whose index and key operands precede its values as siblings and whose targets are places, where a chained assignment has no rule.
func (l *lowerer) store(s *syntax.Store) []syntax.Stmt {
	if s.Chain {
		l.unsupported(s)
		return nil
	}
	var xs []syntax.Expr
	for _, t := range s.Targets {
		xs = append(xs, indexOperands(t)...)
	}
	n := len(xs)
	wants := make([]syntax.Type, n+len(s.Values))
	if len(s.Values) == len(s.Targets) {
		for i, t := range s.Targets {
			wants[n+i] = l.p.TypeOf(t)
		}
	}
	for _, v := range s.Values {
		l.whole[v] = true
	}
	lowered := l.operands(append(xs, s.Values...), wants)
	indexes := lowered[:n]
	targets := make([]syntax.Expr, len(s.Targets))
	for i, t := range s.Targets {
		targets[i] = established(t, &indexes)
	}
	return []syntax.Stmt{&syntax.Store{Origin: s.Origin, Targets: targets, Values: lowered[n:]}}
}

// expr lowers an expression under the type its position requires, hoisting what its rewrites need.
func (l *lowerer) expr(x syntax.Expr, want syntax.Type) syntax.Expr {
	if v, ok := l.constant(x, want); ok {
		return v
	}
	switch x := x.(type) {
	case *syntax.Lit, *syntax.Self:
		return x
	case *syntax.Name:
		if x.Owner != nil {
			return &syntax.Name{Origin: x.Origin, Module: x.Module, Owner: l.written(l.ownerType(x), x.Origin), Name: x.Name}
		}
		return x
	case *syntax.Prim:
		return &syntax.Prim{Origin: x.Origin, Op: x.Op, Args: l.demanded(x.Args, nil)}
	case *syntax.Format:
		var slots []syntax.Expr
		for _, p := range x.Parts {
			if p.Slot != nil {
				slots = append(slots, p.Slot)
			}
		}
		lowered := l.demanded(slots, nil)
		parts := make([]syntax.FormatPart, len(x.Parts))
		for i, p := range x.Parts {
			parts[i] = p
			if p.Slot != nil {
				parts[i].Slot = lowered[0]
				lowered = lowered[1:]
			}
		}
		return &syntax.Format{Origin: x.Origin, Parts: parts}
	case *syntax.Call:
		return l.call(x)
	case *syntax.MethodCall:
		return l.methodCall(x)
	case *syntax.Closure:
		return l.closure(x)
	case *syntax.Construct:
		return l.construct(x)
	case *syntax.ArrayLit:
		var elem syntax.Type
		if a, ok := l.p.TypeOf(x).(*syntax.Array); ok {
			elem = a.Elem
		}
		wants := make([]syntax.Type, len(x.Elems))
		for i := range wants {
			wants[i] = elem
		}
		return &syntax.ArrayLit{Origin: x.Origin, Elems: l.elements(x, x.Elems, wants)}
	case *syntax.DictLit:
		var key, value syntax.Type
		if d, ok := l.p.TypeOf(x).(*syntax.Dict); ok {
			key, value = d.Key, d.Value
		}
		var xs []syntax.Expr
		var wants []syntax.Type
		for _, e := range x.Entries {
			xs = append(xs, e.Key, e.Value)
			wants = append(wants, key, value)
		}
		lowered := l.elements(x, xs, wants)
		entries := make([]*syntax.DictEntry, len(x.Entries))
		for i, e := range x.Entries {
			entries[i] = &syntax.DictEntry{Origin: e.Origin, Key: lowered[2*i], Value: lowered[2*i+1]}
		}
		return &syntax.DictLit{Origin: x.Origin, Entries: entries}
	case *syntax.FieldRead:
		return &syntax.FieldRead{Origin: x.Origin, X: l.expr(x.X, nil), Name: x.Name}
	case *syntax.IndexRead:
		lowered := l.demanded([]syntax.Expr{x.X, x.Index}, nil)
		return &syntax.IndexRead{Origin: x.Origin, X: lowered[0], Index: lowered[1]}
	case *syntax.Slice:
		parts := []syntax.Expr{x.X}
		for _, b := range []syntax.Expr{x.Lo, x.Hi} {
			if b != nil {
				parts = append(parts, b)
			}
		}
		out := &syntax.Slice{Origin: x.Origin}
		lowered := l.demanded(parts, nil)
		out.X, lowered = lowered[0], lowered[1:]
		if x.Lo != nil {
			out.Lo, lowered = lowered[0], lowered[1:]
		}
		if x.Hi != nil {
			out.Hi = lowered[0]
		}
		return out
	case *syntax.Ternary:
		return l.ternary(x, want)
	case *syntax.Coalesce:
		return l.coalesce(x, want)
	case *syntax.Logical:
		return l.logical(x)
	case *syntax.Chain:
		return l.chain(x)
	case *syntax.ServiceCall:
		return l.serviceCall(x)
	case *syntax.Send:
		if len(x.Arms) == 0 && !l.p.Read(x.X) && !l.p.Reader(x.X) {
			return l.expr(x.X, nil)
		}
		return &syntax.Send{Origin: x.Origin, X: l.expr(x.X, nil), Arms: x.Arms}
	case *syntax.Wait:
		return &syntax.Wait{Origin: x.Origin, X: l.expr(x.X, nil)}
	case *syntax.DotMember, *syntax.Create, *syntax.Lookup, *syntax.QueryCall, *syntax.GetOrCreate:
		l.unsupported(x)
		return x
	}
	panic(fmt.Sprintf("lower: unexpected expression %T", x))
}

// ownerType returns the canonical owner of a type-level member reference.
func (l *lowerer) ownerType(x *syntax.Name) syntax.Type {
	t, _ := l.p.Table.ResolveType(x.Owner, l.module)
	if t == nil {
		return x.Owner
	}
	return t
}

// closure lowers a function literal: every parameter receives its written type, and a body expression with a prelude becomes a block.
func (l *lowerer) closure(x *syntax.Closure) syntax.Expr {
	l.push()
	defer l.pop()
	out := &syntax.Closure{Origin: x.Origin}
	for _, p := range x.Params {
		t := l.erase(p.Type)
		if t == nil {
			t = l.written(l.p.LocalType(p), p.Origin)
		}
		out.Params = append(out.Params, &syntax.Param{Origin: p.Origin, Type: t, Name: p.Name})
		l.declare(p.Name, l.p.LocalType(p))
	}
	ft, _ := l.p.TypeOf(x).(*syntax.FuncType)
	saved := l.results
	l.results = nil
	if ft != nil {
		l.results = ft.Results
	}
	defer func() { l.results = saved }()
	if x.Body != nil {
		out.Body = l.block(x.Body)
		return out
	}
	var want syntax.Type
	if len(l.results) == 1 {
		want = l.results[0]
	}
	pre, e := l.sub(func() syntax.Expr { return l.expr(x.Expr, want) })
	if len(pre) == 0 {
		out.Expr = e
		return out
	}
	out.Body = block(x.Origin, append(pre, &syntax.Return{Origin: x.Expr.Where(), Values: []syntax.Expr{e}}))
	return out
}
