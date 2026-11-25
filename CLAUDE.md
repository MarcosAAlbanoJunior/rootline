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
- Lint (depguard, errcheck, govet, ineffassign, staticcheck, unused; golangci-lint v2.15.0, a mesma
  versão do CI): `golangci-lint run` na raiz e `GOWORK=off golangci-lint run` em `hook/` e `demo-app/`.
  `//nolint` só com o linter nomeado e o motivo: `//nolint:errcheck // <motivo>`.
- Referências a spec: `./scripts/check-no-spec-refs.sh` (`git grep` nos `.go` rastreados dos três
  módulos; sai com 1 se achar alguma). É o primeiro passo do gate.
- Versão do hook: `./scripts/check-hook-version.sh` (major.minor de `callerhook.Version` igual ao
  `compatibleHook`). Release do hook ou da raiz: `docs/release.md`.
- Gate (`scripts/check.sh`): referências a spec + versão do hook + build + `go test ./...` + gofmt + `go vet` + lint dos três módulos, igual ao job `check` do CI. Rode antes de declarar uma mudança pronta.
- CI (`.github/workflows/ci.yml`): `check` (o mesmo que o gate, nos três módulos), `cross` (compilação
  para linux/darwin/windows), `placar` (grava todos os cenários com Docker, em série, e roda um `score` sem
  `--update`; desvio temporário do ADR-0008, issue #5) e `e2e` (só na `main` e sob demanda). Actions fixadas por SHA;
  o Dependabot atualiza.

## Comentários e nomes de teste

Código não cita spec, slice, critério de aceite nem "Expected API". Teste tem nome de comportamento.
Comentário diz o porquê e, quando há uma decisão por trás, cita o ADR (`ADR-0014`). Doc comments de
identificadores exportados ficam: são o contrato do godoc.

## Arquitetura

Domínio (`internal/trace`, `analysis`, `detector`, `correlation`, `evaluation`, `explain`) importa só a stdlib e
os próprios pacotes de domínio (depguard garante). Adapters ficam em `internal/adapter/...`.
Sem pacotes `util`, `common` ou `helpers`.
