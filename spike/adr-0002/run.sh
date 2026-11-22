#!/usr/bin/env bash
# run.sh <binary> <label> <loadgen args...>: starts the app, waits, runs loadgen, stops the app.
set -euo pipefail
bin=$1; label=$2; shift 2
"$bin" > "out/app-$label.log" 2>&1 &
pid=$!
for _ in $(seq 50); do curl -fs -o /dev/null http://localhost:58080/customers/1/orders/helper && break; sleep 0.2; done
./bin/loadgen -label "$label" "$@"
kill -INT $pid; wait $pid || true
