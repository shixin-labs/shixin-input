#!/usr/bin/env bash
# Builds our librime (DESIGN §2, §3.8): third_party/librime with
# plugins/librime-shixin merged in, its dependencies linked statically, and
# no Lua. Output: build/librime/out/lib/librime.1.dylib (macOS).
#
# The submodule stays clean: librime only links plugins found in its own
# plugins/ directory and builds its dependencies in place, so everything is
# built in a scratch copy under build/librime/src.
#
# Usage: scripts/build-librime.sh [--asan] [--test]
#   --asan  AddressSanitizer + UBSan build into build/librime/asan (librime's
#           own ENABLE_ASAN links -lasan, which only exists on Linux)
#   --test  run librime's and librime-shixin's C++ tests afterwards
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
W="$ROOT/build/librime"
S="$W/src"
OUT="$W/out"
SAN=""
TEST=0
for a in "$@"; do
  case "$a" in
  # No vptr check: LevelDB always builds with -fno-rtti, so its classes
  # have no typeinfo for UBSan to reference (link fails on macos-15 CI).
  --asan) SAN="-fsanitize=address,undefined -fno-sanitize=vptr -fno-omit-frame-pointer -fno-sanitize-recover=undefined"; OUT="$W/asan" ;;
  --test) TEST=1 ;;
  *) echo "unknown option $a" >&2; exit 2 ;;
  esac
done

BOOST_VERSION=1.89.0
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-13.0}"
export CMAKE_POLICY_VERSION_MINIMUM=3.5 # CMake 4 vs. old dependency CMakeLists
export CMAKE_GENERATOR=Ninja

# build/ is inside the Go module; downloads there (Boost) contain .go files.
mkdir -p "$ROOT/build" && [ -f "$ROOT/build/go.mod" ] || echo "module shixin/build" >"$ROOT/build/go.mod"
git -C "$ROOT" submodule update --init --recursive third_party/librime

# Scratch copy; keep previously built dependencies and Boost.
mkdir -p "$S"
rsync -a --delete --exclude .git \
  --filter 'P /lib/' --filter 'P /bin/' --filter 'P /share/' --filter 'P /include/*/' \
  --filter 'P /include/*.h*' --filter 'P /deps/*/build/' --filter 'P /deps/boost-*/' \
  --filter 'P /deps/*.tar.gz' --filter 'P /plugins/shixin' \
  "$ROOT/third_party/librime/" "$S/"
ln -sfn "$ROOT/plugins/librime-shixin" "$S/plugins/shixin"

if [ ! -d "$S/deps/boost-$BOOST_VERSION/boost" ]; then
  (cd "$S" && boost_version=$BOOST_VERSION ./install-boost.sh --download)
fi
if [ ! -f "$S/lib/libleveldb.a" ] || [ ! -f "$S/lib/libopencc.a" ] || [ ! -f "$S/share/opencc/s2t.json" ]; then
  # deps.mk doesn't quote paths, and ours contain spaces: build through a
  # space-free symlink.
  LINK="$(mktemp -d /tmp/shixin-librime.XXXXXX)/src"
  ln -s "$S" "$LINK"
  (cd "$LINK" && make -f deps.mk rime_root="$LINK")
  rm -f "$LINK"
fi

cmake -S "$S" -B "$OUT" \
  -DCMAKE_BUILD_TYPE=Release \
  -DBUILD_STATIC=ON \
  -DBUILD_SHARED_LIBS=ON \
  -DBUILD_MERGED_PLUGINS=ON \
  -DENABLE_EXTERNAL_PLUGINS=OFF \
  -DBUILD_TEST=ON \
  -DCMAKE_C_FLAGS="$SAN" -DCMAKE_CXX_FLAGS="$SAN" \
  -DCMAKE_SHARED_LINKER_FLAGS="$SAN" -DCMAKE_EXE_LINKER_FLAGS="$SAN" \
  -DBOOST_ROOT="$S/deps/boost-$BOOST_VERSION" \
  -DCMAKE_IGNORE_PREFIX_PATH="/opt/homebrew;/usr/local" \
  -DCMAKE_DISABLE_FIND_PACKAGE_Gflags=ON \
  -DCMAKE_OSX_DEPLOYMENT_TARGET="$MACOSX_DEPLOYMENT_TARGET" \
  -DCMAKE_INSTALL_PREFIX="$OUT/install"
# Headers from Homebrew (e.g. OpenCC 1.4 next to our 1.1.9) silently break
# the ABI; the first build crashed in Opencc::Initialize because of it.
if grep -q -e '-I/opt/homebrew' -e '-I/usr/local/include' "$OUT/build.ninja"; then
  echo "librime would compile against Homebrew/usr/local headers" >&2; exit 1
fi
cmake --build "$OUT"

LIB="$OUT/lib/librime.1.dylib"
echo "built $LIB"
# OpenCC's conversion data (简繁 via s2t.json), built with the deps. librime
# reads it from <shared data>/opencc; the bundle merges it into rime-data.
mkdir -p "$OUT/share/opencc"
rsync -a "$S/share/opencc/" "$OUT/share/opencc/"
# Self-contained: only system libraries.
if otool -L "$LIB" | tail -n +2 | grep -v -e '/usr/lib/' -e '/System/' -e '@rpath/librime' -e 'libclang_rt\.'; then
  echo "librime links non-system libraries" >&2; exit 1
fi
# Lua's C API and librime-lua's components; not -q, which closes the pipe
# early and, with pipefail, made this check never fire. A bare "lua" would
# match Evaluate.
if nm -gU "$LIB" | grep -E '(^|[^A-Za-z])(lua_|luaL_|luaopen_)|Lua(Translator|Filter|Processor|Segmentor)' >/dev/null; then
  echo "librime contains Lua symbols" >&2; exit 1
fi
if [ "$TEST" = 1 ]; then
  (cd "$OUT" && ctest --output-on-failure)
fi
