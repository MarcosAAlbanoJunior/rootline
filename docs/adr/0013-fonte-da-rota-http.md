---
id: ADR-0013
title: Obter a rota HTTP da operação de entrada pelo atributo, pelo nome do span ou pelo middleware do hook
status: Aceito
date: 2026-11-22
summary: Ordem - atributo http.route; senão, derivada estritamente do nome do span do otelhttp ("MÉTODO /padrão"), com a fonte registrada e aviso se houver operações distintas demais; o módulo hook oferece um middleware que grava http.route a partir de r.Pattern.
modules: [trace, dominio, hook]
---

# 0013. Obter a rota HTTP da operação de entrada

## Contexto

O ADR-0005 identifica a operação de entrada HTTP por método + `http.route`, e o nível 2 de
localização (ADR-0002) usa a rota. O spike do ADR-0002 mostrou que o otelhttp v0.72.0, envolvendo
o `ServeMux` por fora, **não emite o atributo `http.route`**: a rota só aparece no nome do span
(`GET /customers/{id}/orders/explicit`). Muitos serviços já usam o otelhttp assim.

## Decisão

**Ordem de obtenção da rota:**
1. **Atributo `http.route`**, se existir.
2. **Derivada do nome do span**, só para spans de servidor do otelhttp e só se o nome casar
   estritamente com `MÉTODO /padrão`, com método HTTP válido. O Resultado registra a fonte:
   "derivada do nome".
3. **Middleware do hook**: o módulo `hook/` oferece um middleware opcional que grava `http.route`
   a partir de `r.Pattern`, para quem quer a rota como atributo.

**Aviso de cardinalidade:** se o número de operações de entrada distintas for anormalmente alto
(sinal de caminho real no nome, como `/customers/42`, em vez do padrão), o Resultado traz um aviso.
O limiar é definido na spec.

**Como o middleware funciona.** O `ServeMux` preenche `r.Pattern` no próprio `*Request` que recebe
(`net/http/server.go`: `h, r.Pattern, r.pat, r.matches = mux.findHandler(r)`), e o otelhttp
termina o span só depois que o handler interno retorna. Por isso o middleware pode **envolver o
mux**, desde que leia `r.Pattern` **depois** de chamar `next`:

```go
mux := http.NewServeMux()
// ... rotas
handler := otelhttp.NewHandler(callerhook.RouteMiddleware(mux), "server")

// no módulo hook:
func RouteMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Pattern != "" {
			trace.SpanFromContext(r.Context()).SetAttributes(semconv.HTTPRoute(r.Pattern))
		}
	})
}
```

**Limitação documentada:** com `http.StripPrefix` ou muxes aninhados, o mux interno recebe uma
cópia do request e o `r.Pattern` dele não chega ao wrapper externo. Nesses casos, o middleware
envolve o mux interno ou os handlers.

## Alternativas consideradas

- **Exigir o atributo `http.route`.** Quebra a adoção por serviços que já usam otelhttp.
- **Derivar do nome sem validação.** Aceitaria nomes com caminhos reais, explodindo a cardinalidade.
- **Middleware obrigatoriamente por handler.** Funciona, mas é mais invasivo; envolver o mux basta
  no caso comum.

## Consequências

- O modelo de trace guarda a fonte da rota junto com a operação de entrada.
- O módulo `hook/` ganha o middleware de rota, com teste do caso comum e do caso aninhado.

## Como validar

- Testes da derivação: casa `GET /customers/{id}`; recusa nome sem método, método inválido,
  nome livre e spans que não são do otelhttp.
- Teste do aviso de cardinalidade com nomes contendo caminhos reais.
- Teste do middleware: rota gravada com o mux envolvido; ausente (e documentada) com StripPrefix.
