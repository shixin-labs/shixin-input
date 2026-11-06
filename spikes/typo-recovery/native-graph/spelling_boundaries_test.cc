#include "spelling_boundaries.h"
#include <cassert>
#include <random>
#include <string>
#include <vector>
int main() {
  const std::vector<std::string> dictionary = {"ni",  "hao",   "zhong", "guo",
                                               "shi", "chang", "cuo",   "wu",
                                               "n",   "h",     "zh",    "g"};
  const auto boundaries = [&](const std::string &raw) {
    return shixin_spike::ExactBoundaries(raw, "'", [&](size_t pos, auto emit) {
      for (const auto &s : dictionary)
        if (raw.compare(pos, s.size(), s) == 0)
          emit(s.size());
    });
  };
  std::mt19937 rng(8191);
  for (int i = 0; i < 10000; i++) {
    std::string raw;
    size_t begin = 0, end = 0;
    for (int part = 0; part < 3; part++) {
      if (part == 1)
        begin = raw.size();
      raw += dictionary[rng() % dictionary.size()];
      if (part == 1)
        end = raw.size();
    }
    raw[begin + rng() % (end - begin)] = 'a' + rng() % 26;
    assert(boundaries(raw).Allows(begin, end));
  }
  assert(boundaries("ni'hso").Allows(3, 6));
  assert(!boundaries("nihao").Allows(0, 1));
  assert(!boundaries(std::string(33, 'n')).Allows(0, 1));
}
