// Package usecase orchestrates the domain through ports, without knowing about
// SQL, HTTP or messaging.
package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
)

// ErrUnavailable signals a transient infrastructure failure. Adapters wrap it so
// that callers can tell a retryable outage apart from a definitive outcome.
var ErrUnavailable = errors.New("usecase: temporarily unavailable")

// Clock returns the current instant. It is injected so that use cases stay
// deterministic under test.
type Clock func() time.Time

// Transactor delimits an atomic change across repositories.
type Transactor interface {
	// WithinTransaction runs fn with a database transaction carried by its ctx.
	// Repositories called with that ctx join the transaction, which commits when
	// fn returns nil and rolls back otherwise.
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// WalletRepository persists wallets. Writes must run within a transaction.
type WalletRepository interface {
	// Create stores a new wallet. It returns a domain conflict carrying
	// FailureCodeWalletAlreadyExists when the player already holds a wallet in
	// that currency.
	Create(ctx context.Context, wallet *domain.Wallet) error
}

// TransactionRepository persists wager transactions. Writes must run within a
// transaction.
type TransactionRepository interface {
	// Create stores a new transaction.
	Create(ctx context.Context, transaction *domain.WagerTransaction) error
}

// LedgerRepository persists the append-only wallet ledger. Writes must run
// within a transaction.
type LedgerRepository interface {
	// Append stores a new ledger entry; existing entries are never changed.
	Append(ctx context.Context, entry *domain.WalletLedgerEntry) error
}

// OutboxRepository records events in the outbox table, to be published after
// the commit. Writes must run within a transaction, which is what makes
// recording an event atomic with the change that caused it.
type OutboxRepository interface {
	// Append records the events as immutable snapshots, pending publication.
	Append(ctx context.Context, events ...domain.Event) error
}
