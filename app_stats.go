package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const statsFilePrefix = "appStats"
const statsHeader = "### am = app memory usage in bytes | ac = app CPU usage % | dm = DuckDB memory usage in bytes ###"

var appStats *AppStatsTracker

type AppStatsTracker struct {
	state        *AppState
	stopCh       chan struct{}
	prevCPUState cpuMeasureState
}

func NewAppStatsTracker(state *AppState) *AppStatsTracker {
	appStats = &AppStatsTracker{
		state:        state,
		stopCh:       make(chan struct{}),
		prevCPUState: initCPUMeasure(),
	}
	return appStats
}

func (t *AppStatsTracker) Start() {
	go t.run()
}

func (t *AppStatsTracker) Stop() {
	close(t.stopCh)
}

func (t *AppStatsTracker) run() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if t.state.GetCaptureAppStats() {
				t.capture()
			}
		case <-t.stopCh:
			return
		}
	}
}

func (t *AppStatsTracker) capture() {
	workDir := t.state.GetWorkingDirectory()
	if workDir == "" {
		return
	}

	cpuPct, newState := measureCPUDelta(&t.prevCPUState)
	t.prevCPUState = newState
	now := newState.captureTime

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	logPath := filepath.Join(workDir, logDirName, fmt.Sprintf("%s_%s.log", statsFilePrefix, now.Format("20060102")))

	_, statErr := os.Stat(logPath)
	isNew := os.IsNotExist(statErr)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	if isNew {
		fmt.Fprintln(f, statsHeader)
	}

	ts := now.Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "%s | am=%d\n", ts, ms.Sys)
	fmt.Fprintf(f, "%s | ac=%.1f\n", ts, cpuPct)
}

// logDuckDBMemory queries duckdb_memory() on the active session and appends a
// dm= entry to today's stats file. Safe to call on a nil receiver or when
// stats capture is disabled.
func (t *AppStatsTracker) logDuckDBMemory(db *sql.DB) {
	if t == nil || !t.state.GetCaptureAppStats() {
		return
	}
	workDir := t.state.GetWorkingDirectory()
	if workDir == "" {
		return
	}

	var total int64
	row := db.QueryRow("SELECT COALESCE(sum(memory_usage_bytes), 0) FROM duckdb_memory()")
	if err := row.Scan(&total); err != nil {
		return
	}

	now := time.Now()
	logPath := filepath.Join(workDir, logDirName, fmt.Sprintf("%s_%s.log", statsFilePrefix, now.Format("20060102")))

	_, statErr := os.Stat(logPath)
	isNew := os.IsNotExist(statErr)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	if isNew {
		fmt.Fprintln(f, statsHeader)
	}

	fmt.Fprintf(f, "%s | dm=%d\n", now.Format("2006-01-02 15:04:05"), total)
}
