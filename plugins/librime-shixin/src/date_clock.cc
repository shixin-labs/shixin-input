#include "date_clock.h"

#ifdef __linux__
#include <atomic>
#endif

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
#ifdef __linux__
namespace {
std::atomic<long> zone_offset{0};
std::atomic<bool> zone_ready{false};
static_assert(std::atomic<long>::is_always_lock_free);

// Gregorian civil date from days since 1970-01-01. March starts each year
// here, so leap days fall at the end of a 400-year era. Constant work and
// no libc timezone lock: a concurrent tzset may be reading the zone file.
DateTime local_date(long long seconds, long offset, int millis) {
  const auto local = seconds + offset;
  auto days = local / 86400;
  auto clock = local % 86400;
  if (clock < 0) {
    clock += 86400;
    --days;
  }
  const auto shifted = days + 719468;
  const auto era = (shifted >= 0 ? shifted : shifted - 146096) / 146097;
  const auto era_day = shifted - era * 146097;
  const auto era_year = (era_day - era_day / 1460 + era_day / 36524 - era_day / 146096) / 365;
  auto year = era_year + era * 400;
  const auto year_day = era_day - (365 * era_year + era_year / 4 - era_year / 100);
  const auto march_month = (5 * year_day + 2) / 153;
  const auto day = year_day - (153 * march_month + 2) / 5 + 1;
  const auto month = march_month + (march_month < 10 ? 3 : -9);
  year += month <= 2;
  const auto weekday = ((days + 4) % 7 + 7) % 7;
  return DateTime{static_cast<int>(year), static_cast<int>(month), static_cast<int>(day),
                  static_cast<int>(clock / 3600), static_cast<int>(clock / 60 % 60),
                  static_cast<int>(clock % 60), static_cast<int>(weekday), offset, seconds, millis};
}
} // namespace

// The only background entry point. It touches no Rime state and shares only
// an atomic UTC offset with the candidate pipeline.
extern "C" __attribute__((visibility("default"))) void shixin_date_refresh_zone() {
  tzset();
  const auto now = std::time(nullptr);
  std::tm local{};
  if (localtime_r(&now, &local)) {
    zone_offset.store(local.tm_gmtoff, std::memory_order_relaxed);
    zone_ready.store(true, std::memory_order_release);
  }
}
#endif

void init_date_clock() {
#ifdef __linux__
  if (!zone_ready.load(std::memory_order_acquire))
    shixin_date_refresh_zone();
#elif !defined(_WIN32)
  tzset();
#endif
  (void)date_now();
}

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
  auto seconds = ms / 1000;
  auto millis = ms % 1000;
  if (millis < 0) {
    millis += 1000;
    --seconds;
  }
  const auto secs = static_cast<std::time_t>(seconds);
#ifdef __linux__
  return local_date(secs, zone_offset.load(std::memory_order_relaxed), static_cast<int>(millis));
#else
  std::tm tm{};
  localtime_r(&secs, &tm);
  return DateTime{tm.tm_year + 1900, tm.tm_mon + 1,
                  tm.tm_mday,        tm.tm_hour,
                  tm.tm_min,         tm.tm_sec,
                  tm.tm_wday,        tm.tm_gmtoff,
                  seconds,          static_cast<int>(millis)};
#endif
#endif
}
} // namespace shixin
