#!/usr/bin/env bash
# Impede o código (.go, nos três módulos) de citar o processo de desenvolvimento: spec, slice,
# critério de aceite ou "Expected API". A regra está no CLAUDE.md.
# Só olha arquivos rastreados pelo git (git grep).
set -euo pipefail

cd "$(dirname "$0")/.."

# ERE sem \b, para funcionar igual no regex do glibc e do macOS.
pattern='(^|[^A-Za-z0-9])AC[0-9]+|TestAC[0-9]|[Ss]lice[ -]?[0-9]|(^|[^A-Za-z])[Ss]pecs?([^A-Za-z]|$)|[Aa]cceptance [Cc]riteri|Affected files|Expected API'

status=0
git grep -nIE "$pattern" -- '*.go' || status=$?

case "$status" in
	0)
		echo "check-no-spec-refs: code must not cite specs, slices or acceptance criteria (see CLAUDE.md)" >&2
		exit 1
		;;
	1) exit 0 ;;
	*) exit "$status" ;;
esac
