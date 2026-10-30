#!/usr/bin/env bash
# All CI gates (DESIGN §9), runnable locally. GitHub Actions runs it too.
#   scripts/ci.sh go      format, vet, build (no cgo), unit tests, race
#   scripts/ci.sh macos   + our librime, engine/generation tests on it incl. the
#                         rime-ice deploy (no-Lua gate) and the replay
#                         corpus (latency + policy gate), ASan/UBSan runs
#   scripts/ci.sh fuzz    libFuzzer on librime-shixin's parsers (a clang
#                         with libFuzzer, e.g. on Linux; elsewhere a random
#                         input driver)
# SHIXIN_PERF_TOLERANCE scales the latency budgets (default 1; CI runners
# are noisy, the workflow sets 2).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
# build/ is inside the Go module; downloads there (Boost) contain .go files.
mkdir -p "$ROOT/build" && [ -f "$ROOT/build/go.mod" ] || echo "module shixin/build" >"$ROOT/build/go.mod"
step() { printf '\n=== %s\n' "$*"; }

go_gates() {
  step "gofmt"
  out=$(gofmt -l cmd internal spikes tools); [ -z "$out" ] || { echo "$out"; exit 1; }
  step "go vet"
  go vet ./...
  step "build (CGO_ENABLED=0)"
  CGO_ENABLED=0 go build ./...
  step "no MyGo in engine packages"
  for p in ./cmd/shixin-engine ./cmd/shixin-builder ./cmd/shixin-ai ./internal/engine ./internal/generation ./internal/lifecycle ./internal/rime ./internal/ipc ./internal/policy ./internal/client ./internal/settings ./internal/rerank ./internal/ai ./internal/llama ./internal/models; do
    if go list -deps "$p" | grep egoist/mygo >/dev/null; then echo "$p imports MyGo" >&2; exit 1; fi
  done
  step "unit tests"
  go test -short ./...
  step "race"
  go test -race -short ./internal/...
}

macos_gates() {
  [ -f build/rime-data/rime_ice.schema.yaml ] || scripts/fetch-rime-data.sh
  step "librime + librime-shixin (C++ tests)"
  scripts/build-librime.sh --test
  local lib="$ROOT/build/librime/out/lib/librime.1.dylib"
  step "llama.cpp for shixin-ai: binding layout (model tests skip without the model)"
  scripts/build-llama.sh
  go test -count=1 ./internal/llama ./internal/ai
  step "our librime: dictionary lock conflicts, 以词定字, rime-ice deploy (no Lua), prism reuse, replay corpus"
  SHIXIN_LIBRIME="$lib" go test -count=1 ./internal/rime ./internal/engine ./internal/client ./internal/generation ./cmd/shixin-engine
  step "no Lua in the bundle"
  LIBRIME="$lib" scripts/bundle-macos.sh "$ROOT/build/ci/Shixin.app" >/dev/null
  if grep -rl "lua_" "$ROOT/build/ci/Shixin.app/Contents/Resources/rime-data/"*.custom.yaml 2>/dev/null; then exit 1; fi
  step "ASan + UBSan: librime tests and the engine on rime-ice"
  scripts/build-librime.sh --asan --test
  local rt
  rt=$(xcrun clang -print-file-name=libclang_rt.asan_osx_dynamic.dylib)
  go test -c -o build/ci/engine.test ./internal/engine
  (cd internal/engine && SHIXIN_LIBRIME="$ROOT/build/librime/asan/lib/librime.1.dylib" \
    DYLD_INSERT_LIBRARIES="$rt" SHIXIN_DYLD_INSERT="$rt" \
    ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
    SHIXIN_PERF_TOLERANCE=20 ../../build/ci/engine.test -test.count=1)
}

fuzz_gates() {
  mkdir -p build/ci
  local S=plugins/librime-shixin/src T=plugins/librime-shixin/test
  local cxx="${CXX:-clang++}" san="-fsanitize=address,undefined -fno-sanitize-recover=undefined"
  # Without libFuzzer (Apple clang), test/fuzz_driver.cc feeds random inputs.
  local fuzzer="-fsanitize=fuzzer" driver=""
  if ! echo 'extern "C" int LLVMFuzzerTestOneInput(const char*, unsigned long) { return 0; }' |
    $cxx -x c++ -fsanitize=fuzzer - -o build/ci/fuzz_probe 2>/dev/null; then
    # CI's Linux clang has libFuzzer; don't quietly fuzz less there.
    if [ -n "${GITHUB_ACTIONS:-}" ] && [ "$(uname)" = Linux ]; then echo "$cxx has no libFuzzer" >&2; exit 1; fi
    fuzzer="" driver="$T/fuzz_driver.cc"
  fi
  if [ -n "$driver" ]; then echo "no libFuzzer in $cxx: fuzzing with the random driver"; else echo "fuzzing with libFuzzer"; fi
  run() { # name, seconds or driver inputs, sources...
    local name=$1 n=$2; shift 2
    $cxx -std=c++17 -g -O1 $fuzzer $san -I$S "$@" $driver -o "build/ci/fuzz_$name"
    if [ -n "$driver" ]; then "build/ci/fuzz_$name" $((n * 50000)); else "build/ci/fuzz_$name" -max_total_time="$n"; fi
  }
  step "fuzz librime-shixin UTF-8 helpers"
  run utf8 60 $T/utf8_fuzz.cc
  step "fuzz librime-shixin parsers: calculator, numbers, code points"
  run parsers 120 $T/parsers_fuzz.cc $S/calc.cc $S/dates.cc $S/numerals.cc
}

case "${1:-go}" in
go) go_gates ;;
macos) go_gates; macos_gates ;;
fuzz) fuzz_gates ;;
*) echo "usage: scripts/ci.sh go|macos|fuzz" >&2; exit 2 ;;
esac
step "ok"
