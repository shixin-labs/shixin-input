#include <gtest/gtest.h>
#include <atomic>
#include <cstdlib>
#include <iostream>
#include <unordered_map>
#include "mirror_cache.h"
static bool watch_allocations = false;
static size_t allocations = 0;
void *operator new(size_t n) {
  if (watch_allocations)
    ++allocations;
  if (auto p = std::malloc(n ? n : 1))
    return p;
  throw std::bad_alloc();
}
void operator delete(void *p) noexcept { std::free(p); }
void *operator new[](size_t n) { return ::operator new(n); }
void operator delete[](void *p) noexcept { ::operator delete(p); }
TEST(MirrorCache, OfflineRoundTripAndDictionaryFreeQueries) {
  const char *file = std::getenv("SHIXIN_SPIKE_TABLE");
  const char *output = std::getenv("SHIXIN_SPIKE_MIRROR_OUTPUT");
  if (!file || !output || std::getenv("SHIXIN_SPIKE_MIRROR_ONLY"))
    GTEST_SKIP();
  rime::Table table{rime::path(std::string(file))};
  ASSERT_TRUE(table.Load());
  auto *meta = table.metadata();
  shixin_spike::BoundIdentity id{meta->dict_file_checksum, meta->num_syllables,
                                 meta->num_entries, table.file_size()};
  std::vector<char> initials(id.syllables);
  for (size_t i = 0; i < initials.size(); ++i)
    initials[i] = table.GetSyllableById(i)[0];
  shixin_spike::InitialCache cache;
  ASSERT_TRUE(shixin_spike::LoadInitialCache(
      rime::path(std::string(file) + ".initial3"), id, initials, &cache));
  shixin_spike::MirrorCache built;
  built.native.resize(cache.entries.size());
  built.spans.resize(cache.entries.size());
  std::unordered_map<std::string, uint32_t> pool;
  for (size_t i = 0; i < cache.entries.size(); ++i) {
    auto &cached = cache.entries[i];
    rime::Code code;
    code.assign(cached.code.begin(), cached.code.end());
    auto native = table.QueryPhrases(code);
    auto *entry = table.Find<rime::table::Entry>(cached.offset);
    bool member = false;
    for (; !native.exhausted(); native.Next())
      if (native.entry() == entry) {
        member = true;
        break;
      }
    ASSERT_TRUE(member);
    ASSERT_EQ(entry->weight, cached.weight);
    auto text = table.GetEntryText(*entry);
    ASSERT_TRUE(shixin_spike::ValidMirrorUTF8(text));
    auto item = pool.emplace(text, uint32_t(built.text.size()));
    if (item.second) {
      built.text.insert(built.text.end(), text.begin(), text.end());
      built.text.push_back(0);
    }
    built.spans[i] = {item.first->second, uint32_t(text.size())};
    built.native[i] = *entry;
  }
  { // Test handoff fixtures; production generation authentication is separate.
    std::ofstream meta_out(std::string(output) + ".identity");
    meta_out << id.checksum << ' ' << id.syllables << ' ' << id.entries << ' '
             << id.table_bytes << '\n';
    std::ofstream initial_out(std::string(output) + ".initials",
                              std::ios::binary);
    initial_out.write(initials.data(), initials.size());
  }
  table.Close(); // All following query checks have no open native table.
  const rime::path path{std::string(output)};
  if (!std::filesystem::exists(path))
    ASSERT_TRUE(shixin_spike::SaveMirror(path, id, cache, built));
  shixin_spike::MirrorCache loaded;
  ASSERT_TRUE(shixin_spike::LoadMirror(path, id, cache, &loaded));
  ASSERT_EQ(loaded.native.size(), built.native.size());
  for (size_t i = 0; i < loaded.native.size(); ++i) {
    ASSERT_EQ(loaded.native[i].weight, built.native[i].weight);
    ASSERT_EQ(loaded.native[i].text.value, built.native[i].text.value);
    ASSERT_EQ(loaded.Text(i), built.Text(i));
  }
  shixin_spike::InitialCacheSearch search;
  size_t queries = 0, results = 0;
  std::vector<long long> timings;
  for (char a = 'a'; a <= 'z'; ++a)
    for (char b = 'a'; b <= 'z'; ++b)
      for (char c = 'a'; c <= 'z'; ++c) {
        std::array<char, 3> raw{a, b, c};
        bool valid = true;
        watch_allocations = true;
        auto start = std::chrono::steady_clock::now();
        auto found = search.Search({raw.data(), 3}, cache);
        for (size_t n = 0; n < found.count; ++n) {
          auto index = loaded.Find(found.entries[n], cache, initials);
          valid &= index < loaded.native.size();
          if (index < loaded.native.size()) {
            valid &= loaded.native[index].weight == found.entries[n].weight;
            valid &= loaded.Text(index) == built.Text(index);
          }
        }
        auto elapsed = std::chrono::steady_clock::now() - start;
        watch_allocations = false;
        ASSERT_TRUE(found.complete);
        ASSERT_TRUE(valid);
        timings.push_back(
            std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed)
                .count());
        ++queries;
        results += found.count;
      }
  EXPECT_EQ(allocations, 0u);
  EXPECT_FALSE(search.Search("xgr", cache, 0).complete);
  EXPECT_FALSE(
      search.Search("xgr", cache, 32768, std::chrono::steady_clock::now())
          .complete);
  std::sort(timings.begin(), timings.end());
  std::cout << "MIRROR queries=" << queries << " results=" << results
            << " allocations=" << allocations << " payload_bytes="
            << (cache.offsets.size() * 4 + cache.entries.size() * 16 +
                loaded.native.size() * 8 + loaded.spans.size() * 8 +
                loaded.text.size())
            << " p99_ns=" << timings[timings.size() * 99 / 100]
            << " max_ns=" << timings.back() << '\n';
  auto bad_id = id;
  ++bad_id.checksum;
  EXPECT_FALSE(shixin_spike::LoadMirror(path, bad_id, cache, &loaded));
  std::ifstream input(static_cast<const std::filesystem::path &>(path),
                      std::ios::binary);
  std::string original((std::istreambuf_iterator<char>(input)), {});
  const std::string corrupt = std::string(output) + ".corrupt";
  for (int mode = 0; mode < 8; ++mode) {
    auto data = original;
    if (mode == 0)
      data.resize(data.size() - 1);
    if (mode == 1)
      data.push_back(0);
    if (mode == 2)
      data[0] ^= 1;
    if (mode == 3)
      data[36] ^= 1; // Native entry identity mismatch.
    if (mode == 4)
      for (int n = 0; n < 4; ++n)
        data[44 + n] = char(0xff); // Span offset.
    if (mode == 5)
      for (int n = 0; n < 4; ++n)
        data[48 + n] = char(0xff); // Span length.
    if (mode == 6)
      data[36 + 16 * cache.entries.size()] = char(0xff); // Invalid UTF-8.
    if (mode == 7)
      for (int n = 0; n < 4; ++n)
        data[40 + n] = char(0xff); // Invalid text ID.
    {
      std::ofstream out(corrupt, std::ios::binary);
      out.write(data.data(), data.size());
    }
    EXPECT_FALSE(
        shixin_spike::LoadMirror(rime::path(corrupt), id, cache, &loaded))
        << mode;
  }
  std::filesystem::remove(corrupt);
}

TEST(MirrorCache, FreshImageOnly) {
  const char *output = std::getenv("SHIXIN_SPIKE_MIRROR_OUTPUT");
  const char *source = std::getenv("SHIXIN_SPIKE_INITIAL_CACHE");
  if (!output || !source || !std::getenv("SHIXIN_SPIKE_MIRROR_ONLY"))
    GTEST_SKIP();
  shixin_spike::BoundIdentity id{};
  std::ifstream meta(std::string(output) + ".identity");
  ASSERT_TRUE(bool(meta >> id.checksum >> id.syllables >> id.entries >>
                   id.table_bytes));
  ASSERT_GT(id.syllables, 0u);
  ASSERT_LE(id.syllables, 4096u);
  std::ifstream input(std::string(output) + ".initials", std::ios::binary);
  std::vector<char> initials(id.syllables);
  ASSERT_TRUE(bool(input.read(initials.data(), initials.size())));
  ASSERT_EQ(input.peek(), std::char_traits<char>::eof());
  shixin_spike::InitialCache cache;
  ASSERT_TRUE(shixin_spike::LoadInitialCache(rime::path(std::string(source)),
                                             id, initials, &cache));
  shixin_spike::MirrorCache mirror;
  ASSERT_TRUE(shixin_spike::LoadMirror(rime::path(std::string(output)), id,
                                       cache, &mirror));
  shixin_spike::InitialCacheSearch search;
  for (const std::string raw : {"xgr", "zhr"}) {
    auto began = std::chrono::steady_clock::now();
    watch_allocations = true;
    auto found = search.Search(raw, cache);
    auto first = found.count ? mirror.Find(found.entries[0], cache, initials)
                             : mirror.native.size();
    auto text =
        first < mirror.native.size() ? mirror.Text(first) : std::string_view{};
    auto elapsed = std::chrono::steady_clock::now() - began;
    watch_allocations = false;
    ASSERT_TRUE(found.complete);
    ASSERT_GT(found.count, 0u);
    ASSERT_EQ(text, "中国人");
    std::cout
        << "FRESH_MIRROR raw=" << raw << " ns="
        << std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed).count()
        << " text=" << text << " allocations=" << allocations << '\n';
  }
  EXPECT_EQ(allocations, 0u);
  std::cout << "FRESH_MIRROR retained_capacity_bytes="
            << (cache.offsets.capacity() * sizeof(uint32_t) +
                cache.entries.capacity() * sizeof(shixin_spike::CachedReading) +
                mirror.native.capacity() * sizeof(rime::table::Entry) +
                mirror.spans.capacity() * sizeof(shixin_spike::TextSpan) +
                mirror.text.capacity() + initials.capacity())
            << '\n';
}
