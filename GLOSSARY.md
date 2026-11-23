# Glossário do Rootline

Cada termo indica o identificador usado no código (código em inglês, docs em português; ADR-0010).

## Abstenção indevida
Resultado "inconclusivo" num cenário cujo gabarito tem resposta. Reportada sempre ao lado da
métrica de alucinação.
- No código: `UnwarrantedAbstention`

## Candidata
Hipótese de causa raiz emitida por um detector na fase 1, com suas evidências e um score determinístico.
- Evitar: "suspeita", "hipótese" (solto), "achado" (achado é o resultado final).
- Relacionados: [Detector](#detector), [Evidência](#evidência)
- No código: `Candidate`

## Cenário
Sequência de commits num repo de fixture gerado por script (referência, regressão e distratores),
com dados, carga e gabarito reprodutíveis. Unidade da avaliação.
- Relacionados: [Gabarito](#gabarito), [ADR-0004](docs/adr/0004-cenarios-de-avaliacao-como-commits-reais.md)
- No código: `Scenario`

## Conjunto reservado
Cenários da avaliação escritos depois de congelar os detectores e nunca usados para ajustá-los.
- Evitar: "test set", "holdout" nos docs.
- No código: `ReservedSet`

## Correlação
Relação entre um commit e uma candidata, por tipo de toque: pilha, query, schema ou
configuração/dependências (peso baixo). Cada toque vira uma evidência; a falta de toque só reduz o score.
- Relacionados: [ADR-0006](docs/adr/0006-correlacao-entre-candidata-e-commit.md)
- No código: `Correlation`, com `Touch` (`StackTouch`, `QueryTouch`, `SchemaTouch`, `ConfigTouch`)

## Detector
Regra determinística da fase 1 que examina um trace e emite candidatas de um tipo de regressão (ex.: N+1).
- Evitar: "analisador", "heurística" (soltos).
- No código: `Detector`

## Evidência
Fato verificável com ID estável que sustenta uma candidata ou um resultado: span, query normalizada,
contagem, trecho de código (arquivo:linha), trecho de diff, plano de execução.
- Relacionados: [Inconclusivo](#inconclusivo)
- No código: `Evidence`

## Índice de código
Mapa do código de uma versão, construído no SHA dela (lido do git): intervalo de linhas por função,
loops, rotas → handlers e strings SQL. Na v1, `go/parser` + `go/ast`.
- Relacionados: [ADR-0007](docs/adr/0007-go-com-cgo-isolado-e-indice-go-ast.md)
- No código: `CodeIndex`

## Inconclusivo
Resultado obrigatório quando nenhuma conclusão pode ser sustentada por evidências válidas, inclusive
quando o LLM cita um ID de evidência inexistente ou não cita nenhum. Pode vir com observações.
- Relacionados: [Observação](#observação)
- No código: `Inconclusive`

## Falso positivo de detector
Detector que classifica errado um distrator (ex.: paginação como N+1). Separado da alucinação.
- No código: `DetectorFalsePositive`

## Gabarito
Resultado esperado de um cenário: causa (ou inconclusivo), localização e commit por rótulo
(`referencia`, `regressao`, `distrator`), nunca por SHA.
- Evitar: "resposta esperada", "expected" (soltos).
- No código: `AnswerKey`

## Gravação
Traces de um cenário salvos em OTLP JSON, com manifesto (cenário, rótulos e SHAs, seed, versão do
hook, carga, overhead). A análise roda sobre gravações, sem Docker.
- Relacionados: [ADR-0008](docs/adr/0008-traces-em-arquivo-e-gravacao-separada-da-analise.md)
- No código: `Recording`

## Hook de chamador
Pacote do Rootline instalado no serviço analisado que anexa a pilha da aplicação a cada span de banco.
Vale igualmente para todas as queries; não é um span manual. Também marca a fase do span de banco (`rootline.db.phase`: `query` ou `prepare`).
- Relacionados: [Pilha da aplicação](#pilha-da-aplicação)
- No código: módulo `hook/`, pacote `callerhook`

## Localização
Ponto do código responsável por uma evidência (função e, se possível, arquivo:linha), com nível.
O nível diz só a procedência: 1 = pilha da aplicação no span de banco (exata); 2 = rota HTTP →
handler ("aproximada", sem linha). Se o loop foi achado, quem diz é `in_loop` e os avisos (por
exemplo `loop_not_found`), não o nível: sem frame em loop, a localização continua no nível 1
([ADR-0017](docs/adr/0017-localizacao-sem-frame-em-loop.md)). A função é a declarada, sem os
sufixos sintéticos `-rangeN` (corpo de range-over-func) e `.funcN` (closure) ([ADR-0019](docs/adr/0019-funcao-declarada-no-lugar-de-nomes-sinteticos.md)).
- Relacionados: [Evidência](#evidência), [ADR-0002](docs/adr/0002-localizacao-de-queries-no-codigo.md)
- No código: `Location`, com `LocationLevel`

## Métrica de alucinação
Taxa de resultados que inventam um commit (no cenário sem commit) ou uma causa (no cenário sem
causa visível). Reportada separada do acerto.
- No código: `HallucinationRate`

## Modo de análise
Como o resultado é produzido: **só detectores** (melhor candidata pelo score), **pipeline**
(detectores + LLM, o padrão) ou **agente livre** (LLM com ferramentas e orçamento; fallback com confiança baixa).
- No código: `AnalysisMode` (`DetectorsOnly`, `Pipeline`, `FreeAgent`)

## Observação
Evidência sem causa atribuída (ex.: "1,1s de tempo próprio no handler X, sem spans filhos").
Pode acompanhar um resultado inconclusivo.
- No código: `Observation`

## Operação de entrada
O span raiz do serviço para uma requisição ou unidade de trabalho. Na v1, só HTTP (método +
`http.route`); filas e jobs entram depois como novos tipos.
- Evitar: "endpoint" no modelo de domínio (é só o caso HTTP).
- No código: `EntryOperation`

## Pai efetivo
O span sob o qual o detector agrupa as queries repetidas e procura o "+1": o pai direto do span de
query ou, quando esse pai é um wrapper genérico repetido (span `INTERNAL`, homônimo de outros irmãos,
só com filhos de banco: spans de query ou prepare, ou com `db.system.name`/`db.system`, como
`pool.acquire`), o avô. Um nível só.
- Evitar: "pai" (solto) quando o sentido é este.
- Relacionados: [Detector](#detector), [ADR-0020](docs/adr/0020-pai-efetivo-com-filhos-do-cliente-de-banco.md)
- No código: `EffectiveParent`

## Patch de preparação
Patch aplicado no commit `referencia` de um cenário para trazer o código pré-existente que ele precisa
(um pacote, uma rota), mantendo o demo-app base mínimo. De preferência só arquivos novos; nunca contém
o marcador do gabarito.
- Relacionados: [Cenário](#cenário), `eval/README.md`
- No código: `ScenarioCommit.Patch` do primeiro commit

## Placar
Resultado do modo só-detectores na `main`, versionado em `eval/placar.json`; portão dos PRs.
- Evitar: "resultado de referência" (colide com [Referência](#referência)).
- No código: `Scoreboard` (`eval/placar.json`)

## Pilha da aplicação
Os frames de uma pilha de chamadas que pertencem ao módulo do serviço analisado, sem stdlib nem
bibliotecas. Capturada pelo hook de chamador.
- No código: `AppStack`

## Resultado
O que o Rootline devolve: causa provável (ou inconclusivo), evidências citadas, correção sugerida,
modo de análise e confiança.
- Evitar: "resposta", "diagnóstico" (soltos).
- No código: `Result`

## Referência
Conjunto de traces usado para comparação: explícito (janela ou versão), implícito (versão anterior
com amostra mínima) ou ausente. Define o início do intervalo do diff.
- Evitar: "baseline" nos docs em português.
- No código: `Baseline`

## Sintoma
Entrada da análise: serviço + operação de entrada + janela ruim (de versão única, ou filtrada por
`--bad-version`). Na v1, só latência.
- No código: `Symptom`

## Suposição do cenário
A suposição do detector ou da localização que um cenário quebra (ex.: "o loop está no frame logo acima
da query"). Obrigatória em todo cenário: cenário que não quebra nada só infla o placar.
- Relacionados: [Cenário](#cenário), `eval/README.md`
- No código: `Scenario.Assumption` (`assumption` no `scenario.json`)
