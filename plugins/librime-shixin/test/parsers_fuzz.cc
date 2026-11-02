// libFuzzer target for librime-shixin's parsers: the calculator, numbers
// and Unicode code points (build: scripts/ci.sh fuzz).
#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <string>
#include <string_view>
#include <vector>

#include "calc.h"
#include "codepoint.h"
#include "numerals.h"
#include "filter_rules.h"

extern "C" int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
  std::string_view s(reinterpret_cast<const char*>(data), size);
  shixin::FilterItem items[shixin::kFilterWindow];
  const size_t n = size < shixin::kFilterWindow ? size : shixin::kFilterWindow;
  for (size_t i = 0; i < n; i++)
    items[i] = {s.substr(i), "completion", int(data[i] % 11) - 1};
  for (auto kind :
       {shixin::FilterKind::LongWord, shixin::FilterKind::EnglishDemote,
        shixin::FilterKind::VSymbols, shixin::FilterKind::Pin}) {
    std::array<size_t, shixin::kFilterWindow> order{};
    shixin::filter_order(kind, items, n, &order);
    bool seen[shixin::kFilterWindow] = {};
    for (size_t i = 0; i < n; i++) {
      if (order[i] >= n || seen[order[i]])
        abort();
      seen[order[i]] = true;
    }
  }
  char capitalized[256];
  shixin::autocap_text(s.substr(0, size / 2), s.substr(size / 2), "completion",
                       capitalized, sizeof(capitalized));
  double v;
  if (shixin::calc_eval(s, &v)) {
    std::string out;
    shixin::calc_format(v, &out);
    if (out.empty() || out.size() > 32)
      abort();
  }
  std::vector<shixin::Reading> readings;
  shixin::number_readings(s, &readings);
  if (readings.size() > 4)
    abort();
  uint32_t cp;
  if (shixin::parse_hex(s, &cp) && shixin::is_printable(cp)) {
    std::string out;
    shixin::utf8_append(cp, &out);
    if (out.empty() || out.size() > 4)
      abort();
  }
  return 0;
}
