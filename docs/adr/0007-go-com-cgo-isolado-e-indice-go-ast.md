---
id: ADR-0007
title: Escrever o Rootline em Go sem cgo no build padrão, com índice de código em go/ast
status: Aceito
date: 2026-11-22
summary: Substitui o ADR-0003. Ferramenta e alvo da v1 em Go; cgo só em adapters opcionais atrás de porta e build tag; índice da v1 em go/parser + go/ast, lendo o código no SHA de cada versão; CI garante CGO_ENABLED=0 e compilação cruzada.
modules: [todos]
---

# 0007. Escrever o Rootline em Go sem cgo no build padrão, com índice de código em go/ast

> Substitui o [ADR-0003](0003-go-como-linguagem-da-ferramenta-e-do-alvo.md). A decisão por Go se
> mantém; muda o índice de código (tree-sitter → `go/ast`), que contrariava o binário estático, e
> entra uma política de cgo. Este ADR reúne a decisão completa para que o ADR-0003 possa ser
> inteiramente substituído.

## Contexto

O ADR-0003 escolheu Go por footprint, distribuição em binário único e alcance no ecossistema
cloud native, e previa tree-sitter para o índice. A pesquisa
`docs/research/2026-11-22-instrumentacao-go.md` mostrou que todos os bindings de tree-sitter em Go
dependem de cgo, e o parser de SQL natural (`pg_query_go`, ADR-0006) também. Decidir cgo caso a
caso deixaria o binário estático refém da última dependência escolhida.

## Decisão

**Linguagem e alvo (mantido do ADR-0003)**
- Rootline em Go, na versão estável mais recente (1.27.4 em 22/11/2026); o serviço analisado na v1 também é Go.
- Biblioteca padrão primeiro; dependência externa só com justificativa em ADR ou PR.
- Stack: Postgres com pgx (sqlc no demo-app, a avaliar); OpenTelemetry Go SDK + Collector; LLM
  atrás de uma porta (Ollama por padrão, Anthropic opcional); `testing` com table-driven tests e
  testcontainers-go; go vet, staticcheck, golangci-lint; docker compose.
- Hexagonal idiomática: domínio puro em `internal/`, interfaces no pacote consumidor, adapters
  separados, CLI em `cmd/rootline`, demo-app como serviço separado no repo.

**Política de cgo**
- O build padrão, o que é distribuído, é `CGO_ENABLED=0`.
- Um adapter com cgo só existe atrás de uma porta e de uma build tag, com uma alternativa em Go
  puro como padrão. O domínio nunca depende de cgo.

**Índice de código da v1**
- `go/parser` + `go/ast` da stdlib, atrás de uma porta. Fornece o intervalo de linhas por função,
  se uma linha está dentro de `for`/`range` (inclusive corpo de range-over-func), registro de
  rota → handler e strings SQL.
- **O código é lido no SHA de cada versão analisada** (ruim e referência), via
  `git show <sha>:<arquivo>` ou worktree temporário, nunca da cópia de trabalho. Senão as linhas
  da pilha não batem com o código indexado.
- `go/types` e `go/packages` ficam fora da v1: a pilha já traz arquivo e linha, e carregar tipos
  exige o módulo compilável no SHA. Entram só se algum caso exigir.
- tree-sitter vira um adapter futuro, com build tag, quando entrar uma segunda linguagem.

**CI**
- Um job compila com `CGO_ENABLED=0` e faz compilação cruzada para linux/amd64, linux/arm64,
  darwin/arm64 e windows/amd64. Falha se algo exigir cgo.

## Motivação

- **Desempenho e footprint:** binário único, startup rápido, pouca memória para CI, sidecar e CLI.
- **Distribuição:** um executável estático, sem runtime e sem toolchain C.
- **Alcance:** o ecossistema de observabilidade e cloud native é majoritariamente Go.
- **Índice:** para um alvo Go, a stdlib resolve o índice sem dependência nova.

## Alternativas consideradas

- **tree-sitter já na v1 (ADR-0003).** Generaliza para outras linguagens, mas exige cgo, toolchain
  C no CI, linkagem estática e compilação cruzada por alvo, e deixa race detector, coverage e
  fuzzer cegos para o lado C. Fica como adapter futuro.
- **cgo proibido sem exceção.** Fecharia a porta para bibliotecas superiores que só existem com C,
  mesmo como opção.
- **cgo livre.** Usa as melhores bibliotecas, mas abandona a principal motivação de distribuição.
- **Java 25 + Spring Boot (plano original).** Ver ADR-0003.

## Consequências

- O parser de SQL (questão em aberto do ADR-0006) deve ter um padrão em Go puro; `pg_query_go`,
  se entrar, só como adapter opcional com build tag.
- O índice precisa acessar o git do serviço para ler cada SHA; isso usa o mesmo `--repo` do ADR-0005.
- Uma segunda linguagem exige um novo adapter de índice, sem mudar o domínio.

## Como validar

- Job de CI com `CGO_ENABLED=0` e a matriz de compilação cruzada.
- Teste de integração: o índice lido de um SHA antigo localiza as linhas corretas mesmo com a
  cópia de trabalho alterada.
- Tamanho do binário, startup e memória da CLI medidos e publicados no README.
