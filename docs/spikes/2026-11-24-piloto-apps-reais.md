---
id: SPIKE-2026-11-24-piloto-apps-reais
title: Piloto do Rootline em dez apps reais escritos por quem não conhece a ferramenta
status: Concluído
summary: Rodada 1 (3 apps, stack livre) - todos Postgres + pgx + N+1, e o Rootline acertou os 3. Rodada 2 (7 apps, stack e família atribuídas) - 7 inconclusive, nenhum falso positivo, nenhum acerto, porque as famílias estão fora dos detectores atuais. Rodada 3 (10 apps, usuário cego só com README e guia) - 3/3 causas, 0/7 falsos positivos, sem script externo; o symptoms sugeriu errado no app-01 sob gravação ruidosa. Rodada 4 (as 19 gravações das rodadas 2 e 3 com o detector de forma) - Candidata de forma só no app-05 e no app-08, com mecanismo e alvo certos. Rodada 5 (as mesmas 19 com a Regressão do ADR-0032, p50 ≥ 2x com +2 ms e p50 ruim ≥ 3 ms) - só as 3 operações-alvo ganham Regressão e passam a ser sugeridas, o analyze acha o mesmo nas 138 operações, e o aviso de Causa sem Regressão sai só em /search e /related do app-03 e do app-03-guia. Orienta o docs/roadmap.md.
---

# Piloto do Rootline em apps reais

## Pergunta

O Rootline funciona num app Go que ele nunca viu, escrito por alguém que não sabe o que os
detectores procuram? O que falta para usá-lo fora do demo-app?

## Método

- **Criadores cegos.** Três agentes receberam só um domínio de negócio e o pedido de um serviço
  Go realista: Docker Compose com volume próprio, OpenTelemetry padrão, `service.version` com o
  SHA, gerador de carga e histórico git com um commit de aparência normal que introduz um problema
  de lentidão ou erro, de livre escolha. Banco e bibliotecas também eram livres. O prompt não cita
  o Rootline nem o que ele detecta. O gabarito ficou lacrado fora do repo do app.
- **Analista como usuário.** Para cada app, o analista instalou o hook de chamador (duas linhas com
  `multitracer`), subiu um Collector com file exporter e gravou as duas versões sob a carga do
  próprio app (60 s a 150 req/s). Depois comparou as rotas entre as versões para achar o sintoma,
  rodou `rootline analyze` e só então abriu o gabarito.
- **Versões comparadas.** Regra igual para todos os apps: a referência é o último commit pelo menos
  5 dias antes do HEAD (o "deploy da semana passada"), e a versão ruim é o HEAD.

## Rodada 1: resultado

| App | Domínio | Stack | Sintoma | Causa no Resultado | Acerto |
|---|---|---|---|---|---|
| app-01 | Telemetria IoT (5,4 mi de leituras) | Postgres 17, pgx, otelpgx, `net/http` | rota de status dos devices de um site, p50 6,4x | N+1, loop num service que chama um helper | causa, função, linha e commit |
| app-02 | Livro-razão multi-tenant | Postgres 18, pgx, otelpgx, chi + otelchi | extrato da conta, p50 3,1x | N+1, contraparte buscada por linha do extrato | causa, função, linha e commit |
| app-03 | Catálogo de livros | Postgres 18, pgx, otelpgx, chi + otelhttp | listagem de livros, p50 4,3x | N+1, autores buscados por livro após um refactor | causa, função, linha e commit |

Nos três, a confiança foi `high`. A evidência mostrou a query repetida (de 14 a 50 vezes por
requisição, contra 2 ou 3 na versão boa) e o toque de commit `location` na função da causa. A
referência e a versão ruim tinham dois commits entre elas, e o Rootline escolheu o certo.

## O que funcionou sem ajuste

- Binário compilado com `-trimpath`, padrão nos Dockerfiles dos três apps.
- chi com otelchi, chi com otelhttp e `net/http` puro. A rota veio de `http.route`.
- Loop num service que chama um helper que emite a query (app-01): a estratégia do loop achou o
  frame do service.
- Commits de refactor de aparência normal ("extract author queries into AuthorRepository").

## Lacunas do uso real

1. **Achar o sintoma.** O `analyze` exige a rota e as versões. Para saber qual rota piorou, foi
   preciso um script externo que compara latência e erro por rota entre versões a partir do OTLP
   JSON. É candidato a um comando `rootline symptoms`.
2. **Instalar o hook.** Sem tag publicada do módulo `hook/`, o `go get …/hook@main` gera uma
   pseudo-versão e baixa também o módulo raiz. Não há guia de instalação para um app de terceiros.
3. **Índice faltando no uso real.** Os três Resultados tinham `missing_index_skipped: no query
   plans`. Sem o adapter `Explainer` ao vivo (ADR-0012, ADR-0025), o detector só roda na avaliação.
4. **Coleta.** Foi preciso subir um Collector com file exporter à parte e apontar o
   `OTEL_EXPORTER_OTLP_ENDPOINT` para ele. Num ambiente com Jaeger ou Tempo, não há adapter de leitura.

## Limites da rodada 1

- **Pouca diversidade.** Mesmo com banco e tipo de problema livres, os três criadores escolheram
  Postgres + pgx + otelpgx e um N+1, que é o caminho mais testado do Rootline. O piloto não diz
  nada sobre MySQL, MongoDB, ORMs, erros, lock ou índice faltando em apps reais.
- **Isolamento imperfeito.** Os criadores usaram uma pasta temporária compartilhada que tinha
  arquivos de trabalho do Rootline. Os repos dos apps não citam o Rootline, mas não há como provar
  que nenhum criador leu esses arquivos. Dois criadores revelaram os SHAs no relatório final, o que
  o prompt proibia. As versões comparadas seguiram a mesma regra neutra em todos os apps.
- **Três apps.** É um piloto do processo, não uma medida de acerto.

## Rodada 2: sete apps com stack e família atribuídas

### Ajustes de método

- Cada criador usou uma pasta temporária própria, sem acesso aos arquivos do Rootline, e o
  relatório final não trazia SHAs, rota nem problema.
- Stack e família de problema atribuídas por app, a partir de uma lista genérica de SRE que não
  depende do que o Rootline detecta.
- A análise rodou em paralelo: um analista por app, cada um com o próprio Collector, e nenhum
  abriu o gabarito. A rodada levou cerca de 8 minutos de relógio. As duas versões de cada app
  rodaram sob a mesma carga da máquina, e a razão entre elas é o que importa.
- Hook de chamador instalado só nos apps com pgx (direto, como driver de `database/sql` ou sob o
  GORM).

### Resultado

| App | Stack | Família | Hook | Rootline | O que faltou |
|---|---|---|---|---|---|
| app-04 | MySQL + `database/sql` | lock numa linha de contador | não | inconclusive | banco sem suporte; nenhum detector de lock |
| app-05 | Redis + Postgres (sqlx sobre pgx) | cache sempre furado (chave com `time.Now()`) | sim | inconclusive | comparar a forma do trace: as agregações passaram a rodar em toda requisição |
| app-06 | MongoDB | índice faltando | não | inconclusive | spans do Mongo não contam como query; sem plano fora do Postgres |
| app-07 | Postgres + GORM | 5xx por divisão por zero num dado legado | sim | inconclusive | sintoma de erro |
| app-08 | Postgres + pgx | dependência HTTP lenta no caminho da requisição | sim | inconclusive | spans que não são de banco |
| app-09 | MySQL + GORM | pool esgotado (transação sem `Rollback`) | não | inconclusive | banco sem suporte; requisições presas nem viram span |
| app-10 | Postgres + pgx | CPU no handler (regex recompilada por chamada) | sim | inconclusive | nenhum detector de CPU |

- **Falso positivo: 0 de 7.** O Rootline não apontou causa errada em nenhum app.
- **Acerto: 0 de 7.** Esperado, porque as famílias estão fora dos dois detectores atuais. O caso
  que mais dói é o app-06: um índice faltando de verdade, invisível por ser MongoDB.
- **Sintoma: 7 de 7.** A rota que piorou bateu com o gabarito em todos os apps.

### O que funcionou

- O hook de chamador em quatro formas de conexão: pgxpool com otelpgx, pgx sem tracer próprio,
  pgx como driver de `database/sql` sob otelsql (`stdlib.RegisterConnConfig`) e GORM sobre pgx
  (`pgx.ParseConfig` → `stdlib.OpenDB` → `postgres.New`). No GORM, o hook anota os spans do
  plugin OpenTelemetry do próprio GORM.
- O Rootline se manteve calado quando não sabia, mesmo com regressões de 14x a 6000x.

### Lacunas novas

1. **`inconclusive` sem explicação.** Os sete analistas reclamaram. O Resultado não diz o que foi
   verificado, o que faltou (hook, planos, banco suportado) nem como obter os planos.
2. **Todas as rotas pioram juntas** quando o pool ou o lock satura (app-04, app-09). Fica difícil
   escolher a operação para o `analyze`.
3. **Requisição presa não vira span.** No app-09, de cerca de 1.274 claims só 27 chegaram ao
   trace. Requisições cortadas pelo timeout do cliente aparecem sem erro no span do servidor
   (app-06). O sintoma que o trace mostra fica menor que o real.
4. **A pista estava na evidência e não virou candidato.** No app-05, a mediana de queries por
   trace subiu de 4 para 5, e no app-08 a duração subiu sem mudança nas queries. Nenhum dos dois
   apareceu como pista.

### Lições do laboratório

- Aquecer antes de gravar. No app-07, a versão boa gravou com o banco frio e as rotas de escrita
  pareceram melhorar na versão ruim.
- Usar `docker compose run --no-deps`. Em dois apps, o `run` recriou a API pelo `depends_on` com o
  ambiente do shell, e a gravação perdeu os traces sem aviso.
- A versão muda o volume compartilhado (migração no app-04, índices recriados no startup do
  app-06). Tirar um snapshot do volume antes de gravar.
- A faixa de portas `6<NN>000` passa de 65535 a partir do app-06. Atribuir faixas válidas.

## Rodada 3: usuário cego com o README e o guia

Teste de aceitação do lote "Rootline usável por quem nunca o viu" (#60), com os ADRs 0027 a 0029 já
na `main`.

### Método

- **Um agente novo por app.** A única fonte sobre o Rootline eram cópias do `README.md` e do
  `docs/guia-instrumentacao.md`. O agente não podia ler o código do Rootline, as ferramentas do
  laboratório, os patches de hook das rodadas anteriores, os gabaritos nem os outros apps. Também
  não podia escrever scripts para ler os traces ou o Resultado, nem achar a causa lendo o código.
- **Instalação como usuário.** Rootline por `go install …/cmd/rootline@main` (commit `9d29c7a`) e hook por
  `go get …/hook@v0.1.0`. Cada agente escolheu a receita do guia, subiu o próprio Collector e gravou
  as duas versões com a carga do app.
- **Mesmas versões da rodada 2:** a referência é o último commit pelo menos 5 dias antes do HEAD.
  Os apps 01 a 03 foram regravados.
- **Os 10 apps em paralelo.** A máquina chegou a load average 36. Os volumes foram guardados antes e
  restaurados depois.
- **Gabarito aberto só depois** do relatório de cada agente.

### Resultado

| App | Receita do hook | `symptoms` sugeriu | Resultado | Bate com o gabarito |
|---|---|---|---|---|
| app-01 | 1 (otelpgx) | `POST /v1/devices/{id}/readings` (**errada**) | inconclusive na sugerida; N+1 na 2ª linha da tabela | causa, função, linha e commit, pela 2ª linha |
| app-02 | 1 (otelpgx) | nada ("none regressed") | N+1, high | causa, função, linha e commit |
| app-03 | 1 (otelpgx) | nada ("none regressed") | N+1, high | causa, função, linha e commit |
| app-04 | nenhuma (MySQL) | nada (generalizada) | inconclusive | sem falso positivo |
| app-05 | 3 (otelsql) | rota certa | inconclusive | sem falso positivo |
| app-06 | nenhuma (MongoDB) | rota certa (generalizada, uma destoa) | inconclusive | sem falso positivo |
| app-07 | 4 (GORM) | rota certa (regressão de erro) | inconclusive | sem falso positivo |
| app-08 | 1 (otelpgx) | rota certa | inconclusive | sem falso positivo |
| app-09 | nenhuma (MySQL) | nada (generalizada) | inconclusive | sem falso positivo |
| app-10 | 1 (otelpgx) | nada (generalizada, 9,6x contra 10x) | inconclusive | sem falso positivo |

- **Causa nos apps 01 a 03: 3 de 3**, com função, arquivo:linha e commit.
- **Falso positivo nos apps 04 a 10: 0 de 7.** Os sete agentes explicaram o `inconclusive` só pelas
  Verificações: banco sem suporte, hook ausente, planos que só a avaliação gera, regressão de erro
  fora da v1 ou causa fora dos detectores.
- **Script externo: 0 de 10.** O sintoma e o Resultado saíram só do `symptoms` e do `analyze`.
- **`symptoms`:**
  - certa em 4 apps;
  - nenhuma sugestão em 5;
  - **errada em 1** (app-01).
  - Nos apps sem sugestão, o agente achou a rota certa pela tabela (razão de p50 e início).
- **Receitas do hook:** as receitas 1, 3 e 4 funcionaram de primeira, com 100% dos spans de query com
  pilha em todos os apps com pgx.

### A sugestão errada do app-01

Na gravação em paralelo, a versão ruim do app-01 gravou cerca de metade dos traces de todas as rotas
(razão de n de 0,47 a 0,57). O p95 da versão boa no `POST /v1/devices/{id}/readings` já estava em
2,5 s. O `POST` ficou com p50 6,93x, contra 5,74x da rota certa, e foi sugerido. Ele estava marcado com
**drop**, e o `analyze` dele deu `inconclusive`.

Um agente cego novo regravou o app-01 sozinho, com a máquina parada:
- todas as rotas ficaram com razão de n igual a 1,00;
- só `GET /v1/sites/{id}/devices` regrediu (p50 6,39x);
- o `symptoms` sugeriu essa rota, e o `analyze` acertou causa, função, linha e commit.

A sugestão errada veio do ruído da gravação, mas a regra não se defendeu dele: sugeriu uma operação
com queda de amostra e p95 de referência já degradado. Pelo critério, a rodada oficial reprova
"nunca sugere errado".

### Lacunas novas

1. **O `symptoms` sugere sob gravação ruidosa.** Quando a razão de n cai em todas as rotas, ou a
   referência já tem cauda longa, a sugestão não é confiável. Ele avisa "drop", mas sugere mesmo
   assim (app-01).
2. **O p95 veta regressões claras no p50.** Nos apps 02 e 03, o p50 subiu 4,5x e 3,8x, e a tabela
   marcou "no" porque o p95 da referência estava ruidoso ou igual. O `analyze` acha o N+1 com
   confiança alta enquanto a `symptom_regression` diz "no latency regression", e nada explica a
   divergência.
3. **`db_spans` reconhece spans de MySQL.** Um span com `db.query.text` conta como query qualquer
   que seja o `db.system.name`. Nos apps 04 e 09, os spans `mysql` viraram "recognized" e passaram
   pelo N+1, contra o que o guia diz. Não houve falso positivo, mas a Verificação contradiz o guia.
4. **O guia não ensina a gravar em contêiner.** Todos os agentes tropeçaram em algum destes pontos:
   - não há `docker run` do Collector;
   - o file exporter roda como uid 10001 e precisa de `/out` com permissão de escrita;
   - o `localhost:4318` do guia não serve para um serviço em contêiner;
   - não diz como levar o patch do hook da versão boa para a ruim sem commitá-lo;
   - não diz como manter o aquecimento fora da gravação.
5. **O "Comece aqui" do README usa `go run ./cmd/...`.** Isso só funciona dentro do repo do Rootline.
   Todos os agentes usaram o `go install` do guia.
6. **Sem orientação quando não há sugestão ou a sugerida dá `inconclusive`.** O "compare as colunas
   de início e sumiço" não diz como decidir. No app-01, a causa estava na 2ª linha, e só a iniciativa
   do agente a achou.
7. **Dicas que não cabem no caso.** A dica do teto fala do N+1 quando ele foi pulado (app-06).
   Timeouts do cliente não aparecem em `error_rate` (app-06). O `symptoms` sugere uma regressão só
   de erro sem avisar que o `analyze` não a explica (app-07).
8. **CLI em `@main` e hook em `@v0.1.0`.** Vários agentes perguntaram qual CLI combina com qual hook.
   A tag da raiz resolve.

### Correções antes da tag

As lacunas 1, 3, 4, 5 e 8 foram corrigidas antes da `v0.1.0`, e as outras viraram issues próprias.

| Lacuna | Correção | Conferência |
|---|---|---|
| 1 | O `symptoms` não sugere com Carga desigual: razão de n mediana fora de [0,8; 1,25], com pelo menos 3 operações ([ADR-0030](../adr/0030-regressao-symptoms-e-carga-desigual.md), #63). | `symptoms` repetido nas 18 gravações das rodadas 2 e 3: o app-01 em paralelo fica sem sugestão, e as outras 17 mantêm a de antes. |
| 3 | Um span de outro banco não conta como query, qualquer que seja o tipo, e entra como ignorado na `db_spans` (#64). | Apps 04 e 09: os spans `mysql` aparecem como ignorados. Os apps 01 a 03 mantêm causa, linha e commit, e o placar do demo-app fica igual (7/7). |
| 4, 5 e 8 | O README instala o CLI por `go install`. O guia ganhou a seção "Serviço em contêiner" e diz qual CLI lê qual hook (#65). | Um agente cego novo gravou o app-03 só com o README e o guia novos. Não houve atrito de Collector, permissão ou endpoint, e a causa saiu certa. |

O mesmo agente achou de novo a lacuna 2: p50 4,3x marcado como "no". A diferença absoluta é de 2,8 ms,
abaixo dos 5 ms da regra, e a tabela não diz isso (#66).

## Rodada 4: detector de forma nas gravações guardadas

Conferência do [ADR-0031](../adr/0031-detector-de-forma-do-trace.md) (#76) antes de qualquer gravação
nova.

### Método

- **Sem regravar.** As 19 gravações são as 7 da rodada 2 e as 12 da rodada 3 (as 18 do ADR-0030
  mais o `app-03-guia`).
- `rootline analyze --repo` em toda operação com Regressão de cada gravação e na operação analisada no
  piloto, com as versões na ordem do git. Rootline no commit `c4028a3`.
- **Gabaritos abertos só depois** de conferir as Candidatas de forma e o N+1 dos apps 01 a 03.

### Resultado

A coluna "Operação" é a do gabarito. No app-09, a do gabarito (o claim) não vira span, e a linha usa a
operação analisada no piloto. "Dominante" é a linha da divisão do tempo que mais explica a diferença
da média. Os caminhos de função omitem o prefixo do módulo.

| Rodada | Gravação | Operação | Dominante | Promovido ou motivo | Gabarito | Confere |
|---|---|---|---|---|---|---|
| 2 | app-04 | `POST /v1/appointments` | tempo próprio, 78% | nada: nenhum mecanismo | lock numa linha de contador (MySQL) | sem causa errada |
| 2 | app-05 | `GET /api/v1/links/{code}/stats` | `postgresql`, 100%; presença 53% → 100% | `added_work`: `(*StatsService).compute`, `stats.go:115`, commit certo | cache sempre furado em `(*StatsService).LinkStats`, `stats.go:90` | mecanismo, alvo e commit; função um frame abaixo |
| 2 | app-06 | `GET /v1/traces/{traceID}` | `mongodb`, 100% | nada: banco mais lento | índice faltando no MongoDB | sem causa errada |
| 2 | app-07 | `GET /api/v1/products/{id}/stock` | – | não se aplica: regressão só de erro | divisão por zero (5xx) | sem causa errada |
| 2 | app-08 | `POST /rooms/{roomID}/messages` | HTTP `moderation:8090`, 99%; 3,4 → 110 ms por chamada | `slow_call`: `(*API).postMessage`, nível 2, sem commit | dependência HTTP lenta em `(*Client).Screen`, `client.go:72` | mecanismo e alvo; função é a que chama; sem commit |
| 2 | app-09 | `GET /v1/orders/{orderID}` | `mysql`, 100% | nada: banco mais lento | pool esgotado por transação sem `Rollback` | sem causa errada |
| 2 | app-10 | `GET /v1/tenants/{tenantID}/reports` | tempo próprio, 96% | nada: tempo próprio maior | regex compilada a cada chamada (CPU) | sem causa errada |
| 3 | app-01-paralelo | `GET /v1/sites/{id}/devices` | `postgresql`, 100% | nada de forma: banco mais lento. Causa pelo N+1 | N+1, `service.go:38` | causa, função, linha e commit |
| 3 | app-01-solo | `GET /v1/sites/{id}/devices` | `postgresql`, 90% | nada de forma: média +4,7 ms, abaixo da guarda de 5 ms. Causa pelo N+1 | N+1, `service.go:38` | causa, função, linha e commit |
| 3 | app-02 | `GET .../accounts/{accountID}/statement` | – | não se aplica: sem Regressão de latência (#66). Causa pelo N+1 | N+1, `service.go:177` | causa, função, linha e commit |
| 3 | app-03 | `GET /api/v1/books` | – | não se aplica: sem Regressão de latência (#66). Causa pelo N+1 | N+1, `store.go:225` | causa, função, linha e commit |
| 3 | app-03-guia | `GET /api/v1/books` | – | não se aplica: sem Regressão de latência (#66). Causa pelo N+1 | N+1, `store.go:225` | causa, função, linha e commit |
| 3 | app-04 | `POST /v1/appointments` | tempo próprio, 75% | nada: nenhum mecanismo | lock numa linha de contador (MySQL) | sem causa errada |
| 3 | app-05 | `GET /api/v1/links/{code}/stats` | `postgresql`, 100%; presença 56% → 100% | `added_work`: `(*StatsService).compute`, `stats.go:115`, commit certo | cache sempre furado em `(*StatsService).LinkStats`, `stats.go:90` | mecanismo, alvo e commit; função um frame abaixo |
| 3 | app-06 | `GET /v1/traces/{traceID}` | `mongodb`, 100% | nada: banco mais lento | índice faltando no MongoDB | sem causa errada |
| 3 | app-07 | `GET /api/v1/products/{id}/stock` | – | não se aplica: regressão só de erro | divisão por zero (5xx) | sem causa errada |
| 3 | app-08 | `POST /rooms/{roomID}/messages` | HTTP `moderation:8090`, 100%; 3,5 → 108 ms por chamada | `slow_call`: `(*API).postMessage`, nível 2, sem commit | dependência HTTP lenta em `(*Client).Screen`, `client.go:72` | mecanismo e alvo; função é a que chama; sem commit |
| 3 | app-09 | `GET /v1/orders/{orderID}` | – (a média caiu de 126 para 64 ms) | nada: a média não acompanhou o p50 | pool esgotado por transação sem `Rollback` | sem causa errada |
| 3 | app-10 | `GET /v1/tenants/{tenantID}/reports` | tempo próprio, 89% | nada: tempo próprio maior | regex compilada a cada chamada (CPU) | sem causa errada |

- **Candidata de forma: 4 gravações, as esperadas** (app-05 e app-08, nas duas rodadas). Nas outras
  15, nenhuma, em nenhuma das operações analisadas.
- **Apps 01 a 03: 5 de 5** com o N+1 certo, e causa, função, arquivo:linha e commit iguais aos da
  rodada 3.
- **Sem causa errada** nas 10 gravações restantes, todas `inconclusive`.

### O que a localização não alcança

- **`added_work`.** O frame comum mais interno às agregações é a função que as executa. O defeito do
  app-05 (a chave do cache com `time.Now()`) está na função que a chama e não emite span.
- **`slow_call`.** O span HTTP não tem pilha: a localização é o handler, e não há commit. O gabarito
  aponta o cliente HTTP, que o commit da regressão alterou. Anotar spans de cliente com o hook ficou
  fora do ADR-0031.

### Achado menor

Quando a média sobe menos de 5 ms (app-01-solo e várias rotas do app-10), a `trace_shape` diz que "a
média não acompanhou o p50", mas o motivo é a guarda de 5 ms.

## Rodada 5: Regressão com p50 que dobra nas gravações guardadas

Conferência do [ADR-0032](../adr/0032-regressao-com-p50-que-dobra.md) (#66): a latência regride
também com p50 ≥ 2x e 2 ms a mais, com o p50 da versão ruim em pelo menos 3 ms, a `symptom_regression` diz o limiar que faltou e uma Causa sem
Regressão de latência ganha um aviso.

### Método

- **Sem regravar.** As mesmas 19 gravações da rodada 4.
- `rootline symptoms --repo` em cada gravação e `rootline analyze --repo` em toda operação com
  amostra mínima (138 operações), com as versões na ordem do git. Cada comando rodou com dois
  binários: o da `main` (`f05ec05`) e o da branch do #66, com o #87 e o #88. A comparação é entre os
  dois.
- **Gabaritos abertos só depois** da conferência, e só os do app-02 e do app-03, para conferir a
  rota sugerida.

### Resultado

"Regressões" conta as operações com Regressão no `symptoms`. "Sugestão" é a operação sugerida, ou –
sem sugestão. Nas linhas sem mudança, só uma coluna mostra o valor.

| Rodada | Gravação | Operações | Regressões | Sugestão | `trace_shape` na sugerida | Aviso de Causa sem Regressão |
|---|---|---|---|---|---|---|
| 2 | app-04 | 6 | 6 | – (Carga desigual) | igual | nenhum |
| 2 | app-05 | 4 | 1 | `GET /api/v1/links/{code}/stats` | igual | nenhum |
| 2 | app-06 | 5 | 4 | `GET /v1/traces/{traceID}` | igual | nenhum |
| 2 | app-07 | 8 | 1 | `GET /api/v1/products/{id}/stock` | igual | nenhum |
| 2 | app-08 | 6 | 1 | `POST /rooms/{roomID}/messages` | igual | nenhum |
| 2 | app-09 | 7 | 7 | – (regressão generalizada) | igual | nenhum |
| 2 | app-10 | 10 | 2 | `GET /v1/tenants/{tenantID}/reports` | igual | nenhum |
| 3 | app-01-paralelo | 8 | 3 | – (Carga desigual) | igual | nenhum |
| 3 | app-01-solo | 9 | 1 | `GET /v1/sites/{id}/devices` | igual | nenhum |
| 3 | app-02 | 7 | **0 → 1** | **– → `GET .../accounts/{accountID}/statement`** | **passa a rodar**: média 69,5 → 12,0 ms, não acompanhou o p50; nada promovido | nenhum |
| 3 | app-03 | 11 | **0 → 1** | **– → `GET /api/v1/books`** | **passa a rodar**: média 1,2 → 4,0 ms, abaixo da guarda de 5 ms; nada promovido | **`/api/v1/search` e `/api/v1/books/{bookID}/related`** |
| 3 | app-03-guia | 11 | **0 → 1** | **– → `GET /api/v1/books`** | **passa a rodar**: média 0,9 → 3,8 ms, abaixo da guarda de 5 ms; nada promovido | **`/api/v1/search` e `/api/v1/books/{bookID}/related`** |
| 3 | app-04 | 6 | 6 | – (Carga desigual) | igual | nenhum |
| 3 | app-05 | 4 | 1 | `GET /api/v1/links/{code}/stats` | igual | nenhum |
| 3 | app-06 | 5 | 5 | `GET /v1/traces/{traceID}` | igual | nenhum |
| 3 | app-07 | 8 | 1 | `GET /api/v1/products/{id}/stock` | igual | nenhum |
| 3 | app-08 | 6 | 1 | `POST /rooms/{roomID}/messages` | igual | nenhum |
| 3 | app-09 | 7 | 7 | – (Carga desigual) | igual | nenhum |
| 3 | app-10 | 10 | 10 | – (regressão generalizada) | igual | nenhum |

- **Só as 3 operações-alvo mudam no `symptoms`.** Nas três, a operação ganha Regressão de latência e
  passa a ser sugerida, e é a rota do gabarito. As outras 16 gravações têm a saída do `symptoms`
  idêntica.
- **O `analyze` não muda o que acha.** Status, Causa (tipo, alvo, função, arquivo:linha e commit) e
  Candidatas iguais nas 138 operações: 13 `cause_found` e 125 `inconclusive`, como antes.
- **A `trace_shape` roda nas 3 e não promove nada.** No app-03 e no app-03-guia, ela cita a guarda
  de 5 ms. No app-02, a média caiu, e ela diz que a média não acompanhou o p50.
- **O aviso aparece em 4 operações, as esperadas:** `/api/v1/search` e `/api/v1/books/{bookID}/related`
  no app-03 e no app-03-guia, com o N+1 certo em `attachAuthors` (`store.go:225`) e p50 de só 1,3x.
  Das 13 operações com Causa, são as únicas sem Regressão de latência. Lá, a `symptom_regression`
  diz, por exemplo, `p50 1.30x (+2.42 ms): under 1.5x and under 2x; p95 0.48x (-15.96 ms): under 2x`.
- **O mínimo de 3 ms não muda nada aqui.** O menor alvo chega a 3,74 ms (app-03-guia), e nenhuma
  outra operação dobrou o p50 com 2 ms a mais. Ele protege a sugestão de uma operação que continua
  extremamente rápida, caso que estas gravações não têm.
- **Placar igual** ao `eval/placar.json`: Causa 10/10, falso positivo 0/3, alucinação 0/2,
  abstenção indevida 0/9.

## Próximos passos

Estão no [roadmap](../roadmap.md).
