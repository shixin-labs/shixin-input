#include "abbreviation_search.h"
#include <random>

static void equivalent(const abbr::Result &a, const abbr::Result &b) {
  assert(a.clean == b.clean && a.corrected == b.corrected &&
         a.states == b.states && a.edges == b.edges &&
         a.peak_queue == b.peak_queue && a.exhausted == b.exhausted);
  for (int g = 0; g < 2; g++)
    for (int i = 0; i < (g ? a.corrected : a.clean); i++) {
      auto x = a.hits[g * 4 + i], y = b.hits[g * 4 + i];
      assert(x.word == y.word && x.cost == y.cost);
    }
}
int main() {
  // Rank at word boundaries and one past the last bit must avoid shift-by-64.
  abbr::RankedBits bits;
  bits.resize(192);
  for (uint32_t i = 0; i < 192; i++)
    if (i % 3 == 0)
      bits.set(i);
  bits.freeze();
  for (uint32_t i = 0; i <= 192; i++)
    assert(bits.before(i) == (i + 2) / 3);
  std::mt19937 rng(135);
  std::vector<std::string> syllables{"ni",  "hao", "zhong", "guo",
                                     "shi", "an",  "chao",  "lv"};
  for (int trial = 0; trial < 100; trial++) {
    abbr::Search plain;
    // Prefix terminals must survive compression; long nonterminal chains must
    // retain all abbreviation and cross-syllable swap boundaries.
    plain.add({"ni"}, "你", 10);
    plain.add({"ni", "hao"}, "你好", 100);
    plain.add({"ni", "hao", "zhong", "guo", "shi", "an"}, "长词", 500);
    for (int j = 0; j < 40; j++) {
      std::vector<std::string> parts;
      for (int k = 0; k < 1 + j % 6; k++)
        parts.push_back(syllables[rng() % syllables.size()]);
      plain.add(parts, "word" + std::to_string(j), j + 1);
    }
    plain.freeze();
    auto radix = plain;
    radix.compressRadix();
    for (uint32_t i = 0; i < plain.nodes.size(); i++) {
      auto a = plain.nodes[i], b = radix.nodeAt(i);
      assert(a.words == b.words && a.edges == b.edges && a.bound == b.bound &&
             a.descendant_bound == b.descendant_bound &&
             a.min_tail == b.min_tail && a.max_tail == b.max_tail);
      if (a.words)
        assert(a.word == b.word);
      for (int j = 0; j < a.edges; j++) {
        auto x = plain.edgeAt(a, j), y = radix.edgeAt(b, j);
        assert(x.child == y.child && x.syllable == y.syllable);
      }
    }
    abbr::Workspace x, y;
    for (auto raw : {"ni", "nh", "nih", "nhao", "hn", "nhiao", "nhzgsa",
                     "nhonggsa", "NIH", "n'h"})
      equivalent(plain.query(raw, x), radix.query(raw, y));
    for (int j = 0; j < 100; j++) {
      std::string raw;
      for (int k = 0; k < 1 + j % 5; k++) {
        auto &s = syllables[rng() % syllables.size()];
        raw += rng() % 2 ? s : s.substr(0, 1);
      }
      if (j % 3 == 0)
        raw[rng() % raw.size()] = 'a' + rng() % 26;
      int states = j % 7 == 0 ? 2 : 2048, edges = j % 11 == 0 ? 3 : 8192;
      equivalent(plain.query(raw, x, states, edges),
                 radix.query(raw, y, states, edges));
    }
  }
}
