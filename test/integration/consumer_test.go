//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestConsumerAppliesOperationsAndDeletesTheMessageOnlyAfterTheCommit(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queues := broker.CreateWagerQueues(t)
	binary.StartWorkers(t, isolated.URL, broker, queues.Env()...)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	bet := testenv.BetInput(w, "from-queue", 2500)

	broker.SendWagerMessage(t, queues.URL, "msg-1", bet)

	assert.Eventually(t, func() bool {
		stored, _, _ := isolated.WalletState(t, w.ID())
		return stored == 7500
	}, 15*time.Second, 100*time.Millisecond, "the consumer must apply the operation of the message")
	assert.Equal(t, "PROCESSED", isolated.Operation(t, w, "from-queue").State)
	assert.Empty(t, broker.ReceiveBodies(t, queues.URL, 0, 2*time.Second),
		"the message must be deleted once its handling is committed")
	assert.Empty(t, broker.ReceiveBodies(t, queues.DeadLetterURL, 0, time.Second))
	isolated.AssertLedgerReconciles(t, w.ID())
}

func TestConsumerAppliesAnOperationOnceAcrossRedeliveriesAndHTTP(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queues := broker.CreateWagerQueues(t)
	binary.StartWorkers(t, isolated.URL, broker, queues.Env()...)
	application := binary.StartAPI(t, isolated.URL, identityProvider, broker)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	bet := testenv.BetInput(w, "shared", 2500)

	broker.SendWagerMessage(t, queues.URL, "msg-1", bet)
	broker.SendWagerMessage(t, queues.URL, "msg-1", bet)
	broker.SendWagerMessage(t, queues.URL, "msg-2", bet)
	assert.Eventually(t, func() bool {
		stored, _, _ := isolated.WalletState(t, w.ID())
		return stored == 7500
	}, 15*time.Second, 100*time.Millisecond, "the consumer must apply the operation")
	status, response := application.SendWager(t, bet)

	stored, version, debits := isolated.WalletState(t, w.ID())
	assert.Equal(t, http.StatusOK, status, "the same operation over HTTP is a replay")
	assert.True(t, response.IdempotentReplay)
	assert.Equal(t, "75.00", response.Balance.Amount)
	assert.Equal(t, int64(7500), stored, "the bet must be debited exactly once")
	assert.Equal(t, int64(2), version)
	assert.Equal(t, 1, debits)
	assert.Equal(t, 2, isolated.InboxMessages(t), "each message is taken in once")
	isolated.AssertLedgerReconciles(t, w.ID())
}

func TestConsumerSendsMessagesItCannotHandleToTheDeadLetterQueue(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queues := broker.CreateWagerQueues(t)
	binary.StartWorkers(t, isolated.URL, broker, queues.Env()...)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	unknownWallet := testenv.BetInput(w, "unknown-wallet", 2500)
	unknownWallet.WalletID = domain.NewID()
	reusedIdentifier := testenv.BetInput(w, "reused", 2500)
	other := testenv.BetInput(w, "other", 3000)

	broker.SendRawMessage(t, queues.URL, w.ID().String(), []byte(`{"messageId":`))
	broker.SendRawMessage(t, queues.URL, w.ID().String(), []byte(`{"messageId":"msg-x","type":"Unknown","data":{}}`))
	broker.SendWagerMessage(t, queues.URL, "msg-unknown-wallet", unknownWallet)
	broker.SendWagerMessage(t, queues.URL, "msg-reused", reusedIdentifier)
	broker.SendWagerMessage(t, queues.URL, "msg-reused", other)

	deadLettered := broker.ReceiveBodies(t, queues.DeadLetterURL, 4, 20*time.Second)
	stored, _, _ := isolated.WalletState(t, w.ID())
	assert.Len(t, deadLettered, 4, "malformed, unknown type, unknown wallet and reused identifier")
	assert.Empty(t, broker.ReceiveBodies(t, queues.URL, 0, 2*time.Second), "nothing may stay in the queue")
	assert.Equal(t, int64(7500), stored, "only the operation that could be handled moved money")
	assert.Equal(t, "PROCESSED", isolated.Operation(t, w, "reused").State)
}

func TestConsumerKeepsTheMessageWhileTheDatabaseIsDown(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queues := broker.CreateWagerQueues(t)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	bet := testenv.BetInput(w, "while-down", 2500)
	binary.StartWorkers(t, isolated.URL, broker, queues.Env()...)

	isolated.Pause(t)
	broker.SendWagerMessage(t, queues.URL, "msg-1", bet)
	time.Sleep(3 * time.Second)
	isolated.Unpause(t)

	assert.Eventually(t, func() bool {
		stored, _, _ := isolated.WalletState(t, w.ID())
		return stored == 7500
	}, 30*time.Second, 200*time.Millisecond, "the message must be handled once the database is back")
	assert.Empty(t, broker.ReceiveBodies(t, queues.DeadLetterURL, 0, 2*time.Second),
		"a transient failure must not send the message to the dead letter queue")
}
