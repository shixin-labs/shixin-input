#include "codepoint.h"

#include <gtest/gtest.h>

namespace {

TEST(Codepoint, ParseHex) {
  uint32_t cp = 0;
  EXPECT_TRUE(shixin::parse_hex("62fe", &cp));
  EXPECT_EQ(cp, 0x62FEu);
  EXPECT_TRUE(shixin::parse_hex("1F600", &cp));
  EXPECT_EQ(cp, 0x1F600u);
  EXPECT_FALSE(shixin::parse_hex("", &cp));
  EXPECT_FALSE(shixin::parse_hex("1234567", &cp));
  EXPECT_FALSE(shixin::parse_hex("12g", &cp));
}

TEST(Codepoint, Printable) {
  EXPECT_TRUE(shixin::is_printable(0x62FE));
  EXPECT_TRUE(shixin::is_printable(0x10FFFF));
  EXPECT_FALSE(shixin::is_printable(0x110000));
  EXPECT_FALSE(shixin::is_printable(0xD800));
  EXPECT_FALSE(shixin::is_printable(0x0A));
  EXPECT_FALSE(shixin::is_printable(0x85));
}

TEST(Codepoint, Utf8) {
  std::string s;
  for (uint32_t cp : {0x41u, 0xE9u, 0x62FEu, 0x1F600u})
    shixin::utf8_append(cp, &s);
  EXPECT_EQ(s, "Aé拾😀");
}

}  // namespace
