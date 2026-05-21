//go:build windows

package main

import (
	"time"

	"golang.org/x/sys/windows"
)

type cpuMeasureState struct {
	kernelTime  windows.Filetime
	userTime    windows.Filetime
	captureTime time.Time
}

func initCPUMeasure() cpuMeasureState {
	var creation, exit, kernel, user windows.Filetime
	_ = windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user)
	return cpuMeasureState{kernelTime: kernel, userTime: user, captureTime: time.Now()}
}

func measureCPUDelta(prev *cpuMeasureState) (cpuPct float64, next cpuMeasureState) {
	var creation, exit, kernel, user windows.Filetime
	_ = windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user)
	now := time.Now()
	elapsed := now.Sub(prev.captureTime).Seconds()

	// GetProcessTimes returns accumulated CPU time in 100-nanosecond intervals
	toSec := func(ft windows.Filetime) float64 {
		return float64(int64(ft.HighDateTime)<<32|int64(ft.LowDateTime)) / 1e7
	}

	kDelta := toSec(kernel) - toSec(prev.kernelTime)
	uDelta := toSec(user) - toSec(prev.userTime)
	if elapsed > 0 {
		cpuPct = ((kDelta + uDelta) / elapsed) * 100
	}
	return cpuPct, cpuMeasureState{kernelTime: kernel, userTime: user, captureTime: now}
}
