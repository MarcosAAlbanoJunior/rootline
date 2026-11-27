---
id: ADR-0033
title: Ler traces do Tempo por uma consulta, com a latência pela busca e os detectores numa amostra de traces completos
status: Aceito
date: 2026-11-26
summary: Complementa o ADR-0008. A TraceSource ganha uma consulta (serviço, versão, operação, janela, limite) sem nada de TraceQL; o primeiro adapter de backend é o Tempo. A Regressão sai das durações da busca e os detectores rodam em até 50 traces completos por versão, escolhidos perto do p50 e na cauda. Validado no spike de 2026-11-26 com o Tempo 3.0.0.
modules: [analysis, adapter, cmd/rootline]
---

# 0033. Ler traces do Tempo por uma consulta, em duas camadas

## Contexto

O ADR-0008 pôs a fonte de traces atrás de uma porta e deixou o Tempo e o Jaeger como adapters
futuros. A porta de hoje (`TraceSource` em `internal/analysis/ports.go`) tem um método só,
`Spans()`, que devolve todos os spans do arquivo. O adapter de arquivo filtra depois, em memória.

O modo watch (ADR-0035) lê os traces do backend que o time já usa, e não de um arquivo. Num backend,
"devolver tudo" não existe: a busca é por filtro e limite, e baixar um trace completo é uma chamada
por trace. Os dois usos dos traces pedem quantidades diferentes:

- **A Regressão** (ADR-0032) só precisa, por trace, da duração da operação de entrada e de saber se
  ela falhou. Com amostra mínima de 30 por versão, ela fica mais estável com centenas.
- **Os detectores** (N+1, forma do trace, localização pelo hook) precisam dos spans filhos com os
  atributos do hook. Isso só vem no trace completo.

O spike de 2026-11-26 (`docs/spikes/2026-11-26-tempo-como-fonte.md`), com o Tempo 3.0.0 e duas
gravações do placar, mediu a API:
- a busca TraceQL devolve, por trace, a duração exata do span de entrada (`durationNanos`) e o
  status dele (`select(status)`), também quando ele não é a raiz; n, p50 e erros batem com o arquivo;
- 200 traces por busca em ~25 ms (~1,1 KB por trace), tanto no live store quanto nos blocos do
  backend; o trace completo custa ~5,5 ms (p50) e ~27 KB;
- listar os valores de `service.version` de um serviço custa ~5 ms.

## Decisão

**A porta ganha uma consulta.** A `TraceSource` passa a receber serviço, versão, operação de
entrada, janela de tempo e limite. A consulta é do domínio e não tem nada de TraceQL nem da API do
Jaeger. O adapter de arquivo implementa a mesma consulta filtrando em memória, e o placar não muda.

**O primeiro adapter de backend é o Tempo.** O Jaeger entra depois, como segundo adapter da mesma
porta. A demo do watch roda com o Tempo num cluster local.

**Duas camadas:**
- **Latência pela busca.** A Regressão usa a duração e o status do span de entrada que a busca
  devolve (`durationNanos` do span casado, com um span por trace e `select(status)`), sem baixar o
  trace. Nunca usa o `durationMs` do trace, que é o da raiz, em milissegundos inteiros, e some quando
  vale 0. A busca pode trazer centenas de traces por versão.
- **Detectores numa amostra de traces completos.** Até 50 traces completos por versão e operação,
  com o teto configurável. A amostra é escolhida pelos percentis da busca: metade perto do p50 e
  metade na cauda (acima do p90). Pegar só os lentos enviesaria a divisão do tempo (ADR-0031).
- As Verificações dizem quantos traces vieram da busca e quantos completos entraram nos detectores.

## Alternativas consideradas

- **Tempo e Jaeger no primeiro corte.** Dobra o spike, os testes de integração e a demo sem mudar o
  que a demo mostra. Um adapter basta para provar a porta. Perdeu por enquanto.
- **Só o Jaeger.** Também serve, mas o Tempo é o backend de traces do stack Grafana, e o filtro que
  o watch precisa (serviço, `service.version`, `http.route`, janela) é o que o TraceQL faz. O spike
  confirma. Perdeu por enquanto.
- **Baixar N traces completos e fazer tudo sobre eles.** É o mais simples, mas o custo cresce com o
  N, e a Regressão ficaria limitada ao mesmo N dos detectores. Perdeu.
- **Manter `Spans()` e baixar tudo da janela.** Não escala e não existe na API de um backend. Perdeu.

## Consequências

- O domínio deixa de assumir que tem todos os spans. Os detectores recebem só a amostra completa,
  e a Regressão recebe as durações da busca.
- O `analyze` e o `symptoms` podem ler do Tempo com uma flag de fonte, antes do watch existir.
- A divisão do tempo passa a ser uma média sobre a amostra, não sobre todos os traces. O teto de 50
  pode deixar a média mais ruidosa que a de hoje.
- Um terceiro adapter (Jaeger, outro backend) só implementa a consulta.
- O adapter do Tempo completa o `traceID` da busca com zeros à esquerda até 32 caracteres, porque a
  busca os corta.
- A consulta filtra a operação pelo nome do span, ou pelo `http.route` quando ele existe, como o
  ADR-0013 já faz com o arquivo.
- Para ler o placar pelo Tempo, as gravações são reemitidas com o horário atual: com o padrão
  `ingestion_time_range_slack: 2m`, spans com horário fora da ingestão não aparecem na busca.

## Como validar

- **Placar igual:** `rootline-eval score` sem `--update` com o adapter de arquivo na porta nova.
- **Mesmo Resultado pelas duas fontes:** um cenário gravado no arquivo e no Tempo dá o mesmo
  Resultado (Causa, linha, commit). A divisão do tempo pode variar dentro da amostra.
- **depguard:** o domínio continua sem importar o adapter do Tempo.
