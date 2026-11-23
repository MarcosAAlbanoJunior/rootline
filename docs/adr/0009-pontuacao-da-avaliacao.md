---
id: ADR-0009
title: Pontuar a avaliação por dimensão, com denominadores explícitos e alucinação ao lado da abstenção
status: Substituído por ADR-0026
date: 2026-11-22
summary: Métricas Causa, Linha, Commit, Estrito, Alucinação, Abstenção indevida e Falso positivo de detector, sempre em contagem/porcentagem com denominador; LLM com N=5, modelo fixado, consistência e custo; portão de PR contra placar versionado; conjunto reservado especificado pelo mantenedor e implementado por sessão sem acesso aos detectores.
modules: [eval, ci]
---

# 0009. Pontuar a avaliação por dimensão

## Contexto

Os ADRs 0001, 0004 e 0008 definem os modos, os grupos de cenários, a métrica de alucinação e o
CI, mas não o que conta como acerto. Sem regras de pontuação, o número publicado pode ser
maquiado sem querer: porcentagem sem denominador, abstenção premiada, casas decimais sem sentido.

## Decisão

**Métricas**

| Métrica | Acerto quando |
|---|---|
| Causa | tipo de regressão e função corretos |
| Linha | arquivo:linha corretos (contada só com localização de nível 1) |
| Commit | rótulo certo, ou "nenhum" quando o gabarito diz nenhum |
| Estrito | Causa, Linha e Commit ao mesmo tempo (número de manchete) |
| Alucinação | commit inventado (cenário sem commit) ou causa inventada (cenário sem causa visível) |
| Abstenção indevida | "inconclusivo" num cenário que tem resposta |
| Falso positivo de detector | detector classifica errado um distrator (ex.: paginação como N+1) |

**Regras de relatório**
- Sempre contagem e porcentagem, sem casas decimais enganosas (ex.: `18/22, 82%`). Opcional:
  intervalo de confiança de Wilson.
- Cada métrica declara o denominador (quantos cenários entram).
- A métrica Linha vem com a distribuição dos níveis de localização.
- Cenários sem referência ficam fora do denominador de Commit, e o relatório diz quantos ficaram fora.
- Alucinação e Abstenção indevida aparecem sempre lado a lado: uma sem a outra premia responder
  sempre "inconclusivo".
- Falso positivo de detector é reportado separado da Alucinação.

**Modos com LLM**
- N = 5 repetições por cenário: média, desvio e consistência (fração das execuções que concordam
  com a resposta mais frequente).
- Modelo fixado no relatório (nome e digest no Ollama, ou ID exato do modelo na Anthropic),
  temperatura 0, seed quando o provedor aceitar. Cada modelo é reportado em separado.
- Custo por análise: tokens de entrada e saída e tempo de execução.

**Portão do PR (modo só-detectores)**
- O placar de referência fica num arquivo versionado (`eval/placar.json`).
- Uma queda em qualquer métrica só passa se o PR atualizar esse arquivo, com justificativa na descrição.

**Conjunto reservado**
- O mantenedor escreve a especificação dos cenários (descrição e gabarito).
- Uma sessão separada, sem acesso ao código dos detectores, implementa os cenários no script de fixture.
- Roda só em release e é reportado em separado.

## Alternativas consideradas

- **Só o acerto estrito.** Esconde onde o sistema erra (causa certa e linha errada, por exemplo).
- **Só porcentagem.** Com 22 cenários, "81,82%" sugere uma precisão que não existe.
- **Alucinação sozinha.** Premia a abstenção.

## Consequências

- O relatório da avaliação é uma tabela por modo × grupo de cenários × métrica, com denominadores.
- O arquivo do placar passa a fazer parte da revisão de PRs que mexem em detectores.
- A separação do conjunto reservado precisa ser aplicada nos dois sentidos (questão em aberto).

## Como validar

- Testes unitários da pontuação: cada métrica com casos de acerto, erro, fora do denominador e abstenção.
- O CI falha quando o só-detectores cai sem atualização do placar.

## Isolamento do conjunto reservado

> Questão em aberto fechada em 2026-11-22, na mesma entrevista de arquitetura.

O isolamento vale nos dois sentidos e é verificável:
- **Desenvolvimento não vê o reservado:** a especificação e os scripts ficam num repo privado
  separado, acessado só pelo job de release via token secreto.
- **O reservado não vê os detectores:** a sessão que implementa os cenários trabalha numa cópia
  sem `internal/detector/` e **sem `.git`** (exportada com `git archive` só das pastas necessárias:
  `demo-app/`, `hook/`, `eval/fixtures/`). Um worktree ou sparse checkout não basta, porque o
  histórico git continua permitindo ler os detectores com `git show`.
