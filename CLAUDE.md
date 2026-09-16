# betledger

Serviço de processamento distribuído de apostas em Go. O escopo completo está em
`SPECS.md` — leia a seção relevante antes de implementar qualquer parte, porque a
spec é o critério de avaliação e tem itens eliminatórios.

## Comandos

```sh
go test ./...             # testes
go test -race ./...       # obrigatório antes de considerar algo pronto
go vet ./...
gofmt -l .                # deve não imprimir nada
go run ./cmd/betledger    # sobe a aplicação
```

## Convenções de código

**Todo o código é em inglês** — identificadores, comentários e **mensagens de
erro**. Só `README.md`, `ARCHITECTURE.md` e mensagens de commit ficam em
português, porque o leitor delas é o avaliador do desafio.

**Comentários**: apenas godoc em declarações exportadas, conciso. Nada de
comentário inline, comentário de campo de struct ou prosa explicativa dentro de
função.

**Sem getters por reflexo**: em DTOs, linhas de banco e payloads de evento, use
campos exportados. A exceção é o núcleo do domínio (`Money`, `Wallet`,
`WagerTransaction`, `WalletLedgerEntry`), onde os campos são não exportados
porque a seção 6 da spec exige estado encapsulado e isso é item pontuado. Mesmo
lá, não crie acessor sem consumidor real.

**Nenhum campo sem regra de negócio por trás**: se nada no domínio consome o
campo, ele pertence à infraestrutura. Foi por isso que `createdAt`/`updatedAt`
saíram das entidades — nenhuma regra depende de relógio, e a expiração de
referência pendente é por número máximo de tentativas, não por TTL.

**Domínio isolado**: `internal/domain` não importa Fx, HTTP, SQS nem biblioteca
de persistência. Só stdlib, `google/uuid` e `testify` nos testes.

**Dinheiro nunca em ponto flutuante**, em nenhuma etapa — parsing, cálculo,
serialização ou persistência. Isso é eliminatório na spec.

## Convenções de teste

Uma tabela por método testado. A struct carrega `wantResult` e `wantErr`, e as
duas são **sempre** afirmadas, sem `if` de ramificação no corpo do subteste:

```go
got, err := test.a.Add(test.b)

assert.ErrorIs(t, err, test.wantErr)
assert.Equal(t, test.wantResult, got)
```

Isso funciona porque, em caso de erro, a operação devolve o valor zero — que
coincide com o `wantResult` não preenchido — e `errors.Is(nil, nil)` é `true`.

**Sem helpers locais** além dos construtores de fixture compartilhados
(`mustMoney`, `mustWallet`, `mustTransaction`, `validExternalParams`). Fixtures
vão inline no literal da tabela, via construtores `Must*` do domínio.

**testify**: `assert` para verificações, `require` só quando continuar causaria
panic. Duas pegadinhas: a ordem é `(t, expected, actual)`, e `assert.Equal` é
estrito quanto a tipo — comparar com `Version()` exige `int64(1)`, não `1`.

**Nome dos casos**: `should return <FAILURE_CODE> when <condição>` para falhas,
`should accept|normalize|parse|format|report when <condição>` para sucessos. O
código de falha aparece literal no nome, mesmo duplicando `wantErr`. Ao editar um
caso, confira que nome e campo continuam batendo — o compilador não pega isso.

Todo subteste chama `t.Parallel()`.

## Erros de domínio

Classes via `errors.Is` (`ErrValidation`, `ErrRejected`, `ErrConflict`,
`ErrNotFound`) e `FailureCode` estável, que também satisfaz `error`:

```go
errors.Is(err, domain.FailureCodeInsufficientFunds)
```

Rejeições de negócio nunca usam `panic`. `panic` só para erro de programação,
como nos construtores `Must*`.
