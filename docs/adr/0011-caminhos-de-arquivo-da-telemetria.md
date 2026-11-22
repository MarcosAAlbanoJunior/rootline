---
id: ADR-0011
title: Converter caminhos da pilha em arquivos do repo pelo caminho relativo ao módulo, validando no SHA
status: Aceito
date: 2026-11-22
summary: Código da aplicação = módulo principal; o hook emite caminho relativo ao módulo; o Rootline acha o go.mod do módulo no SHA e valida o caminho (limpo, relativo, blob na árvore); qualquer falha ou ambiguidade cai para a localização de nível 2 com o motivo.
modules: [hook, indice, git, dominio]
---

# 0011. Converter caminhos da pilha em arquivos do repo

## Contexto

`runtime.Callers` devolve o caminho da máquina onde o binário foi compilado
(`/home/runner/work/app/internal/orders/service.go`) ou, com `-trimpath`, o caminho do módulo
mais o arquivo (`github.com/org/app/internal/orders/service.go`). Nenhum dos dois existe no repo
como está. O `go.mod` pode estar numa subpasta do repo, então "relativo ao módulo" não é
"relativo ao repo". E o caminho vem da telemetria: é entrada não confiável.

## Decisão

**Código da aplicação, na v1** = o módulo principal (`debug.ReadBuildInfo().Main.Path`).
Configuração de prefixos extras fica para depois. Se `Main.Path` estiver vazio (binário sem
informação de módulo), o hook não emite pilha e a localização cai para o nível 2.

**No hook: caminho relativo ao módulo**
- Com `-trimpath`: `frame.File` já é caminho do módulo + arquivo; basta tirar o prefixo do módulo.
- Sem `-trimpath`: o caminho do pacote sai do nome da função (`github.com/org/app/internal/orders.(*Service).List`),
  sem o prefixo do módulo dá `internal/orders`, e o arquivo vem do `frame.File`.
- O hook emite também o caminho do módulo como atributo.

**No Rootline: arquivo no repo**
- No SHA analisado, procura o `go.mod` cuja diretiva `module` bate com o caminho do módulo.
  Caminho no repo = pasta do módulo + caminho relativo ao módulo.
- Mais de um `go.mod` com o mesmo `module`: ignora `testdata/`, `vendor/` e pastas de fixtures; se
  ainda houver ambiguidade, cai para o nível 2 com o motivo.

**Validação antes de ler qualquer arquivo**
- O caminho precisa ser relativo e limpo: `path.Clean` igual a ele mesmo, sem `..`, sem começar
  com `-` ou `/`, sem byte nulo nem barra invertida.
- Precisa existir na árvore daquele SHA como **blob** (`git ls-tree`); symlink e submodule são recusados.
- Qualquer falha cai para o nível 2, com o motivo no Resultado. A análise não aborta.

## Alternativas consideradas

- **Exigir `-trimpath` no serviço analisado.** Simplifica, mas é mais uma exigência sobre o alvo e
  quebra com builds existentes.
- **Casar por sufixo de caminho contra os arquivos do repo.** Ambíguo em repos com nomes de
  arquivo repetidos.
- **Confiar no caminho e ler direto do disco.** Inseguro (travessia de caminho) e incorreto (cópia
  de trabalho em vez do SHA, contrariando o ADR-0007).

## Consequências

- O hook depende de `debug.ReadBuildInfo`, disponível em binários compilados com módulos.
- O índice consulta a árvore do SHA (`git ls-tree`) antes de ler arquivos, o que tem custo; um cache
  por SHA resolve.

## Como validar

- Testes do hook, com e sem `-trimpath`, para: closures (`func1`), métodos genéricos (`[...]` no
  nome), pacote `main` e módulo com sufixo `/v2`.
- Testes de resolução: `go.mod` na raiz e em subpasta; dois `go.mod` com o mesmo `module` (um em
  `testdata/`); ambiguidade real caindo para o nível 2.
- Testes hostis: `../../etc/passwd`, `-rf`, caminho absoluto, caminho com `:`, byte nulo, barra
  invertida, symlink e submodule. Todos caem para o nível 2 sem ler nada fora da árvore.
