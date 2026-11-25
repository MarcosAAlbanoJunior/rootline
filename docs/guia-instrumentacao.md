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
lenta do que é. O `rootline symptoms` só compara uma operação com pelo menos 30 traces em cada
versão.

## Analisar

```sh
go install github.com/MarcosAAlbanoJunior/rootline/cmd/rootline@main
rootline symptoms --traces traces.jsonl --repo .
rootline analyze --traces traces.jsonl --repo . --service <seu-servico> \
  --operation "GET /rota" --bad-version <sha-ruim> --baseline-version <sha-bom>
```

O `symptoms` lista as operações que regrediram e imprime o `analyze` pronto da sugerida. O
`analyze` dá a causa ou `Inconclusive`, e a seção **Checks** diz o que foi verificado, o que foi
pulado e por quê, com uma dica para cada pulo.

## Limitações atuais

- **Só PostgreSQL pelo pgx v5.** MySQL, MongoDB e `database/sql` com outro driver não são
  suportados: a Verificação `db_spans` conta os spans desses bancos como ignorados.
- **Planos de execução só na avaliação.** O detector de índice faltando precisa dos planos das
  queries, e hoje só o `rootline-eval record` os gera (`plans.jsonl`, passado com `--plans`).
  Capturá-los contra um banco de teste está no roadmap.
- **Só latência.** A v1 analisa regressões de latência. A Verificação `error_rate` mostra a taxa de
  erro das duas versões, mas o Rootline não busca a causa de uma regressão de erro.
- **Detectores de N+1 e de índice faltando.** Outras causas (pool esgotado, lock, chamada externa
  lenta) dão `Inconclusive`, e as Verificações dizem o que foi olhado.
- **As receitas não têm teste de CI.** Uma mudança no otelsql ou no plugin do GORM só aparece na
  próxima rodada do laboratório.
