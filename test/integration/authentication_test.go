//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestApplicationRequiresAValidAccessToken(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider)
	walletServiceToken := strings.Split(identityProvider.Token(t, "wallet-service"), ".")
	providerToken := strings.Split(identityProvider.Token(t, "provider-a"), ".")
	forged := walletServiceToken[0] + "." + walletServiceToken[1] + "." + providerToken[2]

	tests := []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{
			name:          "should accept when the token was issued by the identity provider",
			authorization: identityProvider.Bearer(t, "wallet-service"),
			wantStatus:    http.StatusCreated,
		},
		{
			name:       "should return UNAUTHENTICATED when the token is missing",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "should return UNAUTHENTICATED when the token is not a JWT",
			authorization: "Bearer not-a-token",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "should return UNAUTHENTICATED when the signature belongs to another token",
			authorization: "Bearer " + forged,
			wantStatus:    http.StatusUnauthorized,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			status := application.OpenWalletAs(t, domain.NewID().String(), test.authorization)

			assert.Equal(t, test.wantStatus, status)
		})
	}
}

func TestApplicationRejectsTokensIssuedForAnotherAudience(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider, func(cfg *config.Config) {
		cfg.OIDCAudience = "another-api"
	})

	status := application.OpenWalletAs(t, domain.NewID().String(), identityProvider.Bearer(t, "wallet-service"))

	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestApplicationAuthorizesByRoleAndProvider(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	bet := testenv.BetInput(w, "authorization", 2500)

	walletStatus := application.OpenWalletAs(t, domain.NewID().String(), identityProvider.Bearer(t, "provider-a"))
	walletServiceStatus, _ := application.SendWagerAs(t, bet, identityProvider.Bearer(t, "wallet-service"))
	impersonationStatus, impersonation := application.SendWagerAs(t, bet, identityProvider.Bearer(t, "provider-b"))

	assert.Equal(t, http.StatusForbidden, walletStatus, "a game provider must not open wallets")
	assert.Equal(t, http.StatusForbidden, walletServiceStatus, "the wallet service must not send operations")
	assert.Equal(t, http.StatusForbidden, impersonationStatus, "provider-b must not act as provider-a")
	assert.Equal(t, "FORBIDDEN", impersonation.Error.Code)
	balance, _, _ := database.WalletState(t, w.ID())
	assert.Equal(t, int64(10000), balance, "no forbidden request may move money")
}

func TestProvidersNeverSeeEachOthersOperations(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	fromA := testenv.BetInput(w, "shared-identifiers", 2500)
	fromB := fromA
	fromB.ProviderID = "provider-b"

	statusA, resultA := application.SendWager(t, fromA)
	statusB, resultB := application.SendWager(t, fromB)

	assert.Equal(t, http.StatusCreated, statusA)
	assert.Equal(t, http.StatusCreated, statusB,
		"the same key and external id under another provider is a new operation, not a replay")
	assert.False(t, resultB.IdempotentReplay)
	assert.NotEqual(t, resultA.TransactionID, resultB.TransactionID)
	assert.Equal(t, "50.00", resultB.Balance.Amount)
}
