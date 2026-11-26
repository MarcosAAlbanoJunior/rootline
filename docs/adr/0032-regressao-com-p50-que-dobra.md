---
id: ADR-0032
title: Marcar como Regressão o p50 que dobra com 2 ms a mais e explicar a Causa numa operação sem Regressão
status: Aceito
date: 2026-11-25
summary: Substitui o 0030 e mantém o resto dele (teto, sumiço, symptoms, regressão generalizada, Carga desigual). A latência regride também com p50 ≥ 2x e +2 ms, com o p50 da versão ruim em pelo menos 3 ms. A symptom_regression diz qual limiar faltou, e uma Causa sem Regressão de latência ganha um aviso. Validado em 19 gravações das rodadas 2 e 3 e no placar.
modules: [analysis, cmd/rootline, adapter/textreport]
---

# 0032. Marcar como Regressão o p50 que dobra com 2 ms a mais

## Contexto

O ADR-0030 definiu a Regressão de latência como p50 ≥ 1,5x ou p95 ≥ 2x, nos dois casos com pelo
menos 5 ms a mais. Na rodada 3 do piloto (`docs/spikes/2026-11-24-piloto-apps-reais.md`), nos apps
02 e 03, um N+1 real ficou fora da regra. O `symptoms` marcou "no" e não sugeriu nada, e o `analyze`
achou o N+1 com confiança alta enquanto a `symptom_regression` dizia "no latency regression".

A issue (#66) supôs que o p95 da referência vetava a regressão. Não veta: a regra é um OU, e o p95
estável ou ruidoso só deixa de salvar o caso. O que barra é o **piso de 5 ms do ramo do p50**:

| Gravação (rodada 3) | p50 (ms) | Razão | Diferença | p95 (ms) |
|---|---|---|---|---|
| app-02, extrato | 1,41 → 6,36 | 4,52x | 4,96 ms | 868 → 133 |
| app-03, `GET /api/v1/books` | 1,06 → 4,01 | 3,80x | 2,96 ms | 4,9 → 4,9 |
| app-03-guia, `GET /api/v1/books` | 0,87 → 3,74 | 4,32x | 2,87 ms | 1,1 → 4,4 |

O piso falha no caso mais comum. Em 4 das 5 gravações com N+1, o p50 subiu menos de 5 ms. O
app-01-solo (+4,76 ms) só regrediu porque o p95 foi de 1,3 para 7,7 ms.

O que o piso segura nas 19 gravações guardadas (7 da rodada 2 e 12 da rodada 3):
- Entre as operações sem Regressão e com p50 ≥ 1,5x, a maior diferença é de **0,58 ms** (app-02,
  1,58x). GETs abaixo de 1 ms chegam a 1,95x (app-08 da rodada 2).
- Entre as operações com p50 ≥ 2x e menos de 2 ms a mais, todas já regridem pelo p95: as do app-10
  (até 0,84 ms, CPU) e o `POST /v1/logs` do app-06 da rodada 3 (2,59x e +1,90 ms, contenção).

Quando o `analyze` acha uma Causa numa operação sem Regressão, o Resultado não liga uma coisa à outra.
Com a regra nova, isso ainda acontece: no app-03, `/api/v1/search` e `/api/v1/books/{bookID}/related`
dão o N+1 certo em `attachAuthors` com p50 de só 1,3x, porque compartilham a função com a rota
principal.

## Decisão

**Regressão.** Uma operação de entrada regrediu entre a referência e a versão ruim quando as duas
versões têm pelo menos 30 traces dela e:
- **latência**, em qualquer um dos três ramos:
  - p50 ≥ 1,5x, com pelo menos 5 ms a mais;
  - **p50 ≥ 2x, com pelo menos 2 ms a mais e o p50 da versão ruim em pelo menos 3 ms** (novo);
  - p95 ≥ 2x, com pelo menos 5 ms a mais;
- **erro:** a taxa de erro sobe pelo menos 1 ponto percentual e pelo menos dobra.

O ramo novo exige a mesma razão do p95 (2x) e deixa intacta a faixa de 1,5x a 2x, onde fica quase
todo o ruído. O piso de 2 ms saiu destas gravações, não de princípio: fica cerca de 3x acima do maior
ruído visto e abaixo do menor alvo (2,87 ms).

O mínimo de 3 ms no p50 da versão ruim deixa de fora a operação que continua extremamente rápida
depois de piorar. Sem ele, um health check que vai de 0,6 para 2,7 ms sob contenção (4,5x, +2,1 ms)
regrediria, e a sugestão do `symptoms`, que fica com a maior razão de p50, poderia apontá-lo no lugar
da operação que ficou lenta: abaixo de 1 ms, qualquer oscilação vira uma razão grande. O mínimo fica
abaixo do menor alvo (3,74 ms, app-03-guia). O corte vai no p50 da versão ruim, não no da referência:
o app-03-guia parte de 0,87 ms e é um N+1 real.

O tipo (latência, erro ou os dois) acompanha a Regressão. A regra é uma função só do domínio, usada
pela Verificação `symptom_regression` (ADR-0027), pelo `rootline symptoms` e como pré-condição do
detector de forma (ADR-0031). Os limiares são constantes, sem flag. O `analyze` continua analisando
só latência (ADR-0005), e a Regressão de erro só é relatada.

**Causa numa operação sem Regressão.**
- O status e a confiança não mudam. A confiança de uma Candidata mede a evidência dela, não a
  latência, e a Causa pode estar certa numa operação que só piorou um pouco.
- Sem Regressão de latência, a `symptom_regression` diz qual limiar faltou em cada ramo, com a
  razão e a diferença. Por exemplo: `p50 1.30x (+2.42 ms): under 1.5x and under 2x; p95 0.48x (-15.96 ms): under 2x`.
- Quando a `symptom_regression` rodou, não há Regressão de latência e há Causa, o Resultado ganha um
  aviso em `warnings`. O aviso diz que os detectores leem a forma dos traces, não a latência: a causa
  está no código da operação, mas pode não explicar a lentidão procurada, e o `rootline symptoms`
  mostra se outra operação regrediu.
- Sem referência ou sem amostra mínima, não há aviso: a `symptom_regression` já diz que não julgou.
- O `schema_version` não sobe, porque são só extensões.

**Mantido do ADR-0030, sem mudança:**

**Teto.** A versão ruim de uma operação tem um teto quando pelo menos 20% dos traces dela ficam a
2% da duração máxima da operação naquela versão. Esses traces provavelmente foram cortados por um
timeout.

**Sumiço.** O Rootline não infere requisições que não viraram span. Ele relata a razão de n entre
as versões e a destaca abaixo de 50%, dizendo que pode ser requisição presa ou carga diferente.

**`rootline symptoms`.**
```
rootline symptoms --traces <file.jsonl> [--service <name>] [--repo <dir>]
                  [--baseline-version <sha>] [--bad-version <sha>] [--format text|json]
```

Inferência quando faltam flags:
- **Serviço:** com um serviço só no arquivo, ele é usado.
- **Versões:** com exatamente duas versões, a referência é a anterior no histórico do git quando
  `--repo` é dado, como na referência implícita do ADR-0005. Sem `--repo`, é a que aparece primeiro
  no tempo, e a saída avisa que a ordem veio do tempo.
- **Ambiguidade:** com mais de duas versões, ou sem `service.version`, o comando falha, listando as
  versões com o n de traces e a faixa de tempo de cada uma.

Saída, uma linha por operação de entrada: n de cada versão e a razão de n; p50 e p95 de cada versão;
taxa de erro de cada versão; tipo da Regressão; marca de teto; início da regressão. As operações com
Regressão vêm primeiro, pela maior razão de p50. Quando há operação sugerida, sai no fim um comando
`rootline analyze …` pronto, com ela e as versões.

**Regressão generalizada.** Quando pelo menos 75% das operações com amostra mínima regridem, a
saída diz que isso costuma indicar um recurso compartilhado saturado (pool, lock, CPU).

**Operação sugerida.**
- **Fora da regressão generalizada:** a sugerida é a de maior razão de p50.
- **Na regressão generalizada:** só há sugestão quando uma operação destoa, isto é, quando a maior
  razão de p50 é pelo menos 10x a segunda maior.
- **Sem operação que destoe:** não há sugestão nem comando pronto. A tabela traz, por operação, o
  **início da regressão** (o momento do 3º trace da versão ruim acima da p95 da referência, contado
  do primeiro trace da versão ruim) e o **sumiço** (a razão de n).
- **Com Carga desigual, nunca há sugestão.**
- A sugestão é sempre rotulada como sugestão, nunca como origem.

**Carga desigual.** A razão de n mediana é a mediana da razão de n entre as operações com amostra
mínima. Com pelo menos 3 dessas operações, quando ela fica fora de [0,8; 1,25], as duas gravações
não tiveram carga comparável: o `symptoms` não sugere operação nem imprime o `analyze` pronto, diz
por quê, com a razão de n mediana, e mantém o início e o sumiço na tabela. Com menos de 3 operações,
a carga não é julgada: com duas, uma operação regredida que perdeu traces para requisições presas
decidiria a mediana sozinha.

**Abaixo da amostra mínima.** Uma operação com amostra mínima na referência, mas não na versão
ruim, continua na lista, marcada com o sumiço. Ela não concorre à Regressão nem à sugestão.

## Alternativas consideradas

- **Tratar o p95 ruidoso da referência (ignorá-lo, ou trocá-lo por outro percentil).** É o que a
  issue sugeria. Não muda nenhuma das 19 gravações, porque o p95 não veta nada: a regra é um OU.
  Perdeu.
- **Baixar o piso do ramo de 1,5x para 2 ms.** Dá o mesmo resultado nas gravações, mas passa a
  marcar 4 → 6 ms (1,5x, +2 ms), a faixa onde fica o ruído, sem nenhum caso que peça isso. Perdeu.
- **p50 ≥ 3x com 2,5 ms a mais.** Também acerta os três alvos, mas fica ajustada ao app-03 (3,80x e
  2,87 ms), sem margem. Perdeu.
- **Exigir p50 da referência ≥ 1 ms no ramo novo.** Também barra a operação extremamente rápida,
  mas perde o app-03-guia (0,87 ms), e o app-03 (1,06 ms) fica no limite. Perdeu.
- **Ramo novo sem mínimo no p50 da versão ruim.** Deixa entrar operações que continuam abaixo de
  3 ms, onde a razão de p50 é grande por natureza e pode ganhar a sugestão. Perdeu.
- **Piso de 1 ms.** Cobriria a medição sem tracing do gabarito do app-02 (2,4x, +1,1 ms), mas a
  margem até o ruído visto (0,67 ms) ficaria abaixo de 2x. Perdeu por enquanto.
- **Baixar a confiança da Causa sem Regressão.** Puniria o N+1 certo de `/search` e `/related` no
  app-03. Perdeu.
- **Não emitir Causa sem Regressão.** Viraria abstenção indevida e contradiz o ADR-0027, em que os
  detectores rodam mesmo sem Regressão. Perdeu.
- **Status novo para a Causa sem Regressão.** O ADR-0027 já descartou mudar o conjunto de status.
  Perdeu.

## Consequências

- Um N+1 numa operação rápida passa a aparecer como Regressão no `symptoms` e é sugerido. Nas
  gravações, isso vale para os apps 02, 03 e 03-guia.
- O detector de forma (ADR-0031) passa a rodar em regressões menores. A guarda dele continua de 5 ms
  na média por trace, então nada novo é promovido. O p50 e a guarda deixam de andar juntos: uma
  Regressão pelo ramo novo quase sempre fica abaixo da guarda, e a `trace_shape` diz isso.
- Uma operação que continua abaixo de 3 ms depois de piorar só regride pelo p95.
- Uma regressão real de 1 a 2 ms com o p95 estável continua de fora. O aviso e o limiar que faltou
  na `symptom_regression` tornam isso visível quando o `analyze` acha a causa mesmo assim.
- Os limiares saíram de 10 apps com cargas de taxa fixa. Um caso novo que peça ajuste vira ADR novo.
- Tudo que o ADR-0030 dizia sobre a regressão generalizada e a Carga desigual continua valendo.

## Como validar

- **Testes unitários no domínio:**
  - o ramo novo nos limites: 2x exato com +2 ms, logo abaixo de 2x e logo abaixo de 2 ms;
  - o mínimo do p50 da versão ruim: 3 ms exatos regride, logo abaixo de 3 ms não;
  - o formato do app-03: p50 1,06 → 4,01 ms com p95 estável;
  - 2,9x com +1,9 ms não regride (no lugar do caso de 1 → 3 ms, que passa a regredir);
  - o limiar que faltou na `symptom_regression`, um caso por ramo;
  - o aviso: presente com Causa e sem Regressão de latência, ausente com Regressão, ausente sem Causa
    e ausente sem referência ou sem amostra mínima.
- **Gravações guardadas.** `symptoms` e `analyze` em toda operação com amostra mínima das 19
  gravações das rodadas 2 e 3, com as versões na ordem do git. Pronto quando:
  - só as 3 operações-alvo ganham Regressão, e a sugestão passa de nenhuma para a rota do gabarito
    nelas, enquanto as outras 16 gravações ficam iguais;
  - status, Causa, linha e Candidatas ficam iguais em todas as operações;
  - a `trace_shape` passa a rodar nas 3 e não promove nada;
  - o aviso aparece só onde há Causa sem Regressão de latência (pelo protótipo, `/search` e
    `/related` no app-03 e no app-03-guia).

  O resultado é registrado no spike do piloto como rodada 5.
- **Placar:** `rootline-eval score` sem `--update` igual ao `eval/placar.json`, e o gate
  (`scripts/check.sh`).
- **Protótipo (2026-11-25), só com o ramo novo:** nas 19 gravações, a mudança no `symptoms` e na
  `trace_shape` foi a descrita acima, sem nenhuma outra mudança no `analyze`, e o placar ficou igual
  ao da `main` em todos os cenários (Causa 10/10, falso positivo 0/3, alucinação 0/2, abstenção
  indevida 0/9).
