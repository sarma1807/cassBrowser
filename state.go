package main

import (
	"sync"

	"fyne.io/fyne/v2"
)

// AppState holds the application state with thread-safe access
type AppState struct {
	mu sync.RWMutex

	// UI State
	WindowWidth      float32
	WindowHeight     float32
	ThemeScheme      string
	WorkingDirectory string
	AppFontSize      float32
	GridFontSize     float32
	CaptureAppStats  bool
	FirstLaunch      bool

	// Connection Data
	Connections          []DecryptedConnection
	SelectedConnection   *DecryptedConnection
	ExpandedEnvironments map[string]bool

	// Query State
	QueryInput         string
	QueryResultColumns []string
	QueryResultRows    []map[string]interface{}

	// Extraction lock — only one extraction job may run at a time
	ExtractionRunning bool
	ExtractionNavFn   func() // navigates back to the active extraction panel

	// UI references for updates
	MainWindow   fyne.Window
	OnChange     func() // Callback when state changes
	OnMenuChange func() // Callback to rebuild the menu bar
}

// NewAppState creates a new AppState with default values
func NewAppState() *AppState {
	settings := loadSettings()
	connections, _ := loadDecryptedConnections()

	return &AppState{
		WindowWidth:          settings.WindowWidth,
		WindowHeight:         settings.WindowHeight,
		ThemeScheme:          settings.ThemeScheme,
		WorkingDirectory:     settings.WorkingDirectory,
		AppFontSize:          settings.AppFontSize,
		GridFontSize:         settings.GridFontSize,
		CaptureAppStats:      settings.CaptureAppStats,
		FirstLaunch:          settings.FirstLaunch,
		Connections:          connections,
		ExpandedEnvironments: make(map[string]bool),
	}
}

// GetConnections returns the connections list. Callers must not modify the
// returned slice or its elements; call SetConnections to update state.
func (s *AppState) GetConnections() []DecryptedConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Connections
}

// SetConnections updates the connections list
func (s *AppState) SetConnections(connections []DecryptedConnection) {
	s.mu.Lock()
	s.Connections = connections
	s.mu.Unlock()
	s.NotifyChange()
}

// ReloadConnections reloads connections from disk
func (s *AppState) ReloadConnections() error {
	connections, err := loadDecryptedConnections()
	if err != nil {
		return err
	}
	s.SetConnections(connections)
	return nil
}

// GetSelectedConnection returns the currently selected connection
func (s *AppState) GetSelectedConnection() *DecryptedConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SelectedConnection
}

// SetSelectedConnection sets the currently selected connection
func (s *AppState) SetSelectedConnection(conn *DecryptedConnection) {
	s.mu.Lock()
	s.SelectedConnection = conn
	s.mu.Unlock()
	s.NotifyChange()
}

// IsEnvironmentExpanded checks if an environment is expanded
func (s *AppState) IsEnvironmentExpanded(env string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ExpandedEnvironments[env]
}

// ToggleEnvironment toggles the expanded state of an environment
func (s *AppState) ToggleEnvironment(env string) {
	s.mu.Lock()
	s.ExpandedEnvironments[env] = !s.ExpandedEnvironments[env]
	s.mu.Unlock()
	s.NotifyChange()
}

// SaveSettings persists current settings to disk
func (s *AppState) SaveSettings() {
	s.mu.RLock()
	settings := Settings{
		WindowWidth:      s.WindowWidth,
		WindowHeight:     s.WindowHeight,
		ThemeScheme:      s.ThemeScheme,
		WorkingDirectory: s.WorkingDirectory,
		AppFontSize:      s.AppFontSize,
		GridFontSize:     s.GridFontSize,
		CaptureAppStats:  s.CaptureAppStats,
	}
	s.mu.RUnlock()
	saveSettings(settings)
}

// GetWorkingDirectory returns the current working directory
func (s *AppState) GetWorkingDirectory() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.WorkingDirectory
}

// SetWorkingDirectory sets the working directory and saves to settings
func (s *AppState) SetWorkingDirectory(path string) {
	s.mu.Lock()
	s.WorkingDirectory = path
	s.mu.Unlock()
	s.SaveSettings()
	InitAppLogger(path)
}

// IsWorkingDirectoryConfigured returns true if working directory has been set
func (s *AppState) IsWorkingDirectoryConfigured() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.WorkingDirectory != ""
}

// GetThemeScheme returns the current theme scheme name
func (s *AppState) GetThemeScheme() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ThemeScheme
}

// SetThemeScheme sets the theme scheme and saves to settings
func (s *AppState) SetThemeScheme(schemeName string) {
	s.mu.Lock()
	s.ThemeScheme = schemeName
	s.mu.Unlock()
	s.SaveSettings()
	s.NotifyChange()
}

// GetAppFontSettings returns app font settings.
func (s *AppState) GetAppFontSettings() (name string, size float32, bold, italic bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return "monospace", s.AppFontSize, false, false
}

// GetGridFontSettings returns grid font settings.
func (s *AppState) GetGridFontSettings() (name string, size float32, bold, italic bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return "monospace", s.GridFontSize, false, false
}

// SetAppFontSize sets the app font size and saves
func (s *AppState) SetAppFontSize(size float32) {
	s.mu.Lock()
	s.AppFontSize = size
	s.mu.Unlock()
	s.SaveSettings()
	s.NotifyChange()
}

// SetGridFontSize sets the grid font size and saves
func (s *AppState) SetGridFontSize(size float32) {
	s.mu.Lock()
	s.GridFontSize = size
	s.mu.Unlock()
	s.SaveSettings()
	s.NotifyChange()
}

// IsExtractionRunning returns true if a table extraction job is currently active
func (s *AppState) IsExtractionRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ExtractionRunning
}

// SetExtractionRunning marks a table extraction job as active or inactive.
// When cleared, the navigation function is also cleared and the menu is rebuilt.
func (s *AppState) SetExtractionRunning(running bool) {
	s.mu.Lock()
	s.ExtractionRunning = running
	if !running {
		s.ExtractionNavFn = nil
	}
	onMenuChange := s.OnMenuChange
	s.mu.Unlock()
	if onMenuChange != nil {
		onMenuChange()
	}
	s.NotifyChange()
}

// SetExtractionNavFn stores the function that navigates back to the active extraction panel
func (s *AppState) SetExtractionNavFn(fn func()) {
	s.mu.Lock()
	s.ExtractionNavFn = fn
	s.mu.Unlock()
}

// GetExtractionNavFn returns the active extraction navigation function, or nil if none
func (s *AppState) GetExtractionNavFn() func() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ExtractionNavFn
}

// GetCaptureAppStats returns whether app stats capture is enabled
func (s *AppState) GetCaptureAppStats() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CaptureAppStats
}

// SetCaptureAppStats enables or disables app stats capture and saves settings
func (s *AppState) SetCaptureAppStats(enabled bool) {
	s.mu.Lock()
	s.CaptureAppStats = enabled
	s.mu.Unlock()
	s.SaveSettings()
}

// SetWindowSize updates the window dimensions
func (s *AppState) SetWindowSize(width, height float32) {
	s.mu.Lock()
	s.WindowWidth = width
	s.WindowHeight = height
	s.mu.Unlock()
}

// GetWindowSize returns the window dimensions
func (s *AppState) GetWindowSize() (float32, float32) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.WindowWidth, s.WindowHeight
}

// IsFirstLaunch returns true if this is the first time the app is launched
func (s *AppState) IsFirstLaunch() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FirstLaunch
}

// NotifyChange triggers the onChange callback if set
func (s *AppState) NotifyChange() {
	s.mu.RLock()
	onChange := s.OnChange
	s.mu.RUnlock()
	if onChange != nil {
		onChange()
	}
}
