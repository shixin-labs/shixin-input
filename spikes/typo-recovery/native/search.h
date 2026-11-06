#pragma once
#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

namespace typo {
struct Word {
  std::string code;
  double prior;
};
struct Node {
  std::array<int, 26> child{};
  int word = -1;
};
struct Hit {
  int word = -1;
  int cost = 0;
};
struct Result {
  std::array<Hit, 8> hits{};
  int count = 0, states = 0, total = 0;
  bool exhausted = false;
  int exact = -1;
};
class Search {
public:
  std::vector<Node> nodes{1};
  std::vector<Word> words;
  std::array<std::array<bool, 26>, 26> adjacent{};
  Search() {
    std::array<double, 26> x{}, y{};
    const char *rows[] = {"qwertyuiop", "asdfghjkl", "zxcvbnm"};
    double offsets[] = {0, .25, .75};
    for (int r = 0; r < 3; r++)
      for (int c = 0; rows[r][c]; c++) {
        int k = rows[r][c] - 'a';
        x[k] = c + offsets[r];
        y[k] = r;
      }
    for (int a = 0; a < 26; a++)
      for (int b = 0; b < 26; b++)
        adjacent[a][b] = a != b && std::hypot(x[a] - x[b], y[a] - y[b]) <= 1.3;
  }
  static bool valid(std::string_view s) {
    if (s.empty() || s.size() > 32)
      return false;
    for (char c : s)
      if (c < 'a' || c > 'z')
        return false;
    return true;
  }
  void add(std::string code, double prior) {
    if (!valid(code))
      return;
    int n = 0;
    for (char c : code) {
      int next = nodes[n].child[c - 'a'];
      if (!next) {
        next = static_cast<int>(nodes.size());
        nodes[n].child[c - 'a'] = next;
        nodes.emplace_back();
      }
      n = next;
    }
    if (nodes[n].word >= 0) {
      auto &p = words[nodes[n].word].prior;
      p = std::max(p, prior);
      return;
    }
    nodes[n].word = static_cast<int>(words.size());
    words.push_back({std::move(code), prior});
  }
  int exact(std::string_view s) const {
    int n = 0;
    for (char c : s) {
      n = nodes[n].child[c - 'a'];
      if (!n)
        return -1;
    }
    return nodes[n].word;
  }
  Result query(std::string_view s, int cap = 2048) const {
    Result r;
    if (!valid(s))
      return r;
    r.exact = exact(s);
    walk(s, 0, 0, 0, cap, r);
    if (r.exhausted) {
      r.count = 0;
      r.total = 0;
    }
    return r;
  }

private:
  bool better(Hit a, Hit b) const {
    if (a.cost != b.cost)
      return a.cost < b.cost;
    const auto &x = words[a.word];
    const auto &y = words[b.word];
    if (x.prior != y.prior)
      return x.prior > y.prior;
    return x.code < y.code;
  }
  void insert(Result &r, Hit h) const {
    for (int i = 0; i < r.count; i++)
      if (r.hits[i].word == h.word)
        return;
    r.total++;
    int i = 0;
    while (i < r.count && !better(h, r.hits[i]))
      i++;
    if (i >= 8)
      return;
    int end = std::min(r.count, 7);
    for (int j = end; j > i; j--)
      r.hits[j] = r.hits[j - 1];
    r.hits[i] = h;
    r.count = std::min(8, r.count + 1);
  }
  void walk(std::string_view s, int pos, int node, int cost, int cap,
            Result &r) const {
    if (r.exhausted)
      return;
    if (r.states >= cap) {
      r.exhausted = true;
      return;
    }
    r.states++;
    if (pos == static_cast<int>(s.size())) {
      if (cost && nodes[node].word >= 0)
        insert(r, {nodes[node].word, cost});
      return;
    }
    int a = s[pos] - 'a', next = nodes[node].child[a];
    if (next)
      walk(s, pos + 1, next, cost, cap, r);
    if (cost || r.exhausted)
      return;
    if (pos + 1 < static_cast<int>(s.size()) && s[pos] != s[pos + 1]) {
      int b = s[pos + 1] - 'a', n = nodes[node].child[b];
      if (n) {
        n = nodes[n].child[a];
        if (n)
          walk(s, pos + 2, n, 8, cap, r);
      }
    }
    for (int b = 0; b < 26 && !r.exhausted; b++)
      if (adjacent[a][b] && nodes[node].child[b])
        walk(s, pos + 1, nodes[node].child[b], 10, cap, r);
  }
};
} // namespace typo
