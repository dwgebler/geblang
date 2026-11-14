#!/usr/bin/env bash
#
# cross-build.sh - build a Geblang deployment binary for another OS/architecture,
# in any direction (Linux, macOS, and Windows hosts and targets).
#
# `geblang build` appends a platform-independent bundle to a geblang runtime, and
# the runtime is pure Go with no cgo. So this cross-compiles a runtime for the
# target with the Go toolchain, then embeds the bundle into it via
# `geblang build --runtime`. Run it from a geblang source checkout (it builds the
# runtime from ./cmd/geblang); the Go toolchain is the only requirement.
#
# Usage:
#   scripts/cross-build.sh --target <os/arch> --entry <module> --out <path> [options] [-- <build args>]
#
# Options:
#   --target <os/arch>   Target platform: <GOOS>/<GOARCH>, e.g. linux/amd64,
#                        darwin/arm64, windows/amd64. Required.
#   --entry <module>     Entry module exporting main(...). Required.
#   --out <path>         Output binary path (relative to your current directory).
#                        Required. Use a .exe suffix for windows targets.
#   --dir <pkgdir>       Package root containing geblang.yaml. Default: current dir.
#   --                   Pass the remaining args straight to `geblang build`
#                        (for example --no-assert).
#
# Examples:
#   scripts/cross-build.sh --target linux/amd64   --entry app.main --out build/app
#   scripts/cross-build.sh --target darwin/arm64  --entry app.main --out build/app
#   scripts/cross-build.sh --target windows/amd64 --entry app.main --out build/app.exe

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TARGET=""
ENTRY=""
OUT=""
PKGDIR="."
PASSTHRU=()

usage() {
	echo "usage: scripts/cross-build.sh --target <os/arch> --entry <module> --out <path>" >&2
	echo "       [--dir pkgdir] [-- build args]" >&2
}

while [ $# -gt 0 ]; do
	case "$1" in
		--target) TARGET="$2"; shift 2 ;;
		--target=*) TARGET="${1#*=}"; shift ;;
		--entry) ENTRY="$2"; shift 2 ;;
		--entry=*) ENTRY="${1#*=}"; shift ;;
		--out) OUT="$2"; shift 2 ;;
		--out=*) OUT="${1#*=}"; shift ;;
		--dir) PKGDIR="$2"; shift 2 ;;
		--dir=*) PKGDIR="${1#*=}"; shift ;;
		--) shift; PASSTHRU=("$@"); break ;;
		-h|--help) usage; exit 0 ;;
		*) echo "cross-build.sh: unknown option $1 (use -- to pass args to geblang build)" >&2; usage; exit 2 ;;
	esac
done

if [ -z "$TARGET" ] || [ -z "$ENTRY" ] || [ -z "$OUT" ]; then
	echo "cross-build.sh: --target, --entry and --out are required" >&2
	usage
	exit 2
fi

GOOS_T="${TARGET%%/*}"
GOARCH_T="${TARGET##*/}"
if [ -z "$GOOS_T" ] || [ -z "$GOARCH_T" ] || [ "$GOOS_T" = "$TARGET" ]; then
	echo "cross-build.sh: --target must be <os>/<arch>, e.g. linux/amd64 (got $TARGET)" >&2
	exit 2
fi

if ! command -v go >/dev/null 2>&1; then
	echo "cross-build.sh: the Go toolchain is required but was not found on PATH" >&2
	exit 1
fi

# Resolve the package dir and output against the caller's directory, before we
# cd into the source root to run the Go toolchain.
PKGDIR_ABS="$(cd "$PKGDIR" && pwd)"
case "$OUT" in
	/*) OUT_ABS="$OUT" ;;
	*)  OUT_ABS="$(pwd)/$OUT" ;;
esac

RUNTIME="$(mktemp "${TMPDIR:-/tmp}/geblang-runtime.XXXXXX")"
trap 'rm -f "$RUNTIME"' EXIT

echo "cross-build: compiling ${GOOS_T}/${GOARCH_T} runtime"
( cd "$ROOT" && GOOS="$GOOS_T" GOARCH="$GOARCH_T" CGO_ENABLED=0 go build -ldflags=-funcalign=64 -o "$RUNTIME" ./cmd/geblang )

echo "cross-build: embedding bundle (entry=${ENTRY}, dir=${PKGDIR_ABS})"
( cd "$ROOT" && go run ./cmd/geblang build --runtime "$RUNTIME" --entry "$ENTRY" --out "$OUT_ABS" ${PASSTHRU[@]+"${PASSTHRU[@]}"} "$PKGDIR_ABS" )

echo "cross-build: wrote ${OUT_ABS} for ${GOOS_T}/${GOARCH_T}"
