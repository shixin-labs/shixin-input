#include <gtest/gtest.h>
#include "filter_rules.h"

namespace shixin {
TEST(FilterRules, LongWordsKeepFirstThreeAndPromoteOnlyTwoHanWords) {
  FilterItem items[] = {{"你", "phrase"},    {"泥", "phrase"},
                        {"拟", "phrase"},    {"hello", "table"},
                        {"你好", "phrase"},  {"你们好", "phrase"},
                        {"你好吗", "phrase"}};
  std::array<size_t, kFilterWindow> order{};
  filter_order(FilterKind::LongWord, items, 7, &order);
  const size_t expected[] = {0, 1, 2, 4, 5, 3, 6};
  for (size_t i = 0; i < 7; i++)
    EXPECT_EQ(order[i], expected[i]);
  EXPECT_FALSE(chinese_word("👋"));
  EXPECT_FALSE(chinese_word("こんにちは"));
  EXPECT_FALSE(chinese_word(std::string_view("\xe4\xb8", 2)));
}

TEST(FilterRules, EnglishDemotionPreservesCustomPhraseAndStableOrder) {
  FilterItem items[] = {{"hello", "table"},
                        {"你好", "phrase"},
                        {"custom", "user_table"},
                        {"help", "completion"},
                        {"你们好", "phrase"}};
  std::array<size_t, kFilterWindow> order{};
  filter_order(FilterKind::EnglishDemote, items, 5, &order);
  const size_t expected[] = {1, 2, 4, 0, 3};
  for (size_t i = 0; i < 5; i++)
    EXPECT_EQ(order[i], expected[i]);
}

TEST(FilterRules, VSymbolsAndPins) {
  FilterItem items[] = {{"van", "completion"},
                        {"ā", "punct", 1},
                        {"á", "punct", 0},
                        {"vain", "completion"}};
  std::array<size_t, kFilterWindow> order{};
  filter_order(FilterKind::VSymbols, items, 4, &order);
  EXPECT_EQ(order[0], 1u);
  EXPECT_EQ(order[1], 2u);
  EXPECT_EQ(order[2], 0u);
  filter_order(FilterKind::Pin, items, 4, &order);
  EXPECT_EQ(order[0], 2u);
  EXPECT_EQ(order[1], 1u);
  EXPECT_EQ(order[2], 0u);
}

TEST(FilterRules, AutocapOnlyMatchesEnglishWords) {
  char output[256];
  EXPECT_TRUE(autocap_text("Hello", "hello", "table", output, sizeof(output)));
  EXPECT_EQ(std::string(output, 5), "Hello");
  EXPECT_TRUE(autocap_text("HEllo", "hello", "table", output, sizeof(output)));
  EXPECT_EQ(std::string(output, 5), "HELLO");
  EXPECT_TRUE(
      autocap_text("He", "hello", "completion", output, sizeof(output)));
  EXPECT_FALSE(autocap_text("He", "hello", "table", output, sizeof(output)));
  EXPECT_FALSE(
      autocap_text("PS", "Photoshop", "table", output, sizeof(output)));
  EXPECT_FALSE(autocap_text("Ni", "你好", "phrase", output, sizeof(output)));
  EXPECT_FALSE(
      autocap_text("H", "hello", "completion", output, sizeof(output)));
  EXPECT_FALSE(autocap_text("Hello", "hello world", "completion", output,
                            sizeof(output)));
  EXPECT_FALSE(autocap_text("Hello", "hello", "table", output, 4));
}
}  // namespace shixin
