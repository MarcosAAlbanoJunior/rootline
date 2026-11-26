---
id: ADR-0031
title: Dividir o tempo da regressão por dependência e só promover a Candidata dois mecanismos que o trace sustenta
status: Aceito
date: 2026-11-25
summary: O detector de forma reparte a diferença da média por trace entre as dependências e o tempo próprio (sempre como evidência) e só promove a Candidata a chamada externa mais lenta (slow_call) e o trabalho a mais por requisição (added_work). Banco mais lento e tempo próprio maior nunca viram Causa.
modules: [analysis, detector, evaluation, demo-app]
---

# 0031. Dividir o tempo da regressão por dependência e só promover dois mecanismos

## Contexto

No piloto em apps reais (`docs/spikes/2026-11-24-piloto-apps-reais.md`), dois apps regrediram sem
mudar o número de queries, e o Rootline deu `inconclusive` nas rodadas 2 e 3:
- **app-05** (cache sempre furado): as agregações rodavam só em parte das requisições e passaram a
  rodar em todas;
- **app-08** (dependência HTTP lenta): o tempo foi para uma chamada HTTP de saída.

O item 4 do roadmap supunha um "span novo". Os traces mostram outra coisa. Este é um levantamento
das gravações guardadas das rodadas 2 e 3, em cada operação com Regressão (ADR-0030), na ordem do git:

| Gravação | Para onde foi a diferença da média | Presença da dependência |
|---|---|---|
| app-08 (r2 e r3) | `http:moderation:8090`, 99% e 101%; 3,5 → 145 ms por chamada | 100% → 100% |
| app-05 (r2 e r3) | `db:postgresql`, 102% e 100% | 53% → 100% e 56% → 100% |
| app-06 (r2 e r3) | `db:mongodb`, 90% a 100% (índice faltando no Mongo) | 100% → 100% |
| app-09 (r2 e r3) | `db:mysql`, 100% (pool esgotado) | 100% → 100% |
| app-10 (r2 e r3) | tempo próprio, 85% a 97% na rota certa (CPU) | – |
| app-04 (r2 e r3) | tempo próprio, 75% a 100% (lock) | 100% → 90% |
| app-01 (N+1) | `db:postgresql`, 89% a 100%, 4 → 104 queries por trace | 100% → 100% |

A conclusão: "uma dependência explica a maior parte da diferença" acontece em quase todo app. Se
virasse Causa, os apps 04, 06, 09 e 10 ganhariam uma Causa errada. O que separa os dois alvos é o
**mecanismo**.

Também se viu que:
- a média de uma versão pode andar ao contrário do p50 quando a distribuição é bimodal (app-06 r3:
  p50 de 2,7 para 17,1 ms e média de 288 para 142 ms);
- duas gravações da rodada 2 gravaram a versão ruim primeiro, e só a ordem do git as lê certo;
- o hook anota só spans do pgx (ADR-0029), e a chamada HTTP do app-08 não tem pilha.

O ADR-0027 deixou "para onde foi o tempo" para este detector.

## Decisão

O **detector de forma** roda sempre que a operação tem Regressão de latência. Ele devolve a
**divisão do tempo** como evidência e só promove a Candidata dois mecanismos.

**Dependência.** O destino de um span de cliente: `db.system.name`/`db.system` para banco,
`server.address:port` para HTTP, `rpc.service` para RPC e, por último, o nome do span. O **tempo
próprio** do serviço é a duração da raiz menos a união dos intervalos dos filhos diretos, e entra
como mais uma linha. A operação (query normalizada, método HTTP ou nome do span) é o detalhe dentro
da dependência.

**Métrica.**
- O tempo de uma dependência num trace é a união dos intervalos dos spans dela, para que spans
  concorrentes não contem duas vezes, e é zero quando ela não aparece.
- A divisão usa a média por trace, porque a média é aditiva e a fração explicada passa a ter sentido.
- A média deixa de fora os traces no teto (ADR-0030) e os 5% mais lentos de cada versão.
- A **presença** (fração dos traces com a dependência) usa todos os traces fora do teto.
- A fração explicada é a diferença da média da dependência sobre a diferença da média da raiz,
  limitada a 1.
- **Guarda:** só há promoção quando a diferença da média da raiz é positiva e de pelo menos 5 ms.

**Mecanismos promovidos a Candidata:**

| Tipo | Quando |
|---|---|
| `slow_call` (chamada externa mais lenta) | Dependência que não é de banco (HTTP, RPC); explica ≥ 80% da diferença; tempo médio por chamada ≥ 2x; presença varia menos de 25 pontos percentuais |
| `added_work` (trabalho a mais por requisição) | Qualquer dependência; presença sobe ≥ 25 pontos percentuais; explica ≥ 80% da diferença |

A frequência do `added_work` é a presença no trace, não as chamadas por trace. Chamadas a mais
dentro do mesmo trace são território do N+1.

**Nunca promovidos:** banco mais lento e tempo próprio maior. Ficam na divisão do tempo, e a
Verificação diz que a origem está fora do trace (índice, lock, pool, CPU).

**Alvo** (ADR-0026): a dependência, nos dois tipos.

**Localização** (ADR-0024):
- `added_work`: nível 1, o frame da aplicação mais interno comum às pilhas das operações cuja
  presença subiu;
- `slow_call`: nível 2, rota → handler, aproximada. O span HTTP não tem pilha, e a Verificação
  `commit_correlation` diz isso.

**Ordem e confiança.**
- As Candidatas de N+1 e índice faltando vêm sempre antes das de forma, qualquer que seja o score.
- A Causa só sai de uma Candidata de forma quando não há Candidata específica.
- A correlação com commit só reordena dentro da mesma classe.
- Confiança `medium`, fixa.
- Score = fração explicada.

**Contrato.**
- Evidência `time_breakdown`: uma linha por dependência, com tempo médio, presença e chamadas por
  trace nas duas versões, fração explicada e as operações.
- Verificação `trace_shape`: o mecanismo promovido, ou por que nada foi promovido.
- O `schema_version` não sobe, porque são só extensões.

**Fora desta decisão:** anotar spans de cliente que não são de banco com a pilha da aplicação.
Isso muda o hook e fica com o item 6 do roadmap.

## Alternativas consideradas

- **Toda dependência dominante vira Causa.** Cobre mais casos, mas dá Causa errada em 4 dos 10 apps
  do piloto (banco lento por índice, pool ou CPU; tempo próprio por lock ou CPU). Perdeu.
- **Só Observação, sem Candidata.** Não dá falso positivo, mas não acerta nada, e o app-05 e o
  app-08 continuam sem resposta. Perdeu. A divisão do tempo sempre presente aproveita a parte boa.
- **Unidade por query ou por nome de span.** No app-05, cada uma das ≈5 agregações explica cerca de
  20%, e nenhuma passa num critério de maioria. Todo span do otelsql se chama `sql.conn.query`.
  Perdeu para a dependência, com a operação como detalhe.
- **Mediana por trace.** Robusta à cauda, mas não aditiva, e zera uma dependência presente em menos
  da metade dos traces. Perdeu.
- **Média interquartil.** Robusta à cauda, mas, no app-05, descarta a metade rápida (os acertos do
  cache) e distorce a mistura entre traces com e sem a dependência, que é o sinal do `added_work`.
  Perdeu.
- **Estender o hook a todo span de cliente agora.** Daria linha e commit ao `slow_call` e cobriria
  parte do item 6. Custa versão nova do hook, receita nova no guia e regravação, porque as 18
  gravações não têm essa pilha. Perdeu neste lote e fica como item próprio.

## Consequências

- O app-05 e o app-08 passam a ter Causa, e todo Resultado com Regressão ganha a divisão do tempo
  como pista, inclusive os `inconclusive`.
- O `slow_call` sai sem linha e sem commit até o hook anotar spans de cliente HTTP.
- Os limiares (80%, 25 pp, 2x, 5%) saíram de princípio, não de ajuste. Os positivos ficaram em 99%
  ou mais e +44 pp, os negativos sem dependência HTTP e com no máximo 10 pp de variação de presença.
  A amostra de `slow_call` é um app só. Caso novo que peça ajuste vira ADR novo.
- Uma distribuição bimodal pode fazer a média não acompanhar o p50, e o detector se abstém. O erro
  desse lado é abstenção, não Causa errada.
- O eval ganha o primeiro serviço extra no compose (stub HTTP), com custo de gravação no CI.

## Como validar

- Testes unitários no domínio: dependência por tipo de span, união de intervalos, tempo próprio,
  corte dos 5%, guarda, cada mecanismo nos limiares, os dois casos nunca promovidos e a ordem por
  classe.
- Placar com três cenários novos, cada um com a sua suposição: `call-slow` (`slow_call`),
  `cache-miss` (`added_work`, com distrator: uma query que roda sempre e fica pouco mais lenta) e
  `cpu-handler` (negativo, `inconclusive`). Pronto quando o placar ficar 10/10 e os 7 cenários atuais
  não mudarem.
- Nas 19 gravações guardadas das rodadas 2 e 3 (as 18 do ADR-0030 mais o `app-03-guia`), com as
  versões na ordem do git:
  - Candidata de forma certa no app-05 e no app-08, nas duas rodadas;
  - nenhuma Candidata de forma nas outras 15.
  Os gabaritos lacrados do laboratório são conferidos só depois disso.
