package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres/sqlcgen"
	"github.com/davibanfi/betledger/internal/usecase"
)

const (
	transactionsProviderIdempotencyKey = "wager_transactions_provider_idempotency_key"
	transactionsProviderExternalID     = "wager_transactions_provider_external_id"
	transactionsOneReversalPerRef      = "wager_transactions_one_reversal_per_reference"
)

var _ usecase.TransactionRepository = (*TransactionRepository)(nil)

// TransactionRepository stores wager transactions in PostgreSQL.
type TransactionRepository struct{}

// NewTransactionRepository builds the repository.
func NewTransactionRepository() *TransactionRepository {
	return &TransactionRepository{}
}

// Create inserts the transaction within the transaction carried by ctx. A
// provider identity already recorded is reported as an idempotency conflict,
// and a second processed reversal of the same operation as a conflict too.
func (r *TransactionRepository) Create(ctx context.Context, transaction *wager.Transaction) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	params := sqlcgen.InsertWagerTransactionParams{
		ID:                             transaction.ID(),
		Kind:                           transaction.Kind().String(),
		State:                          transaction.State().String(),
		WalletID:                       transaction.WalletID(),
		PlayerID:                       transaction.PlayerID(),
		Currency:                       transaction.Money().Currency().String(),
		AmountMinor:                    transaction.Money().MinorUnits(),
		ProviderID:                     optional(transaction.ProviderID()),
		ExternalTransactionID:          optional(transaction.ExternalTransactionID()),
		IdempotencyKey:                 optional(transaction.IdempotencyKey()),
		PayloadHash:                    optional(transaction.PayloadHash()),
		RoundID:                        optional(transaction.RoundID()),
		GameID:                         optional(transaction.GameID()),
		ReferenceExternalTransactionID: optional(transaction.ReferenceExternalTransactionID()),
		FailureCode:                    optional(string(transaction.FailureCode())),
	}
	if referenceID, ok := transaction.ReferenceTransactionID(); ok {
		params.ReferenceTransactionID = &referenceID
	}
	if balance, ok := transaction.ResultBalance(); ok {
		minorUnits := balance.MinorUnits()
		params.ResultBalanceMinor = &minorUnits
	}

	err = q.InsertWagerTransaction(ctx, params)
	if isUniqueViolation(err, transactionsProviderIdempotencyKey) || isUniqueViolation(err, transactionsProviderExternalID) {
		return domain.ConflictError(domain.FailureCodeIdempotencyConflict,
			"operation %q from provider %s was already recorded",
			transaction.ExternalTransactionID(), transaction.ProviderID())
	}
	if isUniqueViolation(err, transactionsOneReversalPerRef) {
		return domain.ConflictError(domain.FailureCodeReferenceAlreadyReversed,
			"reference %q from provider %s was already reversed",
			transaction.ReferenceExternalTransactionID(), transaction.ProviderID())
	}
	return translate(err)
}

// HasProcessedReversal reports whether the operation already has a processed
// REFUND or ROLLBACK.
func (r *TransactionRepository) HasProcessedReversal(ctx context.Context, referenceID domain.ID) (bool, error) {
	q, err := queries(ctx)
	if err != nil {
		return false, err
	}
	exists, err := q.ExistsProcessedReversal(ctx, &referenceID)
	return exists, translate(err)
}

// FindByIdempotencyKey returns the operation the provider sent with the key.
func (r *TransactionRepository) FindByIdempotencyKey(
	ctx context.Context,
	providerID, idempotencyKey string,
) (*wager.Transaction, bool, error) {
	q, err := queries(ctx)
	if err != nil {
		return nil, false, err
	}
	row, err := q.SelectWagerTransactionByIdempotencyKey(ctx, sqlcgen.SelectWagerTransactionByIdempotencyKeyParams{
		ProviderID:     &providerID,
		IdempotencyKey: &idempotencyKey,
	})
	return rehydrateTransaction(row, err)
}

// FindByExternalID returns the operation the provider identified with the
// external identifier.
func (r *TransactionRepository) FindByExternalID(
	ctx context.Context,
	providerID, externalTransactionID string,
) (*wager.Transaction, bool, error) {
	q, err := queries(ctx)
	if err != nil {
		return nil, false, err
	}
	row, err := q.SelectWagerTransactionByExternalID(ctx, sqlcgen.SelectWagerTransactionByExternalIDParams{
		ProviderID:            &providerID,
		ExternalTransactionID: &externalTransactionID,
	})
	return rehydrateTransaction(row, err)
}

func rehydrateTransaction(row sqlcgen.WagerTransaction, err error) (*wager.Transaction, bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, translate(err)
	}

	currency, err := money.NewCurrency(row.Currency)
	if err != nil {
		return nil, false, err
	}
	amount, err := money.New(row.AmountMinor, currency)
	if err != nil {
		return nil, false, err
	}

	params := wager.RehydrateParams{
		ID:                             row.ID,
		Kind:                           wager.Kind(row.Kind),
		State:                          wager.State(row.State),
		WalletID:                       row.WalletID,
		PlayerID:                       row.PlayerID,
		Money:                          amount,
		ProviderID:                     value(row.ProviderID),
		ExternalTransactionID:          value(row.ExternalTransactionID),
		IdempotencyKey:                 value(row.IdempotencyKey),
		PayloadHash:                    value(row.PayloadHash),
		RoundID:                        value(row.RoundID),
		GameID:                         value(row.GameID),
		ReferenceExternalTransactionID: value(row.ReferenceExternalTransactionID),
		FailureCode:                    domain.FailureCode(value(row.FailureCode)),
	}
	if row.ReferenceTransactionID != nil {
		params.ReferenceTransactionID = *row.ReferenceTransactionID
	}
	if row.ResultBalanceMinor != nil {
		balance, err := money.New(*row.ResultBalanceMinor, currency)
		if err != nil {
			return nil, false, err
		}
		params.ResultBalance = &balance
	}

	transaction, err := wager.Rehydrate(params)
	if err != nil {
		return nil, false, err
	}
	return transaction, true, nil
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}
