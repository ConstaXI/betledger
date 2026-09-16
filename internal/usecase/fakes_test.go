package usecase_test

import (
	"context"
	"errors"

	"github.com/davibanfi/betledger/internal/domain"
)

var errOutsideTransaction = errors.New("fake: write outside transaction")

type fakeTxKey struct{}

type fakeStore struct {
	wallets      []*domain.Wallet
	transactions []*domain.WagerTransaction
	entries      []*domain.WalletLedgerEntry
	events       []domain.Event
	eventTypes   []domain.EventType
}

type fakeTransactor struct {
	calls     int
	committed fakeStore
}

func (f *fakeTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.calls++
	staged := &fakeStore{}
	if err := fn(context.WithValue(ctx, fakeTxKey{}, staged)); err != nil {
		return err
	}
	f.committed.wallets = append(f.committed.wallets, staged.wallets...)
	f.committed.transactions = append(f.committed.transactions, staged.transactions...)
	f.committed.entries = append(f.committed.entries, staged.entries...)
	f.committed.events = append(f.committed.events, staged.events...)
	f.committed.eventTypes = append(f.committed.eventTypes, staged.eventTypes...)
	return nil
}

func stagedFrom(ctx context.Context) (*fakeStore, error) {
	staged, ok := ctx.Value(fakeTxKey{}).(*fakeStore)
	if !ok {
		return nil, errOutsideTransaction
	}
	return staged, nil
}

type fakeWallets struct{ err error }

func (f fakeWallets) Create(ctx context.Context, wallet *domain.Wallet) error {
	if f.err != nil {
		return f.err
	}
	staged, err := stagedFrom(ctx)
	if err != nil {
		return err
	}
	staged.wallets = append(staged.wallets, wallet)
	return nil
}

type fakeTransactions struct{}

func (fakeTransactions) Create(ctx context.Context, transaction *domain.WagerTransaction) error {
	staged, err := stagedFrom(ctx)
	if err != nil {
		return err
	}
	staged.transactions = append(staged.transactions, transaction)
	return nil
}

type fakeLedger struct{}

func (fakeLedger) Append(ctx context.Context, entry *domain.WalletLedgerEntry) error {
	staged, err := stagedFrom(ctx)
	if err != nil {
		return err
	}
	staged.entries = append(staged.entries, entry)
	return nil
}

type fakeOutbox struct{ err error }

func (f fakeOutbox) Append(ctx context.Context, events ...domain.Event) error {
	if f.err != nil {
		return f.err
	}
	staged, err := stagedFrom(ctx)
	if err != nil {
		return err
	}
	for _, event := range events {
		staged.events = append(staged.events, event)
		staged.eventTypes = append(staged.eventTypes, event.EventType)
	}
	return nil
}
