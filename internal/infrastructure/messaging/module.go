package messaging

import (
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/usecase"
)

// Module provides the SQS client, the publisher of the outbox events and the
// consumer of the wagering queue, with their queues resolved on start.
var Module = fx.Module("messaging",
	fx.Provide(
		NewClient,
		fx.Annotate(NewEventPublisher, fx.As(fx.Self()), fx.As(new(usecase.EventPublisher))),
		NewWagerConsumer,
	),
)
