// Package wager implements the wagering operations sent by game providers and
// their state machine.
package wager

import (
	"slices"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
)

// Transaction records a financial operation and its outcome. External
// operations carry the provider metadata; the internal opening does not, and is
// told apart by Kind.IsExternal. A pending reference expires by a maximum number
// of attempts rather than a TTL, which keeps the domain free of a clock.
type Transaction struct {
	// id is the internal identifier of the operation.
	id domain.ID
	// kind determines the balance movement and the reference policy.
	kind Kind
	// state only advances through the transitions allowed by CanTransitionTo.
	state State
	// walletID is the wallet the operation moves.
	walletID domain.ID
	// playerID owns the wallet; it must match the wallet owner.
	playerID domain.ID
	// amount is zero for LOSS and positive for every other kind.
	amount money.Money

	// providerID identifies the game provider; empty for the internal opening.
	providerID string
	// externalTransactionID is the provider identifier, unique per provider.
	externalTransactionID string
	// idempotencyKey is kept verbatim as received; the server never replaces it
	// with a computed key.
	idempotencyKey string
	// payloadHash detects a reused idempotency key carrying different content.
	payloadHash string
	// roundID groups the operations of a single game round.
	roundID string
	// gameID identifies the game where the round happened.
	gameID string

	// referenceExternalTransactionID is the provider identifier of the referenced
	// operation, required for reversals and optional for WIN.
	referenceExternalTransactionID string
	// referenceTransactionID is the internal identifier the reference resolved
	// to, nil until resolution.
	referenceTransactionID domain.ID
	// referenceAttempts counts the attempts that found no concluded reference;
	// reaching the maximum rejects the operation with REFERENCE_NOT_FOUND.
	referenceAttempts int

	// failureCode explains a REJECTED or FAILED outcome.
	failureCode domain.FailureCode
	// resultBalance is the balance observed on conclusion, so that a replay
	// returns the original result even after later movements.
	resultBalance *money.Money

	// createdAt is when the operation was recorded.
	createdAt time.Time
	// updatedAt is when the state last changed, which for an operation waiting
	// for its reference includes each attempt counted.
	updatedAt time.Time
}

// NewExternalParams gathers an operation received over HTTP or SQS.
type NewExternalParams struct {
	ID                             domain.ID
	Kind                           Kind
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	WalletID                       domain.ID
	PlayerID                       domain.ID
	RoundID                        string
	GameID                         string
	Money                          money.Money
	ReferenceExternalTransactionID string
	// CreatedAt is when the operation was received.
	CreatedAt time.Time
}

// NewExternal creates an external operation in the PENDING state.
func NewExternal(params NewExternalParams) (*Transaction, error) {
	if !params.Kind.IsValid() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "transaction kind %q is invalid", params.Kind)
	}
	if !params.Kind.IsExternal() {
		return nil, domain.ValidationError(domain.FailureCodeKindNotAllowed,
			"kind %s is reserved for the internal wallet opening", params.Kind)
	}
	if err := domain.RequireID(params.ID, "transactionId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(params.WalletID, "walletId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(params.PlayerID, "playerId"); err != nil {
		return nil, err
	}
	if err := domain.RequireTime(params.CreatedAt, "createdAt"); err != nil {
		return nil, err
	}
	for field, value := range map[string]string{
		"providerId":            params.ProviderID,
		"externalTransactionId": params.ExternalTransactionID,
		"idempotencyKey":        params.IdempotencyKey,
		"payloadHash":           params.PayloadHash,
		"roundId":               params.RoundID,
		"gameId":                params.GameID,
	} {
		if value == "" {
			return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "%s is required", field)
		}
	}
	if err := validateKindAmount(params.Kind, params.Money); err != nil {
		return nil, err
	}
	if err := validateKindReference(params.Kind, params.ReferenceExternalTransactionID); err != nil {
		return nil, err
	}
	if params.ReferenceExternalTransactionID == params.ExternalTransactionID {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput,
			"operation %s cannot refer to itself", params.ExternalTransactionID)
	}

	return &Transaction{
		id:                             params.ID,
		kind:                           params.Kind,
		state:                          StatePending,
		walletID:                       params.WalletID,
		playerID:                       params.PlayerID,
		amount:                         params.Money,
		providerID:                     params.ProviderID,
		externalTransactionID:          params.ExternalTransactionID,
		idempotencyKey:                 params.IdempotencyKey,
		payloadHash:                    params.PayloadHash,
		roundID:                        params.RoundID,
		gameID:                         params.GameID,
		referenceExternalTransactionID: params.ReferenceExternalTransactionID,
		createdAt:                      params.CreatedAt,
		updatedAt:                      params.CreatedAt,
	}, nil
}

// NewOpening creates the internal wallet opening operation, already concluded
// and without the external metadata that does not apply to it.
func NewOpening(
	id, walletID, playerID domain.ID,
	initialBalance money.Money,
	createdAt time.Time,
) (*Transaction, error) {
	if err := domain.RequireID(id, "transactionId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(walletID, "walletId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(playerID, "playerId"); err != nil {
		return nil, err
	}
	if err := validateKindAmount(KindOpening, initialBalance); err != nil {
		return nil, err
	}
	if err := domain.RequireTime(createdAt, "createdAt"); err != nil {
		return nil, err
	}

	balance := initialBalance
	return &Transaction{
		id:            id,
		kind:          KindOpening,
		state:         StateProcessed,
		walletID:      walletID,
		playerID:      playerID,
		amount:        initialBalance,
		resultBalance: &balance,
		createdAt:     createdAt,
		updatedAt:     createdAt,
	}, nil
}

// RehydrateParams gathers the persisted state of an operation.
type RehydrateParams struct {
	ID                             domain.ID
	Kind                           Kind
	State                          State
	WalletID                       domain.ID
	PlayerID                       domain.ID
	Money                          money.Money
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	RoundID                        string
	GameID                         string
	ReferenceExternalTransactionID string
	ReferenceTransactionID         domain.ID
	ReferenceAttempts              int
	FailureCode                    domain.FailureCode
	ResultBalance                  *money.Money
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
}

// Rehydrate rebuilds a persisted operation without reapplying movements,
// transitions or event emission.
func Rehydrate(params RehydrateParams) (*Transaction, error) {
	if !params.Kind.IsValid() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "transaction kind %q is invalid", params.Kind)
	}
	if !params.State.IsValid() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "state %q is invalid", params.State)
	}
	if err := domain.RequireID(params.ID, "transactionId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(params.WalletID, "walletId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(params.PlayerID, "playerId"); err != nil {
		return nil, err
	}
	if err := params.Money.Validate(); err != nil {
		return nil, err
	}
	if params.ReferenceAttempts < 0 {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput,
			"reference attempts cannot be negative, got %d", params.ReferenceAttempts)
	}
	if err := domain.RequireTime(params.CreatedAt, "createdAt"); err != nil {
		return nil, err
	}
	if err := domain.RequireTime(params.UpdatedAt, "updatedAt"); err != nil {
		return nil, err
	}

	return &Transaction{
		id:                             params.ID,
		kind:                           params.Kind,
		state:                          params.State,
		walletID:                       params.WalletID,
		playerID:                       params.PlayerID,
		amount:                         params.Money,
		providerID:                     params.ProviderID,
		externalTransactionID:          params.ExternalTransactionID,
		idempotencyKey:                 params.IdempotencyKey,
		payloadHash:                    params.PayloadHash,
		roundID:                        params.RoundID,
		gameID:                         params.GameID,
		referenceExternalTransactionID: params.ReferenceExternalTransactionID,
		referenceTransactionID:         params.ReferenceTransactionID,
		referenceAttempts:              params.ReferenceAttempts,
		failureCode:                    params.FailureCode,
		resultBalance:                  params.ResultBalance,
		createdAt:                      params.CreatedAt,
		updatedAt:                      params.UpdatedAt,
	}, nil
}

func validateKindAmount(kind Kind, amount money.Money) error {
	if err := amount.Validate(); err != nil {
		return err
	}
	if amount.IsNegative() {
		return domain.ValidationError(domain.FailureCodeInvalidAmount,
			"%s does not accept a negative amount, got %s", kind, amount)
	}
	if kind == KindLoss {
		if !amount.IsZero() {
			return domain.ValidationError(domain.FailureCodeInvalidAmount,
				"LOSS requires a zero amount, got %s", amount)
		}
		return nil
	}
	if !amount.IsPositive() {
		return domain.ValidationError(domain.FailureCodeInvalidAmount,
			"%s requires an amount greater than zero, got %s", kind, amount)
	}
	return nil
}

func validateKindReference(kind Kind, reference string) error {
	switch {
	case kind.RequiresReference() && reference == "":
		return domain.ValidationError(domain.FailureCodeInvalidInput,
			"%s requires referenceExternalTransactionId", kind)
	case !kind.AllowsReference() && reference != "":
		return domain.ValidationError(domain.FailureCodeInvalidInput,
			"%s does not accept referenceExternalTransactionId", kind)
	default:
		return nil
	}
}

// MarkPendingReference records the wait for a reference not yet available.
func (t *Transaction) MarkPendingReference(at time.Time) error {
	if t.referenceExternalTransactionID == "" {
		return domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"%s %s does not depend on a reference", t.kind, t.externalTransactionID)
	}
	return t.transitionTo(StatePendingReference, at)
}

// RecordMissingReference counts an attempt that still found no concluded
// reference. When the attempts reach maxAttempts, the operation is rejected with
// FailureCodeReferenceNotFound; otherwise it keeps waiting.
func (t *Transaction) RecordMissingReference(maxAttempts int, at time.Time) error {
	if err := domain.RequireTime(at, "attempt instant"); err != nil {
		return err
	}
	if maxAttempts < 1 {
		return domain.ValidationError(domain.FailureCodeInvalidInput,
			"the maximum of reference attempts must be at least 1, got %d", maxAttempts)
	}
	if t.state != StatePendingReference {
		return domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"only an operation in %s waits for its reference, got %s", StatePendingReference, t.state)
	}
	t.referenceAttempts++
	t.updatedAt = at
	if t.referenceAttempts < maxAttempts {
		return nil
	}
	return t.MarkRejected(domain.FailureCodeReferenceNotFound, at)
}

// MarkProcessed concludes the operation successfully, keeping the observed
// balance so that a replay returns the same result even after later movements.
func (t *Transaction) MarkProcessed(balanceAfter money.Money, at time.Time) error {
	if err := balanceAfter.Validate(); err != nil {
		return err
	}
	if balanceAfter.Currency() != t.amount.Currency() {
		return domain.ValidationError(domain.FailureCodeCurrencyMismatch,
			"balance in %s does not match the operation currency %s", balanceAfter.Currency(), t.amount.Currency())
	}
	if err := t.transitionTo(StateProcessed, at); err != nil {
		return err
	}
	balance := balanceAfter
	t.resultBalance = &balance
	return nil
}

// MarkRejected ends the operation by a business rule refusal.
func (t *Transaction) MarkRejected(code domain.FailureCode, at time.Time) error {
	if code == "" {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "a rejection requires a failureCode")
	}
	if err := t.transitionTo(StateRejected, at); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// MarkFailed ends the operation by a permanent infrastructure failure, keeping
// the record for auditing.
func (t *Transaction) MarkFailed(code domain.FailureCode, at time.Time) error {
	if code == "" {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "a failure requires a failureCode")
	}
	if err := t.transitionTo(StateFailed, at); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// ResolveReference checks the operation against the one it references and, when
// they agree, attaches it. The reference must be PROCESSED and of a kind the
// operation may refer to: a WIN or a REFUND refers to a BET, and a ROLLBACK to a
// BET, a WIN or a REFUND. Both must share provider, player, wallet, currency and
// round, and a reversal must carry the same amount. A disagreement is a
// rejection, meant to be recorded; the caller decides what to do with a
// reference still pending.
func (t *Transaction) ResolveReference(reference *Transaction) error {
	if reference == nil || t.referenceExternalTransactionID == "" {
		return domain.ValidationError(domain.FailureCodeInvalidInput,
			"%s %s has no reference to resolve", t.kind, t.externalTransactionID)
	}
	if t.state.IsTerminal() {
		return domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"operation in terminal state %s accepts no further changes", t.state)
	}
	if reference.providerID != t.providerID || reference.externalTransactionID != t.referenceExternalTransactionID {
		return domain.ValidationError(domain.FailureCodeInvalidInput,
			"%s %s is not the reference %s of provider %s", reference.kind, reference.externalTransactionID,
			t.referenceExternalTransactionID, t.providerID)
	}
	if !slices.Contains(referableKinds[t.kind], reference.kind) {
		return domain.RejectionError(domain.FailureCodeReferenceMismatch,
			"%s cannot refer to %s %s", t.kind, reference.kind, reference.externalTransactionID)
	}
	if reference.state != StateProcessed {
		return domain.RejectionError(domain.FailureCodeReferenceNotProcessed,
			"reference %s is %s, not %s", reference.externalTransactionID, reference.state, StateProcessed)
	}
	if reference.walletID != t.walletID || reference.playerID != t.playerID ||
		reference.amount.Currency() != t.amount.Currency() || reference.roundID != t.roundID {
		return domain.RejectionError(domain.FailureCodeReferenceMismatch,
			"reference %s belongs to another player, wallet, currency or round", reference.externalTransactionID)
	}
	if t.kind.IsReversal() && !reference.amount.Equal(t.amount) {
		return domain.RejectionError(domain.FailureCodeReferenceAmountMismatch,
			"%s of %s must reverse the whole %s, not %s", t.kind, reference.externalTransactionID, reference.amount, t.amount)
	}
	t.referenceTransactionID = reference.id
	return nil
}

var referableKinds = map[Kind][]Kind{
	KindWin:      {KindBet},
	KindRefund:   {KindBet},
	KindRollback: {KindBet, KindWin, KindRefund},
}

// transitionTo moves the state and stamps when it moved, so that updatedAt
// always marks the last change of the operation.
func (t *Transaction) transitionTo(target State, at time.Time) error {
	if err := domain.RequireTime(at, "transition instant"); err != nil {
		return err
	}
	if t.state.IsTerminal() {
		return domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"operation in terminal state %s cannot transition to %s", t.state, target)
	}
	if !t.state.CanTransitionTo(target) {
		return domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"transition from %s to %s is not allowed", t.state, target)
	}
	t.state = target
	t.updatedAt = at
	return nil
}

func (t *Transaction) ID() domain.ID                   { return t.id }
func (t *Transaction) Kind() Kind                      { return t.kind }
func (t *Transaction) State() State                    { return t.state }
func (t *Transaction) WalletID() domain.ID             { return t.walletID }
func (t *Transaction) PlayerID() domain.ID             { return t.playerID }
func (t *Transaction) Money() money.Money              { return t.amount }
func (t *Transaction) ProviderID() string              { return t.providerID }
func (t *Transaction) ExternalTransactionID() string   { return t.externalTransactionID }
func (t *Transaction) IdempotencyKey() string          { return t.idempotencyKey }
func (t *Transaction) PayloadHash() string             { return t.payloadHash }
func (t *Transaction) RoundID() string                 { return t.roundID }
func (t *Transaction) GameID() string                  { return t.gameID }
func (t *Transaction) FailureCode() domain.FailureCode { return t.failureCode }
func (t *Transaction) CreatedAt() time.Time            { return t.createdAt }
func (t *Transaction) UpdatedAt() time.Time            { return t.updatedAt }

// ReferenceExternalTransactionID returns the external reference supplied.
func (t *Transaction) ReferenceExternalTransactionID() string {
	return t.referenceExternalTransactionID
}

// ReferenceTransactionID returns the resolved internal reference, if any.
func (t *Transaction) ReferenceTransactionID() (domain.ID, bool) {
	if domain.IsNilID(t.referenceTransactionID) {
		return domain.NilID, false
	}
	return t.referenceTransactionID, true
}

// ReferenceAttempts returns how many attempts found no concluded reference.
func (t *Transaction) ReferenceAttempts() int { return t.referenceAttempts }

// ResultBalance returns the balance observed on conclusion, if any.
func (t *Transaction) ResultBalance() (money.Money, bool) {
	if t.resultBalance == nil {
		return money.Money{}, false
	}
	return *t.resultBalance, true
}
