---
id: ADR-0017
title: Manter o nível 1 com aviso quando nenhum frame da pilha está dentro de um loop
status: Aceito
date: 2026-11-22
summary: Sem frame em loop, a localização continua no frame mais interno resolvível, nível 1 e in_loop false, e ganha o aviso loop_not_found. O nível diz só a procedência da localização (pilha ou rota → handler), não se o loop foi achado.
modules: [detector, analysis, eval]
---

# 0017. Manter o nível 1 com aviso quando nenhum frame da pilha está dentro de um loop

## Contexto

O ADR-0002 define o nível 1: o índice percorre a `rootline.app_stack` do frame mais interno para fora e
escolhe o primeiro cujo ponto de chamada está dentro de um `for`/`range`. O ADR não diz o que acontece
quando nenhum frame está num loop. Isso ocorre quando o loop está além do 8º frame (limite da pilha no
ADR-0002), quando está fora do módulo principal (uma biblioteca que chama um callback da aplicação) ou
quando não há loop.

O ADR-0014 lista quando o nível 1 não é possível (sem hook, pilha ausente, caminho que não resolve,
ambiguidade) e manda cair para o nível 2. "Nenhum frame em loop" não está na lista.

Hoje o detector (`locateSpan`) usa o frame mais interno resolvível, com nível 1 e `in_loop: false`, sem
nenhum aviso. O Resultado não diz que a regra do loop não se cumpriu, o que contraria o princípio de o
Resultado sempre explicar o que faltou (ADR-0014).

## Decisão

Sem frame em loop, a localização continua sendo o frame mais interno resolvível, com nível 1 e
`in_loop: false`, e o Resultado ganha o aviso:

```
loop_not_found: no app_stack frame inside a loop (<n> frames)
```

- `<n>` é o número de frames da pilha do span.
- O aviso aparece uma vez por pilha distinta. Vários spans com a mesma pilha não o repetem.
- Quando algum frame está em loop, o aviso não aparece.
- O prefixo `loop_not_found:` é o identificador estável. O resto do texto pode mudar.

O **nível descreve só a procedência** da localização: 1 = pilha do span (exata, com linha); 2 = rota →
handler (aproximada, sem linha, ADR-0014). Se o loop foi achado ou não, quem diz é `in_loop` e os avisos.

## Alternativas consideradas

- **Rebaixar para o nível 2.** Trataria "sem loop" como falha do nível 1. Perdeu porque troca a linha
  exata da query por um handler aproximado, e a linha da query é a melhor pista justamente quando o loop
  está numa biblioteca que chama a aplicação. Também faria o nível misturar duas coisas, procedência e
  certeza sobre o loop.
- **Devolver localização nula.** Joga fora a pilha, que é evidência real. Perdeu pelo mesmo motivo do
  ADR-0014: informa menos sem ganho de rigor.
- **Manter como está, sem aviso.** Não muda nada, mas o Resultado parece uma localização normal e quem lê
  só o nível não sabe que a regra do loop falhou.

## Consequências

- Positivo: o Resultado explica a falta do loop e mantém a linha exata da query.
- Positivo: a métrica Linha não muda. A linha não é a do gabarito e conta como erro com ou sem nível 2.
- Negativo: quem consome o Resultado, inclusive o LLM, precisa ler `in_loop` e os avisos, não só o nível.
- Risco: o texto do aviso entra no Resultado JSON. Quem casar pelo texto inteiro quebra se ele mudar; por
  isso o contrato é o prefixo.

## Como validar

- Teste unitário do detector com uma pilha de 8 frames resolvíveis e nenhum em loop: nível 1, frame mais
  interno, `InLoop` falso e o aviso `loop_not_found: no app_stack frame inside a loop (8 frames)`
  exatamente uma vez. Com um frame em loop, o aviso não aparece.
- Avaliação: a distribuição dos níveis na métrica Linha (ADR-0009) não muda com esta decisão.
