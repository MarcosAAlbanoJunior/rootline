---
id: ADR-0029
title: Anotar só spans de cliente de banco no hook, emitir a versão dele e publicá-lo por tag
status: Aceito
date: 2026-11-25
summary: Complementa o ADR-0002. O hook só anota o span ativo quando ele é de cliente (SpanKind Client) e emite rootline.hook.version; os atributos são o contrato com o analyze. Publicação por tag hook/vX.Y.Z (primeira hook/v0.1.0); receitas de instalação num guia em docs/, verificadas pelo laboratório, sem módulo de exemplos.
modules: [hook, analysis, docs]
---

# 0029. Anotar só spans de cliente de banco no hook, emitir a versão dele e publicá-lo por tag

## Contexto

O hook de chamador (ADR-0002, pacote `callerhook` no módulo `hook/`) é um `pgx.QueryTracer`. Ele
anota **o span ativo no contexto** quando o pgx chama o tracer. No piloto em apps reais, ele
funcionou em quatro formas de conexão:

- pgxpool com otelpgx num multitracer (apps 01, 02, 03, 08 e 10);
- pgx sem tracer próprio;
- pgx como driver de `database/sql` sob otelsql, via `stdlib.RegisterConnConfig` (app-05);
- GORM sobre pgx, via `pgx.ParseConfig` → `stdlib.OpenDB` → `postgres.New` (app-07).

O mesmo mecanismo abre um buraco. Num app com pgx e nenhuma instrumentação de banco, o span ativo é
o do handler HTTP, e o hook o anotaria com `rootline.db.phase`.

Não há tag publicada do módulo. `go get …/hook@main` gera uma pseudo-versão e baixa também o módulo
raiz, porque os dois caminhos resolvem para o mesmo commit. Não há guia de instalação, e cada
analista do piloto descobriu sozinho a receita do app dele.

## Decisão

**Guarda.**
- O hook só anota o span ativo quando ele é de cliente: `SpanKind` Client.
- A guarda não exige `db.system` nem `db.system.name`. O plugin OpenTelemetry do GORM
  (`gorm.io/plugin/opentelemetry`) abre o span como Client, mas só grava `db.system` no callback
  `after`, depois que o pgx já chamou o hook. Exigir o atributo quebraria a receita do GORM
  (revisão de 2026-11-25, achada ao implementar a guarda).
- Fora disso, o hook não anota nada e incrementa um contador consultável no `Hook`. Um span cujo
  tipo não dá para ler (de uma implementação do OpenTelemetry fora do SDK) é anotado: a guarda
  só descarta o que se sabe não ser cliente.
- A Verificação `caller_hook` (ADR-0027) mostra a fração de spans de banco com pilha, e a dica
  aponta para o guia.

**Versão.**
- O hook emite `rootline.hook.version` em cada span que anota.
- Os atributos do hook formam o contrato entre ele e o `analyze`: `rootline.app_stack`,
  `rootline.db.phase`, `rootline.hook.version` e os `code.*`.
- A Verificação `caller_hook` mostra a versão vista e avisa quando ela é incompatível.
- Antes da 1.0, uma mudança incompatível nesses atributos sobe o minor.

**Publicação.**
- O módulo é publicado por tags `hook/vX.Y.Z`. A primeira é `hook/v0.1.0`, no commit que já tem a
  guarda e a versão.
- O binário `rootline` é instalado por `go install …/cmd/rootline@<tag>`. A primeira tag da raiz,
  `v0.1.0`, sai depois que o teste de aceitação do lote passar.
- Binários pré-compilados ficam para depois.

**Guia.**
- `docs/guia-instrumentacao.md`, em português como os outros docs, traz:
  - os pré-requisitos (`service.version` = SHA);
  - as quatro receitas;
  - o Collector com file exporter;
  - as limitações atuais: só pgx; MySQL, MongoDB e `database/sql` sem pgx não são suportados;
    planos de execução só na avaliação.
- O README ganha uma seção curta "comece aqui" apontando para o guia.
- As receitas são verificadas pelo laboratório de apps reais, não por um módulo de exemplos.

## Alternativas consideradas

- **Guarda com `SpanKind` Client e `db.system` ou `db.system.name`.** Era a decisão original e
  fecha também o caso de um span de cliente que não é de banco. Mas o GORM só grava `db.system`
  depois da query, e um span de cliente HTTP ativo durante uma chamada do pgx é improvável.
  Perdeu na revisão.
- **Sem guarda, só documentar que precisa de instrumentação de banco.** Não muda código, mas o
  erro fica silencioso: spans HTTP anotados como banco confundem o detector. Perdeu.
- **Módulo `hook/examples` com uma receita compilada por forma de conexão.** Daria garantia em CI,
  mas puxa GORM e otelsql como dependências e cria um quarto módulo, contra o ADR-0010. Perdeu.
- **Continuar com pseudo-versões de `@main`.** Zero trabalho de release, mas baixa o módulo raiz e
  não dá ao usuário um número para citar. Perdeu.
- **Publicar o binário com GoReleaser já neste lote.** Mais conveniente, mas é infraestrutura de
  release no meio de um lote de usabilidade. Fica para depois.

## Consequências

- Um app sem instrumentação de banco passa a ter "0% dos spans de banco com pilha" explicado, em
  vez de spans HTTP anotados.
- O `analyze` consegue dizer "seu hook é antigo" em vez de falhar calado.
- Cada mudança nos atributos do hook passa a exigir tag e nota de compatibilidade.
- As receitas não têm teste de CI. Uma mudança no otelsql ou no plugin do GORM só aparece na próxima
  rodada do laboratório.

## Como validar

- Teste unitário: com um span ativo que não é de banco, o hook não anota e o contador sobe; com um
  span de cliente de banco, anota com `rootline.hook.version`.
- Depois da tag: num diretório limpo, `go get …/hook@v0.1.0` baixa só o módulo `hook`. Se baixar
  também o raiz, reabrir a decisão de publicação.
- Teste de aceitação com usuário cego: com só o guia, o agente instala o hook nos apps com pgx do
  laboratório.
