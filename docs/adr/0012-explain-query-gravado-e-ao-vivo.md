---
id: ADR-0012
title: Obter planos de execução por uma porta com adapter gravado e adapter ao vivo protegido por marcador no banco
status: Aceito
date: 2026-11-22
summary: Explainer com dois adapters; o gravado salva na gravação os planos das queries distintas (após ANALYZE, nas duas janelas no cenário sem commit); o ao vivo é desligado por padrão, exige DSN explícito e marcador no banco, só aceita SELECT único e roda EXPLAIN sem ANALYZE, read-only, com timeouts.
modules: [explain, eval, dominio]
---

# 0012. Obter planos de execução: adapter gravado e adapter ao vivo protegido

## Contexto

O brief promete `explain_query` "nunca contra produção", mas o Rootline não tem como saber se um
banco é de produção, e o SQL vem da telemetria, ou seja, é entrada não confiável. Além disso, a
análise roda sobre gravações, sem Docker (ADR-0008), e o cenário de índice faltando depende do
plano de execução.

## Decisão

**Porta `Explainer`, com dois adapters.**

**Gravado (avaliação)**
- Durante a gravação, o script roda `ANALYZE` nas tabelas (estatísticas determinísticas) e depois o
  EXPLAIN de cada query normalizada distinta que aparece nos traces. Os planos ficam na gravação.
- No cenário sem commit, os planos são gravados nas duas janelas (referência e ruim): a mudança de
  plano é a evidência.
- Na análise, o adapter só lê os planos gravados.

**Ao vivo (uso real), com salvaguardas verificáveis**
- Desligado por padrão. Liga só com conexão explícita (`--explain-dsn` ou variável de ambiente);
  nunca usa conexão vinda dos atributos da telemetria.
- Recusa rodar se o banco não tiver o marcador `rootline:explain-allowed` no `COMMENT ON DATABASE`.
- Só aceita um `SELECT` único; comandos múltiplos são recusados (validação pelo extrator de SQL do ADR-0006).
- `EXPLAIN` sem `ANALYZE` (`ANALYZE` executaria a query). Queries com parâmetros sem valor usam
  `GENERIC_PLAN`, disponível desde o PostgreSQL 16.
- Tudo dentro de `BEGIN READ ONLY`, com `SET LOCAL statement_timeout` e `SET LOCAL lock_timeout`
  curtos (para não travar atrás de um DDL), fechado com `ROLLBACK`. A documentação recomenda um
  usuário do banco só com leitura.

**Nos dois adapters**
- Evidência vinda de `GENERIC_PLAN` é marcada como "plano genérico" (ignora os valores reais).
- SQL que não dá para explicar (ex.: literais sanitizados como `?`) é pulado, com o motivo registrado no Resultado.

**Postgres do demo-app:** a versão estável mais recente no momento de fixar a imagem. Em
22/11/2026, a imagem oficial `postgres:latest` é a 19.0. Qualquer versão a partir da 16 suporta
`GENERIC_PLAN`.

## Alternativas consideradas

- **Só adapter ao vivo.** A avaliação passaria a depender de Docker na análise, contrariando o ADR-0008.
- **Confiar em uma flag "--not-production".** Uma promessa do usuário, não uma checagem.
- **`EXPLAIN ANALYZE`.** Dá tempos reais, mas executa a query: inaceitável contra um banco que não controlamos.

## Consequências

- A gravação fica um pouco mais lenta (ANALYZE + EXPLAIN por query distinta) e maior.
- Usar o modo ao vivo exige que o usuário marque o banco explicitamente; isso vai para o README.
- Planos genéricos podem divergir do plano real com valores específicos; a marcação deixa isso visível.

## Como validar

- Testes de integração (testcontainers-go) do adapter ao vivo: recusa sem marcador; recusa
  múltiplos comandos e não-SELECT; roda com marcador; respeita read-only e os dois timeouts.
- Teste do adapter gravado: o cenário de índice faltando mostra a mudança de plano entre as gravações.
- Teste: SQL com `?` gera o motivo "não explicável" no Resultado, sem erro.
