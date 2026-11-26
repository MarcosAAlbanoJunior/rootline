# Guia de instrumentação

Este guia prepara um serviço Go para o Rootline. Ele cobre o que o serviço precisa emitir, como
instalar o hook de chamador, como gravar os traces das duas versões e o que o Rootline ainda não
suporta. As decisões por trás estão no [ADR-0002](adr/0002-localizacao-de-queries-no-codigo.md) e no
[ADR-0029](adr/0029-guarda-versao-e-publicacao-do-hook.md).

## Pré-requisitos

- **OpenTelemetry no serviço**, com spans de servidor HTTP (por exemplo, `otelhttp`) e spans de
  cliente de banco (veja as receitas). O Rootline lê traces OTLP, não métricas nem logs.
- **`service.version` igual ao SHA do commit** que gerou o binário, no resource do OpenTelemetry.
  O Rootline separa as duas versões por esse atributo e, com `--repo`, procura os commits entre
  elas. Use o SHA completo (`git rev-parse HEAD`), passado ao processo, por exemplo, numa variável
  de ambiente:

  ```go
  tp := sdktrace.NewTracerProvider(
  	sdktrace.WithBatcher(exp),
  	sdktrace.WithResource(resource.NewSchemaless(
  		attribute.String("service.name", "<seu-servico>"),
  		attribute.String("service.version", os.Getenv("SERVICE_VERSION")), // SHA do commit
  	)),
  )
  ```

- **Binário com ou sem `-trimpath`.** Os dois funcionam: o hook deriva o caminho do arquivo pelo
  pacote da função, relativo ao módulo principal, e não pelo caminho absoluto do build.
- **PostgreSQL acessado pelo pgx v5** (`github.com/jackc/pgx/v5`), direto ou como driver de
  `database/sql`. Veja as [limitações](#limitações-atuais).

## Instalar o hook

O hook é um módulo separado, publicado por tags `hook/vX.Y.Z`:

```sh
go get github.com/MarcosAAlbanoJunior/rootline/hook@v0.1.0
```

O hook é um `pgx.QueryTracer`. Ele lê o span que a instrumentação de banco abriu e anota nele a
pilha da aplicação. Por isso ele precisa de uma instrumentação de banco **por fora** dele e só
anota spans de cliente (`SpanKind` Client). Sem instrumentação de banco, o span ativo é o do
handler HTTP, e o hook não anota nada. No `rootline analyze`, a Verificação `db_spans` mostra então
0 spans de banco reconhecidos e aponta para este guia. Com a instrumentação e sem o hook, a
`caller_hook` mostra 0% dos spans de banco com a pilha.

Escolha a receita pela forma como o serviço abre a conexão. As quatro foram verificadas no
laboratório de apps reais.

### 1. pgxpool com otelpgx

O hook entra depois do otelpgx num multitracer. A ordem importa: o otelpgx abre o span, e o hook
o anota.

```go
import (
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"

	callerhook "github.com/MarcosAAlbanoJunior/rootline/hook"
)

cfg, err := pgxpool.ParseConfig(databaseURL)
if err != nil {
	return err
}
cfg.ConnConfig.Tracer = multitracer.New(otelpgx.NewTracer(), callerhook.New())
pool, err := pgxpool.NewWithConfig(ctx, cfg)
```

O mesmo vale para `pgx.Connect`: monte o `pgx.ConnConfig` com `pgx.ParseConfig` e use
`pgx.ConnectConfig`.

### 2. pgx sem tracer próprio

Quando o span de banco vem de uma camada acima do pgx (otelsql ou o plugin do GORM, receitas 3 e 4),
o pgx não tem tracer próprio. O hook entra sozinho no `Tracer`, sem multitracer:

```go
connCfg, err := pgx.ParseConfig(databaseURL)
if err != nil {
	return err
}
connCfg.Tracer = callerhook.New()
```

Se o serviço não tem nenhuma instrumentação de banco, use a receita 1: o otelpgx é quem cria os
spans de banco.

### 3. pgx como driver de `database/sql` sob otelsql

Registre a configuração do pgx com o hook e abra a conexão pelo otelsql
(`github.com/XSAM/otelsql`) com o nome registrado:

```go
import (
	"github.com/XSAM/otelsql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	callerhook "github.com/MarcosAAlbanoJunior/rootline/hook"
)

connCfg, err := pgx.ParseConfig(databaseURL)
if err != nil {
	return err
}
connCfg.Tracer = callerhook.New()
db, err := otelsql.Open("pgx", stdlib.RegisterConnConfig(connCfg),
	otelsql.WithAttributes(semconv.DBSystemNamePostgreSQL),
)
```

O `WithAttributes(semconv.DBSystemNamePostgreSQL)` marca os spans como PostgreSQL. Sem ele, a
Verificação `db_spans` não sabe de que banco eles são.

### 4. GORM sobre pgx

O GORM recebe um `*sql.DB` aberto pelo pgx com o hook, e o plugin OpenTelemetry do GORM
(`gorm.io/plugin/opentelemetry/tracing`) cria os spans de banco:

```go
import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"

	callerhook "github.com/MarcosAAlbanoJunior/rootline/hook"
)

connCfg, err := pgx.ParseConfig(dsn)
if err != nil {
	return err
}
connCfg.Tracer = callerhook.New()
db, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*connCfg)}), &gorm.Config{})
if err != nil {
	return err
}
if err := db.Use(tracing.NewPlugin()); err != nil {
	return err
}
```

O `gorm.Open(postgres.Open(dsn))` não serve: ele abre a conexão sem passar pelo `connCfg` e o hook
nunca é chamado.

## Contrato de atributos do hook

O hook escreve estes atributos no span de cliente de banco. Eles são o contrato entre o hook e o
`rootline analyze`. Antes da 1.0, uma mudança incompatível neles sobe o minor da versão do hook, e
a Verificação `caller_hook` avisa quando a versão vista não é a que o `analyze` lê.

| Atributo | Conteúdo |
|---|---|
| `rootline.db.phase` | `query` ou `prepare`. |
| `rootline.hook.version` | A versão do hook, igual à tag sem o prefixo (`0.1.0` em `hook/v0.1.0`). |
| `rootline.app_stack` | Os frames da aplicação, do mais interno para fora, cada um `<função> <arquivo relativo ao módulo>:<linha>`. No máximo 8 entradas e 2048 bytes. |
| `rootline.module.path` | O caminho do módulo principal, base dos arquivos. |
| `code.function.name`, `code.file.path`, `code.line.number` | O frame mais interno da aplicação. |

Os atributos de pilha só aparecem nos spans de query com pelo menos um frame da aplicação.

## Gravar as duas versões

O Rootline lê os traces de um arquivo JSON Lines no formato do file exporter do OpenTelemetry
Collector ([ADR-0008](adr/0008-traces-em-arquivo-e-gravacao-separada-da-analise.md)). Uma
configuração mínima do Collector (imagem `otel/opentelemetry-collector-contrib`):

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
processors:
  batch: {}
exporters:
  file:
    path: /out/traces.jsonl
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [file]
```

No serviço, aponte o exporter OTLP HTTP para o Collector pela variável padrão do OpenTelemetry:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

Grave as duas versões no mesmo arquivo, uma de cada vez:

1. Suba o serviço no commit bom, com `SERVICE_VERSION` igual ao SHA dele, e aplique a carga.
2. Pare o serviço, troque para o commit ruim (com o SHA dele em `SERVICE_VERSION`) e aplique a
   mesma carga.
3. Pare o Collector, para ele gravar o que ficou no lote.

Aqueça o serviço e o banco antes de cada gravação: com o banco frio, a primeira versão parece mais
lenta do que é. O aquecimento pode ficar no arquivo, desde que seja igual nas duas versões. O
`rootline symptoms` só compara uma operação com pelo menos 30 traces em cada versão.

Aplique a mesma carga às duas versões, com a máquina sem outra carga pesada. Se a versão ruim
gravar bem menos (ou bem mais) traces que a boa em quase todas as operações, o `symptoms` não sugere
operação, porque a comparação entre elas deixa de valer
([ADR-0030](adr/0030-regressao-symptoms-e-carga-desigual.md)).

### Serviço em contêiner

Suba o Collector com a configuração acima, salva em `otelcol.yaml`, e uma pasta de saída:

```sh
mkdir -p out && chmod 777 out   # o file exporter roda como uid 10001
docker run -d --name rootline-col -p 4318:4318 \
  -v "$PWD/otelcol.yaml:/etc/otelcol-contrib/config.yaml:ro" \
  -v "$PWD/out:/out" \
  otel/opentelemetry-collector-contrib:0.165.0
```

Com SELinux (Fedora, RHEL), acrescente `:Z` aos dois volumes. Os traces saem em `out/traces.jsonl`.
Pare o Collector com `docker stop rootline-col` depois da segunda versão.

Dentro do contêiner do serviço, `localhost` é o próprio contêiner. Aponte o exporter para o host:

```yaml
services:
  api:
    environment:
      OTEL_EXPORTER_OTLP_ENDPOINT: http://host.docker.internal:4318
      SERVICE_VERSION: ${SERVICE_VERSION}
    extra_hosts:
      - "host.docker.internal:host-gateway"   # necessário no Linux
```

O hook é uma mudança local, que não vai para o histórico. Para gravar as duas versões com ele:

```sh
git diff > ../rootline-hook.patch   # depois de instalar o hook na versão atual
git stash
git checkout <sha-bom>
git apply ../rootline-hook.patch    # se o arquivo da conexão mudou, aplique a receita à mão
SERVICE_VERSION=$(git rev-parse HEAD) docker compose up -d --build api
# aqueça, aplique a carga e pare o serviço
git checkout -- . && git checkout <sha-ruim> && git apply ../rootline-hook.patch
SERVICE_VERSION=$(git rev-parse HEAD) docker compose up -d --build api
# a mesma carga; depois pare o serviço e o Collector
git checkout -- . && git checkout main && git stash pop
```

Na análise, o `--repo` é o repositório do serviço, limpo e com os dois commits. O `rootline` lê o
código de cada versão do git, não da cópia de trabalho.

## Analisar

```sh
go install github.com/MarcosAAlbanoJunior/rootline/cmd/rootline@v0.2.0
rootline symptoms --traces traces.jsonl --repo .
rootline analyze --traces traces.jsonl --repo . --service <seu-servico> \
  --operation "GET /rota" --bad-version <sha-ruim> --baseline-version <sha-bom>
```

O CLI 0.2.x lê o hook 0.1.x; a Verificação `caller_hook` avisa quando eles não combinam e diz
qual atualizar.

O `symptoms` lista as operações que regrediram e imprime o `analyze` pronto da sugerida. O
`analyze` dá a causa ou `Inconclusive`, e a seção **Checks** diz o que foi verificado, o que foi
pulado e por quê, com uma dica para cada pulo.

### Escolher a operação

**Quando o `analyze` da sugerida dá `inconclusive`** e outras linhas da tabela têm Regressão, analise
as seguintes, de cima para baixo, até uma dar a causa. A sugestão é só a de maior razão de p50: a
causa pode estar na segunda ou na terceira linha. Troque a `--operation` no comando que o `symptoms`
imprimiu e leia as **Checks** de cada Resultado.

**Quando não há sugestão**, o texto abaixo da tabela diz o motivo:

- **Carga desigual.** Se a carga das duas gravações foi mesmo diferente (ou a máquina estava
  ocupada), grave de novo com a mesma carga. Se foi a mesma, as requisições ficaram presas na versão
  ruim, e as colunas abaixo ajudam.
- **Nenhuma operação destoa** numa regressão generalizada. Use as colunas abaixo.
- **Nenhuma regrediu.** Confira os SHAs do cabeçalho e se a carga passou pela rota lenta.

Sem sugestão, analise as operações uma a uma, nesta ordem, até uma dar a causa: as de sumiço fora do
padrão, depois as de início mais cedo, depois o resto na ordem da tabela. Se nenhuma operação com
Regressão der causa, pare: ela está fora do que o Rootline enxerga. As **Checks** dizem o que ficou
de fora. Quando a `trace_shape` aponta o tempo próprio, investigue o handler com um profile de CPU e
as esperas por lock; quando aponta o banco, os planos das queries, os locks e o pool.

Duas colunas dão pistas. Nenhuma delas prova a origem:

- **Sumiço** (`n ratio`, marcado com `drop`): a fração dos traces que a operação manteve. Compare
  com as outras linhas. Uma operação que sumiu muito mais que as demais é candidata, porque uma
  requisição presa não termina e não vira span.
- **Início** (`onset`): quanto tempo a versão ruim levou até a operação ficar lenta. Quem começa
  bem antes das outras é candidata; quem começa depois pode ser vítima de um recurso já esgotado.
  Diferenças pequenas não decidem.

Exemplo, com um pool de conexões esgotado:

| Operation | Regression | n ratio | p50 ratio | onset |
|---|---|---|---|---|
| GET /orders/{id} | latency and error | 0.20 drop | 4200x | +4.3s |
| GET /orders | latency and error | 0.20 drop | 1800x | +4.3s |
| POST /orders | latency and error | 0.20 drop | 17x | +4.6s |
| POST /jobs/claim | sample < 30 | 0.02 drop | 0.01x | - |

A maior razão de p50 engana: todas as rotas esperam pelo mesmo recurso. Os inícios são quase iguais
e não decidem. O que destoa é o sumiço do `POST /jobs/claim`, que manteve 2% dos traces contra 20%
das outras, e é a primeira operação a investigar, mesmo marcada com amostra pequena e sem Regressão.
Analise-a. Se o Resultado for `inconclusive` (com poucos traces, é provável), olhe o código e os logs
dessa rota e siga a ordem acima.

## Limitações atuais

- **Só PostgreSQL pelo pgx v5.** MySQL e MongoDB não são suportados pelo hook nem pelos detectores
  de N+1 e de índice faltando: a Verificação `db_spans` conta os spans desses bancos como
  ignorados. Com PostgreSQL por `database/sql` com outro driver, o hook não anota os spans, e a
  causa fica sem a linha e o commit. A divisão do tempo (abaixo) conta qualquer span de banco.
- **Planos de execução só na avaliação.** O detector de índice faltando precisa dos planos das
  queries, e hoje só o `rootline-eval record` os gera (`plans.jsonl`, passado com `--plans`).
  Capturá-los contra um banco de teste está no roadmap.
- **Só latência.** A v1 analisa regressões de latência. A Verificação `error_rate` mostra a taxa de
  erro das duas versões, mas o Rootline não busca a causa de uma regressão de erro.
- **Quatro causas.** O Rootline reconhece N+1, índice faltando, `slow_call` e `added_work`. As
  duas últimas vêm da divisão do tempo da regressão por dependência, que aparece nas evidências
  ([ADR-0031](adr/0031-detector-de-forma-do-trace.md)). Elas precisam da `--baseline-version`, de
  uma regressão de latência (com pelo menos 30 traces da operação em cada versão) e de uma média
  por trace que suba 5 ms ou mais. Só saem como causa quando não há N+1 nem índice faltando:
  - **`slow_call`** (chamada externa mais lenta): uma dependência HTTP ou RPC explica pelo menos
    80% da diferença, o tempo por chamada pelo menos dobra e a fração dos traces em que ela
    aparece varia menos de 25 pontos. A localização é o handler da rota, aproximada, **sem linha
    nem commit**, porque o hook não anota esse span. A chamada precisa estar instrumentada (por
    exemplo, com `otelhttp.NewTransport`); sem span de cliente, o tempo dela conta como tempo
    próprio.
  - **`added_work`** (trabalho a mais por requisição): a fração dos traces em que uma dependência
    aparece sobe pelo menos 25 pontos (um cache que passou a furar, por exemplo) e ela explica pelo
    menos 80% da diferença. Em banco com o hook, a localização é a função da aplicação comum às
    queries que passaram a aparecer, com a linha; o commit sai quando algum entre as duas versões
    mudou uma função da pilha. Sem a pilha, ou sem função comum, a localização cai no handler da
    rota, sem linha nem commit.
- **O que continua `Inconclusive`.** Banco mais lento sem N+1 nem índice faltando reconhecido
  (lock, pool esgotado ou, sem planos, o próprio índice) e tempo próprio maior (CPU, lock ou
  espera sem instrumentação). A origem está fora do trace, e a Verificação `trace_shape` diz para
  onde o tempo foi. Um aumento diluído entre várias dependências também não dá causa.
- **As receitas não têm teste de CI.** Uma mudança no otelsql ou no plugin do GORM só aparece na
  próxima rodada do laboratório.
