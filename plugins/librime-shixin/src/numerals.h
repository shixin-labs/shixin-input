// Chinese numerals for shixin_number_translator and shixin_date_translator.
// Pure functions without librime: they only append to the output string.
#ifndef SHIXIN_NUMERALS_H_
#define SHIXIN_NUMERALS_H_

#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

namespace shixin {

// The longest integer part read out: up to 9999万亿 (16 digits).
constexpr size_t kMaxIntegerDigits = 16;
// The longest fraction read out digit by digit.
constexpr size_t kMaxFractionDigits = 8;

// chinese_integer appends the reading of the decimal digits in digits
// (leading zeros allowed) to out: 一百二十三, or 壹佰贰拾叁 when upper. It
// returns false, appending nothing, if digits is empty, has a non-digit or
// more than kMaxIntegerDigits significant digits.
bool chinese_integer(std::string_view digits, bool upper, std::string* out);

// chinese_digits appends each digit as a numeral: 二零二六, 二〇二六 with
// circle_zero, or 贰零贰陆 when upper. Non-digits are skipped.
void chinese_digits(std::string_view digits, bool upper, bool circle_zero, std::string* out);

struct Reading {
  std::string text;
  const char* comment;  // static
};

// number_readings parses a plain decimal number ("123", "123.45", "123.")
// and appends its readings, each with its kind as the comment: 大写
// 壹佰贰拾叁点肆伍, 大写金额 壹佰贰拾叁元肆角伍分 (amounts only with at most
// two decimals), 小写 一百二十三点四五, 小写金额 一百二十三元四角五分. It
// appends nothing for anything else or a number too long to read.
void number_readings(std::string_view number, std::vector<Reading>* out);

}  // namespace shixin

#endif  // SHIXIN_NUMERALS_H_
