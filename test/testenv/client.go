//go:build integration

package testenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/usecase"
)

// Client calls a running instance of the application over HTTP, authenticating
// with tokens from the identity provider.
type Client struct {
	// BaseURL reaches the HTTP server, such as http://127.0.0.1:41234.
	BaseURL          string
	identityProvider *Keycloak
}

// Get returns the status of a GET to the path.
func (c *Client) Get(t *testing.T, path string) int {
	t.Helper()

	response, err := httpClient().Get(c.BaseURL + path)
	require.NoError(t, err)
	defer response.Body.Close()
	return response.StatusCode
}

// Scrape returns the body of the metrics endpoint, which needs no token,
// because a scraper carries none.
func (c *Client) Scrape(t *testing.T) string {
	t.Helper()

	return scrape(t, c.BaseURL)
}

func scrape(t *testing.T, baseURL string) string {
	t.Helper()

	response, err := httpClient().Get(baseURL + "/metrics")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return string(body)
}

// IsListening reports whether the server still accepts connections.
func (c *Client) IsListening() bool {
	response, err := (&http.Client{Timeout: time.Second}).Get(c.BaseURL + "/health/live")
	if err != nil {
		return false
	}
	response.Body.Close()
	return true
}

// OpenWallet returns the status of opening a 100.00 BRL wallet for the player,
// authenticated as the internal wallet service.
func (c *Client) OpenWallet(t *testing.T, playerID string) int {
	t.Helper()

	return c.OpenWalletAs(t, playerID, c.identityProvider.Bearer(t, "wallet-service"))
}

// OpenWalletAs returns the status of opening a 100.00 BRL wallet for the player
// with the given Authorization header, which may be empty.
func (c *Client) OpenWalletAs(t *testing.T, playerID, authorization string) int {
	t.Helper()

	body := fmt.Sprintf(`{"playerId":%q,"initialBalance":{"amount":"100.00","currency":"BRL"}}`, playerID)
	request, err := http.NewRequest(http.MethodPost, c.BaseURL+"/wallets", strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", authorization)

	response, err := httpClient().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	return response.StatusCode
}

// WagerResponse is the body answered to an operation.
type WagerResponse struct {
	TransactionID string `json:"transactionId"`
	Status        string `json:"status"`
	// Balance is left empty when the response has none.
	Balance struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"balance"`
	FailureCode      string `json:"failureCode"`
	IdempotentReplay bool   `json:"idempotentReplay"`
	Error            struct {
		Code string `json:"code"`
	} `json:"error"`
}

// SendWager posts the operation to /wagering/transactions, authenticated as
// the realm client named by its providerId, and returns the status and the
// decoded body.
func (c *Client) SendWager(t *testing.T, input usecase.ProcessWagerInput) (int, WagerResponse) {
	t.Helper()

	return c.SendWagerAs(t, input, c.identityProvider.Bearer(t, input.ProviderID))
}

// SendWagerAs posts the operation with the given Authorization header, which
// may be empty.
func (c *Client) SendWagerAs(
	t *testing.T,
	input usecase.ProcessWagerInput,
	authorization string,
) (int, WagerResponse) {
	t.Helper()

	status, body, err := c.PostWager(input, authorization)
	require.NoError(t, err)
	return status, body
}

// PostWager posts the operation with the given Authorization header. Unlike
// SendWagerAs it reports failures as an error, so it can be called from
// goroutines other than the test's.
func (c *Client) PostWager(input usecase.ProcessWagerInput, authorization string) (int, WagerResponse, error) {
	payload, err := json.Marshal(map[string]any{
		"providerId":                     input.ProviderID,
		"externalTransactionId":          input.ExternalTransactionID,
		"playerId":                       input.PlayerID,
		"walletId":                       input.WalletID,
		"roundId":                        input.RoundID,
		"gameId":                         input.GameID,
		"kind":                           input.Kind,
		"money":                          input.Money,
		"referenceExternalTransactionId": input.ReferenceExternalTransactionID,
	})
	if err != nil {
		return 0, WagerResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, c.BaseURL+"/wagering/transactions", bytes.NewReader(payload))
	if err != nil {
		return 0, WagerResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", input.IdempotencyKey)
	request.Header.Set("Authorization", authorization)
	request.Header.Set("X-Correlation-Id", input.CorrelationID)

	response, err := httpClient().Do(request)
	if err != nil {
		return 0, WagerResponse{}, err
	}
	defer response.Body.Close()
	var body WagerResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return response.StatusCode, WagerResponse{}, err
	}
	return response.StatusCode, body, nil
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// Read sends a GET authenticated as the realm client and returns the status and
// the body, for the endpoints the tests only inspect.
func (c *Client) Read(t *testing.T, path, clientID string) (int, string) {
	t.Helper()

	return c.Send(t, http.MethodGet, path, clientID)
}

// Send sends a request with no body, authenticated as the realm client.
func (c *Client) Send(t *testing.T, method, path, clientID string) (int, string) {
	t.Helper()

	request, err := http.NewRequest(method, c.BaseURL+path, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", c.identityProvider.Bearer(t, clientID))

	response, err := httpClient().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(body)
}
