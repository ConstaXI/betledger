//go:build integration

package integration

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/davibanfi/betledger/test/testenv"
)

var (
	database         *testenv.Postgres
	identityProvider *testenv.Keycloak
	useCases         testenv.UseCases
)

func TestMain(m *testing.M) {
	started, err := testenv.StartPostgres(context.Background())
	if err != nil {
		started.Terminate()
		log.Fatalf("start postgres: %v", err)
	}
	database = started
	useCases = database.NewUseCases()

	identityProvider, err = testenv.StartKeycloak(context.Background())
	if err != nil {
		identityProvider.Terminate()
		database.Terminate()
		log.Fatalf("start keycloak: %v", err)
	}

	code := m.Run()
	identityProvider.Terminate()
	database.Terminate()
	os.Exit(code)
}
