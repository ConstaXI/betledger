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

	tests := []struct {
		name    string
		options fx.Option
	}{
		{name: "should accept when the API is composed", options: app.API()},
		{name: "should accept when the workers are composed", options: app.Workers()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.NoError(t, fx.ValidateApp(fx.Provide(config.Load), test.options))
		})
	}
}
