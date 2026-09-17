package worker

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx/fxtest"
)

type fakeResolver struct {
	delay     time.Duration
	started   chan struct{}
	startOnce sync.Once
	mu        sync.Mutex
	calls     int
	cancelled bool
}

func (f *fakeResolver) Execute(ctx context.Context) (int, error) {
	f.startOnce.Do(func() { close(f.started) })
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	select {
	case <-time.After(f.delay):
		return 0, nil
	case <-ctx.Done():
		f.mu.Lock()
		f.cancelled = true
		f.mu.Unlock()
		return 0, ctx.Err()
	}
}

func TestPendingReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		attemptDelay  time.Duration
		stopTimeout   time.Duration
		wantErr       error
		wantCancelled bool
	}{
		{
			name:         "should accept when the worker is idle on stop",
			attemptDelay: 0,
			stopTimeout:  time.Second,
		},
		{
			name:         "should accept when the attempt in progress ends within the stop deadline",
			attemptDelay: 100 * time.Millisecond,
			stopTimeout:  2 * time.Second,
		},
		{
			name:          "should return DeadlineExceeded when the attempt outlives the stop deadline, cancelling it",
			attemptDelay:  time.Minute,
			stopTimeout:   50 * time.Millisecond,
			wantErr:       context.DeadlineExceeded,
			wantCancelled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeResolver{delay: test.attemptDelay, started: make(chan struct{})}
			lc := fxtest.NewLifecycle(t)
			worker := newPendingReferences(lc, resolver, 10*time.Millisecond, slog.New(slog.DiscardHandler))
			lc.RequireStart()
			<-resolver.started
			ctx, cancel := context.WithTimeout(context.Background(), test.stopTimeout)
			defer cancel()

			err := lc.Stop(ctx)

			returned := false
			select {
			case <-worker.done:
				returned = true
			default:
			}
			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantCancelled, resolver.cancelled)
			assert.Positive(t, resolver.calls)
			assert.True(t, returned, "the worker must have returned once stopped")
		})
	}
}
