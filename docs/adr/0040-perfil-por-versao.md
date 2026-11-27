---
id: ADR-0040
title: Guardar um Perfil de versão por operação, com uma amostra das durações, e não os traces brutos
status: Aceito
date: 2026-11-26
summary: O Rootline não guarda traces brutos. Por versão × operação, o estado do watch guarda n, erros, uma amostra de até 1000 durações (para a regra de Regressão continuar a mesma), taxa, janela, a divisão do tempo média e as Candidatas de N+1 da amostra de traces completos. Últimas 10 versões por serviço. Com os traces expirados, a Regressão sai do perfil e o que precisa do trace cru sai pulado.
modules: [analysis, adapter, cmd/rootline]
---

# 0040. Guardar um Perfil de versão por operação

## Contexto

O watch lê os traces do backend do time (ADR-0033), que tem a retenção dele, muitas vezes de poucos
dias. A referência de uma operação (ADR-0037) pode ser uma versão que rodou há semanas, e os traces
brutos dela já podem ter expirado.

A regra de Regressão (`Regress` em `internal/analysis/regression.go`) calcula o p50 e o p95 a partir
das durações cruas de cada versão. Guardar só os percentis não basta: o percentil de duas janelas não
se calcula a partir dos percentis de cada uma, e a regra mudaria de comportamento.

O módulo raiz não tem nenhuma dependência externa. O estado do watch fica num arquivo JSON
(ADR-0035).

## Decisão

**O Rootline não guarda traces brutos.** Eles ficam no backend, com a retenção do time.

**Perfil de versão**, por versão × operação de entrada, dentro do estado do watch:
- n e o número de erros;
- uma amostra de até 1000 durações da operação de entrada, por amostragem uniforme (reservoir) das
  durações vindas da busca. Com até 1000 traces, são todas;
- a taxa em traces por minuto e a janela (ADR-0038);
- a divisão do tempo média por dependência (ADR-0031), calculada sobre a amostra de traces
  completos, com quantos traces ela usou;
- as Candidatas de N+1 achadas na amostra de traces completos, por função e query normalizada,
  para o Sinal antecipado saber se o N+1 é novo (ADR-0036).

**Retenção:** as últimas 10 versões por serviço. A mais antiga sai quando entra a 11ª.

**Com os traces brutos da referência expirados:**
- a Regressão é calculada com as durações do perfil, pela mesma regra;
- a divisão do tempo usa a média guardada;
- as Candidatas de N+1 da referência vêm do perfil;
- o que precisa do trace cru da referência sai como pulado, com o motivo nas Verificações. É o
  caso da presença por trace do trabalho a mais por requisição.

## Alternativas consideradas

- **Guardar só n, p50 e p95.** Menor, mas não permite recalcular a regra sobre uma janela diferente
  nem juntar janelas. Perdeu.
- **Histograma de latência.** Compacto e combinável, mas o p50 e o p95 sairiam aproximados, e a
  regra trabalha com diferenças de 2 ms em operações de 3 ms. Perdeu.
- **Guardar os traces brutos.** Faz do Rootline um backend de traces, com volume e retenção para
  gerir. Perdeu.
- **Guardar todas as versões.** O perfil cresce sem limite, para uma referência que quase sempre é
  uma das últimas versões. Perdeu.

## Consequências

- Com até 1000 durações, os percentis do perfil ficam muito perto dos da janela inteira. Acima
  disso, são uma estimativa por amostra.
- O estado cresce com versões × operações × 1000 durações. Para um serviço com 50 operações, são
  cerca de 500 mil números nas 10 versões, o que cabe num arquivo JSON.
- O perfil é o dado que a visão do roadmap usa depois (MCP para agentes, revisão de PR): rotas,
  tráfego e divisão do tempo por versão, sem precisar do backend.

## Como validar

- Testes do domínio: a Regressão a partir do perfil é igual à calculada com as durações cruas quando
  n ≤ 1000; a retenção remove a 11ª versão; com a referência expirada, as Verificações certas saem
  puladas, com o motivo.
- Medir o tamanho do arquivo de estado na demo do watch e registrar.
