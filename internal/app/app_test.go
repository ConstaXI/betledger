package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/app"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

func TestDependencyGraphIsComplete(t *testing.T) {
	t.Parallel()

	assert.NoError(t, fx.ValidateApp(fx.Provide(config.Load), app.Options()))
}
