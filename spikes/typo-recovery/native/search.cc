#include "search.h"
#include <chrono>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <new>
#include <sstream>

static uint64_t allocations = 0;
void *operator new(std::size_t n) {
  ++allocations;
  if (void *p = std::malloc(n ? n : 1))
    return p;
  throw std::bad_alloc();
}
void *operator new[](std::size_t n) { return ::operator new(n); }
void operator delete(void *p) noexcept { std::free(p); }
void operator delete[](void *p) noexcept { std::free(p); }
int main(int argc, char **argv) {
  if (argc != 3) {
    std::cerr << "usage: search lexicon.tsv queries.txt\n";
    return 1;
  }
  typo::Search s;
  std::ifstream lex(argv[1]), inputs(argv[2]);
  if (!lex || !inputs)
    return 2;
  std::string line;
  while (std::getline(lex, line)) {
    std::istringstream in(line);
    std::string code;
    double weight;
    if (!(in >> code >> weight) || !typo::Search::valid(code)) {
      std::cerr << "invalid lexicon row\n";
      return 3;
    }
    s.add(code, weight);
  }
  std::vector<std::string> queries;
  while (std::getline(inputs, line)) {
    if (!typo::Search::valid(line)) {
      std::cerr << "invalid query\n";
      return 4;
    }
    queries.push_back(line);
  }
  uint64_t warm = 0;
  for (const auto &q : queries) {
    auto r = s.query(q);
    warm += r.states;
  } // touch all search paths
  std::cerr << "words=" << s.words.size() << " nodes=" << s.nodes.size()
            << " node_bytes=" << s.nodes.size() * sizeof(typo::Node)
            << " warm_states=" << warm << "\n";
  for (const auto &q : queries) {
    uint64_t a = allocations;
    auto start = std::chrono::steady_clock::now();
    auto r = s.query(q);
    auto end = std::chrono::steady_clock::now();
    uint64_t delta = allocations - a;
    auto ns = std::chrono::duration_cast<std::chrono::nanoseconds>(end - start)
                  .count();
    std::cout << "{\"raw\":\"" << q << "\",\"states\":" << r.states
              << ",\"exhausted\":" << (r.exhausted ? "true" : "false")
              << ",\"exact\":" << (r.exact >= 0 ? "true" : "false")
              << ",\"total\":" << r.total << ",\"ns\":" << ns
              << ",\"allocs\":" << delta << ",\"hits\":[";
    for (int i = 0; i < r.count; i++) {
      if (i)
        std::cout << ',';
      auto h = r.hits[i];
      std::cout << "{\"code\":\"" << s.words[h.word].code
                << "\",\"cost\":" << h.cost
                << ",\"prior\":" << s.words[h.word].prior << '}';
    }
    std::cout << "]}\n";
  }
  return std::cout.good() ? 0 : 5;
}
