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
- **Health checks** — liveness em `GET /health/live` e readiness, que checa o
  banco, em `GET /health/ready`.

Ainda **não** existem: autenticação, as operações de aposta, idempotência, SQS, o
worker que publica a outbox e a aplicação em container. A seção
[Próximos passos](#próximos-passos) lista a ordem prevista.

## Pré-requisitos

- **Go 1.27.1 ou superior**, a versão declarada em [go.mod](go.mod).
- **Docker com o plugin Compose**, para o PostgreSQL e os testes de integração.
- **make**.

O sqlc não precisa ser instalado: ele é uma tool do módulo e roda com
`go tool sqlc`.

## Executar

```sh
cp .env.example .env
make dev
```

O `make dev` sobe o PostgreSQL, aguarda ele ficar saudável, aplica as migrations
e inicia a aplicação. Ela fica no ar até receber `SIGINT` ou `SIGTERM`, quando
para de aceitar conexões, conclui as requisições em andamento e fecha o banco.

Os passos também podem ser executados separadamente:

```sh
make db-up        # sobe o PostgreSQL
make migrate-up   # aplica as migrations
make run          # inicia a aplicação
```

`make help` lista todos os alvos.

## Variáveis de ambiente

| Variável | Obrigatória | Padrão | Descrição |
| --- | --- | --- | --- |
| `DATABASE_URL` | sim | — | Conexão com o PostgreSQL |
| `HTTP_PORT` | não | `8080` | Porta do servidor HTTP |

A configuração é validada na inicialização, e o banco é consultado antes de o
servidor abrir a porta: sem `DATABASE_URL` ou com o banco inacessível, a
aplicação não sobe. O [.env.example](.env.example) tem valores locais que
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

### Abrir carteira

```sh
curl -i -X POST http://localhost:8080/wallets \
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
| `400` | Entrada inválida; corrigir e reenviar | `INVALID_INPUT`, `INVALID_AMOUNT` |
| `404` | Recurso inexistente | `WALLET_NOT_FOUND` |
| `409` | Conflito com estado já persistido | `WALLET_ALREADY_EXISTS` |
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

Os testes de integração ficam atrás da build tag `integration`, então
`go test ./...` roda só os unitários e não exige Docker. Com a tag, o
testcontainers sobe um PostgreSQL descartável por execução, sem estado
compartilhado e sem precisar do `make db-up`. Eles verificam as migrations nos
dois sentidos, a atomicidade da abertura e as constraints do banco — inclusive a
imutabilidade do ledger e da outbox, atacando as tabelas diretamente com SQL.

Para rodar um teste específico:

```sh
go test -run 'TestMoneyAdd' ./internal/domain
go test -tags=integration -run 'TestSchemaEnforcesFinancialInvariants' ./internal/infrastructure/postgres
```

## Estrutura

```
api/                                 contrato HTTP em OpenAPI e página do Swagger UI
cmd/betledger/                       entrypoint, composição da aplicação via Fx
cmd/migrate/                         aplicação e reversão das migrations
internal/domain/                     erros e identificadores compartilhados
internal/domain/money/               valor monetário exato, sem ponto flutuante
internal/domain/ledger/              lançamentos do ledger append-only
internal/domain/wager/               operações dos provedores e máquina de estados
internal/domain/wallet/              carteira, raiz do agregado financeiro
internal/domain/event/               eventos de integração
internal/usecase/                    casos de uso e as portas que eles consomem
internal/infrastructure/config/      carga e validação da configuração de ambiente
internal/infrastructure/httpserver/  handlers, middleware e ciclo de vida do servidor
internal/infrastructure/postgres/    repositórios, migrations e queries do sqlc
```

A regra que orienta a organização: as dependências apontam para dentro.
`internal/domain` não importa nada; `internal/usecase` importa só o domínio; e
tudo que fala com o mundo externo fica em `internal/infrastructure`,
implementando as portas dos casos de uso.

## Próximos passos

Na ordem prevista, seguindo [SPECS.md](SPECS.md):

1. Autenticação com Keycloak e restrição das operações de carteira ao serviço
   interno (seção 2).
2. Aplicação em container, para rodar tudo com `docker compose up --build`.
3. Operações de aposta com idempotência persistente e concorrência por carteira
   (seções 5, 7, 8 e 9).
4. SQS com inbox e o worker de publicação da outbox (seções 10 e 11).
5. Observabilidade e reconciliação (seções 9 e 12).
