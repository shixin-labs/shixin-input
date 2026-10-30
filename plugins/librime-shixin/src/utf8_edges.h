// UTF-8 helpers for librime-shixin. Pure functions on string_view: no
// allocation, never read out of bounds, tolerant of invalid UTF-8.
#ifndef SHIXIN_UTF8_EDGES_H_
#define SHIXIN_UTF8_EDGES_H_

#include <cstddef>
#include <string_view>

namespace shixin {

inline bool is_continuation(unsigned char c) {
  return (c & 0xC0) == 0x80;
}

// first_char returns the first code point of s (a lead byte and its
// continuation bytes), or an empty view if s is empty.
inline std::string_view first_char(std::string_view s) {
  if (s.empty())
    return s;
  size_t n = 1;
  while (n < s.size() && n < 4 && is_continuation(s[n]))
    ++n;
  return s.substr(0, n);
}

// last_char returns the last code point of s.
inline std::string_view last_char(std::string_view s) {
  if (s.empty())
    return s;
  size_t i = s.size() - 1;
  size_t steps = 0;
  while (i > 0 && steps < 3 && is_continuation(s[i])) {
    --i;
    ++steps;
  }
  return s.substr(i);
}

// char_count counts code points (lead bytes) in s.
inline size_t char_count(std::string_view s) {
  size_t n = 0;
  for (unsigned char c : s)
    if (!is_continuation(c))
      ++n;
  return n;
}

}  // namespace shixin

#endif  // SHIXIN_UTF8_EDGES_H_
