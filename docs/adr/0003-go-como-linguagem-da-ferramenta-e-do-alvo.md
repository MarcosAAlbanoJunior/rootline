---
id: ADR-0003
title: Escrever o Rootline em Go e analisar serviços Go na v1
status: Substituído por ADR-0007
date: 2026-11-22
summary: Ferramenta e alvo da v1 passam de Java para Go, por footprint, distribuição em binário único e alcance no ecossistema cloud native; stdlib primeiro, arquitetura hexagonal idiomática.
modules: [todos]
---

# 0003. Escrever o Rootline em Go e analisar serviços Go na v1

> **Substituído pelo [ADR-0007](0007-go-com-cgo-isolado-e-indice-go-ast.md)**, que mantém Go e troca o
> índice tree-sitter por `go/ast` com uma política de cgo.

## Contexto

O brief original definia Java 25 + Spring Boot tanto para a ferramenta quanto para o serviço
analisado na v1. O Rootline vai rodar em CI, como sidecar ou como CLI. Nesses lugares,
startup, memória e facilidade de instalação pesam. O ecossistema de observabilidade com que ele
se integra (OpenTelemetry Collector, Prometheus, Jaeger, Kubernetes) é majoritariamente Go.

## Decisão

- O Rootline é escrito em Go, na versão estável mais recente (1.27.4 em 22/11/2026, conforme
  `go.dev/dl`). Go não tem LTS; a versão do `go.mod` acompanha as estáveis.
- O serviço analisado na v1 (demo-app e cenários da avaliação) também é Go.
- **Biblioteca padrão primeiro** (`net/http`, `log/slog`, `context`, `errors`). Dependência
  externa só com justificativa em ADR ou PR.
- **Stack:** Postgres com pgx (sqlc no demo-app, a avaliar); OpenTelemetry Go SDK + Collector;
  índice de código com tree-sitter via binding Go (ver o risco abaixo); LLM atrás de uma porta,
  com adaptador Ollama por padrão e Anthropic opcional; `testing` da stdlib com table-driven
  tests e testcontainers-go para integração; `go vet`, staticcheck e golangci-lint; docker
  compose para subir tudo com um comando.
- **Arquitetura hexagonal idiomática:** domínio puro em `internal/` (detectores, candidatas,
  evidências, validação de citação) sem dependência de OTel, banco ou LLM. As interfaces são
  declaradas no pacote que as consome. Os adapters ficam separados (fonte de traces, índice de
  código, git, LLM). A CLI fica em `cmd/rootline` e o demo-app é um serviço Go separado no
  repo. O layout de pacotes é decidido à parte.

## Motivação

- **Desempenho e footprint:** binário único, startup rápido, pouca memória. Importa para CI, sidecar e CLI.
- **Distribuição:** um executável, sem runtime para instalar.
- **Alcance:** integração e adoção mais fáceis no ecossistema cloud native, que é majoritariamente Go.
- **Índice de código:** bindings de tree-sitter disponíveis em Go.

## Alternativas consideradas

- **Java 25 + Spring Boot (plano original).** Ecossistema maduro (agente OTel automático,
  Spring AI), mas exige JVM para rodar a ferramenta, tem startup e memória piores para CI/CLI e
  fica mais longe do ecossistema cloud native.
- **Ferramenta em Go analisando Java.** Mantém o agente automático do Java no alvo, mas obriga a
  manter dois ecossistemas já na v1. Fica possível depois, já que o design prevê outras linguagens.

## Consequências

- Não há agente automático equivalente ao do Java: a instrumentação do alvo é por biblioteca. O
  ADR-0002 foi reaberto para Go.
- Sem lazy loading de ORM: as variações de N+1 da avaliação mudam (ADR-0002).
- Validação de arquitetura passa de ArchUnit para regras de import entre pacotes (golangci-lint
  com depguard ou um teste sobre `go list`), a definir junto com o layout de pacotes.
- **Risco (a decidir no índice de código):** os bindings de tree-sitter em Go dependem de cgo,
  conforme a pesquisa `docs/research/2026-11-22-instrumentacao-go.md`. Isso exige toolchain C
  no build e no CI, dificulta binário estático e cross-compilação e deixa race detector,
  coverage e fuzzer cegos para o lado C. Isso enfraquece as motivações de footprint e
  distribuição. Para um alvo Go, `go/parser` + `go/ast` da stdlib resolvem o índice sem cgo.
  A decisão fica para a pergunta 7 da entrevista de arquitetura.

## Como validar

- `CGO_ENABLED=0 go build ./cmd/rootline` gera um binário que roda sem dependências (vale
  enquanto o índice não usar cgo; se usar, o ADR do índice documenta como o binário é distribuído).
- Tamanho do binário, tempo de startup e memória da CLI medidos e publicados no README.
