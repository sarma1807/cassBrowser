//go:build windows

package main

func setFileCreationMask() {
	// Windows does not support umask; per-file permissions are set via ACLs
}
