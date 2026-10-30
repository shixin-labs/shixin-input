#include "numerals.h"

namespace shixin {

namespace {

const char* const kLowerDigit[10] = {"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"};
const char* const kUpperDigit[10] = {"零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"};
const char* const kLowerUnit[4] = {"", "十", "百", "千"};
const char* const kUpperUnit[4] = {"", "拾", "佰", "仟"};
const uint32_t kPow10[4] = {1, 10, 100, 1000};

bool is_digit(char c) {
  return c >= '0' && c <= '9';
}

bool all_digits(std::string_view s) {
  for (char c : s)
    if (!is_digit(c))
      return false;
  return true;
}

// read4 appends v (1–9999). leading means v starts the whole number, where
// 10–19 read 十… rather than 一十… (lower case only; 壹拾 stays).
void read4(uint32_t v, bool upper, bool leading, std::string* out) {
  const char* const* digit = upper ? kUpperDigit : kLowerDigit;
  const char* const* unit = upper ? kUpperUnit : kLowerUnit;
  bool started = false, zero = false;
  for (int pos = 3; pos >= 0; --pos) {
    uint32_t d = v / kPow10[pos] % 10;
    if (d == 0) {
      zero = started;
      continue;
    }
    if (zero)
      out->append(digit[0]);
    zero = false;
    if (!(d == 1 && pos == 1 && !started && leading && !upper))
      out->append(digit[d]);
    out->append(unit[pos]);
    started = true;
  }
}

// read appends v (1 to 10^16 - 1) with 万 and 亿 groups, a 零 standing for
// the zeros between groups: 一亿零一十, 十万零一十.
void read(uint64_t v, bool upper, bool leading, std::string* out) {
  const char* zero = upper ? kUpperDigit[0] : kLowerDigit[0];
  if (v >= 100000000) {
    read(v / 100000000, upper, leading, out);
    out->append("亿");
    uint64_t low = v % 100000000;
    if (low) {
      if (low < 10000000)
        out->append(zero);
      read(low, upper, false, out);
    }
    return;
  }
  if (v >= 10000) {
    read4(static_cast<uint32_t>(v / 10000), upper, leading, out);
    out->append("万");
    uint32_t low = static_cast<uint32_t>(v % 10000);
    if (low) {
      if (low < 1000)
        out->append(zero);
      read4(low, upper, false, out);
    }
    return;
  }
  read4(static_cast<uint32_t>(v), upper, leading, out);
}

// amount appends the money reading of integer part digits (already
// validated) and up to two decimals: 壹佰元整, 壹元零伍分, 伍角.
void amount(std::string_view integer, std::string_view frac, bool upper, std::string* out) {
  const char* const* digit = upper ? kUpperDigit : kLowerDigit;
  bool has_int = integer.find_first_not_of('0') != std::string_view::npos;
  int jiao = frac.size() > 0 ? frac[0] - '0' : 0;
  int fen = frac.size() > 1 ? frac[1] - '0' : 0;
  if (has_int) {
    chinese_integer(integer, upper, out);
    out->append("元");
  }
  if (jiao == 0 && fen == 0) {
    if (!has_int)
      out->append(digit[0]).append("元");
    out->append("整");
    return;
  }
  if (jiao)
    out->append(digit[jiao]).append("角");
  else if (has_int)
    out->append(digit[0]);
  if (fen)
    out->append(digit[fen]).append("分");
}

}  // namespace

bool chinese_integer(std::string_view digits, bool upper, std::string* out) {
  if (digits.empty() || !all_digits(digits))
    return false;
  size_t first = digits.find_first_not_of('0');
  if (first == std::string_view::npos) {
    out->append(kLowerDigit[0]);
    return true;
  }
  digits.remove_prefix(first);
  if (digits.size() > kMaxIntegerDigits)
    return false;
  uint64_t v = 0;
  for (char c : digits)
    v = v * 10 + static_cast<uint64_t>(c - '0');
  read(v, upper, true, out);
  return true;
}

void chinese_digits(std::string_view digits, bool upper, bool circle_zero, std::string* out) {
  const char* const* digit = upper ? kUpperDigit : kLowerDigit;
  for (char c : digits) {
    if (!is_digit(c))
      continue;
    if (c == '0' && circle_zero)
      out->append("〇");
    else
      out->append(digit[c - '0']);
  }
}

void number_readings(std::string_view number, std::vector<Reading>* out) {
  size_t dot = number.find('.');
  std::string_view integer = number.substr(0, dot);
  std::string_view frac = dot == std::string_view::npos ? std::string_view() : number.substr(dot + 1);
  if (integer.empty() || !all_digits(integer) || !all_digits(frac) || frac.size() > kMaxFractionDigits)
    return;
  size_t first = integer.find_first_not_of('0');
  if (first != std::string_view::npos && integer.size() - first > kMaxIntegerDigits)
    return;
  for (bool upper : {true, false}) {
    std::string n;
    chinese_integer(integer, upper, &n);
    if (!frac.empty()) {
      n.append("点");
      chinese_digits(frac, upper, false, &n);
    }
    out->push_back({std::move(n), upper ? "大写" : "小写"});
    if (frac.size() <= 2) {
      std::string m;
      amount(integer, frac, upper, &m);
      out->push_back({std::move(m), upper ? "大写金额" : "小写金额"});
    }
  }
}

}  // namespace shixin
