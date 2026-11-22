---
id: RES-2026-11-22-instrumentacao-go
title: Estado da instrumentação OTel em Go, do tree-sitter em Go e da versão estável do Go
status: Concluída
summary: Go estável é 1.27.4; nenhum wrapper de banco anexa o chamador (otelsql tem hook por span, otelpgx não); OBI é estável mas não dá localização no código; os bindings de tree-sitter em Go dependem de cgo.
---

# Estado da instrumentação OTel em Go, do tree-sitter em Go e da versão estável do Go

## Resposta curta

- A versão estável atual do Go é a **1.27.4**.
- Nenhum wrapper de banco anexa o chamador da query. O `XSAM/otelsql` tem um hook por span
  (`AttributesGetter`) onde dá para fazer isso; o `exaring/otelpgx` não tem, e precisaria de um
  tracer embrulhando o dele.
- O OBI (eBPF) é estável e rastreia queries de `database/sql` e pgx sem mudar código, mas não
  produz atributos de localização no código.
- Os bindings de tree-sitter em Go dependem de cgo, o que complica binário estático e cross-compilação.

## Evidências

### Versão do Go
- `https://go.dev/dl/?mode=json`: a primeira versão marcada `"stable": true` é `go1.27.4`; a anterior é `go1.26.11`. O JSON não traz data.
- Notas da 1.27: https://go.dev/doc/go1.27

### Atributos `code.*` (semconv)
- `code.function.name`, `code.file.path` e `code.line.number` são estáveis desde a semconv 1.33.0;
  substituem `code.filepath` e `code.lineno`.
  https://opentelemetry.io/docs/specs/semconv/registry/attributes/code/ ·
  https://opentelemetry.io/docs/specs/semconv/non-normative/code-attrs-migration/
- Não confirmado: o status atual de `code.stacktrace`.

### exaring/otelpgx (pgx nativo)
- v0.13.0 (08/10/2026) é a mais recente visível no pkg.go.dev, que avisa existir uma mais nova. Requer Go 1.25+ e pgx v5+.
- Opções: `WithTracerProvider`, `WithTracerAttributes` (só estáticos), `WithSpanNameFunc`,
  `WithSpanNameCtxFunc` (recebe ctx, mas devolve só o nome), `WithIncludeQueryParameters`,
  `WithDisableSQLStatementInAttributes`, entre outras.
- **Nenhuma opção adiciona localização no código nem atributos dinâmicos por span.** Para isso
  é preciso um `pgx.QueryTracer` próprio que embrulhe o do otelpgx.
  https://github.com/exaring/otelpgx · https://github.com/exaring/otelpgx/blob/main/options.go

### XSAM/otelsql (database/sql)
- v0.44.0 (06/09/2026) é a mais recente visível no pkg.go.dev, que avisa existir uma mais nova. Testada com Go 1.25 a 1.27.
- `WithAttributesGetter(func(ctx, method, query, args) []attribute.KeyValue)` é "called on every
  span creations": dá para chamar `runtime.Callers` ali (inferência nossa, não documentada).
- Nenhuma localização no código pronta. https://github.com/XSAM/otelsql

### Auto-instrumentação por eBPF
- OBI v1.0.0 é "the first stable release". Rastreia `database/sql` (Go >= 1.17) e pgx v5
  (testado até v5.8.0), inclusive via `pgx/v5/stdlib`.
- **Não há atributos de localização no código.** A seção de limitações diz que atributos
  específicos da aplicação, que o eBPF não deriva, ficam fora do escopo e recomenda os SDKs.
- Requisitos: Linux amd64/arm64, kernel 5.8+ (ou RHEL 4.18+ com backports), BTF, root ou capabilities.
  https://opentelemetry.io/docs/zero-code/obi/
- O projeto anterior (`opentelemetry-go-instrumentation`) foi arquivado em 20/11/2026 e incorporado ao OBI.
  https://github.com/open-telemetry/opentelemetry-go-instrumentation

### tree-sitter em Go
- Binding oficial `github.com/tree-sitter/go-tree-sitter`: v0.25.0 (02/02/2025) visível; o repo
  contém fontes C, e o README exige `Close()` "due to bugs with runtime.SetFinalizer and CGO".
  As gramáticas são módulos separados. https://github.com/tree-sitter/go-tree-sitter
- O runtime em Go puro de terceiros `odvcencio/gotreesitter` afirma que "every Go tree-sitter
  binding in the ecosystem depends on CGo". Cita como custos: toolchain C por alvo na
  cross-compilação, imagens de CI com gcc e race detector/coverage/fuzzer cegos para o lado C.
  https://pkg.go.dev/github.com/odvcencio/gotreesitter

## Incertezas

- Versões mais novas de otelpgx e otelsql existem, mas não foram identificadas; confirmar no `go get`.
- Status de `code.stacktrace` na semconv: confirmar antes de usar o nome.
- Custo de `runtime.Callers` por query: medir no spike do ADR-0002.
