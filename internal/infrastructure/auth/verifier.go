// Package auth validates the access tokens issued by the OpenID Connect
// identity provider.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

const identityProviderTimeout = 5 * time.Second

// ErrInvalidToken reports a token that is malformed, expired, not signed by the
// identity provider, or issued for another issuer or audience.
var ErrInvalidToken = errors.New("invalid access token")

// TokenVerifier checks access tokens against the signing keys published by
// the identity provider, which are cached and refreshed when an unknown key
// appears.
type TokenVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewTokenVerifier builds the verifier. The identity provider is discovered on
// start, so that an unreachable or misconfigured provider prevents the
// application from starting instead of failing every request.
func NewTokenVerifier(lc fx.Lifecycle, cfg config.Config) *TokenVerifier {
	client := &http.Client{Timeout: identityProviderTimeout}
	tokenVerifier := &TokenVerifier{}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), cfg.OIDCIssuerURL)
			if err != nil {
				return fmt.Errorf("failed to discover the identity provider %s: %w", cfg.OIDCIssuerURL, err)
			}
			tokenVerifier.verifier = provider.VerifierContext(
				oidc.ClientContext(context.Background(), client),
				&oidc.Config{ClientID: cfg.OIDCAudience},
			)
			return nil
		},
	})
	return tokenVerifier
}

// Verify checks the signature, issuer, audience and expiry of the raw token.
func (v *TokenVerifier) Verify(ctx context.Context, rawToken string) error {
	if _, err := v.verifier.Verify(ctx, rawToken); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return nil
}
