package mesh

import "testing"

func TestOutcomeOfSettlesEveryResponseByTheMutationMark(t *testing.T) {
	tests := []struct {
		name    string
		r       Response
		mutates bool
		want    Outcome
	}{
		{"done", Response{Done: true, Clean: true}, true, Payload},
		{"done after an unclean call", Response{Done: true}, false, Payload},
		{"declined", Declined(), true, Unavailable},
		{"expired", Expired(), false, Lapsed},
		{"read without a response", Response{}, false, NoResponse},
		{"mutation without a response", Response{}, true, Unknown},
	}
	for _, tt := range tests {
		if got := OutcomeOf(tt.r, tt.mutates); got != tt.want {
			t.Errorf("%s: OutcomeOf = %v, want %v", tt.name, got, tt.want)
		}
	}
}
