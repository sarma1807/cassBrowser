//go:build windows

package main

import (
	"time"

	"golang.org/x/sys/windows"
)

func getDiskSpaceInfo(path string) (total, free uint64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeToCaller, totalBytes, totalFree uint64
	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeToCaller, &totalBytes, &totalFree)
	if err != nil {
		return 0, 0, err
	}
	return totalBytes, freeToCaller, nil
}

func getFileCreatedTime(path string) (time.Time, bool) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return time.Time{}, false
	}
	h, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(h)

	var ctime, atime, wtime windows.Filetime
	if err := windows.GetFileTime(h, &ctime, &atime, &wtime); err != nil {
		return time.Time{}, false
	}
	ns := ctime.Nanoseconds()
	if ns <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, ns), true
}
