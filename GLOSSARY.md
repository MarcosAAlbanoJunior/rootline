# Glossário do Rootline

Cada termo indica o identificador usado no código (código em inglês, docs em português; ADR-0010).

## Abstenção indevida
Resultado "inconclusivo" num cenário cujo gabarito tem resposta. Reportada sempre ao lado da
métrica de alucinação.
- No código: `UnwarrantedAbstention` (`unwarranted_abstention` no placar)


## Alvo da causa
O "o quê" de uma causa além da função, quando o tipo tem um e ele vem de um campo estruturado da
evidência. No índice faltando, é a tabela do nó do plano; no trabalho a mais por requisição e na
chamada externa mais lenta, é a dependência da divisão do tempo; no N+1, não há alvo. Entra na métrica
Causa junto com o tipo e a função. Um alvo que só existe em texto (ex.: a coluna dentro do filtro)
fica como evidência, fora da métrica.
- Relacionados: [Índice faltando](#índice-faltando), [Gabarito](#gabarito), [ADR-0026](docs/adr/0026-pontuacao-com-alvo-da-causa.md)

## Amostragem declarada
A política de amostragem do backend de traces, informada na configuração do Rootline: `none`, `head`
com a taxa, ou `tail`. Com `tail`, a latência não é comparada e só sai o sinal estrutural (ADR-0034).
- Evitar: "amostragem detectada"; o Rootline não descobre a política pelos dados.
- Relacionados: [Regressão](#regressão), [Sinal antecipado](#sinal-antecipado)

## Candidata
Hipótese de causa raiz emitida por um detector na fase 1, com suas evidências e um score determinístico.
- Evitar: "suspeita", "hipótese" (solto), "achado" (achado é o resultado final).
- Relacionados: [Detector](#detector), [Evidência](#evidência)
- No código: `Candidate`

## Carga desigual
Quando as duas versões não tiveram carga comparável: a mediana da razão de taxa (traces por minuto
da janela de cada versão) entre as operações com amostra mínima (pelo menos 3) fica fora de
[0,8; 1,25] (ADR-0038). Pode ser carga diferente ou requisições presas. O `rootline symptoms` não
sugere operação.
- Relacionados: [Regressão](#regressão), [Gravação](#gravação)

## Cenário
Sequência de commits num repo de fixture gerado por script (referência, regressão e distratores),
com dados, carga e gabarito reprodutíveis. Unidade da avaliação.
- Relacionados: [Gabarito](#gabarito), [ADR-0004](docs/adr/0004-cenarios-de-avaliacao-como-commits-reais.md)
- No código: `Scenario`

## Chamada externa mais lenta
Causa em que uma dependência que não é de banco (HTTP, RPC) explica pelo menos 80% da diferença da
divisão do tempo, com o tempo por chamada pelo menos dobrado e a presença quase igual. O alvo é a
dependência. A localização é de nível 2, porque o span não tem pilha da aplicação.
- Evitar: "dependência lenta" (solto), que também descreve um banco mais lento, que nunca vira Causa.
- Relacionados: [Divisão do tempo](#divisão-do-tempo), [Dependência](#dependência), [ADR-0031](docs/adr/0031-detector-de-forma-do-trace.md)
- No código: tipo de causa `slow_call`

## Conjunto reservado
Cenários da avaliação escritos depois de congelar os detectores e nunca usados para ajustá-los.
- Evitar: "test set", "holdout" nos docs.
- No código: `ReservedSet`

## Correlação
Relação entre um commit e uma candidata, por tipo de toque: pilha, query, schema ou
configuração/dependências (peso baixo). Cada toque vira uma evidência; a falta de toque só reduz o score.
- Relacionados: [ADR-0006](docs/adr/0006-correlacao-entre-candidata-e-commit.md)
- No código: `Correlation`, com `Touch` (`StackTouch`, `QueryTouch`, `SchemaTouch`, `ConfigTouch`)

## Dependência
O destino de um span de cliente na divisão do tempo: o sistema do banco, o `host:porta` do HTTP, o
serviço RPC ou, por último, o nome do span. A operação (query normalizada, método HTTP) é o detalhe
dentro dela.
- Evitar: "serviço externo" (o banco também é dependência), "endpoint".
- Relacionados: [Divisão do tempo](#divisão-do-tempo), [ADR-0031](docs/adr/0031-detector-de-forma-do-trace.md)
- No código: `Dependency`

## Detector
Regra determinística da fase 1 que examina um trace e emite candidatas de um tipo de regressão (ex.: N+1).
- Evitar: "analisador", "heurística" (soltos).
- No código: `Detector`

## Distrator
Código ou commit de um cenário que parece causa e não é (ex.: paginação em lotes pequenos, que
parece N+1). Um distrator de código é declarado no `scenario.json` e marcado com
`// rootline:distractor`. O gabarito guarda a linha dele no commit ruim. Uma candidata que casa com
ele conta como falso positivo de detector.
- Relacionados: [Falso positivo de detector](#falso-positivo-de-detector), [Gabarito](#gabarito), [ADR-0022](docs/adr/0022-distrator-declarado-no-cenario.md)
- No código: `Distractor`

## Divisão do tempo
A diferença da média por trace de uma operação entre a referência e a versão ruim, repartida entre
as dependências e o tempo próprio. Deixa de fora os traces no teto e os 5% mais lentos de cada versão.
Sai como evidência em todo Resultado com Regressão de latência e só vira Candidata em dois mecanismos:
chamada externa mais lenta e trabalho a mais por requisição. É o que o detector de forma calcula.
- Evitar: "forma" (solto), que colide com a [Forma de paginação](#forma-de-paginação); "breakdown" nos docs.
- Relacionados: [Dependência](#dependência), [Tempo próprio](#tempo-próprio), [Presença](#presença), [ADR-0031](docs/adr/0031-detector-de-forma-do-trace.md)
- No código: evidência `time_breakdown`, Verificação `trace_shape`

## Evidência
Fato verificável com ID estável que sustenta uma candidata ou um resultado: span, query normalizada,
contagem, trecho de código (arquivo:linha), trecho de diff, plano de execução.
- Relacionados: [Inconclusivo](#inconclusivo)
- No código: `Evidence`

## Índice de código
Mapa do código de uma versão, construído no SHA dela (lido do git): intervalo de linhas por função,
loops, rotas → handlers e strings SQL. Na v1, `go/parser` + `go/ast`.
- Evitar: "índice" solto, que também pode ler como o índice do banco do [Índice faltando](#índice-faltando).
- Relacionados: [ADR-0007](docs/adr/0007-go-com-cgo-isolado-e-indice-go-ast.md)
- No código: `CodeIndex`


## Índice faltando
Causa em que uma query lê uma tabela grande inteira para devolver poucas linhas, porque nenhum índice
cobre o filtro. É reconhecida só pelo plano de execução: Seq Scan (ou Parallel Seq Scan) com filtro
seletivo numa tabela grande, sem tempo no critério. O alvo é a tabela.
- Evitar: "índice" solto, porque no Rootline "índice" sozinho também nomeia o índice de código.
  Use "índice faltando" ou "índice do banco" para este sentido.
- Relacionados: [Plano de execução](#plano-de-execução), [Alvo da causa](#alvo-da-causa), [ADR-0023](docs/adr/0023-indice-faltando-pelo-plano-de-execucao.md)
- No código: tipo de causa `missing_index`

## Inconclusivo
Resultado obrigatório quando nenhuma conclusão pode ser sustentada por evidências válidas, inclusive
quando o LLM cita um ID de evidência inexistente ou não cita nenhum. Pode vir com observações.
- Relacionados: [Observação](#observação)
- No código: `Inconclusive`

## Falso positivo de detector
Detector que classifica errado um distrator (ex.: paginação como N+1). Separado da alucinação. Conta
os cenários com distrator em que alguma candidata do Resultado casa com ele: arquivo:linha no nível 1,
função no nível 2. Para esta métrica, menos é melhor, e o portão do placar reprova quando ela sobe.
- Relacionados: [Distrator](#distrator), [ADR-0026](docs/adr/0026-pontuacao-com-alvo-da-causa.md), [ADR-0022](docs/adr/0022-distrator-declarado-no-cenario.md)
- No código: `DetectorFalsePositive` (`detector_false_positive` no placar)

## Forma de paginação
Ocorrência de queries repetidas que é leitura paginada, não N+1: a query tem `LIMIT`, as linhas são
páginas cheias iguais (k ≥ 2) seguidas de uma página menor (inclusive vazia), e não há "+1". O
detector a descarta e registra a observação `pagination_shape:`.
- Evitar: "paginação" (solto) quando o sentido é a regra do detector.
- Relacionados: [Detector](#detector), [Observação](#observação), [ADR-0021](docs/adr/0021-forma-de-paginacao-nao-e-n-mais-1.md)
- No código: `paginationShape`

## Gabarito
Resultado esperado de um cenário: causa (ou inconclusivo), localização e commit por rótulo
(`referencia`, `regressao`, `distrator`), nunca por SHA. Quando o cenário tem distratores de código,
o gabarito também guarda a função, o arquivo e a linha de cada um no commit ruim. Quando o tipo de
causa tem alvo, o gabarito guarda o alvo (ex.: a tabela no índice faltando).
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
Cada tipo de causa escolhe a regra do nível 1: o N+1 procura o primeiro frame em loop; o índice
faltando usa o frame mais interno, onde a query é emitida, sem regra do loop ([ADR-0024](docs/adr/0024-localizacao-por-tipo-de-causa.md)).
- Relacionados: [Evidência](#evidência), [ADR-0002](docs/adr/0002-localizacao-de-queries-no-codigo.md)
- No código: `Location`, com `LocationLevel`

## Métrica de alucinação
Taxa de resultados que inventam um commit (no cenário sem commit) ou uma causa (no cenário sem
causa visível). Reportada separada do acerto e sempre ao lado da abstenção indevida. O cenário sem
causa visível tem o gabarito `inconclusive`.
- No código: `Hallucination` (`hallucination` no placar), `CauseInconclusive`

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

## Perfil de versão
O que o Rootline guarda de uma versão × operação de entrada no modo watch, no lugar dos traces
brutos: n, erros, uma amostra de até 1000 durações, taxa, janela, divisão do tempo e Candidatas de
N+1. Serve de referência quando os traces da versão já expiraram no backend (ADR-0040).
- Evitar: "baseline salvo", "snapshot".
- Relacionados: [Referência](#referência), [Divisão do tempo](#divisão-do-tempo)

## Placar
Resultado do modo só-detectores na `main`, versionado em `eval/placar.json`; portão dos PRs.
- Evitar: "resultado de referência" (colide com [Referência](#referência)).
- No código: `Scoreboard` (`eval/placar.json`)


## Plano de execução
Como o Postgres pretende executar uma query, obtido com `EXPLAIN (GENERIC_PLAN, FORMAT JSON)` sem
`ANALYZE`, junto com o tamanho estimado (`reltuples`) das tabelas do plano. Na avaliação, vem
gravado por versão; no uso real, viria do adapter ao vivo. É estimativa do planner, não medição.
- Evitar: "explain" solto como substantivo.
- Relacionados: [Índice faltando](#índice-faltando), [Gravação](#gravação), [ADR-0012](docs/adr/0012-explain-query-gravado-e-ao-vivo.md), [ADR-0025](docs/adr/0025-planos-gravados-por-versao.md)
- No código: porta `Explainer`; arquivo `plans.jsonl` na gravação

## Pilha da aplicação
Os frames de uma pilha de chamadas que pertencem ao módulo do serviço analisado, sem stdlib nem
bibliotecas. Capturada pelo hook de chamador.
- No código: `AppStack`

## Prazo sem amostra
O tempo depois do deploy (48 h por padrão) em que o watch desiste de esperar a amostra mínima e
publica um resumo das operações que ficaram sem comparação (ADR-0035).
- Relacionados: [Regressão](#regressão)

## Presença
A fração dos traces de uma operação (fora do teto) em que uma dependência aparece. É diferente das
chamadas por trace: chamadas a mais no mesmo trace são território do N+1.
- Relacionados: [Trabalho a mais por requisição](#trabalho-a-mais-por-requisição), [Divisão do tempo](#divisão-do-tempo)

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

## Regressão
Piora de uma operação de entrada entre a referência e a versão ruim, por uma regra única (ADR-0032, mantida no ADR-0038):
amostra mínima nas duas versões e latência (p50 ou p95) ou taxa de erro acima dos limiares. Um p50
que dobra com 2 ms a mais já conta, se a versão ruim ficar em pelo menos 3 ms, para não perder um N+1
numa operação rápida sem marcar a que continua extremamente rápida. Tem
tipo: latência, erro ou os dois.
- Evitar: "piorou" solto, sem a regra; "degradação".
- Relacionados: [Regressão generalizada](#regressão-generalizada), [Sintoma](#sintoma), [Referência](#referência)

## Regressão generalizada
Quando quase todas as operações de entrada do serviço regridem juntas (≥ 75%), sinal típico de um
recurso compartilhado saturado. Só há operação sugerida quando uma delas destoa, sempre como
sugestão, nunca como origem.
- Relacionados: [Regressão](#regressão)

## Sinal antecipado
Uma Candidata de N+1 publicada pelo watch antes da amostra mínima: aparece em pelo menos 3 traces da
versão nova, não existe na referência e está num local tocado por um commit do deploy. Sai como não
confirmada e depois é confirmada ou mantida como Causa sem Regressão, nunca retirada (ADR-0036).
- Evitar: "alerta precoce", "N+1 provisório".
- Relacionados: [Candidata](#candidata), [Regressão](#regressão), [Correlação](#correlação)

## Sintoma
Entrada da análise: serviço + operação de entrada + janela ruim (de versão única, ou filtrada por
`--bad-version`). Na v1, só latência.
- No código: `Symptom`

## Suposição do cenário
A suposição do detector ou da localização que um cenário quebra (ex.: "o loop está no frame logo acima
da query"). Obrigatória em todo cenário: cenário que não quebra nada só infla o placar.
- Relacionados: [Cenário](#cenário), `eval/README.md`
- No código: `Scenario.Assumption` (`assumption` no `scenario.json`)

## Tempo próprio
O tempo do span raiz de uma operação que não está coberto por nenhum filho direto: a duração da raiz
menos a união dos intervalos dos filhos. CPU, lock ou espera não instrumentada aparecem aqui. Na
divisão do tempo, entra como uma linha, mas nunca vira Causa.
- Relacionados: [Divisão do tempo](#divisão-do-tempo), [ADR-0031](docs/adr/0031-detector-de-forma-do-trace.md)

## Teto
Duração em que boa parte dos traces de uma operação para, na versão ruim (≥ 20% a 2% da duração
máxima), em geral por um timeout. Esses traces têm menos spans filhos do que o trabalho real.
- Relacionados: [Regressão](#regressão), [Verificação](#verificação)

## Trabalho a mais por requisição
Causa em que a presença de uma dependência sobe pelo menos 25 pontos percentuais e ela explica pelo
menos 80% da diferença da divisão do tempo, como um cache que passou a furar sempre. O alvo é a
dependência. A localização é o frame da aplicação mais interno comum às pilhas das operações que
passaram a aparecer em mais traces.
- Evitar: "query nova" (a operação pode já existir e só rodar mais vezes).
- Relacionados: [Presença](#presença), [Divisão do tempo](#divisão-do-tempo), [ADR-0031](docs/adr/0031-detector-de-forma-do-trace.md)
- No código: tipo de causa `added_work`

## Verificação
Um detector ou pré-condição avaliado na análise e relatado no Resultado, com status (rodou, pulou,
não se aplica), o fato observado e a dica do que fazer. Explica por que o Resultado é o que é,
principalmente quando ele é inconclusivo.
- Evitar: "aviso" para isso (aviso é só sobre o resultado em si); "diagnóstico".
- Relacionados: [Resultado](#resultado), [Inconclusivo](#inconclusivo)
- No código: `Check` (campo `checks` do Resultado)
