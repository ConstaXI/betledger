# Arquitetura

Decisões arquiteturais do betledger e o raciocínio por trás delas. O escopo está
em [SPECS.md](SPECS.md); cada seção indica se a decisão já está implementada.

## Camadas

`internal/domain` concentra as regras de negócio e não depende de Fx, HTTP, SQS
nem de persistência. Toda integração com o mundo externo fica nas bordas, e a
composição da aplicação acontece apenas no entrypoint.

O domínio é dividido em um pacote por conceito — `money`, `ledger`, `wager`,
`wallet` e `event` —, e as dependências entre eles seguem uma direção única:
`money` não conhece nada acima dele, e `event` pode ler todos. Como em Go cada
pasta é um pacote, essa direção é imposta pelo compilador, que recusa ciclos.

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

Quando a mesma causa exige códigos diferentes conforme o contexto, o erro
preserva a causa comum: saldo insuficiente é `INSUFFICIENT_FUNDS` numa aposta e
`INSUFFICIENT_FUNDS_FOR_REVERSAL` numa reversão, e ambos satisfazem
`errors.Is(err, domain.ErrInsufficientFunds)`.

## Eventos

**Implementado.** Os quatro eventos da spec são construídos pelo domínio, que
define tipo e versão e recusa eventos incoerentes com o estado — não é possível
emitir `WagerTransactionProcessed` para uma transação não concluída.

O `aggregateId` é sempre a carteira. Isso dá ordenação por carteira aos
consumidores e serve de `MessageGroupId` na fila FIFO, sem serializar carteiras
independentes entre si.

## Publicação com transactional outbox

**Implementado.** Os eventos são gravados na outbox no mesmo commit da mudança
que os originou, e um worker separado os publica depois, na fila FIFO
`wallet-events.fifo`. Nada é publicado antes do commit, porque o publisher só
enxerga linhas já confirmadas.

- **Ordem por carteira.** A outbox tem uma coluna `sequence` atribuída no insert,
  enquanto o lock da carteira está tomado, e por isso segue a ordem de commit de
  cada carteira — ao contrário de `created_at`, que marca o início da transação.
  O publisher só toma o **evento não publicado mais antigo de cada carteira**; o
  seguinte espera até ele ser publicado, mesmo que outro publisher esteja livre.
  Uma falha de publicação segura só aquela carteira.
- **Vários publishers.** Cada um toma um lote com `FOR UPDATE SKIP LOCKED` e
  adia `next_attempt_at` pelo tempo do lease; outro publisher pula esses
  registros. Se o publisher morrer, o lease expira e outro assume.
- **Pelo menos uma vez.** Publicar e marcar como publicado não são atômicos: se o
  processo morrer entre os dois, o evento é publicado de novo quando o lease
  expira, com o mesmo `eventId`. O `MessageDeduplicationId` é o `eventId`, então a
  fila descarta a repetição dentro da janela de 5 minutos; fora dela, cabe ao
  consumidor deduplicar pelo `eventId`, o que o contrato documenta.
- **Nada é descartado.** A publicação que falha é tentada de novo com backoff
  exponencial, sem limite de tentativas: um evento confirmado no banco nunca se
  perde. A outbox não tem DLQ; um evento preso aparece como atraso da outbox.
- **Roteamento.** `MessageGroupId` é a carteira, então consumidores processam os
  eventos de uma carteira em ordem e carteiras diferentes em paralelo. O corpo da
  mensagem é o snapshot gravado, byte a byte.

Os testes com LocalStack real cobrem a interrupção entre o commit e a publicação,
a interrupção entre a publicação e a confirmação — a fila recebe cada evento uma
vez —, e três publishers simultâneos, conferindo entrega única e ordem por
carteira. Sem a regra do evento mais antigo por carteira, a ordem se perde.

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

**Implementado.** A coordenação é por carteira, nunca global. Cada operação
carrega a carteira com `SELECT ... FOR UPDATE`, que serializa as operações da
mesma carteira sem bloquear as demais, e grava o saldo com
`UPDATE ... WHERE version = <versão lida>`, que falha em vez de sobrescrever se a
versão mudou. As duas defesas são redundantes de propósito: o lock é o mecanismo,
a versão é a garantia contra lost update caso ele deixe de ser tomado.

Os cenários da spec — duas apostas de 80.00 sobre 100.00 e a mesma aposta enviada
50 vezes — rodam em paralelo contra o PostgreSQL real, em várias rodadas, tanto
dentro de um processo quanto distribuídos entre três processos independentes da
aplicação. Nenhum estado de coordenação vive em memória, então a correção não
depende de haver uma única instância; os testes com várias instâncias falham se
o `FOR UPDATE` for removido.

## Idempotência

**Implementado.** A chave é única por provedor no banco, e a operação guarda um
hash SHA-256 do JSON canônico dos campos de negócio, sem a chave e sem metadados
de transporte. Mesma chave e mesmo hash devolvem o resultado persistido; mesma
chave com hash diferente, ou a mesma `(providerId, externalTransactionId)` sob
outra chave, é conflito. A consulta acontece depois do lock da carteira, então
duas cópias simultâneas não passam ambas pela checagem.

Recusas de negócio são **gravadas** como `REJECTED`, com o evento
`WagerTransactionRejected`, em vez de desfazer a transação. Assim o replay de uma
recusa devolve a mesma recusa, e não uma nova tentativa que poderia ser aceita
depois de um crédito.

## Reversões e referências

**Implementado.** A referência é buscada por
`(providerId, referenceExternalTransactionId)` depois do lock da carteira, e a
própria transação a confere (`ResolveReference`): o tipo precisa ser referenciável
(`WIN` e `REFUND` apontam para `BET`; `ROLLBACK` para `BET`, `WIN` ou `REFUND`), o
estado `PROCESSED`, e jogador, carteira, moeda e rodada iguais; numa reversão, o
valor também. Divergência é recusa de negócio, gravada como `REJECTED` com um
código específico (`REFERENCE_MISMATCH`, `REFERENCE_NOT_PROCESSED`,
`REFERENCE_AMOUNT_MISMATCH`). O `ROLLBACK` move no sentido contrário ao da
referência: credita um `BET` e debita um `WIN` ou um `REFUND`.

**Uma reversão por operação, de qualquer tipo.** A spec exige que uma referência
não receba duas reversões bem-sucedidas do mesmo tipo e pede uma política para
`REFUND` e `ROLLBACK` sobre a mesma aposta. A regra adotada é mais estrita: cada
operação é revertida no máximo uma vez, seja por `REFUND` ou por `ROLLBACK`. As
duas devolvem o mesmo débito, então aceitar as duas devolveria a aposta em dobro.
O que se desfaz é a própria reversão: um `REFUND` pode receber um `ROLLBACK`, que
debita de volta, e a aposta original continua com sua reversão consumida, sem
reabrir. A segunda tentativa é `REFERENCE_ALREADY_REVERSED`.

A regra vive em três camadas. O lock da carteira serializa a reversão e a
checagem de reversão existente, já que referência e reversão são sempre da mesma
carteira. O caso de uso recusa a segunda reversão como `REJECTED`, auditável. E o
banco tem um índice único parcial em `reference_transaction_id` para reversões
`PROCESSED`, além de um `CHECK` que exige a referência resolvida numa reversão
processada; um teste com várias reversões simultâneas da mesma aposta comprova
um único crédito, e sem a checagem do caso de uso o índice ainda recusa a
segunda.

**Referência ausente ou pendente.** Quando a referência não chegou, ou ela mesma
ainda espera outra referência, a operação é gravada como `PENDING_REFERENCE`, com
o evento `WagerTransactionPendingReference`, sem mover dinheiro, e o HTTP
responde `202`. Uma referência que terminou sem sucesso (`REJECTED` ou `FAILED`)
é definitiva: a operação é recusada com `REFERENCE_NOT_PROCESSED`, em vez de
esperar.

**Retomada das pendentes.** Um worker tenta de novo as operações em
`PENDING_REFERENCE` usando o mesmo código de conclusão do recebimento (resolução
da referência, `Wallet.Apply`, eventos e gravação), sob o lock da carteira. O
estado da espera vive no banco — `reference_attempts` e `next_attempt_at` —, então
a retomada sobrevive a reinícios sem nada em memória.

- **Expiração por tentativas, não por TTL.** O domínio só conta as tentativas que
  não encontraram a referência concluída (`RecordMissingReference`) e recusa a
  operação com `REFERENCE_NOT_FOUND` ao atingir o máximo, emitindo
  `WagerTransactionRejected`. Nenhuma regra do domínio lê relógio.
- **Backoff exponencial calculado na aplicação**: a espera começa em
  `REFERENCE_RETRY_BASE_DELAY` e dobra a cada tentativa, até
  `REFERENCE_RETRY_MAX_DELAY`. Sem jitter, porque as tentativas não disputam um
  recurso externo e o lease já espalha o trabalho entre workers.
- **Várias instâncias com lease.** Cada worker toma um lote com
  `FOR UPDATE SKIP LOCKED` e adia `next_attempt_at` pelo tempo do lease, num único
  comando; outro worker pula esses registros. Se o worker morrer, o lease expira e
  a operação volta a ser tomada. O lease só divide o trabalho: a correção vem do
  lock da carteira e de a gravação exigir que a operação ainda esteja pendente, e
  um teste com vários workers simultâneos comprova um único crédito por operação.
- **Encerramento.** No `SIGTERM` o worker para de tomar trabalho e espera a
  tentativa em andamento; se o prazo de parada acabar antes, ela é cancelada, a
  transação é desfeita e o lease devolve a operação a outro worker.

O `correlationId` dos eventos emitidos por uma retomada é
`pending-reference-<transactionId>`, já que não há requisição que os origine.

## Autenticação e autorização

**Implementado.** O IdP é o
Keycloak, recomendado pela spec, que roda no Compose e nos testes com o mesmo
realm importado de um arquivo versionado. A comunicação é serviço a serviço, então
cada provedor e o serviço interno de carteiras são clients confidenciais com
`client_credentials`, sem usuários nem senhas.

A aplicação valida o JWT localmente, sem consultar o Keycloak a cada requisição:
assinatura com as chaves publicadas no JWKS, emissor, expiração e audiência. As
chaves ficam em cache e são buscadas de novo quando aparece um `kid`
desconhecido, o que cobre a rotação. A audiência `betledger-api` é adicionada aos
tokens por um client scope do realm; sem essa checagem, qualquer token emitido
pelo realm para outra aplicação seria aceito. A validação usa `go-oidc`, que
implementa a descoberta OIDC e a verificação de JWKS, em vez de código próprio de
criptografia.

As rotas são **protegidas por padrão**: o roteador só atende sem token os health
checks e a documentação, declarados explicitamente como públicos; qualquer outro
caminho, inclusive inexistente, passa pela autenticação. Uma rota nova esquecida
fica fechada, não aberta.

No Compose, a aplicação alcança o Keycloak por `keycloak:8080`, enquanto os
clientes no host usam `localhost:8081`. Para que o emissor dos tokens seja um só,
o Keycloak fixa o hostname público (`KC_HOSTNAME`) e deixa as URLs internas, como a
do JWKS, seguirem o endereço usado. A aplicação busca a descoberta pelo endereço
interno (`OIDC_DISCOVERY_URL`), mas continua exigindo que o emissor anunciado seja
o configurado em `OIDC_ISSUER_URL`; a checagem não é relaxada.

O Keycloak é descoberto na subida, e a aplicação não sobe se ele estiver
inacessível. Depois disso, a indisponibilidade dele não afeta as requisições
enquanto as chaves em cache forem válidas. Ele não entra no readiness por isso.

O modelo de permissões usa **papéis de realm**, porque as permissões são do
serviço como um todo: `wallet-operator` para o serviço interno, que abre
carteiras, e `game-provider` para os provedores, que enviam operações. Cada rota
declara o papel que exige, e token válido sem ele recebe `403`.

A identidade do provedor vem de um claim **`provider_id` fixo no client**, e não
do id do client (`azp`). Assim, recriar ou renomear um client no Keycloak não
muda a identidade gravada nas transações. O `providerId` do corpo continua no
contrato, igual ao da spec e ao payload do SQS, mas precisa bater com o do token:
divergência é `403`, em vez de o servidor trocar o valor em silêncio.

Com o provedor vindo do token, o isolamento entre provedores sai do modelo de
dados, e não de checagens espalhadas: a chave de idempotência e o
`externalTransactionId` já são únicos por provedor, então um provedor nunca
encontra, reaproveita nem reenvia uma operação de outro.

## Composição e shutdown

**Implementado.** São **duas aplicações** a partir do mesmo módulo: `cmd/api`
serve o HTTP e `cmd/workers` roda a retomada de referências e a publicação da
outbox. Cada uma monta o próprio grafo do Fx no seu `main`, sem um pacote de
composição compartilhado: os provedores que as duas usam aparecem nos dois
arquivos. A duplicação é deliberada — cada aplicação se lê inteira num arquivo só
e pode mudar sem arrastar a outra —, e o custo é lembrar de aplicar nos dois um
ajuste que valha para ambos.

A separação existe porque as duas escalam por motivos diferentes: a API é curta e
sensível a latência, os workers são longos e sensíveis a throughput e a disputa
por locks. Assim dá para escalá-las em separado, e um deploy da API não
interrompe uma rodada de trabalho no meio. Como toda a coordenação vive no banco,
subir mais de um processo de workers é seguro. Os workers não expõem HTTP: sua
saúde é o processo estar vivo, e uma dependência inacessível impede a subida.

As duas são compostas com Uber Fx, e servidor, workers e recursos são
gerenciados por `fx.Lifecycle`. Cada entrypoint tem um teste que valida seu grafo
de dependências sem precisar de infraestrutura, e os testes de integração sobem
os binários compilados como processos, do mesmo jeito que rodam em produção. A inicialização valida a configuração, consulta o
banco e só então abre o listener, para que uma falha impeça a subida em vez de
ocorrer em segundo plano. O encerramento segue a ordem inversa: o servidor para
de aceitar conexões e conclui as em andamento, os workers de referências
pendentes e de publicação concluem a rodada em andamento, e só depois o pool do
banco é fechado. O readiness checa o PostgreSQL e o SQS, e a aplicação não sobe
se a fila de eventos não existir. Um teste valida o grafo de dependências do Fx sem precisar de banco.

## HTTP

**Implementado.** Cada classe de erro de domínio corresponde a um status HTTP
fixo — entrada inválida `400`, conflito `409`, recusa de negócio `422`,
indisponibilidade transitória `503` —, e o corpo sempre carrega o `code` estável.
Assim o cliente distingue, só pelo contrato, o que deve corrigir, o que pode
repetir e o que é definitivo. Falhas internas são registradas no log, mas seus
detalhes nunca chegam ao cliente.

Em `POST /wagering/transactions`, o status separa os desfechos: `201` para
operação aplicada agora, `200` para replay de uma aplicada, `202` para operação
esperando a referência, e `422` para `REJECTED` — nova ou replay —, com o corpo
do resultado em vez do corpo de erro, já que a recusa é um estado persistido com
`transactionId`.

Cada requisição tem um prazo de 5 segundos, abaixo do `WriteTimeout` do servidor.
Sem ele, uma requisição feita com o banco travado ficava pendurada
indefinidamente; com ele, o prazo expirado aborta a operação pendente e a
resposta é `503`, que o cliente pode repetir com segurança graças à idempotência.

## Interpretações adotadas

- `WIN` aceita referência opcional a uma aposta da mesma rodada; `BET` e `LOSS`
  não aceitam referência. Um `WIN` com referência não consome a reversão da
  aposta.
- Uma operação não pode referenciar a si mesma; isso é entrada inválida.
- `LOSS` tem valor zero e só registra o desfecho da rodada: não gera lançamento,
  não muda a versão da carteira e emite apenas `WagerTransactionProcessed`.
- Saldo inicial zero não cria `OPENING`, lançamento nem eventos, e a carteira
  nasce com versão 1.
- A moeda é validada pelo formato ISO 4217, não contra uma lista fechada.

## Trabalho não concluído

- **Consumidor SQS** com inbox e DLQ.
- **Reconciliação** e **observabilidade** além dos logs JSON.
