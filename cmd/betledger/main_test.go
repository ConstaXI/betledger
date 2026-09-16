package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
)

func TestDependencyGraphIsComplete(t *testing.T) {
	t.Parallel()

	assert.NoError(t, fx.ValidateApp(options()))
}
