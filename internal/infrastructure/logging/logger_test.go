package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// attached are the correlation identifiers attached to the context, in
		// the order they were attached.
		attached   []string
		wantRecord map[string]any
	}{
		{
			name:     "should report the correlation id when the context carries one",
			attached: []string{"req-1"},
			wantRecord: map[string]any{
				"level": "INFO", "msg": "message handled", "walletId": "w-1", "correlationId": "req-1",
			},
		},
		{
			name:       "should report no correlation id when the context carries none",
			wantRecord: map[string]any{"level": "INFO", "msg": "message handled", "walletId": "w-1"},
		},
		{
			name:       "should report no correlation id when the identifier is empty",
			attached:   []string{""},
			wantRecord: map[string]any{"level": "INFO", "msg": "message handled", "walletId": "w-1"},
		},
		{
			name:     "should report the last correlation id when the context was attached twice",
			attached: []string{"req-1", "req-2"},
			wantRecord: map[string]any{
				"level": "INFO", "msg": "message handled", "walletId": "w-1", "correlationId": "req-2",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var buffer bytes.Buffer
			ctx := context.Background()
			for _, id := range test.attached {
				ctx = WithCorrelationID(ctx, id)
			}

			New(&buffer).InfoContext(ctx, "message handled", "walletId", "w-1")

			record := map[string]any{}
			require.NoError(t, json.Unmarshal(buffer.Bytes(), &record))
			delete(record, slog.TimeKey)
			assert.Equal(t, test.wantRecord, record)
		})
	}
}

func TestCorrelationID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attached   []string
		wantResult string
	}{
		{name: "should report the identifier when the context carries one", attached: []string{"req-1"}, wantResult: "req-1"},
		{name: "should report nothing when the context carries none"},
		{name: "should report the last identifier when the context was attached twice", attached: []string{"req-1", "req-2"}, wantResult: "req-2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			for _, id := range test.attached {
				ctx = WithCorrelationID(ctx, id)
			}

			got := CorrelationID(ctx)

			assert.Equal(t, test.wantResult, got)
		})
	}
}
