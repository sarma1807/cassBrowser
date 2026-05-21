package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const logDirName = "appLogs"

type appLoggerState struct {
	mu          sync.Mutex
	workingDir  string
	currentDate string
	file        *os.File
}

var appLogger appLoggerState

// InitAppLogger initializes (or reinitializes) the logger with the given working directory.
// Safe to call multiple times; closes any open log file before switching directories.
func InitAppLogger(workingDir string) {
	appLogger.mu.Lock()
	defer appLogger.mu.Unlock()

	if appLogger.file != nil {
		appLogger.file.Close()
		appLogger.file = nil
	}
	appLogger.workingDir = workingDir
	appLogger.currentDate = ""
}

// AppLog writes a timestamped entry to today's log file.
// Silently does nothing if working directory is not configured.
func AppLog(message string) {
	appLogger.mu.Lock()
	defer appLogger.mu.Unlock()

	if appLogger.workingDir == "" {
		return
	}

	now := time.Now()
	dateStr := now.Format("20060102")
	timestamp := now.Format("2006-01-02 15:04:05")

	if appLogger.currentDate != dateStr {
		if appLogger.file != nil {
			appLogger.file.Close()
			appLogger.file = nil
		}
		appLogger.currentDate = dateStr
	}

	if appLogger.file == nil {
		logDir := filepath.Join(appLogger.workingDir, logDirName)
		if err := os.MkdirAll(logDir, 0700); err != nil {
			return
		}
		logFile := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", AppName, dateStr))
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		appLogger.file = f
	}

	fmt.Fprintf(appLogger.file, "%s | %s\n", timestamp, message)
}

// CloseAppLogger flushes and closes the current log file.
func CloseAppLogger() {
	appLogger.mu.Lock()
	defer appLogger.mu.Unlock()
	if appLogger.file != nil {
		appLogger.file.Close()
		appLogger.file = nil
	}
}
