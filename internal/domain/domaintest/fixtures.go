// Package domaintest provides fixtures shared by the tests of the domain
// packages. It is imported only from tests.
package domaintest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// MustParseMoney parses a non-negative amount, failing the test otherwise.
func MustParseMoney(t testing.TB, amount, currency string) money.Money {
	t.Helper()

	value, err := money.Parse(amount, currency)
	require.NoError(t, err)
	return value
}

// MustParseSignedMoney parses an amount that may be negative, failing the test
// otherwise.
func MustParseSignedMoney(t testing.TB, amount, currency string) money.Money {
	t.Helper()

	value, err := money.ParseSigned(amount, currency)
	require.NoError(t, err)
	return value
}

// MustOpenWallet opens a wallet for a new player, failing the test otherwise.
func MustOpenWallet(t testing.TB, initialBalance money.Money) *wallet.Wallet {
	t.Helper()

	opened, err := wallet.Open(domain.NewID(), domain.NewID(), initialBalance)
	require.NoError(t, err)
	return opened
}

// ValidExternalParams returns a valid external operation of the kind, with a
// reference when the kind requires one.
func ValidExternalParams(t testing.TB, kind wager.Kind, amount string) wager.NewExternalParams {
	t.Helper()

	params := wager.NewExternalParams{
		ID:                    domain.NewID(),
		Kind:                  kind,
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "provider-a:transaction-123",
		PayloadHash:           "hash",
		WalletID:              domain.NewID(),
		PlayerID:              domain.NewID(),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Money:                 MustParseMoney(t, amount, "BRL"),
	}
	if kind.RequiresReference() {
		params.ReferenceExternalTransactionID = "transaction-122"
	}
	return params
}

// MustExternalTransaction creates a valid external operation, failing the test
// otherwise.
func MustExternalTransaction(t testing.TB, kind wager.Kind, amount string) *wager.Transaction {
	t.Helper()

	transaction, err := wager.NewExternal(ValidExternalParams(t, kind, amount))
	require.NoError(t, err)
	return transaction
}
