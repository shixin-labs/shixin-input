#include "abbreviation_search.h"
#include "dp_search.h"
#include <random>
#include <set>

static std::vector<std::string>
spellings(const std::vector<std::string> &syllables) {
  std::vector<std::string> all{""};
  for (auto &s : syllables) {
    std::set<std::string> forms{s, s.substr(0, 1)};
    if (s.size() > 2 && (s.substr(0, 2) == "zh" || s.substr(0, 2) == "ch" ||
                         s.substr(0, 2) == "sh"))
      forms.insert(s.substr(0, 2));
    std::vector<std::string> next;
    for (auto &a : all)
      for (auto &f : forms)
        next.push_back(a + f);
    all.swap(next);
  }
  return all;
}
static int distance(std::string raw, const std::string &target,
                    const abbr::Search &s) {
  if (raw == target)
    return 0;
  if (raw.size() != target.size())
    return 100;
  for (size_t i = 0; i + 1 < raw.size(); i++) {
    auto edited = raw;
    std::swap(edited[i], edited[i + 1]);
    if (edited == target)
      return 8;
  }
  for (size_t i = 0; i < raw.size(); i++)
    for (char c = 'a'; c <= 'z'; c++)
      if (s.adjacent(raw[i], c)) {
        auto edited = raw;
        edited[i] = c;
        if (edited == target)
          return 10;
      }
  return 100;
}
int main() {
  std::mt19937 rng(273);
  std::vector<std::string> syllables{"ni",  "hao", "zhong", "guo",
                                     "shi", "an",  "chao",  "lv"};
  struct Expected {
    std::string text;
    int cost;
    float score;
  };
  abbr::Workspace w;
  abbr::DPWorkspace dw;
  for (int trial = 0; trial < 100; trial++) {
    abbr::Search index;
    std::vector<std::vector<std::string>> forms;
    for (int i = 0; i < 12; i++) {
      std::vector<std::string> parts;
      for (int j = 0; j < 1 + i % 3; j++)
        parts.push_back(syllables[rng() % syllables.size()]);
      forms.push_back(spellings(parts));
      index.add(parts, "word" + std::to_string(i), i + 1);
    }
    index.freeze();
    index.compactPayloads();
    index.packNodes();
    for (int q = 0; q < 100; q++) {
      int pick = rng() % 12;
      auto raw = forms[pick][rng() % forms[pick].size()];
      if (q % 3 == 1)
        raw[rng() % raw.size()] = 'a' + rng() % 26;
      else if (q % 3 == 2 && raw.size() > 1) {
        size_t pos = rng() % (raw.size() - 1);
        std::swap(raw[pos], raw[pos + 1]);
      }
      std::array<std::vector<Expected>, 2> want;
      for (int i = 0; i < 12; i++) {
        bool exact = false;
        int cost = 100;
        for (auto &f : forms[i]) {
          int d = distance(raw, f, index);
          if (!d)
            exact = true;
          else
            cost = std::min(cost, d);
        }
        float prior = static_cast<float>(std::log1p(i + 1));
        if (exact)
          want[0].push_back({"word" + std::to_string(i), 0, .3f * prior});
        if (cost < 100)
          want[1].push_back(
              {"word" + std::to_string(i), cost, .3f * prior - .2f * cost});
      }
      int clean_limit = q % 9;
      auto result = index.query(raw, w, 2048, 8192, true, clean_limit);
      auto dp = abbr::queryDP(index, raw, dw, clean_limit);
      assert(!dp.exhausted);
      assert(dp.clean == result.clean && dp.corrected == result.corrected);
      for (int group = 0; group < 2; group++)
        for (int j = 0; j < (group ? dp.corrected : dp.clean); j++) {
          int pos = (group ? clean_limit : 0) + j;
          assert(index.text(dp.hits[pos].word) ==
                 index.text(result.hits[pos].word));
          assert(dp.hits[pos].cost == result.hits[pos].cost);
        }
      assert(!result.exhausted);
      assert(result.states <= 2048 && result.edges <= 8192);
      for (int group = 0; group < 2; group++) {
        auto &expected = want[group];
        std::sort(expected.begin(), expected.end(),
                  [](const Expected &a, const Expected &b) {
                    if (a.score != b.score)
                      return a.score > b.score;
                    return a.text < b.text;
                  });
        size_t limit = group ? 8 - clean_limit : clean_limit;
        if (expected.size() > limit)
          expected.resize(limit);
        assert(expected.size() ==
               static_cast<size_t>(group ? result.corrected : result.clean));
        for (size_t i = 0; i < expected.size(); i++) {
          auto h = result.hits[(group ? clean_limit : 0) + i];
          assert(index.text(h.word) == expected[i].text &&
                 h.cost == expected[i].cost);
        }
      }
    }
  }
  abbr::Search fixture;
  fixture.add({"ni", "hao"}, "你好", 100);
  fixture.freeze();
  for (auto raw : {"nh", "nih", "njh", "hn", "nhiao"}) {
    auto r = fixture.query(raw, w, 2048, 8192, true);
    assert(!r.exhausted && r.clean + r.corrected > 0);
  }
  auto limited = fixture.query("nh", w, 2048, 0, true);
  assert(limited.exhausted && limited.clean == 0 && limited.corrected == 0 &&
         limited.edges == 0);
  assert(fixture.query("n'h", w, 2048, 8192, true).clean == 0);
  auto dp_limited = abbr::queryDP(fixture, "nh", dw, 4, 2048, 0);
  assert(dp_limited.exhausted && !dp_limited.clean && !dp_limited.corrected);
  assert(!abbr::queryDP(fixture, "n'h", dw).clean);
}
