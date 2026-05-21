//go:build !windows

package main

import "syscall"

func setFileCreationMask() {
	syscall.Umask(0077)
}
