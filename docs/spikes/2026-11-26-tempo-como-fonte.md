---
id: SPIKE-2026-11-26-tempo
title: Tempo como fonte de traces do watch (fatia 0 do ADR-0033)
status: Concluído
summary: Tempo 3.0.0 local com duas gravações do placar, no live store e nos blocos do backend. A busca TraceQL devolve, por trace, a duração exata do span de entrada (durationNanos) e o status, inclusive quando ele não é a raiz; n, p50 e erros batem com o arquivo. 200 traces por busca em ~25 ms; trace completo em ~5,5 ms (p50); valores de service.version em ~5 ms. Surpresas - traceID sem zeros à esquerda, durationMs do trace é da raiz e some quando é 0, e spans com horário mais de 2 min fora da ingestão não aparecem na busca.
---

# Tempo como fonte de traces do watch

Responde às questões em aberto do [ADR-0033](../adr/0033-traces-do-tempo-por-consulta-em-duas-camadas.md).

- **Data:** 26/11/2026, uma sessão.
- **Escala:** pequena. Duas gravações do placar (`n1-basic` e `n1-helper`, 5 606 spans cada, 200
  traces por versão na operação-alvo) e uma cópia da `n1-pagination` com erros injetados. Os números
  de latência valem como ordem de grandeza num Tempo local e vazio, não como medida de produção.

## Versões

| Peça | Versão |
|---|---|
| Tempo | 3.0.0 (`grafana/tempo:latest` em 26/11/2026, modo monolítico, storage local) |
| Docker | 29.10.0 |

## Como foi feito

1. Tempo monolítico com o receptor OTLP HTTP e storage local:

   ```yaml
   server:
     http_listen_port: 3200
   distributor:
     receivers:
       otlp:
         protocols:
           http:
             endpoint: 0.0.0.0:4318
   storage:
     trace:
       backend: local
       local:
         path: /var/tempo/traces
       wal:
         path: /var/tempo/wal
   ```

2. Cada linha do `traces.jsonl` de uma gravação é um pedido de exportação OTLP JSON. Um script
   deslocou todos os horários (spans e eventos) para que a gravação começasse no momento da carga, e
   enviou cada linha num `POST /v1/traces`.
3. Na `n1-helper`, cada span de entrada do `demo-app` ganhou um pai de um serviço falso `gateway`
   (1 ms antes, 3 ms depois), para testar a operação de entrada que não é a raiz do trace.
4. Numa cópia da `n1-pagination`, com outra `service.version`, um em cada dez spans de entrada foi
   marcado com status de erro (40 de 400).
5. As buscas foram comparadas, versão a versão, com o n, o p50 e os erros calculados do arquivo.

## Resultados

### Busca da operação de entrada

Consulta usada (uma por versão e operação):

```
{resource.service.name="demo-app" && resource.service.version="<sha>"
 && name="GET /customers/{id}/orders" && kind=server} | select(status)
```

com `spss=1` (um span por trace, o de entrada), `limit`, `start` e `end`.

| Gravação | Versão | Arquivo: n / p50 | Busca: n / p50 do span | `durationMs` do trace (p50) | Raiz |
|---|---|---|---|---|---|
| n1-basic | ffbd7e6 | 200 / 0,796 ms | 200 / 0,796 ms | 0 (campo ausente) | demo-app |
| n1-basic | b236ed9 | 200 / 1,981 ms | 200 / 1,981 ms | 1 ms | demo-app |
| n1-helper | e108e7f | 200 / 0,818 ms | 200 / 0,818 ms | 4 ms | gateway |
| n1-helper | 0a3f4b3 | 200 / 1,959 ms | 200 / 1,959 ms | 5 ms | gateway |

- O `durationNanos` do span casado é a duração exata do span de entrada, igual ao arquivo, com a
  operação na raiz ou não. **A Regressão sai da busca sem baixar nenhum trace.**
- O `durationMs` do trace é a duração da raiz (o `gateway` na `n1-helper`), em milissegundos
  inteiros, e o campo some quando vale 0. Não serve para a Regressão, que trabalha com 2 ms em
  operações de 3 ms (ADR-0032).
- **Erros:** o `select(status)` traz o status de cada span de entrada na mesma busca: 40 `error` e
  360 `unset` na cópia com erros, exatamente o injetado. Um filtro `&& status=error` à parte também
  dá 40. Não é preciso uma segunda consulta.

### Custo

| Chamada | Resultado |
|---|---|
| Valores de `service.version` (`/api/v2/search/tag/resource.service.version/values` com `q` do serviço) | ~5 ms, 328 B, as 4 versões |
| Busca, 20 traces | ~9,5 ms, ~24 KB |
| Busca, 100 traces | ~20 ms, ~120 KB |
| Busca, 200 traces (todos) | ~23 a 32 ms, ~215 KB (~1,1 KB por trace) |
| Busca com o `select(status)`, 400 traces | ~42 ms |
| Trace completo (`/api/v2/traces/<id>`), 50 em série | p50 5,5 ms, máx. 14,1 ms, total 292 ms; ~27 KB e 23 spans por trace |

- **Dados com mais de 15 minutos.** Por padrão (`query_backend_after: 15m`), a busca sobre uma janela
  mais antiga vai para os blocos do backend, e não para o live store. A mesma busca da n1-basic,
  feita 17 minutos depois, numa janela só do passado, leu 1 bloco do backend e devolveu os mesmos
  200 traces por versão, com o mesmo p50 (0,796 e 1,981 ms), em ~25 ms. É o caminho da janela de
  24 h da referência (ADR-0037).
- Com `limit` maior que o número de traces, a busca devolve todos (varredura de 20 a 100 000, três
  repetições, sempre igual). O teto padrão do Tempo é `max_result_limit: 262144`.
- Uma busca feita enquanto o bloco ainda estava sendo fechado devolveu 100 de 200 traces. Depois
  disso não se repetiu.

## Surpresas

- **`traceID` sem zeros à esquerda.** A busca devolve o ID como número hexadecimal, sem os zeros à
  esquerda: 11 e 15 dos 200 IDs de cada versão vieram com menos de 32 caracteres. O adapter completa
  com zeros até 32 antes de buscar o trace completo ou de comparar com o arquivo.
- **Horário fora da ingestão fica invisível na busca.** Com `ingestion_time_range_slack: 2m`
  (padrão), spans enviados com horário 10 minutos no passado não aparecem na busca TraceQL. A busca
  por ID e os valores de tag continuam achando. Em produção os spans chegam na hora. Para ler o
  placar pelo Tempo, as gravações precisam ser reemitidas com o horário atual, como fez este spike.
- **Atraso até ficar pesquisável.** Os traces só apareceram na busca cerca de 37 s depois do envio,
  quando o bloco foi fechado (`max_block_duration: 30s`). Isso é irrelevante para o watch, porque o
  gatilho é a amostra.
- **Sem `http.route` no span de entrada.** O `demo-app` não emite o atributo (o ADR-0013 já tira a
  rota do nome do span). A consulta filtra pelo nome do span, e o adapter precisa aceitar o
  atributo, quando existir, como o ADR-0013 faz.

## O que muda nos ADRs

- **ADR-0033:** as questões em aberto têm resposta. A busca devolve a duração e o status do span de
  entrada mesmo fora da raiz; os erros vêm na mesma busca; o custo é baixo nesta escala. O ADR passou
  a Aceito, com as surpresas acima nas consequências.
- **ADR-0035:** listar os valores de `service.version` custa ~5 ms. Consultar a cada minuto é
  barato.
- **Fora do escopo deste spike:** o Jaeger; o Tempo distribuído e com volume de produção; a busca
  sobre milhões de spans; a amostragem real do Collector.
