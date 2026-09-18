package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/infrastructure/metrics"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestRecorder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// record is what the application reports, as the use case or the
		// adapter that observed it would.
		record func(ctx context.Context, recorder *metrics.Recorder)
		// wantSeries are the lines the metrics endpoint must expose afterwards.
		wantSeries []string
	}{
		{
			name: "should report PROCESSED when an operation is applied",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.WagerConcluded(ctx, usecase.WagerOutcome{
					Kind:     wager.KindBet,
					State:    wager.StateProcessed,
					Duration: 25 * time.Millisecond,
				})
			},
			wantSeries: []string{
				`betledger_wager_operations_total{failure_code="",kind="BET",replay="false",status="PROCESSED"} 1`,
				`betledger_wager_duration_seconds_sum{status="PROCESSED"} 0.025`,
			},
		},
		{
			name: "should report the failure code when an operation is rejected",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.WagerConcluded(ctx, usecase.WagerOutcome{
					Kind:        wager.KindBet,
					State:       wager.StateRejected,
					FailureCode: domain.FailureCodeInsufficientFunds,
				})
			},
			wantSeries: []string{
				`betledger_wager_operations_total{failure_code="INSUFFICIENT_FUNDS",kind="BET",` +
					`replay="false",status="REJECTED"} 1`,
			},
		},
		{
			name: "should report the duplicate when an operation is replayed",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.WagerConcluded(ctx, usecase.WagerOutcome{
					Kind:   wager.KindWin,
					State:  wager.StateProcessed,
					Replay: true,
				})
			},
			wantSeries: []string{
				`betledger_wager_operations_total{failure_code="",kind="WIN",replay="true",status="PROCESSED"} 1`,
			},
		},
		{
			name: "should report the duplicate when the inbox already held the message",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.MessageTaken(ctx, true)
			},
			wantSeries: []string{`betledger_inbox_messages_total{duplicate="true"} 1`},
		},
		{
			name: "should report when a message is dead lettered",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.MessageDeadLettered(ctx)
			},
			wantSeries: []string{`betledger_messages_dead_lettered_total 1`},
		},
		{
			name: "should report the status when a reference is attempted again",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.ReferenceAttempted(ctx, wager.StatePendingReference)
			},
			wantSeries: []string{`betledger_reference_attempts_total{status="PENDING_REFERENCE"} 1`},
		},
		{
			name: "should report the delay when an event is published",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.EventPublished(ctx, 2*time.Second, true)
			},
			wantSeries: []string{
				`betledger_outbox_publications_total{published="true"} 1`,
				`betledger_outbox_delay_seconds_sum 2`,
			},
		},
		{
			name: "should report when a publication fails",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.EventPublished(ctx, time.Second, false)
			},
			wantSeries: []string{`betledger_outbox_publications_total{published="false"} 1`},
		},
		{
			name: "should report the divergence when a wallet does not match its ledger",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.ReconciliationChecked(ctx, false)
			},
			wantSeries: []string{`betledger_wallet_reconciliations_total{consistent="false"} 1`},
		},
		{
			name: "should report when a write finds the row changed",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.ConcurrencyConflict(ctx)
			},
			wantSeries: []string{`betledger_concurrency_conflicts_total 1`},
		},
		{
			name: "should report the route when a request is handled",
			record: func(ctx context.Context, recorder *metrics.Recorder) {
				recorder.RequestHandled(ctx, http.MethodPost, "POST /wagering/transactions",
					http.StatusCreated, 12*time.Millisecond)
			},
			wantSeries: []string{
				`betledger_http_duration_seconds_count{method="POST",` +
					`route="POST /wagering/transactions",status="201"} 1`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			registry := metrics.NewRegistry()
			provider, err := metrics.NewMeterProvider(registry)
			require.NoError(t, err)
			recorder, err := metrics.NewRecorder(provider)
			require.NoError(t, err)

			test.record(t.Context(), recorder)

			mux := http.NewServeMux()
			metrics.NewRoute(registry).Register(mux)
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			assert.Equal(t, http.StatusOK, response.Code)
			for _, series := range test.wantSeries {
				assert.Contains(t, response.Body.String(), series)
			}
		})
	}
}
