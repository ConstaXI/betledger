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
	"github.com/davibanfi/betledger/internal/domain/money"
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
	// Find loads the wallet without locking it, for reads. It returns a domain
	// not-found error carrying FailureCodeWalletNotFound when it does not exist.
	Find(ctx context.Context, id domain.ID) (*wallet.Wallet, error)
	// Reconcile reads the stored balance together with the balance rebuilt from
	// the ledger, in a single consistent view.
	Reconcile(ctx context.Context, id domain.ID) (Reconciliation, error)
	// GetForUpdate loads the wallet and locks it until the transaction ends, so
	// writers of the same wallet run one at a time while other wallets proceed in
	// parallel. It returns a domain not-found error carrying
	// FailureCodeWalletNotFound when the wallet does not exist.
	GetForUpdate(ctx context.Context, id domain.ID) (*wallet.Wallet, error)
	// UpdateBalance stores the wallet balance and version, only if the stored
	// version is still expectedVersion. Otherwise it returns ErrConcurrentUpdate.
	UpdateBalance(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error
}

// PendingReference identifies an operation leased for another attempt to
// resolve its reference.
type PendingReference struct {
	TransactionID domain.ID
	// WalletID is locked before the operation is read again, as every change to
	// the wallet's operations requires.
	WalletID domain.ID
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
	// HasProcessedReversal reports whether a REFUND or a ROLLBACK of the
	// operation was already processed.
	HasProcessedReversal(ctx context.Context, referenceID domain.ID) (bool, error)
	// FindByID returns the operation, and false when there is none.
	FindByID(ctx context.Context, id domain.ID) (*wager.Transaction, bool, error)
	// LeasePendingReferences takes up to limit operations in PENDING_REFERENCE
	// whose next attempt is due and postpones them to leaseUntil, so that other
	// workers skip them meanwhile. An operation whose worker dies is taken again
	// once the lease runs out.
	LeasePendingReferences(ctx context.Context, dueAt, leaseUntil time.Time, limit int) ([]PendingReference, error)
	// UpdatePendingReference stores the outcome of another attempt on an
	// operation still in PENDING_REFERENCE, with the next attempt at
	// nextAttemptAt when it keeps waiting. It returns ErrConcurrentUpdate when
	// the operation is no longer waiting.
	UpdatePendingReference(ctx context.Context, transaction *wager.Transaction, nextAttemptAt time.Time) error
}

// Reconciliation is the stored balance of a wallet beside the one rebuilt from
// its ledger.
type Reconciliation struct {
	Stored     money.Money
	Calculated money.Money
	// CheckedEntries is how many ledger entries were summed.
	CheckedEntries int
}

// LedgerCursor points at the last entry of a page, so the next page continues
// right after it. Entries are ordered by when they were recorded and, within
// the same instant, by identifier.
type LedgerCursor struct {
	RecordedAt time.Time
	EntryID    domain.ID
}

// LedgerEntry is a stored ledger entry together with when it was recorded,
// which the domain entity does not carry because no rule depends on it.
type LedgerEntry struct {
	Entry      *ledger.Entry
	RecordedAt time.Time
}

// LedgerRepository persists the append-only wallet ledger. Writes must run
// within a transaction.
type LedgerRepository interface {
	// Append stores a new ledger entry; existing entries are never changed.
	Append(ctx context.Context, entry *ledger.Entry) error
	// Page returns up to limit entries of the wallet, in a stable order,
	// starting after the cursor when there is one.
	Page(ctx context.Context, walletID domain.ID, cursor *LedgerCursor, limit int) ([]LedgerEntry, error)
}

// OutboxRecord is an event recorded in the outbox, as it is published.
type OutboxRecord struct {
	EventID     domain.ID
	AggregateID domain.ID
	EventType   string
	// Payload is the immutable JSON snapshot of the event, published byte for
	// byte on every attempt.
	Payload []byte
	// Attempts counts the publications of the event that failed.
	Attempts int
}

// OutboxRepository records events in the outbox table, to be published after
// the commit. Writes must run within a transaction, which is what makes
// recording an event atomic with the change that caused it.
type OutboxRepository interface {
	// Append records the events as immutable snapshots, pending publication.
	Append(ctx context.Context, events ...event.Event) error
	// LeasePending takes up to limit unpublished events whose next attempt is
	// due and postpones them to leaseUntil, so that other publishers skip them
	// meanwhile. It takes only the oldest unpublished event of each aggregate,
	// so events of the same wallet are published in the order they were
	// committed, even across publishers.
	LeasePending(ctx context.Context, dueAt, leaseUntil time.Time, limit int) ([]OutboxRecord, error)
	// MarkPublished records the event as published.
	MarkPublished(ctx context.Context, eventID domain.ID, publishedAt time.Time) error
	// ReschedulePublication records a failed publication and when to try again.
	ReschedulePublication(ctx context.Context, eventID domain.ID, attempts int, nextAttemptAt time.Time) error
}

// InboxMessage is a message the consumer already took in.
type InboxMessage struct {
	MessageID string
	// PayloadHash is the hash of the business fields carried by the message,
	// which tells a redelivery of the same message apart from another message
	// reusing its identifier.
	PayloadHash string
	// TransactionID is the operation the message produced.
	TransactionID domain.ID
}

// InboxRepository records the messages already taken in, so that a redelivery
// is not processed again. Writes must run within a transaction, the same one
// that applies the operation.
type InboxRepository interface {
	// Find returns the message already recorded, and false when there is none.
	Find(ctx context.Context, messageID string) (InboxMessage, bool, error)
	// Record stores the message. A message already recorded is reported as a
	// domain conflict carrying FailureCodeIdempotencyConflict.
	Record(ctx context.Context, message InboxMessage) error
}

// EventPublisher delivers recorded events to the outside world. The same event
// may be delivered more than once, always with the same event id, which is how
// consumers tell a republication apart.
type EventPublisher interface {
	Publish(ctx context.Context, record OutboxRecord) error
}
