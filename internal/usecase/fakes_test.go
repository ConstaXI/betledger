package usecase_test

import (
	"context"
	"errors"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

var errOutsideTransaction = errors.New("fake: write outside transaction")

type fakeTxKey struct{}

type fakeStore struct {
	wallets      map[domain.ID]*wallet.Wallet
	transactions []*wager.Transaction
	updated      map[domain.ID]*wager.Transaction
	nextAttempts map[domain.ID]time.Time
	entries      []*ledger.Entry
	events       []event.Event
	eventTypes   []event.Type
}

type fakeTransaction struct {
	committed *fakeStore
	staged    *fakeStore
}

type fakeTransactor struct {
	calls     int
	committed fakeStore
}

func newFakeStore() fakeStore {
	return fakeStore{
		wallets:      map[domain.ID]*wallet.Wallet{},
		updated:      map[domain.ID]*wager.Transaction{},
		nextAttempts: map[domain.ID]time.Time{},
	}
}

func newFakeTransactor(wallets ...*wallet.Wallet) *fakeTransactor {
	transactor := &fakeTransactor{committed: newFakeStore()}
	for _, w := range wallets {
		transactor.committed.wallets[w.ID()] = copyWallet(w)
	}
	return transactor
}

func (f *fakeTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.calls++
	staged := newFakeStore()
	tx := &fakeTransaction{committed: &f.committed, staged: &staged}
	if err := fn(context.WithValue(ctx, fakeTxKey{}, tx)); err != nil {
		return err
	}
	for id, w := range tx.staged.wallets {
		f.committed.wallets[id] = w
	}
	f.committed.transactions = append(f.committed.transactions, tx.staged.transactions...)
	for i, transaction := range f.committed.transactions {
		if updated, ok := tx.staged.updated[transaction.ID()]; ok {
			f.committed.transactions[i] = updated
		}
	}
	for id, at := range tx.staged.nextAttempts {
		f.committed.nextAttempts[id] = at
	}
	f.committed.entries = append(f.committed.entries, tx.staged.entries...)
	f.committed.events = append(f.committed.events, tx.staged.events...)
	f.committed.eventTypes = append(f.committed.eventTypes, tx.staged.eventTypes...)
	return nil
}

func transactionFrom(ctx context.Context) (*fakeTransaction, error) {
	tx, ok := ctx.Value(fakeTxKey{}).(*fakeTransaction)
	if !ok {
		return nil, errOutsideTransaction
	}
	return tx, nil
}

func copyWallet(w *wallet.Wallet) *wallet.Wallet {
	copied, err := wallet.Rehydrate(w.ID(), w.PlayerID(), w.Balance(), w.Version())
	if err != nil {
		panic(err)
	}
	return copied
}

type fakeWallets struct{ err error }

func (f fakeWallets) Create(ctx context.Context, w *wallet.Wallet) error {
	if f.err != nil {
		return f.err
	}
	tx, err := transactionFrom(ctx)
	if err != nil {
		return err
	}
	tx.staged.wallets[w.ID()] = copyWallet(w)
	return nil
}

func (f fakeWallets) GetForUpdate(ctx context.Context, id domain.ID) (*wallet.Wallet, error) {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return nil, err
	}
	if w, ok := tx.staged.wallets[id]; ok {
		return copyWallet(w), nil
	}
	if w, ok := tx.committed.wallets[id]; ok {
		return copyWallet(w), nil
	}
	return nil, domain.NotFoundError(domain.FailureCodeWalletNotFound, "wallet %s not found", id)
}

func (f fakeWallets) UpdateBalance(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error {
	current, err := f.GetForUpdate(ctx, w.ID())
	if err != nil {
		return err
	}
	if current.Version() != expectedVersion {
		return usecase.ErrConcurrentUpdate
	}
	tx, _ := transactionFrom(ctx)
	tx.staged.wallets[w.ID()] = copyWallet(w)
	return nil
}

type fakeTransactions struct{}

func (fakeTransactions) Create(ctx context.Context, transaction *wager.Transaction) error {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return err
	}
	tx.staged.transactions = append(tx.staged.transactions, transaction)
	return nil
}

func (fakeTransactions) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, bool, error) {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, transaction := range tx.committed.transactions {
		if transaction.ProviderID() == providerID && transaction.IdempotencyKey() == key {
			return transaction, true, nil
		}
	}
	return nil, false, nil
}

func (fakeTransactions) FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, bool, error) {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, transaction := range tx.committed.transactions {
		if transaction.ProviderID() == providerID && transaction.ExternalTransactionID() == externalID {
			return transaction, true, nil
		}
	}
	return nil, false, nil
}

func (fakeTransactions) HasProcessedReversal(ctx context.Context, referenceID domain.ID) (bool, error) {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return false, err
	}
	for _, transaction := range tx.committed.transactions {
		resolved, ok := transaction.ReferenceTransactionID()
		if ok && resolved == referenceID && transaction.Kind().IsReversal() &&
			transaction.State() == wager.StateProcessed {
			return true, nil
		}
	}
	return false, nil
}

func (fakeTransactions) FindByID(ctx context.Context, id domain.ID) (*wager.Transaction, bool, error) {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, transaction := range tx.committed.transactions {
		if transaction.ID() == id {
			return copyTransaction(transaction), true, nil
		}
	}
	return nil, false, nil
}

func (fakeTransactions) LeasePendingReferences(
	ctx context.Context,
	dueAt, leaseUntil time.Time,
	limit int,
) ([]usecase.PendingReference, error) {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return nil, err
	}
	var leased []usecase.PendingReference
	for _, transaction := range tx.committed.transactions {
		due := !tx.committed.nextAttempts[transaction.ID()].After(dueAt)
		if transaction.State() == wager.StatePendingReference && due && len(leased) < limit {
			tx.staged.nextAttempts[transaction.ID()] = leaseUntil
			leased = append(leased, usecase.PendingReference{
				TransactionID: transaction.ID(),
				WalletID:      transaction.WalletID(),
			})
		}
	}
	return leased, nil
}

func (fakeTransactions) UpdatePendingReference(
	ctx context.Context,
	transaction *wager.Transaction,
	nextAttemptAt time.Time,
) error {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return err
	}
	for _, stored := range tx.committed.transactions {
		if stored.ID() == transaction.ID() && stored.State() == wager.StatePendingReference {
			tx.staged.updated[transaction.ID()] = copyTransaction(transaction)
			tx.staged.nextAttempts[transaction.ID()] = nextAttemptAt
			return nil
		}
	}
	return usecase.ErrConcurrentUpdate
}

func copyTransaction(transaction *wager.Transaction) *wager.Transaction {
	params := wager.RehydrateParams{
		ID:                             transaction.ID(),
		Kind:                           transaction.Kind(),
		State:                          transaction.State(),
		WalletID:                       transaction.WalletID(),
		PlayerID:                       transaction.PlayerID(),
		Money:                          transaction.Money(),
		ProviderID:                     transaction.ProviderID(),
		ExternalTransactionID:          transaction.ExternalTransactionID(),
		IdempotencyKey:                 transaction.IdempotencyKey(),
		PayloadHash:                    transaction.PayloadHash(),
		RoundID:                        transaction.RoundID(),
		GameID:                         transaction.GameID(),
		ReferenceExternalTransactionID: transaction.ReferenceExternalTransactionID(),
		ReferenceAttempts:              transaction.ReferenceAttempts(),
		FailureCode:                    transaction.FailureCode(),
	}
	params.ReferenceTransactionID, _ = transaction.ReferenceTransactionID()
	if balance, ok := transaction.ResultBalance(); ok {
		params.ResultBalance = &balance
	}
	copied, err := wager.Rehydrate(params)
	if err != nil {
		panic(err)
	}
	return copied
}

type fakeLedger struct{}

func (fakeLedger) Append(ctx context.Context, entry *ledger.Entry) error {
	tx, err := transactionFrom(ctx)
	if err != nil {
		return err
	}
	tx.staged.entries = append(tx.staged.entries, entry)
	return nil
}

type fakeOutbox struct{ err error }

func (f fakeOutbox) Append(ctx context.Context, events ...event.Event) error {
	if f.err != nil {
		return f.err
	}
	tx, err := transactionFrom(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		tx.staged.events = append(tx.staged.events, e)
		tx.staged.eventTypes = append(tx.staged.eventTypes, e.Type)
	}
	return nil
}
