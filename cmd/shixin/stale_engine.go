package main

import "time"

// predates reports whether a process that started at started is running
// code older than a binary installed at installed. A second of slack keeps
// clock granularity from restarting an engine started right after install.
func predates(started, installed time.Time) bool {
	return started.Before(installed.Add(-time.Second))
}
