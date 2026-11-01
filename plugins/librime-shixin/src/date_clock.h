// Current local time for the date translator. Windows reads the OS clock
// and current bias directly instead of the CRT's process-lifetime zone cache.
#pragma once
#include "dates.h"

namespace shixin {
// Initialize before serving keys; Linux refreshes are performed by the Go
// host in the background, never from date_now().
void init_date_clock();
DateTime date_now();
}
