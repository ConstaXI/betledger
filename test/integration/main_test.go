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
	broker           *testenv.LocalStack
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

	for key, value := range map[string]string{
		"AWS_REGION":            "us-east-1",
		"AWS_ACCESS_KEY_ID":     "test",
		"AWS_SECRET_ACCESS_KEY": "test",
	} {
		_ = os.Setenv(key, value)
	}
	broker, err = testenv.StartLocalStack(context.Background())
	if err != nil {
		broker.Terminate()
		identityProvider.Terminate()
		database.Terminate()
		log.Fatalf("start localstack: %v", err)
	}

	binary, err = testenv.BuildBinary(context.Background())
	if err != nil {
		binary.Remove()
		broker.Terminate()
		identityProvider.Terminate()
		database.Terminate()
		log.Fatalf("build binary: %v", err)
	}

	code := m.Run()
	binary.Remove()
	broker.Terminate()
	identityProvider.Terminate()
	database.Terminate()
	os.Exit(code)
}
