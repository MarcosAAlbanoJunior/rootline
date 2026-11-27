---
id: ADR-0039
title: Permitir uma única escrita externa, a publicação do Resultado, com JSON no stdout sempre e comentário no commit culpado como primeiro destino
status: Aceito
date: 2026-11-26
summary: Muda o brief, que proibia qualquer escrita em sistemas externos. O Rootline pode publicar o Resultado, e só ele, atrás de uma porta de publicação. JSON no stdout sempre; o primeiro destino externo é um comentário do GitHub no commit culpado, na linha quando ela está no diff; Slack depois. Por padrão só sai Resultado com Causa. Tokens só por variável de ambiente.
modules: [analysis, adapter, cmd/rootline]
---

# 0039. Publicar o Resultado em destino externo

## Contexto

O brief deixa fora da v1 "ações automáticas de correção, abertura de PR ou qualquer escrita em
sistemas externos". O watch (ADR-0035) só tem valor se o Resultado chegar a alguém sem que essa
pessoa rode um comando, e entregar a mensagem é escrever num sistema externo.

O Resultado leva nomes de função, de arquivo, de tabela e a query normalizada (com `$1`, sem
valores). Num canal aberto, isso expõe a estrutura do código a quem não vê o repositório.

Na rodada 2 do piloto, o Rootline não acertou nenhum dos sete apps, porque as causas estavam fora
dos detectores. Publicar todo `Inconclusive` encheria o destino de mensagens sem resposta.

## Decisão

**Escopo da escrita.** O Rootline pode escrever fora dele só para publicar um Resultado, um Sinal
antecipado (ADR-0036) ou o resumo do prazo (ADR-0035). Ele nunca escreve no backend de traces, no
repositório, no banco da aplicação nem em issue ou PR. A linha do brief muda para dizer isso.

**Porta de publicação.** Os destinos ficam atrás de uma porta declarada por quem publica. O domínio
monta a mensagem, e o adapter só entrega.

**Destinos, em ordem:**
1. **JSON no stdout:** sempre ligado. Qualquer time pode encaminhar a mensagem com o que já usa.
2. **Comentário do GitHub no commit culpado:** o primeiro destino externo. O comentário fica no
   commit que a correlação apontou, ancorado no arquivo e na linha quando ela está no diff daquele
   commit, e como comentário geral do commit quando não está. Quem lê é quem já vê o código.
3. **Slack**, depois, para um canal do próprio time.

**O que sai por padrão.** Só Resultado com Causa e Sinal antecipado. Um `Inconclusive` com Regressão
vai para o resumo do prazo, e não vira mensagem própria. A configuração pode ligar a mensagem de
`Inconclusive`.

**Segredos.** Os tokens dos destinos vêm só de variável de ambiente, nunca do arquivo de
configuração nem do estado.

## Alternativas consideradas

- **Manter a proibição e só imprimir.** O watch viraria um log que alguém precisa ler. Perdeu.
- **Slack como primeiro destino.** É o mais simples de entregar, mas manda nome de tabela e função
  para um canal que pode ter gente sem acesso ao código. Perdeu como primeiro.
- **Comentário no PR que levou o commit.** Exige achar o PR pelo commit e não existe para push
  direto. O commit sempre existe. Perdeu por enquanto.
- **Abrir issue.** É escrita além do Resultado e cria trabalho a fechar. Perdeu.

## Consequências

- O brief e o ADR passam a dizer a mesma coisa sobre escrita externa.
- O token do GitHub precisa de permissão de escrita em comentários do repositório.
- Num repositório público, o comentário é público, mas não traz nada que o código já não mostre,
  além dos números de latência.
- O comentário no commit prepara o caminho da revisão de PR com dados de produção do roadmap.

## Como validar

- Testes do domínio: a mensagem por tipo (Causa, Sinal antecipado, resumo); `Inconclusive` fora
  das mensagens por padrão.
- Teste do adapter do GitHub contra um servidor HTTP falso: o corpo do comentário, a âncora no
  arquivo e na linha, o comentário geral quando a linha não está no diff, e o token lido do
  ambiente.
- depguard: o domínio não importa o adapter de publicação.
