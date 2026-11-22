---
id: ADR-0002
title: Localizar a origem de queries pela pilha da aplicação capturada no span de banco
status: Aceito
date: 2026-11-22
summary: Hook de chamador (multitracer do pgx) grava em rootline.app_stack os frames do módulo principal e marca a fase do span (query/prepare); o índice (AST) acha o primeiro frame dentro de um loop; sem hook, rota → handler (aproximada). Spike confirmou 4/4 cenários; custo ~3,6µs/query (~1,9µs com cache).
modules: [hook, dominio, indice, demo-app, eval]
---

# 0002. Localizar a origem de queries pela pilha da aplicação capturada no span de banco

> Histórico: a primeira versão deste ADR, para Java, nunca foi aceita e foi reescrita depois da
> mudança para Go (ADR-0003, hoje ADR-0007). Aceito em 22/11/2026 com base no
> [spike](../spikes/2026-11-22-adr-0002-evidencias.md).

## Contexto

O Resultado precisa apontar a função e a linha responsáveis pela regressão. Em Go não há agente
automático: a instrumentação padrão é por biblioteca (otelhttp, otelpgx). Nenhum wrapper de banco
registra de onde a query foi chamada (pesquisa `docs/research/2026-11-22-instrumentacao-go.md`).

**Ponto crítico:** no N+1, a função que executa a query não é onde está o bug. O bug é o loop que
a chama, muitas vezes em outra camada. Um único frame, o chamador imediato, aponta o lugar errado.

## Decisão

**O que o Rootline exige do serviço analisado:** OTel Go SDK, otelhttp, otelpgx e o **hook de
chamador**, um pacote pequeno do módulo `hook/`. O hook vale igualmente para todas as queries.
Continua proibido criar spans manuais onde sabemos que está o bug. O **OBI (eBPF)** fica registrado
como fonte complementar futura de spans; não substitui o hook porque não fornece localização.

**Composição:** o hook é um `pgx.QueryTracer` e `pgx.PrepareTracer` colocado **depois do otelpgx**
no `github.com/jackc/pgx/v5/multitracer`, que repassa o `ctx` em ordem; o hook enriquece o span
que o otelpgx criou. Sem embrulho próprio. Plano B (se um dia for preciso): `database/sql` via
`pgx/stdlib` + otelsql com `AttributesGetter`.

**O que o hook grava em cada span de banco**
- `code.function.name`, `code.file.path` e `code.line.number` (estáveis na semconv) do frame mais
  interno da aplicação, com caminho relativo ao módulo (ADR-0011).
- `rootline.app_stack`: lista de strings `função arquivo:linha`, só frames do módulo principal,
  **no máximo 8 frames e 2048 bytes**. Não usa `code.stacktrace`, que é estável mas definido como
  string no formato natural do runtime (caminhos absolutos, sem filtro).
- `rootline.module.path`: caminho do módulo principal.
- `rootline.db.phase`: `query` ou `prepare`. O otelpgx cria spans de prepare com o mesmo nome e o
  mesmo SQL da query; sem essa marca, eles inflariam a contagem de repetições.

**Cache de resolução por PC**, ligado por padrão, guardando a **expansão completa de inlining** de
cada PC (um PC de função inlinada corresponde a vários frames; guardar só o primeiro pode perder o
frame do loop).

**Localização em dois níveis**, com a confiança marcada no Resultado:
1. **Pilha da aplicação no span de banco.** As linhas vêm dos frames e correspondem ao SHA em
   execução. O índice (AST) percorre os frames do mais interno para fora e escolhe o primeiro cujo
   ponto de chamada está dentro do corpo de um `for`/`range`. Isso cobre, sem caso especial:
   - goroutines e errgroup: a pilha termina na closure, cuja linha está dentro do loop que a lança;
   - range-over-func: o corpo do loop aparece como função sintética `...-range1`, com a linha
     dentro do corpo do `for range`.
2. **Fallback, sem o hook:** rota da operação de entrada (ADR-0013) → índice localiza o registro
   da rota → função handler, marcada como "localização aproximada" (função, sem linha).

**Localizar não é classificar.** A regra do loop só diz onde está o código responsável. Quem decide
que algo é N+1 é o detector, pela evidência do trace (critério na spec do detector, com os sinais:
linhas por query, proporção com o "+1" e padrão dos parâmetros). O detector conta só spans com
`rootline.db.phase = query`. Sem o hook, usa uma heurística (tem `pgx.prepare_stmt.name` e não
tem `pgx.rows_affected` → prepare), e o Resultado marca a confiança como menor. O otelpgx v0.13.0
não tem opção para desligar spans de prepare.

**Variações de N+1 da avaliação:** helper em outra camada; goroutines lançadas num loop
(errgroup); iterador com range-over-func; e o distrator de paginação em lotes fixos.

## Evidências do spike

[`docs/spikes/2026-11-22-adr-0002-evidencias.md`](../spikes/2026-11-22-adr-0002-evidencias.md),
com código e dados brutos na branch `spike/adr-0002`:
- a regra acertou os 4 cenários (explicit, helper, errgroup, rangefunc), e a query "+1" não apontou loop;
- `code.file.path` idêntico com e sem `-trimpath`;
- custo por query: captura ~0,91 µs, resolução ~1,95 µs (~0,23 µs com cache), atributos ~0,75 µs;
- `pgx.rows_affected` presente em todo span de query.

## Alternativas consideradas

- **Só bibliotecas padrão, sem hook.** Toda localização cai no nível 2 (só o handler).
- **OBI (eBPF).** Estável e zero-code, mas sem localização no código e com exigências de kernel e privilégio.
- **Só o chamador imediato.** Aponta a função que faz a query, não o loop que a multiplica.
- **`code.stacktrace`.** Nome estável, mas formato diferente do que o Rootline precisa.
- **Embrulho próprio de `pgx.QueryTracer`.** Desnecessário: o pgx já tem `multitracer`.
- **Spans manuais nas funções da aplicação.** Proibido.

## Consequências

- O Rootline publica e mantém o módulo `hook/`, com testes e documentação de instalação.
- O índice precisa responder se uma linha está dentro de um corpo de loop.
- A avaliação reporta acerto com e sem o hook, e o overhead medido na gravação (ADR-0008). O spike
  mostrou que medir overhead de forma confiável exige mais rodadas e ambiente controlado: a mesma
  configuração variou ±10% entre rodadas.
- Risco: custo do hook em serviços com muitas queries. Mitigações: profundidade e tamanho
  limitados, cache por PC; amostragem se a medição na gravação mostrar necessidade.

## Como validar

- Testes do hook: derivação de caminho (closures, range-over-func, genéricos, `main`, `/v2`, módulo
  com prefixo parecido, código de terceiros); limites de frames e bytes; cache com função inlinada;
  fase `query`/`prepare`.
- Testes do índice: regra do loop com pilhas e arquivos em fixtures, incluindo errgroup e range-over-func.
- Avaliação: acerto de localização por nível e por variação, com e sem o hook.
