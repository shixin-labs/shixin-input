// Date and time formats for shixin_date_translator. Pure: the component
// reads the clock, these functions only format it.
#ifndef SHIXIN_DATES_H_
#define SHIXIN_DATES_H_

#include <string>
#include <vector>

namespace shixin {

// DateTime is a local time.
struct DateTime {
  int year, month, day;      // month 1–12
  int hour, minute, second;  // 24-hour clock
  int weekday;               // 0 = Sunday
  long utc_offset;           // seconds east of UTC
  long long unix_seconds;
  int millis;
};

// What a trigger code (rime-ice's date_translator/* keys) asks for.
enum class DateKind {
  kDate,       // rq:   2026-10-26, 2026/10/26, …, 2026年10月26日
  kTime,       // sj:   18:13, 18:13:11, 18点13分
  kWeek,       // xq:   星期一, 周一, 礼拜一
  kDateTime,   // dt:   2026-10-26T18:13:11+08:00, …
  kTimestamp,  // ts:   Unix seconds and milliseconds
  kDateZh,     // rqzh: 二〇二六年十月二十六日, 二零二六年十月二十六日
  kDateEn,     // rqen: October 26, 2026, 26 October 2026, Oct 26, 2026
};

// date_candidates appends the texts for kind at t, most common first.
void date_candidates(DateKind kind, const DateTime& t, std::vector<std::string>* out);

}  // namespace shixin

#endif  // SHIXIN_DATES_H_
