---
id: ADR-0005
title: Definir o Sintoma como operação de entrada + janela ruim, com referência explícita, implícita ou ausente
status: Aceito
date: 2026-11-22
summary: v1 só analisa latência; Sintoma = serviço + operação de entrada (http.route primeiro) + janela ruim de versão única (ou --bad-version); referência explícita, versão anterior com amostra mínima, ou nenhuma; correlação com commit desligada com aviso quando falta referência ou SHA.
modules: [dominio, cli, git]
---

# 0005. Definir o Sintoma e a escolha da referência

## Contexto

A comparação com uma referência e o intervalo do diff dependem do que o usuário informa. O
cenário sem commit (ADR-0004) exige comparar a mesma versão em outra janela. Deploys canary e
rolling deixam duas versões na mesma janela. A telemetria pode trazer um SHA que não existe no
clone local.

## Decisão

**Escopo:** a v1 analisa só **latência**. Taxa de erro fica explicitamente fora.

**Sintoma** = serviço + **operação de entrada** (o span raiz do serviço) + **janela ruim**.
- O primeiro tipo de operação de entrada suportado é HTTP, identificado por método + `http.route`.
- Consumidores de fila e jobs entram depois, como novos tipos de operação de entrada, sem refazer o modelo.

**Versão da janela ruim**
- A janela ruim precisa ter uma única `service.version`. Se tiver mais de uma, a CLI falha
  listando as versões e a faixa de tempo de cada uma.
- `--bad-version <sha>` filtra os traces daquela versão dentro da janela (canary, rolling deploy).

**Referência, em ordem:**
1. **Explícita:** `--baseline-window` ou `--baseline-version`.
2. **Implícita:** a versão imediatamente anterior à da janela ruim, se tiver a **amostra mínima**
   de traces da operação de entrada. Abaixo do mínimo, recua mais uma versão; se ainda faltar,
   segue sem referência.
3. **Sem referência:** os detectores estruturais rodam normalmente; a correlação com o commit
   fica desligada.

**Canary (com `--bad-version`):** a referência implícita passa a ser a outra versão presente na
mesma janela, desde que ela seja a anterior à versão ruim no histórico do git e atinja a amostra
mínima. Caso contrário, por exemplo num rollback em que a versão ruim é a mais antiga, vale a
ordem acima. Mesma janela significa mesma carga e mesmo horário, uma comparação mais justa.

Toda escolha aparece no Resultado: qual referência foi usada, por que e quantos traces ela tem.

**Git**
- O Rootline lê o clone local do serviço (`--repo`), só para leitura.
- Intervalo do diff = `shaReferência..shaRuim`. Com a mesma versão nos dois lados, o diff é vazio
  por definição e o Resultado diz "nenhum commit correlacionado".
- Se o SHA da telemetria (da janela ruim ou da referência) não existir no `--repo` (fetch
  faltando, squash ou rebase, build de árvore suja), a correlação com o commit é desligada com
  mensagem clara, e a análise segue.

## Alternativas consideradas

- **Escolher a versão dominante quando a janela mistura versões.** A regressão costuma estar na
  versão minoritária, a recém-implantada; escolher a dominante analisaria a versão errada sem avisar.
- **Só a versão anterior como referência.** Não cobre regressões vindas dos dados.
- **Sintoma acoplado a HTTP.** Obrigaria a refazer o modelo para filas e jobs.

## Consequências

- Recuar uma versão amplia o intervalo do diff; o Resultado informa os commits incluídos.
- O valor padrão da amostra mínima é definido na spec da CLI e fica configurável.
- Cada degradação (sem referência, SHA ausente) é um caminho de código com teste e mensagem própria.

## Como validar

- Testes unitários da escolha de referência: explícita, implícita, recuo por amostra insuficiente,
  sem referência, janela com várias versões, com e sem `--bad-version`.
- Teste de integração com um repo git real: SHA ausente desliga a correlação sem abortar a análise.
