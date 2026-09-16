# Decisões de arquitetura

Este documento registra as decisões técnicas do betledger e o raciocínio por trás
delas. O escopo exigido está em [SPECS.md](SPECS.md).

O projeto está em construção incremental: as seções abaixo distinguem o que já
está **implementado** do que está apenas **decidido**, e a seção
[Trabalho não concluído](#trabalho-não-concluído) lista o que ainda não foi
resolvido.

## Dinheiro

**Implementado.**

`Money` é um value object imutável com valor e moeda, representado por `int64` em
unidades mínimas (centavos) com escala fixa de duas casas. Ponto flutuante não
aparece em nenhuma etapa: parsing, aritmética, comparação e serialização operam
sobre inteiros e strings.

O intervalo representável é de aproximadamente ±92.233.720.368.547.758,07.
Operações que estourariam esse limite devolvem `AMOUNT_OVERFLOW` em vez de
truncar silenciosamente, e há verificação explícita de overflow no parsing, na
soma, na subtração e na negação.

**Normalização antes do hash de idempotência.** O parser aceita `"25"`, `"25.5"` e
`"25.50"` como equivalentes, normalizando para a escala de duas casas. A forma
canônica resultante é a que deve alimentar o hash de idempotência, garantindo que
entradas equivalentes por HTTP e por SQS produzam o mesmo hash. São rejeitados:
valor vazio, `NaN`, `Infinity`, notação científica, escala excedente, vírgula como
separador, sinal explícito de mais e espaços.

**Dois construtores a partir de string**, com responsabilidades distintas:

- `ParseMoney` — contrato externo, rejeita valores negativos.
- `ParseSignedMoney` — usos internos que admitem sinal, como a diferença da
  reconciliação e o round-trip de JSON.

A distinção existe porque a spec proíbe negativos nas entradas financeiras
externas, mas valores negativos são legítimos em diferenças internas.

**Limite conhecido**: o parser acumula o valor absoluto antes de aplicar o sinal,
então a faixa parseável é ±`MaxInt64` unidades mínimas. `MinInt64` é alcançável
apenas por `NewMoney`, e o `Amount()` dele não volta pelo parser. Optamos por
documentar em vez de tratar, porque as invariantes de saldo impedem chegar a esse
valor e o tratamento adicionaria complexidade a um caso inacessível.

**Persistência prevista**: `BIGINT` para as unidades mínimas e `CHAR(3)` para a
moeda, preservando valor e moeda exatamente.

## Modelagem e encapsulamento

**Implementado.**

As entidades têm estado encapsulado, construtores que validam e métodos
explícitos de transição, conforme a seção 6 da spec. Campos não exportados
impedem que `wallet.balance` ou `transaction.state` sejam alterados por fora,
burlando as invariantes.

**Criação e reidratação são separadas.** `OpenWallet` cria; `RehydrateWallet`
reconstrói o estado persistido sem reaplicar movimentações. O mesmo vale para
`NewExternalTransaction` e `RehydrateWagerTransaction`.

**O agregado emite o lançamento do ledger.** `Wallet.Credit` e `Wallet.Debit`
devolvem o `WalletLedgerEntry` correspondente, em vez de deixar o chamador criá-lo.
Isso torna impossível alterar saldo sem produzir o lançamento, e o construtor do
lançamento valida `balanceAfter = balanceBefore ± money`.

**Sem timestamps no domínio.** As entidades não carregam `createdAt`/`updatedAt` e
os métodos não recebem `now`. Nenhuma regra de negócio depende de relógio, e
manter esses campos significaria estado sem invariante por trás. As colunas
correspondentes são responsabilidade da infraestrutura.

**A moeda da carteira é a do próprio saldo.** Um campo `currency` separado seria
estado redundante, que precisaria ser mantido em sincronia com `balance`.

## Erros e códigos de falha

**Implementado.**

Erros de domínio são classificáveis por duas vias:

- **Classe**, via `errors.Is`: `ErrValidation`, `ErrRejected`, `ErrConflict`,
  `ErrNotFound`.
- **Código estável**, via `errors.Is` também, porque `FailureCode` satisfaz
  `error`: `errors.Is(err, domain.FailureCodeInsufficientFunds)`.

A segunda via existe para que a camada HTTP mapeie rejeições para o corpo de
resposta sem precisar extrair o código antes. `CodeOf` continua disponível quando
o código é necessário como string.

`panic` nunca representa rejeição de negócio — só erro de programação, nos
construtores `Must*`.

**Códigos de falha definidos até aqui**, todos parte do contrato externo:

| Código | Situação |
| --- | --- |
| `INVALID_INPUT` | Entrada malformada ou valor de domínio não inicializado |
| `INVALID_AMOUNT` | Valor monetário inválido para a operação |
| `AMOUNT_OVERFLOW` | Valor fora do intervalo representável |
| `CURRENCY_MISMATCH` | Moedas incompatíveis entre si ou com a carteira |
| `INSUFFICIENT_FUNDS` | Saldo insuficiente para uma aposta |
| `INSUFFICIENT_FUNDS_FOR_REVERSAL` | Saldo insuficiente para desfazer uma operação |
| `REFERENCE_NOT_FOUND` | Referência não encontrada após esgotar as tentativas |
| `REFERENCE_NOT_PROCESSED` | Referência existe, mas não está concluída com sucesso |
| `REFERENCE_MISMATCH` | Referência diverge em provedor, jogador, carteira, moeda ou rodada |
| `REFERENCE_AMOUNT_MISMATCH` | Valor da reversão diferente do valor referenciado |
| `REFERENCE_ALREADY_REVERSED` | Referência já recebeu uma reversão do mesmo tipo |
| `INVALID_STATE_TRANSITION` | Transição não permitida pela máquina de estados |
| `TRANSACTION_KIND_NOT_ALLOWED` | `OPENING` recebido por origem externa |
| `IDEMPOTENCY_CONFLICT` | Chave reutilizada com conteúdo diferente |
| `WALLET_ALREADY_EXISTS` | Carteira já existe para o par `(playerId, currency)` |
| `WALLET_NOT_FOUND` | Carteira inexistente |
| `TRANSACTION_NOT_FOUND` | Transação inexistente |

A spec exige que a falta de saldo ao **desfazer** uma operação tenha código
diferente da falta de saldo ao **apostar**. `Wallet.Debit` devolve um erro que
satisfaz `errors.Is(err, ErrInsufficientFunds)`, e a camada de aplicação escolhe
entre os dois códigos — o agregado não conhece o tipo da operação, então não
poderia decidir sozinho.

## Máquina de estados da transação

**Implementado.**

```
PENDING           -> PENDING_REFERENCE | PROCESSED | REJECTED | FAILED
PENDING_REFERENCE -> PROCESSED | REJECTED | FAILED
PROCESSED | REJECTED | FAILED -> (nenhuma)
```

Estados terminais não admitem nova transição, e a validação fica no domínio. O
replay de uma operação concluída consulta o resultado persistido sem reaplicar
nada.

**A transação guarda o saldo observado na conclusão** (`resultBalance`), porque a
spec exige que o replay devolva o saldo do processamento original mesmo que a
carteira tenha recebido outras movimentações depois.

**Distinção entre falhas**: `REJECTED` é recusa por regra de negócio, definitiva.
`FAILED` é falha permanente de infraestrutura, registrada para auditoria. Falhas
transitórias não produzem estado terminal — a operação permanece pendente e é
retomada.

**`OPENING` é interna.** `NewExternalTransaction` e `ParseExternalTransactionKind`
rejeitam esse tipo com `TRANSACTION_KIND_NOT_ALLOWED`, e a transação de abertura
nasce sem os metadados externos inaplicáveis (provedor, ID externo, chave, hash,
rodada, jogo).

## Regras por tipo de operação

**Implementado** (validação estrutural; a aplicação das regras contra o estado
persistido ainda não).

| Tipo | Valor | Referência externa |
| --- | --- | --- |
| `BET` | maior que zero | proibida |
| `WIN` | maior que zero | opcional |
| `LOSS` | exatamente zero | proibida |
| `REFUND` | maior que zero | obrigatória |
| `ROLLBACK` | maior que zero | obrigatória |

`LOSS` não produz lançamento no ledger nem altera a versão da carteira.

## Concorrência

**Decidido, não implementado.**

A coordenação é sempre por carteira, nunca global. `Wallet` carrega `version`,
incrementado a cada mudança de saldo, para servir à atualização condicional no
banco (`UPDATE ... WHERE version = :loaded`).

**A versão inicial é 1 e a abertura não a incrementa**, mesmo quando credita o
saldo inicial — a spec é explícita quanto a isso. Por isso `OpenWallet` já cria a
carteira com o saldo, e `OpeningLedgerEntry` emite o lançamento de zero até o
saldo inicial sem tocar em estado.

A escolha entre travamento pessimista (`SELECT ... FOR UPDATE`) e controle
otimista com retry será tomada junto com a implementação dos repositórios, e
registrada aqui. Ambas atendem à exigência de carteiras independentes avançarem
em paralelo.

## Referências pendentes

**Decidido, não implementado.**

A expiração de uma referência pendente será por **número máximo de tentativas**,
não por TTL. A consequência dessa escolha é que o domínio não precisa de relógio,
o que simplifica teste e raciocínio. O contador de tentativas pertence ao registro
de acompanhamento, não à entidade.

Esgotadas as tentativas, a operação termina como `REJECTED` com
`REFERENCE_NOT_FOUND`.

## Composição e ciclo de vida

**Implementado.**

A aplicação é composta com Uber Fx, organizada em `fx.Module` por
responsabilidade. Configuração, roteador e servidor são fornecidos por
construtores, e o ciclo de vida é gerenciado por `fx.Lifecycle`:

- **Start** valida a configuração antes de subir e abre o listener explicitamente,
  de modo que uma porta ocupada falha a inicialização em vez de virar erro
  silencioso numa goroutine.
- **Stop** usa `server.Shutdown`, que para de aceitar conexões novas e aguarda as
  em andamento dentro do prazo do contexto.

O domínio não conhece Fx: a composição acontece apenas nas bordas.

## Testes

**Implementado** para o domínio.

Testes table-driven, uma tabela por método. A tabela carrega `wantResult` e
`wantErr`, ambos sempre afirmados, sem ramificação no corpo do subteste — em caso
de erro a operação devolve o valor zero, que coincide com o `wantResult` não
preenchido. Os nomes dos casos seguem `should return <FAILURE_CODE> when
<condição>`, de modo que a expectativa fica legível na saída do `go test`.

A biblioteca de asserção é `testify`. Todo subteste roda em paralelo, e a suíte é
executada com `-race`.

A suíte foi validada por mutação: alterações deliberadas no domínio (remover a
checagem de saldo negativo, aceitar referência onde não é permitida) foram
detectadas pelos testes. Essa verificação revelou que a proteção contra saldo
negativo tem duas camadas — além da checagem em `Wallet.Debit`, o construtor do
lançamento também recusa saldo negativo.

## Trabalho não concluído

Nada abaixo está implementado. A ordem prevista está no
[README](README.md#próximos-passos).

- **Persistência**: PostgreSQL, migrations versionadas, repositórios, delimitação
  da transação SQL e escolha entre `pgx` com SQL explícito ou alternativa.
- **Idempotência persistente**: algoritmo do hash (JSON canônico com ordenação de
  chaves, excluindo a chave de idempotência e metadados de transporte), detecção
  de conflito e reprodução do resultado original.
- **Reversões contra o estado persistido**: resolução por
  `(providerId, referenceExternalTransactionId)`, concordância de provedor,
  jogador, carteira, moeda e rodada, e a política que impede `REFUND` e `ROLLBACK`
  devolverem o mesmo débito duas vezes.
- **Inbox e outbox transacional**, com publicação após o commit, retry com backoff,
  múltiplos publishers e recuperação de trabalho abandonado.
- **Eventos de integração** com envelope tipado e snapshot imutável no payload.
- **SQS**: filas FIFO, redrive para DLQ, `MessageGroupId`,
  `MessageDeduplicationId`, visibility timeout e limites de tentativa.
- **Autenticação e autorização**: Keycloak via `client_credentials`, `providerId`
  derivado da identidade autenticada, isolamento entre provedores e restrição das
  operações internas de carteira.
- **Endpoints de negócio**, reconciliação e readiness probe — hoje só existe
  `GET /health/live`.
- **Observabilidade**: logs JSON com identificadores de rastreio e métricas.
- **Testes de integração** com containers reais, cenários de concorrência com
  múltiplas instâncias e simulações de interrupção.

## Interpretações adotadas

- **`WIN` aceita referência opcional**, conforme "pode informar uma aposta da
  mesma rodada como referência". `BET` e `LOSS` não aceitam referência alguma.
- **Saldo inicial zero** não cria `OPENING`, lançamento nem eventos financeiros,
  e a carteira nasce mesmo assim com versão 1.
- **A moeda é validada por formato** (três letras maiúsculas, ISO 4217), não
  contra uma lista fechada. Os cenários principais usam BRL, e há testes de
  incompatibilidade entre moedas.
