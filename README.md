# betledger

Serviço de processamento distribuído de operações financeiras de provedores de
jogos, em Go com Uber Fx. O escopo completo do desafio está em [SPECS.md](SPECS.md),
e as decisões técnicas em [ARCHITECTURE.md](ARCHITECTURE.md).

## Estado atual

O projeto está em construção incremental. O que existe hoje:

- **Modelo de domínio** — `Money`, `Wallet`, `WagerTransaction`,
  `WalletLedgerEntry` e os eventos de integração.
- **Abertura de carteira de ponta a ponta** — `POST /wallets`, use case,
  PostgreSQL com migrations e outbox transacional.
- **Os cinco tipos de operação** — `BET`, `WIN`, `LOSS`, `REFUND` e `ROLLBACK`
  em `POST /wagering/transactions`, com idempotência persistente, lock por
  carteira e no máximo uma reversão por operação, imposta também pelo banco.
- **Autenticação e autorização** — Keycloak no Compose; os endpoints de negócio
  exigem um access token `client_credentials` com o papel adequado, e cada
  provedor só age em nome próprio.
- **Aplicação em container** — `docker compose up --build` sobe tudo, com as
  migrations aplicadas antes da aplicação.
- **Publicação de eventos** — um worker publica a outbox na fila FIFO
  `wallet-events.fifo`, no LocalStack, depois do commit, na ordem de cada carteira.
- **Health checks** — liveness em `GET /health/live` e readiness, que checa o
  banco e o SQS, em `GET /health/ready`.

Ainda **não** existem: o consumidor SQS com inbox e DLQ e as rotas de leitura. A seção
[Próximos passos](#próximos-passos) lista a ordem prevista.

## Pré-requisitos

- **Docker com o plugin Compose.** Basta ele para executar a aplicação.
- **Go 1.27.1 ou superior**, a versão declarada em [go.mod](go.mod) e no
  [Dockerfile](Dockerfile), e **make**, para desenvolver e rodar os testes.

O sqlc não precisa ser instalado: ele é uma tool do módulo e roda com
`go tool sqlc`.

## Executar

A partir de um checkout limpo:

```sh
docker compose up --build
```

O Compose sobe o PostgreSQL, o Keycloak e o LocalStack, com as filas criadas por
[deploy/localstack/create-queues.sh](deploy/localstack/create-queues.sh), espera
os três ficarem saudáveis,
aplica as migrations num container de execução única (`migrate`) e só então
inicia a aplicação em http://localhost:8080. A imagem é multi-stage e roda um
binário estático sobre `distroless`, sem shell e como usuário sem privilégios.

Para desenvolver, a aplicação pode rodar direto no host, contra os mesmos
containers:

```sh
cp .env.example .env
make dev
```

O `make dev` sobe o PostgreSQL, o Keycloak e o LocalStack, aguarda os três ficarem saudáveis,
aplica as migrations e inicia a aplicação. Ela fica no ar até receber `SIGINT` ou `SIGTERM`, quando
para de aceitar conexões, conclui as requisições em andamento e fecha o banco.

Os passos também podem ser executados separadamente:

```sh
make infra-up     # sobe o PostgreSQL, o Keycloak e o LocalStack
make migrate-up   # aplica as migrations
make run          # inicia a aplicação
```

`make help` lista todos os alvos.

## Variáveis de ambiente

| Variável | Obrigatória | Padrão | Descrição |
| --- | --- | --- | --- |
| `DATABASE_URL` | sim | — | Conexão com o PostgreSQL |
| `HTTP_PORT` | não | `8080` | Porta do servidor HTTP |
| `OIDC_ISSUER_URL` | sim | — | Emissor dos tokens; a descoberta OIDC fornece as chaves de assinatura |
| `OIDC_DISCOVERY_URL` | não | `OIDC_ISSUER_URL` | Endereço de onde buscar a descoberta OIDC, quando o Keycloak é alcançado por outro endereço |
| `OIDC_AUDIENCE` | não | `betledger-api` | Audiência exigida no claim `aud` |
| `REFERENCE_MAX_ATTEMPTS` | não | `8` | Tentativas sem encontrar a referência antes de recusar com `REFERENCE_NOT_FOUND` |
| `REFERENCE_RETRY_BASE_DELAY` | não | `1s` | Espera após a primeira tentativa sem sucesso; dobra a cada nova |
| `REFERENCE_RETRY_MAX_DELAY` | não | `5m` | Teto da espera entre tentativas |
| `REFERENCE_RETRY_LEASE` | não | `30s` | Por quanto tempo um worker reserva as operações que tomou |
| `REFERENCE_POLL_INTERVAL` | não | `1s` | Intervalo com que o worker ocioso procura operações vencidas |
| `AWS_REGION` | não | `us-east-1` | Região da AWS |
| `AWS_ENDPOINT_URL` | não | — | Endpoint da AWS; aponta para o LocalStack localmente |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | sim | — | Credenciais da AWS; qualquer valor no LocalStack |
| `EVENTS_QUEUE_NAME` | não | `wallet-events.fifo` | Fila FIFO que recebe os eventos publicados |
| `OUTBOX_POLL_INTERVAL` | não | `500ms` | Intervalo com que o publisher ocioso procura eventos |
| `OUTBOX_RETRY_BASE_DELAY` | não | `1s` | Espera após a primeira publicação que falhou; dobra a cada nova |
| `OUTBOX_RETRY_MAX_DELAY` | não | `1m` | Teto da espera entre publicações de um evento |
| `OUTBOX_LEASE` | não | `30s` | Por quanto tempo um publisher reserva os eventos que tomou |

A configuração é validada na inicialização, e o banco é consultado antes de o
servidor abrir a porta: sem `DATABASE_URL` ou `OIDC_ISSUER_URL`, com o banco
inacessível ou com o Keycloak inacessível, a aplicação não sobe. O [.env.example](.env.example) tem valores locais que
funcionam com o Compose; o Makefile lê o `.env` se ele existir.

## Migrations

As migrations ficam em
[internal/infrastructure/postgres/migrations](internal/infrastructure/postgres/migrations),
no formato do goose, com aplicação e reversão no mesmo arquivo.

```sh
make migrate-up       # aplica todas as pendentes
make migrate-down     # reverte a última aplicada
make migrate-status   # mostra quais estão aplicadas
```

Elas são executadas por um binário próprio, e não pela aplicação ao subir, para
que migrar seja um passo explícito. Várias execuções simultâneas são seguras: um
advisory lock do PostgreSQL garante que cada migration seja aplicada uma vez.

Depois de alterar uma migration ou uma query em
[queries/](internal/infrastructure/postgres/queries), regenere o código com
`make generate`.

## API

A documentação interativa fica em
**[http://localhost:8080/docs](http://localhost:8080/docs)**, com a aplicação no
ar, e a especificação OpenAPI em
[http://localhost:8080/openapi.yaml](http://localhost:8080/openapi.yaml). A fonte
é [api/openapi.yaml](api/openapi.yaml), escrita à mão; um teste de contrato
valida as respostas reais dos handlers contra ela e quebra se um status, campo ou
formato não estiver documentado.

### Autenticação e autorização

Health checks e documentação são públicos; todo o resto exige
`Authorization: Bearer <token>`. Os tokens vêm do Keycloak pelo grant
`client_credentials`, e o realm importado no Compose
([deploy/keycloak/betledger-realm.json](deploy/keycloak/betledger-realm.json))
tem três clients:

| Client | Papel | `provider_id` | Secret (só para desenvolvimento) |
| --- | --- | --- | --- |
| `wallet-service` | `wallet-operator` | — | `wallet-service-secret` |
| `provider-a` | `game-provider` | `provider-a` | `provider-a-secret` |
| `provider-b` | `game-provider` | `provider-b` | `provider-b-secret` |

```sh
TOKEN=$(make -s token CLIENT=wallet-service)
```

- `POST /wallets` exige `wallet-operator`.
- `POST /wagering/transactions` exige `game-provider`, e o `providerId` do corpo
  precisa ser o do claim `provider_id` do token.

Sem token, ou com token inválido, expirado ou de outra audiência, a resposta é
`401 UNAUTHENTICATED`; com token válido mas sem permissão, `403 FORBIDDEN`.
Como o provedor vem do token, a idempotência também fica isolada: a mesma chave
enviada por outro provedor é outra operação, nunca um replay.
O console do Keycloak fica em http://localhost:8081, com `admin`/`admin`.

### Abrir carteira

```sh
curl -i -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'X-Correlation-Id: req-123' \
  -d '{
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "initialBalance": { "amount": "1000.00", "currency": "BRL" }
  }'
```

```http
HTTP/1.1 201 Created
Location: /wallets/0192f291-27dd-7d3f-8071-5f8685deef37
X-Correlation-Id: req-123

{
  "id": "0192f291-27dd-7d3f-8071-5f8685deef37",
  "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
  "balance": { "amount": "1000.00", "currency": "BRL" },
  "version": 1
}
```

Com saldo positivo, a abertura grava no mesmo commit a carteira, a transação
`OPENING`, o lançamento de crédito e os eventos `WagerTransactionProcessed` e
`WalletBalanceChanged` na outbox. Com saldo zero, grava só a carteira.

O `amount` é sempre uma string decimal com até duas casas; números JSON são
recusados, para que o valor nunca passe por ponto flutuante.

### Enviar operação

```sh
curl -i -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $(make -s token CLIENT=provider-a)" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: provider-a:transaction-123' \
  -d '{
    "providerId": "provider-a",
    "externalTransactionId": "transaction-123",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "0192f291-27dd-7d3f-8071-5f8685deef37",
    "roundId": "round-987",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": { "amount": "25.00", "currency": "BRL" }
  }'
```

```http
HTTP/1.1 201 Created

{
  "transactionId": "0192f298-345e-7e38-af88-e43f851a819d",
  "status": "PROCESSED",
  "balance": { "amount": "975.00", "currency": "BRL" },
  "idempotentReplay": false
}
```

O header `Idempotency-Key` é obrigatório.

| Tipo | Movimento | Referência (`referenceExternalTransactionId`) |
| --- | --- | --- |
| `BET` | débito | nenhuma |
| `WIN` | crédito | opcional: um `BET` da mesma rodada |
| `LOSS` | nenhum; valor sempre `"0.00"` | nenhuma |
| `REFUND` | crédito | obrigatória: um `BET` |
| `ROLLBACK` | contrário ao da referência | obrigatória: um `BET`, `WIN` ou `REFUND` |

A referência precisa estar `PROCESSED` e ser do mesmo jogador, carteira, moeda e
rodada; numa reversão, também do mesmo valor. Cada operação é revertida no
máximo uma vez. Se a referência ainda não chegou, a operação fica em
`PENDING_REFERENCE` e não move dinheiro. Um worker em segundo plano tenta de novo
com backoff exponencial, inclusive depois de um reinício, e a conclui quando a
referência chega — ou a recusa com `REFERENCE_NOT_FOUND` quando as tentativas
acabam.

| Status | Quando |
| --- | --- |
| `201` | Operação aplicada agora |
| `202` | Operação gravada em `PENDING_REFERENCE`, esperando a referência |
| `200` | Replay de operação já aplicada, com `idempotentReplay: true` e o saldo original |
| `422` | Recusa de negócio, gravada como `REJECTED`, com `failureCode` no corpo do resultado; o replay responde igual |
| `409` | `IDEMPOTENCY_CONFLICT`: chave reutilizada com outro corpo, ou operação reenviada com outra chave |
| `404` | `WALLET_NOT_FOUND` |

### Eventos publicados

Todo evento gravado na outbox é publicado em `wallet-events.fifo` depois do
commit que o originou. O corpo da mensagem é o evento exatamente como gravado —
envelope com `eventId`, `eventType`, `aggregateId`, `correlationId`,
`causationId` opcional, `occurredAt`, `version` e `data` —, e as mensagens levam
também os atributos `eventType` e `eventId`.

| Campo SQS | Valor | Para quê |
| --- | --- | --- |
| `MessageGroupId` | `aggregateId`, a carteira | eventos de uma carteira são consumidos em ordem; carteiras diferentes, em paralelo |
| `MessageDeduplicationId` | `eventId` | a fila descarta uma republicação dentro da janela de 5 minutos |

A entrega é **pelo menos uma vez**: um evento pode chegar de novo, sempre com o
mesmo `eventId`, e o consumidor deve deduplicar por ele. Os eventos de uma
carteira chegam na ordem em que foram confirmados no banco.

Para ver os eventos localmente:

```sh
docker compose exec localstack awslocal sqs receive-message \
  --queue-url http://sqs.us-east-1.localhost.localstack.cloud:4566/000000000000/wallet-events.fifo \
  --max-number-of-messages 10 --attribute-names All
```

### Correlação

Toda resposta carrega `X-Correlation-Id`. Se a requisição enviar um valor com
até 128 caracteres entre letras, dígitos, `-`, `_` e `.`, ele é mantido; caso
contrário, é gerado um novo. O identificador acompanha os eventos gravados.

### Erros

Erros seguem sempre o mesmo formato:

```json
{ "error": { "code": "WALLET_ALREADY_EXISTS", "message": "..." } }
```

| Status | Quando | Exemplos de `code` |
| --- | --- | --- |
| `400` | Entrada inválida; corrigir e reenviar | `INVALID_INPUT`, `INVALID_AMOUNT`, `TRANSACTION_KIND_NOT_ALLOWED` |
| `401` | Token ausente, inválido ou expirado | `UNAUTHENTICATED` |
| `403` | Token válido sem permissão para a operação | `FORBIDDEN` |
| `404` | Recurso inexistente | `WALLET_NOT_FOUND` |
| `409` | Conflito com estado já persistido | `WALLET_ALREADY_EXISTS`, `IDEMPOTENCY_CONFLICT` |
| `422` | Recusa definitiva por regra de negócio | `INSUFFICIENT_FUNDS` |
| `503` | Indisponibilidade transitória; pode ser repetido | `SERVICE_UNAVAILABLE` |
| `500` | Erro inesperado | `INTERNAL_ERROR` |

## Testes

```sh
make test               # unitários
make test-race          # unitários com detector de corrida
make test-integration   # unitários e integração, contra containers reais
make check              # vet, testes, formatação e sqlc em dia
```

Os testes unitários ficam ao lado do código que testam, como é convenção em Go.
Os de integração ficam em [test/integration](test/integration), só com os testes,
e a infraestrutura que eles usam — containers, a aplicação em execução, chamadas
HTTP e consultas ao banco — fica em [test/testenv](test/testenv).

Eles ficam atrás da build tag `integration`, então `go test ./...` roda só os
unitários e não exige Docker. Com a tag, o testcontainers sobe um PostgreSQL e
um Keycloak e um LocalStack descartáveis por execução, com o mesmo realm e as
mesmas filas do Compose, sem estado
compartilhado e sem precisar do `make infra-up`. Cobrem estes níveis:

- **Persistência**: migrations nos dois sentidos, atomicidade, idempotência, as
  constraints do banco — inclusive a imutabilidade do ledger e da outbox,
  atacando as tabelas diretamente com SQL — e os cenários de concorrência da
  spec, como duas apostas de 80.00 sobre 100.00 e a mesma aposta enviada 50 vezes
  em paralelo.
- **Várias instâncias**: o binário da aplicação é compilado uma vez e executado
  como três processos independentes, cada um com sua memória e seu pool de
  conexões, contra o mesmo banco. As duas apostas de 80.00 chegam a instâncias
  diferentes, os reenvios vão para a terceira, e a mesma aposta é enviada 50
  vezes distribuída entre as três.
- **Publicação e retomada**: publisher e worker de referências contra PostgreSQL
  e SQS reais — eventos entregues na ordem de cada carteira, republicação após
  interrupção entre o commit e a publicação e entre a publicação e a confirmação,
  vários publishers e resolvedores disputando o mesmo banco, lease abandonado e
  retomada após reinício. Esses testes usam um PostgreSQL próprio, porque os
  workers tomam todo o trabalho vencido do banco.
- **Aplicação**: a aplicação real composta pelo Fx, chamada por HTTP. Verifica que
  ela sobe e serve, que no encerramento para de aceitar conexões e fecha o pool do
  banco, que o estado sobrevive a um reinício, e que com o banco travado o
  readiness falha e as escritas respondem `503` dentro do prazo, voltando ao normal
  quando o banco retorna. A autenticação é testada com tokens reais: token
válido, ausente, malformado, com assinatura de outro token e de outra audiência,
além de papel errado, provedor se passando por outro e isolamento da
idempotência entre provedores.

Para rodar um teste específico:

```sh
go test -run 'TestMoneyAdd' ./internal/domain/money
go test -tags=integration -run 'TestSchemaEnforcesFinancialInvariants' ./test/integration
```

## Estrutura

```
api/                                 contrato HTTP em OpenAPI e página do Swagger UI
Dockerfile                           imagem multi-stage com a aplicação e o binário de migrations
cmd/betledger/                       entrypoint
cmd/migrate/                         aplicação e reversão das migrations
deploy/keycloak/                     realm importado pelo Keycloak no Compose e nos testes
deploy/localstack/                   provisionamento das filas no Compose e nos testes
internal/app/                        composição da aplicação via Fx
internal/domain/                     erros e identificadores compartilhados
internal/domain/money/               valor monetário exato, sem ponto flutuante
internal/domain/ledger/              lançamentos do ledger append-only
internal/domain/wager/               operações dos provedores e máquina de estados
internal/domain/wallet/              carteira, raiz do agregado financeiro
internal/domain/event/               eventos de integração
internal/usecase/                    casos de uso e as portas que eles consomem
internal/infrastructure/auth/        validação dos access tokens do Keycloak
internal/infrastructure/config/      carga e validação da configuração de ambiente
internal/infrastructure/httpserver/  handlers, middleware e ciclo de vida do servidor
internal/infrastructure/messaging/   cliente SQS e publicação dos eventos
internal/infrastructure/postgres/    repositórios, migrations e queries do sqlc
internal/infrastructure/worker/      workers em segundo plano: retomada de referências e publicação da outbox
test/integration/                    testes de integração, só os testes
test/testenv/                        containers, aplicação e helpers dos testes de integração
```

A regra que orienta a organização: as dependências apontam para dentro.
`internal/domain` não importa nada; `internal/usecase` importa só o domínio; e
tudo que fala com o mundo externo fica em `internal/infrastructure`,
implementando as portas dos casos de uso.

## Próximos passos

Na ordem prevista, seguindo [SPECS.md](SPECS.md):

1. Consumidor SQS de `wager-transactions.fifo` com inbox e DLQ (seção 10).
2. Rotas de leitura, observabilidade e reconciliação (seções 9 e 12).
