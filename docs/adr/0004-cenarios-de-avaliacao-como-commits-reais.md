---
id: ADR-0004
title: Montar os cenários da avaliação como commits reais num repo de fixture gerado por script
status: Aceito
date: 2026-11-22
summary: Cada regressão nasce de um commit real num repo gerado por script versionado em eval/fixtures/, com dados, carga e gabarito (por rótulo) reprodutíveis; sem feature flags na v1; gabaritos e detectores usam sinais estruturais, não tempo absoluto; dois cenários medem alucinação.
modules: [eval, demo-app, dominio]
---

# 0004. Montar os cenários da avaliação como commits reais num repo de fixture

## Contexto

O brief previa regressões ligadas por feature flag, mas o produto promete apontar o commit
culpado, e o ADR-0001 usa a correlação candidata ↔ diff como evidência. Uma regressão ligada por
flag não gera diff: a correlação nunca dispara e o distrator "commit irrelevante" nem pode
existir. Além disso, latência medida em container varia de uma execução para outra.

## Decisão

**Commits reais num repo de fixture**
- Um script gerador, versionado no rootline em `eval/fixtures/`, cria um repo git do demo-app por
  cenário. O repo gerado fica fora do histórico do rootline.
- Cada cenário é uma sequência curta de commits: uma referência saudável, o commit da regressão e
  eventuais distratores. Cada versão sobe com `service.version` igual ao seu SHA.
- O script fixa autor e datas dos commits. Mesmo assim, o **gabarito aponta commits por rótulo**
  (`referencia`, `regressao`, `distrator`), nunca por SHA.

**Reprodutibilidade**
- Dados com seed fixa, para volume e conteúdo.
- Carga com taxa e duração fixas.

**Sinais estruturais, não tempo absoluto.** Gabaritos e detectores se apoiam em contagem de
queries, linhas por query e forma da árvore de spans. O tempo caracteriza o sintoma, mas não
decide a causa.

**Sem feature flags na v1.** Uma demonstração manual é só subir o commit da regressão.

**Cenários de alucinação**, reportados juntos como uma métrica de alucinação:
- **Sem commit:** a lentidão vem dos dados (um índice que sempre faltou, exposto pelo crescimento
  da tabela), com o mesmo SHA na referência e na janela ruim. Resultado esperado: causa
  encontrada e nenhum commit correlacionado. Mede se o Rootline inventa um commit.
- **Sem causa visível:** a lentidão vem de algo que o Rootline não enxerga (dependência externa
  não instrumentada respondendo devagar). Resultado esperado: "inconclusivo". Mede se o Rootline
  inventa uma causa.

## Alternativas consideradas

- **Feature flags.** Simples de ligar, mas não geram diff e anulam a correlação com o commit.
- **Histórico real do demo-app no repo do rootline.** Mistura o histórico do produto com o dos
  cenários e põe distratores falsos na `main`.
- **Gabarito por SHA.** Quebra a cada mudança no script gerador.

## Consequências

- A baseline precisa poder ser a mesma versão em outra janela de tempo, não só a versão anterior.
  Isso entra no contrato do Sintoma.
- O demo-app fica sem código de flags e o índice não vê ramos mortos.
- Detectores com limiar de tempo absoluto ficam proibidos como critério de causa.
- O relatório da avaliação ganha uma linha de métrica de alucinação: commit inventado e causa inventada.

## Como validar

- Rodar o script gerador duas vezes produz os mesmos SHAs e os mesmos dados.
- Duas execuções da avaliação produzem os mesmos sinais estruturais por cenário (contagens e árvore
  de spans), mesmo com latências diferentes.
- O relatório da avaliação mostra a métrica de alucinação separada do acerto.
