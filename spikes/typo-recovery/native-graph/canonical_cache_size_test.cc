#include <gtest/gtest.h>
#include <array>
#include <cmath>
#include <cstdlib>
#include <iostream>
#include <fstream>
#include <sstream>
#include <unordered_set>
#include <rime/dict/table.h>
#include "initial_cache.h"
class CanonicalInspectTable : public rime::Table {
public:
  using Table::Table;
  rime::table::Index *index() { return index_; }
  uint32_t Offset(const rime::table::Entry *e) const {
    return uint32_t(reinterpret_cast<const char *>(e) - address());
  }
};
TEST(CanonicalCacheSize, ReadingCapsAndFocusedTargets) {
  const char *file = std::getenv("SHIXIN_SPIKE_TABLE");
  if (!file)
    GTEST_SKIP();
  CanonicalInspectTable table{rime::path(std::string(file))};
  ASSERT_TRUE(table.Load());
  auto index = table.index();
  std::vector<char> initials(index->size);
  std::vector<std::string> syllables(index->size);
  for (size_t i = 0; i < index->size; ++i) {
    syllables[i] = table.GetSyllableById(i);
    initials[i] = syllables[i][0];
  }
  shixin_spike::InitialCache initial;
  shixin_spike::BoundIdentity id{table.metadata()->dict_file_checksum,
                                 index->size, table.metadata()->num_entries,
                                 table.file_size()};
  ASSERT_TRUE(shixin_spike::LoadInitialCache(
      rime::path(std::string(file) + ".initial3"), id, initials, &initial));
  std::unordered_set<uint32_t> initial_entries;
  for (const auto &e : initial.entries)
    initial_entries.insert(e.offset);
  struct Target {
    std::string text;
    std::array<std::string, 3> reading;
    size_t length;
    int32_t string_id = -1;
    size_t native_rank = 0;
  };
  std::vector<Target> targets{{"你好", {"ni", "hao", ""}, 2},
                              {"吊灯", {"diao", "deng", ""}, 2},
                              {"纯净物", {"chun", "jing", "wu"}, 3}};
  if (const char *labels = std::getenv("SHIXIN_SPIKE_CANONICAL_LABELS")) {
    std::ifstream input(labels);
    ASSERT_TRUE(input);
    std::string line;
    while (std::getline(input, line)) {
      auto tab = line.find('\t');
      ASSERT_NE(tab, std::string::npos);
      Target t;
      t.text = line.substr(tab + 1);
      t.length = 0;
      std::istringstream reading(line.substr(0, tab));
      std::string syllable;
      while (reading >> syllable) {
        ASSERT_LT(t.length, 3u);
        t.reading[t.length++] = syllable;
      }
      ASSERT_GE(t.length, 2u);
      ASSERT_FALSE(t.text.empty());
      targets.push_back(std::move(t));
    }
  }
  size_t target_number = 0, corpus_native = 0;
  for (auto &target : targets) {
    rime::Code code;
    for (size_t i = 0; i < target.length; ++i) {
      auto it =
          std::find(syllables.begin(), syllables.end(), target.reading[i]);
      if (it == syllables.end()) {
        code.clear();
        break;
      }
      code.push_back(it - syllables.begin());
    }
    auto a = table.QueryPhrases(code);
    size_t rank = 0;
    for (; !a.exhausted(); a.Next()) {
      ++rank;
      if (table.GetEntryText(*a.entry()) == target.text) {
        target.string_id = a.entry()->text.value;
        target.native_rank = rank;
        break;
      }
    }
    if (target_number < 3) {
      ASSERT_GE(target.string_id, 0);
    } else if (target.string_id >= 0)
      ++corpus_native;
    ++target_number;
    std::cout << "TARGET text=" << target.text
              << " native_reading_rank=" << target.native_rank << '\n';
  }
  for (bool hybrid : {true, false})
    for (size_t cap : {1, 2, 4}) {
      size_t records = 0, groups = 0, pool = 0, two_records = 0,
             three_records = 0;
      std::unordered_set<int32_t> texts;
      std::unordered_set<uint32_t> firsts, prefixes;
      std::vector<bool> retained(targets.size(), false);
      auto collect = [&](const rime::List<rime::table::Entry> &entries,
                         std::array<uint16_t, 3> code, size_t length) {
        std::array<const rime::table::Entry *, 4> best{};
        size_t n = 0;
        for (size_t i = 0; i < entries.size; ++i) {
          const auto *e = &entries.at[i];
          ASSERT_TRUE(std::isfinite(e->weight));
          if (hybrid && length == 3) {
            if (initial_entries.count(table.Offset(e)) == 0)
              continue;
            // Initial buckets can retain more than four words under one
            // reading.
          } else {
            size_t at = 0;
            while (at < n && (best[at]->weight > e->weight ||
                              (best[at]->weight == e->weight &&
                               table.Offset(best[at]) < table.Offset(e))))
              ++at;
            if (at >= cap)
              continue;
            if (n < cap)
              ++n;
            for (size_t j = n - 1; j > at; --j)
              best[j] = best[j - 1];
            best[at] = e;
            continue;
          }
          // This branch is counted in the second loop below to keep one
          // collector.
        }
        std::vector<const rime::table::Entry *> chosen;
        if (hybrid && length == 3) {
          for (size_t i = 0; i < entries.size; ++i)
            if (initial_entries.count(table.Offset(&entries.at[i])))
              chosen.push_back(&entries.at[i]);
        } else
          chosen.assign(best.begin(), best.begin() + n);
        if (chosen.empty())
          return;
        ++groups;
        firsts.insert(code[0]);
        if (length == 3)
          prefixes.insert((uint32_t(code[0]) << 16) | code[1]);
        for (const auto *e : chosen) {
          ++records;
          if (length == 2)
            ++two_records;
          else
            ++three_records;
          if (texts.insert(e->text.value).second)
            pool += table.GetEntryText(*e).size() + 1;
          for (size_t t = 0; t < targets.size(); ++t) {
            if (e->text.value != targets[t].string_id ||
                length != targets[t].length)
              continue;
            bool same = true;
            for (size_t p = 0; p < length; ++p)
              same &= syllables[code[p]] == targets[t].reading[p];
            retained[t] = retained[t] || same;
          }
        }
      };
      for (size_t a = 0; a < index->size; ++a) {
        auto next = index->at[a].next_level.get();
        if (!next)
          continue;
        auto two = &next->trunk();
        for (size_t b = 0; b < two->size; ++b) {
          auto &second = two->at[b];
          collect(second.entries, {uint16_t(a), uint16_t(second.key), 0}, 2);
          auto more = second.next_level.get();
          if (!more)
            continue;
          auto three = &more->trunk();
          for (size_t c = 0; c < three->size; ++c) {
            auto &third = three->at[c];
            collect(third.entries,
                    {uint16_t(a), uint16_t(second.key), uint16_t(third.key)},
                    3);
          }
        }
      }
      size_t prefix_nodes = firsts.size() + prefixes.size();
      size_t corpus_retained = 0;
      for (size_t t = 3; t < retained.size(); ++t)
        corpus_retained += retained[t];
      std::cout << "CORPUS_RETENTION variant="
                << (hybrid ? "hybrid" : "canonical") << " cap=" << cap
                << " labels=" << (targets.size() - 3)
                << " native_present=" << corpus_native
                << " retained=" << corpus_retained << '\n';
      for (size_t t = 3; t < targets.size(); ++t)
        if (targets[t].string_id >= 0 && !retained[t])
          std::cout << "DROPPED variant=" << (hybrid ? "hybrid" : "canonical")
                    << " cap=" << cap << " text=" << targets[t].text
                    << " native_reading_rank=" << targets[t].native_rank
                    << '\n';
      std::cout << "CANONICAL_SIZE variant="
                << (hybrid ? "two_reading_plus_initial_three"
                           : "two_and_three_reading")
                << " cap=" << cap << " records=" << records
                << " two_records=" << two_records
                << " three_records=" << three_records << " groups=" << groups
                << " unique_texts=" << texts.size()
                << " text_pool_bytes=" << pool
                << " prefix_nodes=" << prefix_nodes
                << " projected_payload_bytes="
                << (records * 20 + groups * 16 + prefix_nodes * 16 + pool)
                << " nihao=" << retained[0] << " diaodeng=" << retained[1]
                << " chunjingwu=" << retained[2] << '\n';
    }
}
