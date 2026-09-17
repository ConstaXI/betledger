//go:build integration

package integration

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestApplicationAuthorizesWalletOpening(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider, broker)
	anotherAudience := testenv.StartApplication(t, database.URL, identityProvider, broker, func(cfg *config.Config) {
		cfg.OIDCAudience = "another-api"
	})
	walletServiceToken := strings.Split(identityProvider.Token(t, "wallet-service"), ".")
	providerToken := strings.Split(identityProvider.Token(t, "provider-a"), ".")
	forged := walletServiceToken[0] + "." + walletServiceToken[1] + "." + providerToken[2]

	tests := []struct {
		name          string
		application   *testenv.Application
		authorization string
		wantStatus    int
	}{
		{
			name:          "should accept when the internal wallet service holds a valid token",
			application:   application,
			authorization: identityProvider.Bearer(t, "wallet-service"),
			wantStatus:    http.StatusCreated,
		},
		{
			name:        "should return UNAUTHENTICATED when the token is missing",
			application: application,
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:          "should return UNAUTHENTICATED when the token is not a JWT",
			application:   application,
			authorization: "Bearer not-a-token",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "should return UNAUTHENTICATED when the signature belongs to another token",
			application:   application,
			authorization: "Bearer " + forged,
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "should return UNAUTHENTICATED when the token was issued for another audience",
			application:   anotherAudience,
			authorization: identityProvider.Bearer(t, "wallet-service"),
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "should return FORBIDDEN when a game provider opens a wallet",
			application:   application,
			authorization: identityProvider.Bearer(t, "provider-a"),
			wantStatus:    http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			status := test.application.OpenWalletAs(t, domain.NewID().String(), test.authorization)

			assert.Equal(t, test.wantStatus, status)
		})
	}
}

func TestApplicationAuthorizesOperations(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider, broker)
	fromProvider := func(providerID string) func(w *wallet.Wallet) usecase.ProcessWagerInput {
		return func(w *wallet.Wallet) usecase.ProcessWagerInput {
			input := testenv.BetInput(w, "shared-identifiers", 2500)
			input.ProviderID = providerID
			return input
		}
	}

	tests := []struct {
		name          string
		earlier       []func(w *wallet.Wallet) usecase.ProcessWagerInput
		input         func(w *wallet.Wallet) usecase.ProcessWagerInput
		clientID      string
		wantStatus    int
		wantCode      string
		wantReplay    bool
		wantBalance   int64
		wantEarlierID bool
	}{
		{
			name:        "should accept when a provider acts for itself",
			input:       fromProvider("provider-a"),
			clientID:    "provider-a",
			wantStatus:  http.StatusCreated,
			wantBalance: 7500,
		},
		{
			name:        "should return FORBIDDEN when the internal wallet service sends an operation",
			input:       fromProvider("provider-a"),
			clientID:    "wallet-service",
			wantStatus:  http.StatusForbidden,
			wantCode:    "FORBIDDEN",
			wantBalance: 10000,
		},
		{
			name:        "should return FORBIDDEN when a provider acts for another one",
			input:       fromProvider("provider-a"),
			clientID:    "provider-b",
			wantStatus:  http.StatusForbidden,
			wantCode:    "FORBIDDEN",
			wantBalance: 10000,
		},
		{
			name:        "should accept when another provider reuses the same identifiers, as a new operation",
			earlier:     []func(w *wallet.Wallet) usecase.ProcessWagerInput{fromProvider("provider-a")},
			input:       fromProvider("provider-b"),
			clientID:    "provider-b",
			wantStatus:  http.StatusCreated,
			wantBalance: 5000,
		},
		{
			name:          "should report a replay when the same provider resends its operation",
			earlier:       []func(w *wallet.Wallet) usecase.ProcessWagerInput{fromProvider("provider-a")},
			input:         fromProvider("provider-a"),
			clientID:      "provider-a",
			wantStatus:    http.StatusOK,
			wantReplay:    true,
			wantBalance:   7500,
			wantEarlierID: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
			var earlierIDs []string
			for _, earlier := range test.earlier {
				_, response := application.SendWager(t, earlier(w))
				earlierIDs = append(earlierIDs, response.TransactionID)
			}

			status, response := application.SendWagerAs(t, test.input(w), identityProvider.Bearer(t, test.clientID))

			balance, _, _ := database.WalletState(t, w.ID())
			assert.Equal(t, test.wantStatus, status)
			assert.Equal(t, test.wantCode, response.Error.Code)
			assert.Equal(t, test.wantReplay, response.IdempotentReplay)
			assert.Equal(t, test.wantEarlierID, slices.Contains(earlierIDs, response.TransactionID))
			assert.Equal(t, test.wantBalance, balance)
		})
	}
}
