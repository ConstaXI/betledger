package wager

// State is the state of a wagering operation.
type State string

const (
	StatePending          State = "PENDING"
	StatePendingReference State = "PENDING_REFERENCE"
	StateProcessed        State = "PROCESSED"
	StateRejected         State = "REJECTED"
	StateFailed           State = "FAILED"
)

func (s State) String() string { return string(s) }

// IsValid reports whether the state is known.
func (s State) IsValid() bool {
	switch s {
	case StatePending, StatePendingReference, StateProcessed, StateRejected, StateFailed:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the state is final and admits no further
// transitions.
func (s State) IsTerminal() bool {
	return s == StateProcessed || s == StateRejected || s == StateFailed
}

// CanTransitionTo describes the state machine:
//
//	PENDING           -> PENDING_REFERENCE | PROCESSED | REJECTED | FAILED
//	PENDING_REFERENCE -> PROCESSED | REJECTED | FAILED
//	PROCESSED | REJECTED | FAILED -> (none)
func (s State) CanTransitionTo(target State) bool {
	if !s.IsValid() || !target.IsValid() || s.IsTerminal() {
		return false
	}
	if target == StatePending {
		return false
	}
	if target == StatePendingReference {
		return s == StatePending
	}
	return true
}
