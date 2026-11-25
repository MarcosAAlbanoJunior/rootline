---
id: SPIKE-2026-11-24-piloto-apps-reais
title: Piloto do Rootline em dez apps reais escritos por quem não conhece a ferramenta
status: Concluído
summary: Rodada 1 (3 apps, stack livre) - todos Postgres + pgx + N+1, e o Rootline acertou os 3. Rodada 2 (7 apps, stack e família atribuídas) - 7 inconclusive, nenhum falso positivo, nenhum acerto, porque as famílias estão fora dos detectores atuais. Rodada 3 (10 apps, usuário cego só com README e guia) - 3/3 causas, 0/7 falsos positivos, sem script externo; o symptoms sugeriu errado no app-01 sob gravação ruidosa. Orienta o docs/roadmap.md.
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

## Próximos passos

Estão no [roadmap](../roadmap.md).
