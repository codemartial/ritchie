package rlr

import (
	"slices"
	"testing"
)

func TestALoopThatLeavesCancelsTheBodiesThatItStartedAheadAndThatNoIterationReached(t *testing.T) {
	g := Open(clientScope())
	defer g.Cancel()
	ended := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var visited []int
	for i, task := range Ahead(g, slices.Values([]int{0, 1, 2}), func(i int, l *Lead) *Task {
		return l.Run(func(inv Invocation) ([]any, error) {
			defer close(ended[i])
			if i > 0 {
				<-inv.cancelled()
			}
			return []any{int64(i)}, nil
		})
	}) {
		visited = append(visited, i)
		g.Adopt(task)
		if err := task.Join(); err != nil {
			t.Errorf("join of the body of the first iteration = %v, want no failure", err)
		}
		break
	}
	if !slices.Equal(visited, []int{0}) {
		t.Errorf("the loop ran the iterations %v, want the first alone", visited)
	}
	awaitClosed(t, ended[1], "the end of the body that the second iteration started ahead")
	awaitClosed(t, ended[2], "the end of the body that the third iteration started ahead")
}
