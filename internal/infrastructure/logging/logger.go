// Package logging builds the application logger and carries, through the
// context, the identifier that ties together the records of one operation.
package logging

import (
	"context"
	"io"
	"log/slog"
)

// correlationField is the attribute under which the correlation identifier is
// logged, in every record of an operation.
const correlationField = "correlationId"

// New builds the JSON logger of the application. Records logged with a context
// carrying a correlation identifier are stamped with it, so that no call site
// repeats it.
func New(w io.Writer) *slog.Logger {
	return slog.New(&contextHandler{next: slog.NewJSONHandler(w, nil)})
}

type correlationKey struct{}

// WithCorrelationID attaches to the context the identifier that traces one
// operation, from the entry that received it to the events it produces.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID returns the identifier attached to the context, and an empty
// string when there is none, as in work that no request started.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

// contextHandler stamps each record with the correlation identifier of the
// context it was logged with. Records logged without a context, through the
// methods that take none, carry no identifier.
type contextHandler struct {
	next slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		record.AddAttrs(slog.String(correlationField, id))
	}
	return h.next.Handle(ctx, record)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}
