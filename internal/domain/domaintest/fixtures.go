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

// MustProcessedReference returns a PROCESSED operation of the kind on the
// wallet, identified as transaction-122, the reference ValidExternalParams
// gives to reversals. It returns nil when kind is empty, for operations that
// refer to nothing.
func MustProcessedReference(t testing.TB, w *wallet.Wallet, kind wager.Kind, amount string) *wager.Transaction {
	t.Helper()

	if kind == "" {
		return nil
	}
	params := ValidExternalParams(t, kind, amount)
	params.ExternalTransactionID = "transaction-122"
	params.IdempotencyKey = "provider-a:transaction-122"
	if kind.RequiresReference() {
		params.ReferenceExternalTransactionID = "transaction-121"
	}
	params.WalletID = w.ID()
	params.PlayerID = w.PlayerID()
	reference, err := wager.NewExternal(params)
	require.NoError(t, err)
	require.NoError(t, reference.MarkProcessed(w.Balance()))
	return reference
}

// MustResolveReference resolves the operation to the reference, failing the
// test otherwise. It does nothing when reference is nil.
func MustResolveReference(t testing.TB, operation, reference *wager.Transaction) {
	t.Helper()

	if reference == nil {
		return
	}
	require.NoError(t, operation.ResolveReference(reference))
}

// MustRehydrateWallet rebuilds a wallet with the given state, failing the test
// otherwise.
func MustRehydrateWallet(t testing.TB, id, playerID domain.ID, balance string, version int64) *wallet.Wallet {
	t.Helper()

	rehydrated, err := wallet.Rehydrate(id, playerID, MustParseMoney(t, balance, "BRL"), version)
	require.NoError(t, err)
	return rehydrated
}

// MustRehydrateTransaction rebuilds an operation with the given state, failing
// the test otherwise.
func MustRehydrateTransaction(t testing.TB, params wager.RehydrateParams) *wager.Transaction {
	t.Helper()

	rehydrated, err := wager.Rehydrate(params)
	require.NoError(t, err)
	return rehydrated
}
