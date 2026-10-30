#include "rerank_slot.h"

#include <gtest/gtest.h>

#include <memory>
#include <string>
#include <vector>

namespace {

using namespace shixin::rerank;

struct Fixture {
  std::unique_ptr<Slot> slot = std::make_unique<Slot>();
  std::vector<Item> items;

  Fixture() { slot->enabled = 1; }
  void Add(std::string_view text, std::string_view type = "phrase", double weight = -10) {
    items.push_back(Item{text, type, weight});
  }
  void Export(uint32_t hash = 7) { shixin::rerank::Export(slot.get(), hash, 0, items.data(), int(items.size())); }
  std::string Text(int i) const {
    const Entry& e = slot->entries[i];
    return std::string(slot->arena + e.offset, e.length);
  }
  void Import(std::vector<uint8_t> perm) {
    slot->import_seq = slot->export_seq;
    slot->import_count = uint16_t(perm.size());
    std::copy(perm.begin(), perm.end(), slot->perm);
  }
};

TEST(RerankSlot, ExportsEntries) {
  Fixture f;
  f.Add("台湾", "phrase", -1.5);
  f.Add("天物", "user_phrase");
  f.Add("TW", "phrase");
  f.Add("2026年", "date");
  f.Export();
  ASSERT_EQ(f.slot->count, 4);
  EXPECT_EQ(f.Text(0), "台湾");
  EXPECT_EQ(f.Text(1), "天物");
  EXPECT_FLOAT_EQ(f.slot->entries[0].weight, -1.5f);
  EXPECT_EQ(f.slot->entries[0].flags, 0);
  EXPECT_EQ(f.slot->entries[1].flags, kEntryUser);
  EXPECT_EQ(f.slot->entries[2].flags, kEntryFixed);  // English
  EXPECT_EQ(f.slot->entries[3].flags, kEntryFixed);  // translator
  EXPECT_NE(f.slot->export_seq, 0u);
}

TEST(RerankSlot, SameExportKeepsSequence) {
  Fixture f;
  f.Add("台湾");
  f.Add("天物");
  f.Export();
  uint32_t seq = f.slot->export_seq;
  f.Export();
  EXPECT_EQ(f.slot->export_seq, seq);
  f.Export(8);  // other input
  EXPECT_NE(f.slot->export_seq, seq);
  seq = f.slot->export_seq;
  f.items[1].text = "天文";
  f.Export(8);
  EXPECT_NE(f.slot->export_seq, seq);
  seq = f.slot->export_seq;
  f.items.pop_back();
  f.Export(8);
  EXPECT_NE(f.slot->export_seq, seq);
}

TEST(RerankSlot, ArenaOverflowEndsExport) {
  Fixture f;
  std::string big(kArenaSize - 3, 'x');
  f.Add("台湾");  // 6 bytes
  f.Add(big);
  f.Add("天");
  f.Export();
  EXPECT_EQ(f.slot->count, 1);
  std::vector<std::string> many(kMaxEntries + 10, "字");
  Fixture g;
  for (auto& s : many)
    g.Add(s);
  g.Export();
  EXPECT_EQ(g.slot->count, kMaxEntries);
}

TEST(RerankSlot, AppliesOnlyMatchingPermutations) {
  Fixture f;
  f.Add("台湾");
  f.Add("天物");
  f.Add("太晚");
  f.Export();
  EXPECT_EQ(Permutation(f.slot.get(), 3), nullptr);  // nothing imported
  f.Import({2, 0, 1});
  const uint8_t* p = Permutation(f.slot.get(), 3);
  ASSERT_NE(p, nullptr);
  EXPECT_EQ(p[0], 2);
  EXPECT_EQ(Permutation(f.slot.get(), 2), nullptr);  // other length
  f.slot->enabled = 0;
  EXPECT_EQ(Permutation(f.slot.get(), 3), nullptr);
  f.slot->enabled = 1;
  f.Export(9);  // a new export invalidates the import
  EXPECT_EQ(Permutation(f.slot.get(), 3), nullptr);
  EXPECT_EQ(f.slot->rejected, 0u);
}

TEST(RerankSlot, RejectsInvalidPermutations) {
  for (std::vector<uint8_t> bad : {std::vector<uint8_t>{0, 0, 1}, {0, 1, 3}, {0, 1, 200}, {1, 0, 2}}) {
    Fixture f;
    f.Add("台湾");
    f.Add("天物");
    f.Add("太晚");
    if (bad == std::vector<uint8_t>{1, 0, 2})
      f.items[0].type = "date";  // fixed entries stay in place
    f.Export();
    f.Import(bad);
    EXPECT_EQ(Permutation(f.slot.get(), 3), nullptr) << int(bad[0]) << int(bad[1]) << int(bad[2]);
    EXPECT_EQ(f.slot->rejected, 1u);
    EXPECT_EQ(f.slot->import_seq, 0u);
  }
}

TEST(RerankSlot, Abbreviation) {
  EXPECT_TRUE(IsAbbreviation("tw"));
  EXPECT_TRUE(IsAbbreviation("zgr"));
  EXPECT_TRUE(IsAbbreviation("x'f"));
  EXPECT_FALSE(IsAbbreviation("taiw"));
  EXPECT_FALSE(IsAbbreviation("lv"));  // ü
  EXPECT_FALSE(IsAbbreviation(""));
  EXPECT_FALSE(IsAbbreviation("'"));
  EXPECT_FALSE(IsAbbreviation("TW"));
}

TEST(RerankSlot, Hash) {
  EXPECT_EQ(Hash("taiwan", 0, 6), Hash("taiwan", 0, 6));
  EXPECT_NE(Hash("taiwan", 0, 6), Hash("taiwan", 0, 3));
  EXPECT_NE(Hash("taiwan", 0, 6), Hash("taiwa", 0, 6));
}

TEST(RerankSlot, Attach) {
  std::vector<Slot> slots(3);
  EXPECT_FALSE(Attach(slots.data(), 3, sizeof(Slot) - 4));
  EXPECT_EQ(SlotAt(0), nullptr);
  EXPECT_TRUE(Attach(slots.data(), 3, sizeof(Slot)));
  EXPECT_EQ(SlotAt(2), &slots[2]);
  EXPECT_EQ(SlotAt(3), nullptr);
  EXPECT_EQ(SlotAt(-1), nullptr);
  EXPECT_TRUE(Attach(nullptr, 0, 0));
  EXPECT_EQ(SlotAt(0), nullptr);
}

}  // namespace
