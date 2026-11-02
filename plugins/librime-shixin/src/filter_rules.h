// Bounded, allocation-free candidate ordering and English case rules.
// Behavior is inspired by rime-ice's GPL-3.0 Lua filters; implementation is
// native C++ with fixed storage. See docs/DESIGN.md §3.8.
#ifndef SHIXIN_FILTER_RULES_H_
#define SHIXIN_FILTER_RULES_H_
#include <array>
#include <cstddef>
#include <string_view>
#include "utf8_edges.h"

namespace shixin {
constexpr size_t kFilterWindow = 100;
enum class FilterKind { LongWord, EnglishDemote, VSymbols, Autocap, Pin };

inline bool ascii_letter(char c) {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z');
}
inline bool english_word(std::string_view s) {
  if (s.size() > 256)
    return false;
  bool letter = false;
  for (unsigned char c : s) {
    if (c >= 128 || c <= 32)
      return false;
    letter |= ascii_letter(c);
  }
  return letter;
}
inline bool chinese_word(std::string_view s) {
  if (s.empty() || s.size() > 256)
    return false;
  // Decode UTF-8 defensively and accept Han only, never emoji/kana/digits.
  for (size_t i = 0; i < s.size();) {
    unsigned char c = s[i++];
    unsigned cp = c;
    size_t extra = 0;
    if (c >= 0xc2 && c <= 0xdf) {
      cp = c & 31;
      extra = 1;
    } else if (c >= 0xe0 && c <= 0xef) {
      cp = c & 15;
      extra = 2;
    } else if (c >= 0xf0 && c <= 0xf4) {
      cp = c & 7;
      extra = 3;
    } else
      return false;
    if (i + extra > s.size())
      return false;
    for (size_t j = 0; j < extra; j++) {
      unsigned char d = s[i++];
      if (!is_continuation(d))
        return false;
      cp = (cp << 6) | (d & 63);
    }
    if (!((cp >= 0x3400 && cp <= 0x4dbf) || (cp >= 0x4e00 && cp <= 0x9fff) ||
          (cp >= 0xf900 && cp <= 0xfaff) || (cp >= 0x20000 && cp <= 0x323af)))
      return false;
  }
  return true;
}

struct FilterItem {
  std::string_view text;
  std::string_view type;
  int pin = -1;
};

// Produces a stable permutation. Long-word priority keeps the first three
// candidates and promotes at most two longer Han words to positions 4–5.
inline void filter_order(FilterKind kind,
                         const FilterItem* items,
                         size_t n,
                         std::array<size_t, kFilterWindow>* order) {
  n = n > kFilterWindow ? kFilterWindow : n;
  for (size_t i = 0; i < n; i++)
    (*order)[i] = i;
  auto rank = [&](size_t i) {
    if (kind == FilterKind::Pin)
      return items[i].pin < 0 ? 1000 : items[i].pin;
    if (kind == FilterKind::EnglishDemote)
      return english_word(items[i].text) && items[i].type != "user_table" ? 1
                                                                          : 0;
    if (kind == FilterKind::VSymbols)
      return items[i].text.size() <= 4 && char_count(items[i].text) == 1 ? 0
                                                                         : 1;
    return 0;
  };
  if (kind == FilterKind::LongWord) {
    if (n < 4 || items[0].text.size() > 256)
      return;
    size_t at = 3, length = char_count(items[0].text);
    for (size_t i = 3; i < n && at < 5; i++) {
      size_t id = (*order)[i];
      if (!chinese_word(items[id].text) || char_count(items[id].text) <= length)
        continue;
      for (size_t j = i; j > at; j--)
        (*order)[j] = (*order)[j - 1];
      (*order)[at++] = id;
    }
    return;
  }
  // Insertion sort needs no temporary allocation and preserves equal ranks.
  for (size_t i = 1; i < n; i++) {
    size_t id = (*order)[i], j = i;
    while (j && rank(id) < rank((*order)[j - 1])) {
      (*order)[j] = (*order)[j - 1];
      j--;
    }
    (*order)[j] = id;
  }
}

// Returns false when case changes would alter a non-English or unrelated
// candidate. Output storage belongs to a prewarmed shadow candidate.
inline bool autocap_text(std::string_view code,
                         std::string_view text,
                         std::string_view type,
                         char* output,
                         size_t capacity) {
  if (code.size() < 2 || code[0] < 'A' || code[0] > 'Z' ||
      text.size() > capacity || !english_word(text))
    return false;
  auto lower = [](char c) {
    return c >= 'A' && c <= 'Z' ? char(c + 'a' - 'A') : c;
  };
  // Match letters/digits, ignoring ASCII punctuation in codes and words.
  size_t a = 0, b = 0;
  auto wordchar = [](char c) {
    return ascii_letter(c) || (c >= '0' && c <= '9');
  };
  while (true) {
    while (a < code.size() && !wordchar(code[a]))
      a++;
    while (b < text.size() && !wordchar(text[b]))
      b++;
    if (a == code.size() || b == text.size())
      break;
    if (lower(code[a++]) != lower(text[b++]))
      return false;
  }
  if (a != code.size() || (b != text.size() && type != "completion"))
    return false;
  bool all = code[1] >= 'A' && code[1] <= 'Z', changed = false;
  for (size_t i = 0; i < text.size(); i++) {
    char c = text[i];
    if ((all || i == 0) && c >= 'a' && c <= 'z')
      c -= 'a' - 'A';
    output[i] = c;
    changed |= c != text[i];
  }
  return changed;
}
}  // namespace shixin
#endif
