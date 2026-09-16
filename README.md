# betledger

Serviço de processamento distribuído de operações financeiras de provedores de
jogos, em Go com Uber Fx. O escopo completo do desafio está em [SPECS.md](SPECS.md),
e as decisões técnicas em [ARCHITECTURE.md](ARCHITECTURE.md).

## Estado atual

O projeto está em construção incremental. O que existe e funciona hoje:

- **Modelo de domínio completo** — `Money`, `Wallet`, `WagerTransaction` e
  `WalletLedgerEntry`, com invariantes, máquina de estados e erros classificáveis.
- **Composição via Fx** com ciclo de vida, servidor HTTP e shutdown gracioso.
- **Liveness probe** em `GET /health/live`.
- **197 testes unitários** de domínio, passando com `-race`.

Ainda **não** existem: PostgreSQL e migrations, SQS, Keycloak, Docker Compose,
inbox/outbox, os endpoints de negócio e o readiness probe. A seção
[Próximos passos](#próximos-passos) lista a ordem prevista.

## Pré-requisitos

- **Go 1.27.1 ou superior** — a versão está declarada em [go.mod](go.mod).

Nenhuma outra dependência é necessária no estado atual: não há banco, fila nem
container envolvidos ainda.

```sh
go version   # confirme a instalação
```

## Executar a aplicação

```sh
go run ./cmd/betledger
```

A aplicação sobe o servidor HTTP e fica no ar até receber `SIGINT` ou `SIGTERM`,
quando encerra de forma graciosa. Para verificar:

```sh
curl -i http://localhost:8080/health/live
# HTTP/1.1 200 OK
# {"status":"ok"}
```

## Variáveis de ambiente

| Variável | Obrigatória | Padrão | Descrição |
| --- | --- | --- | --- |
| `HTTP_PORT` | não | `8080` | Porta do servidor HTTP |

A configuração é validada na inicialização: um valor inválido impede a aplicação
de subir, em vez de falhar depois. Ainda não existe `.env.example` porque não há
segredos nem serviços externos configurados.

## Testes

```sh
go test ./...           # suíte completa
go test -race ./...     # com detector de corrida
go vet ./...
gofmt -l .              # não deve imprimir nada
```

Todos os testes atuais são unitários e não precisam de infraestrutura. Quando os
testes de integração entrarem, eles vão exigir containers reais (PostgreSQL,
LocalStack e Keycloak) e as instruções de preparação virão nesta seção.

Para rodar um teste específico:

```sh
go test -run 'TestMoneyAdd' ./internal/domain
go test -run 'TestParseMoney/should_return_INVALID_INPUT_when_currency_has_a_digit' ./internal/domain
```

Com cobertura:

```sh
go test -cover ./internal/domain
```

## Estrutura

```
cmd/betledger/      entrypoint, composição da aplicação via Fx
internal/domain/    modelo de negócio, sem dependência de Fx, HTTP, SQS ou banco
internal/config/    carga e validação da configuração de ambiente
internal/httpserver/ servidor HTTP e seu ciclo de vida no Fx
```

A regra que orienta a organização: `internal/domain` não importa nada de
infraestrutura. Tudo que fala com o mundo externo fica fora dele.

## Próximos passos

Na ordem prevista, seguindo [SPECS.md](SPECS.md):

1. Eventos de domínio (seção 11) — `WagerTransactionProcessed`,
   `WalletBalanceChanged`, `WagerTransactionRejected` e
   `WagerTransactionPendingReference`.
2. PostgreSQL, migrations versionadas e repositórios.
3. Endpoints de negócio e idempotência persistente (seções 9 e 5).
4. SQS com inbox e outbox transacional (seções 10 e 11).
5. Keycloak e autorização por provedor (seção 2).
6. Observabilidade e reconciliação (seções 9 e 12).
