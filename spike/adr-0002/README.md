# Spike ADR-0002

Código descartável que responde às perguntas do ADR-0002. Não segue TDD nem a arquitetura do
projeto, e não vai para a `main`. O resultado está em `docs/spikes/2026-11-22-adr-0002-evidencias.md`.

## Reproduzir

Requisitos: Go 1.27.4, Docker (ou Podman com `docker compose`).

```bash
cd spike/adr-0002
mkdir -p out && chmod 777 out
docker compose up -d                        # postgres:19.0 + otelcol-contrib:0.165.0
go build -o bin/app ./app && go build -trimpath -o bin/app-trimpath ./app
go build -o bin/loadgen ./cmd/loadgen && go build -o bin/loopcheck ./cmd/loopcheck

SERVICE_VERSION=spike-hook ./run.sh ./bin/app hook -n 5 -warmup 0   # traces com o hook
./bin/loopcheck -traces out/traces.jsonl                            # inventário + regra do loop

go test -run '^$' -bench . -benchmem -count 5 ./callerhook/         # custo do hook por query

for r in 1 2 3; do                                                  # overhead ponta a ponta
  ROOTLINE_HOOK=off ./run.sh ./bin/app off-r$r -n 500 -warmup 50
  ROOTLINE_HOOK=on  ./run.sh ./bin/app on-r$r  -n 500 -warmup 50
  ROOTLINE_HOOK_CACHE=on ./run.sh ./bin/app cache-r$r -n 500 -warmup 50
done
docker compose down
```

Evidências brutas em `evidence/`.
