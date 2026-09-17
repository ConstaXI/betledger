//go:build integration

package testenv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
	"github.com/davibanfi/betledger/internal/usecase"
)

// UseCases are the use cases wired to the PostgreSQL adapters, without Fx.
type UseCases struct {
	OpenWallet   *usecase.OpenWallet
	ProcessWager *usecase.ProcessWager
}

// NewUseCases wires the use cases to the database.
func (p *Postgres) NewUseCases() UseCases {
	return UseCases{
		OpenWallet: usecase.NewOpenWallet(
			postgres.NewTransactor(p.Pool),
			postgres.NewWalletRepository(),
			postgres.NewTransactionRepository(),
			postgres.NewLedgerRepository(),
			postgres.NewOutboxRepository(),
			time.Now,
		),
		ProcessWager: usecase.NewProcessWager(
			postgres.NewTransactor(p.Pool),
			postgres.NewWalletRepository(),
			postgres.NewTransactionRepository(),
			postgres.NewLedgerRepository(),
			postgres.NewOutboxRepository(),
			time.Now,
		),
	}
}

// MustOpenWallet opens a wallet for a new player through the use case.
func (u UseCases) MustOpenWallet(t *testing.T, balance money.Money) *wallet.Wallet {
	t.Helper()

	opened, err := u.OpenWallet.Execute(context.Background(), usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: balance,
		CorrelationID:  "req-seed",
	})
	require.NoError(t, err)
	return opened
}

// BetInput builds a BET on the wallet. The external identifier is scoped to the
// wallet, so tests sharing a database never collide.
func BetInput(w *wallet.Wallet, externalID string, amountMinor int64) usecase.ProcessWagerInput {
	scoped := w.ID().String() + ":" + externalID
	return usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: scoped,
		IdempotencyKey:        "provider-a:" + scoped,
		PlayerID:              w.PlayerID(),
		WalletID:              w.ID(),
		RoundID:               "round-1",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindBet,
		Money:                 money.MustNew(amountMinor, w.Currency()),
		CorrelationID:         "req-" + externalID,
	}
}

// WalletState reads the stored balance, version and number of debits.
func (p *Postgres) WalletState(t *testing.T, walletID domain.ID) (balance, version int64, debits int) {
	t.Helper()

	require.NoError(t, p.Pool.QueryRow(context.Background(), `
		SELECT w.balance_minor, w.version,
			(SELECT count(*) FROM wallet_ledger_entries l WHERE l.wallet_id = w.id AND l.direction = 'DEBIT')
		FROM wallets w WHERE w.id = $1`, walletID).Scan(&balance, &version, &debits))
	return balance, version, debits
}

// AssertLedgerReconciles checks that the stored balance equals the credits minus
// the debits recorded in the ledger.
func (p *Postgres) AssertLedgerReconciles(t *testing.T, walletID domain.ID) {
	t.Helper()

	var stored, rebuilt int64
	require.NoError(t, p.Pool.QueryRow(context.Background(), `
		SELECT w.balance_minor,
			COALESCE(SUM(CASE l.direction WHEN 'CREDIT' THEN l.amount_minor ELSE -l.amount_minor END), 0)
		FROM wallets w
		LEFT JOIN wallet_ledger_entries l ON l.wallet_id = w.id
		WHERE w.id = $1
		GROUP BY w.balance_minor`, walletID).Scan(&stored, &rebuilt))
	assert.Equal(t, stored, rebuilt, "stored balance must equal credits minus debits in the ledger")
}

// Connections counts the open connections tagged with the application name.
func (p *Postgres) Connections(t *testing.T, applicationName string) int {
	t.Helper()

	var count int
	require.NoError(t, p.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM pg_stat_activity WHERE application_name = $1", applicationName).Scan(&count))
	return count
}
