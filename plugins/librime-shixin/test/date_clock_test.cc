#include "date_clock.h"
#include <chrono>
#include <gtest/gtest.h>

#ifdef _WIN32
#include <cstdlib>
#include <ctime>
#include <windows.h>
#endif

TEST(DateClock, CurrentInstant) {
  using namespace std::chrono;
  const auto before =
      duration_cast<seconds>(system_clock::now().time_since_epoch()).count();
  const auto now = shixin::date_now();
  const auto after =
      duration_cast<seconds>(system_clock::now().time_since_epoch()).count();
  EXPECT_GE(now.unix_seconds, before);
  EXPECT_LE(now.unix_seconds, after);
  EXPECT_GE(now.millis, 0);
  EXPECT_LT(now.millis, 1000);
  EXPECT_GE(now.year, 2026);
  EXPECT_GE(now.month, 1);
  EXPECT_LE(now.month, 12);
}

#ifdef _WIN32
TEST(DateClock, UsesWindowsZoneInsteadOfStaleCRTZone) {
  const char *old = std::getenv("TZ");
  const bool had_zone = old != nullptr;
  const std::string saved = old ? old : "";
  _putenv_s("TZ", "UTC-13");
  _tzset();
  SYSTEMTIME before{}, after{};
  GetLocalTime(&before);
  const auto actual = shixin::date_now();
  GetLocalTime(&after);
  _putenv_s("TZ", had_zone ? saved.c_str() : "");
  _tzset();
  auto matches = [&](const SYSTEMTIME &t) {
    return actual.year == t.wYear && actual.month == t.wMonth &&
           actual.day == t.wDay && actual.hour == t.wHour &&
           actual.minute == t.wMinute;
  };
  EXPECT_TRUE(matches(before) || matches(after));
}
#endif
