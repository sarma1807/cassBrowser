package main

import (
	"syscall"
	"time"
)

// getFileCreatedTime returns the file's birth (creation) time using the macOS stat Birthtimespec.
// Returns zero time and false if birth time is unavailable.
func getFileCreatedTime(path string) (time.Time, bool) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return time.Time{}, false
	}
	ts := stat.Birthtimespec
	if ts.Sec == 0 && ts.Nsec == 0 {
		return time.Time{}, false
	}
	return time.Unix(ts.Sec, ts.Nsec), true
}
