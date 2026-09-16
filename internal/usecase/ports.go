// Package usecase orchestrates the domain through ports, without knowing about
// SQL, HTTP or messaging.
package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// ErrUnavailable signals a transient infrastructure failure. Adapters wrap it so
// that callers can tell a retryable outage apart from a definitive outcome.
var ErrUnavailable = errors.New("usecase: temporarily unavailable")

// ErrConcurrentUpdate signals that a wallet changed between being loaded and
// being written. The wallet lock should make it unreachable; it exists so that a
// lost update is refused instead of silently written.
var ErrConcurrentUpdate = errors.New("usecase: wallet changed concurrently")

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
	Create(ctx context.Context, w *wallet.Wallet) error
	// GetForUpdate loads the wallet and locks it until the transaction ends, so
	// writers of the same wallet run one at a time while other wallets proceed in
	// parallel. It returns a domain not-found error carrying
	// FailureCodeWalletNotFound when the wallet does not exist.
	GetForUpdate(ctx context.Context, id domain.ID) (*wallet.Wallet, error)
	// UpdateBalance stores the wallet balance and version, only if the stored
	// version is still expectedVersion. Otherwise it returns ErrConcurrentUpdate.
	UpdateBalance(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error
}

// TransactionRepository persists wager transactions. Writes must run within a
// transaction.
type TransactionRepository interface {
	// Create stores a new transaction.
	Create(ctx context.Context, transaction *wager.Transaction) error
	// FindByIdempotencyKey returns the operation a provider sent with the key, and
	// false when there is none.
	FindByIdempotencyKey(ctx context.Context, providerID, idempotencyKey string) (*wager.Transaction, bool, error)
	// FindByExternalID returns the operation a provider identified with the
	// external identifier, and false when there is none.
	FindByExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, bool, error)
}

// LedgerRepository persists the append-only wallet ledger. Writes must run
// within a transaction.
type LedgerRepository interface {
	// Append stores a new ledger entry; existing entries are never changed.
	Append(ctx context.Context, entry *ledger.Entry) error
}

// OutboxRepository records events in the outbox table, to be published after
// the commit. Writes must run within a transaction, which is what makes
// recording an event atomic with the change that caused it.
type OutboxRepository interface {
	// Append records the events as immutable snapshots, pending publication.
	Append(ctx context.Context, events ...event.Event) error
}
