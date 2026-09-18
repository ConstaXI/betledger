// Package metrics records what the application does, through the OpenTelemetry
// metrics API, and exposes it in the Prometheus exposition format.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
)

var _ usecase.Metrics = (*Recorder)(nil)

// scopeName identifies the instrumentation these metrics come from.
const scopeName = "github.com/davibanfi/betledger"

// Instrument names. They are declared once because the views that set the
// histogram boundaries match instruments by name, and a typo there would
// silently leave a histogram with the default boundaries.
const (
	operationsName      = "betledger.wager.operations"
	wagerDurationName   = "betledger.wager.duration"
	messagesName        = "betledger.inbox.messages"
	deadLetteredName    = "betledger.messages.dead_lettered"
	attemptsName        = "betledger.reference.attempts"
	publicationsName    = "betledger.outbox.publications"
	outboxDelayName     = "betledger.outbox.delay"
	reconciliationsName = "betledger.wallet.reconciliations"
	conflictsName       = "betledger.concurrency.conflicts"
	requestsName        = "betledger.http.duration"
)

// Module provides the metrics stack: the registry the endpoint serves, the
// meter provider that feeds it and the recorder the application records
// through. The provider is flushed and shut down with the application, after
// the components that record through it.
var Module = fx.Module("metrics",
	fx.Provide(
		NewRegistry,
		NewMeterProvider,
		fx.Annotate(NewRecorder, fx.As(fx.Self()), fx.As(new(usecase.Metrics))),
	),
	fx.Invoke(registerLifecycle),
)

// NewRegistry builds the registry the metrics endpoint serves. It is the
// application's own, instead of the global one, so that nothing reaches the
// endpoint without passing through this package.
func NewRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// NewMeterProvider builds the provider that exports to the registry. Histograms
// measure seconds, so each one declares its boundaries: the default ones grow
// in thousands and would put every measurement in the first bucket.
func NewMeterProvider(registry *prometheus.Registry) (*sdkmetric.MeterProvider, error) {
	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registry),
		otelprometheus.WithoutTargetInfo(),
		otelprometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build the Prometheus exporter: %w", err)
	}

	latency := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	delay := []float64{0.1, 0.5, 1, 5, 15, 30, 60, 300}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithView(
			bucketView(wagerDurationName, latency),
			bucketView(requestsName, latency),
			bucketView(outboxDelayName, delay),
		),
	), nil
}

// bucketView sets the boundaries of one histogram, leaving the rest of its
// definition as declared.
func bucketView(instrument string, boundaries []float64) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: instrument},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: boundaries},
		},
	)
}

// Recorder records the instruments of the application. It satisfies
// usecase.Metrics, which the use cases report through, and adds what only the
// adapters see: requests, dead lettered messages.
type Recorder struct {
	operations      metric.Int64Counter
	wagerDuration   metric.Float64Histogram
	messages        metric.Int64Counter
	deadLettered    metric.Int64Counter
	attempts        metric.Int64Counter
	publications    metric.Int64Counter
	outboxDelay     metric.Float64Histogram
	reconciliations metric.Int64Counter
	conflicts       metric.Int64Counter
	requests        metric.Float64Histogram
}

// NewRecorder builds the instruments. A failure here is a programming error in
// their declaration, and it stops the application from starting rather than
// leaving it running blind.
func NewRecorder(provider *sdkmetric.MeterProvider) (*Recorder, error) {
	meter := provider.Meter(scopeName)
	var errs []error
	r := &Recorder{}

	var err error
	r.operations, err = meter.Int64Counter(operationsName,
		metric.WithDescription("Provider operations concluded, by kind, status and failure code."))
	errs = append(errs, err)
	r.wagerDuration, err = meter.Float64Histogram(wagerDurationName, metric.WithUnit("s"),
		metric.WithDescription("Time an operation took to be applied."))
	errs = append(errs, err)
	r.messages, err = meter.Int64Counter(messagesName,
		metric.WithDescription("Messages taken in from the queue, telling redeliveries apart."))
	errs = append(errs, err)
	r.deadLettered, err = meter.Int64Counter(deadLetteredName,
		metric.WithDescription("Messages sent to the dead letter queue."))
	errs = append(errs, err)
	r.attempts, err = meter.Int64Counter(attemptsName,
		metric.WithDescription("Attempts to resolve a pending reference, by the status they left."))
	errs = append(errs, err)
	r.publications, err = meter.Int64Counter(publicationsName,
		metric.WithDescription("Publications of recorded events, by outcome."))
	errs = append(errs, err)
	r.outboxDelay, err = meter.Float64Histogram(outboxDelayName, metric.WithUnit("s"),
		metric.WithDescription("Time an event waited in the outbox before being published."))
	errs = append(errs, err)
	r.reconciliations, err = meter.Int64Counter(reconciliationsName,
		metric.WithDescription("Reconciliations of a wallet, telling the diverging ones apart."))
	errs = append(errs, err)
	r.conflicts, err = meter.Int64Counter(conflictsName,
		metric.WithDescription("Writes refused because the row had changed since it was loaded."))
	errs = append(errs, err)
	r.requests, err = meter.Float64Histogram(requestsName, metric.WithUnit("s"),
		metric.WithDescription("Time a request took, by route and status."))
	errs = append(errs, err)

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("failed to build the instruments: %w", err)
	}
	return r, nil
}

// WagerConcluded records the operation and how long applying it took.
func (r *Recorder) WagerConcluded(ctx context.Context, outcome usecase.WagerOutcome) {
	r.operations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", outcome.Kind.String()),
		attribute.String("status", outcome.State.String()),
		attribute.String("failure_code", string(outcome.FailureCode)),
		attribute.Bool("replay", outcome.Replay),
	))
	r.wagerDuration.Record(ctx, outcome.Duration.Seconds(),
		metric.WithAttributes(attribute.String("status", outcome.State.String())))
}

// MessageTaken records a message taken in from the queue.
func (r *Recorder) MessageTaken(ctx context.Context, duplicate bool) {
	r.messages.Add(ctx, 1, metric.WithAttributes(attribute.Bool("duplicate", duplicate)))
}

// MessageDeadLettered records a message the consumer could not handle.
func (r *Recorder) MessageDeadLettered(ctx context.Context) {
	r.deadLettered.Add(ctx, 1)
}

// ReferenceAttempted records another attempt on an operation waiting for its
// reference.
func (r *Recorder) ReferenceAttempted(ctx context.Context, state wager.State) {
	r.attempts.Add(ctx, 1, metric.WithAttributes(attribute.String("status", state.String())))
}

// EventPublished records a publication and how long the event waited. A failed
// publication is one that will be tried again, which is what the retries of the
// outbox amount to.
func (r *Recorder) EventPublished(ctx context.Context, delay time.Duration, published bool) {
	r.publications.Add(ctx, 1, metric.WithAttributes(attribute.Bool("published", published)))
	r.outboxDelay.Record(ctx, delay.Seconds())
}

// ReconciliationChecked records a reconciliation and whether it diverged.
func (r *Recorder) ReconciliationChecked(ctx context.Context, consistent bool) {
	r.reconciliations.Add(ctx, 1, metric.WithAttributes(attribute.Bool("consistent", consistent)))
}

// ConcurrencyConflict records a write refused because the row had changed.
func (r *Recorder) ConcurrencyConflict(ctx context.Context) {
	r.conflicts.Add(ctx, 1)
}

// RequestHandled records how long a request took. The route is the pattern that
// matched, never the path, so that an identifier in the URL does not open a
// series of its own.
func (r *Recorder) RequestHandled(ctx context.Context, method, route string, status int, elapsed time.Duration) {
	r.requests.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("route", route),
		attribute.Int("status", status),
	))
}

// Route serves the metrics endpoint. It is public, as the health checks are: a
// scraper carries no token, and what it reads are counters and histograms,
// never a balance, an identifier or a payload.
type Route struct {
	registry *prometheus.Registry
}

// NewRoute builds the route.
func NewRoute(registry *prometheus.Registry) *Route {
	return &Route{registry: registry}
}

// Register mounts the metrics endpoint. The signature is the one httpserver.Route
// declares, which this package does not import, so that the server depends on
// the metrics and not the other way round.
func (r *Route) Register(mux *http.ServeMux) {
	mux.Handle("GET /metrics", promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{}))
}

func registerLifecycle(lc fx.Lifecycle, provider *sdkmetric.MeterProvider) {
	lc.Append(fx.Hook{
		OnStop: provider.Shutdown,
	})
}
