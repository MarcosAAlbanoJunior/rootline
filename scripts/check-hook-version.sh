#!/usr/bin/env bash
# Mantém juntas a versão do hook e a que o analyze lê: o major.minor de callerhook.Version
# (hook/callerhook.go) tem de ser o compatibleHook (internal/analysis/db_checks.go). A tag
# hook/vX.Y.Z segue o callerhook.Version; o passo a passo está em docs/release.md (ADR-0029).
set -euo pipefail

cd "$(dirname "$0")/.."

hook="$(sed -nE 's/^const Version = "([0-9]+\.[0-9]+)\.[0-9]+"$/\1/p' hook/callerhook.go)"
analyze="$(sed -nE 's/^const compatibleHook = "([0-9]+\.[0-9]+)"$/\1/p' internal/analysis/db_checks.go)"

if [ -z "$hook" ] || [ -z "$analyze" ]; then
	echo "check-hook-version: não achei callerhook.Version ou compatibleHook" >&2
	exit 1
fi
if [ "$hook" != "$analyze" ]; then
	echo "check-hook-version: o hook é $hook.x e o analyze lê $analyze.x; atualize os dois juntos (docs/release.md)" >&2
	exit 1
fi
