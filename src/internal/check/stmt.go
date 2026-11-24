// This file is the statement walk: scopes, the one flow-fact set, labels and loops,
// handler arms, and the typing of every statement kind.
package check

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/diagnostics"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// routine types the body of a declared function or method.
func (c *checker) routine(fn *syntax.Func, e *resolve.Entry, module string) {
	if fn.Body == nil || e == nil {
		return
	}
	t := c.p.Table
	r := &routine{fn: fn, entry: e, module: module, node: Node{Func: fn}}
	c.nodes = append(c.nodes, r.node)
	for _, res := range fn.Results {
		rt := t.Type(res)
		if rt == nil {
			return
		}
		if isError(rt) {
			c.faultAt(res, diagnostics.ErrorResultPosition, "an error type is not a success result")
			return
		}
		r.results = append(r.results, rt)
	}
	r.none = len(r.results) == 0
	if fn.Receiver != nil && e.Level != resolve.TypeLevel {
		switch e.Routine {
		case resolve.ServiceMethod:
			syntax.Inspect(fn.Body, func(n syntax.Node) bool {
				if s, ok := n.(*syntax.Self); ok {
					c.faultAt(s, diagnostics.ServiceSelf, "a service method has no self")
				}
				return true
			})
			return
		default:
			r.receiver = c.resolveType(fn.Receiver, module)
		}
	}
	sc := newScope(nil)
	sc.outer = true
	for _, p := range fn.Params {
		pt := t.Type(p.Type)
		if pt == nil {
			return
		}
		sc.names[p.Name] = &binding{name: p.Name, typ: pt, decl: p, param: true}
		c.p.locals[p] = pt
	}
	c.body(fn.Body, sc, r, newFlow())
}

// body walks a routine body from the given flow facts and checks that a valued routine cannot complete without a return.
func (c *checker) body(b *syntax.Block, sc *scope, r *routine, f flow) {
	r.facts = &f
	for _, s := range b.Stmts {
		f = c.stmt(s, sc, r, f)
	}
	if f.live && !r.none && len(r.results) > 0 {
		switch {
		case r.fn != nil:
			from := r.fn.Start
			if r.fn.Receiver != nil {
				from = r.fn.Receiver.Where().End
			}
			c.reportName(r.fn, from, r.fn.Name, diagnostics.FunctionMissingReturn, fmt.Sprintf("%s returns a value on every path", diagnostics.Code(r.fn.Name)))
		case r.closure != nil:
			c.reportAt(r.closure, diagnostics.FunctionMissingReturn, "the closure returns a value on every path")
		}
	}
}

// stmt walks one statement under the flow facts before it and returns the facts after it.
func (c *checker) stmt(s syntax.Stmt, sc *scope, r *routine, f flow) flow {
	r.facts = &f
	switch s := s.(type) {
	case *syntax.Bad, *syntax.Join:
		return f
	case *syntax.Block:
		return c.block(s, sc, r, f)
	case *syntax.VarDecl:
		return c.varDecl(s, sc, r, f)
	case *syntax.ConstDecl:
		return c.constDecl(s, sc, r, f)
	case *syntax.Store:
		return c.store(s, sc, r, f)
	case *syntax.CompoundStore:
		return c.compoundStore(s, sc, r, f)
	case *syntax.ExprStmt:
		c.results(s.X, sc, r, nil)
		return f
	case *syntax.For:
		return c.forLoop(s, &loop{label: s.Label, node: s}, sc, r, f)
	case *syntax.CondFor:
		return c.condFor(s, nil, sc, r, f)
	case *syntax.IterFor:
		return c.iterFor(s, nil, sc, newScope(sc), r, f)
	case *syntax.Break:
		return c.transfer(s, s.Label, true, r, f)
	case *syntax.Continue:
		return c.transfer(s, s.Label, false, r, f)
	case *syntax.Return:
		c.returnStmt(s, sc, r)
		return f.dead()
	case *syntax.Resume:
		c.resume(s, sc, r)
		return f.dead()
	case *syntax.If:
		return c.ifStmt(s, sc, r, f)
	case *syntax.Match:
		return c.match(s, sc, r, f)
	case *syntax.Handled:
		return c.handled(s, sc, r, f)
	}
	panic(fmt.Sprintf("check: unexpected statement %T", s))
}

// block walks a brace block in a scope of its own.
func (c *checker) block(b *syntax.Block, sc *scope, r *routine, f flow) flow {
	inner := newScope(sc)
	for _, s := range b.Stmts {
		f = c.stmt(s, inner, r, f)
	}
	return f
}

// declare introduces a name in a scope, rejecting a second introduction in the same scope or of a parameter in the outermost scope.
func (c *checker) declare(sc *scope, from int, b *binding) {
	if sc.declared(b.name) {
		c.faultName(b.decl, from, b.name, diagnostics.NameDuplicateName, fmt.Sprintf("%s is declared more than once in this scope", diagnostics.Code(b.name)))
		return
	}
	sc.names[b.name] = b
}

// varDecl types a variable declaration: its written types, its value list, and the names it introduces together.
func (c *checker) varDecl(s *syntax.VarDecl, sc *scope, r *routine, f flow) flow {
	distinct := map[syntax.Type]bool{}
	for _, t := range s.Targets {
		distinct[t.Type] = true
	}
	if len(distinct) != 1 && len(distinct) != len(s.Targets) {
		for i := 1; i < len(s.Targets); i++ {
			if s.Targets[i].Type != s.Targets[i-1].Type && distinct[s.Targets[i].Type] {
				c.faultAt(s.Targets[i], diagnostics.VariableTypeDistribution, "targets write one shared type or a type each")
				break
			}
		}
	}
	types := make([]syntax.Type, len(s.Targets))
	ok := true
	for i, t := range s.Targets {
		if types[i] = c.resolveType(t.Type, r.module); types[i] == nil {
			ok = false
		}
	}
	if len(s.Values) == 0 && len(s.Targets) > 1 {
		c.faultAt(s, diagnostics.VariableMissingInitializer, "a declaration of several names has an initializer")
		ok = false
	}
	if syntax.ClassifyBare(s.Targets[0].Name) == syntax.Constant {
		return c.constTargets(s, types, ok, sc, r, f)
	}
	if ok && len(s.Values) > 0 {
		ok = c.valueList(s.Values, types, sc, r)
	}
	for i, t := range s.Targets {
		f.drop(t.Name, "after the declaration of "+t.Name+" at "+where(t))
		if types[i] == nil {
			c.declare(sc, t.Type.Where().End, &binding{name: t.Name, decl: t})
			continue
		}
		b := c.mustRead(&binding{name: t.Name, typ: types[i], decl: t})
		c.p.locals[t] = types[i]
		c.declare(sc, t.Type.Where().End, b)
		if len(s.Values) > 0 {
			f.assigned[b] = true
		}
	}
	return f
}

// constTargets types a declaration of several local constants, whose value list is one constant call with a result for each target or one constant expression for each, and evaluates it.
func (c *checker) constTargets(s *syntax.VarDecl, types []syntax.Type, ok bool, sc *scope, r *routine, f flow) flow {
	values := make([]*Constant, len(s.Targets))
	if ok && c.valueList(s.Values, types, sc, r) {
		if len(s.Values) == 1 {
			v, _, one := c.required(s.Values[0], nil, r.module)
			results, several := v.form.(tuple)
			ok = one && several && len(results) == len(types)
			for i := range results[:min(len(results), len(types))] {
				values[i] = c.constant(widened(results[i], types[i]), types[i], s.Values[0].Where())
			}
		} else {
			for i, v := range s.Values {
				_, value, one := c.required(v, types[i], r.module)
				values[i] = value
				ok = ok && one
			}
		}
	}
	for i, t := range s.Targets {
		if types[i] == nil {
			continue
		}
		b := &binding{name: t.Name, typ: types[i], decl: t, constant: true}
		if ok {
			b.value = values[i]
			c.p.values[t] = values[i]
		}
		c.p.locals[t] = types[i]
		c.declare(sc, t.Type.Where().End, b)
	}
	return f
}

// valueList types the values of a declaration or store against the target types: one call of the same arity, or one value per target.
func (c *checker) valueList(values []syntax.Expr, types []syntax.Type, sc *scope, r *routine) bool {
	if len(values) == 1 && len(types) > 1 || len(values) == 1 && isCall(values[0]) {
		results, ok := c.results(values[0], sc, r, types)
		if !ok {
			return false
		}
		if len(results) != len(types) {
			if len(types) == 1 && len(results) == 1 {
				return c.assignable(values[0], results[0], types[0])
			}
			if len(results) == 0 {
				c.faultAt(values[0], diagnostics.ExpressionArity, "the call yields no value")
				return false
			}
			c.faultAt(values[0], diagnostics.VariableArity, fmt.Sprintf("%d values are supplied for %d targets", len(results), len(types)))
			return false
		}
		ok = true
		for i, t := range types {
			if len(results) == 1 {
				ok = c.assignable(values[0], results[0], t) && ok
			} else if !Assignable(results[i], t) {
				c.faultAt(values[0], diagnostics.TypeMismatch, fmt.Sprintf("result %d has type %s where %s is required", i+1, show(results[i]), show(t)))
				ok = false
			}
		}
		return ok
	}
	if len(values) != len(types) {
		valid := true
		for _, v := range values {
			valid = c.TypeOf(v, sc, r, nil) != nil && valid
		}
		if valid {
			o := values[0].Where()
			c.fault(o, o.Start, values[len(values)-1].Where().End, diagnostics.VariableArity, fmt.Sprintf("%d values are supplied for %d targets", len(values), len(types)))
		}
		return false
	}
	ok := true
	for i, v := range values {
		if c.assign(v, sc, r, types[i]) == nil {
			ok = false
		}
	}
	return ok
}

// assignable checks a typed single value against the type its position requires.
func (c *checker) assignable(x syntax.Expr, got, want syntax.Type) bool {
	if isNone(got) || !Assignable(got, want) && !c.fixedConstant(x, got, want) {
		c.mismatch(x, got, want)
		return false
	}
	return true
}

func isCall(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Call, *syntax.MethodCall, *syntax.ServiceCall, *syntax.QueryCall, *syntax.Send:
		return true
	case *syntax.DotMember:
		return x.Call
	case *syntax.Wait:
		return sentOf(x) != nil
	}
	return false
}

// constDecl types a local constant and evaluates it.
func (c *checker) constDecl(s *syntax.ConstDecl, sc *scope, r *routine, f flow) flow {
	t := c.resolveType(s.Type, r.module)
	if t == nil {
		return f
	}
	b := &binding{name: s.Name, typ: t, decl: s, constant: true}
	c.p.locals[s] = t
	if c.assign(s.Value, sc, r, t) != nil {
		if _, value, ok := c.required(s.Value, t, r.module); ok {
			b.value = value
			c.p.values[s] = value
		}
	}
	c.declare(sc, s.Type.Where().End, b)
	return f
}

// store types an assignment to one or more targets.
func (c *checker) store(s *syntax.Store, sc *scope, r *routine, f flow) flow {
	if s.Chain {
		c.faultAt(s, diagnostics.UnsupportedSyntax, "chained assignment is not supported")
		return f
	}
	types := make([]syntax.Type, len(s.Targets))
	roots := make([]*binding, len(s.Targets))
	ok := true
	for i, x := range s.Targets {
		types[i], roots[i] = c.target(x, sc, r, false)
		if types[i] == nil {
			ok = false
		}
	}
	if ok {
		ok = c.valueList(s.Values, types, sc, r)
	}
	for i, x := range s.Targets {
		var value syntax.Type
		switch {
		case !ok:
		case len(s.Values) == len(s.Targets):
			value = c.p.types[s.Values[i]]
		case i < len(c.p.results[s.Values[0]]):
			value = c.p.results[s.Values[0]][i]
		}
		if ix, indexes := x.(*syntax.IndexRead); indexes && value != nil {
			c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.indexed("[]=", c.p.types[ix.X], c.p.types[ix.Index], value)}
		}
		c.stored(f, x, value)
		if roots[i] != nil && ok {
			if _, bare := x.(*syntax.Name); bare {
				f.assigned[roots[i]] = true
				f.stores[roots[i]] = true
			}
		}
	}
	return f
}

// stored applies a store to a target: the target's path and every path under it lose their presence, and a tracked path holds a value afterwards when the stored value has a type that is not nullable.
func (c *checker) stored(f flow, x syntax.Expr, value syntax.Type) {
	path := c.pathOf(x)
	if path == "" {
		return
	}
	why := "after the store at " + where(x)
	_, tracked := c.p.types[x].(*syntax.Nullable)
	if tracked && !present(value) {
		why += ", whose value may be absent"
	}
	f.drop(path, why)
	if tracked && present(value) {
		f.add(path)
	}
}

// present reports whether a value of a type always holds a value: it is known and not nullable.
func present(t syntax.Type) bool {
	_, nullable := t.(*syntax.Nullable)
	return t != nil && !nullable && !isNone(t)
}

// target types an assignment target and returns its type with the variable at its root, reporting a constant root, an error field, or a non-mutand.
func (c *checker) target(x syntax.Expr, sc *scope, r *routine, read bool) (syntax.Type, *binding) {
	switch x := x.(type) {
	case *syntax.Name:
		if x.Module != "" || x.Owner != nil {
			c.faultAt(x, diagnostics.AssignmentInvalidTarget, "an assignment target is a variable or self with field and index selectors")
			return nil, nil
		}
		if b := sc.lookup(x.Name); b != nil {
			if b.typ == nil {
				return nil, nil
			}
			if b.constant {
				c.faultAt(x, diagnostics.ConstantReassignment, fmt.Sprintf("the constant %s cannot be reassigned", x.Name))
				return nil, nil
			}
			if read {
				c.read(x, b, r)
			}
			c.p.stored[x] = b
			c.attach(x, b.typ)
			return b.typ, b
		}
		if e := c.p.Table.Scope(r.module, x.Name); e != nil && e.Kind == resolve.ConstantKind {
			c.faultAt(x, diagnostics.ConstantReassignment, fmt.Sprintf("the constant %s cannot be reassigned", x.Name))
			return nil, nil
		}
		c.faultAt(x, diagnostics.NameUnknownName, "unknown name "+x.Name)
		return nil, nil
	case *syntax.Self:
		if r.receiver == nil {
			c.faultAt(x, diagnostics.NameSelfOutsideMethod, "self is in scope only inside a method body")
			return nil, nil
		}
		c.attach(x, r.receiver)
		return r.receiver, nil
	case *syntax.FieldRead:
		recv, root := c.target(x.X, sc, r, true)
		if recv == nil {
			return nil, nil
		}
		recv = c.prefix(x.X, recv, r)
		if isError(recv) {
			c.faultAt(x, diagnostics.ErrorImmutable, "the fields of an error value are immutable")
			return nil, nil
		}
		t := c.field(x, recv, r)
		c.attach(x, t)
		return t, root
	case *syntax.IndexRead:
		recv, root := c.target(x.X, sc, r, true)
		if recv == nil {
			return nil, nil
		}
		recv = c.prefix(x.X, recv, r)
		var t syntax.Type
		switch container := recv.(type) {
		case *syntax.Array:
			if index := c.assign(x.Index, sc, r, intType); index != nil && c.admitsIndex(x.Index, container, sc, r) {
				t = container.Elem
				c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.indexed("[]", container, index)}
			}
		case *syntax.Dict:
			if key := c.assign(x.Index, sc, r, container.Key); key != nil {
				t = container.Value
				c.p.callees[x] = &callee{kind: syntax.BuiltinFunction, op: c.indexed("[]", container, key)}
			}
		default:
			c.faultAt(x, diagnostics.IndexUnsupported, fmt.Sprintf("a value of type %s cannot be indexed", show(recv)))
		}
		c.attach(x, t)
		return t, root
	}
	c.faultAt(x, diagnostics.AssignmentInvalidTarget, "an assignment target is a variable or self with field and index selectors")
	return nil, nil
}

// prefix returns the type of the prefix of a target, narrowed to its value type when the prefix is a nullable path known present.
func (c *checker) prefix(x syntax.Expr, t syntax.Type, r *routine) syntax.Type {
	return c.attach(x, c.narrowed(x, t, r))
}

// compoundStore types a compound assignment through the operator row of its target and value types.
func (c *checker) compoundStore(s *syntax.CompoundStore, sc *scope, r *routine, f flow) flow {
	t, root := c.target(s.Target, sc, r, true)
	if t == nil {
		return f
	}
	if root != nil && !root.param && !f.assigned[root] {
		if n, ok := s.Target.(*syntax.Name); ok {
			c.reportAt(n, diagnostics.VariableUseBeforeAssign, diagnostics.Code(n.Name)+" cannot be read while unassigned")
		}
	}
	if _, bare := s.Target.(*syntax.Name); bare && root != nil {
		f.stores[root] = true
	}
	if s.Op == "??=" {
		n, ok := t.(*syntax.Nullable)
		if !ok {
			c.faultAt(s, diagnostics.TypeMismatch, fmt.Sprintf("the target of ??= is a nullable and not %s", show(t)))
			return f
		}
		v := c.TypeOf(s.Value, sc, r, n.Elem)
		if v != nil && !Same(unwrap(v), n.Elem) {
			c.faultAt(s.Value, diagnostics.TypeMismatch, fmt.Sprintf("the value of ??= has the base type %s and not %s", show(n.Elem), show(v)))
		}
		c.stored(f, s.Target, v)
		return f
	}
	t = c.prefix(s.Target, t, r)
	op := strings.TrimSuffix(s.Op, "=")
	v := c.TypeOf(s.Value, sc, r, t)
	if v == nil {
		return f
	}
	types := []syntax.Type{t, v}
	row, bind, _ := c.selectRow(builtin.Operator, op, types)
	if row == nil || !Assignable(instantiate(row.Results[0], bind, t), t) {
		c.operatorFault(s, s.Op, types, r)
		return f
	}
	c.p.callees[s.Target] = &callee{kind: syntax.BuiltinFunction, op: c.resolved(row, bind, types)}
	if path := c.pathOf(s.Target); path != "" {
		f.mutate(path, "after the store at "+where(s.Target))
	}
	return f
}

// loopBody walks the body of a loop from facts it owns, with the loop on the loop stack, and returns the facts at the end of the body, where the body's scope is the given one when the loop declares variables in it.
func (c *checker) loopBody(l *loop, body *syntax.Block, sc *scope, r *routine, f flow, shared bool) flow {
	r.loops = append(r.loops, l)
	out := f
	if shared {
		for _, s := range body.Stmts {
			out = c.stmt(s, sc, r, out)
		}
	} else {
		out = c.block(body, sc, r, f)
	}
	r.loops = r.loops[:len(r.loops)-1]
	return out
}

// label reports a loop label that an enclosing loop already uses.
func (c *checker) label(l *loop, r *routine) {
	for _, outer := range r.loops {
		if l.label != "" && outer.label == l.label {
			c.faultName(l.node, l.node.Where().Start, l.label, diagnostics.LabelDuplicate, fmt.Sprintf("the label %s is in use by an enclosing loop", l.label))
		}
	}
}

// forLoop walks a loop without a condition, which only its breaks leave.
func (c *checker) forLoop(s *syntax.For, l *loop, sc *scope, r *routine, f flow) flow {
	c.label(l, r)
	c.fixedPoint(l, r, f, func(head flow) flow { return c.loopBody(l, s.Body, sc, r, head, false) })
	return meetAll(l.breaks)
}

// condFor walks a condition loop, where a compile-time condition keeps only its selected path and the condition is typed at the head of each walk of the body.
func (c *checker) condFor(s *syntax.CondFor, l *loop, sc *scope, r *routine, f flow) flow {
	if l == nil {
		l = &loop{label: s.Label, node: s}
	}
	c.label(l, r)
	var no flow
	known, value := false, false
	head := c.fixedPoint(l, r, f, func(head flow) flow {
		r.facts = &head
		t := c.condition(s.Cond, sc, r)
		if _, nullable := t.(*syntax.Nullable); nullable {
			c.faultAt(s.Cond, diagnostics.TypeMismatch, fmt.Sprintf("a loop condition is a Bool and not %s", show(t)))
			t = nil
		}
		yes := head
		no = head.copy()
		if t != nil {
			yes, no = c.narrow(s.Cond, head)
			known, value = c.constBool(s.Cond, sc, r)
		}
		return c.loopBody(l, s.Body, sc, r, yes, false)
	})
	switch {
	case known && value:
		return meetAll(l.breaks)
	case known:
		return head
	}
	return meetAll(append(l.breaks, no.edge(fmt.Sprintf("on the path where %s is false", text(s.Cond)))))
}

// constBool evaluates a condition that is a constant expression, returning whether it is one and its value.
func (c *checker) constBool(x syntax.Expr, sc *scope, r *routine) (bool, bool) {
	v := c.constantOf(x, sc, r)
	lit, ok := v.(*syntax.Lit)
	if !ok {
		return false, false
	}
	return lit.Kind == syntax.KwTrue || lit.Kind == syntax.KwFalse, lit.Kind == syntax.KwTrue
}

// constantOf returns the literal of an expression that is a constant expression with a scalar value, evaluating it once and recording a value that is a collection, or nil.
func (c *checker) constantOf(x syntax.Expr, sc *scope, r *routine) syntax.Expr {
	if v, done := c.p.constant[x]; done {
		return v
	}
	if c.p.collections[x] != nil || !c.constantForm(x, sc) {
		return nil
	}
	e := evaluation{c: c, module: r.module}
	v, ok := e.eval(x)
	c.p.constant[x] = nil
	if !ok {
		if e.blame == nil {
			c.evaluated[x] = evaluated{}
		}
		return nil
	}
	c.evaluated[x] = evaluated{v, true}
	k := c.constant(v, c.p.types[x], x.Where())
	switch {
	case k == nil:
	case k.Collection():
		delete(c.p.constant, x)
		c.p.collections[x] = k
	default:
		c.p.constant[x] = k.Literal()
	}
	return c.p.constant[x]
}

// record records the value of a constant expression: the literal of a scalar value, and the value itself of a collection.
func (c *checker) record(x syntax.Expr, k *Constant) {
	switch {
	case k == nil:
	case k.Collection():
		c.p.collections[x] = k
	default:
		c.p.constant[x] = k.Literal()
	}
}

// unevaluated reports whether the evaluation of an expression was tried and gave no value.
func (c *checker) unevaluated(x syntax.Expr) bool {
	v, done := c.p.constant[x]
	return done && v == nil && c.p.collections[x] == nil
}

// constantForm reports whether an expression is a constant expression by its form: literals, constants, operators over them, and built-in operations that admit constant values.
func (c *checker) constantForm(x syntax.Expr, sc *scope) bool {
	switch x := x.(type) {
	case *syntax.Lit:
		return x.Kind != syntax.KwNone
	case *syntax.Name:
		if c.unevaluated(x) {
			return false
		}
		if x.Owner != nil {
			return c.p.constant[x] != nil
		}
		if b := sc.lookup(x.Name); b != nil {
			return b.constant
		}
		return c.p.constant[x] != nil || c.p.collections[x] != nil
	case *syntax.Prim:
		if c.unevaluated(x) {
			return false
		}
		for _, a := range x.Args {
			if !c.constantForm(a, sc) {
				return false
			}
		}
		return true
	case *syntax.Logical:
		return c.constantForm(x.Left, sc) && c.constantForm(x.Right, sc)
	case *syntax.Chain:
		for _, a := range x.Operands {
			if !c.constantForm(a, sc) {
				return false
			}
		}
		return true
	case *syntax.Format:
		for _, p := range x.Parts {
			if p.Slot != nil && !c.constantForm(p.Slot, sc) {
				return false
			}
		}
		return true
	case *syntax.Call, *syntax.MethodCall:
		k := c.p.callees[x]
		if k == nil || k.op == nil || !k.op.Row.Constant {
			return false
		}
		var args []syntax.Expr
		switch x := x.(type) {
		case *syntax.Call:
			for _, a := range x.Args {
				args = append(args, a.Value)
			}
		case *syntax.MethodCall:
			args = append(args, x.Receiver)
			for _, a := range x.Args {
				args = append(args, a.Value)
			}
		}
		for _, a := range args {
			if !c.constantForm(a, sc) {
				return false
			}
		}
		return true
	case *syntax.Slice:
		return c.constantForm(x.X, sc) && (x.Lo == nil || c.constantForm(x.Lo, sc)) && (x.Hi == nil || c.constantForm(x.Hi, sc))
	case *syntax.IndexRead:
		_, dict := c.p.types[x.X].(*syntax.Dict)
		return !dict && c.constantForm(x.X, sc) && c.constantForm(x.Index, sc)
	}
	return false
}

// iterFor walks an iterator loop over an array, a dictionary, or a range source, declaring its variables in the given inner scope.
func (c *checker) iterFor(s *syntax.IterFor, l *loop, sc, inner *scope, r *routine, f flow) flow {
	if l == nil {
		l = &loop{label: s.Label, node: s}
	}
	var elems []syntax.Type
	isRange := false
	if call, ok := s.Source.(*syntax.Call); ok {
		if callee, ok := call.Callee.(*syntax.Name); ok && callee.Name == "range" && callee.Owner == nil && callee.Module == "" && sc.lookup("range") == nil && c.p.Table.Scope(r.module, "range") == nil {
			isRange = true
			results, ok := c.builtinCall(call, builtin.Lookup(builtin.Function, "range"), c.argValues(call.Args, nil), nil, sc, r, nil)
			if ok {
				elems = results
			}
		}
	}
	if !isRange {
		t := c.TypeOf(s.Source, sc, r, nil)
		switch t := t.(type) {
		case *syntax.Array:
			elems = []syntax.Type{t.Elem}
		case *syntax.Dict:
			elems = []syntax.Type{t.Key, t.Value}
		case nil:
		default:
			c.faultAt(s.Source, diagnostics.TypeMismatch, fmt.Sprintf("a value of type %s is not a loop source", show(t)))
		}
	}
	if elems != nil {
		if len(s.Vars) != len(elems) {
			o := s.Origin
			from := o.Start + len("for ")
			if s.Label != "" {
				from = strings.Index(o.File.Text[o.Start:o.End], "for ") + o.Start + len("for ")
			}
			text := o.File.Text[from:o.End]
			end := from + strings.Index(text, " in ")
			c.fault(o, from, end, diagnostics.VariableArity, fmt.Sprintf("the source yields %d values per step for %d loop variables", len(elems), len(s.Vars)))
		} else {
			for i, v := range s.Vars {
				inner.names[v] = &binding{name: v, typ: elems[i], decl: s, param: true}
			}
		}
	}
	c.p.loopVars[s] = nil
	for _, v := range s.Vars {
		if inner.names[v] == nil {
			inner.names[v] = &binding{name: v, decl: s, param: true}
		}
		c.p.loopVars[s] = append(c.p.loopVars[s], inner.names[v].typ)
		f.drop(v, "after the declaration of "+v+" at "+where(s))
	}
	c.label(l, r)
	vars := maps.Clone(inner.names)
	head := c.fixedPoint(l, r, f, func(head flow) flow {
		inner.names = maps.Clone(vars)
		return c.loopBody(l, s.Body, inner, r, head, true)
	})
	return meetAll(append(l.breaks, head.edge("on the path that ends the loop at "+where(s))))
}

// transfer walks a break or continue, finding its loop among those the arm or routine admits.
func (c *checker) transfer(s syntax.Stmt, label string, isBreak bool, r *routine, f flow) flow {
	var target *loop
	if label != "" {
		for i := len(r.loops) - 1; i >= 0; i-- {
			if r.loops[i].label == label {
				target = r.loops[i]
				break
			}
		}
		if target == nil {
			c.labelFault(s, label, diagnostics.LabelUnknown, fmt.Sprintf("no enclosing loop is labelled %s", label))
			return f.dead()
		}
		if r.arm != nil && target.inArm {
			c.labelFault(s, label, diagnostics.LabelUnavailable, "a label outside the handler arm is not a transfer target")
			return f.dead()
		}
	} else if len(r.loops) > 0 {
		target = r.loops[len(r.loops)-1]
	}
	if target == nil {
		word := "continue"
		if isBreak {
			word = "break"
		}
		o := s.Where()
		c.fault(o, o.Start, o.Start+len(word), diagnostics.ControlMissingLoop, word+" requires an enclosing loop")
		return f.dead()
	}
	if isBreak {
		target.breaks = append(target.breaks, f.edge("on the path through the break at "+where(s)))
	} else {
		target.continues = append(target.continues, f.edge("on the edge back from the continue at "+where(s)))
	}
	return f.dead()
}

// labelFault reports a fault at the label that follows a break or continue keyword.
func (c *checker) labelFault(s syntax.Stmt, label, reason, message string) {
	o := s.Where()
	rest := o.File.Text[o.End:]
	if i := strings.Index(rest, label); i >= 0 && strings.TrimSpace(rest[:i]) == "" {
		c.fault(o, o.End+i, o.End+i+len(label), reason, message)
		return
	}
	c.faultAt(s, reason, message)
}

// returnStmt types the values of a return against the routine's results, where one error-valued expression is a raise.
func (c *checker) returnStmt(s *syntax.Return, sc *scope, r *routine) {
	if len(s.Values) == 1 {
		if !isCall(s.Values[0]) || len(r.results) <= 1 {
			var want syntax.Type
			if len(r.results) == 1 {
				want = r.results[0]
			}
			t := c.TypeOf(s.Values[0], sc, r, want)
			if t == nil {
				return
			}
			if isError(t) {
				return
			}
			if want == nil {
				c.faultAt(s, diagnostics.FunctionResultArity, fmt.Sprintf("the routine returns %d values and not 1", len(r.results)))
				return
			}
			c.assignable(s.Values[0], t, want)
			return
		}
	}
	if len(s.Values) == 0 {
		if len(r.results) > 0 {
			c.faultAt(s, diagnostics.FunctionResultArity, fmt.Sprintf("the routine returns %d values", len(r.results)))
		}
		return
	}
	if len(s.Values) == 1 {
		results, ok := c.results(s.Values[0], sc, r, r.results)
		if ok && len(results) != len(r.results) {
			c.faultAt(s.Values[0], diagnostics.ExpressionArity, fmt.Sprintf("the call yields %d values where the routine returns %d", len(results), len(r.results)))
		}
		return
	}
	errors := 0
	types := make([]syntax.Type, len(s.Values))
	for i, v := range s.Values {
		var want syntax.Type
		if i < len(r.results) {
			want = r.results[i]
		}
		types[i] = c.TypeOf(v, sc, r, want)
		if types[i] != nil && isError(types[i]) {
			errors++
		}
	}
	if errors > 0 {
		c.faultAt(s, diagnostics.ErrorMixedReturn, "a return lists success values or one error value")
		return
	}
	if len(s.Values) != len(r.results) {
		c.faultAt(s, diagnostics.FunctionResultArity, fmt.Sprintf("the routine returns %d values and not %d", len(r.results), len(s.Values)))
		return
	}
	for i, v := range s.Values {
		if types[i] != nil {
			c.assignable(v, types[i], r.results[i])
		}
	}
}

// noResults reports a concise arm that supplies values to a recovered statement without results.
func (c *checker) noResults(arm *syntax.Arm) {
	o := arm.Origin
	from := strings.Index(o.File.Text[o.Start:o.End], "=>") + o.Start
	c.fault(o, from, o.End, diagnostics.HandleIncompatibleReplacement, "the recovered statement has no results to replace")
}

// resume types a resume against the results of the statement its arm recovers.
func (c *checker) resume(s *syntax.Resume, sc *scope, r *routine) {
	if r.arm == nil {
		c.faultAt(s, diagnostics.ResumeOutsideHandler, "resume is valid only inside a handler arm")
		return
	}
	if len(s.Values) == 0 {
		r.arm.noop = true
		return
	}
	if r.arm.compound {
		if len(s.Values) == 1 && isCall(s.Values[0]) {
			c.results(s.Values[0], sc, r, nil)
			return
		}
		for _, v := range s.Values {
			c.armValue(v, sc, r)
		}
		return
	}
	c.replacement(s, s.Values, sc, r)
}

// replacement checks that supplied values match the results of the recovered statement in number and type.
func (c *checker) replacement(at syntax.Node, values []syntax.Expr, sc *scope, r *routine) {
	want := r.arm.results
	if len(values) == 1 && isCall(values[0]) {
		results, ok := c.results(values[0], sc, r, want)
		if !ok {
			return
		}
		if len(results) == 0 {
			r.arm.noop = true
			return
		}
		if len(results) != len(want) {
			c.faultAt(at, diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("the recovered statement has %d results and the replacement supplies %d", len(want), len(results)))
			return
		}
		for i, t := range results {
			if !Assignable(t, want[i]) {
				c.faultAt(values[0], diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("result %d has type %s where %s is required", i+1, show(t), show(want[i])))
			}
		}
		return
	}
	if len(values) != len(want) {
		c.faultAt(at, diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("the recovered statement has %d results and the replacement supplies %d", len(want), len(values)))
		for _, v := range values {
			c.TypeOf(v, sc, r, nil)
		}
		return
	}
	for i, v := range values {
		if t := c.TypeOf(v, sc, r, want[i]); t != nil && (isNone(t) || !Assignable(t, want[i])) {
			c.faultAt(v, diagnostics.HandleIncompatibleReplacement, fmt.Sprintf("a replacement of type %s cannot stand where %s is required", show(t), show(want[i])))
		}
	}
}

// ifStmt walks an if chain, where a compile-time condition keeps only its selected path.
func (c *checker) ifStmt(s *syntax.If, sc *scope, r *routine, f flow) flow {
	cond := c.condition(s.Cond, sc, r)
	yes, no := f.copy(), f.copy()
	known, value := false, false
	if cond != nil {
		yes, no = c.narrow(s.Cond, f)
		known, value = c.constBool(s.Cond, sc, r)
	}
	then := c.block(s.Then, sc, r, yes)
	other := no
	if s.Else != nil {
		other = c.stmt(s.Else, sc, r, no)
	}
	switch {
	case known && value:
		return then
	case known:
		return other
	}
	return meet(then.edge(fmt.Sprintf("on the path where %s is true", text(s.Cond))), other.edge(fmt.Sprintf("on the path where %s is false", text(s.Cond))))
}

// match walks a match statement: a value match, a nullable match, an outcome match on a call, or a match without a subject.
func (c *checker) match(s *syntax.Match, sc *scope, r *routine, f flow) flow {
	var subject syntax.Type
	var outcome []syntax.Type
	isOutcome := false
	if s.Subject == nil {
		subject = boolType
	} else if fallibleForm(s.Subject) && outcomeArms(s.Arms) {
		results, ok := c.results(s.Subject, sc, r, nil)
		isOutcome = true
		if ok {
			outcome = results
			if len(results) == 1 {
				subject = results[0]
			}
		}
	} else {
		subject = c.TypeOf(s.Subject, sc, r, nil)
		if c.p.narrowed[s.Subject] && slices.ContainsFunc(s.Arms, func(a *syntax.Arm) bool { return a.Kind == syntax.TypedArm || a.Kind == syntax.NoneArm }) {
			delete(c.p.narrowed, s.Subject)
			subject = c.attach(s.Subject, nullable(subject))
		}
	}
	if s.Subject != nil && !isOutcome && subject != nil && slices.ContainsFunc(s.Arms, func(a *syntax.Arm) bool { return a.Kind == syntax.CaseArm }) && c.laterEquality(s.Subject, subject) {
		return f
	}
	if c.kernel && s.Subject != nil && !isOutcome && slices.ContainsFunc(s.Arms, func(a *syntax.Arm) bool { return a.Kind == syntax.TypedArm || a.Kind == syntax.NoneArm }) {
		c.faultAt(s, diagnostics.UnsupportedSyntax, "a kernel match has no member arm and no none arm over a nullable subject")
		return f
	}
	var paths []flow
	covered, taken := false, false
	rest := f.copy()
	for _, arm := range s.Arms {
		inner := newScope(sc)
		g := f.copy()
		if s.Subject == nil {
			g = rest.copy()
		}
		g.live = f.live && !taken
		r.facts = &g
		switch arm.Kind {
		case syntax.CaseArm:
			if s.Subject == nil {
				g = c.candidates(s, arm, rest, inner, r)
				always, never := constantArm(arm.Patterns, func(x syntax.Expr) (bool, bool) { return c.constBool(x, inner, r) })
				g.live = f.live && !taken && !never
				covered = covered || always
				taken = taken || always
				r.facts = &g
				break
			}
			for _, p := range arm.Patterns {
				c.candidate(p, s, subject, isOutcome, inner, r)
			}
		case syntax.TypedArm:
			t := c.resolveType(arm.Type, r.module)
			if t != nil {
				switch {
				case isOutcome && isError(t):
				case isOutcome:
					switch {
					case len(outcome) == 1 && Same(t, outcome[0]):
						covered = true
					case len(outcome) != 1 || !presentMember(outcome[0], t):
						c.reportAt(arm.Type, diagnostics.MatchInvalidArm, fmt.Sprintf("the call yields %s and not %s", showAll(outcome), show(t)))
					}
				case subject != nil:
					if n, ok := subject.(*syntax.Nullable); !ok || !Same(t, n.Elem) {
						c.reportAt(arm.Type, diagnostics.MatchInvalidArm, fmt.Sprintf("a member arm selects the value of a nullable subject and %s has no member %s", show(subject), show(t)))
					}
				}
				if arm.Binding != "" {
					inner.names[arm.Binding] = &binding{name: arm.Binding, typ: t, decl: arm, param: true}
					c.p.locals[arm] = t
					g.drop(arm.Binding, "after the declaration of "+arm.Binding+" at "+where(arm))
				}
				if _, ok := subject.(*syntax.Nullable); ok && !isOutcome {
					if path := c.pathOf(s.Subject); path != "" {
						g.add(path)
					}
				}
			}
		case syntax.ResultsArm:
			c.resultsArm(arm, isOutcome, outcome, inner, r)
			covered = true
		case syntax.NoneArm:
			switch {
			case isOutcome && len(outcome) == 1 && nullable(outcome[0]) == outcome[0]:
			case isOutcome && len(outcome) > 0:
				c.reportAt(arm, diagnostics.MatchInvalidArm, "case none selects the success of a call that returns nothing")
			case !isOutcome && subject != nil:
				if _, ok := subject.(*syntax.Nullable); !ok {
					c.reportAt(arm, diagnostics.MatchInvalidArm, fmt.Sprintf("case none selects the absence of a nullable subject and not a %s", show(subject)))
				}
			}
		case syntax.AnyArm:
			covered = true
		case syntax.AnyErrorArm:
			if !isOutcome {
				c.reportAt(arm, diagnostics.MatchInvalidArm, "any! selects the failures of a call subject")
			}
			if arm.Binding != "" {
				inner.names[arm.Binding] = c.mustRead(&binding{name: arm.Binding, typ: errorType, decl: arm, param: true})
				c.p.locals[arm] = errorType
			}
		}
		if arm.Body != nil {
			g = c.stmt(arm.Body, inner, r, g)
		}
		paths = append(paths, g.edge(armEdge(arm)))
	}
	if !covered {
		if s.Subject == nil {
			paths = append(paths, rest.edge("on the path where no candidate of the match at "+where(s)+" holds"))
		} else {
			paths = append(paths, f.edge("on the path where no arm of the match at "+where(s)+" selects"))
		}
	}
	return meetAll(paths)
}

// armEdge describes the edge from the end of a match arm to the end of the match.
func armEdge(arm *syntax.Arm) string {
	if arm.Kind == syntax.CaseArm && len(arm.Patterns) > 0 {
		return fmt.Sprintf("on the path where %s holds", text(arm.Patterns[0]))
	}
	return "on the path through the arm at " + where(arm)
}

// candidates types the candidates of a case arm of a match without a subject, each on the false edge of the candidates before it, and returns the facts on which the arm starts, the meet of the true edges of its candidates, leaving in rest the false edge of its last candidate, on which later arms start.
func (c *checker) candidates(s *syntax.Match, arm *syntax.Arm, rest flow, sc *scope, r *routine) flow {
	yes := rest.dead()
	for _, p := range arm.Patterns {
		r.facts = &rest
		c.candidate(p, s, boolType, false, sc, r)
		y, n := c.narrow(p, rest)
		yes = meet(yes, y)
		clear(rest.present)
		maps.Copy(rest.present, n.present)
	}
	yes.live = rest.live
	return yes
}

// resultsArm binds the targets of a success arm of an outcome match whose subject has several results, each at the type of its result.
func (c *checker) resultsArm(arm *syntax.Arm, isOutcome bool, outcome []syntax.Type, inner *scope, r *routine) {
	switch {
	case !isOutcome:
		c.reportAt(arm, diagnostics.MatchInvalidArm, "a list of bindings selects the success of a call with several results")
	case outcome != nil && len(outcome) != len(arm.Targets):
		c.reportAt(arm, diagnostics.MatchInvalidArm, fmt.Sprintf("the call yields %s and the arm binds %d results", showAll(outcome), len(arm.Targets)))
	}
	for i, target := range arm.Targets {
		t := c.resolveType(target.Type, r.module)
		if t == nil {
			continue
		}
		if isOutcome && len(outcome) == len(arm.Targets) && !Same(t, outcome[i]) {
			c.reportAt(target.Type, diagnostics.MatchInvalidArm, fmt.Sprintf("result %d of the call is %s and not %s", i+1, show(outcome[i]), show(t)))
		}
		inner.names[target.Name] = &binding{name: target.Name, typ: t, decl: target, param: true}
		c.p.locals[target] = t
	}
}

// presentMember reports whether a type is the present member T of a nullable success form T?.
func presentMember(success, t syntax.Type) bool {
	n, ok := success.(*syntax.Nullable)
	return ok && Same(t, n.Elem)
}

// constantArm reads the candidates of an arm of a match without a subject: whether one is the compile-time value true, and whether every one is the compile-time value false.
func constantArm(patterns []syntax.Expr, constBool func(syntax.Expr) (bool, bool)) (always, never bool) {
	never = true
	for _, p := range patterns {
		known, value := constBool(p)
		always = always || known && value
		never = never && known && !value
	}
	return always, never
}

func showAll(types []syntax.Type) string {
	if len(types) == 0 {
		return "nothing"
	}
	var parts []string
	for _, t := range types {
		parts = append(parts, show(t))
	}
	return strings.Join(parts, ", ")
}

// fallibleForm reports whether an expression has a form whose evaluation can fail with an outcome of its own: a call, an operator, index, slice, or formatted string, or the wait for a read at the place of its send.
func fallibleForm(x syntax.Expr) bool {
	switch x.(type) {
	case *syntax.Prim, *syntax.Format, *syntax.IndexRead, *syntax.Slice:
		return true
	case *syntax.Send:
		return false
	}
	return isCall(x)
}

// outcomeArms reports whether an arm list has an arm form that only an outcome match admits.
func outcomeArms(arms []*syntax.Arm) bool {
	for _, a := range arms {
		switch a.Kind {
		case syntax.TypedArm, syntax.NoneArm, syntax.AnyErrorArm, syntax.ResultsArm:
			return true
		}
	}
	return false
}

// candidate types one candidate of a case arm: an equality test in the subject's type, or a Bool predicate.
func (c *checker) candidate(p syntax.Expr, s *syntax.Match, subject syntax.Type, isOutcome bool, sc *scope, r *routine) {
	if subject == nil {
		c.TypeOf(p, sc, r, nil)
		return
	}
	t := c.TypeOf(p, sc, r, subject)
	if t == nil {
		return
	}
	if Assignable(t, subject) && (!isNone(t) || !isOutcome) {
		return
	}
	if Same(t, boolType) && !Same(subject, boolType) && s.Subject != nil {
		return
	}
	c.mismatch(p, t, subject)
}

// handled walks a handled statement and its arms, giving each arm the results of the statement it recovers and the loops it may transfer to.
func (c *checker) handled(s *syntax.Handled, sc *scope, r *routine, f flow) flow {
	pre := f.copy()
	var after flow
	var l *loop
	base := sc
	switch inner := s.Stmt.(type) {
	case *syntax.For:
		l = &loop{label: inner.Label, node: inner}
		after = c.forLoop(inner, l, sc, r, f)
	case *syntax.CondFor:
		l = &loop{label: inner.Label, node: inner}
		after = c.condFor(inner, l, sc, r, f)
	case *syntax.IterFor:
		l = &loop{label: inner.Label, node: inner}
		base = newScope(sc)
		after = c.iterFor(inner, l, sc, base, r, f)
	default:
		after = c.stmt(s.Stmt, sc, r, f)
	}
	if len(s.Arms) == 0 {
		o := s.Origin
		from := o.Start + strings.LastIndex(o.File.Text[o.Start:o.End], "handle")
		c.fault(o, from, o.End, diagnostics.HandleMissingBody, "a handler contains one or more arms")
	}
	results, hasValues := c.statementResults(s.Stmt)
	compound := isCompound(s)
	if !compound && hasValues {
		c.p.recovered[s] = results
	}
	for _, arm := range s.Arms {
		if arm.Kind == syntax.AnyArm && len(arm.Patterns) > 0 {
			c.faultAt(arm, diagnostics.HandleAnyQualified, "any has no qualifier")
		}
	}
	if s.Exhaustive {
		for _, arm := range s.Arms {
			if arm.Kind == syntax.AnyArm {
				c.faultAt(arm, diagnostics.HandleInvalidPattern, "an exhaustive handler has no any arm")
			}
		}
	}
	breaks := 0
	if l != nil {
		breaks = len(l.breaks)
	}
	paths := []flow{}
	stores := c.storedPlaces(s.Stmt)
	start := c.ended(pre, s.Stmt)
	pre = c.noopPath(pre, stores, s.Stmt)
	valued := valuedReturn(s.Stmt)
	for _, arm := range s.Arms {
		inner := newScope(base)
		g := start.copy()
		r.facts = &g
		var t syntax.Type = errorType
		if arm.Kind == syntax.TypedArm {
			t = c.resolveType(arm.Type, r.module)
			if t != nil && !isError(t) {
				c.faultAt(arm.Type, diagnostics.HandleInvalidPattern, fmt.Sprintf("a handler arm selects an error type and not %s", show(t)))
				t = nil
			}
		}
		if arm.Binding != "" && t != nil {
			inner.names[arm.Binding] = c.mustRead(&binding{name: arm.Binding, typ: t, decl: arm, param: true})
			c.p.locals[arm] = t
		}
		ctx := &armContext{results: results, hasValues: hasValues, outer: r.arm, compound: compound}
		savedLoops, savedArm := r.loops, r.arm
		for _, outer := range r.loops {
			outer.inArm = true
		}
		if l != nil {
			r.loops = append(append([]*loop(nil), r.loops...), l)
		}
		r.arm = ctx
		switch {
		case arm.Values != nil && compound:
			for _, v := range arm.Values {
				c.armValue(v, inner, r)
			}
			if len(arm.Values) == 1 && len(c.p.results[arm.Values[0]]) == 0 && isCall(arm.Values[0]) {
				ctx.noop = true
				paths = append(paths, pre)
			} else {
				paths = append(paths, after)
			}
		case arm.Values != nil:
			if !hasValues {
				if len(arm.Values) == 1 && isCall(arm.Values[0]) {
					if results, ok := c.results(arm.Values[0], inner, r, nil); ok && len(results) == 0 {
						ctx.noop = true
					} else if ok {
						c.noResults(arm)
					}
				} else {
					c.noResults(arm)
					for _, v := range arm.Values {
						c.TypeOf(v, inner, r, nil)
					}
				}
			} else {
				c.replacement(armValues(arm), arm.Values, inner, r)
			}
			if ctx.noop {
				paths = append(paths, pre)
			} else {
				paths = append(paths, after)
			}
		case arm.Body != nil:
			for _, v := range stores.vars {
				delete(g.stores, base.lookup(v.name))
			}
			g = c.stmt(arm.Body, inner, r, g)
			if g.live {
				ctx.noop = true
				paths = append(paths, c.armPath(g, stores, base))
			} else if ctx.noop {
				paths = append(paths, pre)
			} else {
				paths = append(paths, after)
			}
		}
		if ctx.noop {
			c.p.noop[arm] = map[string]bool{}
			if arm.Body != nil {
				c.p.noop[arm] = c.armAssigned(g, stores, base)
			}
			if stores.place {
				c.reportAt(arm, diagnostics.HandleNoOpStore, "a no-op arm cannot recover a statement that stores a field, an element, an entry, or a receiver")
			} else if valued {
				c.reportAt(arm, diagnostics.HandleNoOpReturn, "a no-op arm cannot recover a return that supplies values")
			}
		}
		for _, outer := range savedLoops {
			outer.inArm = false
		}
		r.loops, r.arm = savedLoops, savedArm
	}
	if l != nil {
		after = meet(after, meetAll(l.breaks[breaks:]))
	}
	for i, arm := range s.Arms {
		if i < len(paths) {
			paths[i] = paths[i].edge("on the path through the handler arm at " + where(arm))
		}
	}
	return meetAll(append(paths, after.edge("on the path where the handled statement at "+where(s)+" completes")))
}

// armValues returns the span of a concise arm's value list from its arrow, as a node.
func armValues(arm *syntax.Arm) syntax.Node {
	o := arm.Origin
	from := strings.Index(o.File.Text[o.Start:o.End], "=>") + o.Start
	return &syntax.Bad{Origin: syntax.Origin{File: o.File, Start: from, End: o.End, Construct: syntax.ArmNode}}
}

// placeStores records what a recovered statement can store: the variables, and whether it stores a place or a receiver.
type placeStores struct {
	vars  []*binding
	place bool
}

// valuedReturn reports whether a no-op arm can recover a return that supplies values: the protected statement is such a return, or it is a block or a loop that holds such a return whose values hold a call, which a failure of the call lets the arm recover.
func valuedReturn(s syntax.Stmt) bool {
	if ret, ok := s.(*syntax.Return); ok {
		return len(ret.Values) > 0
	}
	found := false
	syntax.Inspect(s, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.Return:
			found = found || len(n.Values) > 0 && slices.ContainsFunc(n.Values, hasCall)
		}
		return !found
	})
	return found
}

// hasCall reports whether an expression holds a call.
func hasCall(x syntax.Expr) bool {
	found := false
	syntax.Inspect(x, func(n syntax.Node) bool {
		if e, ok := n.(syntax.Expr); ok && isCall(e) {
			found = true
		}
		return !found
	})
	return found
}

// storedPlaces finds the variables and places that a statement can store, at any depth.
func (c *checker) storedPlaces(s syntax.Stmt) placeStores {
	var out placeStores
	syntax.Inspect(s, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Store:
			for _, t := range n.Targets {
				c.placeOf(t, &out)
			}
		case *syntax.CompoundStore:
			c.placeOf(n.Target, &out)
		case *syntax.VarDecl:
			for _, t := range n.Targets {
				out.vars = append(out.vars, &binding{name: t.Name, decl: t})
			}
		case *syntax.MethodCall:
			if k := c.p.callees[n]; k != nil && k.kind == syntax.StructMethod && c.isMutand(n.Receiver) {
				out.place = true
			}
		case *syntax.Closure:
			return false
		}
		return true
	})
	return out
}

// placeOf classifies one assignment target as a variable or a place.
func (c *checker) placeOf(x syntax.Expr, out *placeStores) {
	switch x := x.(type) {
	case *syntax.Name:
		out.vars = append(out.vars, &binding{name: x.Name})
	default:
		out.place = true
	}
}

// isMutand reports whether an expression is a variable or self with field and index selectors.
func (c *checker) isMutand(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Name:
		return x.Module == "" && x.Owner == nil
	case *syntax.Self:
		return true
	case *syntax.FieldRead:
		return c.isMutand(x.X)
	case *syntax.IndexRead:
		return c.isMutand(x.X)
	}
	return false
}

// armPath returns the facts on the path out of a brace arm that completes: the facts at the end of the arm, without each variable that the recovered statement stores and the arm does not store on every path through it, because the failure can occur before the protected store.
func (c *checker) armPath(f flow, stores placeStores, sc *scope) flow {
	out := f.copy()
	for _, v := range stores.vars {
		if b := sc.lookup(v.name); b != nil && !out.stores[b] {
			delete(out.assigned, b)
		}
	}
	return out
}

// armAssigned returns the names of the variables that a recovered statement stores and that a no-op arm assigns on every path through it that completes, given the facts at the end of the arm.
func (c *checker) armAssigned(f flow, stores placeStores, sc *scope) map[string]bool {
	out := map[string]bool{}
	if !f.live {
		return out
	}
	for _, v := range stores.vars {
		if b := sc.lookup(v.name); v.decl == nil && b != nil && f.stores[b] {
			out[v.name] = true
		}
	}
	return out
}

// noopPath returns the facts after a no-op arm: those given, with every variable the recovered statement can store removed, and without the paths that the statement can end.
func (c *checker) noopPath(f flow, stores placeStores, s syntax.Stmt) flow {
	out := c.ended(f, s)
	for b := range out.assigned {
		for _, v := range stores.vars {
			if v.name == b.name {
				delete(out.assigned, b)
			}
		}
	}
	return out
}

// ended returns the facts on which every arm of a handler on a statement starts: those given, without every path that a store, a declaration, or a struct method call inside the statement can end, because the failure can occur at any point of the statement.
func (c *checker) ended(f flow, s syntax.Stmt) flow {
	out := f.copy()
	why := func(what string, n syntax.Node) string {
		return fmt.Sprintf("in a handler arm, which can run after the %s at %s", what, where(n))
	}
	syntax.Inspect(s, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Store:
			for _, t := range n.Targets {
				out.drop(c.pathOf(t), why("store", t))
			}
		case *syntax.CompoundStore:
			out.drop(c.pathOf(n.Target), why("store", n.Target))
		case *syntax.VarDecl:
			for _, t := range n.Targets {
				out.drop(t.Name, why("declaration of "+t.Name, t))
			}
		case *syntax.MethodCall:
			if k := c.p.callees[n]; k != nil && k.kind == syntax.StructMethod {
				out.mutate(c.pathOf(n.Receiver), why("call of "+n.Name, n))
			}
		case *syntax.Closure:
			return false
		}
		return true
	})
	return out
}

// statementResults returns the result types of a statement that a handler recovers, and whether the statement has results at all.
func (c *checker) statementResults(s syntax.Stmt) ([]syntax.Type, bool) {
	switch s := s.(type) {
	case *syntax.VarDecl:
		if len(s.Values) == 0 {
			return nil, false
		}
		var out []syntax.Type
		for _, t := range s.Targets {
			out = append(out, c.p.locals[t])
		}
		return out, true
	case *syntax.Store:
		var out []syntax.Type
		for _, t := range s.Targets {
			out = append(out, c.p.types[t])
		}
		return out, true
	case *syntax.CompoundStore:
		return []syntax.Type{c.p.types[s.Target]}, true
	case *syntax.Return:
		if len(s.Values) == 0 {
			return nil, false
		}
		return c.valueTypes(s.Values), true
	case *syntax.Resume:
		if len(s.Values) == 0 {
			return nil, false
		}
		return c.valueTypes(s.Values), true
	case *syntax.Handled:
		return c.statementResults(s.Stmt)
	case *syntax.Match:
		if s.Origin.Construct == syntax.CompoundStoreStmt && len(s.Arms) == 1 {
			if b, ok := s.Arms[0].Body.(*syntax.Block); ok && len(b.Stmts) > 0 {
				return c.statementResults(b.Stmts[len(b.Stmts)-1])
			}
		}
	case *syntax.Block:
		if b := region(s); b != nil && len(b.Stmts) > 0 {
			return c.statementResults(b.Stmts[len(b.Stmts)-1])
		}
	}
	return nil, false
}

// valueTypes returns the types that the values of a return or resume supply, where a call supplies every result it yields.
func (c *checker) valueTypes(values []syntax.Expr) []syntax.Type {
	var out []syntax.Type
	for _, v := range values {
		if rs := c.p.results[v]; isCall(v) && len(rs) > 0 {
			out = append(out, rs...)
			continue
		}
		out = append(out, c.p.types[v])
	}
	return out
}

// region returns the region block of a statement: the statement itself when it is a region, or the only statement of a block that holds one region, which is how phase A protects one recovered statement.
func region(s syntax.Stmt) *syntax.Block {
	b, ok := s.(*syntax.Block)
	if !ok {
		return nil
	}
	if b.Region {
		return b
	}
	if len(b.Stmts) == 1 {
		if inner, ok := b.Stmts[0].(*syntax.Block); ok && inner.Region {
			return inner
		}
	}
	return nil
}

// isCompound reports whether a handler protects a block or a loop body, whose statements it recovers one at a time, and not one statement or one region.
func isCompound(s *syntax.Handled) bool {
	switch s.Stmt.(type) {
	case *syntax.Block:
		return region(s.Stmt) == nil
	case *syntax.For, *syntax.CondFor, *syntax.IterFor:
		return true
	}
	return false
}

// armValue types a value of an arm of a block or loop handler, whose context is the results of every statement the arm can recover, deferring a contextual value until raise inference has found those statements.
func (c *checker) armValue(v syntax.Expr, sc *scope, r *routine) {
	if deferrable(v) {
		c.deferred[v] = deferredValue{sc: sc, r: r}
		return
	}
	c.TypeOf(v, sc, r, nil)
}

// deferrable reports whether a value takes its type from its context alone, as a contextual literal or an array or dictionary literal of them does, which contains no call, so that deferring it adds no edge to the call graph.
func deferrable(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.ArrayLit:
		for _, e := range x.Elems {
			if !deferrable(e) {
				return false
			}
		}
		return true
	case *syntax.DictLit:
		for _, e := range x.Entries {
			if !deferrable(e.Key) || !deferrable(e.Value) {
				return false
			}
		}
		return true
	}
	return contextual(x)
}
