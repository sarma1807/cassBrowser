package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gocql/gocql"
)

// MainWindow represents the main application window layout
type MainWindow struct {
	state             *AppState
	window            fyne.Window
	split             *container.Split
	leftPanel         *ConnectionTree
	detailPanel       *DetailPanel
	statusLabel       *widget.Label
	titleText         *canvas.Text
	content           *fyne.Container
	leftFull          *fyne.Container
	leftStrip         *fyne.Container
	leftWrapper       *fyne.Container
	rightContainer    *fyne.Container
	collapsedLayout   *fyne.Container
	mainWrapper       *fyne.Container
	collapsed         bool
	savedSplitOffset  float64
	rebuildMenu       func()
	viewExtractionBtn *widget.Button
}

// NewMainWindow creates the main window UI
func NewMainWindow(state *AppState, window fyne.Window) *MainWindow {
	mw := &MainWindow{
		state:  state,
		window: window,
	}

	// Create status bar
	mw.statusLabel = widget.NewLabel("Ready")

	// Create panels
	mw.leftPanel = NewConnectionTree(state, mw.onConnectionSelected, mw.onAddConnection, mw.DisconnectAll)
	mw.leftPanel.SetOnTableSelected(mw.onTableSelected)
	mw.leftPanel.SetOnConnectionExpanded(mw.onConnectionExpanded)
	mw.leftPanel.SetOnKeyspaceSelected(mw.onKeyspaceSelected)
	mw.leftPanel.SetOnLocalFileSelected(mw.onLocalFileSelected)
	mw.leftPanel.SetOnJoinLocalSelected(mw.onJoinLocalSelected)
	mw.leftPanel.SetOnBrowseLocalNoFiles(mw.onBrowseLocalNoFiles)
	mw.leftPanel.SetOnConnectionError(mw.onConnectionError)
	mw.leftPanel.SetOnAppStatsSelected(mw.onAppStatsSelected)
	mw.leftPanel.SetOnEnvironmentToggled(mw.SetStatusReady)
	mw.detailPanel = NewDetailPanel(state, window, mw.refreshTree, mw.SetStatus)
	mw.detailPanel.SetOnSelectTreeConnection(mw.leftPanel.SelectConnection)
	mw.detailPanel.SetOnRescanLocalFiles(mw.leftPanel.RescanLocalFiles)
	mw.detailPanel.SetOnSelectTreeLocalFile(mw.leftPanel.SelectLocalFile)

	// Create split container (30/70) with padding on sides
	leftPadding := canvas.NewRectangle(color.Transparent)
	leftPadding.SetMinSize(fyne.NewSize(8, 0))
	rightPadding := canvas.NewRectangle(color.Transparent)
	rightPadding.SetMinSize(fyne.NewSize(8, 0))

	collapseBtn := widget.NewButton("◀", mw.toggleLeftPanel)
	connectionsHeader := container.NewBorder(nil, nil, nil, collapseBtn,
		widget.NewLabel("Connections"))

	mw.viewExtractionBtn = widget.NewButtonWithIcon("View Active Extraction", theme.MediaFastForwardIcon(), func() {
		if fn := state.GetExtractionNavFn(); fn != nil {
			fn()
		}
	})
	mw.viewExtractionBtn.Importance = widget.HighImportance
	mw.viewExtractionBtn.Hide()

	mw.leftFull = container.NewBorder(
		connectionsHeader,
		container.NewVBox(widget.NewSeparator(), mw.viewExtractionBtn),
		leftPadding,
		rightPadding,
		mw.leftPanel.Container(),
	)

	expandBtn := widget.NewButton("▶", mw.toggleLeftPanel)
	mw.leftStrip = container.NewVBox(expandBtn)

	leftPadding2 := canvas.NewRectangle(color.Transparent)
	leftPadding2.SetMinSize(fyne.NewSize(8, 0))
	rightPadding2 := canvas.NewRectangle(color.Transparent)
	rightPadding2.SetMinSize(fyne.NewSize(8, 0))

	mw.rightContainer = container.NewBorder(
		nil, nil,
		leftPadding2,
		rightPadding2,
		mw.detailPanel.Container(),
	)

	mw.savedSplitOffset = 0.25
	mw.leftWrapper = container.NewStack(mw.leftFull)
	mw.split = NewDebouncedHSplit(mw.leftWrapper, mw.rightContainer, 150)
	mw.split.Offset = mw.savedSplitOffset
	mw.collapsedLayout = container.NewBorder(nil, nil, mw.leftStrip, nil, mw.rightContainer)
	mw.mainWrapper = container.NewStack(mw.split)

	// Create header
	header := mw.createHeader()

	// Create status bar
	statusBar := mw.createStatusBar()

	// Combine into main layout
	mw.content = container.NewBorder(
		header,
		statusBar,
		nil, nil,
		mw.mainWrapper,
	)

	// Set up state change callback
	state.OnChange = func() {
		mw.Refresh()
	}

	return mw
}

// Content returns the main content container
func (mw *MainWindow) Content() fyne.CanvasObject {
	return mw.content
}

// createHeader creates the header bar with title and datetime
func (mw *MainWindow) createHeader() fyne.CanvasObject {
	// Get font size from state (name/bold/italic are hardcoded to monospace below)
	_, fontSize, _, _ := mw.state.GetAppFontSettings()

	// Apply font size to theme
	if th, ok := fyne.CurrentApp().Settings().Theme().(*CassBrowserTheme); ok {
		th.SetCustomFontSize(fontSize)
		fyne.CurrentApp().Settings().SetTheme(th)
	}

	mw.titleText = canvas.NewText(AppDisplayName+" by oramad", nil)
	mw.titleText.TextSize = fontSize

	spacer := layout.NewSpacer()

	return container.NewHBox(
		mw.titleText,
		spacer,
	)
}

// RefreshTitle updates the title with current font settings
func (mw *MainWindow) RefreshTitle() {
	if mw.titleText == nil {
		return
	}

	_, fontSize, _, _ := mw.state.GetAppFontSettings()

	if th, ok := fyne.CurrentApp().Settings().Theme().(*CassBrowserTheme); ok {
		th.SetCustomFontSize(fontSize)
		fyne.CurrentApp().Settings().SetTheme(th)
	}

	mw.titleText.TextSize = fontSize
	mw.titleText.Refresh()
}

// createStatusBar creates the status bar at the bottom
func (mw *MainWindow) createStatusBar() fyne.CanvasObject {
	spacer := widget.NewLabel("")
	return container.NewHBox(
		widget.NewLabel("Status :"),
		container.NewVBox(mw.statusLabel, spacer),
	)
}

// SetStatusReady resets the status bar to the default application tagline.
func (mw *MainWindow) SetStatusReady() {
	mw.SetStatus(AppTagline)
}

// SetStatus updates the status bar message
func (mw *MainWindow) SetStatus(message string) {
	mw.statusLabel.SetText(message)
}

// Refresh updates the UI components
func (mw *MainWindow) Refresh() {
	mw.leftPanel.Refresh()
	mw.detailPanel.Refresh()
	mw.RefreshTitle()
	if mw.state.IsExtractionRunning() {
		mw.viewExtractionBtn.Show()
	} else {
		mw.viewExtractionBtn.Hide()
	}
}

// refreshTree refreshes just the connection tree
func (mw *MainWindow) refreshTree() {
	mw.leftPanel.Refresh()
}

// onConnectionSelected handles connection selection from the tree
func (mw *MainWindow) onConnectionSelected(conn *DecryptedConnection) {
	mw.state.SetSelectedConnection(conn)
	// Check if connection is already connected (has an active session)
	session := mw.leftPanel.GetSession(conn.ConnName)
	if session != nil {
		// Show cluster info for connected connections
		mw.detailPanel.ShowClusterInfoFromSession(conn, session)
		mw.SetStatus("Connected to : " + conn.ConnName)
	} else {
		// Show connection details for disconnected connections
		mw.detailPanel.ShowConnectionDetails(conn)
		mw.SetStatus("Selected Connection : " + conn.ConnName)
	}
}

// onAddConnection handles the add connection action
func (mw *MainWindow) onAddConnection() {
	mw.ShowAddConnectionForm()
}

// onTableSelected handles table selection from the tree
func (mw *MainWindow) onTableSelected(conn *DecryptedConnection, keyspace, table string) {
	mw.state.SetSelectedConnection(conn)
	mw.SetStatus("Table : " + keyspace + "." + table)
	// Show table details in the detail panel
	mw.detailPanel.ShowTableDetails(conn, keyspace, table, mw.leftPanel.GetSession(conn.ConnName))
}

// onConnectionExpanded handles when a connection is expanded (connected)
func (mw *MainWindow) onConnectionExpanded(conn *DecryptedConnection, session *gocql.Session) {
	mw.state.SetSelectedConnection(conn)
	mw.SetStatus("Connected to : " + conn.ConnName)
	mw.detailPanel.ShowClusterInfoFromSession(conn, session)
	if mw.rebuildMenu != nil {
		mw.rebuildMenu()
	}
}

// onConnectionError handles connection failures during tree expansion
func (mw *MainWindow) onConnectionError(conn *DecryptedConnection, err error) {
	mw.state.SetSelectedConnection(conn)
	mw.detailPanel.ShowConnectionDetailsWithError(conn, err)
	mw.SetStatus("Connection failed : " + conn.ConnName)
}

// onLocalFileSelected handles local parquet file selection from the tree
func (mw *MainWindow) onLocalFileSelected(path string) {
	mw.SetStatus("Local file : " + path)
	mw.detailPanel.ShowLocalFileView(path)
}

// onBrowseLocalNoFiles handles the case where Data In Local Files is opened but no parquet files exist
func (mw *MainWindow) onBrowseLocalNoFiles() {
	mw.SetStatus("No parquet files are found.")
	_, appFontSize, _, _ := mw.state.GetAppFontSettings()
	titleLabel := widget.NewLabel("Data In Local Files")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}
	errMsg := newErrorText("No parquet files are found.", appFontSize)
	errMsg.TextStyle = fyne.TextStyle{Bold: true}
	mw.detailPanel.setContent(container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		widget.NewLabel(""),
		widget.NewLabel(""),
		errMsg,
	))
}

// onJoinLocalSelected handles clicks on the JOIN Data In Local Files tree node
func (mw *MainWindow) onJoinLocalSelected() {
	mw.SetStatus("JOIN Data In Local Files")
	mw.detailPanel.ShowJoinLocalFileList()
}

// onAppStatsSelected handles clicks on the App Stats tree node
func (mw *MainWindow) onAppStatsSelected() {
	mw.SetStatus("App Stats")
	mw.detailPanel.ShowAppStats()
}

// onKeyspaceSelected handles keyspace selection from the tree
func (mw *MainWindow) onKeyspaceSelected(conn *DecryptedConnection, keyspace string, session *gocql.Session) {
	mw.state.SetSelectedConnection(conn)
	mw.SetStatus("Keyspace : " + keyspace)
	// Show keyspace info in the detail panel
	mw.detailPanel.ShowKeyspaceInfo(conn, keyspace, session)
}

// ShowAddConnectionForm displays the add connection form in the detail panel
func (mw *MainWindow) ShowAddConnectionForm() {
	mw.detailPanel.ShowAddConnectionForm()
	mw.SetStatus("Adding new connection")
}

// ShowEditConnectionForm displays the edit connection form
func (mw *MainWindow) ShowEditConnectionForm(conn *DecryptedConnection) {
	mw.detailPanel.ShowEditConnectionForm(conn)
	mw.SetStatus("Editing connection : " + conn.ConnName)
}

// ShowColorSettings displays the color settings panel
func (mw *MainWindow) ShowColorSettings() {
	mw.detailPanel.ShowColorSettings()
	mw.SetStatus("Color Settings - Select a theme")
}

// ShowFontSettings displays the font settings panel
func (mw *MainWindow) ShowFontSettings() {
	mw.detailPanel.ShowFontSettings()
	mw.SetStatus("Font Settings - Configure title font")
}

// ShowWorkingDirectorySetup displays the working directory setup panel
func (mw *MainWindow) ShowWorkingDirectorySetup(isInitialSetup bool, onComplete func()) {
	mw.detailPanel.ShowWorkingDirectorySetup(isInitialSetup, onComplete)
	mw.SetStatus("Working Directory Setup")
}

// ShowAbout displays the about information in the detail panel
func (mw *MainWindow) ShowAbout() {
	mw.detailPanel.ShowAbout()
	mw.SetStatus("About " + AppDisplayName)
}

// CloseAllSessions closes all open Cassandra sessions in the connection tree
func (mw *MainWindow) CloseAllSessions() {
	mw.leftPanel.CloseAllSessions()
}

// toggleLeftPanel collapses or expands the left connection panel on demand.
// When collapsed the split widget is removed entirely, so the resize divider
// is not visible or draggable.
func (mw *MainWindow) toggleLeftPanel() {
	if mw.collapsed {
		mw.split.Offset = mw.savedSplitOffset
		mw.mainWrapper.Objects = []fyne.CanvasObject{mw.split}
		mw.collapsed = false
	} else {
		mw.savedSplitOffset = mw.split.Offset
		mw.mainWrapper.Objects = []fyne.CanvasObject{mw.collapsedLayout}
		mw.collapsed = true
	}
	mw.mainWrapper.Refresh()
}

// DisconnectAll gracefully closes all active Cassandra sessions, refreshes the
// tree, resets the right panel, and updates the status bar.
func (mw *MainWindow) DisconnectAll() {
	AppLog("requested to disconnect from all Cassandra connections")
	mw.leftPanel.CloseAllSessions()
	mw.leftPanel.CollapseAllEnvBranches()
	mw.leftPanel.Refresh()
	mw.detailPanel.ShowAbout()
	mw.SetStatus("Disconnected from all Cassandra connections")
	if mw.rebuildMenu != nil {
		mw.rebuildMenu()
	}
}

// HasActiveSessions reports whether any Cassandra sessions are currently open.
func (mw *MainWindow) HasActiveSessions() bool {
	return mw.leftPanel.HasActiveSessions()
}

// SetRebuildMenu stores a callback that rebuilds the menu bar.
func (mw *MainWindow) SetRebuildMenu(fn func()) {
	mw.rebuildMenu = fn
}
