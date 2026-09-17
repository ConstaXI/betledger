package usecase

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/wager"
)

// ReadTransaction answers the queries about the operations of a provider.
type ReadTransaction struct {
	transactor   Transactor
	transactions TransactionRepository
}

// NewReadTransaction builds the use case.
func NewReadTransaction(transactor Transactor, transactions TransactionRepository) *ReadTransaction {
	return &ReadTransaction{transactor: transactor, transactions: transactions}
}

// ByID returns the operation with the internal identifier, provided it belongs
// to the provider asking for it. An operation of another provider is reported
// as not found, so that a provider cannot learn that it exists.
func (uc *ReadTransaction) ByID(
	ctx context.Context,
	providerID string,
	transactionID domain.ID,
) (*wager.Transaction, error) {
	return uc.find(ctx, providerID, func(ctx context.Context) (*wager.Transaction, bool, error) {
		return uc.transactions.FindByID(ctx, transactionID)
	}, transactionID.String())
}

// ByExternalID returns the operation the provider identified with the external
// identifier.
func (uc *ReadTransaction) ByExternalID(
	ctx context.Context,
	providerID, externalTransactionID string,
) (*wager.Transaction, error) {
	return uc.find(ctx, providerID, func(ctx context.Context) (*wager.Transaction, bool, error) {
		return uc.transactions.FindByExternalID(ctx, providerID, externalTransactionID)
	}, externalTransactionID)
}

func (uc *ReadTransaction) find(
	ctx context.Context,
	providerID string,
	lookup func(ctx context.Context) (*wager.Transaction, bool, error),
	identifier string,
) (*wager.Transaction, error) {
	var transaction *wager.Transaction
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		found, exists, err := lookup(ctx)
		if err != nil {
			return err
		}
		if !exists || found.ProviderID() != providerID {
			return domain.NotFoundError(domain.FailureCodeTransactionNotFound,
				"operation %q not found for provider %s", identifier, providerID)
		}
		transaction = found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return transaction, nil
}
