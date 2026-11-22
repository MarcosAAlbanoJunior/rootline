# Rootline

Ferramenta OSS de análise de causa raiz (Go 1.27.4). Antes de mudar algo, leia `docs/brief.md` e
`GLOSSARY.md`. Decisões em `docs/adr/`. Código em inglês, docs em português.

## Módulos

Raiz (`github.com/MarcosAAlbanoJunior/rootline`), `hook/` e `demo-app/` são módulos separados.
Para trabalhar com os três juntos, crie localmente um `go.work` (fica fora do git):

```
go work init . ./hook ./demo-app
```

## Comandos

- Build: `CGO_ENABLED=0 go build ./...`
- Testes (TIA): `go test ./...` sem `-count=1`. O cache do `go test` reexecuta só os pacotes afetados.
- `hook/` e `demo-app/`, por módulo: `cd hook && GOWORK=off CGO_ENABLED=0 go build ./... && GOWORK=off go test ./...` (idem em `demo-app/`).
- e2e (precisa de Docker): `go test -tags e2e ./internal/recording/`
- Formatação (etapa do gate): `test -z "$(gofmt -l .)"`; corrija com `gofmt -w .`
- Depguard (direção das dependências do domínio): `golangci-lint run`
- Gate do specgate (`specgate.config`): build + `go test ./...` + gofmt + `go vet` + depguard

## Arquitetura

Domínio (`internal/trace`, `analysis`, `detector`, `correlation`, `evaluation`) importa só a stdlib e
os próprios pacotes de domínio (depguard garante). Adapters ficam em `internal/adapter/...`.
Sem pacotes `util`, `common` ou `helpers`.
