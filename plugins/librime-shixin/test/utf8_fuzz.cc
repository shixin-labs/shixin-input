// libFuzzer target for the UTF-8 helpers (run: build with -DSHIXIN_FUZZ=ON).
#include <cstddef>
#include <cstdint>
#include <cstdlib>

#include "utf8_edges.h"

extern "C" int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
  std::string_view s(reinterpret_cast<const char*>(data), size);
  auto f = shixin::first_char(s);
  auto l = shixin::last_char(s);
  if (f.size() > s.size() || l.size() > s.size() || f.size() > 4 || l.size() > 4)
    abort();
  if (!s.empty() && (f.empty() || l.empty()))
    abort();
  (void)shixin::char_count(s);
  return 0;
}
