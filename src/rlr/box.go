// A box holds a present value behind a pointer that its copies share, and a write first
// gives the written box a copy of its own, so a copy takes constant time and a write
// through a path of boxes copies one value per box on the path.
package rlr

// Box is a nullable of a struct that contains itself.
type Box[T any] struct {
	p *T
}

func (b Box[T]) payload() (any, bool) {
	if b.p == nil {
		return nil, false
	}
	return *b.p, true
}

// Display renders a nullable in a standalone context.
func (b Box[T]) Display(module string) string { return render(b, module) }

// BoxSome is the present box holding v.
func BoxSome[T any](v T) Box[T] { return Box[T]{p: &v} }

// BoxNone is the absent box.
func BoxNone[T any]() Box[T] { return Box[T]{} }

// IsSome reports whether the box is present.
func (b Box[T]) IsSome() bool { return b.p != nil }

// IsNone reports whether the box is absent.
func (b Box[T]) IsNone() bool { return b.p == nil }

// Value is the present value, which shares the storage of its own fields with the box, read without a presence test, because generated code reads a nullable at its value type only where the kernel checker knows it present.
func (b Box[T]) Value() T { return *b.p }

// Place is the location of the present value after the box takes its own copy of it, made by clone, so that a write through the location reaches no other box, taken without a presence test for the same reason as Value.
func (b *Box[T]) Place(clone func(T) T) *T {
	v := clone(*b.p)
	b.p = &v
	return b.p
}

// MapBox is the display form of a box whose payload displays through a form of its own.
func MapBox[T, U any](b Box[T], f func(T) U) Option[U] {
	if b.p == nil {
		return None[U]()
	}
	return Some(f(*b.p))
}

// EqualBoxes reports whether two boxes are both absent, or both present with equal values.
func EqualBoxes[T any](a, b Box[T], eq func(T, T) bool) bool {
	if a.p == nil || b.p == nil {
		return a.p == b.p
	}
	return a.p == b.p || eq(*a.p, *b.p)
}
