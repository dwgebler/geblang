#!/usr/bin/env bash
# Serve-path load matrix: starts the gebweb fixture app (VM mode) and
# the raw http.serve fixture, then runs the load generator at several
# concurrency levels against each. Requires a built ./geblang at the
# repo root (use `make bench-web`).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${GEBLANG_BIN:-$ROOT/geblang}"
DUR="${BENCH_WEB_DURATION:-15s}"
LEVELS="${BENCH_WEB_CONCURRENCY:-16 64 256}"
LOADGEN="$ROOT/benchmarks/web/loadgen-bin"

go build -o "$LOADGEN" "$ROOT/benchmarks/web/loadgen"

run_target() {
    local name="$1" script="$2" url="$3"
    echo "== $name =="
    "$BIN" "$script" &
    local pid=$!
    trap "kill -9 $pid 2>/dev/null || true" RETURN
    for _ in $(seq 1 50); do
        curl -sf -o /dev/null "$url" && break
        sleep 0.2
    done
    for c in $LEVELS; do
        "$LOADGEN" -url "$url" -c "$c" -d "$DUR"
    done
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
}

if [ -d "$ROOT/gebweb" ]; then
    run_target "gebweb (VM mode)" "$ROOT/benchmarks/web/fixtures/gebweb-app/main.gb" "http://localhost:8201/json/7"
fi
run_target "raw http.serve" "$ROOT/benchmarks/web/fixtures/rawapp.gb" "http://localhost:8202/"
rm -f "$LOADGEN"
