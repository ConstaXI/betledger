# Arquitetura

Decisões arquiteturais do betledger e o raciocínio por trás delas. O escopo está
em [SPECS.md](SPECS.md); cada seção indica se a decisão já está implementada.

## Camadas

`internal/domain` concentra as regras de negócio e não depende de Fx, HTTP, SQS
nem de persistência. Toda integração com o mundo externo fica nas bordas, e a
composição da aplicação acontece apenas no entrypoint.

## Dinheiro

**Implementado.** `Money` usa `int64` em centavos com escala fixa de duas casas,
sem ponto flutuante em nenhuma etapa. Operações que estourariam o limite
representável devolvem erro em vez de truncar.

O parser normaliza formas equivalentes (`"25"`, `"25.5"`, `"25.50"`) para a escala
canônica, que é a base do hash de idempotência. Isso garante o mesmo hash para a
mesma operação, chegue ela por HTTP ou por SQS.

Na persistência, o valor vai para `BIGINT` em centavos e a moeda para `CHAR(3)`.

## Agregado e ledger

**Implementado.** `Wallet` é a raiz do agregado financeiro, e alterar o saldo
sempre produz o lançamento correspondente: `Credit` e `Debit` devolvem o
`WalletLedgerEntry`, que valida `saldoPosterior = saldoAnterior ± valor`. Não
existe caminho para mover dinheiro sem registrá-lo no ledger.

As entidades têm estado encapsulado, e criação e reidratação são separadas, para
que carregar uma carteira do banco nunca reaplique movimentações.

O domínio não lê o relógio: as entidades não carregam timestamps, e os eventos
recebem o instante de quem os cria. Isso mantém as regras determinísticas.

## Transações e estados

**Implementado.**

```
PENDING           -> PENDING_REFERENCE | PROCESSED | REJECTED | FAILED
PENDING_REFERENCE -> PROCESSED | REJECTED | FAILED
PROCESSED | REJECTED | FAILED -> (terminal)
```

`REJECTED` é recusa definitiva por regra de negócio; `FAILED` é falha permanente
de infraestrutura, mantida para auditoria. Falhas transitórias não geram estado
terminal: a operação continua pendente e é retomada.

A transação guarda o saldo observado ao concluir, para que o replay devolva o
resultado original mesmo após outras movimentações na carteira.

## Erros

**Implementado.** Erros de domínio são classificáveis por classe (`ErrRejected`,
`ErrValidation`, ...) e por um `FailureCode` estável, ambos via `errors.Is`.

Quando a mesma causa exige códigos diferentes conforme o contexto — saldo
insuficiente numa aposta versus numa reversão — o domínio sinaliza a causa e a
camada de aplicação escolhe o código, já que o agregado não conhece o tipo da
operação.

## Eventos

**Implementado.** Os quatro eventos da spec são construídos pelo domínio, que
define tipo e versão e recusa eventos incoerentes com o estado — não é possível
emitir `WagerTransactionProcessed` para uma transação não concluída.

O `aggregateId` é sempre a carteira. Isso dá ordenação por carteira aos
consumidores e serve de `MessageGroupId` na fila FIFO, sem serializar carteiras
independentes entre si.

## Persistência e fronteira transacional

**Implementado.** PostgreSQL acessado com `pgx`, e SQL explícito com código
gerado pelo sqlc, que lê as próprias migrations como schema.

Os use cases recebem os repositórios diretamente e delimitam a atomicidade com um
`Transactor`: a transação viaja no `context`, e todo repositório chamado com ele
participa dela. Como uma transação implícita pode ser perdida em silêncio — basta
chamar um repositório com o contexto errado —, os repositórios **recusam escrita
fora de transação**, e isso é testado.

As invariantes financeiras são impostas pelo schema, independentemente do
código: carteira única por jogador e moeda, saldo não negativo, no máximo uma
abertura por carteira, e ledger que recusa `UPDATE`, `DELETE` e `TRUNCATE` e
confere a aritmética de cada lançamento. A outbox guarda o evento como `JSON` —
que preserva o snapshot byte a byte — e recusa alterações nele; só as colunas de
controle de publicação mudam.

As migrations rodam por um binário próprio, e não na subida da aplicação, sob um
advisory lock que torna seguras as execuções simultâneas.

## Concorrência

**Parcialmente decidido.** A coordenação é por carteira, nunca global. `Wallet`
carrega uma versão incrementada a cada mudança de saldo, base para impedir lost
updates. A escolha entre lock pessimista e controle otimista será feita junto com
o use case de apostas, o primeiro com escritores concorrentes sobre a mesma
carteira.

## Referências pendentes

**Decidido.** Uma referência pendente expira por número máximo de tentativas, não
por TTL, o que dispensa relógio no domínio. Esgotadas as tentativas, a operação
termina como `REJECTED` com `REFERENCE_NOT_FOUND`.

## Composição e shutdown

**Implementado.** A aplicação é composta com Uber Fx, e servidor e recursos são
gerenciados por `fx.Lifecycle`. A inicialização valida a configuração, consulta o
banco e só então abre o listener, para que uma falha impeça a subida em vez de
ocorrer em segundo plano. O encerramento segue a ordem inversa: o servidor para
de aceitar conexões e conclui as em andamento, e só depois o pool do banco é
fechado. Um teste valida o grafo de dependências do Fx sem precisar de banco.

## HTTP

**Implementado.** Cada classe de erro de domínio corresponde a um status HTTP
fixo — entrada inválida `400`, conflito `409`, recusa de negócio `422`,
indisponibilidade transitória `503` —, e o corpo sempre carrega o `code` estável.
Assim o cliente distingue, só pelo contrato, o que deve corrigir, o que pode
repetir e o que é definitivo. Falhas internas são registradas no log, mas seus
detalhes nunca chegam ao cliente.

## Interpretações adotadas

- `WIN` aceita referência opcional a uma aposta da mesma rodada; `BET` e `LOSS`
  não aceitam referência.
- Saldo inicial zero não cria `OPENING`, lançamento nem eventos, e a carteira
  nasce com versão 1.
- A moeda é validada pelo formato ISO 4217, não contra uma lista fechada.

## Trabalho não concluído

- **Autenticação e autorização** com Keycloak e isolamento por provedor.
- **Operações de aposta**, com idempotência persistente e reprodução do resultado
  original.
- **Reversões** contra o estado persistido, incluindo a política que impede
  `REFUND` e `ROLLBACK` sobre o mesmo débito.
- **Publicação da outbox** e **inbox**, com SQS em filas FIFO e DLQ.
- **Aplicação em container** no Docker Compose.
- **Reconciliação** e **observabilidade** além dos logs JSON.
