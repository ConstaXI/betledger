// Command workers runs the background workers of betledger: the retry of
// operations waiting for a reference and the publication of the outbox. It
// serves no HTTP, and several instances may run at once.
package main

import (
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/app"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

func main() {
	fx.New(fx.Provide(config.Load), app.Workers()).Run()
}
