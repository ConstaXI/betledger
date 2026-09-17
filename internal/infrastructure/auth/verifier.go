// Package auth validates the access tokens issued by the OpenID Connect
// identity provider.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

const identityProviderTimeout = 5 * time.Second

// Roles granted by the identity provider as realm roles.
const (
	RoleWalletOperator = "wallet-operator"
	RoleGameProvider   = "game-provider"
)

// Principal is the authenticated caller, as stated by a verified token.
type Principal struct {
	// Roles are the realm roles granted to the caller.
	Roles []string
	// ProviderID is the game provider the caller acts for, taken from the
	// provider_id claim; empty for callers that are not providers.
	ProviderID string
}

// HasRole reports whether the caller was granted the role.
func (p Principal) HasRole(role string) bool {
	return slices.Contains(p.Roles, role)
}

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
// application from starting instead of failing every request. The discovery
// document may come from an internal address, but the issuer it announces must
// still be the configured one.
func NewTokenVerifier(lc fx.Lifecycle, cfg config.Config) *TokenVerifier {
	client := &http.Client{Timeout: identityProviderTimeout}
	tokenVerifier := &TokenVerifier{}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			discoveryCtx := oidc.InsecureIssuerURLContext(oidc.ClientContext(ctx, client), cfg.OIDCIssuerURL)
			provider, err := oidc.NewProvider(discoveryCtx, cfg.OIDCDiscoveryURL)
			if err != nil {
				return fmt.Errorf("failed to discover the identity provider at %s: %w", cfg.OIDCDiscoveryURL, err)
			}
			var discovered struct {
				Issuer string `json:"issuer"`
			}
			if err := provider.Claims(&discovered); err != nil {
				return fmt.Errorf("failed to read the discovery document: %w", err)
			}
			if discovered.Issuer != cfg.OIDCIssuerURL {
				return fmt.Errorf("the identity provider announces issuer %s, expected %s",
					discovered.Issuer, cfg.OIDCIssuerURL)
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

// Verify checks the signature, issuer, audience and expiry of the raw token and
// returns the caller it identifies.
func (v *TokenVerifier) Verify(ctx context.Context, rawToken string) (Principal, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	var claims struct {
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
		ProviderID string `json:"provider_id"`
	}
	if err := token.Claims(&claims); err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	return Principal{Roles: claims.RealmAccess.Roles, ProviderID: claims.ProviderID}, nil
}
