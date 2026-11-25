# Release

O Rootline publica dois módulos por tag: a raiz (`vX.Y.Z`, o CLI) e o hook (`hook/vX.Y.Z`). As
tags ficam gravadas no proxy do Go e não podem ser reaproveitadas. Uma tag errada é corrigida com
uma versão nova. Decisões: [ADR-0029](adr/0029-guarda-versao-e-publicacao-do-hook.md).

## Hook (`hook/vX.Y.Z`)

1. Mude `Version` em `hook/callerhook.go`. Antes da 1.0, uma mudança incompatível nos atributos do
   contrato sobe o minor; o resto sobe o patch.
2. Se o minor mudou, mude também `compatibleHook` em `internal/analysis/db_checks.go` e o analyze
   que lê os atributos novos. O `scripts/check-hook-version.sh`, parte do gate e do CI, falha
   enquanto os dois não combinam.
3. No guia (`docs/guia-instrumentacao.md`), atualize o `go get …/hook@vX.Y.Z` e a frase sobre qual
   CLI lê qual hook.
4. Depois do merge na `main`, crie e publique a tag no commit do merge:
   `git tag -a hook/vX.Y.Z -m "hook vX.Y.Z" <sha> && git push origin hook/vX.Y.Z`.
5. Confira num cache limpo que só o módulo do hook é baixado:
   `GOMODCACHE=$(mktemp -d) GOWORK=off go get github.com/MarcosAAlbanoJunior/rootline/hook@vX.Y.Z`,
   de dentro de um módulo de teste.

## Raiz (`vX.Y.Z`)

1. Troque no README e no guia o `go install …/cmd/rootline@<versão anterior>` pela versão nova,
   no mesmo PR da mudança, para que o commit da tag já aponte para ela.
2. Se a raiz passou a ler um minor novo do hook, publique antes a tag do hook (seção acima).
3. Depois do merge na `main`, com o CI verde:
   `git tag -a vX.Y.Z -m "rootline vX.Y.Z" <sha> && git push origin vX.Y.Z`.
4. Confira num cache limpo:
   `GOMODCACHE=$(mktemp -d) GOBIN=$(mktemp -d) GOWORK=off go install github.com/MarcosAAlbanoJunior/rootline/cmd/rootline@vX.Y.Z`.
