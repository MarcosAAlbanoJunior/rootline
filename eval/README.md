# Avaliação

Cada cenário em `fixtures/scenarios/<nome>/` vira um repo de fixture com commits reais
([ADR-0004](../docs/adr/0004-cenarios-de-avaliacao-como-commits-reais.md)). O placar (`placar.json`)
guarda o resultado do modo só-detectores na `main` e é o portão dos PRs
([ADR-0009](../docs/adr/0009-pontuacao-da-avaliacao.md)).

## Regra dos cenários

Todo cenário quebra uma suposição do detector ou da localização, e diz qual no campo `assumption` do
`scenario.json`. Não há exceção: o `LoadScenario` recusa cenário sem `assumption`. Um cenário que não
quebra nada só infla o placar.

Exemplos: o n1-basic é o caso básico (loop no service, query no repository); um cenário com o loop dois
frames acima da query, em outro pacote, quebra a suposição de que o loop está logo acima da query.

## Cenários atuais

| Cenário | Suposição que quebra |
|---|---|
| `n1-basic` | Nenhuma: caso básico, loop no service e query no repository. |
| `n1-helper` | O loop está logo acima da query: aqui fica dois frames acima, em outro pacote, e a query repetida é uma agregada de totais. |
| `n1-rangefunc` | O frame do loop tem o nome da função declarada: o corpo de um `for range` sobre `iter.Seq2` vira `History-range1` ([ADR-0019](../docs/adr/0019-funcao-declarada-no-lugar-de-nomes-sinteticos.md)). |
| `n1-errgroup` | O frame da função que contém o loop está na pilha da query: cada query roda numa goroutine lançada no loop com `errgroup`, e a pilha começa na closure (`Summary.func1`), cuja linha fica dentro do loop ([ADR-0019](../docs/adr/0019-funcao-declarada-no-lugar-de-nomes-sinteticos.md)). |
| `n1-workpool` | As queries repetidas têm o mesmo pai direto: cada uma roda num job de um pool de trabalho, sob um span `workpool.job` próprio, e o detector as agrupa pelo pai efetivo ([ADR-0020](../docs/adr/0020-pai-efetivo-com-filhos-do-cliente-de-banco.md)). |

O `n1-workpool` é o primeiro cenário com carga multirrota: além da rota do sintoma
(`/customers/{id}/shipments`, peso 3), a carga passa pela rota saudável `/customers/{id}/dashboard`
(peso 1), que usa o mesmo pool com dois jobs de queries diferentes. A rota saudável prova na gravação
que o handler responde sob o pool, mas **não é pontuada**: o `score` analisa só a rota do sintoma.

## Código pré-existente: patch de preparação

O demo-app base fica mínimo. O código que um cenário precisa antes da regressão (um pacote novo, uma
rota nova) entra por um **patch de preparação** no commit `referencia`:

```json
"commits": [
  {"label": "referencia", "patch": "preparacao.patch"},
  {"label": "regressao", "patch": "regressao.patch"}
]
```

- Prefira arquivos novos. Edite arquivos existentes só no mínimo (por exemplo, ligar a rota nova em
  `demo-app/main.go`), porque cada edição pode deixar de aplicar quando o demo-app mudar.
- O patch de preparação não pode conter o marcador `rootline:answer`: nenhum commit gerado pode ter
  esse texto.
- O commit seguinte aplica o patch dele por cima do de preparação.

`go test ./internal/fixture/` gera todos os cenários deste diretório. Patch que deixou de aplicar falha
ali, citando o cenário, antes de chegar ao CI.

## `scenario.json`

```json
{
  "name": "n1-basic",
  "assumption": "caso básico: loop no service, query no repository",
  "commits": [{"label": "referencia"}, {"label": "regressao", "patch": "regressao.patch"}],
  "answer": {
    "cause": "n_plus_one",
    "function": "<módulo>/internal/orders.(*Service).OrdersWithItems",
    "file": "demo-app/internal/orders/service.go",
    "marker": " // rootline:answer",
    "commit": "regressao"
  },
  "symptom": {
    "service": "demo-app", "method": "GET", "route": "/customers/{id}/orders",
    "route_source": "span_name", "bad": "regressao", "baseline": "referencia"
  },
  "load": {
    "requests": 200, "rate_per_second": 50,
    "routes": [{"route": "/customers/{id}/orders", "weight": 1}]
  }
}
```

- **`symptom`**: o que a análise recebe. `route` é obrigatório e é a rota analisada; ela tem de estar
  entre as rotas da carga. `bad` e `baseline` são rótulos de commit (`baseline` vazio = sem referência).
- **`load`**: opcional. Sem o bloco, valem 200 requisições, 50 por segundo, uma rota
  `/customers/{id}/orders` com peso 1. Com várias rotas, cada uma se repete `weight` vezes num ciclo, na
  ordem declarada; a requisição `i` vai para `ciclo[i mod tamanho]` e `{id}` vira `i mod 20 + 1`. Não há
  aleatoriedade. A rota do sintoma recebe `requests × peso / soma dos pesos` requisições.
- **`answer`**: o gabarito. A linha não é digitada: o gerador acha a linha que termina com `marker` no
  commit `commit`, remove o marcador e grava a linha.

## Gerar, gravar e pontuar todos os cenários

A gravação precisa de Docker. A pontuação não.

```sh
go build -o bin/rootline-eval ./cmd/rootline-eval

# só gerar o repo de fixture de um cenário
bin/rootline-eval generate --scenario eval/fixtures/scenarios/n1-basic --source . --out /tmp/n1-gen

# gravar todos os cenários, um diretório por cenário
for dir in eval/fixtures/scenarios/*/; do
  name=$(basename "$dir")
  bin/rootline-eval record --scenario "$dir" --source . --out "/tmp/recordings/$name"
done

# pontuar todas as gravações e comparar com o placar (sai com 1 se houver regressão)
bin/rootline-eval score --recordings /tmp/recordings --placar eval/placar.json

# regravar o placar (só com justificativa no PR)
bin/rootline-eval score --recordings /tmp/recordings --placar eval/placar.json --update
```

O placar guarda, em cada modo, o resultado de cada cenário (`scenarios`) e o agregado derivado deles.
Uma métrica que era acerto e virou erro em qualquer cenário é regressão, mesmo que o agregado não mude.
Cenário novo não é regressão; cenário que sumiu é.
