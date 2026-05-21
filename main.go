package main

import (
	"fmt"
	"os"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"
)

func main() {
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		fmt.Fprintln(os.Stderr, "ERROR : this app requires GUI - cannot execute in console.")
		os.Exit(1)
	}

	fmt.Println("App is starting - Thanks for your patience while it loads everything ...")

	// Restrict all file creation to owner read+write only (0600 for files, 0700 for dirs).
	setFileCreationMask()

	// Ensure config directory exists
	err := ensureConfigDir()
	if err != nil {
		fmt.Printf("Error creating config directory : %v\n", err)
		os.Exit(1)
	}

	// Run one-time file mutations in the background so app.New() and
	// migration work in parallel. State must not load until this finishes
	// because both steps rewrite the connections file.
	migrationDone := make(chan struct{})
	go func() {
		defer close(migrationDone)
		migrateConnectionsEncryption() //nolint
		deduplicateConnectionNames()   //nolint
	}()

	// Create the Fyne application (runs concurrently with migration above)
	a := app.New()
	a.SetIcon(theme.VisibilityIcon())

	// Connections file must be fully migrated before NewAppState reads it.
	<-migrationDone

	// Create application state
	state := NewAppState()

	// Initialize logger (may be empty on first launch until working dir is configured)
	InitAppLogger(state.GetWorkingDirectory())
	AppLog("app started")

	// Ensure working subdirectories exist (appLogs, data, temp)
	if state.IsWorkingDirectoryConfigured() {
		if err := ensureWorkingSubDirectories(state.GetWorkingDirectory()); err != nil {
			AppLog("Warning : could not create working sub-directories : " + err.Error())
		}
	}

	// Apply saved theme scheme
	a.Settings().SetTheme(NewCassBrowserThemeWithScheme(state.GetThemeScheme()))

	// Create the main window
	w := a.NewWindow(AppDisplayName + " by oramad")
	state.MainWindow = w

	// Set initial window size - maximize on first launch
	if state.IsFirstLaunch() {
		w.Resize(fyne.NewSize(1280, 720))
	} else {
		width, height := state.GetWindowSize()
		w.Resize(fyne.NewSize(width, height))
	}

	// Start app stats tracker (runs every 30s when capture_app_stats is enabled)
	NewAppStatsTracker(state).Start()

	// Create main UI with debounced resize (150ms delay)
	mainUI := NewMainWindow(state, w)
	debouncedContent := NewDebouncedContainer(mainUI.Content(), 150)
	w.SetContent(debouncedContent)

	// Create main menu
	setupMainMenu(w, state, mainUI)

	// Save window size and clean up when the window is actually destroyed.
	w.SetOnClosed(func() {
		AppLog("app closed")
		appStats.Stop()
		size := w.Canvas().Size()
		state.SetWindowSize(size.Width, size.Height)
		state.SaveSettings()
		mainUI.CloseAllSessions()
	})

	// Center and show window
	w.CenterOnScreen()

	// Check if working directory needs configuration
	if state.IsFirstLaunch() || !state.IsWorkingDirectoryConfigured() {
		// Show working directory setup in right panel
		mainUI.ShowWorkingDirectorySetup(true, nil)
	} else {
		// Show About on app start
		mainUI.ShowAbout()
	}

	w.ShowAndRun()
	CloseAppLogger()
}

// setupMainMenu creates the application menu bar
func setupMainMenu(w fyne.Window, state *AppState, mainUI *MainWindow) {
	var rebuildMenu func()

	rebuildMenu = func() {
		// Connections menu items
		addConnectionItem := fyne.NewMenuItem("Add New Connection", func() {
			mainUI.ShowAddConnectionForm()
		})

		disconnectAllItem := fyne.NewMenuItem("Disconnect All", func() {
			mainUI.DisconnectAll()
		})
		disconnectAllItem.Disabled = !mainUI.HasActiveSessions()

		// Connections menu
		connectionsMenu := fyne.NewMenu("Connections", addConnectionItem, fyne.NewMenuItemSeparator(), disconnectAllItem)

		// Settings menu items
		fontSizeItem := fyne.NewMenuItem("Font Size", func() {
			mainUI.ShowFontSettings()
		})

		colorsItem := fyne.NewMenuItem("Colors", func() {
			mainUI.ShowColorSettings()
		})

		workingDirItem := fyne.NewMenuItem("Working Directory", func() {
			mainUI.ShowWorkingDirectorySetup(false, nil)
		})

		var appStatsItem *fyne.MenuItem
		if state.GetCaptureAppStats() {
			appStatsItem = fyne.NewMenuItem("Disable App Stats", func() {
				state.SetCaptureAppStats(false)
				rebuildMenu()
			})
		} else {
			appStatsItem = fyne.NewMenuItem("Enable App Stats", func() {
				state.SetCaptureAppStats(true)
				rebuildMenu()
			})
		}

		settingsMenu := fyne.NewMenu("Settings", fontSizeItem, colorsItem, workingDirItem, appStatsItem)

		helpMenu := fyne.NewMenu("Help", fyne.NewMenuItem("About this App", func() {
			mainUI.ShowAbout()
		}))

		// Create main menu with Connections first, Help last
		mainMenu := fyne.NewMainMenu(connectionsMenu, settingsMenu, helpMenu)
		w.SetMainMenu(mainMenu)
	}

	state.OnMenuChange = rebuildMenu
	rebuildMenu()
	mainUI.SetRebuildMenu(rebuildMenu)
}
