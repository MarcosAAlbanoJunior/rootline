#!/usr/bin/env bash
# Gate local, igual ao job `check` do CI: referências a spec no código, versão do hook, build,
# testes, gofmt, vet e lint dos três módulos.
# Rode antes de declarar uma mudança pronta.
set -euo pipefail

cd "$(dirname "$0")/.."
export CGO_ENABLED=0

echo "== referências a spec"
./scripts/check-no-spec-refs.sh

echo "== versão do hook"
./scripts/check-hook-version.sh

command -v golangci-lint >/dev/null || { echo "check: golangci-lint v2.15.0 não encontrado no PATH" >&2; exit 1; }

echo "== raiz"
go build ./...
go test ./...
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
	echo "check: arquivos sem gofmt:" >&2
	echo "$unformatted" >&2
	exit 1
fi
go vet ./...
golangci-lint run

for mod in hook demo-app; do
	echo "== $mod"
	(
		cd "$mod"
		export GOWORK=off
		go build ./...
		go test ./...
		go vet ./...
		golangci-lint run
	)
done

echo "check: ok"
