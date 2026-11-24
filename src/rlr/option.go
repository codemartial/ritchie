package rlr

// Option is a nullable value.
type Option[T any] struct {
	value T
	ok    bool
}

const absentDisplay = "none"

// optional is the type-erased view of an option the display forms take.
type optional interface {
	payload() (any, bool)
}

func (o Option[T]) payload() (any, bool) { return o.value, o.ok }

// Display renders a nullable in a standalone context.
func (o Option[T]) Display(module string) string { return render(o, module) }

// Some is the present option holding v.
func Some[T any](v T) Option[T] {
	return Option[T]{value: v, ok: true}
}

// None is the absent option.
func None[T any]() Option[T] {
	return Option[T]{}
}

// Get is the comma-ok accessor.
func (o Option[T]) Get() (T, bool) {
	return o.value, o.ok
}

// IsSome reports whether the option is present.
func (o Option[T]) IsSome() bool { return o.ok }

// IsNone reports whether the option is absent.
func (o Option[T]) IsNone() bool { return !o.ok }

// Unwrap is the present value.
func (o Option[T]) Unwrap() T {
	if !o.ok {
		panic("option: Unwrap called on None")
	}
	return o.value
}

// Ref is the live location of the present value.
func (o *Option[T]) Ref() *T {
	if !o.ok {
		panic("option: Ref called on None")
	}
	return &o.value
}

// Value is the present value, read without a presence test, because generated code reads a nullable at its value type only where the kernel checker knows it present.
func (o Option[T]) Value() T { return o.value }

// Place is the live location of the present value, taken without a presence test for the same reason as Value.
func (o *Option[T]) Place() *T { return &o.value }

// MapOption is the option with a present payload passed through f.
func MapOption[T, U any](o Option[T], f func(T) U) Option[U] {
	if !o.ok {
		return None[U]()
	}
	return Some(f(o.value))
}

// WidenOption is the nullable integer widened to a wider width.
func WidenOption[T, U Integer](o Option[T]) Option[U] {
	if !o.ok {
		return None[U]()
	}
	return Some(U(o.value))
}

// CloneOption is the independent copy of a nullable whose payload carries storage of its own.
func CloneOption[T any](o Option[T], clone func(T) T) Option[T] {
	if !o.ok {
		return None[T]()
	}
	return Some(clone(o.value))
}

// EqualOptions is the equality of two nullables under the payload equality eq.
func EqualOptions[T any](a, b Option[T], eq func(T, T) bool) bool {
	if a.ok != b.ok {
		return false
	}
	return !a.ok || eq(a.value, b.value)
}

// OptionEqual is the equality function over nullables of a payload type with equality eq.
func OptionEqual[T any](eq func(T, T) bool) func(Option[T], Option[T]) bool {
	return func(a, b Option[T]) bool { return EqualOptions(a, b, eq) }
}
