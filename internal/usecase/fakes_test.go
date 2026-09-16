package usecase_test

import (
	"context"
	"errors"

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

func newFakeTransactor(wallets ...*wallet.Wallet) *fakeTransactor {
	transactor := &fakeTransactor{committed: fakeStore{wallets: map[domain.ID]*wallet.Wallet{}}}
	for _, w := range wallets {
		transactor.committed.wallets[w.ID()] = copyWallet(w)
	}
	return transactor
}

func (f *fakeTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.calls++
	if f.committed.wallets == nil {
		f.committed.wallets = map[domain.ID]*wallet.Wallet{}
	}
	tx := &fakeTransaction{committed: &f.committed, staged: &fakeStore{wallets: map[domain.ID]*wallet.Wallet{}}}
	if err := fn(context.WithValue(ctx, fakeTxKey{}, tx)); err != nil {
		return err
	}
	for id, w := range tx.staged.wallets {
		f.committed.wallets[id] = w
	}
	f.committed.transactions = append(f.committed.transactions, tx.staged.transactions...)
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
