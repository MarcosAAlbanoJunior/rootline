---
id: ADR-0027
title: Explicar o Resultado por Verificações estruturadas, com saída em texto por padrão
status: Aceito
date: 2026-11-25
summary: O Resultado ganha o campo checks, uma Verificação por detector ou pré-condição (id, status ran/skipped/not_applicable, reason, hint); warnings fica só para avisos sobre o próprio resultado. A CLI imprime texto Markdown por padrão (--format json para máquinas) e sai com 0 em qualquer análise concluída. Traces que pararam no teto saem da contagem do N+1.
modules: [analysis, detector, cmd/rootline]
---

# 0027. Explicar o Resultado por Verificações estruturadas

## Contexto

Na rodada 2 do piloto em apps reais (`docs/spikes/2026-11-24-piloto-apps-reais.md`), os sete
analistas receberam `inconclusive` sem candidatas. A única explicação era o aviso
`missing_index_skipped: no query plans`, que não diz como obter os planos. Nenhum Resultado dizia:

- que o hook de chamador não estava instalado (app-04, app-06, app-09);
- que os spans de banco do MongoDB foram ignorados (app-06);
- que a taxa de erro subiu e a v1 não analisa erro (ADR-0005; app-07, app-09);
- que os traces da versão ruim pararam num timeout e têm menos spans filhos (app-04: 6 queries por
  trace contra 21 na referência).

Hoje o `Result` tem `warnings []string` com prefixos como identificador estável. Os analistas
precisaram de um script externo (`show.py`) para ler o JSON. A CLI só imprime JSON e sai com 0 sem
nada no stderr.

## Decisão

O Resultado passa a explicar o que foi verificado, por meio de uma lista de **Verificações**.

**Contrato.** Novo campo `checks`, uma entrada por detector ou pré-condição:
- `id`: identificador estável;
- `status`: `ran`, `skipped` ou `not_applicable`;
- `reason`: o fato observado, com números (ex.: "0 spans de banco reconhecidos; 1.204 spans com
  `db.system.name=mongodb` ignorados");
- `hint`: o que o usuário faz a seguir, ou a limitação atual dita com honestidade.

O `schema_version` do Resultado sobe. `warnings` fica só para avisos sobre o resultado em si, como a
localização aproximada ou a referência sem traces. O aviso `missing_index_skipped:` vira a
Verificação `missing_index`.

**Verificações desta versão:**

| id | O que relata |
|---|---|
| `symptom_regression` | Se a operação regrediu (regra do ADR-0030), com p50, p95 e n de cada versão e a razão de n |
| `error_rate` | Taxa de erro nas duas versões; quando ela regride, diz que a v1 não analisa erro (ADR-0005) |
| `truncated_traces` | Quantos traces da versão ruim pararam no teto (ADR-0030) |
| `caller_hook` | Fração dos spans de banco com pilha da aplicação e a versão do hook vista (ADR-0029) |
| `db_spans` | Spans de banco reconhecidos e spans de banco ignorados, por sistema |
| `n_plus_one` | Se o detector rodou, ou por que não |
| `missing_index` | Se o detector rodou, ou por que não. Sem planos, diz que hoje eles só são gerados na avaliação e que a captura num banco de teste está no roadmap |
| `commit_correlation` | Se a correlação com commit rodou, ou por que não (sem referência, SHA fora do repo) |

Cada detector declara as próprias pré-condições e devolve a própria Verificação. A montagem fica no
domínio (`internal/analysis`).

**Fora desta decisão:** dizer para onde foi o tempo (handler, span HTTP, banco). Isso é um
detector, não uma Verificação, e fica para o detector de forma do trace.

**Teto e N+1.** Os traces da versão ruim que pararam no teto (definição no ADR-0030) saem da
contagem de queries por trace do detector de N+1, porque foram cortados e têm menos spans filhos.

**Status.** O conjunto de status não muda. Sem regressão de latência, os detectores rodam do mesmo
jeito, e a Verificação `symptom_regression` diz isso.

**Saída da CLI.**
- `--format text|json`, com `text` como padrão. O texto é Markdown simples, nesta ordem: sintoma
  numa linha, causa ou "Inconclusivo", evidências, Verificações (as puladas primeiro, com dica).
- `json` é o `Result` sem mudança de forma.
- O renderizador de texto é um adapter de saída, fora do domínio.
- Exit code 0 para qualquer análise concluída, `conclusive` ou `inconclusive`; diferente de 0 só
  para erro de uso ou de entrada. O status de CI vem do JSON.

O `rootline-eval` chama `analysis.Analyze` direto e não depende do formato da CLI.

## Alternativas consideradas

- **Manter `warnings` com mais prefixos.** Não muda o contrato, mas o parse de string já é frágil
  com um prefixo só. Não dá para testar por detector nem para renderizar texto sem reparsear. Perdeu.
- **Status novo (`no_regression`, `unsupported`).** Mais explícito, mas muda o contrato do status e
  a pontuação da avaliação (ADR-0026) sem ganho sobre a Verificação. Perdeu.
- **Manter JSON como padrão e só documentar o `jq`.** Mais barato, mas o critério de pronto do lote
  proíbe script externo, e quem nunca viu o Rootline lê o terminal. Perdeu.
- **Exit code diferente para `inconclusive`.** Útil em CI, mas `inconclusive` é um resultado
  legítimo, e tratá-lo como falha confunde o uso interativo. Perdeu.

## Consequências

- Quem lê o Resultado sabe o que foi checado e o que fazer, sem ler o código.
- Cada detector passa a ter um ponto a mais para manter (as próprias pré-condições).
- Scripts que leem a saída padrão da CLI como JSON precisam passar `--format json`. Ainda não há
  usuários externos.
- A exclusão dos traces no teto muda a contagem do N+1 só quando há teto. O placar atual não tem
  cenário com teto e não deve mudar.

## Como validar

- Testes unitários por Verificação no domínio, um por status.
- Teste de golden file do texto renderizado.
- O placar continua igual (`rootline-eval score`).
- Teste de aceitação com usuário cego: nos apps 04 a 10 do laboratório, um agente que só leu o
  README e o guia explica o que faltou, sem script externo.
