---
id: SPIKE-2026-11-24-piloto-apps-reais
title: Piloto do Rootline em três apps reais escritos por quem não conhece a ferramenta
status: Concluído
summary: Três apps Go escritos por agentes sem contexto do Rootline; o Rootline acertou causa, função, arquivo:linha e commit nos 3. Os três escolheram Postgres + pgx + otelpgx e um N+1, então o piloto valida o caminho principal fora do demo-app mas não estressa a diversidade. Lacunas do uso real - achar o sintoma, instalar o hook, índice faltando sem planos.
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

## Resultado

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

## Limites deste piloto

- **Pouca diversidade.** Mesmo com banco e tipo de problema livres, os três criadores escolheram
  Postgres + pgx + otelpgx e um N+1, que é o caminho mais testado do Rootline. O piloto não diz
  nada sobre MySQL, MongoDB, ORMs, erros, lock ou índice faltando em apps reais.
- **Isolamento imperfeito.** Os criadores usaram uma pasta temporária compartilhada que tinha
  arquivos de trabalho do Rootline. Os repos dos apps não citam o Rootline, mas não há como provar
  que nenhum criador leu esses arquivos. Dois criadores revelaram os SHAs no relatório final, o que
  o prompt proibia. As versões comparadas seguiram a mesma regra neutra em todos os apps.
- **Três apps.** É um piloto do processo, não uma medida de acerto.

## Próxima rodada (7 apps)

- Uma pasta temporária por criador, sem acesso aos arquivos do Rootline.
- Stack e família de problema atribuídas por app, a partir de uma lista genérica de SRE (lock,
  falta de cache, índice faltando, erro 5xx, timeout de dependência, pool esgotado, CPU), que não
  depende do que o Rootline detecta.
- O relatório final do criador sem SHAs nem referências relativas a commits.
