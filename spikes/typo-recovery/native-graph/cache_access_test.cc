#include <gtest/gtest.h>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <unordered_set>
#include <rime/dict/table.h>
#include "initial_cache.h"

// Read-only diagnosis of mapped table access. No engine/session/user dictionary.
TEST(SpikeCacheAccess, NativeReadingValidationAndText) {
  const char* file = std::getenv("SHIXIN_SPIKE_TABLE");
  if (!file) GTEST_SKIP();
  const char* raw_env = std::getenv("SHIXIN_SPIKE_RAW");
  const std::string raw = raw_env ? raw_env : "xgr";
  rime::Table table{rime::path(std::string(file))};
  ASSERT_TRUE(table.Load());
  const auto* meta = table.metadata();
  shixin_spike::BoundIdentity identity{meta->dict_file_checksum,
      meta->num_syllables, meta->num_entries, table.file_size()};
  std::vector<char> initials(meta->num_syllables);
  for (size_t i = 0; i < initials.size(); ++i) {
    auto spelling = table.GetSyllableById(i);
    initials[i] = spelling.empty() ? 0 : spelling[0];
  }
  shixin_spike::InitialCache cache;
  ASSERT_TRUE(shixin_spike::LoadInitialCache(
      rime::path(std::string(file) + ".initial3"), identity, initials, &cache));
  if (std::getenv("SHIXIN_SPIKE_CACHE_SIZE")) {
    std::unordered_set<std::string> texts;
    size_t pool_bytes = 0;
    for (const auto& candidate : cache.entries) {
      rime::Code code;
      code.assign(candidate.code.begin(), candidate.code.end());
      auto native = table.QueryPhrases(code);
      auto* entry = table.Find<rime::table::Entry>(candidate.offset);
      bool member = false;
      for (; !native.exhausted(); native.Next())
        if (native.entry() == entry) { member = true; break; }
      ASSERT_TRUE(member);
      ASSERT_EQ(entry->weight, candidate.weight);
      auto text = table.GetEntryText(*entry);
      ASSERT_FALSE(text.empty());
      if (texts.insert(text).second) pool_bytes += text.size() + 1;
    }
    const size_t records = cache.entries.size();
    const size_t index_bytes = cache.offsets.size() * sizeof(uint32_t);
    std::cout << "MIRROR_SIZE records=" << records << " unique_texts=" << texts.size()
              << " text_pool_bytes=" << pool_bytes << " index_bytes=" << index_bytes
              << " native_entry_bytes=" << sizeof(rime::table::Entry)
              << " projected_payload_28byte_records=" << (index_bytes + 28 * records + pool_bytes)
              << " projected_payload_32byte_records=" << (index_bytes + 32 * records + pool_bytes)
              << " verified_native_readings=" << records << '\n';
    return;
  }
  shixin_spike::InitialCacheSearch search;
  auto found = search.Search(raw, cache);
  ASSERT_TRUE(found.complete);
  ASSERT_GT(found.count, 0u);
  using Clock = std::chrono::steady_clock;
  auto us = [](Clock::duration elapsed) {
    return std::chrono::duration_cast<std::chrono::microseconds>(elapsed).count();
  };
  for (int repeat = 0; repeat < 2; ++repeat) {
    for (size_t i = 0; i < found.count; ++i) {
      const auto& candidate = found.entries[i];
      rime::Code code;
      code.assign(candidate.code.begin(), candidate.code.end());
      auto begin = Clock::now();
      auto native = table.QueryPhrases(code);
      auto queried = Clock::now();
      auto* entry = table.Find<rime::table::Entry>(candidate.offset);
      bool member = false;
      size_t scans = 0;
      for (; !native.exhausted(); native.Next()) {
        ++scans;
        if (native.entry() == entry) { member = true; break; }
      }
      auto validated = Clock::now();
      ASSERT_TRUE(member);
      ASSERT_EQ(entry->weight, candidate.weight);
      auto weighted = Clock::now();
      auto text = table.GetEntryText(*entry);
      auto decoded = Clock::now();
      std::cout << "ACCESS raw=" << raw << " repeat=" << repeat << " entry=" << i
                << " query_us=" << us(queried - begin)
                << " member_us=" << us(validated - queried)
                << " weight_us=" << us(weighted - validated)
                << " text_us=" << us(decoded - weighted)
                << " scans=" << scans << " text=" << text << '\n';
    }
  }
}
