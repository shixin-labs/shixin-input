// Offline layout-only repacking of trusted native artifacts. No Rime sessions.
#include <gtest/gtest.h>
#include <cstdlib>
#include <cstring>
#include <functional>
#include <iostream>
#include <rime/dict/table.h>

class RepackTable : public rime::Table {
public:
  using rime::MappedFile::Allocate;
  using rime::MappedFile::Create;
  using rime::MappedFile::CreateArray;
  using rime::Table::index_;
  using rime::Table::metadata_;
  using rime::Table::syllabary_;
  using Table::Table;
};

TEST(RepackTable, PreserveAllEntriesAndCodes) {
  const char *input = std::getenv("SHIXIN_SPIKE_TABLE");
  const char *output = std::getenv("SHIXIN_SPIKE_REPACK_TABLE");
  if (!input || !output)
    GTEST_SKIP();
  RepackTable source{rime::path(std::string(input))};
  ASSERT_TRUE(source.Load());
  ASSERT_GT(source.metadata()->string_table_size, 0u);
  RepackTable dest{rime::path(std::string(output))};
  if (!dest.Exists()) {
    ASSERT_TRUE(dest.Create(source.file_size() * 2));
    dest.metadata_ = dest.Allocate<rime::table::Metadata>();
    ASSERT_NE(dest.metadata_, nullptr);
    std::memcpy(dest.metadata_->format, source.metadata()->format,
                sizeof(dest.metadata_->format));
    dest.metadata_->dict_file_checksum = source.metadata()->dict_file_checksum;
    dest.metadata_->num_syllables = source.metadata()->num_syllables;
    dest.metadata_->num_entries = source.metadata()->num_entries;
    dest.metadata_->reserved_1 = source.metadata()->reserved_1;
    dest.metadata_->reserved_2 = source.metadata()->reserved_2;
    dest.metadata_->string_table_size = source.metadata()->string_table_size;
    dest.syllabary_ = dest.CreateArray<rime::table::StringType>(
        source.metadata()->num_syllables);
    for (size_t i = 0; i < source.metadata()->num_syllables; ++i)
      dest.syllabary_->at[i].str_id() = source.syllabary_->at[i].str_id();
    dest.metadata_->syllabary = dest.syllabary_;
    dest.index_ =
        dest.CreateArray<rime::table::HeadIndexNode>(source.index_->size);
    dest.metadata_->index = dest.index_;
    using List = rime::List<rime::table::Entry>;
    using Tail = rime::table::TailIndex;
    using Next = rime::OffsetPtr<rime::table::PhraseIndex>;
    std::vector<std::pair<const List *, List *>> entry_jobs;
    std::vector<std::pair<const Tail *, Next *>> tail_jobs;
    std::function<void(const rime::table::TrunkIndex *, Next *, int)>
        copy_index;
    copy_index = [&](const auto *old, Next *next, int depth) {
      auto *fresh = dest.CreateArray<rime::table::TrunkIndexNode>(old->size);
      *next = reinterpret_cast<rime::table::PhraseIndex *>(fresh);
      for (size_t i = 0; i < old->size; ++i) {
        const auto &a = old->at[i];
        auto &b = fresh->at[i];
        b.key = a.key;
        entry_jobs.push_back({&a.entries, &b.entries});
        if (!a.next_level)
          continue;
        if (depth == 3)
          tail_jobs.push_back({&a.next_level->tail(), &b.next_level});
        else
          copy_index(&a.next_level->trunk(), &b.next_level, depth + 1);
      }
    };
    for (size_t i = 0; i < source.index_->size; ++i) {
      const auto &a = source.index_->at[i];
      auto &b = dest.index_->at[i];
      entry_jobs.push_back({&a.entries, &b.entries});
      if (a.next_level)
        copy_index(&a.next_level->trunk(), &b.next_level, 2);
    }
    const size_t index_end = dest.file_size();
    auto *strings = dest.Allocate<char>(source.metadata()->string_table_size);
    std::memcpy(strings, source.metadata()->string_table.get(),
                source.metadata()->string_table_size);
    dest.metadata_->string_table = strings;
    for (const auto &job : entry_jobs) {
      job.second->size = job.first->size;
      if (!job.first->size)
        continue;
      auto *entries = dest.Allocate<rime::table::Entry>(job.first->size);
      std::memcpy(entries, job.first->at.get(),
                  job.first->size * sizeof(*entries));
      job.second->at = entries;
    }
    for (const auto &job : tail_jobs) {
      auto *tail = dest.CreateArray<rime::table::LongEntry>(job.first->size);
      *job.second = reinterpret_cast<rime::table::PhraseIndex *>(tail);
      for (size_t i = 0; i < job.first->size; ++i) {
        const auto &a = job.first->at[i];
        auto &b = tail->at[i];
        std::memcpy(&b.entry, &a.entry, sizeof(b.entry));
        b.extra_code.size = a.extra_code.size;
        if (a.extra_code.size) {
          auto *code = dest.Allocate<rime::SyllableId>(a.extra_code.size);
          std::memcpy(code, a.extra_code.at.get(),
                      a.extra_code.size * sizeof(*code));
          b.extra_code.at = code;
        }
      }
    }
    std::cout << "REPACK_LAYOUT index_end_bytes=" << index_end
              << " string_bytes=" << source.metadata()->string_table_size
              << " output_bytes=" << dest.file_size()
              << " entry_lists=" << entry_jobs.size()
              << " tail_lists=" << tail_jobs.size() << '\n';
    ASSERT_TRUE(dest.Save());
    dest.Close();
  }
  ASSERT_TRUE(dest.Load());
  ASSERT_EQ(dest.metadata()->num_syllables, source.metadata()->num_syllables);
  ASSERT_EQ(dest.metadata()->num_entries, source.metadata()->num_entries);
  ASSERT_EQ(dest.dict_file_checksum(), source.dict_file_checksum());
  ASSERT_EQ(dest.metadata()->string_table_size,
            source.metadata()->string_table_size);
  EXPECT_EQ(std::memcmp(dest.metadata()->string_table.get(),
                        source.metadata()->string_table.get(),
                        source.metadata()->string_table_size),
            0);
  for (size_t i = 0; i < source.metadata()->num_syllables; ++i)
    EXPECT_EQ(dest.GetSyllableById(i), source.GetSyllableById(i));
  size_t short_entries = 0, long_entries = 0, code_syllables = 0;
  auto verify_entries = [&](const auto &a, const auto &b) {
    ASSERT_EQ(a.size, b.size);
    if (a.size)
      EXPECT_EQ(std::memcmp(a.at.get(), b.at.get(),
                            a.size * sizeof(rime::table::Entry)),
                0);
    short_entries += a.size;
  };
  std::function<void(const rime::table::TrunkIndex &,
                     const rime::table::TrunkIndex &, int)>
      verify;
  verify = [&](const auto &a, const auto &b, int depth) {
    ASSERT_EQ(a.size, b.size);
    for (size_t i = 0; i < a.size; ++i) {
      EXPECT_EQ(a.at[i].key, b.at[i].key);
      verify_entries(a.at[i].entries, b.at[i].entries);
      ASSERT_EQ(bool(a.at[i].next_level), bool(b.at[i].next_level));
      if (!a.at[i].next_level)
        continue;
      if (depth < 3) {
        verify(a.at[i].next_level->trunk(), b.at[i].next_level->trunk(),
               depth + 1);
        continue;
      }
      const auto &x = a.at[i].next_level->tail();
      const auto &y = b.at[i].next_level->tail();
      ASSERT_EQ(x.size, y.size);
      for (size_t j = 0; j < x.size; ++j) {
        EXPECT_EQ(std::memcmp(&x.at[j].entry, &y.at[j].entry,
                              sizeof(rime::table::Entry)),
                  0);
        ASSERT_EQ(x.at[j].extra_code.size, y.at[j].extra_code.size);
        if (x.at[j].extra_code.size)
          EXPECT_EQ(
              std::memcmp(x.at[j].extra_code.at.get(),
                          y.at[j].extra_code.at.get(),
                          x.at[j].extra_code.size * sizeof(rime::SyllableId)),
              0);
        code_syllables += x.at[j].extra_code.size;
        ++long_entries;
      }
    }
  };
  ASSERT_EQ(source.index_->size, dest.index_->size);
  for (size_t i = 0; i < source.index_->size; ++i) {
    const auto &a = source.index_->at[i];
    const auto &b = dest.index_->at[i];
    verify_entries(a.entries, b.entries);
    ASSERT_EQ(bool(a.next_level), bool(b.next_level));
    if (a.next_level)
      verify(a.next_level->trunk(), b.next_level->trunk(), 2);
  }
  EXPECT_EQ(short_entries + long_entries, source.metadata()->num_entries);
  std::cout << "REPACK_VERIFIED short_entries=" << short_entries
            << " long_entries=" << long_entries
            << " extra_code_syllables=" << code_syllables
            << " bytes=" << dest.file_size() << '\n';
}
