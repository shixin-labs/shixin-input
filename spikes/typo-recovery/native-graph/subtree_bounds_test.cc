// Read-only dictionary diagnostic. Not a runtime component or a complete IME.
#include <gtest/gtest.h>
#include <rime/dict/table.h>
#include <algorithm>
#include <array>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <queue>
#include <vector>
#include "subtree_bounds.h"
#include "subtree_bounds_io.h"
#include "initial_cache.h"
#include <atomic>
#include <new>
static std::atomic<size_t> allocations{0};
static bool count_allocations = false;
void *operator new(std::size_t n) {
  if (count_allocations)
    ++allocations;
  if (void *p = std::malloc(n ? n : 1))
    return p;
  throw std::bad_alloc();
}
void operator delete(void *p) noexcept { std::free(p); }
void *operator new[](std::size_t n) { return ::operator new(n); }
void operator delete[](void *p) noexcept { ::operator delete(p); }

class InspectTable : public rime::Table {
public:
  using Table::Table;
  rime::table::Index *index() { return index_; }
  uint32_t Offset(const rime::table::Entry *entry) const {
    return uint32_t(reinterpret_cast<const char *>(entry) - address());
  }
};
using shixin_spike::PrefixBound;
static float MaxEntry(const rime::List<rime::table::Entry> &entries) {
  float value = -std::numeric_limits<float>::infinity();
  for (size_t i = 0; i < entries.size; ++i)
    value = std::max(value, entries.at[i].weight);
  return value;
}
TEST(SubtreeBounds, ThreeInitialNeighborDiagnostic) {
  const char *path = std::getenv("SHIXIN_SPIKE_TABLE");
  if (!path)
    GTEST_SKIP();
  InspectTable table{rime::path(path)};
  ASSERT_TRUE(table.Load());
  auto index = table.index();
  std::vector<PrefixBound> bounds(index->size);
  std::vector<char> initial(index->size);
  size_t second_count = 0, third_count = 0, entries_count = 0;
  auto started = std::chrono::steady_clock::now();
  for (size_t a = 0; a < index->size; ++a) {
    auto spelling = table.GetSyllableById(a);
    initial[a] = spelling.empty() ? 0 : spelling[0];
    auto next = index->at[a].next_level.get();
    if (!next)
      continue;
    auto children = &next->trunk();
    bounds[a].children = children;
    bounds[a].best_three.assign(children->size,
                                -std::numeric_limits<float>::infinity());
    second_count += children->size;
    for (size_t b = 0; b < children->size; ++b) {
      auto more = children->at[b].next_level.get();
      if (!more)
        continue;
      auto leaves = &more->trunk();
      third_count += leaves->size;
      float best = -std::numeric_limits<float>::infinity();
      for (size_t c = 0; c < leaves->size; ++c) {
        best = std::max(best, MaxEntry(leaves->at[c].entries));
        entries_count += leaves->at[c].entries.size;
      }
      bounds[a].best_three[b] = best;
      bounds[a].best = std::max(bounds[a].best, best);
    }
  }
  std::cout << "BOUNDS syllables=" << index->size
            << " two_prefixes=" << second_count
            << " three_prefixes=" << third_count
            << " three_entries=" << entries_count
            << " float_bytes=" << (second_count + index->size) * sizeof(float)
            << " metadata_payload_bytes="
            << second_count * sizeof(float) +
                   bounds.size() * sizeof(PrefixBound) + initial.size()
            << " preprocessing_us="
            << std::chrono::duration_cast<std::chrono::microseconds>(
                   std::chrono::steady_clock::now() - started)
                   .count()
            << "\n";
  if (const char *output = std::getenv("SHIXIN_SPIKE_BOUNDS_OUTPUT")) {
    const shixin_spike::BoundIdentity identity{
        table.dict_file_checksum(), index->size, table.metadata()->num_entries,
        table.file_size()};
    ASSERT_TRUE(shixin_spike::SaveBounds(rime::path(output), identity, bounds));
    std::vector<PrefixBound> loaded;
    ASSERT_TRUE(
        shixin_spike::LoadBounds(rime::path(output), identity, index, &loaded));
    ASSERT_EQ(loaded.size(), bounds.size());
    for (size_t i = 0; i < bounds.size(); ++i) {
      EXPECT_EQ(loaded[i].children, bounds[i].children);
      EXPECT_EQ(loaded[i].best, bounds[i].best);
      EXPECT_EQ(loaded[i].best_three, bounds[i].best_three);
    }
    auto wrong = identity;
    wrong.table_bytes++;
    EXPECT_FALSE(
        shixin_spike::LoadBounds(rime::path(output), wrong, index, &loaded));
    std::cout << "SIDECAR bytes="
              << std::filesystem::file_size(rime::path(output))
              << " roundtrip=PASS identity_rejection=PASS\n";
  }
  shixin_spike::ThreeInitialSearch search;
  if (!search.Initialize(initial)) {
    for (size_t i = 0; i < initial.size(); ++i)
      std::cout << i << ":" << table.GetSyllableById(i) << " ";
    std::cout << "\n";
    FAIL() << "noncontiguous syllable initials";
  }
  std::cout << "FIXED_WORKSPACE_BYTES " << sizeof(search) << "\n";
  shixin_spike::Keyboard keyboard;
  auto edit = [&](char raw, char canonical) {
    if (raw == canonical)
      return 0;
    if (raw < 'a' || raw > 'z' || canonical < 'a' || canonical > 'z')
      return 2;
    return keyboard.adjacent[raw - 'a'][canonical - 'a'] ? 1 : 2;
  };
  for (std::string raw :
       {"xgr", "zhr", "zgt", "ngr", "zfr", "zge", "zgf", "zvr"}) {
    auto began = std::chrono::steady_clock::now();
    auto result = search.Search(raw, bounds, initial, 1000000);
    auto first_us = std::chrono::duration_cast<std::chrono::microseconds>(
                        std::chrono::steady_clock::now() - began)
                        .count();
    ASSERT_TRUE(result.complete);
    std::vector<shixin_spike::BoundCandidate> oracle;
    std::vector<long long> timings;
    size_t budget_complete = 0;
    for (int repeat = 0; repeat < 51; ++repeat) {
      allocations = 0;
      count_allocations = true;
      auto start = std::chrono::steady_clock::now();
      auto sample = search.Search(raw, bounds, initial);
      auto elapsed = std::chrono::duration_cast<std::chrono::nanoseconds>(
                         std::chrono::steady_clock::now() - start)
                         .count();
      count_allocations = false;
      EXPECT_EQ(allocations.load(), 0u);
      if (repeat) {
        timings.push_back(elapsed);
        budget_complete += sample.complete;
      }
    }
    std::sort(timings.begin(), timings.end());
    // Exhaustive reference, independent of search bounds and traversal order.
    for (size_t a = 0; a < index->size; ++a) {
      int e1 = edit(raw[0], initial[a]);
      if (e1 > 1 || !bounds[a].children)
        continue;
      auto children = bounds[a].children;
      for (size_t b = 0; b < children->size; ++b) {
        int e2 = e1 + edit(raw[1], initial[children->at[b].key]);
        auto next = children->at[b].next_level.get();
        if (e2 > 1 || !next)
          continue;
        auto leaves = &next->trunk();
        for (size_t c = 0; c < leaves->size; ++c) {
          if (e2 + edit(raw[2], initial[leaves->at[c].key]) != 1)
            continue;
          auto &entries = leaves->at[c].entries;
          for (size_t i = 0; i < entries.size; ++i)
            oracle.push_back({&entries.at[i],
                              {rime::SyllableId(a), children->at[b].key,
                               leaves->at[c].key}});
        }
      }
    }
    std::sort(oracle.begin(), oracle.end(), [](const auto &a, const auto &b) {
      if (a.entry->weight != b.entry->weight)
        return a.entry->weight > b.entry->weight;
      if (a.code != b.code)
        return a.code < b.code;
      return a.entry < b.entry;
    });
    if (oracle.size() > 8)
      oracle.resize(8);
    ASSERT_EQ(result.count, oracle.size());
    for (size_t i = 0; i < oracle.size(); ++i) {
      EXPECT_EQ(result.candidates[i].entry, oracle[i].entry) << raw;
      EXPECT_EQ(result.candidates[i].code, oracle[i].code) << raw;
    }
    auto capped = search.Search(raw, bounds, initial, 1);
    EXPECT_FALSE(capped.complete);
    EXPECT_EQ(capped.probes, 1u);
    std::cout << "QUERY " << raw << " states=" << result.visited
              << " probes=" << result.probes << " peak_states=" << result.peak
              << " first_us=" << first_us
              << " warm_p50_us=" << timings[25] / 1000.0
              << " warm_max_us=" << timings.back() / 1000.0
              << " cap32768_complete=" << budget_complete << "/50 top=";
    for (size_t i = 0; i < result.count; ++i)
      std::cout << table.GetEntryText(*result.candidates[i].entry) << ",";
    std::cout << "\n";
  }
  // Independent exhaustive oracle: enumerate every native three-syllable
  // entry, corrupt each initial to every adjacent key, then keep the best eight
  // for each possible raw code. This is test-only memory, not runtime storage.
  using Candidate = shixin_spike::BoundCandidate;
  std::vector<std::vector<Candidate>> expected(26 * 26 * 26);
  auto rank = [](const Candidate &a, const Candidate &b) {
    if (a.entry->weight != b.entry->weight)
      return a.entry->weight > b.entry->weight;
    if (a.code != b.code)
      return a.code < b.code;
    return a.entry < b.entry;
  };
  auto code_index = [](const std::array<char, 3> &code) {
    return (code[0] - 'a') * 26 * 26 + (code[1] - 'a') * 26 + code[2] - 'a';
  };
  size_t oracle_variants = 0;
  std::vector<std::vector<shixin_spike::CachedReading>> clean_cache(26 * 26 *
                                                                    26);
  for (size_t a = 0; a < index->size; ++a) {
    if (initial[a] < 'a' || initial[a] > 'z' || !bounds[a].children)
      continue;
    auto children = bounds[a].children;
    for (size_t b = 0; b < children->size; ++b) {
      auto next = children->at[b].next_level.get();
      auto bi = children->at[b].key;
      if (!next || initial[bi] < 'a' || initial[bi] > 'z')
        continue;
      auto leaves = &next->trunk();
      for (size_t c = 0; c < leaves->size; ++c) {
        auto ci = leaves->at[c].key;
        if (initial[ci] < 'a' || initial[ci] > 'z')
          continue;
        std::array<char, 3> canonical{initial[a], initial[bi], initial[ci]};
        auto &entries = leaves->at[c].entries;
        for (size_t i = 0; i < entries.size; ++i) {
          Candidate candidate{&entries.at[i], {rime::SyllableId(a), bi, ci}};
          shixin_spike::CachedReading cached{
              table.Offset(&entries.at[i]),
              {uint16_t(a), uint16_t(bi), uint16_t(ci)},
              0,
              entries.at[i].weight};
          auto &clean = clean_cache[code_index(canonical)];
          auto where = std::lower_bound(clean.begin(), clean.end(), cached,
                                        shixin_spike::BetterReading);
          if (where != clean.end() || clean.size() < 8) {
            clean.insert(where, cached);
            if (clean.size() > 8)
              clean.pop_back();
          }
          for (size_t p = 0; p < 3; ++p)
            for (char key = 'a'; key <= 'z'; ++key) {
              if (!keyboard.adjacent[canonical[p] - 'a'][key - 'a'])
                continue;
              auto corrupted = canonical;
              corrupted[p] = key;
              auto &list = expected[code_index(corrupted)];
              ++oracle_variants;
              auto at =
                  std::lower_bound(list.begin(), list.end(), candidate, rank);
              if (at == list.end() && list.size() == 8)
                continue;
              list.insert(at, candidate);
              if (list.size() > 8)
                list.pop_back();
            }
        }
      }
    }
  }
  shixin_spike::InitialCache cache;
  cache.offsets.push_back(0);
  for (const auto &clean : clean_cache) {
    cache.entries.insert(cache.entries.end(), clean.begin(), clean.end());
    cache.offsets.push_back(cache.entries.size());
  }
  if (const char *output = std::getenv("SHIXIN_SPIKE_CACHE_OUTPUT")) {
    shixin_spike::BoundIdentity identity{
        table.dict_file_checksum(), index->size, table.metadata()->num_entries,
        table.file_size()};
    ASSERT_TRUE(
        shixin_spike::SaveInitialCache(rime::path(output), identity, cache));
    shixin_spike::InitialCache loaded;
    ASSERT_TRUE(shixin_spike::LoadInitialCache(rime::path(output), identity,
                                               initial, &loaded));
    EXPECT_EQ(cache.offsets, loaded.offsets);
    EXPECT_EQ(cache.entries.size(), loaded.entries.size());
    auto wrong = identity;
    wrong.checksum ^= 1;
    EXPECT_FALSE(shixin_spike::LoadInitialCache(rime::path(output), wrong,
                                                initial, &loaded));
    std::ifstream source(output, std::ios::binary);
    std::string original((std::istreambuf_iterator<char>(source)),
                         std::istreambuf_iterator<char>());
    const size_t first_record =
        32 + 4 * (shixin_spike::InitialCache::codes + 1);
    for (int mode = 0; mode < 7; ++mode) {
      auto broken = original;
      if (mode == 0)
        broken.pop_back();
      if (mode == 1)
        broken.push_back('x');
      if (mode == 2)
        broken[8] ^= 1;
      if (mode == 3)
        broken[32] = 1;
      if (mode == 4) {
        broken[first_record + 12] = 0;
        broken[first_record + 13] = 0;
        broken[first_record + 14] = char(0xc0);
        broken[first_record + 15] = char(0x7f);
      }
      if (mode == 5)
        for (int byte = 0; byte < 4; ++byte)
          broken[first_record + byte] = char(0xff);
      if (mode == 6) {
        broken[first_record + 4] = 0;
        broken[first_record + 5] = 0;
      }
      std::string bad_path =
          std::string(output) + ".invalid-" + std::to_string(mode);
      {
        std::ofstream bad(bad_path, std::ios::binary);
        bad.write(broken.data(), broken.size());
      }
      EXPECT_FALSE(shixin_spike::LoadInitialCache(rime::path(bad_path),
                                                  identity, initial, &loaded))
          << mode;
    }
    ASSERT_TRUE(shixin_spike::LoadInitialCache(rime::path(output), identity,
                                               initial, &loaded));
    cache = std::move(loaded);
  }
  shixin_spike::InitialCacheSearch cache_search;
  size_t cache_max_probes = 0;
  // Every lowercase three-letter input: bounded storage, explicit exhaustion,
  // and every retained reading obeys the one-neighbor-substitution contract.
  size_t completed = 0, exhausted = 0, max_peak = 0, max_probes = 0,
         max_visited = 0;
  std::array<char, 3> raw{};
  for (char a = 'a'; a <= 'z'; ++a)
    for (char b = 'a'; b <= 'z'; ++b)
      for (char c = 'a'; c <= 'z'; ++c) {
        raw = {a, b, c};
        allocations = 0;
        count_allocations = true;
        auto result = search.Search(std::string_view(raw.data(), raw.size()),
                                    bounds, initial);
        count_allocations = false;
        ASSERT_EQ(allocations.load(), 0u);
        ASSERT_LE(result.peak, 4096u);
        ASSERT_LE(result.probes, 32768u);
        ASSERT_LE(result.count, 8u);
        const auto &exact = expected[code_index(raw)];
        allocations = 0;
        count_allocations = true;
        auto cached =
            cache_search.Search(std::string_view(raw.data(), 3), cache);
        count_allocations = false;
        ASSERT_EQ(allocations.load(), 0u);
        ASSERT_TRUE(cached.complete);
        ASSERT_EQ(cached.count, exact.size());
        cache_max_probes = std::max(cache_max_probes, cached.probes);
        for (size_t i = 0; i < cached.count; ++i) {
          ASSERT_EQ(cached.entries[i].offset, table.Offset(exact[i].entry));
          for (size_t p = 0; p < 3; ++p)
            ASSERT_EQ(cached.entries[i].code[p], exact[i].code[p]);
          ASSERT_EQ(cached.entries[i].weight, exact[i].entry->weight);
        }
        ASSERT_TRUE(result.complete);
        ASSERT_EQ(result.count, exact.size()) << std::string(raw.data(), 3);
        for (size_t i = 0; i < result.count; ++i) {
          ASSERT_EQ(result.candidates[i].entry, exact[i].entry)
              << std::string(raw.data(), 3);
          ASSERT_EQ(result.candidates[i].code, exact[i].code);
        }
        completed += result.complete;
        exhausted += !result.complete;
        max_peak = std::max(max_peak, result.peak);
        max_probes = std::max(max_probes, result.probes);
        max_visited = std::max(max_visited, result.visited);
        for (size_t i = 0; i < result.count; ++i) {
          const auto &found = result.candidates[i];
          int errors = 0;
          for (size_t p = 0; p < 3; ++p) {
            ASSERT_GE(found.code[p], 0);
            ASSERT_LT(size_t(found.code[p]), initial.size());
            errors += edit(raw[p], initial[found.code[p]]);
          }
          ASSERT_EQ(errors, 1);
          rime::Code code;
          code.assign(found.code.begin(), found.code.end());
          auto accessor = table.QueryPhrases(code);
          bool exists = false;
          for (; !accessor.exhausted(); accessor.Next())
            if (accessor.entry() == found.entry) {
              exists = true;
              break;
            }
          ASSERT_TRUE(exists);
        }
      }
  std::cout << "ALPHABET_SWEEP codes=" << completed + exhausted
            << " complete=" << completed << " exhausted=" << exhausted
            << " max_peak=" << max_peak << " max_probes=" << max_probes
            << " max_visited=" << max_visited
            << " oracle_variants=" << oracle_variants << "\n";
  std::cout << "INITIAL_CACHE records=" << cache.entries.size()
            << " payload_bytes="
            << cache.entries.size() * sizeof(shixin_spike::CachedReading) +
                   cache.offsets.size() * 4
            << " workspace_bytes=" << sizeof(cache_search)
            << " max_probes=" << cache_max_probes << " all_codes_exact=17576\n";
  EXPECT_FALSE(cache_search.Search("xgr", cache, 0).complete);
  EXPECT_FALSE(
      cache_search.Search("xgr", cache, 32768, std::chrono::steady_clock::now())
          .complete);
  EXPECT_FALSE(search.Search("ab", bounds, initial).complete);
  EXPECT_FALSE(search.Search("a'c", bounds, initial).complete);
  EXPECT_FALSE(search.Search("abc", bounds, initial, 0).complete);
  EXPECT_FALSE(search.Search("xgr", bounds, initial, 32768, 1).complete);
  EXPECT_FALSE(search
                   .Search("xgr", bounds, initial, 32768, 4096,
                           std::chrono::steady_clock::now())
                   .complete);
}
