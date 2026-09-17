//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestReadEndpointsAnswerTheStoredState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	application := binary.StartAPI(t, database.URL, identityProvider, broker)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	for bet := range 3 {
		_, err := useCases.ProcessWager.Execute(ctx, testenv.BetInput(w, fmt.Sprintf("bet-%d", bet), 1000))
		require.NoError(t, err)
	}

	walletStatus, walletBody := application.Read(t, "/wallets/"+w.ID().String(), "wallet-service")
	firstStatus, firstBody := application.Read(t, "/wallets/"+w.ID().String()+"/ledger?limit=2", "wallet-service")

	var wallet struct {
		ID      string            `json:"id"`
		Balance map[string]string `json:"balance"`
		Version int64             `json:"version"`
	}
	require.NoError(t, json.Unmarshal([]byte(walletBody), &wallet))
	var firstPage struct {
		Entries []struct {
			ID           string            `json:"id"`
			Direction    string            `json:"direction"`
			BalanceAfter map[string]string `json:"balanceAfter"`
		} `json:"entries"`
		NextCursor string `json:"nextCursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(firstBody), &firstPage))
	secondStatus, secondBody := application.Read(t,
		"/wallets/"+w.ID().String()+"/ledger?limit=2&cursor="+firstPage.NextCursor, "wallet-service")
	var secondPage struct {
		Entries []struct {
			ID           string            `json:"id"`
			Direction    string            `json:"direction"`
			BalanceAfter map[string]string `json:"balanceAfter"`
		} `json:"entries"`
		NextCursor string `json:"nextCursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(secondBody), &secondPage))

	assert.Equal(t, http.StatusOK, walletStatus)
	assert.Equal(t, w.ID().String(), wallet.ID)
	assert.Equal(t, "70.00", wallet.Balance["amount"])
	assert.Equal(t, int64(4), wallet.Version)

	assert.Equal(t, http.StatusOK, firstStatus)
	assert.Equal(t, http.StatusOK, secondStatus)
	assert.Len(t, firstPage.Entries, 2)
	assert.Len(t, secondPage.Entries, 2)
	assert.NotEmpty(t, firstPage.NextCursor, "a page that fills the limit points at the next one")
	assert.Empty(t, secondPage.NextCursor, "the last page points at nothing")
	assert.Equal(t, "CREDIT", firstPage.Entries[0].Direction, "the opening comes first")
	assert.Equal(t, "100.00", firstPage.Entries[0].BalanceAfter["amount"])
	assert.Equal(t, "70.00", secondPage.Entries[1].BalanceAfter["amount"], "the oldest entries come first")
	assert.NotEqual(t, firstPage.Entries[0].ID, secondPage.Entries[0].ID, "pages must not overlap")
}

func TestReconciliationReportsDivergenceWithoutChangingTheBalance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	application := binary.StartAPI(t, database.URL, identityProvider, broker)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	_, err := useCases.ProcessWager.Execute(ctx, testenv.BetInput(w, "bet", 2500))
	require.NoError(t, err)
	path := "/wallets/" + w.ID().String() + "/reconciliation"

	consistentStatus, consistentBody := application.Send(t, http.MethodPost, path, "wallet-service")
	database.DivergeWalletBalance(t, w.ID(), 1)
	divergentStatus, divergentBody := application.Send(t, http.MethodPost, path, "wallet-service")

	var consistent, divergent struct {
		StoredBalance     map[string]string `json:"storedBalance"`
		CalculatedBalance map[string]string `json:"calculatedBalance"`
		Difference        map[string]string `json:"difference"`
		Consistent        bool              `json:"consistent"`
		CheckedEntries    int               `json:"checkedEntries"`
	}
	require.NoError(t, json.Unmarshal([]byte(consistentBody), &consistent))
	require.NoError(t, json.Unmarshal([]byte(divergentBody), &divergent))

	assert.Equal(t, http.StatusOK, consistentStatus)
	assert.True(t, consistent.Consistent)
	assert.Equal(t, "75.00", consistent.StoredBalance["amount"])
	assert.Equal(t, "75.00", consistent.CalculatedBalance["amount"])
	assert.Equal(t, "0.00", consistent.Difference["amount"])
	assert.Equal(t, 2, consistent.CheckedEntries)

	assert.Equal(t, http.StatusOK, divergentStatus)
	assert.False(t, divergent.Consistent, "a balance the ledger does not explain must be reported")
	assert.Equal(t, "75.01", divergent.StoredBalance["amount"])
	assert.Equal(t, "75.00", divergent.CalculatedBalance["amount"])
	assert.Equal(t, "0.01", divergent.Difference["amount"])

	stored, _, _ := database.WalletState(t, w.ID())
	assert.Equal(t, int64(7501), stored, "reconciling never changes a balance")
}

func TestOperationQueriesAreIsolatedPerProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	application := binary.StartAPI(t, database.URL, identityProvider, broker)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	bet := testenv.BetInput(w, "queried", 2500)
	result, err := useCases.ProcessWager.Execute(ctx, bet)
	require.NoError(t, err)
	byID := "/wagering/transactions/" + result.TransactionID.String()
	byExternalID := "/providers/provider-a/wagering/transactions/" + bet.ExternalTransactionID

	ownerStatus, ownerBody := application.Read(t, byID, "provider-a")
	otherStatus, _ := application.Read(t, byID, "provider-b")
	externalStatus, _ := application.Read(t, byExternalID, "provider-a")
	foreignPathStatus, _ := application.Read(t, byExternalID, "provider-b")
	unknownStatus, _ := application.Read(t, "/wagering/transactions/"+domain.NewID().String(), "provider-a")

	var transaction struct {
		TransactionID string            `json:"transactionId"`
		Status        string            `json:"status"`
		Balance       map[string]string `json:"balance"`
		ProviderID    string            `json:"providerId"`
	}
	require.NoError(t, json.Unmarshal([]byte(ownerBody), &transaction))
	assert.Equal(t, http.StatusOK, ownerStatus)
	assert.Equal(t, result.TransactionID.String(), transaction.TransactionID)
	assert.Equal(t, "PROCESSED", transaction.Status)
	assert.Equal(t, "75.00", transaction.Balance["amount"])
	assert.Equal(t, "provider-a", transaction.ProviderID)

	assert.Equal(t, http.StatusNotFound, otherStatus, "another provider must not learn that it exists")
	assert.Equal(t, http.StatusOK, externalStatus)
	assert.Equal(t, http.StatusForbidden, foreignPathStatus, "the path provider must match the token")
	assert.Equal(t, http.StatusNotFound, unknownStatus)
}
