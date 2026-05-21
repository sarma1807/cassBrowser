//go:build !windows

package main

import (
	"syscall"
	"time"
)

type cpuMeasureState struct {
	rusage      syscall.Rusage
	captureTime time.Time
}

func initCPUMeasure() cpuMeasureState {
	var r syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	return cpuMeasureState{rusage: r, captureTime: time.Now()}
}

func measureCPUDelta(prev *cpuMeasureState) (cpuPct float64, next cpuMeasureState) {
	var r syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	now := time.Now()
	elapsed := now.Sub(prev.captureTime).Seconds()
	uDelta := float64(r.Utime.Sec-prev.rusage.Utime.Sec) + float64(r.Utime.Usec-prev.rusage.Utime.Usec)/1e6
	sDelta := float64(r.Stime.Sec-prev.rusage.Stime.Sec) + float64(r.Stime.Usec-prev.rusage.Stime.Usec)/1e6
	if elapsed > 0 {
		cpuPct = ((uDelta + sDelta) / elapsed) * 100
	}
	return cpuPct, cpuMeasureState{rusage: r, captureTime: now}
}
