---
id: ADR-0026
title: Pontuar a avaliação por dimensão, com a Causa medida por tipo, função e alvo do tipo
status: Aceito
date: 2026-11-23
summary: Substitui o ADR-0009. Mesmas métricas, regras de relatório, modos com LLM, portão e conjunto reservado; a Causa passa a ser tipo + função + o alvo próprio do tipo quando ele vier de campo estruturado (tabela do plano no missing_index; nenhum alvo extra no N+1).
modules: [eval, evaluation, ci]
---

# 0026. Pontuar a avaliação por dimensão, com a Causa medida por tipo, função e alvo

## Contexto

O ADR-0009 definiu as métricas da avaliação. A Causa acertava quando o tipo de regressão e a função
estavam corretos. Com um só tipo de causa (N+1), isso bastava: a função diz onde está o loop, e não
há outro "o quê" a acertar.

O índice faltando (ADR-0023) tem um alvo além da função: a tabela que precisa do índice. Um
Resultado pode acertar o tipo e a função (o método do repository) e apontar a tabela errada, por
exemplo quando a query faz join. Pela regra do ADR-0009, isso contaria como acerto de Causa.

A tabela vem de um campo estruturado do plano (`Relation Name`). A coluna, que seria o alvo mais
preciso, só existe no texto do `Filter`, e extraí-la exige parser de SQL, em aberto no ADR-0006.

Incluir a tabela só na Causa do índice faltando, como regra especial, criaria uma régua diferente
por tipo no mesmo placar.

## Decisão

**Métricas**

| Métrica | Acerto quando |
|---|---|
| Causa | tipo de regressão, função e o alvo do tipo corretos (ver abaixo) |
| Linha | arquivo:linha corretos (contada só com localização de nível 1) |
| Commit | rótulo certo, ou "nenhum" quando o gabarito diz nenhum |
| Estrito | Causa, Linha e Commit ao mesmo tempo (número de manchete) |
| Alucinação | commit inventado (cenário sem commit) ou causa inventada (cenário sem causa visível) |
| Abstenção indevida | "inconclusivo" num cenário que tem resposta |
| Falso positivo de detector | detector classifica errado um distrator (ex.: paginação como N+1); casamento e portão no ADR-0022 |

**Alvo da causa.** É regra geral para todos os tipos: a Causa é tipo + função + o alvo próprio do
tipo, **só quando o alvo vem de um campo estruturado** da evidência, nunca de texto parseado.
- `n_plus_one`: sem alvo extra. A regra é a mesma do ADR-0009.
- `missing_index`: o alvo é a tabela do nó do plano (`Relation Name`).
- A coluna do filtro fica como evidência, fora da métrica. Ela pode entrar no alvo quando houver
  parser de SQL, por um novo ADR.
- Um tipo novo declara o seu alvo, ou declara que não tem.

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
- Uma queda em qualquer métrica só passa se o PR atualizar esse arquivo, com justificativa na
  descrição. Para o falso positivo de detector, menos é melhor, e o portão reprova quando ele sobe
  (ADR-0022).

**Conjunto reservado**
- O mantenedor escreve a especificação dos cenários (descrição e gabarito).
- Uma sessão separada, sem acesso ao código dos detectores, implementa os cenários no script de fixture.
- Roda só em release e é reportado em separado.
- O isolamento vale nos dois sentidos e é verificável:
  - **Desenvolvimento não vê o reservado:** a especificação e os scripts ficam num repo privado
    separado, acessado só pelo job de release via token secreto.
  - **O reservado não vê os detectores:** a sessão que implementa os cenários trabalha numa cópia
    sem `internal/detector/` e **sem `.git`**. A cópia é exportada com `git archive` só das pastas
    necessárias (`demo-app/`, `hook/`, `eval/fixtures/`). Um worktree ou sparse checkout não basta,
    porque o histórico git continua permitindo ler os detectores com `git show`.

## Alternativas consideradas

- **Manter a Causa como tipo + função.** Aceitaria a tabela errada no índice faltando. Perdeu.
- **Incluir a tabela só na Causa do `missing_index`, como exceção.** Resolve o caso, mas cria uma
  régua por tipo no mesmo placar, sem regra que diga o que fazer no próximo tipo. Perdeu para a
  regra geral do alvo.
- **Incluir a coluna no alvo já.** É o alvo mais preciso, mas a coluna sai de texto (`Filter`), e um
  cast, um alias ou um filtro composto virariam erro de Causa sem que o detector tenha errado.
  Perdeu até haver parser de SQL.
- **Só o acerto estrito** (alternativa do ADR-0009). Esconde onde o sistema erra. Perdeu.
- **Só porcentagem** (alternativa do ADR-0009). Com 22 cenários, "81,82%" sugere uma precisão que
  não existe. Perdeu.
- **Alucinação sozinha** (alternativa do ADR-0009). Premia a abstenção. Perdeu.

## Consequências

- O gabarito ganha o campo do alvo, preenchido nos tipos que têm um. Os cenários de N+1 não mudam,
  e o placar deles também não.
- A pontuação precisa receber o alvo da candidata. Para o índice faltando, é a tabela da evidência
  do plano.
- O relatório da avaliação é uma tabela por modo × grupo de cenários × métrica, com denominadores.
- O arquivo do placar faz parte da revisão dos PRs que mexem em detectores.
- Risco: um tipo futuro com alvo só disponível em texto fica sem alvo na Causa até haver parser, e
  pode acertar a Causa com o alvo errado. A evidência continua mostrando o alvo.

## Como validar

- Testes unitários da pontuação: cada métrica com casos de acerto, erro, fora do denominador e
  abstenção.
- Teste: num `missing_index` com tipo e função certos e tabela errada, a Causa é erro. No N+1, a
  Causa não depende de alvo.
- O CI falha quando o só-detectores cai sem atualização do placar.
