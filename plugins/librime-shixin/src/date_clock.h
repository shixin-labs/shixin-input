// Current local time for the date translator. Windows reads the OS clock
// and current bias directly instead of the CRT's process-lifetime zone cache.
#pragma once
#include "dates.h"

namespace shixin {
DateTime date_now();
}
