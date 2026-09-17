package usecase_test

import (
	"testing"

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
		PlayerID:              domain.NewID(),
		WalletID:              domain.NewID(),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindBet,
		Money:                 money.MustNew(2500, money.MustCurrency("BRL")),
		CorrelationID:         "req-1",
	}
	baseHash, err := usecase.PayloadHash(base)
	require.NoError(t, err)
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

func TestPayloadHashIsStable(t *testing.T) {
	t.Parallel()

	input := usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "provider-a:transaction-123",
		PlayerID:              domain.ID{0x01, 0x92, 0xf2, 0x8f, 0x5d, 0xc0, 0x7d, 0x58, 0xbd, 0xb2, 0x81, 0x4a, 0xd6, 0xa0, 0xf4, 0xa1},
		WalletID:              domain.ID{0x01, 0x92, 0xf2, 0x91, 0x27, 0xdd, 0x7d, 0x3f, 0x80, 0x71, 0x5f, 0x86, 0x85, 0xde, 0xef, 0x37},
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindBet,
		Money:                 money.MustNew(2500, money.MustCurrency("BRL")),
	}

	got, err := usecase.PayloadHash(input)

	require.NoError(t, err)
	assert.Equal(t, "629836932b79106b99523d06a1e7fa80689b0ea1e1c47aa3f0a5a2c87d0c4344", got)
}
