#include "search.h"
#include <cassert>
#include <random>
#include <set>

bool oracle(std::string_view raw, std::string_view target,
            const typo::Search &s) {
  if (raw.size() != target.size() || raw == target)
    return false;
  for (size_t i = 0; i < raw.size(); i++) {
    std::string c(raw);
    if (i + 1 < raw.size()) {
      std::swap(c[i], c[i + 1]);
      if (c == target)
        return true;
    }
    for (int j = 0; j < 26; j++)
      if (s.adjacent[raw[i] - 'a'][j]) {
        c = raw;
        c[i] = 'a' + j;
        if (c == target)
          return true;
      }
  }
  return false;
}
int main() {
  typo::Search s;
  s.add("nihao", 100);
  s.add("nihaoma", 10);
  s.add("pinyin", 50);
  for (auto raw : {"nihoa", "nihso", "nhiao"}) {
    auto r = s.query(raw);
    assert(r.count == 1 && s.words[r.hits[0].word].code == "nihao");
  }
  assert(s.query("pinin").count == 0);
  assert(s.query("NIHAO").count == 0);
  auto r = s.query("nihao", 1);
  assert(r.exhausted && r.count == 0 && r.exact >= 0);
  std::mt19937 rng(42);
  typo::Search f;
  for (int i = 0; i < 150; i++) {
    std::string w(4, 'a');
    for (char &c : w)
      c = 'a' + rng() % 26;
    f.add(w, 1);
  }
  for (int t = 0; t < 10000; t++) {
    std::string raw = f.words[rng() % f.words.size()].code;
    size_t pos = rng() % 4;
    if (t % 2)
      raw[pos] = 'a' + rng() % 26;
    else if (pos < 3)
      std::swap(raw[pos], raw[pos + 1]);
    auto got = f.query(raw);
    assert(!got.exhausted);
    std::set<std::string> want, actual;
    for (auto &w : f.words)
      if (oracle(raw, w.code, f))
        want.insert(w.code);
    for (int i = 0; i < got.count; i++)
      actual.insert(f.words[got.hits[i].word].code);
    assert(want.size() < 8 && actual == want);
  }
  // Output cap must retain the eight highest priors with deterministic ties.
  typo::Search cap;
  for (int i = 0; i < 10; i++) {
    std::string w = "aaaaaaaaaa";
    w[i] = 's';
    cap.add(w, i);
  }
  auto top = cap.query("aaaaaaaaaa");
  assert(top.count == 8 && top.total == 10);
  assert(cap.words[top.hits[0].word].prior == 9);
}
