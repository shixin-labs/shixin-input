#include "numerals.h"

#include <gtest/gtest.h>

namespace {

std::string lower(const char* digits) {
  std::string s;
  EXPECT_TRUE(shixin::chinese_integer(digits, false, &s)) << digits;
  return s;
}

std::string upper(const char* digits) {
  std::string s;
  EXPECT_TRUE(shixin::chinese_integer(digits, true, &s)) << digits;
  return s;
}

std::vector<shixin::Reading> readings(const char* number) {
  std::vector<shixin::Reading> r;
  shixin::number_readings(number, &r);
  return r;
}

TEST(Numerals, Integers) {
  EXPECT_EQ(lower("0"), "零");
  EXPECT_EQ(lower("000"), "零");
  EXPECT_EQ(lower("7"), "七");
  EXPECT_EQ(lower("10"), "十");
  EXPECT_EQ(lower("12"), "十二");
  EXPECT_EQ(lower("20"), "二十");
  EXPECT_EQ(lower("110"), "一百一十");
  EXPECT_EQ(lower("123"), "一百二十三");
  EXPECT_EQ(lower("1001"), "一千零一");
  EXPECT_EQ(lower("1010"), "一千零一十");
  EXPECT_EQ(lower("100000"), "十万");
  EXPECT_EQ(lower("100010"), "十万零一十");
  EXPECT_EQ(lower("10000001"), "一千万零一");
  EXPECT_EQ(lower("100001000"), "一亿零一千");
  EXPECT_EQ(lower("110000000"), "一亿一千万");
  EXPECT_EQ(lower("101000000"), "一亿零一百万");
  EXPECT_EQ(lower("1000000000"), "十亿");
  EXPECT_EQ(lower("1000100000000"), "一万零一亿");
  EXPECT_EQ(lower("007"), "七");
  EXPECT_EQ(lower("9999999999999999"), "九千九百九十九万九千九百九十九亿九千九百九十九万九千九百九十九");
}

TEST(Numerals, Upper) {
  EXPECT_EQ(upper("10"), "壹拾");
  EXPECT_EQ(upper("123"), "壹佰贰拾叁");
  EXPECT_EQ(upper("1001"), "壹仟零壹");
  EXPECT_EQ(upper("100010"), "壹拾万零壹拾");
}

TEST(Numerals, Rejects) {
  std::string s;
  EXPECT_FALSE(shixin::chinese_integer("", false, &s));
  EXPECT_FALSE(shixin::chinese_integer("12a", false, &s));
  EXPECT_FALSE(shixin::chinese_integer("10000000000000000", false, &s));  // 17 digits
  EXPECT_TRUE(shixin::chinese_integer("00000000000000000001", false, &s));  // leading zeros don't count
  EXPECT_EQ(s, "一");
  EXPECT_TRUE(readings("").empty());
  EXPECT_TRUE(readings(".5").empty());
  EXPECT_TRUE(readings("1.2.3").empty());
  EXPECT_TRUE(readings("1e5").empty());
  EXPECT_TRUE(readings("10000000000000000").empty());
  EXPECT_TRUE(readings("1.123456789").empty());
}

TEST(Numerals, Readings) {
  auto r = readings("123.45");
  ASSERT_EQ(r.size(), 4u);
  EXPECT_EQ(r[0].text, "壹佰贰拾叁点肆伍");
  EXPECT_STREQ(r[0].comment, "大写");
  EXPECT_EQ(r[1].text, "壹佰贰拾叁元肆角伍分");
  EXPECT_EQ(r[2].text, "一百二十三点四五");
  EXPECT_EQ(r[3].text, "一百二十三元四角五分");
}

TEST(Numerals, Amounts) {
  auto amount = [](const char* n) { return readings(n).at(1).text; };
  EXPECT_EQ(amount("123"), "壹佰贰拾叁元整");
  EXPECT_EQ(amount("123."), "壹佰贰拾叁元整");
  EXPECT_EQ(amount("10.00"), "壹拾元整");
  EXPECT_EQ(amount("1.5"), "壹元伍角");
  EXPECT_EQ(amount("1.05"), "壹元零伍分");
  EXPECT_EQ(amount("0.5"), "伍角");
  EXPECT_EQ(amount("0.05"), "伍分");
  EXPECT_EQ(amount("0"), "零元整");
  EXPECT_EQ(amount("1000000.01"), "壹佰万元零壹分");
  // Three decimals: no amount.
  auto r = readings("1.234");
  ASSERT_EQ(r.size(), 2u);
  EXPECT_EQ(r[0].text, "壹点贰叁肆");
  EXPECT_EQ(r[1].text, "一点二三四");
}

TEST(Numerals, Digits) {
  std::string s;
  shixin::chinese_digits("2026", false, true, &s);
  EXPECT_EQ(s, "二〇二六");
  s.clear();
  shixin::chinese_digits("2026", false, false, &s);
  EXPECT_EQ(s, "二零二六");
}

}  // namespace
