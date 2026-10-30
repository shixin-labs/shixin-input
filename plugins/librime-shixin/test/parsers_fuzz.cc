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

extern "C" int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size) {
  std::string_view s(reinterpret_cast<const char*>(data), size);
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
