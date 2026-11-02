#!/usr/bin/env bash
# All CI gates (DESIGN §9), runnable locally. GitHub Actions runs them too.
#   scripts/ci.sh go        format, vet, build (no cgo), unit tests, race
#   scripts/ci.sh race      short Go tests with the race detector, all packages
#   scripts/ci.sh macos     + our librime and its C++ tests, llama.cpp, the
#                           engine on our librime incl. the rime-ice deploy
#                           (no-Lua gate) and the replay corpus (latency +
#                           policy gate), the bundle's no-Lua check
#   scripts/ci.sh asan      librime under ASan/UBSan: its C++ tests, and the
#                           engine tests that exercise librime-shixin, without
#                           the long rime-ice runs (every push)
#   scripts/ci.sh asan-full the same with every engine test (weekly)
#   scripts/ci.sh fuzz      libFuzzer on librime-shixin's parsers (a clang
#                           with libFuzzer, e.g. on Linux; elsewhere a random
#                           input driver)
# SHIXIN_PERF_TOLERANCE scales the latency budgets (default 1). "off"
# turns the timing gates off: the CI workflow sets it, as shared runners
# can't time them reliably, and the ASan runs always do, as timings under a
# sanitizer mean nothing. Run the gates locally before merging.
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
  for p in ./cmd/shixin-engine ./cmd/shixin-builder ./cmd/shixin-ai ./internal/engine ./internal/generation ./internal/lifecycle ./internal/rime ./internal/ipc ./internal/policy ./internal/client ./internal/settings ./internal/dictionary ./internal/rerank ./internal/ai ./internal/llama ./internal/models ./internal/netclient ./internal/update ./internal/winsetup ./internal/dynlib ./internal/process; do
    if go list -deps "$p" | grep egoist/mygo >/dev/null; then echo "$p imports MyGo" >&2; exit 1; fi
  done
  step "unit tests"
  # Package concurrency otherwise makes the real-hardware timing gates
  # measure competing tests instead of the input engine.
  go test -p 1 -short ./...
  race_gates
}

race_gates() {
  step "race (all Go packages)"
  # Instrumented runs check concurrency, not latency. Product builds remain
  # CGO_ENABLED=0; the regular tests retain allocation and latency coverage.
  CGO_ENABLED=1 SHIXIN_PERF_TOLERANCE=off go test -p 1 -race -short ./...
}

macos_gates() {
  [ -f build/rime-data/rime_ice.schema.yaml ] || scripts/fetch-rime-data.sh
  step "librime + librime-shixin (C++ tests)"
  scripts/build-librime.sh --test
  local lib="$ROOT/build/librime/out/lib/librime.1.dylib"
  step "llama.cpp for shixin-ai: binding layout (model tests skip without the model)"
  scripts/build-llama.sh
  go test -p 1 -count=1 ./internal/llama ./internal/ai
  step "our librime: dictionary lock conflicts, 以词定字, rime-ice deploy (no Lua), prism reuse, replay corpus"
  SHIXIN_LIBRIME="$lib" go test -p 1 -count=1 ./internal/rime ./internal/engine ./internal/client ./internal/generation ./cmd/shixin-engine
  step "our librime: race detector on client, engine and coordinator"
  CGO_ENABLED=1 SHIXIN_PERF_TOLERANCE=off SHIXIN_LIBRIME="$lib" go test -p 1 -race -short -count=1 ./internal/rime ./internal/engine ./internal/client ./internal/generation ./cmd/shixin-engine
  step "no Lua in the bundle"
  LIBRIME="$lib" scripts/bundle-macos.sh "$ROOT/build/ci/Shixin.app" >/dev/null
  if grep -rl "lua_" "$ROOT/build/ci/Shixin.app/Contents/Resources/rime-data/"*.custom.yaml 2>/dev/null; then exit 1; fi
}

# The engine tests that only repeat rime-ice deploys and typing at scale.
# They run under ASan weekly (asan-full), and on every push without it.
long_engine_tests='^Test(Replay|RimeIce|RimeIceMixedPinyin)$'

asan_gates() { # full: run every engine test
  [ -f build/rime-data/rime_ice.schema.yaml ] || scripts/fetch-rime-data.sh
  step "ASan + UBSan: librime tests and the engine on rime-ice${1:+ (all tests)}"
  scripts/build-librime.sh --asan --test
  local rt skip=()
  rt=$(xcrun clang -print-file-name=libclang_rt.asan_osx_dynamic.dylib)
  [ -n "${1:-}" ] || skip=(-test.skip "$long_engine_tests")
  mkdir -p build/ci
  go test -c -o build/ci/engine.test ./internal/engine
  (cd internal/engine && SHIXIN_LIBRIME="$ROOT/build/librime/asan/lib/librime.1.dylib" \
    DYLD_INSERT_LIBRARIES="$rt" SHIXIN_DYLD_INSERT="$rt" \
    ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
    SHIXIN_PERF_TOLERANCE=off ../../build/ci/engine.test -test.count=1 ${skip[@]+"${skip[@]}"})
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
race) race_gates ;;
macos) go_gates; macos_gates ;;
asan) asan_gates ;;
asan-full) asan_gates full ;;
fuzz) fuzz_gates ;;
*) echo "usage: scripts/ci.sh go|race|macos|asan|asan-full|fuzz" >&2; exit 2 ;;
esac
step "ok"
