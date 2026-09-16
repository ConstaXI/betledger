package postgres

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
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
func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	err = q.InsertWallet(ctx, sqlcgen.InsertWalletParams{
		ID:           wallet.ID(),
		PlayerID:     wallet.PlayerID(),
		Currency:     wallet.Currency().String(),
		BalanceMinor: wallet.Balance().MinorUnits(),
		Version:      wallet.Version(),
	})
	if isUniqueViolation(err, walletsPlayerCurrencyKey) {
		return domain.ConflictError(domain.FailureCodeWalletAlreadyExists,
			"player %s already holds a %s wallet", wallet.PlayerID(), wallet.Currency())
	}
	return translate(err)
}
