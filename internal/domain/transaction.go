package domain

// TransactionKind is the kind of a wagering operation. OPENING is reserved for
// the internal wallet opening and is rejected from HTTP and SQS.
type TransactionKind string

const (
	KindOpening  TransactionKind = "OPENING"
	KindBet      TransactionKind = "BET"
	KindWin      TransactionKind = "WIN"
	KindLoss     TransactionKind = "LOSS"
	KindRefund   TransactionKind = "REFUND"
	KindRollback TransactionKind = "ROLLBACK"
)

func (k TransactionKind) String() string { return string(k) }

// IsValid reports whether the kind is known to the domain.
func (k TransactionKind) IsValid() bool {
	switch k {
	case KindOpening, KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		return true
	default:
		return false
	}
}

// IsExternal reports whether the kind may arrive over HTTP or SQS.
func (k TransactionKind) IsExternal() bool {
	return k.IsValid() && k != KindOpening
}

// IsReversal reports whether the kind undoes a previous operation.
func (k TransactionKind) IsReversal() bool {
	return k == KindRefund || k == KindRollback
}

// RequiresReference reports whether the kind demands an external reference.
func (k TransactionKind) RequiresReference() bool { return k.IsReversal() }

// AllowsReference reports whether the kind accepts an external reference. WIN
// may point at a bet of the same round; BET and LOSS accept none.
func (k TransactionKind) AllowsReference() bool {
	return k.RequiresReference() || k == KindWin
}

// ParseExternalTransactionKind validates a kind received from outside,
// rejecting OPENING.
func ParseExternalTransactionKind(value string) (TransactionKind, error) {
	kind := TransactionKind(value)
	if !kind.IsValid() {
		return "", ValidationError(FailureCodeInvalidInput, "transaction kind %q is invalid", value)
	}
	if !kind.IsExternal() {
		return "", ValidationError(FailureCodeKindNotAllowed,
			"kind %s is reserved for the internal wallet opening", kind)
	}
	return kind, nil
}

// TransactionState is the state of a wagering operation.
type TransactionState string

const (
	StatePending          TransactionState = "PENDING"
	StatePendingReference TransactionState = "PENDING_REFERENCE"
	StateProcessed        TransactionState = "PROCESSED"
	StateRejected         TransactionState = "REJECTED"
	StateFailed           TransactionState = "FAILED"
)

func (s TransactionState) String() string { return string(s) }

// IsValid reports whether the state is known.
func (s TransactionState) IsValid() bool {
	switch s {
	case StatePending, StatePendingReference, StateProcessed, StateRejected, StateFailed:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the state is final and admits no further
// transitions.
func (s TransactionState) IsTerminal() bool {
	return s == StateProcessed || s == StateRejected || s == StateFailed
}

// CanTransitionTo describes the state machine:
//
//	PENDING           -> PENDING_REFERENCE | PROCESSED | REJECTED | FAILED
//	PENDING_REFERENCE -> PROCESSED | REJECTED | FAILED
//	PROCESSED | REJECTED | FAILED -> (none)
func (s TransactionState) CanTransitionTo(target TransactionState) bool {
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

// WagerTransaction records a financial operation and its outcome. External
// operations carry the provider metadata; the internal opening does not, and is
// told apart by Kind.IsExternal. A pending reference expires by a maximum
// number of attempts rather than a TTL, which keeps the domain free of a clock.
type WagerTransaction struct {
	id       ID
	kind     TransactionKind
	state    TransactionState
	walletID ID
	playerID ID
	money    Money

	providerID            string
	externalTransactionID string
	idempotencyKey        string
	payloadHash           string
	roundID               string
	gameID                string

	referenceExternalTransactionID string
	referenceTransactionID         ID

	failureCode   FailureCode
	resultBalance *Money
}

// NewExternalTransactionParams gathers an operation received over HTTP or SQS.
type NewExternalTransactionParams struct {
	ID                             ID
	Kind                           TransactionKind
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	WalletID                       ID
	PlayerID                       ID
	RoundID                        string
	GameID                         string
	Money                          Money
	ReferenceExternalTransactionID string
}

// NewExternalTransaction creates an external operation in the PENDING state.
func NewExternalTransaction(params NewExternalTransactionParams) (*WagerTransaction, error) {
	if !params.Kind.IsValid() {
		return nil, ValidationError(FailureCodeInvalidInput, "transaction kind %q is invalid", params.Kind)
	}
	if !params.Kind.IsExternal() {
		return nil, ValidationError(FailureCodeKindNotAllowed,
			"kind %s is reserved for the internal wallet opening", params.Kind)
	}
	if err := requireID(params.ID, "transactionId"); err != nil {
		return nil, err
	}
	if err := requireID(params.WalletID, "walletId"); err != nil {
		return nil, err
	}
	if err := requireID(params.PlayerID, "playerId"); err != nil {
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
			return nil, ValidationError(FailureCodeInvalidInput, "%s is required", field)
		}
	}
	if err := validateKindAmount(params.Kind, params.Money); err != nil {
		return nil, err
	}
	if err := validateKindReference(params.Kind, params.ReferenceExternalTransactionID); err != nil {
		return nil, err
	}

	return &WagerTransaction{
		id:                             params.ID,
		kind:                           params.Kind,
		state:                          StatePending,
		walletID:                       params.WalletID,
		playerID:                       params.PlayerID,
		money:                          params.Money,
		providerID:                     params.ProviderID,
		externalTransactionID:          params.ExternalTransactionID,
		idempotencyKey:                 params.IdempotencyKey,
		payloadHash:                    params.PayloadHash,
		roundID:                        params.RoundID,
		gameID:                         params.GameID,
		referenceExternalTransactionID: params.ReferenceExternalTransactionID,
	}, nil
}

// NewOpeningTransaction creates the internal wallet opening operation, already
// concluded and without the external metadata that does not apply to it.
func NewOpeningTransaction(id, walletID, playerID ID, initialBalance Money) (*WagerTransaction, error) {
	if err := requireID(id, "transactionId"); err != nil {
		return nil, err
	}
	if err := requireID(walletID, "walletId"); err != nil {
		return nil, err
	}
	if err := requireID(playerID, "playerId"); err != nil {
		return nil, err
	}
	if err := validateKindAmount(KindOpening, initialBalance); err != nil {
		return nil, err
	}

	balance := initialBalance
	return &WagerTransaction{
		id:            id,
		kind:          KindOpening,
		state:         StateProcessed,
		walletID:      walletID,
		playerID:      playerID,
		money:         initialBalance,
		resultBalance: &balance,
	}, nil
}

// RehydrateWagerTransactionParams gathers the persisted state of an operation.
type RehydrateWagerTransactionParams struct {
	ID                             ID
	Kind                           TransactionKind
	State                          TransactionState
	WalletID                       ID
	PlayerID                       ID
	Money                          Money
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PayloadHash                    string
	RoundID                        string
	GameID                         string
	ReferenceExternalTransactionID string
	ReferenceTransactionID         ID
	FailureCode                    FailureCode
	ResultBalance                  *Money
}

// RehydrateWagerTransaction rebuilds a persisted operation without reapplying
// movements, transitions or event emission.
func RehydrateWagerTransaction(params RehydrateWagerTransactionParams) (*WagerTransaction, error) {
	if !params.Kind.IsValid() {
		return nil, ValidationError(FailureCodeInvalidInput, "transaction kind %q is invalid", params.Kind)
	}
	if !params.State.IsValid() {
		return nil, ValidationError(FailureCodeInvalidInput, "state %q is invalid", params.State)
	}
	if err := requireID(params.ID, "transactionId"); err != nil {
		return nil, err
	}
	if err := requireID(params.WalletID, "walletId"); err != nil {
		return nil, err
	}
	if err := requireID(params.PlayerID, "playerId"); err != nil {
		return nil, err
	}
	if err := params.Money.Validate(); err != nil {
		return nil, err
	}

	return &WagerTransaction{
		id:                             params.ID,
		kind:                           params.Kind,
		state:                          params.State,
		walletID:                       params.WalletID,
		playerID:                       params.PlayerID,
		money:                          params.Money,
		providerID:                     params.ProviderID,
		externalTransactionID:          params.ExternalTransactionID,
		idempotencyKey:                 params.IdempotencyKey,
		payloadHash:                    params.PayloadHash,
		roundID:                        params.RoundID,
		gameID:                         params.GameID,
		referenceExternalTransactionID: params.ReferenceExternalTransactionID,
		referenceTransactionID:         params.ReferenceTransactionID,
		failureCode:                    params.FailureCode,
		resultBalance:                  params.ResultBalance,
	}, nil
}

func validateKindAmount(kind TransactionKind, money Money) error {
	if err := money.Validate(); err != nil {
		return err
	}
	if money.IsNegative() {
		return ValidationError(FailureCodeInvalidAmount,
			"%s does not accept a negative amount, got %s", kind, money)
	}
	if kind == KindLoss {
		if !money.IsZero() {
			return ValidationError(FailureCodeInvalidAmount,
				"LOSS requires a zero amount, got %s", money)
		}
		return nil
	}
	if !money.IsPositive() {
		return ValidationError(FailureCodeInvalidAmount,
			"%s requires an amount greater than zero, got %s", kind, money)
	}
	return nil
}

func validateKindReference(kind TransactionKind, reference string) error {
	switch {
	case kind.RequiresReference() && reference == "":
		return ValidationError(FailureCodeInvalidInput,
			"%s requires referenceExternalTransactionId", kind)
	case !kind.AllowsReference() && reference != "":
		return ValidationError(FailureCodeInvalidInput,
			"%s does not accept referenceExternalTransactionId", kind)
	default:
		return nil
	}
}

// MarkPendingReference records the wait for a reference not yet available.
func (t *WagerTransaction) MarkPendingReference() error {
	if !t.kind.AllowsReference() {
		return ValidationError(FailureCodeInvalidStateTransition,
			"%s does not depend on a reference", t.kind)
	}
	return t.transitionTo(StatePendingReference)
}

// MarkProcessed concludes the operation successfully, keeping the observed
// balance so that a replay returns the same result even after later movements.
func (t *WagerTransaction) MarkProcessed(balanceAfter Money) error {
	if err := balanceAfter.Validate(); err != nil {
		return err
	}
	if balanceAfter.Currency() != t.money.Currency() {
		return ValidationError(FailureCodeCurrencyMismatch,
			"balance in %s does not match the operation currency %s", balanceAfter.Currency(), t.money.Currency())
	}
	if err := t.transitionTo(StateProcessed); err != nil {
		return err
	}
	balance := balanceAfter
	t.resultBalance = &balance
	return nil
}

// MarkRejected ends the operation by a business rule refusal.
func (t *WagerTransaction) MarkRejected(code FailureCode) error {
	if code == "" {
		return ValidationError(FailureCodeInvalidInput, "a rejection requires a failureCode")
	}
	if err := t.transitionTo(StateRejected); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// MarkFailed ends the operation by a permanent infrastructure failure, keeping
// the record for auditing.
func (t *WagerTransaction) MarkFailed(code FailureCode) error {
	if code == "" {
		return ValidationError(FailureCodeInvalidInput, "a failure requires a failureCode")
	}
	if err := t.transitionTo(StateFailed); err != nil {
		return err
	}
	t.failureCode = code
	return nil
}

// ResolveReference attaches the internal reference found for the operation.
func (t *WagerTransaction) ResolveReference(referenceID ID) error {
	if !t.kind.AllowsReference() {
		return ValidationError(FailureCodeInvalidStateTransition,
			"%s does not accept a reference", t.kind)
	}
	if err := requireID(referenceID, "referenceTransactionId"); err != nil {
		return err
	}
	if t.state.IsTerminal() {
		return ValidationError(FailureCodeInvalidStateTransition,
			"operation in terminal state %s accepts no further changes", t.state)
	}
	t.referenceTransactionID = referenceID
	return nil
}

func (t *WagerTransaction) transitionTo(target TransactionState) error {
	if t.state.IsTerminal() {
		return ValidationError(FailureCodeInvalidStateTransition,
			"operation in terminal state %s cannot transition to %s", t.state, target)
	}
	if !t.state.CanTransitionTo(target) {
		return ValidationError(FailureCodeInvalidStateTransition,
			"transition from %s to %s is not allowed", t.state, target)
	}
	t.state = target
	return nil
}

func (t *WagerTransaction) ID() ID                        { return t.id }
func (t *WagerTransaction) Kind() TransactionKind         { return t.kind }
func (t *WagerTransaction) State() TransactionState       { return t.state }
func (t *WagerTransaction) WalletID() ID                  { return t.walletID }
func (t *WagerTransaction) PlayerID() ID                  { return t.playerID }
func (t *WagerTransaction) Money() Money                  { return t.money }
func (t *WagerTransaction) ProviderID() string            { return t.providerID }
func (t *WagerTransaction) ExternalTransactionID() string { return t.externalTransactionID }
func (t *WagerTransaction) IdempotencyKey() string        { return t.idempotencyKey }
func (t *WagerTransaction) PayloadHash() string           { return t.payloadHash }
func (t *WagerTransaction) RoundID() string               { return t.roundID }
func (t *WagerTransaction) GameID() string                { return t.gameID }
func (t *WagerTransaction) FailureCode() FailureCode      { return t.failureCode }

// ReferenceExternalTransactionID returns the external reference supplied.
func (t *WagerTransaction) ReferenceExternalTransactionID() string {
	return t.referenceExternalTransactionID
}

// ReferenceTransactionID returns the resolved internal reference, if any.
func (t *WagerTransaction) ReferenceTransactionID() (ID, bool) {
	if IsNilID(t.referenceTransactionID) {
		return NilID, false
	}
	return t.referenceTransactionID, true
}

// ResultBalance returns the balance observed on conclusion, if any.
func (t *WagerTransaction) ResultBalance() (Money, bool) {
	if t.resultBalance == nil {
		return Money{}, false
	}
	return *t.resultBalance, true
}

// MovesBalance reports whether the kind changes the wallet balance. LOSS does
// not.
func (t *WagerTransaction) MovesBalance() bool {
	return t.kind != KindLoss
}
