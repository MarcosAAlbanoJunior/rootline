---
id: ADR-0015
title: Ler a árvore de cada SHA uma vez e validar caminhos contra ela em memória
status: Aceito
date: 2026-11-22
summary: O adapter gitcli roda um `git ls-tree -r` por SHA, guarda a árvore em memória e valida existência e tipo (arquivo regular) de cada caminho nela, em vez de um processo git por caminho. Memoiza o módulo por (SHA, módulo). Erros viram warnings.
modules: [gitcli, goindex, analysis]
---

# 0015. Ler a árvore de cada SHA uma vez e validar caminhos contra ela em memória

## Contexto
O ADR-0011 exige validar cada caminho vindo da telemetria (limpo, relativo, blob regular) e achar o `go.mod` no SHA. O ADR-0007 manda o índice ler cada SHA direto do git. Uma análise resolve muitos caminhos do mesmo SHA; um processo git por validação seria lento e, como um SHA não muda, repetiria trabalho idêntico.

## Decisão
O `gitcli` roda um único `git ls-tree -r -z <sha>` por SHA e guarda o resultado em memória (caminho → modo, tipo, hash). Existência e tipo são checados nesse mapa; symlinks, submódulos e diretórios são recusados. O diretório do módulo é memoizado por (SHA, caminho do módulo), não por (SHA, arquivo).
- Falha ao rodar o git não é guardada em cache.
- Erros de `ResolvePath` e `InLoop` viram `warnings` no Resultado, nunca são engolidos.
- O acesso ao mapa é protegido por mutex.

## Alternativas consideradas
- **Um `git cat-file -e` por caminho.** Simples, mas um processo por validação; perdeu por custo.
- **`git cat-file --batch-check` persistente.** Evita o processo repetido, mas exige gerir um subprocesso com estado e protocolo. Perdeu por complexidade sem ganho claro nesta escala.
- **Cache por (SHA, arquivo) do módulo.** Perdeu porque muitos arquivos compartilham o mesmo módulo; (SHA, módulo) tem menos entradas e acerta mais.

## Consequências
- Positivo: um processo git por SHA; validação em microssegundos e testável sem git real (via `Runner`).
- Negativo: a árvore inteira do SHA fica em memória; em monorepos muito grandes isso pode pesar. Não há medição com repositórios grandes.

## Como validar
- Testes do `gitcli` com `Runner` falso contam as chamadas ao git (uma por SHA).
- Testes de recusa de symlink, submódulo, diretório e caminho fora da árvore.
- Medir memória e tempo com um repositório grande antes da v1 (dado que falta; a skill `research` pode ajudar a escolher o repositório).
