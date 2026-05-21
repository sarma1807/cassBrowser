package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var configDir string
var settingsFile string

const rowsPerPage = 20

// Settings holds user preferences
type Settings struct {
	WindowWidth      float32 `json:"window_width"`
	WindowHeight     float32 `json:"window_height"`
	ThemeScheme      string  `json:"theme_scheme"`
	WorkingDirectory string  `json:"working_directory"`
	// App font size - used to scale all UI elements
	AppFontSize float32 `json:"app_font_size"`
	// Grid font size - used specifically for data grids/tables
	GridFontSize    float32 `json:"grid_font_size"`
	CaptureAppStats bool    `json:"capture_app_stats"`
	FirstLaunch     bool    `json:"-"` // Not persisted, used to detect first launch
}

func initSettingsPaths() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home dir not found
		configDir = "." + AppName
	} else {
		configDir = filepath.Join(homeDir, "."+AppName)
	}
	settingsFile = filepath.Join(configDir, AppName+".settings")
}

func init() {
	initSettingsPaths()
}

func ensureConfigDir() error {
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return os.MkdirAll(configDir, 0700)
	}
	return nil
}

func loadSettings() Settings {
	settings := Settings{
		WindowWidth:  1200,            // Default window width
		WindowHeight: 800,             // Default window height
		ThemeScheme:  "Midnight Gold", // Default theme scheme
		AppFontSize:  18,              // Default font size
		GridFontSize: 14,              // Default: slightly smaller for data
	}

	data, err := os.ReadFile(settingsFile)
	if err != nil {
		// File doesn't exist or can't be read - this is first launch
		settings.FirstLaunch = true
		saveSettings(settings)
		return settings
	}

	json.Unmarshal(data, &settings)

	// Ensure WindowWidth has a valid value
	if settings.WindowWidth <= 0 {
		settings.WindowWidth = 1200
	}

	// Ensure WindowHeight has a valid value
	if settings.WindowHeight <= 0 {
		settings.WindowHeight = 800
	}

	// Ensure ThemeScheme has a valid value
	if settings.ThemeScheme == "" {
		settings.ThemeScheme = "Midnight Gold"
	}

	// Ensure AppFontSize has a valid value
	if settings.AppFontSize <= 0 {
		settings.AppFontSize = 18
	}

	// Ensure GridFontSize has a valid value
	if settings.GridFontSize <= 0 {
		settings.GridFontSize = 14
	}

	return settings
}

func saveSettings(settings Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsFile, append(data, '\n'), 0600)
}

// getDefaultWorkingDirectory returns the default working directory path
func getDefaultWorkingDirectory() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", AppName+"_work")
	}
	return filepath.Join(homeDir, AppName+"_work")
}

// validateWorkingDirectory checks if a path is valid for use as working directory
func validateWorkingDirectory(path string) error {
	if path == "" {
		return errEmptyPath
	}

	// Check if path is absolute (recommend but allow relative)
	if !filepath.IsAbs(path) {
		// Convert to absolute for validation
		absPath, err := filepath.Abs(path)
		if err != nil {
			return errInvalidPath
		}
		path = absPath
	}

	// Check if directory exists
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		// Directory doesn't exist, check if parent exists and we can create it
		parent := filepath.Dir(path)
		parentInfo, parentErr := os.Stat(parent)
		if os.IsNotExist(parentErr) {
			return errParentNotExists
		}
		if parentErr != nil {
			return errCannotAccessParent
		}
		if !parentInfo.IsDir() {
			return errParentNotDir
		}
		// Test write permission on parent without creating the target directory
		testFile := filepath.Join(parent, ".cassbrowser_test")
		f, createErr := os.OpenFile(testFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if createErr != nil {
			return errCannotCreateDir
		}
		f.Close()
		os.Remove(testFile)
		return nil
	} else if err != nil {
		return errCannotAccessPath
	} else if !info.IsDir() {
		return errNotADirectory
	}

	// Test write permission by creating a temp file
	testFile := filepath.Join(path, ".cassbrowser_test")
	f, err := os.OpenFile(testFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errNoWritePermission
	}
	f.Close()

	// Test read permission
	_, err = os.ReadFile(testFile)
	if err != nil {
		os.Remove(testFile)
		return errNoReadPermission
	}

	// Clean up test file
	os.Remove(testFile)

	return nil
}

// ensureWorkingSubDirectories creates the working directory itself and its required
// subdirectories (appLogs, data, temp). Safe to call repeatedly — existing
// directories are left unchanged.
func ensureWorkingSubDirectories(basePath string) error {
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return fmt.Errorf("could not create working directory: %w", err)
	}
	for _, sub := range []string{"appLogs", "data", "temp"} {
		if err := os.MkdirAll(filepath.Join(basePath, sub), 0700); err != nil {
			return fmt.Errorf("could not create %s directory: %w", sub, err)
		}
	}
	return nil
}

// CalculateDirSize returns the total size in bytes of all files in a directory tree.
// Unreadable entries are skipped silently.
func CalculateDirSize(path string) (uint64, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0, nil
	}
	var total uint64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !info.IsDir() {
			total += uint64(info.Size())
		}
		return nil
	})
	return total, err
}

// MigrateWorkingDirectory moves all contents from old to new working directory,
// then removes the old directory contents after a successful copy.
func MigrateWorkingDirectory(oldPath, newPath string) error {
	if err := ensureWorkingSubDirectories(newPath); err != nil {
		return err
	}

	// Copy all contents from old into new
	if err := copyDirectoryContents(oldPath, newPath); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	// Remove old directory contents after successful copy
	entries, err := os.ReadDir(oldPath)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(oldPath, entry.Name())); err != nil {
			return fmt.Errorf("cleanup failed: %w", err)
		}
	}

	return nil
}

// copyDirectoryContents copies all files from src to dst directory
func copyDirectoryContents(src, dst string) error {
	// Check if source exists
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // Nothing to copy
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := os.MkdirAll(dstPath, 0700); err != nil {
				return err
			}
			if err := copyDirectoryContents(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0600); err != nil {
				return err
			}
		}
	}

	return nil
}

// Validation error messages
var (
	errEmptyPath          = &ValidationError{"Path cannot be empty"}
	errInvalidPath        = &ValidationError{"Invalid path format"}
	errParentNotExists    = &ValidationError{"Parent directory does not exist"}
	errCannotAccessParent = &ValidationError{"Cannot access parent directory"}
	errParentNotDir       = &ValidationError{"Parent path is not a directory"}
	errCannotCreateDir    = &ValidationError{"Cannot create directory (check permissions)"}
	errCannotAccessPath   = &ValidationError{"Cannot access the specified path"}
	errNotADirectory      = &ValidationError{"Path exists but is not a directory"}
	errNoWritePermission  = &ValidationError{"No write permission for this directory"}
	errNoReadPermission   = &ValidationError{"No read permission for this directory"}
)

// ValidationError represents a working directory validation error
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
