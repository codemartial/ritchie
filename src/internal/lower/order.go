// This file is the evaluation-order rule of the lowering: it decides which operand
// is stored in a temporary before a later sibling and which operand is a place.
package lower

import (
	"fmt"

	"ritchie.dev/internal/syntax"
)

// root is the local or the receiver that a path of field and index selections starts at.
type root struct {
	name string
	self bool
}

// rootOf returns the root of a path of field and index selections.
func rootOf(x syntax.Expr) (root, bool) {
	for {
		switch y := x.(type) {
		case *syntax.Name:
			return root{name: y.Name}, y.Module == "" && y.Owner == nil
		case *syntax.Self:
			return root{self: true}, true
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			x = y.X
		default:
			return root{}, false
		}
	}
}

// changed returns the roots that the evaluation of a source expression can change, which are the roots of the receivers of its calls of methods that can change their receiver, because an expression holds no store outside its closures and a rewrite stores only to the temporaries that it declares.
func (l *lowerer) changed(x syntax.Expr) map[root]bool {
	var out map[root]bool
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Closure:
			return false
		case *syntax.MethodCall:
			if r, ok := rootOf(n.Receiver); ok && l.p.ChangesReceiver(n) {
				if out == nil {
					out = map[root]bool{}
				}
				out[r] = true
			}
		}
		return true
	})
	return out
}

// uses reports whether the value of a lowered expression depends on one of some roots after the calls that it holds have run: it holds a use of one outside every call of a routine, because such a call is evaluated at its place among its siblings and its results are values.
func (l *lowerer) uses(x syntax.Expr, roots map[root]bool) bool {
	if len(roots) == 0 {
		return false
	}
	found := false
	syntax.Inspect(x, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Name:
			found = found || n.Module == "" && n.Owner == nil && roots[root{name: n.Name}]
		case *syntax.Self:
			found = found || roots[root{self: true}]
		case syntax.Expr:
			return !l.routines[n]
		}
		return !found
	})
	return found
}

// isRead reports whether a lowered expression reads a value without an operation: a literal, a name, self, or a field of one of them.
func isRead(x syntax.Expr) bool {
	switch x := x.(type) {
	case *syntax.Lit, *syntax.Name, *syntax.Self:
		return true
	case *syntax.FieldRead:
		return isRead(x.X)
	}
	return plain(x)
}

// operands lowers sibling expressions, which are evaluated in written order, and stores an operand in a temporary before a later sibling in two cases, which are that the sibling holds a call of a method that can change a root that the operand uses and that the rewrites of the sibling hoist statements while the operand is an operation, whose effect and failure come before those statements, where every operation to the left of a stored operand is stored before it, every other operand is read where it is used, and every operand before settled is a read.
func (l *lowerer) operands(xs []syntax.Expr, wants []syntax.Type) []syntax.Expr {
	out := make([]syntax.Expr, len(xs))
	settled := 0
	for i, x := range xs {
		var want syntax.Type
		if i < len(wants) {
			want = wants[i]
		}
		pre, v := l.sub(func() syntax.Expr { return l.expr(x, want) })
		changed := l.changed(x)
		last := -1
		for j := i - 1; j >= settled && len(pre) > 0; j-- {
			if !isRead(out[j]) {
				last = j
				break
			}
		}
		for j := i - 1; j > last && len(changed) > 0; j-- {
			if l.uses(out[j], changed) {
				last = j
				break
			}
		}
		for j := range last + 1 {
			if j >= settled && !isRead(out[j]) || l.uses(out[j], changed) {
				out[j] = l.temporary(xs[j], out[j])
			}
		}
		settled = max(settled, last+1)
		if len(pre) > 0 {
			settled = i
		}
		l.pre = append(l.pre, pre...)
		out[i] = v
	}
	return out
}

// demanded lowers the operands of an operation that uses their values, where two or more operands that are remote reads are each stored in a temporary, with every operation in front of the last of them, in written order, so that the routine sends each read before the operation waits for the first.
func (l *lowerer) demanded(xs []syntax.Expr, wants []syntax.Type) []syntax.Expr {
	out := l.operands(xs, wants)
	last, reads := -1, 0
	for i, x := range xs {
		if l.p.Read(x) {
			last, reads = i, reads+1
		}
	}
	if reads < 2 {
		return out
	}
	for i := range last + 1 {
		if !isRead(out[i]) {
			out[i] = l.temporary(xs[i], out[i])
		}
	}
	return out
}

// temporary stores a lowered expression in a temporary of the type of its source and returns the temporary.
func (l *lowerer) temporary(source, lowered syntax.Expr) syntax.Expr {
	o := source.Where()
	t := l.p.TypeOf(source)
	tmp := l.fresh("t")
	l.hoist(&syntax.VarDecl{Origin: o, Targets: []*syntax.VarTarget{{Origin: o, Type: l.written(t, o), Name: tmp}}, Values: []syntax.Expr{lowered}})
	l.declare(tmp, t)
	return name(o, tmp)
}

// temporaries stores every lowered expression that read does not accept in a temporary, in order, and returns the expressions to use in their place.
func (l *lowerer) temporaries(sources, lowered []syntax.Expr, read func(syntax.Expr) bool) []syntax.Expr {
	out := make([]syntax.Expr, len(lowered))
	for i, v := range lowered {
		out[i] = v
		if !read(v) {
			out[i] = l.temporary(sources[i], v)
		}
	}
	return out
}

// isPlace reports whether a source expression is a place: a path of field and index selections that starts at self or at a name that is no constant, because no statement writes a constant.
func (l *lowerer) isPlace(x syntax.Expr) bool {
	for {
		switch y := x.(type) {
		case *syntax.Name:
			return y.Module == "" && y.Owner == nil && l.p.Constant(y) == nil && !l.p.Collection(y)
		case *syntax.Self:
			return true
		case *syntax.FieldRead:
			x = y.X
		case *syntax.IndexRead:
			x = y.X
		default:
			return false
		}
	}
}

// indexOperands returns the index and key operands of a place in written order, which are the operands that establishing the place evaluates.
func indexOperands(x syntax.Expr) []syntax.Expr {
	switch x := x.(type) {
	case *syntax.FieldRead:
		return indexOperands(x.X)
	case *syntax.IndexRead:
		return append(indexOperands(x.X), x.Index)
	}
	return nil
}

// established returns a place with its index and key operands replaced by the lowered ones, which it takes from the front of the list, so that the place itself is stored in no temporary.
func established(x syntax.Expr, lowered *[]syntax.Expr) syntax.Expr {
	switch x := x.(type) {
	case *syntax.Name, *syntax.Self:
		return x
	case *syntax.FieldRead:
		return &syntax.FieldRead{Origin: x.Origin, X: established(x.X, lowered), Name: x.Name}
	case *syntax.IndexRead:
		y := established(x.X, lowered)
		index := (*lowered)[0]
		*lowered = (*lowered)[1:]
		return &syntax.IndexRead{Origin: x.Origin, X: y, Index: index}
	}
	panic(fmt.Sprintf("lower: unexpected place %T", x))
}
