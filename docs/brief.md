# Rootline — brief do projeto

## Objetivo
Rootline é uma ferramenta open source de análise de causa raiz que conhece o código.
Ela cruza traces do OpenTelemetry com o código-fonte e o histórico do git para apontar
o método, a query ou o commit exato por trás de uma regressão (latência, erro, falha de build).

Exemplo de saída esperada:
"A latência de GET /orders subiu de 80ms para 1.2s. Causa provável: N+1 em
OrderRepository.findByCustomer (OrderRepository.java:42), introduzido no commit a1b2c3d.
Evidências: span X com 120 queries iguais, diff do commit, plano de execução."

É um projeto de portfólio com foco enterprise: precisa ter arquitetura limpa,
decisões documentadas (ADRs), testes sérios e números medidos.

## Escopo da v1 (só Java)
1. **App de demonstração**: um serviço Spring Boot simples (pedidos/clientes) instrumentado
   com OpenTelemetry (agente Java), com Postgres. Ele permite ligar regressões injetadas por
   feature flag: N+1, índice faltando, lock/contenção no banco, chamada lenta dentro de loop.
2. **Coleta**: receber traces OTLP (via OpenTelemetry Collector) e guardá-los para consulta.
3. **Índice de código**: parsear o repositório com tree-sitter, mapeando função → arquivo/linha
   e localizando onde as queries nascem (repositories JPA). O índice é amarrado ao SHA do git
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
- Go e Node (o design precisa permitir isso depois via OTel + tree-sitter, mas sem implementar agora)
- UI elaborada (CLI + saída em Markdown/JSON bastam)
- Ações automáticas de correção, abertura de PR ou qualquer escrita em sistemas externos
- Multi-tenant, autenticação, deploy em nuvem

## Stack
- Java 25 (LTS), Spring Boot 4.x (confirmar a versão estável mais recente), Maven
- Spring AI com provedor plugável (Ollama local por padrão; Anthropic/Bedrock opcionais)
- OpenTelemetry (agente Java + Collector), Postgres, Testcontainers
- tree-sitter (via binding Java) para o índice de código
- docker compose para subir tudo localmente com um comando

## Princípios
- O contexto mandado ao LLM é mínimo e filtrado: só o span suspeito, as linhas relevantes
  e o diff. Nunca o trace inteiro ou o repositório inteiro.
- Toda conclusão do agente precisa citar a evidência. Sem evidência, a resposta é "inconclusivo".
- Ferramentas do agente são somente leitura.
- O que dá para resolver de forma determinística (achar span lento, contar queries repetidas,
  mapear linha → função) é feito em código, não pelo LLM. O LLM entra para correlacionar e explicar.
- Feedback loops rápidos: build, testes e checagem de arquitetura (ArchUnit) rodando a cada mudança.

## Estrutura sugerida (multi-módulo Maven)
- rootline-core: modelos, correlação trace ↔ código ↔ commit
- rootline-index: indexação com tree-sitter
- rootline-agent: agente e ferramentas
- rootline-cli: ponto de entrada
- demo-app: serviço com regressões injetáveis
- eval: cenários e relatório de acerto
- docs/adr: decisões de arquitetura

## Primeiro passo
Antes de escrever código, proponha a arquitetura da v1 e a estrutura de módulos,
liste as decisões em aberto (ex.: onde armazenar traces, qual binding de tree-sitter)
e escreva os primeiros ADRs. Só depois comece pelo demo-app com a regressão de N+1.
