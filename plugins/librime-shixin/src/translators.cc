#include "translators.h"

#include <chrono>
#include <cstdio>
#include <ctime>
#include <iterator>
#include <type_traits>
#include <vector>

#include <rime/candidate.h>
#include <rime/config.h>
#include <rime/schema.h>
#include <rime/segmentation.h>
#include <rime/ticket.h>
#include <rime/translation.h>

#include "calc.h"
#include "codepoint.h"
#include "numerals.h"

namespace shixin {

using namespace rime;

namespace {

// An input in rime-ice's alphabet throughout (cCpi, Ubeef) is also an abc
// segment, where a pinyin sentence covering it would come first.
constexpr double kPatternQuality = 1000;

Config* config_of(const Ticket& ticket) {
  return ticket.schema ? ticket.schema->config() : nullptr;
}

// pattern_prefix returns the letters a recognizer pattern starts with:
// "^U[a-f0-9]+" → "U", "^cC.+" → "cC".
std::string pattern_prefix(const Ticket& ticket, const std::string& tag, const std::string& fallback) {
  std::string p;
  Config* config = config_of(ticket);
  if (!config || !config->GetString("recognizer/patterns/" + tag, &p) || p.size() < 2 || p[0] != '^')
    return fallback;
  size_t n = 1;
  while (n < p.size() && ((p[n] >= 'a' && p[n] <= 'z') || (p[n] >= 'A' && p[n] <= 'Z')))
    ++n;
  return n > 1 ? p.substr(1, n - 1) : fallback;
}

an<Translation> translation_of(std::vector<an<Candidate>>* cands) {
  if (cands->empty())
    return nullptr;
  auto t = New<FifoTranslation>();
  for (auto& c : *cands)
    t->Append(c);
  return t;
}

DateTime now() {
  using namespace std::chrono;
  long long ms = duration_cast<milliseconds>(system_clock::now().time_since_epoch()).count();
  std::time_t secs = static_cast<std::time_t>(ms / 1000);
  std::tm tm{};
  long offset;
#ifdef _WIN32
  localtime_s(&tm, &secs);
  std::tm copy = tm;
  offset = static_cast<long>(_mkgmtime(&copy) - secs);
#else
  localtime_r(&secs, &tm);
  offset = tm.tm_gmtoff;
#endif
  return DateTime{tm.tm_year + 1900, tm.tm_mon + 1, tm.tm_mday, tm.tm_hour,      tm.tm_min,
                  tm.tm_sec,         tm.tm_wday,    offset,    ms / 1000, static_cast<int>(ms % 1000)};
}

}  // namespace

// ---------------------------------------------------------------- dates

DateTranslator::DateTranslator(const Ticket& ticket) : Translator(ticket) {
  if (name_space_ == "translator")
    name_space_ = "date_translator";
  const struct {
    const char* key;
    const char* fallback;
    DateKind kind;
  } kCodes[] = {
      {"date", "rq", DateKind::kDate},
      {"time", "sj", DateKind::kTime},
      {"week", "xq", DateKind::kWeek},
      {"datetime", "dt", DateKind::kDateTime},
      {"timestamp", "ts", DateKind::kTimestamp},
      {"datezh", "rqzh", DateKind::kDateZh},
      {"dateen", "rqen", DateKind::kDateEn},
  };
  Config* config = config_of(ticket);
  static_assert(std::size(kCodes) == std::extent_v<decltype(codes_)>, "one code per date kind");
  for (size_t i = 0; i < std::size(kCodes); ++i) {
    codes_[i] = {kCodes[i].fallback, kCodes[i].kind};
    if (config)
      config->GetString(name_space_ + "/" + kCodes[i].key, &codes_[i].code);
  }
  // Load the time zone now rather than on the first rq.
#ifdef _WIN32
  _tzset();
#else
  tzset();
#endif
}

an<Translation> DateTranslator::Query(const std::string& input, const Segment& segment) {
  if (!segment.HasTag(tag_))
    return nullptr;
  for (const Code& c : codes_) {
    if (c.code.empty() || input != c.code)
      continue;
    std::vector<std::string> texts;
    date_candidates(c.kind, now(), &texts);
    std::vector<an<Candidate>> cands;
    for (auto& text : texts) {
      auto cand = New<SimpleCandidate>("date", segment.start, segment.end, text);
      cand->set_quality(100);  // ahead of the words for the same code, as in rime-ice
      cands.push_back(cand);
    }
    return translation_of(&cands);
  }
  return nullptr;
}

// ---------------------------------------------------------------- patterns

PatternTranslator::PatternTranslator(const Ticket& ticket, const std::string& tag, const std::string& prefix)
    : Translator(ticket), tag_(tag), prefix_(prefix) {}

bool PatternTranslator::Argument(const std::string& input, const Segment& segment, std::string_view* arg) const {
  if (!segment.HasTag(tag_) || input.size() <= prefix_.size() || input.compare(0, prefix_.size(), prefix_) != 0)
    return false;
  *arg = std::string_view(input).substr(prefix_.size());
  return true;
}

NumberTranslator::NumberTranslator(const Ticket& ticket)
    : PatternTranslator(ticket, "number", pattern_prefix(ticket, "number", "R")) {}

an<Translation> NumberTranslator::Query(const std::string& input, const Segment& segment) {
  std::string_view arg;
  if (!Argument(input, segment, &arg))
    return nullptr;
  std::vector<Reading> readings;
  number_readings(arg, &readings);
  std::vector<an<Candidate>> cands;
  for (auto& r : readings)
    cands.push_back(New<SimpleCandidate>("number", segment.start, segment.end, r.text, r.comment));
  return translation_of(&cands);
}

UnicodeTranslator::UnicodeTranslator(const Ticket& ticket)
    : PatternTranslator(ticket, "unicode", pattern_prefix(ticket, "unicode", "U")) {}

an<Translation> UnicodeTranslator::Query(const std::string& input, const Segment& segment) {
  std::string_view arg;
  uint32_t cp;
  if (!Argument(input, segment, &arg) || !parse_hex(arg, &cp))
    return nullptr;
  std::vector<an<Candidate>> cands;
  auto add = [&](uint32_t c) {
    if (!is_printable(c))
      return;
    std::string text;
    utf8_append(c, &text);
    char comment[16];
    std::snprintf(comment, sizeof comment, "U+%04X", static_cast<unsigned>(c));
    auto cand = New<SimpleCandidate>("unicode", segment.start, segment.end, text, comment);
    cand->set_quality(kPatternQuality);
    cands.push_back(cand);
  };
  // The code point, then those one more hex digit would give.
  add(cp);
  if (cp <= 0x10FFF)
    for (uint32_t d = 0; d < 16; ++d)
      add(cp * 16 + d);
  return translation_of(&cands);
}

CalcTranslator::CalcTranslator(const Ticket& ticket)
    : PatternTranslator(ticket, "calculator", pattern_prefix(ticket, "calculator", "cC")) {
  if (Config* config = config_of(ticket))
    config->GetString("calculator/prefix", &prefix_);
}

an<Translation> CalcTranslator::Query(const std::string& input, const Segment& segment) {
  std::string_view arg;
  double v;
  if (!Argument(input, segment, &arg) || !calc_eval(arg, &v))
    return nullptr;
  std::string result;
  calc_format(v, &result);
  std::vector<an<Candidate>> cands;
  for (auto text : {result, std::string(arg) + "=" + result}) {
    auto cand = New<SimpleCandidate>("calc", segment.start, segment.end, text);
    cand->set_quality(kPatternQuality);
    cands.push_back(cand);
  }
  return translation_of(&cands);
}

}  // namespace shixin
