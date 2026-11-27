# Rootline — brief do projeto

## Objetivo
Rootline é uma ferramenta open source de análise de causa raiz que conhece o código.
Ela cruza traces do OpenTelemetry com o código-fonte e o histórico do git para apontar
o método, a query ou o commit exato por trás de uma regressão (latência, erro, falha de build).

Exemplo de saída esperada:
"A latência de GET /orders subiu de 80ms para 1.2s. Causa provável: N+1 em
orders.(*Service).ListByCustomer (internal/orders/service.go:42), introduzido no commit a1b2c3d.
Evidências: span X com 120 queries iguais, diff do commit, plano de execução."

É um projeto de portfólio com foco enterprise: precisa ter arquitetura limpa,
decisões documentadas (ADRs), testes sérios e números medidos.

## Escopo da v1 (só Go)
1. **App de demonstração**: um serviço Go simples (pedidos/clientes) com net/http e pgx,
   instrumentado com o OpenTelemetry Go SDK (otelhttp + wrapper de banco), com Postgres. As regressões
   (N+1, índice faltando, lock/contenção no banco, chamada lenta dentro de loop) nascem de commits
   reais num repo de fixture gerado por script, sem feature flags ([ADR-0004](adr/0004-cenarios-de-avaliacao-como-commits-reais.md)).
2. **Coleta**: receber traces OTLP (via OpenTelemetry Collector) e guardá-los para consulta.
3. **Índice de código**: parsear o código no SHA de cada versão analisada (lido do git, nunca da
   cópia de trabalho), mapeando função → arquivo/linha e localizando onde as queries nascem. O índice é amarrado ao SHA do git
   (o serviço publica service.version = SHA do commit).
4. **Agente de análise**: recebe um sintoma (endpoint + janela de tempo) e usa ferramentas
   somente leitura:
   - get_trace / find_slow_spans
   - get_code(file, startLine, endLine)
   - diff_between(shaA, shaB)
   - explain_query(sql), executado só contra a base de demonstração/teste, nunca produção
   Ele devolve: causa provável, evidências (span, linha, query, commit) e uma correção sugerida.
5. **Bateria de avaliação**: cenários com regressão injetada e resposta esperada (método +
   causa). Medir quantos o agente acerta. Isso é parte central do projeto, não um extra.

## Fora do escopo da v1
- Java e Node (o design precisa permitir isso depois via OTel + índice de código por linguagem, mas sem implementar agora)
- UI elaborada (CLI + saída em Markdown/JSON bastam)
- Ações automáticas de correção, abertura de PR ou qualquer escrita em sistemas externos, exceto a
  publicação do Resultado ([ADR-0039](adr/0039-publicar-o-resultado-em-destino-externo.md))
- Multi-tenant, autenticação, deploy em nuvem

## Stack
Decisão e motivação: [ADR-0007](adr/0007-go-com-cgo-isolado-e-indice-go-ast.md) (substitui o ADR-0003).

- Go, versão estável mais recente (1.27.4 em 22/11/2026; Go não tem LTS, confirmar em go.dev antes de fixar no go.mod)
- Biblioteca padrão primeiro (net/http, log/slog, context, errors); dependência externa só com justificativa em ADR ou PR
- Postgres com pgx; sqlc para as queries do demo-app (a avaliar)
- OpenTelemetry Go SDK + Collector
- Build padrão sem cgo (`CGO_ENABLED=0`); cgo só em adapters opcionais, atrás de porta e build tag
- Índice de código atrás de uma porta: `go/parser` + `go/ast` na v1; tree-sitter como adapter futuro para outras linguagens
- LLM atrás de uma porta (interface), com adaptador Ollama local por padrão e Anthropic opcional
- Testes: testing da stdlib, table-driven tests, testcontainers-go para integração; TDD
- Lint: go vet, staticcheck, golangci-lint
- docker compose para subir tudo localmente com um comando

## Princípios
- O contexto mandado ao LLM é mínimo e filtrado: só o span suspeito, as linhas relevantes
  e o diff. Nunca o trace inteiro ou o repositório inteiro.
- Toda conclusão do agente precisa citar a evidência. Sem evidência, a resposta é "inconclusivo".
- Ferramentas do agente são somente leitura.
- O que dá para resolver de forma determinística (achar span lento, contar queries repetidas,
  mapear linha → função) é feito em código, não pelo LLM. O LLM entra para correlacionar e explicar.
- Feedback loops rápidos: build, testes e checagem de arquitetura (regras de import entre pacotes) rodando a cada mudança.

## Estrutura (hexagonal, idiomática em Go)
O layout exato de pacotes é decidido na pergunta 8 da entrevista de arquitetura.
- `internal/`: domínio puro (detectores, candidatas, evidências, validação de citação), sem OTel, banco ou LLM
- adapters separados: fonte de traces, índice de código, git, LLM
- interfaces declaradas no pacote que as consome
- `cmd/rootline`: CLI
- demo-app: serviço Go separado no repo, com regressões injetáveis
- eval: cenários e relatório de acerto
- docs/adr: decisões de arquitetura

## Primeiro passo
Antes de escrever código, proponha a arquitetura da v1 e a estrutura de módulos,
liste as decisões em aberto (ex.: onde armazenar traces, qual binding de tree-sitter)
e escreva os primeiros ADRs. Só depois comece pelo demo-app com a regressão de N+1.
