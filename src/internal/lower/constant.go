// This file is the constants and defaults family of the Rewrites table.
package lower

import (
	"fmt"
	"slices"

	"ritchie.dev/internal/builtin"
	"ritchie.dev/internal/check"
	"ritchie.dev/internal/resolve"
	"ritchie.dev/internal/syntax"
)

// constant substitutes the literal of an expression that is a constant expression with a scalar value, through a declared temporary where no position gives the literal its type, and substitutes nothing for a constant expression whose value is a collection, where the name of a constant stays and a literal or an operation is lowered by its form with each operand under this rule.
func (l *lowerer) constant(x syntax.Expr, want syntax.Type) (syntax.Expr, bool) {
	v := l.p.Constant(x)
	if v == nil {
		return nil, false
	}
	o := x.Where()
	value := l.literal(v, o)
	if t := l.p.TypeOf(x); want == nil && (retyped(t) || needsContext(v) || fixedBytes(t)) {
		tmp := l.fresh("c")
		l.hoist(&syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(t, o), Name: tmp}}, Values: []syntax.Expr{value}})
		l.declare(tmp, t)
		return name(o, tmp), true
	}
	return value, true
}

// initializer lowers the initializer of a constant whose value is a collection to the form of the constant row, or to the literal of the whole value when that form needs a statement in front of the declaration, because the initializer of a constant reads no variable and a module has no place for a statement.
func (l *lowerer) initializer(x syntax.Expr, t syntax.Type, k *check.Constant) syntax.Expr {
	pre, v := l.sub(func() syntax.Expr { return l.expr(x, t) })
	if len(pre) > 0 {
		return l.literal(k.Literal(), x.Where())
	}
	return v
}

// moduleConstant lowers a module constant whose value is a collection to its declaration with its lowered initializer, and returns nil for a constant with a scalar value, whose declaration vanishes.
func (l *lowerer) moduleConstant(d *syntax.Const) *syntax.Const {
	if !l.p.Collection(d.Value) {
		return nil
	}
	l.names = map[string]bool{}
	l.stopped = false
	l.scope = newScope(nil)
	l.results = nil
	v := l.initializer(d.Value, l.p.Table.Type(d.Type), l.p.Value(d))
	return &syntax.Const{Origin: d.Origin, Type: l.erase(d.Type), Name: d.Name, Value: v}
}

// constDecl lowers a local constant whose value is a collection to its declaration with its lowered initializer, and to nothing for a constant with a scalar value.
func (l *lowerer) constDecl(s *syntax.ConstDecl) []syntax.Stmt {
	k := l.p.Value(s)
	if k == nil || !k.Collection() {
		return nil
	}
	t := l.p.LocalType(s)
	v := l.initializer(s.Value, t, k)
	l.declare(s.Name, t)
	return []syntax.Stmt{&syntax.ConstDecl{Origin: s.Origin, Type: l.erase(s.Type), Name: s.Name, Value: v}}
}

// constants lowers a declaration of several local constants to the declaration of those whose value is a collection, each with its lowered initializer, where one constant is the declaration of a single constant.
func (l *lowerer) constants(s *syntax.VarDecl) []syntax.Stmt {
	var targets []*syntax.VarTarget
	var values []syntax.Expr
	for i, t := range s.Targets {
		k := l.p.Value(t)
		if k == nil || !k.Collection() {
			continue
		}
		if len(s.Values) != len(s.Targets) {
			panic(fmt.Sprintf("lower: the constant %s is a collection among the results of one call", t.Name))
		}
		targets = append(targets, &syntax.VarTarget{Origin: t.Origin, Type: l.erase(t.Type), Name: t.Name})
		values = append(values, l.initializer(s.Values[i], l.p.LocalType(t), k))
		l.declare(t.Name, l.p.LocalType(t))
	}
	if len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 {
		return []syntax.Stmt{&syntax.ConstDecl{Origin: s.Origin, Type: targets[0].Type, Name: targets[0].Name, Value: values[0]}}
	}
	return []syntax.Stmt{&syntax.VarDecl{Origin: s.Origin, Targets: targets, Values: values}}
}

// needsContext reports whether an evaluated value is or holds a none or an empty collection literal outside every construction, whose fields give each value inside it a type.
func needsContext(v syntax.Expr) bool {
	found := false
	syntax.Inspect(v, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Lit:
			found = found || n.Kind == syntax.KwNone
		case *syntax.ArrayLit:
			found = found || len(n.Elems) == 0
		case *syntax.DictLit:
			found = found || len(n.Entries) == 0
		}
		_, construct := n.(*syntax.Construct)
		return !found && !construct
	})
	return found
}

// literal returns an evaluated value at an origin, where the owner of a member reference and the type of a construction are written for the module.
func (l *lowerer) literal(v syntax.Expr, o syntax.Origin) syntax.Expr {
	return rebuild(reorigin(v, o), func(x syntax.Expr) (syntax.Expr, bool) {
		switch x := x.(type) {
		case *syntax.Name:
			if x.Owner != nil {
				return &syntax.Name{Origin: o, Owner: l.written(x.Owner, o), Name: x.Name}, true
			}
		case *syntax.Construct:
			var args []*syntax.Arg
			for _, a := range x.Args {
				args = append(args, &syntax.Arg{Origin: o, Name: a.Name, Value: l.literal(a.Value, o)})
			}
			return &syntax.Construct{Origin: o, Type: l.written(x.Type, o), Args: args}, true
		}
		return nil, false
	}, nil)
}

// fieldsOf returns the fields of the struct or error type that a canonical type denotes.
func (l *lowerer) fieldsOf(t syntax.Type) []*syntax.Field {
	if n, ok := t.(*syntax.Named); ok {
		if e := l.p.Table.Lookup(n.Module, n.Name); e != nil && e.Decl != nil {
			return resolve.FieldsOf(e.Decl)
		}
	}
	return nil
}

// slot is one parameter or field as an argument list fills it: its name and the node whose evaluated default the checked program holds, or nil when it has none.
type slot struct {
	name     string
	defaults syntax.Node
}

// paramSlots returns the slots of the parameters of a routine.
func paramSlots(params []*syntax.Param) []slot {
	out := make([]slot, len(params))
	for i, p := range params {
		out[i] = slot{name: p.Name}
		if p.Default != nil {
			out[i].defaults = p
		}
	}
	return out
}

// fieldSlots returns the slots of the fields of a struct or error type.
func fieldSlots(fields []*syntax.Field) []slot {
	out := make([]slot, len(fields))
	for i, f := range fields {
		out[i] = slot{name: f.Name}
		if f.Default != nil {
			out[i].defaults = f
		}
	}
	return out
}

// index returns the slot that each argument fills, by name when the arguments carry names and by position otherwise, or -1 for an argument beyond the slots.
func index(args []*syntax.Arg, slots []slot) []int {
	out := make([]int, len(args))
	for i, a := range args {
		out[i] = -1
		for j, s := range slots {
			if (a.Name == "" && j == i) || (a.Name != "" && s.name == a.Name) {
				out[i] = j
			}
		}
	}
	return out
}

// order places lowered arguments in the order of their slots with the evaluated default of each slot left out in its place.
func (l *lowerer) order(o syntax.Origin, args []*syntax.Arg, sources []syntax.Expr, slots []slot, named bool) []*syntax.Arg {
	at := index(args, slots)
	if !slices.IsSorted(at) && sources != nil {
		values := make([]syntax.Expr, len(args))
		for i, a := range args {
			values[i] = a.Value
		}
		values = l.temporaries(sources, values, isRead)
		moved := make([]*syntax.Arg, len(args))
		for i, a := range args {
			moved[i] = &syntax.Arg{Origin: a.Origin, Name: a.Name, Value: values[i]}
		}
		args = moved
	}
	var out []*syntax.Arg
	for j, s := range slots {
		k := slices.Index(at, j)
		var arg *syntax.Arg
		switch {
		case k >= 0:
			arg = &syntax.Arg{Origin: args[k].Origin, Value: args[k].Value}
		case s.defaults != nil:
			v := l.p.Default(s.defaults)
			if v == nil {
				panic(fmt.Sprintf("lower: the default of %s has no value", s.name))
			}
			arg = &syntax.Arg{Origin: o, Value: l.literal(v, o)}
		default:
			continue
		}
		if named {
			arg.Name = s.name
		}
		out = append(out, arg)
	}
	for k, j := range at {
		if j < 0 {
			out = append(out, args[k])
		}
	}
	return out
}

// rowParams returns the parameters of a built-in by the names its row gives them, or nil when the row names none.
func rowParams(row *builtin.Row) []*syntax.Param {
	var out []*syntax.Param
	for _, n := range row.Names {
		out = append(out, &syntax.Param{Name: n})
	}
	return out
}

// retyped reports whether a type is Decimal or BitSet, whose literal takes another type outside a typed position, or holds one as an element, key, value, or payload at some depth.
func retyped(t syntax.Type) bool {
	switch t := t.(type) {
	case *syntax.Array:
		return retyped(t.Elem)
	case *syntax.Dict:
		return retyped(t.Key) || retyped(t.Value)
	case *syntax.Nullable:
		return retyped(t.Elem)
	}
	n, ok := t.(*syntax.Named)
	return ok && n.Module == "" && (n.Name == "Decimal" || n.Name == "BitSet")
}

// fixedBytes reports whether an array type has fixed byte arrays as its elements at some depth, or is one.
func fixedBytes(t syntax.Type) bool {
	a, ok := t.(*syntax.Array)
	if !ok {
		return false
	}
	if n, ok := a.Elem.(*syntax.Named); ok && n.Module == "" && n.Name == "Byte" {
		return a.Length != nil
	}
	return fixedBytes(a.Elem)
}

// call lowers a call, where the caller supplies the evaluated default of every parameter it leaves out and a callee that is a function value is the first sibling of the arguments.
func (l *lowerer) call(x *syntax.Call) syntax.Expr {
	var params []*syntax.Param
	var types []syntax.Type
	var leading []syntax.Expr
	if k := l.p.CalleeOf(x); k != nil && k.Func() != nil {
		params = k.Func().Params
		for _, p := range params {
			types = append(types, l.p.Table.Type(p.Type))
		}
	} else if k != nil && k.Row() != nil {
		params = rowParams(k.Row())
	} else if ft, ok := l.p.TypeOf(x.Callee).(*syntax.FuncType); ok {
		types = ft.Params
		leading = []syntax.Expr{x.Callee}
	}
	out := &syntax.Call{Origin: x.Origin, Kind: x.Kind}
	if leading == nil {
		out.Callee = l.expr(x.Callee, nil)
		out.Args = l.args(x.Origin, x.Args, nil, params, types)
	} else {
		lowered := l.args(x.Origin, x.Args, leading, params, types)
		out.Callee, out.Args = lowered[0].Value, lowered[1:]
	}
	l.routines[out] = l.p.Operation(x) == nil
	return out
}

// methodCall lowers a method call: the receiver of a method that can change it is a place, whose index and key operands are the first siblings of the arguments, and any other receiver is a value and the first sibling, where a receiver that such a method reads from a constant is stored in a temporary.
func (l *lowerer) methodCall(x *syntax.MethodCall) syntax.Expr {
	var params []*syntax.Param
	var types []syntax.Type
	k := l.p.CalleeOf(x)
	if k != nil && k.Func() != nil {
		params = k.Func().Params
		for _, p := range params {
			types = append(types, l.p.Table.Type(p.Type))
		}
	} else if k != nil && k.Row() != nil {
		params = rowParams(k.Row())
	}
	out := &syntax.MethodCall{Origin: x.Origin, Name: x.Name, Kind: x.Kind}
	l.routines[out] = l.p.Operation(x) == nil
	if l.p.ChangesReceiver(x) && l.isPlace(x.Receiver) {
		indexes := indexOperands(x.Receiver)
		lowered := l.args(x.Origin, x.Args, indexes, params, types)
		values := make([]syntax.Expr, len(indexes))
		for i := range indexes {
			values[i] = lowered[i].Value
		}
		out.Receiver, out.Args = established(x.Receiver, &values), lowered[len(indexes):]
		return out
	}
	recv := x.Receiver
	if l.p.ChangesReceiver(x) && l.p.ReadsConstant(recv) {
		recv = l.temporary(recv, l.expr(recv, nil))
	}
	lowered := l.args(x.Origin, x.Args, []syntax.Expr{recv}, params, types)
	out.Receiver, out.Args = lowered[0].Value, lowered[1:]
	return out
}

// serviceCall lowers a call of a service method, where the caller supplies the evaluated default of every parameter it leaves out.
func (l *lowerer) serviceCall(x *syntax.ServiceCall) syntax.Expr {
	var params []*syntax.Param
	var types []syntax.Type
	if k := l.p.CalleeOf(x); k != nil && k.Func() != nil {
		params = k.Func().Params
		for _, p := range params {
			types = append(types, l.p.Table.Type(p.Type))
		}
	}
	out := &syntax.ServiceCall{Origin: x.Origin, Service: l.erase(x.Service), Name: x.Name, Args: l.args(x.Origin, x.Args, nil, params, types)}
	l.routines[out] = true
	return out
}

// args lowers an argument list as the siblings that follow the leading operands of its call, under the parameter types, and lists the leading operands and then the arguments positionally in parameter order with the evaluated default of every parameter not supplied, where a leading operand that is an operation is stored before the arguments that a change of order stores.
func (l *lowerer) args(o syntax.Origin, args []*syntax.Arg, leading []syntax.Expr, params []*syntax.Param, types []syntax.Type) []*syntax.Arg {
	slots := paramSlots(params)
	at := index(args, slots)
	xs := slices.Clone(leading)
	wants := make([]syntax.Type, len(leading))
	for i, a := range args {
		var want syntax.Type
		switch {
		case at[i] >= 0 && at[i] < len(types):
			want = types[at[i]]
		case a.Name == "" && i < len(types):
			want = types[i]
		}
		xs = append(xs, a.Value)
		wants = append(wants, want)
	}
	lowered := l.demanded(xs, wants)
	if !slices.IsSorted(at) {
		copy(lowered, l.temporaries(leading, lowered[:len(leading)], isRead))
	}
	var out []*syntax.Arg
	for i, x := range leading {
		out = append(out, &syntax.Arg{Origin: x.Where(), Value: lowered[i]})
	}
	lowered, xs = lowered[len(leading):], xs[len(leading):]
	supplied := make([]*syntax.Arg, len(args))
	for i, a := range args {
		supplied[i] = &syntax.Arg{Origin: a.Origin, Name: a.Name, Value: lowered[i]}
	}
	return append(out, l.order(o, supplied, xs, slots, false)...)
}

// construct lowers a construction, listing every field in declared order with the evaluated default of every field left out.
func (l *lowerer) construct(x *syntax.Construct) syntax.Expr {
	fields := l.fieldsOf(l.p.TypeOf(x))
	slots := fieldSlots(fields)
	at := index(x.Args, slots)
	var xs []syntax.Expr
	var wants []syntax.Type
	for i, a := range x.Args {
		var want syntax.Type
		if at[i] >= 0 {
			want = l.p.Table.Type(fields[at[i]].Type)
		}
		xs = append(xs, a.Value)
		wants = append(wants, want)
	}
	lowered := l.demanded(xs, wants)
	out := make([]*syntax.Arg, len(x.Args))
	for i, a := range x.Args {
		out[i] = &syntax.Arg{Origin: a.Origin, Name: a.Name, Value: lowered[i]}
	}
	named := len(x.Args) > 0 && x.Args[0].Name != ""
	return &syntax.Construct{Origin: x.Origin, Type: l.erase(x.Type), Args: l.order(x.Origin, out, xs, slots, named)}
}
