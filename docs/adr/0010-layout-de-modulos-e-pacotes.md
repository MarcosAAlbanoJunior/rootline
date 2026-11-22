---
id: ADR-0010
title: Organizar o código em três módulos Go, um pacote por contexto e código em inglês
status: Aceito
date: 2026-11-22
summary: Módulos principal, hook e demo-app (validados com GOWORK=off; go.work fora do repo); domínio em internal/ só com stdlib, regras no depguard; um pacote por contexto, sem util/common/helpers; código em inglês, docs em português com glossário mapeando termos; git via os/exec com validação de SHA e "--".
modules: [todos]
---

# 0010. Organizar o código em três módulos Go, um pacote por contexto e código em inglês

## Contexto

O Rootline tem três consumidores diferentes de código: a CLI de análise, os serviços analisados
(que importam o hook de chamador) e o demo-app dos cenários. Os ADRs anteriores pedem domínio puro
e regras de dependência verificadas no CI, e o projeto é open source de portfólio.

## Decisão

**Módulos**
```
rootline/
├── go.mod                 # principal: CLI e análise
├── cmd/rootline/          # main: só monta as dependências
├── internal/
│   ├── trace/             # domínio: span, árvore, operação de entrada
│   ├── analysis/          # domínio: sintoma, candidata, evidência, resultado, score,
│   │                      #   validação de citação, pipeline e as portas que ele consome
│   ├── detector/          # domínio: detectores (começam como arquivos no mesmo pacote)
│   ├── correlation/       # domínio: toques commit ↔ candidata
│   ├── evaluation/        # pontuação, placar, relatório
│   └── adapter/
│       ├── otlpfile/      # fonte de traces OTLP JSON
│       ├── goindex/       # índice go/parser + go/ast
│       ├── gitcli/        # git via os/exec, só leitura
│       ├── ollama/        # LLM
│       ├── anthropic/     # LLM
│       └── explain/       # explain_query
├── hook/                  # módulo próprio: hook de chamador (depende só de OTel e pgx)
├── demo-app/              # módulo próprio: serviço dos cenários
└── eval/fixtures/         # script gerador dos cenários
```
- `hook/` é um módulo próprio para que um serviço que o importa não herde as dependências do
  Rootline; versionado como `hook/vX.Y.Z`.
- `demo-app/` usa `replace` para o hook local (o demo-app não é publicado).
- `go.work` fica **fora do repositório**. O CI compila e testa `hook/` e `demo-app/` com
  `GOWORK=off`, validando os módulos como um consumidor os vê.

**Regras de pacotes**
- Um pacote por contexto em `internal/`. O contexto começa como um pacote único e só vira
  subpacote quando crescer demais ou misturar responsabilidades.
- Proibidos pacotes genéricos: `util`, `common`, `helpers`.
- Os pacotes de domínio (`trace`, `analysis`, `detector`, `correlation`) importam só a stdlib e
  uns aos outros. As interfaces ficam no pacote que as consome. Adapters importam o domínio, nunca
  o contrário. O depguard do golangci-lint faz valer essas regras no CI.

**Idioma**
- Código em inglês: identificadores, pacotes, mensagens de erro e de log.
- Documentação em português: README, ADRs, glossário, specs.
- O glossário mapeia cada termo para o identificador usado no código (ex.: Candidata → `Candidate`).

**Git (`gitcli`)**
- `os/exec` chamando o `git` instalado, só leitura.
- O SHA vem da telemetria, então é entrada não confiável: é validado como hexadecimal antes de ir
  ao git, e os argumentos usam `--` para separar opções de valores, evitando argument injection.
  Coberto por teste.

## Alternativas consideradas

- **Um módulo só.** O serviço que importa o hook herdaria cliente de LLM e OTLP proto.
- **go-git.** Dependência grande para algo que o `git` instalado já faz, com comportamento
  potencialmente diferente do que o usuário vê no terminal.
- **Código em português.** Mantém um só idioma, mas reduz o público de um projeto open source.
- **Subpacotes por detector desde o início.** Estrutura antes da necessidade.

## Consequências

- Mudança no hook exige atenção ao versionamento do módulo `hook/`.
- O glossário passa a ter a linha "No código" em cada termo.
- Toda entrada vinda da telemetria (SHA, caminhos, SQL, rotas) é tratada como não confiável.

## Como validar

- depguard no CI com as regras de import do domínio.
- CI com `GOWORK=off` para `hook/` e `demo-app/`.
- Teste do gitcli: SHA não hexadecimal é rejeitado antes de chamar o git; argumento iniciado por
  `-` não vira opção.
