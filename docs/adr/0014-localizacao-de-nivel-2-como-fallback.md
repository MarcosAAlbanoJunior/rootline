---
id: ADR-0014
title: Usar a rota → handler como localização de nível 2 quando o nível 1 falha
status: Aceito
date: 2026-11-22
summary: Sem localização de nível 1 (pilha), a análise devolve rota → handler, só com função e arquivo, marcada como aproximada e com o motivo nos warnings; Location só é nula se o nível 2 também falhar. O nível 2 nunca conta como acerto em Linha, mas fica no denominador.
modules: [analysis, goindex, detector, eval]
---

# 0014. Usar a rota → handler como localização de nível 2 quando o nível 1 falha

## Contexto
O ADR-0002 define a localização de nível 1 (primeiro frame da `rootline.app_stack` dentro de um loop) e cita a rota → handler para quando não há hook. O ADR-0011 manda o caminho que não resolve cair para o nível 2. Faltava dizer o que o Resultado contém nesse caso e como a métrica Linha (ADR-0009) o trata. Sem isso, uma candidata sem pilha aparecia sem nenhuma localização, o que é pior que uma localização aproximada e honesta.

## Decisão
Quando o nível 1 não é possível (sem hook, pilha ausente, caminho que não resolve, ambiguidade), o pipeline tenta o nível 2: acha o registro da rota da operação (`mux.HandleFunc("GET /x", h.list)`) no SHA ruim e devolve o handler declarado.
- `Location` do nível 2 tem `level: 2`, `function`, `file` e `approximate: true`. Não tem `line` e `in_loop` fica ausente, porque nada foi verificado.
- O motivo da queda entra nas `observations` e nos `warnings` do Resultado. Erros do nível 2 (leitura, ambiguidade) também viram `warnings`, nunca são engolidos.
- `Location` é nula só se o nível 2 também falhar (sem índice com suporte, registro não encontrado ou erro), sempre com o motivo nos `warnings`.
- Na métrica Linha, localização ausente ou de nível 2 conta como erro e permanece no denominador. A métrica traz a distribuição dos níveis (ADR-0009).

## Alternativas consideradas
- **Devolver `null` sem nível 2.** Simples, mas joga fora uma pista útil e barata para quem lê o Resultado. Perdeu por informar menos sem ganho de rigor, já que o nível 2 não infla a métrica.
- **Contar o nível 2 como acerto parcial em Linha.** Inflaria o número de manchete e esconderia a perda do hook. Perdeu pela regra de contar com denominador explícito.
- **Excluir o nível 2 do denominador.** Faria a Linha subir quando o hook falha. Perdeu pelo mesmo motivo.

## Consequências
- Positivo: o Resultado sempre explica por que a linha falta, e há uma pista (handler) na maioria dos casos.
- Positivo: a Linha continua sem ser inflada.
- Negativo: o nível 2 sem `GrepGoFiles` lê todos os arquivos Go do SHA; o custo é aceito porque só ocorre no fallback e o resultado é memoizado por (SHA, método, rota).
- Risco: handlers registrados por mecanismos que não são uma chamada com literal de rota (reflexão, tabelas geradas) não são achados e caem em `null`.

## Como validar
- Testes unitários de `levelTwo` e de `FindRouteHandler` (achou, não achou, ambíguo, erro).
- O placar reporta a distribuição dos níveis na métrica Linha.
