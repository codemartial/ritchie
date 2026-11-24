// This file classifies a response into the outcome a caller acts on.
package mesh

// OutcomeOf is the outcome class of a response the runtime delivered: a payload when the handler finished, lapsed when it did not start before its budget ended, unavailable when it did not start for another reason, and otherwise no response for an invocation that cannot mutate and unknown for one that can.
func OutcomeOf(r Response, mutates bool) Outcome {
	switch {
	case r.Done:
		return Payload
	case r.Clean && r.Expired:
		return Lapsed
	case r.Clean:
		return Unavailable
	case !mutates:
		return NoResponse
	}
	return Unknown
}

// Outcome is the class of outcome a caller observes.
type Outcome uint8

const (
	Payload Outcome = iota
	Unavailable
	Lapsed
	NoResponse
	Unknown
)
