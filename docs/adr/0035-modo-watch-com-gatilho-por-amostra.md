---
id: ADR-0035
title: Rodar o rootline watch como uma instância por serviço, que percebe a versão nova pelos traces e compara cada operação quando ela junta a amostra mínima
status: Aceito
date: 2026-11-26
summary: O watch consulta os valores de service.version a cada minuto; a versão nova é a que aparece pela primeira vez, e o deploy é o horário do primeiro trace dela. Cada operação é comparada quando as duas versões têm 30 traces; depois de 48 h sai um resumo das que ficaram sem amostra. Estado num arquivo JSON gravado de forma atômica; cada versão × operação é publicada uma vez.
modules: [analysis, adapter, cmd/rootline]
---

# 0035. Rodar o rootline watch com gatilho por amostra

## Contexto

O fluxo de hoje é manual: gravar o `traces.jsonl`, rodar o `symptoms` e depois o `analyze`. Se
ninguém desconfiar do deploy, ninguém roda. A proposta (`docs/proximo-passo-watch.md`) é um modo
serviço que avisa sozinho depois de cada deploy, lendo o backend de traces (ADR-0033).

Duas coisas tornam o problema diferente de um timer depois do deploy:
- As operações têm tráfegos muito diferentes. Uma rota quente junta 30 traces em minutos, e outra só
  é chamada horas depois. Um timer fixo analisa a rota rara sem amostra ou atrasa a rota quente.
- O pré-requisito do Rootline já é o serviço emitir `service.version` com o SHA (ADR-0005). A versão
  nova aparece nos traces sem precisar de nenhuma outra integração.

## Decisão

**Uma instância por serviço.** O `rootline watch` lê a configuração de um serviço: backend,
amostragem declarada (ADR-0034), clone do repositório e destinos (ADR-0039). Vários serviços são
vários processos. Nada é instalado nos pods.

**Versão nova pelos traces.** A cada minuto (configurável), o watch lista os valores de
`service.version` do serviço no backend. Uma versão que aparece pela primeira vez é nova, e o
momento do deploy é o horário do primeiro trace dela.
- Se a versão não está no clone, o watch faz `git fetch` e tenta de novo. Se ela continuar ausente
  até o prazo, sai no resumo como "versão sem commit no repositório" e não é analisada: sem SHA, não
  há linha nem commit para apontar.
- Rollback (a versão nova é ancestral da anterior no git) não é analisado (ADR-0037).

**Gatilho por operação.** Cada operação de entrada da versão nova é comparada com a referência
(ADR-0037) quando as duas versões têm a amostra mínima da Regressão (30 traces, ADR-0032). Cada
operação tem o próprio relógio.
- Sem Regressão: a operação fica marcada como ok, sem mensagem.
- Com Regressão: roda o `analyze` dela e publica o Resultado (ADR-0039).
- **Prazo sem amostra:** 48 h depois do deploy (configurável), sai um resumo único com as operações
  que ficaram sem amostra e as versões sem commit, em vez de silêncio.

**Estado.** Um arquivo JSON, gravado de forma atômica (arquivo temporário + rename), num volume.
Guarda as versões vistas com o horário do primeiro trace, o status de cada versão × operação
(aguardando, ok, Regressão, publicada) e o perfil por versão (ADR-0040).
- Cada versão × operação é publicada uma vez. Reiniciar o watch não repete mensagens.
- Uma operação no limite da regra não oscila entre mensagens, porque a comparação é feita uma vez,
  quando a amostra fecha.

## Alternativas consideradas

- **Webhook ou CLI no pipeline de deploy.** Adianta a detecção em até um minuto, mas exige mexer no
  pipeline de cada time. Fica como gatilho opcional para depois. Perdeu no primeiro corte.
- **Observar o Deployment no Kubernetes.** Pede permissão no cluster e só serve para Kubernetes.
  Perdeu.
- **Timer fixo depois do deploy.** Falha com as operações raras. Perdeu.
- **SQLite em Go puro para o estado.** Seria a primeira dependência externa do módulo raiz, para um
  volume pequeno (versões × operações × uma amostra de durações). Fica para quando o arquivo não der
  conta. Perdeu por enquanto.
- **Uma instância para vários serviços.** Junta estado e configuração de serviços diferentes num
  processo, sem ganho no primeiro corte. Perdeu por enquanto.

## Consequências

- Perceber o deploy leva até um intervalo de consulta, irrelevante perto do tempo de juntar amostra.
- O watch depende do custo de listar os valores de `service.version`, que o spike do ADR-0033 mede.
- Dois deploys seguidos antes da amostra: a comparação passa a ser entre a última versão com
  amostra e a atual. A correlação olha todos os commits do intervalo (`RevList`), então o commit
  culpado continua aparecendo.
- O estado num arquivo exige um volume e uma instância só por serviço. Duas instâncias com o mesmo
  arquivo publicariam em dobro.

## Como validar

- Testes do domínio com relógio e backend falsos: a versão nova detectada; a rota quente comparada
  antes da rara; o resumo no prazo; a versão sem commit; o rollback não analisado; reiniciar não
  repete publicação.
- Demo: o demo-app num cluster local com o Tempo. Depois do deploy de um cenário com N+1, a
  mensagem com Causa sai sem nenhum passo manual. O tempo entre a amostra fechar e a mensagem sair é
  medido e registrado.
