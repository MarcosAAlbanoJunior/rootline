---
id: ADR-0006
title: Correlacionar candidata e commit por quatro tipos de toque, com evidência negativa fraca
status: Aceito
date: 2026-11-22
summary: Um commit se correlaciona com uma candidata se tocou a pilha, a query, o schema ou (peso baixo) configuração/dependências; cada toque vira evidência tipada e pesa no score; "não correlacionado" só reduz o score, nunca descarta. Parser de SQL em aberto (cgo).
modules: [dominio, indice, git]
---

# 0006. Correlacionar candidata e commit por quatro tipos de toque

## Contexto

O ADR-0001 manda calcular, na fase 1, se o commit tocou na candidata. Comparar o diff só com os
frames da pilha erra o cenário de índice faltando: o commit culpado é uma migration, que não
aparece em nenhuma pilha. Além disso, nenhuma lista de tipos de toque cobre todas as formas de um
commit causar uma regressão.

## Decisão

Para cada commit do intervalo `shaReferência..shaRuim` e cada candidata, a fase 1 calcula os toques:

1. **Tocou a pilha:** o commit alterou linhas dentro de uma função da pilha da aplicação da
   candidata (intervalo de linhas da função, vindo do índice, contra os hunks do diff). Tocar a
   função da localização pesa mais que tocar um frame mais externo.
2. **Tocou a query:** o commit alterou o texto SQL que produz a query normalizada da candidata
   (arquivo de queries do sqlc ou string SQL no código).
3. **Tocou o schema:** o commit alterou uma migration que menciona uma tabela usada pela query da
   candidata (`DROP INDEX`, `ALTER TABLE`, mudança de coluna).
4. **Tocou configuração ou dependências (peso baixo):** `go.mod`/`go.sum`, configuração de pool
   de conexões, timeouts. Não aponta uma candidata específica, mas é causa clássica de contenção.
   O tipo existe no modelo, mas pode ficar fora dos cenários da v1.

Cada toque vira uma evidência com o tipo marcado e entra no score determinístico da candidata.

**Evidência negativa é fraca.** "Nenhum toque" só reduz o score; nunca descarta uma candidata nem
um commit, porque os quatro tipos não cobrem todas as formas de causar uma regressão.

## Alternativas consideradas

- **Só toque na pilha.** Erra o índice faltando, um dos quatro cenários básicos.
- **"Não correlacionado" descarta o commit.** Transforma uma lacuna do modelo em certeza falsa.
- **Deixar a correlação para o LLM.** Contraria o ADR-0001: é determinístico, então é feito em código.

## Consequências

- O índice precisa fornecer o intervalo de linhas de cada função e a localização das strings SQL.
- Os tipos 2 e 3 precisam saber quais tabelas uma query usa, o que exige analisar SQL do Postgres.
- O distrator "commit irrelevante" funciona: ele fica sem toque e com score menor, mas continua visível.

## Como validar

- Testes unitários por tipo de toque, com diffs, pilhas e migrations em fixtures.
- Teste: um commit sem nenhum toque reduz o score, mas a candidata continua no Resultado.
- Avaliação: o cenário de índice faltando correlaciona o commit da migration (toque de schema).

## Questões em aberto

- **Parser de SQL.** O `pg_query_go` é a opção natural, mas depende de cgo, o que conflita com o
  binário estático do ADR-0003. A pesquisa deve comparar: parser com cgo, alternativa em Go puro
  e extração limitada ao subconjunto de SQL que o projeto gera. A escolha também cobre a
  normalização de queries usada pelos detectores.
