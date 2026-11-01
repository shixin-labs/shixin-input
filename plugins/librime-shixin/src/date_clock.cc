#include "date_clock.h"

#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#else
#include <chrono>
#include <ctime>
#endif

namespace shixin {
DateTime date_now() {
#ifdef _WIN32
  FILETIME utc{}, local{};
  GetSystemTimePreciseAsFileTime(&utc);
  // This is a conversion of the current instant, so the OS's current DST
  // bias is the right one. These clock conversions do not open a file or
  // reload the registry/time-zone database on the engine's key thread.
  if (!FileTimeToLocalFileTime(&utc, &local))
    local = utc;
  SYSTEMTIME wall{};
  if (!FileTimeToSystemTime(&local, &wall))
    return {};
  auto ticks = [](const FILETIME &t) {
    return (static_cast<long long>(t.dwHighDateTime) << 32) | t.dwLowDateTime;
  };
  constexpr long long kUnixEpoch = 116444736000000000LL;
  const auto unix_ms = (ticks(utc) - kUnixEpoch) / 10000;
  return DateTime{wall.wYear,
                  wall.wMonth,
                  wall.wDay,
                  wall.wHour,
                  wall.wMinute,
                  wall.wSecond,
                  wall.wDayOfWeek,
                  static_cast<long>((ticks(local) - ticks(utc)) / 10000000),
                  unix_ms / 1000,
                  static_cast<int>(unix_ms % 1000)};
#else
  using namespace std::chrono;
  const auto ms =
      duration_cast<milliseconds>(system_clock::now().time_since_epoch())
          .count();
  const auto secs = static_cast<std::time_t>(ms / 1000);
  std::tm tm{};
  localtime_r(&secs, &tm);
  return DateTime{tm.tm_year + 1900, tm.tm_mon + 1,
                  tm.tm_mday,        tm.tm_hour,
                  tm.tm_min,         tm.tm_sec,
                  tm.tm_wday,        tm.tm_gmtoff,
                  ms / 1000,         static_cast<int>(ms % 1000)};
#endif
}
} // namespace shixin
