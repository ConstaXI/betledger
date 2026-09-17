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
	binary           *testenv.Binary
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

	binary, err = testenv.BuildBinary(context.Background())
	if err != nil {
		binary.Remove()
		identityProvider.Terminate()
		database.Terminate()
		log.Fatalf("build binary: %v", err)
	}

	code := m.Run()
	binary.Remove()
	identityProvider.Terminate()
	database.Terminate()
	os.Exit(code)
}
