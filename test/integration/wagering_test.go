//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

// The steps run in order: each one depends on the balance left by the previous.
func TestWageringEndpointAnswersEveryOutcome(t *testing.T) {
	t.Parallel()

	application := testenv.StartApplication(t, database.URL, identityProvider, broker)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	unknownWallet := testenv.BetInput(w, "unknown-wallet", 1000)
	unknownWallet.WalletID = domain.NewID()
	conflicting := testenv.BetInput(w, "bet", 3000)

	steps := []struct {
		name            string
		input           usecase.ProcessWagerInput
		wantStatus      int
		wantState       string
		wantAmount      string
		wantFailureCode string
		wantReplay      bool
	}{
		{
			name:       "should return 201 when a bet is applied",
			input:      testenv.WagerInput(w, wager.KindBet, "bet", 2500),
			wantStatus: http.StatusCreated,
			wantState:  "PROCESSED",
			wantAmount: "75.00",
		},
		{
			name:       "should return 201 when a win is applied",
			input:      testenv.WagerInput(w, wager.KindWin, "win", 1000),
			wantStatus: http.StatusCreated,
			wantState:  "PROCESSED",
			wantAmount: "85.00",
		},
		{
			name:       "should return 201 when a loss is recorded",
			input:      testenv.WagerInput(w, wager.KindLoss, "loss", 0),
			wantStatus: http.StatusCreated,
			wantState:  "PROCESSED",
			wantAmount: "85.00",
		},
		{
			name:       "should return 200 when the bet is sent again",
			input:      testenv.WagerInput(w, wager.KindBet, "bet", 2500),
			wantStatus: http.StatusOK,
			wantState:  "PROCESSED",
			wantAmount: "75.00",
			wantReplay: true,
		},
		{
			name:       "should return 201 when the win is rolled back",
			input:      testenv.ReferringInput(w, wager.KindRollback, "rollback-win", "win", 1000),
			wantStatus: http.StatusCreated,
			wantState:  "PROCESSED",
			wantAmount: "75.00",
		},
		{
			name:       "should return 202 when a refund arrives before its bet",
			input:      testenv.ReferringInput(w, wager.KindRefund, "early-refund", "late-bet", 1000),
			wantStatus: http.StatusAccepted,
			wantState:  "PENDING_REFERENCE",
		},
		{
			name:            "should return 422 INSUFFICIENT_FUNDS when the bet exceeds the balance",
			input:           testenv.WagerInput(w, wager.KindBet, "too-large", 9000),
			wantStatus:      http.StatusUnprocessableEntity,
			wantState:       "REJECTED",
			wantFailureCode: "INSUFFICIENT_FUNDS",
		},
		{
			name:            "should return 422 INSUFFICIENT_FUNDS when the rejected bet is sent again",
			input:           testenv.WagerInput(w, wager.KindBet, "too-large", 9000),
			wantStatus:      http.StatusUnprocessableEntity,
			wantState:       "REJECTED",
			wantFailureCode: "INSUFFICIENT_FUNDS",
			wantReplay:      true,
		},
	}

	for _, step := range steps {
		status, body := application.SendWager(t, step.input)

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
	assert.Equal(t, int64(7500), balance)
	assert.Equal(t, int64(4), version)
	database.AssertLedgerReconciles(t, w.ID())
}
