---
id: SPIKE-2026-11-22-adr-0002
title: Evidências do spike do ADR-0002 (localização pela pilha da aplicação em Go)
status: Concluído
summary: A regra "primeiro frame dentro de um loop" acertou os 4 cenários de N+1; pgx já compõe tracers (multitracer); o caminho relativo ao módulo é igual com e sem -trimpath. Surpresas - otelhttp não emite http.route, spans de prepare do otelpgx repetem a query sem pilha, code.stacktrace é estável mas tem outro formato. Custo do hook ~3,6µs/query (~1,9µs com cache).
---

# Evidências do spike do ADR-0002

- **Código e evidências brutas:** branch [`spike/adr-0002`](https://github.com/MarcosAAlbanoJunior/rootline/tree/spike/adr-0002/spike/adr-0002)
  (pasta `spike/adr-0002/`, com `README.md` explicando como reproduzir e `evidence/` com os traces
  OTLP JSON, a saída do verificador, os benchmarks e as medições de latência).
- **Data:** 22/11/2026. Timebox de 1 dia, usado em uma sessão.

## Versões

| Peça | Versão |
|---|---|
| Go | 1.27.4 (linux/amd64) |
| pgx | v5.13.0 |
| otelpgx | v0.13.0 |
| otelhttp | v0.72.0 |
| OTel Go SDK | v1.47.0 (semconv v1.43.0) |
| OTel Collector contrib | 0.165.0 (file exporter, OTLP JSON) |
| PostgreSQL | 19.0 |
| Máquina | AMD Ryzen 7 5700, Fedora, Postgres e Collector em Docker local |

## O que foi montado

- Demo mínimo (`net/http` + pgxpool) com quatro rotas de N+1 sobre os mesmos dados (20 clientes,
  10 pedidos cada, 3 itens por pedido):
  - `explicit`: loop no handler, query inline;
  - `helper`: loop no service, query no repository atrás de uma função auxiliar;
  - `errgroup`: uma goroutine por pedido (`errgroup`, limite 4);
  - `rangefunc`: iterador `iter.Seq2` com range-over-func.
- Hook de chamador como `pgx.QueryTracer`, depois do otelpgx num `multitracer`. Ele grava
  `code.function.name`, `code.file.path`, `code.line.number`, `rootline.module.path` e
  `rootline.app_stack` (até 8 frames do módulo principal, até 2048 bytes).
- Verificador (`cmd/loopcheck`): lê o arquivo do Collector e aplica a regra do ADR-0002 com
  `go/ast` a cada span de banco.

## Respostas às perguntas do ADR-0002

### 1. O pgx compõe tracers? Sim.

O pgx v5.13.0 tem `github.com/jackc/pgx/v5/multitracer`. O `TraceQueryStart` dele chama os tracers
em ordem e repassa o `ctx` devolvido por um para o seguinte. Com o hook depois do otelpgx,
`trace.SpanFromContext(ctx)` devolve o span da query. **Não é preciso escrever um embrulho.**

### 2. A regra "primeiro frame com o ponto de chamada dentro de um loop" acerta? Sim, nos 4 cenários.

Sobre todos os traces gravados com o hook (`evidence/loopcheck-hook.txt`):

| Cenário | Queries de itens | Frame escolhido (de dentro para fora) | Correto? |
|---|---|---|---|
| explicit | 50 | frame 0/1 `main.main.func2` `app/main.go:73` (loop 72-84) | sim |
| helper | 60 | frame 2/4 `service.(*Service).Helper` `app/service/service.go:27` (loop 26-32) | sim |
| errgroup | 50 | frame 2/3 `service.(*Service).Errgroup.func1` `app/service/service.go:48` (loop 46-57) | sim |
| rangefunc | 50 | frame 2/6 `service.(*Service).RangeFunc-range1` `app/service/service.go:68` (loop 64-73) | sim |

A query "+1" (pedidos do cliente) não tem nenhum frame dentro de um loop nos quatro cenários. Isso
também está correto: ela é a query pai, não a repetida.

### 3. Como aparecem os frames de range-over-func e de goroutines?

- **range-over-func:** o corpo do loop aparece como uma função sintética com sufixo `-range1`
  (`service.(*Service).RangeFunc-range1`), com a linha dentro do corpo do `for range`. O frame do
  iterador (`repo.(*Repo).Orders.func1`) fica mais para fora e não é alcançado, porque a regra
  para no primeiro frame que acerta.
- **errgroup:** a pilha da goroutine tem só 3 frames da aplicação e termina na closure
  (`Errgroup.func1`). A linha da closure fica dentro do corpo do loop que a lança, então a regra
  acerta sem precisar de nada especial.

### 4. O caminho relativo ao módulo funciona com e sem `-trimpath`? Sim.

O hook deriva o caminho do pacote pelo nome da função (e usa `debug.ReadBuildInfo().Path` para o
pacote `main`). Os conjuntos de `code.file.path` gravados com e sem `-trimpath` são idênticos:
`app/main.go` e `app/repo/repo.go`. Testes unitários cobrem closures, range-over-func, genéricos
(`[...]`), pacote `main`, raiz do módulo, sufixo `/v2`, módulo com prefixo parecido e código de terceiros.

### 5. Nome do atributo da pilha

Na semconv v1.43.0, `code.stacktrace` é **estável**, mas é uma **string na representação natural do
runtime** (no Go, o formato de panic, com caminhos absolutos). A pilha do Rootline é outra coisa:
filtrada para o módulo principal, com caminhos relativos ao módulo e limitada a 8 frames. Usar
`code.stacktrace` com esse conteúdo contrariaria a convenção. **Recomendação:** manter
`rootline.app_stack` (lista de strings) e continuar usando os três atributos `code.*` estáveis para
o frame mais interno.

### 6. Custo do hook

**Por query (benchmarks, pilha de ~20 frames, `evidence/benchmarks.txt`):**

| Etapa | Tempo | Alocação |
|---|---|---|
| Captura (`runtime.Callers`) | ~0,91 µs | 512 B, 1 alloc |
| Resolução (frames, filtro, caminho) | ~1,95 µs | 1320 B, 18 allocs |
| Resolução com cache por PC | ~0,23 µs | 320 B, 1 alloc |
| Montagem dos atributos | ~0,75 µs | 1216 B, 12 allocs |
| **Total sem cache / com cache** | **~3,6 µs / ~1,9 µs** | |

**Ponta a ponta (11 queries por requisição, 500 requisições sequenciais por rota, 3 rodadas;
mediana das rodadas, `evidence/overhead.txt`):**

| Rota | p50 sem hook | p50 com hook | p50 com hook + cache | p95 sem / com / cache |
|---|---|---|---|---|
| errgroup | 696 µs | 748 µs (+7%) | 733 µs (+5%) | 1,39 / 1,41 / 1,53 ms |
| explicit | 1,27 ms | 1,39 ms (+9%) | 1,41 ms (+10%) | 2,28 / 2,28 / 2,10 ms |
| helper | 1,18 ms | 1,47 ms (+25%) | 1,36 ms (+15%) | 1,78 / 2,27 / 2,40 ms |
| rangefunc | 1,29 ms | 1,64 ms (+27%) | 1,36 ms (+6%) | 2,12 / 2,52 / 2,33 ms |

**Como ler esses números, com honestidade:**
- A mesma configuração variou cerca de ±10% entre rodadas. Diferenças abaixo disso não são sinal.
- Os benchmarks preveem ~40 µs por requisição (11 × 3,6 µs), cerca de 3%. O ponta a ponta mostra
  diferenças maiores e inconsistentes; parte provável vem do custo de exportar atributos maiores
  e de GC, mas este setup não separa isso do ruído.
- É o pior caso relativo: Postgres local, queries de ~0,1 ms. Com latência de rede de banco real
  (~1 ms por query), o mesmo custo absoluto pesa uma ordem de grandeza menos.
- **Conclusão:** o custo absoluto por query é baixo e o cache corta pela metade. Uma medida
  confiável de overhead precisa de mais rodadas e de um ambiente mais controlado (CPU fixa); isso
  cabe à fase de gravação da avaliação (ADR-0008), que já registra o overhead no manifesto.

## Surpresas (não previstas no ADR-0002)

1. **otelhttp v0.72.0 não emite o atributo `http.route`.** Com o `otelhttp.NewHandler` envolvendo
   o `ServeMux` por fora, a rota só aparece no **nome** do span (`GET /customers/{id}/orders/explicit`).
   O ADR-0005 identifica a operação de entrada por método + `http.route`, e o nível 2 do ADR-0002
   usa a rota. É preciso decidir de onde vem a rota.
2. **Spans de prepare do otelpgx repetem a query sem pilha.** Na primeira vez que uma conexão usa
   um statement, o otelpgx cria um span de prepare com o **mesmo nome** (`SELECT`) e o **mesmo
   `db.query.text`** da query, com `pgx.prepare_stmt.name` e sem `pgx.rows_affected`. O hook só
   implementa `QueryTracer`, então esses spans ficam sem pilha (7 nos traces gravados, até um por
   conexão × statement). Se o detector contar esses spans, o número de repetições fica inflado em
   até o tamanho do pool.
3. **`pgx.rows_affected` vem em todo span de query** (3 linhas por query de itens). É o sinal
   "linhas por query" que a spec do detector de N+1 precisa para separar N+1 de paginação, e ele
   já está disponível sem mudar nada.
4. **Cache por PC e inlining.** O cache do spike resolve cada PC isoladamente e guarda só o
   primeiro frame. Quando uma função é inlinada, um único PC corresponde a vários frames (a função
   inlinada e quem a chamou); guardar só o primeiro pode perder justamente o frame do loop. Neste
   spike o resultado com cache continuou correto, mas a implementação real precisa guardar a
   expansão completa de cada PC.

## Recomendações para o ADR-0002

O spike confirma a estratégia. O ADR-0002 pode passar a Aceito com estes ajustes:

1. Composição pelo `multitracer` do pgx, sem embrulho próprio.
2. Atributo da pilha: `rootline.app_stack`, não `code.stacktrace`.
3. Cache por PC ligado por padrão, guardando a expansão completa de inlining.
4. Spans de prepare: definir se o hook também implementa `PrepareTracer` e garantir que o detector
   não conte prepare como repetição.
5. Rota HTTP: definir a fonte (nome do span ou atributo) para o ADR-0005 e o nível 2.
