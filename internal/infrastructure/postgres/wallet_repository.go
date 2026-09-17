package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres/sqlcgen"
	"github.com/davibanfi/betledger/internal/usecase"
)

const walletsPlayerCurrencyKey = "wallets_player_currency_key"

var _ usecase.WalletRepository = (*WalletRepository)(nil)

// WalletRepository stores wallets in PostgreSQL.
type WalletRepository struct{}

// NewWalletRepository builds the repository.
func NewWalletRepository() *WalletRepository {
	return &WalletRepository{}
}

// Create inserts the wallet within the transaction carried by ctx.
func (r *WalletRepository) Create(ctx context.Context, w *wallet.Wallet) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	err = q.InsertWallet(ctx, sqlcgen.InsertWalletParams{
		ID:           w.ID(),
		PlayerID:     w.PlayerID(),
		Currency:     w.Currency().String(),
		BalanceMinor: w.Balance().MinorUnits(),
		Version:      w.Version(),
	})
	if isUniqueViolation(err, walletsPlayerCurrencyKey) {
		return domain.ConflictError(domain.FailureCodeWalletAlreadyExists,
			"player %s already holds a %s wallet", w.PlayerID(), w.Currency())
	}
	return translate(err)
}

// Find loads the wallet without locking it.
func (r *WalletRepository) Find(ctx context.Context, id domain.ID) (*wallet.Wallet, error) {
	q, err := queries(ctx)
	if err != nil {
		return nil, err
	}
	row, err := q.SelectWallet(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NotFoundError(domain.FailureCodeWalletNotFound, "wallet %s not found", id)
	}
	if err != nil {
		return nil, translate(err)
	}
	return rehydrateWallet(row.ID, row.PlayerID, row.Currency, row.BalanceMinor, row.Version)
}

// Reconcile reads the stored balance and the one rebuilt from the ledger in a
// single statement, so both come from the same snapshot.
func (r *WalletRepository) Reconcile(ctx context.Context, id domain.ID) (usecase.Reconciliation, error) {
	q, err := queries(ctx)
	if err != nil {
		return usecase.Reconciliation{}, err
	}
	row, err := q.SelectWalletReconciliation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.Reconciliation{}, domain.NotFoundError(domain.FailureCodeWalletNotFound,
			"wallet %s not found", id)
	}
	if err != nil {
		return usecase.Reconciliation{}, translate(err)
	}

	currency, err := money.NewCurrency(row.Currency)
	if err != nil {
		return usecase.Reconciliation{}, err
	}
	stored, err := money.New(row.BalanceMinor, currency)
	if err != nil {
		return usecase.Reconciliation{}, err
	}
	calculated, err := money.New(row.RebuiltMinor, currency)
	if err != nil {
		return usecase.Reconciliation{}, err
	}
	return usecase.Reconciliation{
		Stored:         stored,
		Calculated:     calculated,
		CheckedEntries: int(row.CheckedEntries),
	}, nil
}

// GetForUpdate loads the wallet with a row lock held until the transaction
// carried by ctx ends.
func (r *WalletRepository) GetForUpdate(ctx context.Context, id domain.ID) (*wallet.Wallet, error) {
	q, err := queries(ctx)
	if err != nil {
		return nil, err
	}

	row, err := q.SelectWalletForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NotFoundError(domain.FailureCodeWalletNotFound, "wallet %s not found", id)
	}
	if err != nil {
		return nil, translate(err)
	}

	return rehydrateWallet(row.ID, row.PlayerID, row.Currency, row.BalanceMinor, row.Version)
}

func rehydrateWallet(id, playerID domain.ID, currencyCode string, balanceMinor, version int64) (*wallet.Wallet, error) {
	currency, err := money.NewCurrency(currencyCode)
	if err != nil {
		return nil, err
	}
	balance, err := money.New(balanceMinor, currency)
	if err != nil {
		return nil, err
	}
	return wallet.Rehydrate(id, playerID, balance, version)
}

// UpdateBalance stores the balance and version when the stored version still
// matches expectedVersion.
func (r *WalletRepository) UpdateBalance(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	updated, err := q.UpdateWalletBalance(ctx, sqlcgen.UpdateWalletBalanceParams{
		ID:              w.ID(),
		BalanceMinor:    w.Balance().MinorUnits(),
		Version:         w.Version(),
		ExpectedVersion: expectedVersion,
	})
	if err != nil {
		return translate(err)
	}
	if updated == 0 {
		return usecase.ErrConcurrentUpdate
	}
	return nil
}
