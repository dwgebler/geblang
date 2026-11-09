#!/usr/bin/env bash
#
# benchmarks/run.sh -- Geblang performance benchmark harness.
#
# Runs the bench workloads against the geblang VM, CPython, PHP, and Node.
# Prints median / min / max milliseconds across N repeats.
#
# Usage:
#   benchmarks/run.sh                  # host-installed python/php/node
#   benchmarks/run.sh --docker         # python/php/node in official containers
#   benchmarks/run.sh --repeats 7
#   benchmarks/run.sh --case numeric_loop
#   benchmarks/run.sh --lang geblang          # skip the comparison languages
#   benchmarks/run.sh --lang geblang,python
#   benchmarks/run.sh --csv
#
# Environment variables:
#   GEBLANG_BIN         path to the geblang binary (default: build/geblang-bench).
#   BENCH_PYTHON_IMAGE  python image tag used by --docker (default: python:3.13-alpine).
#   BENCH_PHP_IMAGE     php image tag used by --docker (default: php:8.4-cli-alpine).
#   BENCH_NODE_IMAGE    node image tag used by --docker (default: node:22-alpine).
#   GOCACHE             passed to `go build` when geblang needs to be built.
#   GOTOOLCHAIN         passed to `go build` (default: auto so the right Go is fetched if missing).
#
# Replaces the previous benchmarks/run.py - shell + standard Unix tools
# means --docker mode doesn't need Python on the host.

set -euo pipefail

# Resolve repo root from this script's location.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ---- Benchmark cases (parallel arrays so we stay portable bash). ----
CASE_NAMES=(numeric_loop recursive_fib list_pipeline string_concat dict_ops class_dispatch regex_match json_roundtrip list_functional string_build method_calls dict_delete dict_literal)
CASE_ARGS=("2000000"     "28"          "5000"        "20000"        "10000"  "50000"        "100000"     "100"           "10000"         "50000"      "500000"     "40000"     "200000")

# ---- CLI parsing. ----
REPEATS=5
USE_DOCKER=0
SELECTED_CASE=""
SELECTED_LANGS=""
OUTPUT_FORMAT="table"

while [ $# -gt 0 ]; do
    case "$1" in
        --repeats)
            REPEATS="$2"; shift 2 ;;
        --repeats=*)
            REPEATS="${1#--repeats=}"; shift ;;
        --docker)
            USE_DOCKER=1; shift ;;
        --case)
            SELECTED_CASE="$2"; shift 2 ;;
        --case=*)
            SELECTED_CASE="${1#--case=}"; shift ;;
        --lang)
            SELECTED_LANGS="$2"; shift 2 ;;
        --lang=*)
            SELECTED_LANGS="${1#--lang=}"; shift ;;
        --csv)
            OUTPUT_FORMAT="csv"; shift ;;
        -h|--help)
            sed -n '3,21p' "$0" | sed 's/^# \{0,1\}//'
            exit 0 ;;
        *)
            echo "unknown argument: $1" >&2
            echo "use --help for usage." >&2
            exit 2 ;;
    esac
done

if ! [[ "$REPEATS" =~ ^[0-9]+$ ]] || [ "$REPEATS" -lt 1 ]; then
    echo "--repeats must be a positive integer" >&2
    exit 2
fi

# ---- Bench against the binary `make build` produces. Override
#      GEBLANG_BIN to compare against a different build. ----
GEBLANG_BIN="${GEBLANG_BIN:-$ROOT/geblang}"
if [ ! -x "$GEBLANG_BIN" ]; then
    ( cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/geblang-go-cache}" \
        GOTOOLCHAIN="${GOTOOLCHAIN:-auto}" \
        go build -o "$GEBLANG_BIN" ./cmd/geblang )
fi

# ---- Helpers. ----
#
# Command building writes into the global CMD array (instead of
# round-tripping through command substitution, which can't carry NUL
# bytes). Each helper sets CMD or returns nonzero with an explanation
# on stderr when the interpreter / docker is unavailable.

declare -a CMD=()

# Append a path + a list of whitespace-split args to CMD.
push_cmd_args() {
    CMD+=("$1")
    shift
    # shellcheck disable=SC2206
    local extra=($1)  # intentional word-splitting on args list
    CMD+=("${extra[@]}")
}

set_cmd_host() {
    local lang="$1" case_name="$2" args="$3"
    CMD=()
    case "$lang" in
        geblang)
            CMD=("$GEBLANG_BIN" --vm-strict "$ROOT/benchmarks/geblang/$case_name.gb")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        python)
            local py
            py="$(command -v python3 || true)"
            if [ -z "$py" ]; then return 1; fi
            CMD=("$py" "$ROOT/benchmarks/python/$case_name.py")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        php)
            local php
            php="$(command -v php || true)"
            if [ -z "$php" ]; then return 1; fi
            CMD=("$php" "$ROOT/benchmarks/php/$case_name.php")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        node)
            local node
            node="$(command -v node || true)"
            if [ -z "$node" ]; then return 1; fi
            CMD=("$node" "$ROOT/benchmarks/node/$case_name.js")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        *)
            return 1 ;;
    esac
}

set_cmd_docker() {
    local lang="$1" case_name="$2" args="$3"
    CMD=()
    case "$lang" in
        geblang)
            # geblang is the binary we just built on the host - no
            # container needed even in --docker mode.
            set_cmd_host geblang "$case_name" "$args"
            return $? ;;
        python)
            if ! command -v docker >/dev/null 2>&1; then return 1; fi
            local image="${BENCH_PYTHON_IMAGE:-python:3.13-alpine}"
            CMD=(docker run --rm -v "$ROOT:/work:ro" -w /work "$image" python "/work/benchmarks/python/$case_name.py")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        php)
            if ! command -v docker >/dev/null 2>&1; then return 1; fi
            local image="${BENCH_PHP_IMAGE:-php:8.4-cli-alpine}"
            CMD=(docker run --rm -v "$ROOT:/work:ro" -w /work "$image" php "/work/benchmarks/php/$case_name.php")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        node)
            if ! command -v docker >/dev/null 2>&1; then return 1; fi
            local image="${BENCH_NODE_IMAGE:-node:22-alpine}"
            CMD=(docker run --rm -v "$ROOT:/work:ro" -w /work "$image" node "/work/benchmarks/node/$case_name.js")
            # shellcheck disable=SC2206
            local extra=($args); CMD+=("${extra[@]}")
            ;;
        *)
            return 1 ;;
    esac
}

# Run CMD once and print "<elapsed_ms>\t<stdout>". Aborts on failure.
# Stderr is captured separately so it doesn't pollute the output-
# consistency check - Docker's image-pull progress messages, in
# particular, would otherwise be treated as the benchmark's stdout.
run_once() {
    local start_ns end_ns elapsed_ms output stderr_tmp rc
    stderr_tmp="$(mktemp)"
    start_ns="$(date +%s%N)"
    if ! output="$("${CMD[@]}" 2>"$stderr_tmp")"; then
        rc=$?
        echo "command failed (exit $rc): ${CMD[*]}" >&2
        echo "stderr:" >&2
        cat "$stderr_tmp" >&2
        rm -f "$stderr_tmp"
        exit 1
    fi
    end_ns="$(date +%s%N)"
    rm -f "$stderr_tmp"
    elapsed_ms=$(( (end_ns - start_ns) / 1000000 ))
    # Guard against the rare wall-clock-went-backwards case (an NTP
    # step during the run, or another scheduler hiccup). A negative
    # elapsed value would otherwise pollute min/median.
    if [ "$elapsed_ms" -lt 0 ]; then
        elapsed_ms=0
    fi
    output="$(printf '%s' "$output" | tr -d '\r')"
    # Trim trailing whitespace including newlines.
    while [[ "$output" =~ [[:space:]]$ ]]; do output="${output%?}"; done
    printf '%s\t%s\n' "$elapsed_ms" "$output"
}

# Pre-pull the docker images we'll use so the first benchmark run isn't
# dominated by a fresh layer download. Silent on success; prints
# progress on stderr only if a pull is actually needed.
prepull_docker_images() {
    if ! command -v docker >/dev/null 2>&1; then
        echo "--docker requested but docker not on PATH" >&2
        exit 2
    fi
    local images=("${BENCH_PYTHON_IMAGE:-python:3.13-alpine}" "${BENCH_PHP_IMAGE:-php:8.4-cli-alpine}" "${BENCH_NODE_IMAGE:-node:22-alpine}")
    local img
    for img in "${images[@]}"; do
        if ! docker image inspect "$img" >/dev/null 2>&1; then
            echo "pulling $img ..." >&2
            docker pull "$img" >&2
        fi
    done
}

# Compute median / min / max from stdin (one integer per line).
# Echoes "<median>\t<min>\t<max>".
stats() {
    local sorted n min max median
    sorted="$(sort -n)"
    n="$(printf '%s' "$sorted" | grep -c '^')"
    min="$(printf '%s' "$sorted" | head -1)"
    max="$(printf '%s' "$sorted" | tail -1)"
    if (( n % 2 == 1 )); then
        local mid=$(( (n + 1) / 2 ))
        median="$(printf '%s' "$sorted" | sed -n "${mid}p")"
    else
        local lo=$(( n / 2 )) hi=$(( n / 2 + 1 )) a b
        a="$(printf '%s' "$sorted" | sed -n "${lo}p")"
        b="$(printf '%s' "$sorted" | sed -n "${hi}p")"
        median=$(( (a + b) / 2 ))
    fi
    printf '%s\t%s\t%s\n' "$median" "$min" "$max"
}

# ---- Main loop. ----

LANGS=(geblang python php node)
if [ -n "$SELECTED_LANGS" ]; then
    IFS=',' read -r -a REQUESTED <<<"$SELECTED_LANGS"
    FILTERED=()
    for lang in "${REQUESTED[@]}"; do
        case "$lang" in
            geblang|python|php|node) FILTERED+=("$lang") ;;
            *)
                echo "unknown language in --lang: $lang (expected geblang, python, php, node)" >&2
                exit 2 ;;
        esac
    done
    LANGS=("${FILTERED[@]}")
fi
RESULTS=()        # tab-separated rows: case<TAB>lang<TAB>median<TAB>min<TAB>max<TAB>output
OUTPUTS_KEY=()    # case-name lookup keys
OUTPUTS_VAL=()    # the canonical output observed for each case

key_index() {
    local target="$1" i
    for i in "${!OUTPUTS_KEY[@]}"; do
        if [ "${OUTPUTS_KEY[$i]}" = "$target" ]; then
            echo "$i"; return
        fi
    done
    echo -1
}

run_case_lang() {
    local case_name="$1" args="$2" lang="$3"
    if [ "$USE_DOCKER" -eq 1 ]; then
        if ! set_cmd_docker "$lang" "$case_name" "$args"; then
            RESULTS+=("$(printf '%s\t%s\tskipped\tskipped\tskipped\truntime not installed' "$case_name" "$lang")")
            return
        fi
    else
        if ! set_cmd_host "$lang" "$case_name" "$args"; then
            RESULTS+=("$(printf '%s\t%s\tskipped\tskipped\tskipped\truntime not installed' "$case_name" "$lang")")
            return
        fi
    fi
    local timings="" last_output="" i
    for ((i = 0; i < REPEATS; i++)); do
        local result; result="$(run_once)"
        local ms="${result%%	*}"; local out="${result#*	}"
        timings="${timings}${ms}"$'\n'
        last_output="$out"
    done
    local stats_line median min max
    stats_line="$(printf '%s' "$timings" | grep -v '^$' | stats)"
    median="$(cut -f1 <<<"$stats_line")"
    min="$(cut -f2 <<<"$stats_line")"
    max="$(cut -f3 <<<"$stats_line")"
    local idx; idx="$(key_index "$case_name")"
    if [ "$idx" -ge 0 ]; then
        if [ "${OUTPUTS_VAL[$idx]}" != "$last_output" ]; then
            echo "benchmark $case_name returned different outputs:" >&2
            echo "  ${OUTPUTS_VAL[$idx]}" >&2
            echo "  $last_output" >&2
            exit 1
        fi
    else
        OUTPUTS_KEY+=("$case_name")
        OUTPUTS_VAL+=("$last_output")
    fi
    RESULTS+=("$(printf '%s\t%s\t%s\t%s\t%s\t%s' "$case_name" "$lang" "$median" "$min" "$max" "$last_output")")
}

if [ "$USE_DOCKER" -eq 1 ]; then
    prepull_docker_images
fi

for idx in "${!CASE_NAMES[@]}"; do
    case_name="${CASE_NAMES[$idx]}"
    if [ -n "$SELECTED_CASE" ] && [ "$SELECTED_CASE" != "$case_name" ]; then
        continue
    fi
    args="${CASE_ARGS[$idx]}"
    for lang in "${LANGS[@]}"; do
        run_case_lang "$case_name" "$args" "$lang"
    done
done

# ---- Output. ----

if [ "$OUTPUT_FORMAT" = "csv" ]; then
    echo "case,language,median_ms,min_ms,max_ms,output"
    for row in "${RESULTS[@]}"; do
        IFS=$'\t' read -r c l m mn mx o <<<"$row"
        printf '%s,%s,%s,%s,%s,"%s"\n' "$c" "$l" "$m" "$mn" "$mx" "$o"
    done
    exit 0
fi

printf 'case            language   median ms   min ms     max ms     output\n'
printf -- '--------------  ---------  ----------  ---------  ---------  ----------------\n'
for row in "${RESULTS[@]}"; do
    IFS=$'\t' read -r c l m mn mx o <<<"$row"
    printf '%-14s  %-9s  %10s  %9s  %9s  %s\n' "$c" "$l" "$m" "$mn" "$mx" "$o"
done
