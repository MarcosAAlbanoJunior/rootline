---
id: ROADMAP
title: Radar dos próximos passos do Rootline
status: Vivo
summary: Ordem dos próximos passos depois do piloto em dez apps reais. Primeiro tirar o atrito de uso (explicar o inconclusive, achar o sintoma, instalar o hook), depois ampliar o que o Rootline enxerga (forma do trace, erro, outros bancos). A próxima grande mudança é o watch automático por deploy; depois dele, a visão é levar a produção para quem escreve o código (MCP para agentes, revisão de PR com dados de produção).
---

# Radar dos próximos passos

Base: o [piloto em dez apps reais](spikes/2026-11-24-piloto-apps-reais.md). Na rodada 2, o Rootline
não deu nenhum falso positivo, mas também não acertou nada, porque as sete famílias de problema
estão fora dos dois detectores atuais (N+1 e índice faltando, só em Postgres com pgx). Cada item
abaixo diz quais apps do piloto ele cobriria.

Itens que mudam a arquitetura passam por `/grill-me` e ganham um ADR antes dos tickets.

## Próxima grande mudança: Rootline automático por deploy

Um `rootline watch` que lê os traces do Tempo ou do Jaeger, percebe cada versão nova, compara cada
operação com a anterior assim que houver amostra e publica a Causa, sem passos manuais e sem rodar
no CI. Proposta completa, com as decisões que pedem ADR e o plano em fatias:
[proximo-passo-watch.md](proximo-passo-watch.md).
As decisões estão nos ADR-0033 a ADR-0040. O spike do Tempo (fatia 0) está feito; o próximo passo é `/to-tickets` com as
fatias 1 a 6.

## Visão: levar a produção para o momento em que o código é escrito

Depois do watch, o Rootline tem algo raro: a ligação, sempre atualizada e com evidência, entre o
comportamento real em produção e a função, a linha e o commit. Hoje ele usa essa ligação para olhar
para trás (o deploy piorou, qual commit foi?). A visão é usá-la **antes**: quando o código está
sendo escrito ou revisado, inclusive por um agente.

O gargalo que isso ataca: com boa parte do código escrita por IA, o difícil deixou de ser escrever e
passou a ser revisar e confiar. Um PR gerado por agente passa nos testes e coloca um N+1, porque nem
o agente nem o revisor sabem que aquela função roda 4 mil vezes por minuto com 24 pedidos por
cliente. Essa informação só existe em produção.

Itens que mudam a arquitetura passam por `/grill-me` e ADR, como o resto do radar. A ordem abaixo é
a sugerida.

### V1. Rootline como ferramenta de agentes de código (MCP)

**É opcional e fica desligado por padrão.** O `symptoms`, o `analyze` e o watch funcionam sem ele.
Quem quiser liga o MCP explicitamente, e ele só funciona com duas coisas: os traces guardados num
backend que o Rootline consegue ler (Tempo, Jaeger ou outro com adapter) e o perfil agregado por
versão que o watch calcula ([proposta do watch](proximo-passo-watch.md#onde-ficam-os-traces-e-o-que-o-rootline-guarda)).
Quem usa só o fluxo com arquivo, ou não guarda traces, não tem o MCP e não perde nada do resto.

O Rootline ganha um servidor MCP (`rootline mcp` no laptop do dev ou o próprio watch respondendo por
HTTP). Nada muda em produção: os pods continuam só emitindo traces, e o Rootline consulta o backend
de traces e o clone do repositório, só leitura, como hoje.

Ferramentas que o agente chama:

| Ferramenta | O que responde |
|---|---|
| `function_in_production(função)` | Rotas que passam por ela, chamadas por minuto, queries que dispara e linhas por query (do hook) |
| `route_profile(rota)` | p50 e p95 atuais e a divisão do tempo entre banco, chamadas externas e tempo próprio |
| `deploy_impact(versão)` | O que o watch achou na versão: Regressões, Causas e operações ainda sem amostra |
| `analyze(...)` | O `analyze` de hoje |

O comportamento que se quer do agente, antes de mudar o código:

> Vou fazer o que você pediu, mas antes consultei a produção. `orders.(*Service).List` atende
> `GET /customers/{id}/orders`, com 4 mil requisições por minuto e 24 pedidos por cliente em média
> (4 008 traces da versão `9b7d31c`). Buscar os itens dentro do loop daria 24 queries a mais por
> requisição, cerca de 96 mil por minuto no Postgres. Vou buscar todos numa query só, com
> `WHERE order_id = ANY($1)`.

A consulta acontece duas vezes. Antes de escrever, o agente consulta as funções que planeja alterar.
Ao terminar, consulta de novo cada função que aparece no diff, porque a mudança pode ter tocado algo
que não estava no plano. A fonte são os traces com o hook, não logs: eles já ligam cada requisição à
rota, à função, à linha e à query, com contagens.

E depois do deploy: "minha mudança de ontem piorou algo?" vira uma chamada a `deploy_impact`, com a
Causa, a linha e o commit, sem abrir o painel de observabilidade.

O ciclo fecha quando o watch confirma, no deploy seguinte, que a regressão sumiu. Propor correção
muita ferramenta propõe; provar com dados de produção que a correção funcionou quase nenhuma prova. O
Rootline continua sem escrever código: ele dá o diagnóstico e a prova, e o agente faz a mudança.

É barato porque o difícil (ler traces, mapear para o código, comparar versões) já existe. O MCP é
uma camada fina sobre o watch.

### V2. Revisão de PR com dados de produção

O mesmo dado, num comentário no PR antes do merge: "este PR muda `orders.(*Service).List`, que está
no caminho de uma rota com 4 mil req/min; a linha 42 passa a chamar `ItemsByOrder` dentro de um loop
que, em produção, roda 24 vezes por requisição; estimativa de +24 queries por requisição". Junta o
hook (função → query, com contagens reais), o `go/ast` (o loop no diff) e os traces (cardinalidade e
tráfego). Nenhum linter sabe quantas vezes o loop roda, e nenhum APM olha o diff.

A revisão **prevê** o impacto a partir do diff e do Perfil de versão da base, e não sobe a versão
do PR para medir: medir seria o job `placar` a cada PR, lento e com carga de laboratório. Vem
depois do watch, porque o perfil que ela consulta é o dele.

É a aposta mais difícil: a estimativa pode errar. Por isso ela segue as regras do `analyze`: toda
afirmação cita a evidência, e sem evidência o comentário diz que não sabe.

### V3. Benchmark aberto de análise de causa raiz

Os cenários com commits reais, o gabarito lacrado, a alucinação medida e o placar público viram um
benchmark que outras ferramentas de análise de causa raiz rodam, no espírito do SWE-bench para código.
Pode correr em paralelo, porque a base já existe em `eval/`.

### V4. Custo por commit

Traduzir a Regressão para custo: "o commit `7e2a5f1` acrescenta cerca de 96 mil queries por minuto",
e, com o custo do banco configurado, quanto isso custa por mês. É o mesmo dado numa unidade que
gestão entende.

### V5. Histórico de impacto

Cada deploy deixa um registro: quais commits mudaram o comportamento em produção, de que tipo, em que
parte do código e se vieram de um agente ou de uma pessoa. Com o tempo, ele responde quais partes do
código mais regridem e se os PRs de agente regridem mais. Pares de diff e efeito real em produção,
com evidência, quase não existem.

## Curto prazo: tirar o atrito de uso

1. **Explicar o `inconclusive`.** O Resultado diz o que foi verificado, o que faltou (hook de
   chamador, planos, banco suportado) e como resolver cada falta. Foi a reclamação dos sete
   analistas da rodada 2.
2. **`rootline symptoms`.** Compara latência e erro por rota entre duas versões e aponta a rota de
   origem. Precisa tratar o caso em que todas as rotas pioram juntas (app-04, app-09), as
   requisições cortadas por timeout e as que não viraram span.
3. **Guia "instrumentar um app" e tag `hook/v0.1.0`.** O guia traz as quatro receitas de instalação
   já validadas: pgxpool com otelpgx, pgx sem tracer próprio, pgx sob otelsql e GORM sobre pgx.
   Inclui o Collector com file exporter.

## Médio prazo: ampliar o que o Rootline enxerga

4. **Detector de forma do trace** ([ADR-0031](adr/0031-detector-de-forma-do-trace.md)). Divide a
   diferença da média por trace entre as dependências e o tempo próprio, e sai como evidência em todo
   Resultado com Regressão de latência. Só promove a Candidata dois mecanismos: a chamada externa mais
   lenta (app-08) e o trabalho a mais por requisição (app-05, cache que passou a furar). Banco mais
   lento e tempo próprio maior ficam como pista (app-06, app-09, app-10, app-04).
   **Feito** e conferido na
   [rodada 4 do piloto](spikes/2026-11-24-piloto-apps-reais.md#rodada-4-detector-de-forma-nas-gravações-guardadas).
   Ficou de fora a linha e o commit da chamada externa, que dependem do hook anotar spans de cliente
   (item 6), e a função do defeito quando ela não emite span (app-05).
5. **Sintoma de erro.** Hoje o sintoma é só latência (ADR-0005). Cobriria app-07 e app-09. Precisa
   de ADR.
6. **Cobertura de bancos.** Hook de chamador para `database/sql` (app-04, app-09), spans do MongoDB
   como queries (app-06) e captura de plano fora do Postgres.

## Depois

7. **Lock e contenção no banco** (app-04) e **CPU no handler** (app-10) como detectores próprios.
   Os dois precisam de dados que o trace não traz, como o estado de lock do banco ou o profiling.
   Talvez fiquem fora do escopo.
8. **Explainer ao vivo** (ADR-0012), para o índice faltando rodar num banco real.
9. **Modo pipeline com LLM**, item central do brief.

## Pendências pequenas

- O `CLAUDE.md` ainda lista os pacotes de domínio sem `internal/explain`.
- Issue #5 (placar no CI) aberta.
