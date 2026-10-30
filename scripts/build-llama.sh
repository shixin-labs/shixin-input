#!/usr/bin/env bash
# Builds llama.cpp (third_party/llama.cpp, MIT) for shixin-ai (DESIGN §7.2):
# shared libraries that find each other next to libllama (@loader_path),
# the Metal backend with its shaders embedded, and nothing else: no tools,
# no network (curl/OpenSSL), no OpenMP, no BLAS. The CPU backend stays, as
# llama.cpp needs it for host-side ops; shixin-ai puts every layer on the
# GPU. Output: build/llama/lib/libllama.dylib (macOS) and its libggml*.
#
# Usage: scripts/build-llama.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/third_party/llama.cpp"
OUT="$ROOT/build/llama/out"
LIB="$ROOT/build/llama/lib"
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-13.0}"
export CMAKE_GENERATOR=Ninja

mkdir -p "$ROOT/build" && [ -f "$ROOT/build/go.mod" ] || echo "module shixin/build" >"$ROOT/build/go.mod"
[ -f "$SRC/CMakeLists.txt" ] || git -C "$ROOT" submodule update --init --depth 1 third_party/llama.cpp

# The libraries are up to date when built from this commit by this script
# (CI restores them from its cache; a fresh checkout's new source times
# would otherwise make Ninja rebuild everything).
STAMP="$(git -C "$SRC" rev-parse HEAD) $(shasum -a 256 "$0" | cut -d' ' -f1)"
if [ -f "$LIB/libllama.dylib" ] && [ "$(cat "$LIB/.stamp" 2>/dev/null)" = "$STAMP" ]; then
  echo "up to date: $LIB/libllama.dylib"
  exit 0
fi

# A build directory configured from another checkout can't be reused.
if [ -f "$OUT/CMakeCache.txt" ] && ! grep -qxF "CMAKE_HOME_DIRECTORY:INTERNAL=$SRC" "$OUT/CMakeCache.txt"; then
  rm -rf "$OUT"
fi
cmake -S "$SRC" -B "$OUT" \
  -DCMAKE_BUILD_TYPE=Release \
  -DBUILD_SHARED_LIBS=ON \
  -DCMAKE_INSTALL_RPATH=@loader_path \
  -DCMAKE_BUILD_WITH_INSTALL_RPATH=ON \
  -DLLAMA_BUILD_COMMON=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_TOOLS=OFF \
  -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=OFF -DLLAMA_BUILD_APP=OFF \
  -DLLAMA_BUILD_UI=OFF -DLLAMA_OPENSSL=OFF \
  -DGGML_METAL=ON -DGGML_METAL_EMBED_LIBRARY=ON -DGGML_NATIVE=OFF \
  -DGGML_OPENMP=OFF -DGGML_BLAS=OFF -DGGML_ACCELERATE=OFF -DGGML_RPC=OFF \
  -DGGML_BACKEND_DL=OFF \
  -DCMAKE_IGNORE_PREFIX_PATH="/opt/homebrew;/usr/local" \
  -DCMAKE_OSX_DEPLOYMENT_TARGET="$MACOSX_DEPLOYMENT_TARGET" >/dev/null
# As for librime: Homebrew headers silently break the ABI.
if grep -q -e '-I/opt/homebrew' -e '-I/usr/local/include' "$OUT/build.ninja"; then
  echo "llama.cpp would compile against Homebrew/usr/local headers" >&2; exit 1
fi
cmake --build "$OUT" --target llama

rm -rf "$LIB" && mkdir -p "$LIB"
find "$OUT/bin" -maxdepth 1 -name '*.dylib' -exec cp -P {} "$LIB/" \;
# Self-contained: system libraries and each other only.
for f in "$LIB"/*.dylib; do
  [ -L "$f" ] && continue
  if otool -L "$f" | tail -n +2 | grep -v -e '/usr/lib/' -e '/System/' -e '@rpath/lib\(llama\|ggml\)'; then
    echo "$f links non-system libraries" >&2; exit 1
  fi
done
echo "$STAMP" >"$LIB/.stamp"
echo "built $LIB/libllama.dylib"
