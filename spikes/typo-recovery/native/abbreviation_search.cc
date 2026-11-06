#include "abbreviation_search.h"
#include "dp_search.h"
#include "packed_image.h"
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
// Input/output text excludes ASCII controls, quote and backslash in
// preparation.
static bool safe(std::string_view s) {
  for (unsigned char c : s)
    if (c < 32 || c == '"' || c == '\\')
      return false;
  return true;
}
int main(int argc, char **argv) {
  const auto mode =
      argc >= 4 ? std::string_view(argv[3]) : std::string_view("plain");
  if ((argc < 3 || argc > 5) ||
      (mode != "plain" && mode != "radix" && mode != "strings" &&
       mode != "packed" && mode != "packed-strings" && mode != "save-packed" &&
       mode != "load-packed" && mode != "load-constrained" &&
       mode != "load-dp")) {
    std::cerr << "usage: abbreviation-search syllable-lexicon.tsv queries.txt "
                 "[plain|radix|strings|packed|packed-strings|save-packed|load-"
                 "packed|load-constrained|load-dp] [clean-quota:0..8]\n";
    return 1;
  }
  int clean_limit = 4;
  if (argc == 5) {
    if ((mode != "load-constrained" && mode != "load-dp") ||
        std::string_view(argv[4]).size() != 1 || argv[4][0] < '0' ||
        argv[4][0] > '8')
      return 1;
    clean_limit = argv[4][0] - '0';
  }
  std::ifstream lex(argv[1]), queries(argv[2]);
  if (!lex || (!queries && mode != "save-packed"))
    return 2;
  abbr::Search s;
  std::string line;
  uint64_t rows = 0;
  if (mode == "load-packed" || mode == "load-constrained" ||
      mode == "load-dp") {
    if (!abbr::loadPackedImage(s, argv[1]))
      return 8;
  } else {
    while (std::getline(lex, line)) {
      auto a = line.find('\t'), b = line.find('\t', a + 1);
      if (a == std::string::npos || b == std::string::npos)
        return 3;
      std::vector<std::string> syllables;
      std::istringstream split(line.substr(0, a));
      std::string syl;
      while (split >> syl)
        syllables.push_back(syl);
      std::string text = line.substr(a + 1, b - a - 1);
      if (!safe(text))
        return 4;
      double prior;
      std::istringstream weight(line.substr(b + 1));
      if (!(weight >> prior) || !s.add(syllables, text, prior))
        return 5;
      rows++;
    }
    s.freeze();
    if (mode == "radix")
      s.compressRadix();
    if (mode == "strings" || mode == "packed-strings" || mode == "save-packed")
      s.compactPayloads();
    if (mode == "packed" || mode == "packed-strings" || mode == "save-packed")
      s.packNodes();
    if (mode == "save-packed")
      return abbr::savePackedImage(s, argv[2]) ? 0 : 9;
  }
  std::vector<std::string> inputs;
  while (std::getline(queries, line)) {
    if (!typo::Search::valid(line))
      return 6;
    inputs.push_back(line);
  }
  abbr::Workspace workspace;
  abbr::DPWorkspace dp_workspace;
  const auto query = [&](std::string_view raw) {
    if (mode == "load-dp")
      return abbr::queryDP(s, raw, dp_workspace, clean_limit);
    return s.query(raw, workspace, 2048, 8192, mode == "load-constrained",
                   clean_limit);
  };
  uint64_t warm = 0;
  for (const auto &q : inputs)
    warm += query(q).states;
  std::cerr << "{\"input_rows\":" << rows << ",\"nodes\":" << s.nodes.size()
            << ",\"edges\":" << s.edges.size()
            << ",\"retained_words\":" << s.words.size()
            << ",\"syllables\":" << s.syllables.size()
            << ",\"node_bytes\":" << s.nodes.capacity() * sizeof(abbr::Node)
            << ",\"edge_bytes\":" << s.edges.capacity() * sizeof(abbr::Edge)
            << ",\"word_bytes\":" << s.words.capacity() * sizeof(abbr::Word)
            << ",\"text_capacity\":" << s.text_blob.capacity()
            << ",\"code_capacity\":" << s.code_blob.capacity()
            << ",\"radix_chains\":" << s.radix_chains.size()
            << ",\"radix_label_count\":" << s.radix_labels.size()
            << ",\"radix_chain_bytes\":"
            << s.radix_chains.capacity() * sizeof(abbr::RadixChain)
            << ",\"radix_ranked_bits_bytes\":"
            << s.radix_removed.bytes() + s.radix_starts.bytes()
            << ",\"radix_label_bytes\":"
            << s.radix_labels.capacity() * sizeof(uint16_t)
            << ",\"packed_branch_bytes\":"
            << s.packed_branches.capacity() * sizeof(abbr::PackedBranch)
            << ",\"packed_leaf_bytes\":"
            << s.packed_leaves.capacity() * sizeof(uint32_t)
            << ",\"packed_child_bytes\":"
            << s.packed_children.capacity() * sizeof(uint32_t)
            << ",\"packed_syllable_bytes\":"
            << s.packed_syllables.capacity() * sizeof(uint16_t)
            << ",\"packed_rank_bytes\":" << s.packed_leaf_bits.bytes()
            << ",\"packed_logical_nodes\":" << s.packed_logical_nodes
            << ",\"workspace_bytes\":"
            << (mode == "load-dp" ? sizeof(dp_workspace) : sizeof(workspace))
            << ",\"warm_states\":" << warm << "}\n";
  for (const auto &q : inputs) {
    auto before = allocations;
    auto start = std::chrono::steady_clock::now();
    auto r = query(q);
    auto ns = std::chrono::duration_cast<std::chrono::nanoseconds>(
                  std::chrono::steady_clock::now() - start)
                  .count();
    auto delta = allocations - before;
    std::cout << "{\"raw\":\"" << q << "\",\"states\":" << r.states
              << ",\"edges\":" << r.edges << ",\"peak_queue\":" << r.peak_queue
              << ",\"exhausted\":" << (r.exhausted ? "true" : "false")
              << ",\"ns\":" << ns << ",\"allocs\":" << delta;
    if (mode == "load-constrained")
      std::cout << ",\"pruned_bound\":" << r.pruned_bound
                << ",\"pruned_prefix\":" << r.pruned_prefix
                << ",\"lookahead_checks\":" << r.lookahead_checks;
    std::cout << ",\"hits\":[";
    bool comma = false;
    for (int group = 0; group < 2; group++)
      for (int i = 0; i < (group ? r.corrected : r.clean); i++) {
        auto h = r.hits[(group ? r.clean_limit : 0) + i];
        if (comma)
          std::cout << ',';
        comma = true;
        std::cout << "{\"text\":\"" << s.text(h.word) << "\",\"code\":\""
                  << s.code(h.word) << "\",\"cost\":" << int(h.cost)
                  << ",\"prior\":" << s.words[h.word].prior << '}';
      }
    std::cout << "]}\n";
  }
  return std::cout.good() ? 0 : 7;
}
