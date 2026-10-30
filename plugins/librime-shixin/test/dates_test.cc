#include "dates.h"

#include <gtest/gtest.h>

namespace {

using shixin::DateKind;

// 2026-03-08 09:05:07 at UTC+08:00, a Sunday.
const shixin::DateTime kSunday{2026, 3, 8, 9, 5, 7, 0, 8 * 3600, 1772931907, 42};

std::vector<std::string> dates(DateKind kind, const shixin::DateTime& t = kSunday) {
  std::vector<std::string> out;
  shixin::date_candidates(kind, t, &out);
  return out;
}

TEST(Dates, Formats) {
  EXPECT_EQ(dates(DateKind::kDate),
            (std::vector<std::string>{"2026-03-08", "2026/03/08", "2026.03.08", "20260308", "2026年3月8日", "3月8日"}));
  EXPECT_EQ(dates(DateKind::kTime), (std::vector<std::string>{"09:05", "09:05:07", "9点5分"}));
  EXPECT_EQ(dates(DateKind::kWeek), (std::vector<std::string>{"星期日", "周日", "礼拜天"}));
  EXPECT_EQ(dates(DateKind::kDateTime),
            (std::vector<std::string>{"2026-03-08T09:05:07+08:00", "2026-03-08 09:05:07", "20260308090507"}));
  EXPECT_EQ(dates(DateKind::kTimestamp), (std::vector<std::string>{"1772931907", "1772931907042"}));
  EXPECT_EQ(dates(DateKind::kDateZh), (std::vector<std::string>{"二〇二六年三月八日", "二零二六年三月八日"}));
  EXPECT_EQ(dates(DateKind::kDateEn), (std::vector<std::string>{"March 8, 2026", "8 March 2026", "Mar 8, 2026"}));
}

TEST(Dates, ChineseTens) {
  shixin::DateTime t = kSunday;
  t.month = 12;
  t.day = 31;
  t.weekday = 4;
  EXPECT_EQ(dates(DateKind::kDateZh, t)[1], "二零二六年十二月三十一日");
  EXPECT_EQ(dates(DateKind::kWeek, t)[2], "礼拜四");
  t.month = 10;
  t.day = 20;
  EXPECT_EQ(dates(DateKind::kDateZh, t)[0], "二〇二六年十月二十日");
}

TEST(Dates, NegativeOffset) {
  shixin::DateTime t = kSunday;
  t.utc_offset = -(9 * 3600 + 30 * 60);
  EXPECT_EQ(dates(DateKind::kDateTime, t)[0], "2026-03-08T09:05:07-09:30");
  t.utc_offset = 0;
  EXPECT_EQ(dates(DateKind::kDateTime, t)[0], "2026-03-08T09:05:07+00:00");
}

}  // namespace
