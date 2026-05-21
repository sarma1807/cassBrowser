package main

import (
	"time"

	"golang.org/x/sys/unix"
)

// getFileCreatedTime returns the file's birth (creation) time using the Linux statx syscall.
// Returns zero time and false if the filesystem does not support birth time.
func getFileCreatedTime(path string) (time.Time, bool) {
	var stat unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &stat); err != nil {
		return time.Time{}, false
	}
	if stat.Mask&unix.STATX_BTIME == 0 {
		return time.Time{}, false
	}
	return time.Unix(stat.Btime.Sec, int64(stat.Btime.Nsec)), true
}
