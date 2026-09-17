//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/test/testenv"
)

// The steps run in order: each one depends on the balance left by the previous.
func TestWageringEndpointAnswersEveryOutcome(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	unknownWallet := testenv.BetInput(w, "unknown-wallet", 1000)
	unknownWallet.WalletID = domain.NewID()
	conflicting := testenv.BetInput(w, "bet", 3000)

	steps := []struct {
		name            string
		kind            wager.Kind
		externalID      string
		amountMinor     int64
		wantStatus      int
		wantState       string
		wantAmount      string
		wantFailureCode string
		wantReplay      bool
	}{
		{
			name:        "should return 201 when a bet is applied",
			kind:        wager.KindBet,
			externalID:  "bet",
			amountMinor: 2500,
			wantStatus:  http.StatusCreated,
			wantState:   "PROCESSED",
			wantAmount:  "75.00",
		},
		{
			name:        "should return 201 when a win is applied",
			kind:        wager.KindWin,
			externalID:  "win",
			amountMinor: 1000,
			wantStatus:  http.StatusCreated,
			wantState:   "PROCESSED",
			wantAmount:  "85.00",
		},
		{
			name:        "should return 201 when a loss is recorded",
			kind:        wager.KindLoss,
			externalID:  "loss",
			amountMinor: 0,
			wantStatus:  http.StatusCreated,
			wantState:   "PROCESSED",
			wantAmount:  "85.00",
		},
		{
			name:        "should return 200 when the bet is sent again",
			kind:        wager.KindBet,
			externalID:  "bet",
			amountMinor: 2500,
			wantStatus:  http.StatusOK,
			wantState:   "PROCESSED",
			wantAmount:  "75.00",
			wantReplay:  true,
		},
		{
			name:            "should return 422 INSUFFICIENT_FUNDS when the bet exceeds the balance",
			kind:            wager.KindBet,
			externalID:      "too-large",
			amountMinor:     9000,
			wantStatus:      http.StatusUnprocessableEntity,
			wantState:       "REJECTED",
			wantFailureCode: "INSUFFICIENT_FUNDS",
		},
		{
			name:            "should return 422 INSUFFICIENT_FUNDS when the rejected bet is sent again",
			kind:            wager.KindBet,
			externalID:      "too-large",
			amountMinor:     9000,
			wantStatus:      http.StatusUnprocessableEntity,
			wantState:       "REJECTED",
			wantFailureCode: "INSUFFICIENT_FUNDS",
			wantReplay:      true,
		},
	}

	for _, step := range steps {
		status, body := application.SendWager(t, testenv.WagerInput(w, step.kind, step.externalID, step.amountMinor))

		assert.Equal(t, step.wantStatus, status, step.name)
		assert.Equal(t, step.wantState, body.Status, step.name)
		assert.Equal(t, step.wantAmount, body.Balance.Amount, step.name)
		assert.Equal(t, step.wantFailureCode, body.FailureCode, step.name)
		assert.Equal(t, step.wantReplay, body.IdempotentReplay, step.name)
	}

	conflictStatus, conflictBody := application.SendWager(t, conflicting)
	assert.Equal(t, http.StatusConflict, conflictStatus)
	assert.Equal(t, "IDEMPOTENCY_CONFLICT", conflictBody.Error.Code)

	notFoundStatus, notFoundBody := application.SendWager(t, unknownWallet)
	assert.Equal(t, http.StatusNotFound, notFoundStatus)
	assert.Equal(t, "WALLET_NOT_FOUND", notFoundBody.Error.Code)

	balance, version, _ := database.WalletState(t, w.ID())
	assert.Equal(t, int64(8500), balance)
	assert.Equal(t, int64(3), version)
	database.AssertLedgerReconciles(t, w.ID())
}
