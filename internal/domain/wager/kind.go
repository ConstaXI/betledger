package wager

import "github.com/davibanfi/betledger/internal/domain"

// Kind is the kind of a wagering operation. OPENING is reserved for the internal
// wallet opening and is rejected from HTTP and SQS.
type Kind string

const (
	KindOpening  Kind = "OPENING"
	KindBet      Kind = "BET"
	KindWin      Kind = "WIN"
	KindLoss     Kind = "LOSS"
	KindRefund   Kind = "REFUND"
	KindRollback Kind = "ROLLBACK"
)

func (k Kind) String() string { return string(k) }

// IsValid reports whether the kind is known to the domain.
func (k Kind) IsValid() bool {
	switch k {
	case KindOpening, KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		return true
	default:
		return false
	}
}

// IsExternal reports whether the kind may arrive over HTTP or SQS.
func (k Kind) IsExternal() bool {
	return k.IsValid() && k != KindOpening
}

// IsReversal reports whether the kind undoes a previous operation.
func (k Kind) IsReversal() bool {
	return k == KindRefund || k == KindRollback
}

// RequiresReference reports whether the kind demands an external reference.
func (k Kind) RequiresReference() bool { return k.IsReversal() }

// AllowsReference reports whether the kind accepts an external reference. WIN
// may point at a bet of the same round; BET and LOSS accept none.
func (k Kind) AllowsReference() bool {
	return k.RequiresReference() || k == KindWin
}

// ParseExternalKind validates a kind received from outside, rejecting OPENING.
func ParseExternalKind(value string) (Kind, error) {
	kind := Kind(value)
	if !kind.IsValid() {
		return "", domain.ValidationError(domain.FailureCodeInvalidInput, "transaction kind %q is invalid", value)
	}
	if !kind.IsExternal() {
		return "", domain.ValidationError(domain.FailureCodeKindNotAllowed,
			"kind %s is reserved for the internal wallet opening", kind)
	}
	return kind, nil
}
