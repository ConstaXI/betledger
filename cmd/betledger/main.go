// Command betledger starts the application, composing its dependencies via Fx.
package main

import (
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/config"
	"github.com/davibanfi/betledger/internal/httpserver"
)

func main() {
	fx.New(
		fx.Provide(config.Load),
		httpserver.Module,
	).Run()
}
