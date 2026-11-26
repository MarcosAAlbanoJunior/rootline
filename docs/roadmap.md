---
id: ROADMAP
title: Radar dos próximos passos do Rootline
status: Vivo
summary: Ordem dos próximos passos depois do piloto em dez apps reais. Primeiro tirar o atrito de uso (explicar o inconclusive, achar o sintoma, instalar o hook), depois ampliar o que o Rootline enxerga (forma do trace, erro, outros bancos).
---

# Radar dos próximos passos

Base: o [piloto em dez apps reais](spikes/2026-11-24-piloto-apps-reais.md). Na rodada 2, o Rootline
não deu nenhum falso positivo, mas também não acertou nada, porque as sete famílias de problema
estão fora dos dois detectores atuais (N+1 e índice faltando, só em Postgres com pgx). Cada item
abaixo diz quais apps do piloto ele cobriria.

Itens que mudam a arquitetura passam por `/grill-me` e ganham um ADR antes dos tickets.

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
