// Offline layout diagnostic. Scanning the mapping itself faults pages;
// category counts are address coverage, not measured incremental RSS.
#include <gtest/gtest.h>
#include <array>
#include <cstdlib>
#include <iostream>
#include <rime/dict/table.h>

class PageInspectTable : public rime::Table {
public:
  using Table::Table;
  rime::table::Index *index() { return index_; }
  const char *base() const { return address(); }
};

TEST(TablePages, IndexEntryAndStringCoverage) {
  const char *file = std::getenv("SHIXIN_SPIKE_TABLE");
  if (!file)
    GTEST_SKIP();
  PageInspectTable table{rime::path(std::string(file))};
  ASSERT_TRUE(table.Load());
  for (size_t page_size : {4096u, 16384u}) {
    std::vector<unsigned char> pages((table.file_size() + page_size - 1) /
                                     page_size);
    auto mark = [&](const void *pointer, size_t bytes, unsigned char category) {
      if (!bytes)
        return;
      const uintptr_t address = reinterpret_cast<uintptr_t>(pointer);
      const uintptr_t base = reinterpret_cast<uintptr_t>(table.base());
      ASSERT_GE(address, base);
      const size_t offset = address - base;
      ASSERT_LE(offset, table.file_size());
      ASSERT_LE(bytes, table.file_size() - offset);
      for (size_t p = offset / page_size; p <= (offset + bytes - 1) / page_size;
           ++p)
        pages[p] |= category;
    };
    mark(table.metadata(), sizeof(rime::table::Metadata), 1);
    mark(table.metadata()->syllabary.get(),
         offsetof(rime::table::Syllabary, at) +
             table.metadata()->num_syllables * sizeof(rime::table::StringType),
         1);
    mark(table.metadata()->string_table.get(),
         table.metadata()->string_table_size, 8);
    auto *root = table.index();
    mark(root,
         offsetof(rime::table::Index, at) +
             root->size * sizeof(rime::table::HeadIndexNode),
         2);
    auto entries = [&](const rime::List<rime::table::Entry> &list) {
      mark(list.at.get(), list.size * sizeof(rime::table::Entry), 4);
    };
    size_t two_nodes = 0, three_nodes = 0;
    for (const auto &a : *root) {
      entries(a.entries);
      if (!a.next_level)
        continue;
      auto &two = a.next_level->trunk();
      two_nodes += two.size;
      mark(&two,
           offsetof(rime::table::TrunkIndex, at) +
               two.size * sizeof(rime::table::TrunkIndexNode),
           2);
      for (const auto &b : two) {
        entries(b.entries);
        if (!b.next_level)
          continue;
        auto &three = b.next_level->trunk();
        three_nodes += three.size;
        mark(&three,
             offsetof(rime::table::TrunkIndex, at) +
                 three.size * sizeof(rime::table::TrunkIndexNode),
             2);
        for (const auto &c : three)
          entries(c.entries);
      }
    }
    auto count = [&](unsigned char mask) {
      return std::count_if(pages.begin(), pages.end(), [&](unsigned char bits) {
        return bool(bits & mask);
      });
    };
    std::cout << "PAGE_LAYOUT page_bytes=" << page_size
              << " file_bytes=" << table.file_size()
              << " total_pages=" << pages.size()
              << " metadata_pages=" << count(1) << " index_pages=" << count(2)
              << " short_entry_pages=" << count(4)
              << " string_pages=" << count(8)
              << " index_string_pages=" << count(1 | 2 | 8)
              << " index_entry_string_pages=" << count(15)
              << " two_nodes=" << two_nodes << " three_nodes=" << three_nodes
              << '\n';
    EXPECT_LE(size_t(count(15)), pages.size());
    EXPECT_GT(count(2), 0);
    EXPECT_GT(count(8), 0);
  }
}
