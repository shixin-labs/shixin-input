// Code point helpers for shixin_unicode_translator. Pure, header-only.
#ifndef SHIXIN_CODEPOINT_H_
#define SHIXIN_CODEPOINT_H_

#include <cstdint>
#include <string>
#include <string_view>

namespace shixin {

// parse_hex parses 1–6 hex digits (either case) into *cp.
inline bool parse_hex(std::string_view s, uint32_t* cp) {
  if (s.empty() || s.size() > 6)
    return false;
  uint32_t v = 0;
  for (char c : s) {
    uint32_t d;
    if (c >= '0' && c <= '9')
      d = static_cast<uint32_t>(c - '0');
    else if (c >= 'a' && c <= 'f')
      d = static_cast<uint32_t>(c - 'a' + 10);
    else if (c >= 'A' && c <= 'F')
      d = static_cast<uint32_t>(c - 'A' + 10);
    else
      return false;
    v = v * 16 + d;
  }
  *cp = v;
  return true;
}

// is_printable reports whether cp is a Unicode scalar value worth
// committing: not a surrogate, beyond U+10FFFF, or a C0/C1 control.
inline bool is_printable(uint32_t cp) {
  return cp <= 0x10FFFF && !(cp >= 0xD800 && cp <= 0xDFFF) && cp >= 0x20 && !(cp >= 0x7F && cp <= 0x9F);
}

// utf8_append appends the UTF-8 encoding of a scalar value cp.
inline void utf8_append(uint32_t cp, std::string* out) {
  if (cp < 0x80) {
    out->push_back(static_cast<char>(cp));
  } else if (cp < 0x800) {
    out->push_back(static_cast<char>(0xC0 | (cp >> 6)));
    out->push_back(static_cast<char>(0x80 | (cp & 0x3F)));
  } else if (cp < 0x10000) {
    out->push_back(static_cast<char>(0xE0 | (cp >> 12)));
    out->push_back(static_cast<char>(0x80 | ((cp >> 6) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | (cp & 0x3F)));
  } else {
    out->push_back(static_cast<char>(0xF0 | (cp >> 18)));
    out->push_back(static_cast<char>(0x80 | ((cp >> 12) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | ((cp >> 6) & 0x3F)));
    out->push_back(static_cast<char>(0x80 | (cp & 0x3F)));
  }
}

}  // namespace shixin

#endif  // SHIXIN_CODEPOINT_H_
