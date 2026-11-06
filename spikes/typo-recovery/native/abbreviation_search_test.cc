#include "abbreviation_search.h"
#include <cassert>
#include <random>
#include <set>

std::vector<std::string> forms(const std::vector<std::string> &syllables) {
  std::vector<std::string> all{""};
  for (auto &s : syllables) {
    std::set<std::string> pieces{s, s.substr(0, 1)};
    if (s.size() > 2 && (s.substr(0, 2) == "zh" || s.substr(0, 2) == "ch" ||
                         s.substr(0, 2) == "sh"))
      pieces.insert(s.substr(0, 2));
    std::vector<std::string> next;
    for (auto &a : all)
      for (auto &p : pieces)
        next.push_back(a + p);
    all.swap(next);
  }
  return all;
}
int distance(std::string raw, std::string target, const abbr::Search &s) {
  if (raw == target)
    return 0;
  if (raw.size() != target.size())
    return 100;
  for (size_t i = 0; i < raw.size(); i++) {
    if (i + 1 < raw.size()) {
      auto v = raw;
      std::swap(v[i], v[i + 1]);
      if (v == target)
        return 8;
    }
  }
  for (size_t i = 0; i < raw.size(); i++)
    for (char c = 'a'; c <= 'z'; c++)
      if (s.adjacent(raw[i], c)) {
        auto v = raw;
        v[i] = c;
        if (v == target)
          return 10;
      }
  return 100;
}
int main() {
  abbr::Workspace w;
  abbr::Search s;
  s.add({"ni", "hao"}, "你好", 100);
  s.freeze();
  for (auto raw : {"nihao", "nh", "nih", "nhao"}) {
    auto r = s.query(raw, w);
    assert(!r.exhausted && r.clean == 1 && s.text(r.hits[0].word) == "你好");
  }
  for (auto raw : {"nihoa", "nihso", "hn", "njh", "nhiao"}) {
    auto r = s.query(raw, w);
    assert(!r.exhausted && r.corrected == 1 &&
           s.text(r.hits[4].word) == "你好");
  }
  assert(s.query("NIH", w).clean == 0);
  auto exhausted = s.query("nh", w, 0);
  assert(exhausted.exhausted && exhausted.clean == 0 &&
         exhausted.corrected == 0);
  // Independent exhaustive form expansion is test-only; production search never
  // builds these combinations. A one-word lexicon avoids output truncation.
  std::mt19937 rng(49);
  std::vector<std::string> syllables{"ni", "hao", "zhong", "guo", "shi", "an"};
  for (int trial = 0; trial < 100; trial++) {
    std::vector<std::string> parts;
    for (int j = 0; j < 2 + trial % 2; j++)
      parts.push_back(syllables[rng() % syllables.size()]);
    abbr::Search x;
    x.add(parts, "词", 100);
    x.freeze();
    auto all = forms(parts);
    for (int i = 0; i < 100; i++) {
      auto raw = all[rng() % all.size()];
      size_t p = rng() % raw.size();
      if (i % 3 == 1 && p + 1 < raw.size())
        std::swap(raw[p], raw[p + 1]);
      if (i % 3 == 2)
        raw[p] = 'a' + rng() % 26;
      int want = 100;
      bool exact = false;
      for (auto &f : all) {
        int d = distance(raw, f, x);
        if (!d)
          exact = true;
        else
          want = std::min(want, d);
      }
      auto r = x.query(raw, w);
      assert(!r.exhausted);
      assert((r.clean == 1) == exact);
      assert((r.corrected == 1) == (want < 100));
      if (r.corrected)
        assert(r.hits[4].cost == want);
    }
  }
  // Four per category, deterministic prior order; source terminal lists also
  // truncate at four because lower priors cannot enter the same output quota.
  abbr::Search cap;
  for (int i = 0; i < 10; i++)
    cap.add({"ni", "hao"}, "word" + std::to_string(i), i);
  cap.freeze();
  auto top = cap.query("nh", w);
  assert(top.clean == 4 && cap.text(top.hits[0].word) == "word9");
  // Multi-word top-K property, including high-prior prefix words that cannot
  // consume the remaining raw code. Compare independently expanded forms.
  for (int trial = 0; trial < 50; trial++) {
    abbr::Search index;
    std::vector<std::vector<std::string>> spellings;
    for (int i = 0; i < 12; i++) {
      std::vector<std::string> parts;
      for (int j = 0; j < 1 + i % 3; j++)
        parts.push_back(syllables[rng() % syllables.size()]);
      spellings.push_back(forms(parts));
      index.add(parts, "w" + std::to_string(i), i + 1);
    }
    index.freeze();
    for (int q = 0; q < 50; q++) {
      int pick = rng() % 12;
      auto raw = spellings[pick][rng() % spellings[pick].size()];
      if (q % 2)
        raw[rng() % raw.size()] = 'a' + rng() % 26;
      struct Expected {
        std::string text;
        float score;
      };
      std::array<std::vector<Expected>, 2> want;
      for (int i = 0; i < 12; i++) {
        bool exact = false;
        int cost = 100;
        for (auto &form : spellings[i]) {
          int d = distance(raw, form, index);
          if (d == 0)
            exact = true;
          else
            cost = std::min(cost, d);
        }
        float prior = static_cast<float>(std::log1p(i + 1));
        if (exact)
          want[0].push_back({"w" + std::to_string(i), .3f * prior});
        if (cost < 100)
          want[1].push_back(
              {"w" + std::to_string(i), .3f * prior - .2f * cost});
      }
      auto result = index.query(raw, w);
      assert(!result.exhausted);
      for (int group = 0; group < 2; group++) {
        auto &expected = want[group];
        std::sort(expected.begin(), expected.end(),
                  [](const Expected &a, const Expected &b) {
                    if (a.score != b.score)
                      return a.score > b.score;
                    return a.text < b.text;
                  });
        if (expected.size() > 4)
          expected.resize(4);
        assert(expected.size() ==
               static_cast<size_t>(group ? result.corrected : result.clean));
        for (size_t i = 0; i < expected.size(); i++)
          assert(index.text(result.hits[group * 4 + i].word) ==
                 expected[i].text);
      }
    }
  }
}
