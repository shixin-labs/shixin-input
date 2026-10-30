#include "dates.h"

#include <cstdio>
#include <string_view>

#include "numerals.h"

namespace shixin {

namespace {

const char* const kWeekday[7] = {"日", "一", "二", "三", "四", "五", "六"};
const char* const kMonthEn[12] = {"January", "February", "March",     "April",   "May",      "June",
                                  "July",    "August",   "September", "October", "November", "December"};

// fmt appends printf-formatted text; numbers only, so the C locale's
// decimal point never matters.
template <typename... Args>
void fmt(std::vector<std::string>* out, const char* format, Args... args) {
  char buf[64];
  int n = std::snprintf(buf, sizeof buf, format, args...);
  if (n > 0)
    out->emplace_back(buf, static_cast<size_t>(n) < sizeof buf ? static_cast<size_t>(n) : sizeof buf - 1);
}

std::string zh_date(const DateTime& t, bool circle_zero) {
  char year[16];
  int n = std::snprintf(year, sizeof year, "%d", t.year);
  std::string s;
  chinese_digits(std::string_view(year, n > 0 ? static_cast<size_t>(n) : 0), false, circle_zero, &s);
  s.append("年");
  chinese_integer(std::to_string(t.month), false, &s);
  s.append("月");
  chinese_integer(std::to_string(t.day), false, &s);
  s.append("日");
  return s;
}

}  // namespace

void date_candidates(DateKind kind, const DateTime& t, std::vector<std::string>* out) {
  switch (kind) {
    case DateKind::kDate:
      fmt(out, "%04d-%02d-%02d", t.year, t.month, t.day);
      fmt(out, "%04d/%02d/%02d", t.year, t.month, t.day);
      fmt(out, "%04d.%02d.%02d", t.year, t.month, t.day);
      fmt(out, "%04d%02d%02d", t.year, t.month, t.day);
      fmt(out, "%d年%d月%d日", t.year, t.month, t.day);
      fmt(out, "%d月%d日", t.month, t.day);
      break;
    case DateKind::kTime:
      fmt(out, "%02d:%02d", t.hour, t.minute);
      fmt(out, "%02d:%02d:%02d", t.hour, t.minute, t.second);
      fmt(out, "%d点%d分", t.hour, t.minute);
      break;
    case DateKind::kWeek: {
      const char* d = kWeekday[((t.weekday % 7) + 7) % 7];
      fmt(out, "星期%s", d);
      fmt(out, "周%s", d);
      fmt(out, "礼拜%s", t.weekday == 0 ? "天" : d);
      break;
    }
    case DateKind::kDateTime: {
      long off = t.utc_offset < 0 ? -t.utc_offset : t.utc_offset;
      fmt(out, "%04d-%02d-%02dT%02d:%02d:%02d%c%02ld:%02ld", t.year, t.month, t.day, t.hour, t.minute, t.second,
          t.utc_offset < 0 ? '-' : '+', off / 3600, off / 60 % 60);
      fmt(out, "%04d-%02d-%02d %02d:%02d:%02d", t.year, t.month, t.day, t.hour, t.minute, t.second);
      fmt(out, "%04d%02d%02d%02d%02d%02d", t.year, t.month, t.day, t.hour, t.minute, t.second);
      break;
    }
    case DateKind::kTimestamp:
      fmt(out, "%lld", t.unix_seconds);
      fmt(out, "%lld%03d", t.unix_seconds, t.millis);
      break;
    case DateKind::kDateZh:
      out->push_back(zh_date(t, true));
      out->push_back(zh_date(t, false));
      break;
    case DateKind::kDateEn: {
      const char* m = kMonthEn[((t.month - 1) % 12 + 12) % 12];
      fmt(out, "%s %d, %d", m, t.day, t.year);
      fmt(out, "%d %s %d", t.day, m, t.year);
      fmt(out, "%.3s %d, %d", m, t.day, t.year);
      break;
    }
  }
}

}  // namespace shixin
