---
id: ADR-0008
title: Ler traces de arquivos OTLP JSON e separar a gravação dos cenários da análise
status: Aceito
date: 2026-11-22
summary: Fonte de traces atrás de uma porta, com adapter de arquivo OTLP JSON (file exporter do Collector) na v1; gravação lenta e ocasional, análise rápida sem Docker; gravações com manifesto, em cache com chave de tudo que altera os traces, publicadas como artefato de release; só-detectores em todo PR, modos com LLM sob demanda.
modules: [traces, eval, ci]
---

# 0008. Ler traces de arquivos OTLP JSON e separar a gravação da análise

## Contexto

Rodar um cenário (ADR-0004) exige gerar o repo de fixture, subir Postgres e o demo-app e aplicar
carga, o que leva minutos. A análise leva segundos. Se a avaliação fizer as duas coisas toda vez,
ela não cabe no ciclo de desenvolvimento nem no CI de cada PR.

## Decisão

**Fonte de traces**
- É uma porta. O adapter da v1 lê **arquivos OTLP JSON** do file exporter do OpenTelemetry
  Collector. A v1 não tem banco de traces, nem Jaeger ou Tempo no compose.
- Jaeger e Tempo são adapters futuros, pela API de consulta deles.
- Dependência: os tipos do protocolo OTLP (`go.opentelemetry.io/proto/otlp`), Go puro, do próprio
  projeto OpenTelemetry.

**Gravação (lenta, ocasional)**
- Sobe o cenário, aplica a carga e salva os traces.
- Cada gravação tem um **manifesto**: cenário, rótulos e SHAs dos commits, seed, versão do hook,
  configuração da carga e overhead medido.

**Análise (rápida, a cada mudança)**
- Roda sobre as gravações, sem Docker, em segundos.

**Cache**
- As gravações ficam fora do git, num cache local e no cache do CI.
- A **chave do cache inclui tudo o que altera os traces:** hash do script gerador, do código do
  hook e do demo-app (inclusive `go.sum`), a seed, a versão do Go, a versão do Collector e a imagem
  do Postgres (a versão do Postgres muda planos de execução).

**Publicação**
- As gravações que geram os números publicados vão como **artefato de release** no GitHub,
  comprimidas, para qualquer pessoa reproduzir a avaliação só com a fase de análise, sem Docker.

**CI**
- O modo só-detectores roda em todo PR (é determinístico).
- Os modos com LLM rodam sob demanda ou agendados, com temperatura baixa e N repetições por
  cenário, reportando média e variância. Os detalhes ficam no ADR da avaliação.

## Alternativas consideradas

- **Avaliação ao vivo a cada execução.** Fiel, mas lenta e dependente de Docker; não cabe em todo PR.
- **Banco de traces (Jaeger/Tempo) já na v1.** Mais realista para produção, mas acrescenta serviço,
  API e estado sem ajudar a avaliação.
- **Gravações no git.** Grandes e regeneráveis; incham o repo.

## Consequências

- Mudança no hook, no demo-app, no script gerador, nas versões do Go ou do Collector ou na imagem do
  Postgres invalida o cache e exige gravar de novo. Mudança em detectores, correlação ou LLM não exige.
- O overhead do hook (ADR-0002) é medido na gravação e registrado no manifesto.
- O release do Rootline passa a carregar os artefatos de gravação usados nos números.

## Como validar

- Teste: alterar um arquivo do hook muda a chave do cache.
- A avaliação só-detectores roda no CI de PR sem Docker, a partir do cache.
- Reprodução a partir do artefato de release gera os mesmos números do modo só-detectores.

## Desvio temporário

Até a slice do cache de gravações entrar, o portão de placar do PR grava o cenário com Docker no
próprio runner (`rootline-eval record`) e depois pontua só com detectores, em vez de usar o cache.
A decisão acima não muda. O desvio acaba com a remoção acompanhada na
issue #5 (https://github.com/MarcosAAlbanoJunior/rootline/issues/5).
