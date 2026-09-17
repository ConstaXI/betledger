//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
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
