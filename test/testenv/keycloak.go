//go:build integration

package testenv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Audience is the audience the realm adds to every access token.
const Audience = "betledger-api"

// Keycloak is a disposable Keycloak container with the realm used by the
// application imported, the same one Docker Compose imports.
type Keycloak struct {
	// IssuerURL is the issuer of the tokens, reachable from the host.
	IssuerURL string
	container *testcontainers.DockerContainer
}

// StartKeycloak starts the container and waits until the realm is served. The
// caller must call Terminate when done.
func StartKeycloak(ctx context.Context) (*Keycloak, error) {
	_, source, _, _ := runtime.Caller(0)
	realm := filepath.Join(filepath.Dir(source), "..", "..", "deploy", "keycloak", "betledger-realm.json")

	container, err := testcontainers.Run(ctx, "quay.io/keycloak/keycloak:26.4",
		testcontainers.WithCmd("start-dev", "--import-realm"),
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      realm,
			ContainerFilePath: "/opt/keycloak/data/import/betledger-realm.json",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/realms/betledger/.well-known/openid-configuration").
				WithPort("8080/tcp").
				WithStartupTimeout(3*time.Minute),
		),
	)
	identityProvider := &Keycloak{container: container}
	if err != nil {
		return identityProvider, err
	}

	endpoint, err := container.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		return identityProvider, err
	}
	identityProvider.IssuerURL = endpoint + "/realms/betledger"
	return identityProvider, nil
}

// Terminate removes the container.
func (k *Keycloak) Terminate() {
	if k == nil || k.container == nil {
		return
	}
	_ = testcontainers.TerminateContainer(k.container)
}

// Token obtains an access token for the realm client through the
// client_credentials grant.
func (k *Keycloak) Token(t *testing.T, clientID string) string {
	t.Helper()

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientID + "-secret"},
	}
	response, err := httpClient().Post(k.IssuerURL+"/protocol/openid-connect/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode, "the realm must issue a token to %s", clientID)

	var body struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	return body.AccessToken
}

// Bearer returns the Authorization header value for a token of the client.
func (k *Keycloak) Bearer(t *testing.T, clientID string) string {
	t.Helper()

	return fmt.Sprintf("Bearer %s", k.Token(t, clientID))
}
