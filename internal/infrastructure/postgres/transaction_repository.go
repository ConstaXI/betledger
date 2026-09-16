package postgres

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres/sqlcgen"
	"github.com/davibanfi/betledger/internal/usecase"
)

var _ usecase.TransactionRepository = (*TransactionRepository)(nil)

// TransactionRepository stores wager transactions in PostgreSQL.
type TransactionRepository struct{}

// NewTransactionRepository builds the repository.
func NewTransactionRepository() *TransactionRepository {
	return &TransactionRepository{}
}

// Create inserts the transaction within the transaction carried by ctx.
func (r *TransactionRepository) Create(ctx context.Context, transaction *domain.WagerTransaction) error {
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

	return translate(q.InsertWagerTransaction(ctx, params))
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
