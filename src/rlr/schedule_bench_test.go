// This file measures what a sequential program pays for a call of a routine that
// reads and whose result it uses at once: the body as a task of the group, and
// the body in place.
package rlr

import "testing"

func BenchmarkABodyThatIsStartedAndWaitedForAtOnce(b *testing.B) {
	g := Open(clientScope())
	defer g.Cancel()
	b.ReportAllocs()
	for b.Loop() {
		t := g.Run(func(Invocation) ([]any, error) { return []any{int64(1)}, nil })
		if err := t.Join(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkABodyThatRunsInPlace(b *testing.B) {
	g := Open(clientScope())
	defer g.Cancel()
	b.ReportAllocs()
	for b.Loop() {
		t := g.RunAtOnce(func(Invocation) ([]any, error) { return []any{int64(1)}, nil })
		if err := t.Join(); err != nil {
			b.Fatal(err)
		}
	}
}
