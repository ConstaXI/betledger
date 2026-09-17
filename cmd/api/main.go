// Command api serves the HTTP endpoints of betledger, composing its
// dependencies via Fx.
package main

import (
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/app"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

func main() {
	fx.New(fx.Provide(config.Load), app.API()).Run()
}
