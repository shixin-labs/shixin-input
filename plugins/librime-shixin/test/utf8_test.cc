#include "utf8_edges.h"

#include <gtest/gtest.h>

using shixin::char_count;
using shixin::first_char;
using shixin::last_char;

TEST(Utf8Edges, Chinese) {
  EXPECT_EQ(first_char("试验"), "试");
  EXPECT_EQ(last_char("试验"), "验");
  EXPECT_EQ(char_count("拾新输入法"), 5u);
}

TEST(Utf8Edges, MixedAndAstral) {
  EXPECT_EQ(first_char("a拾"), "a");
  EXPECT_EQ(last_char("a拾"), "拾");
  EXPECT_EQ(last_char("拾😀"), "😀");  // 4-byte code point
  EXPECT_EQ(char_count("拾😀"), 2u);
}

TEST(Utf8Edges, EmptyAndInvalid) {
  EXPECT_EQ(first_char(""), "");
  EXPECT_EQ(last_char(""), "");
  // Stray continuation bytes never make the helpers read out of bounds.
  std::string bad = "\x80\x80\x80\x80\x80";
  EXPECT_LE(first_char(bad).size(), 4u);
  EXPECT_LE(last_char(bad).size(), 4u);
  EXPECT_EQ(char_count(bad), 0u);
}
