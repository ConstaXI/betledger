package usecase_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestPayloadHash(t *testing.T) {
	t.Parallel()

	base := usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "provider-a:transaction-123",
		PlayerID:              uuid.MustParse("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		WalletID:              uuid.MustParse("0192f291-27dd-7d3f-8071-5f8685deef37"),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindBet,
		Money:                 money.MustNew(2500, money.MustCurrency("BRL")),
		CorrelationID:         "req-1",
	}
	baseHash, err := usecase.PayloadHash(base)
	require.NoError(t, err)
	require.Equal(t, "629836932b79106b99523d06a1e7fa80689b0ea1e1c47aa3f0a5a2c87d0c4344", baseHash,
		"the documented hash of the base operation, computed independently, must not change")
	equivalentAmount, err := money.Parse("25", "BRL")
	require.NoError(t, err)

	tests := []struct {
		name     string
		mutate   func(input *usecase.ProcessWagerInput)
		wantSame bool
		wantErr  error
	}{
		{
			name:     "should report the same hash when only the idempotency key changes",
			mutate:   func(input *usecase.ProcessWagerInput) { input.IdempotencyKey = "another-key" },
			wantSame: true,
		},
		{
			name:     "should report the same hash when only the correlation id changes",
			mutate:   func(input *usecase.ProcessWagerInput) { input.CorrelationID = "another-request" },
			wantSame: true,
		},
		{
			name:     "should report the same hash when the amount is written in an equivalent form",
			mutate:   func(input *usecase.ProcessWagerInput) { input.Money = equivalentAmount },
			wantSame: true,
		},
		{
			name: "should report a different hash when the amount changes",
			mutate: func(input *usecase.ProcessWagerInput) {
				input.Money = money.MustNew(2501, money.MustCurrency("BRL"))
			},
		},
		{
			name:   "should report a different hash when the round changes",
			mutate: func(input *usecase.ProcessWagerInput) { input.RoundID = "round-988" },
		},
		{
			name:   "should report a different hash when a reference is added",
			mutate: func(input *usecase.ProcessWagerInput) { input.ReferenceExternalTransactionID = "transaction-122" },
		},
		{
			name:    "should return INVALID_INPUT when the money is uninitialized",
			mutate:  func(input *usecase.ProcessWagerInput) { input.Money = money.Money{} },
			wantErr: domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := base
			test.mutate(&input)

			got, err := usecase.PayloadHash(input)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantSame, got == baseHash)
		})
	}
}
