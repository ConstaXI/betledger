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
	database *testenv.Postgres
	useCases testenv.UseCases
)

func TestMain(m *testing.M) {
	started, err := testenv.StartPostgres(context.Background())
	if err != nil {
		started.Terminate()
		log.Fatalf("start postgres: %v", err)
	}
	database = started
	useCases = database.NewUseCases()

	code := m.Run()
	database.Terminate()
	os.Exit(code)
}
