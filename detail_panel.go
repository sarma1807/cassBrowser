package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gocql/gocql"
	parquet "github.com/parquet-go/parquet-go"
)

// whereClauseColumnCandidates extracts likely column name candidates from a CQL WHERE
// clause by finding identifiers that appear immediately before a comparison operator.
// Used for pre-flight validation against the table's known column set.
var whereClauseOpRe = regexp.MustCompile(`(?i)\b([a-zA-Z_][a-zA-Z0-9_]*)\s*(?:!=|<>|<=|>=|=|<|>|\bIN\b|\bCONTAINS\b|\bLIKE\b)`)

// cql keywords / functions that may appear before an operator but are not column names
var whereClauseSkipWords = map[string]bool{
	"token": true, "writetime": true, "ttl": true,
}

func whereClauseColumnCandidates(whereClause string) []string {
	wc := strings.TrimSpace(whereClause)
	upper := strings.ToUpper(wc)
	if strings.HasPrefix(upper, "WHERE ") || strings.HasPrefix(upper, "WHERE\t") {
		wc = strings.TrimSpace(wc[5:])
	}
	matches := whereClauseOpRe.FindAllStringSubmatch(wc, -1)
	seen := make(map[string]bool)
	var cols []string
	for _, m := range matches {
		col := m[1]
		lower := strings.ToLower(col)
		if whereClauseSkipWords[lower] || seen[lower] {
			continue
		}
		seen[lower] = true
		cols = append(cols, col)
	}
	return cols
}

// runTestConnection performs a connection test and updates a canvas.Text result inline.
// Shows "Testing..." immediately, then green ✓ or red ✗ when the goroutine completes.
func runTestConnection(conn DecryptedConnection, resultContainer *fyne.Container, state *AppState, setStatus func(string)) {
	_, fontSize, isBold, isItalic := state.GetAppFontSettings()
	if fontSize <= 0 {
		fontSize = 18
	}
	style := fyne.TextStyle{Bold: isBold, Italic: isItalic}

	placeholder := canvas.NewText("Testing connection ...", theme.ForegroundColor())
	placeholder.TextSize = fontSize
	placeholder.TextStyle = style
	resultContainer.Objects = []fyne.CanvasObject{placeholder}
	resultContainer.Refresh()

	go func() {
		err := testConnection(conn)
		if err != nil {
			lines := strings.Split(strings.ReplaceAll("✗  "+err.Error(), ": ", "\n   "), "\n")
			var objects []fyne.CanvasObject
			for _, line := range lines {
				t := newErrorText(line, fontSize)
				t.TextStyle = style
				objects = append(objects, t)
			}
			AppLog("connection test to Cassandra cluster has failed")
			fyne.Do(func() {
				resultContainer.Objects = objects
				resultContainer.Refresh()
				if setStatus != nil {
					setStatus("Test Connection : failed")
				}
			})
		} else {
			t := newSuccessText("✓  "+conn.ConnName+" connected successfully", fontSize)
			t.TextStyle = fyne.TextStyle{Bold: true, Italic: style.Italic}
			spacer1 := canvas.NewText("", theme.ForegroundColor())
			spacer2 := canvas.NewText("", theme.ForegroundColor())
			AppLog("connection test to Cassandra cluster was successful")
			fyne.Do(func() {
				resultContainer.Objects = []fyne.CanvasObject{spacer1, spacer2, t}
				resultContainer.Refresh()
				if setStatus != nil {
					setStatus("Test Connection : ✓ " + conn.ConnName + " connected successfully")
				}
			})
		}
	}()
}

// KeyValueRow holds a single key-value pair for display in a two-column table.
type KeyValueRow struct {
	Key   string
	Value string
}

// newKeyValueTable builds a borderless-looking two-column widget.Table where the
// key column is rendered bold. keyWidth, valueWidth, and rowHeight set fixed cell dimensions.
// Pass rowHeight=0 to use the Fyne default row height.
// Font style follows the app's current settings from state.
func newKeyValueTable(rows []KeyValueRow, keyWidth, valueWidth, rowHeight float32, state *AppState) *widget.Table {
	appFontName, appFontSize, appFontBold, appFontItalic := state.GetAppFontSettings()
	if appFontSize <= 0 {
		appFontSize = 18
	}
	baseStyle := fyne.TextStyle{
		Monospace: appFontName == "monospace",
		Bold:      appFontBold,
		Italic:    appFontItalic,
	}
	boldStyle := fyne.TextStyle{
		Monospace: appFontName == "monospace",
		Bold:      true,
		Italic:    appFontItalic,
	}

	t := widget.NewTable(
		func() (int, int) { return len(rows), 2 },
		func() fyne.CanvasObject {
			text := canvas.NewText("", theme.ForegroundColor())
			text.TextSize = appFontSize
			text.TextStyle = baseStyle
			return container.NewPadded(text)
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			text := obj.(*fyne.Container).Objects[0].(*canvas.Text)
			text.TextSize = appFontSize
			text.Color = theme.ForegroundColor()
			if id.Col == 0 {
				text.Text = rows[id.Row].Key
				text.TextStyle = boldStyle
			} else {
				text.Text = rows[id.Row].Value
				text.TextStyle = baseStyle
			}
			text.Refresh()
		},
	)
	t.SetColumnWidth(0, keyWidth)
	t.SetColumnWidth(1, valueWidth)
	if rowHeight > 0 {
		for i := range rows {
			t.SetRowHeight(i, rowHeight)
		}
	}
	return t
}

// kvTableSized wraps a newKeyValueTable call and constrains the table to exactly
// its content dimensions, so it does not expand to fill the parent container.
// widget.Table only draws separators between cells, never on its outer edges, so a
// stroked rectangle is overlaid via container.NewStack to supply the missing left and
// top border lines. Extra height buffer accounts for per-row separator thickness.
func kvTableSized(rows []KeyValueRow, keyWidth, valueWidth, rowHeight float32, state *AppState) fyne.CanvasObject {
	if rowHeight <= 0 {
		rowHeight = 32
	}
	t := newKeyValueTable(rows, keyWidth, valueWidth, rowHeight, state)

	tableSize := fyne.NewSize(keyWidth+valueWidth, float32(len(rows)+1)*rowHeight)
	fixed := container.NewGridWrap(tableSize, t)

	// Overlay a stroked rectangle to draw the missing outer left and top borders.
	border := canvas.NewRectangle(color.Transparent)
	border.StrokeColor = theme.DisabledColor()
	border.StrokeWidth = 1
	tableWithBorder := container.NewStack(fixed, border)

	pad := theme.Padding() * 3
	leftSpacer := canvas.NewRectangle(color.Transparent)
	leftSpacer.SetMinSize(fyne.NewSize(pad, 1))
	topSpacer := canvas.NewRectangle(color.Transparent)
	topSpacer.SetMinSize(fyne.NewSize(1, theme.Padding()))

	return container.NewPadded(container.NewVBox(
		topSpacer,
		container.NewHBox(leftSpacer, tableWithBorder),
	))
}

// DetailPanel represents the right panel with dynamic content
type DetailPanel struct {
	state                *AppState
	window               fyne.Window
	container            *fyne.Container
	content              *fyne.Container
	refreshTree          func()
	setStatus            func(string)
	selectTreeConnection func(string)
	rescanLocalFiles     func()
	selectTreeLocalFile  func(string)

	// Special output panel components
	specialOutputPanel   *fyne.Container
	specialOutputContent *fyne.Container
	specialOutputVisible bool
	mainSplit            *container.Split
}

// NewDetailPanel creates a new detail panel
func NewDetailPanel(state *AppState, window fyne.Window, refreshTree func(), setStatus func(string)) *DetailPanel {
	dp := &DetailPanel{
		state:       state,
		window:      window,
		refreshTree: refreshTree,
		setStatus:   setStatus,
	}

	// Placeholder until ShowAbout() is called below
	dp.content = container.NewVBox()

	// Create special output panel (initially hidden)
	dp.specialOutputContent = container.NewVBox()
	dp.specialOutputPanel = dp.createSpecialOutputPanel()
	dp.specialOutputVisible = false

	// Create main container without output panel initially
	dp.container = container.NewStack(
		container.NewVScroll(dp.content),
	)

	dp.ShowAbout()

	return dp
}

// SetOnSelectTreeConnection sets the callback used to highlight a connection in the tree
func (dp *DetailPanel) SetOnSelectTreeConnection(callback func(connName string)) {
	dp.selectTreeConnection = callback
}

// SetOnRescanLocalFiles sets the callback that triggers a local file rescan in the tree
func (dp *DetailPanel) SetOnRescanLocalFiles(callback func()) {
	dp.rescanLocalFiles = callback
}

// SetOnSelectTreeLocalFile sets the callback used to highlight a local file in the tree
func (dp *DetailPanel) SetOnSelectTreeLocalFile(callback func(filename string)) {
	dp.selectTreeLocalFile = callback
}

// Container returns the panel container
func (dp *DetailPanel) Container() fyne.CanvasObject {
	return dp.container
}

// Refresh updates the panel
func (dp *DetailPanel) Refresh() {
	dp.container.Refresh()
}

// setStatusReady resets the status bar to the default application tagline.
func (dp *DetailPanel) setStatusReady() {
	dp.setStatus(AppTagline)
}

// setContent replaces the panel content
func (dp *DetailPanel) setContent(content *fyne.Container) {
	dp.content = content
	dp.specialOutputVisible = false // Hide special output when changing content
	dp.rebuildContainer()
}

// rebuildContainer rebuilds the main container with or without special output panel
func (dp *DetailPanel) rebuildContainer() {
	mainContent := container.NewVScroll(container.NewPadded(dp.content))

	if dp.specialOutputVisible {
		dp.container.Objects = []fyne.CanvasObject{
			container.NewBorder(nil, dp.specialOutputPanel, nil, nil, mainContent),
		}
	} else {
		dp.container.Objects = []fyne.CanvasObject{mainContent}
	}
	dp.container.Refresh()
}

// getFontSize returns the user-configured app font size, defaulting to 18.
func (dp *DetailPanel) getFontSize() float32 {
	_, size, _, _ := dp.state.GetAppFontSettings()
	if size <= 0 {
		return 18
	}
	return size
}

// errorText creates a canvas.Text in error style at the app font size.
func (dp *DetailPanel) errorText(msg string) *canvas.Text {
	return newErrorText(msg, dp.getFontSize())
}

// successText creates a canvas.Text in success style at the app font size.
func (dp *DetailPanel) successText(msg string) *canvas.Text {
	return newSuccessText(msg, dp.getFontSize())
}

// createSpecialOutputPanel creates the styled special output panel
func (dp *DetailPanel) createSpecialOutputPanel() *fyne.Container {
	_, appFontSize, _, _ := dp.state.GetAppFontSettings()

	// Header with title and close button
	titleLabel := canvas.NewText("Output", nil)
	titleLabel.TextSize = appFontSize
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		dp.HideSpecialOutput()
	})
	closeBtn.Importance = widget.LowImportance

	header := container.NewBorder(
		nil, nil,
		container.NewHBox(titleLabel),
		closeBtn,
	)

	// Content area with scroll
	scrollContent := container.NewVScroll(dp.specialOutputContent)
	scrollContent.SetMinSize(fyne.NewSize(0, 100))

	// Wrap in a padded container
	panel := container.NewBorder(
		container.NewVBox(
			widget.NewSeparator(),
			header,
			widget.NewSeparator(),
		),
		nil, nil, nil,
		scrollContent,
	)

	return panel
}

// ShowSpecialOutput displays the special output panel with the given content
func (dp *DetailPanel) ShowSpecialOutput(title string, content fyne.CanvasObject) {
	// Update special output content
	dp.specialOutputContent.Objects = []fyne.CanvasObject{content}
	dp.specialOutputContent.Refresh()

	// Update title if custom title provided
	if title != "" {
		_, appFontSize, _, _ := dp.state.GetAppFontSettings()
		// Recreate header with new title
		titleLabel := canvas.NewText(title, nil)
		titleLabel.TextSize = appFontSize
		titleLabel.TextStyle = fyne.TextStyle{Bold: true}

		closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
			dp.HideSpecialOutput()
		})
		closeBtn.Importance = widget.LowImportance

		header := container.NewBorder(
			nil, nil,
			container.NewHBox(titleLabel),
			closeBtn,
		)

		scrollContent := container.NewVScroll(dp.specialOutputContent)
		scrollContent.SetMinSize(fyne.NewSize(0, 100))

		dp.specialOutputPanel.Objects = []fyne.CanvasObject{
			container.NewBorder(
				container.NewVBox(
					widget.NewSeparator(),
					header,
					widget.NewSeparator(),
				),
				nil, nil, nil,
				scrollContent,
			),
		}
	}

	dp.specialOutputVisible = true
	dp.rebuildContainer()
}

// ShowSpecialOutputSuccess displays a success message in the special output panel
func (dp *DetailPanel) ShowSpecialOutputSuccess(title string, message string) {
	lines := strings.Split(message, "\n")
	content := container.NewVBox()
	for _, line := range lines {
		msgText := dp.successText(line)
		msgText.TextStyle = fyne.TextStyle{Bold: true}
		content.Add(msgText)
	}
	dp.ShowSpecialOutput(title, content)
}

// ShowSpecialOutputError displays an error message in the special output panel
func (dp *DetailPanel) ShowSpecialOutputError(title string, message string) {
	lines := strings.Split(message, "\n")
	content := container.NewVBox()
	for _, line := range lines {
		msgText := dp.errorText(line)
		content.Add(msgText)
	}
	dp.ShowSpecialOutput(title, content)
}

// ShowSpecialOutputInfo displays an info message in the special output panel
func (dp *DetailPanel) ShowSpecialOutputInfo(title string, message string) {
	_, appFontSize, _, _ := dp.state.GetAppFontSettings()
	infoColor := color.RGBA{R: 23, G: 162, B: 184, A: 255} // Cyan/Info blue
	msgText := canvas.NewText(message, infoColor)
	msgText.TextSize = appFontSize

	content := container.NewVBox(
		container.NewCenter(msgText),
	)
	dp.ShowSpecialOutput(title, content)
}

// ShowSpecialOutputWarning displays a warning message in the special output panel
func (dp *DetailPanel) ShowSpecialOutputWarning(title string, message string) {
	_, appFontSize, _, _ := dp.state.GetAppFontSettings()
	warningColor := color.RGBA{R: 255, G: 193, B: 7, A: 255} // Amber/Yellow warning

	// Split message into lines for multi-line display
	lines := strings.Split(message, "\n")
	content := container.NewVBox()
	for _, line := range lines {
		msgText := canvas.NewText(line, warningColor)
		msgText.TextSize = appFontSize
		if strings.HasPrefix(line, "•") {
		} else {
			msgText.TextStyle = fyne.TextStyle{Bold: true}
		}
		content.Add(msgText)
	}

	dp.ShowSpecialOutput(title, container.NewPadded(content))
}

// HideSpecialOutput hides the special output panel
func (dp *DetailPanel) HideSpecialOutput() {
	dp.specialOutputVisible = false
	dp.rebuildContainer()
}

// ShowConnectionDetails displays connection details with action buttons
func (dp *DetailPanel) ShowConnectionDetails(conn *DecryptedConnection) {
	dp.showConnectionDetailsInternal(conn, nil)
}

// ShowConnectionDetailsWithError displays connection details with an inline error banner.
func (dp *DetailPanel) ShowConnectionDetailsWithError(conn *DecryptedConnection, connErr error) {
	dp.showConnectionDetailsInternal(conn, connErr)
}

func (dp *DetailPanel) showConnectionDetailsInternal(conn *DecryptedConnection, connErr error) {
	titleLabel := widget.NewLabel("Connection Details")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	username := conn.Username
	if username == "" {
		if conn.PromptUsername {
			username = "(will prompt)"
		} else {
			username = "(not set)"
		}
	}

	password := conn.Password
	if password == "" {
		if conn.PromptPassword {
			password = "(will prompt)"
		} else {
			password = "(not set)"
		}
	} else {
		password = strings.Repeat("*", len(password))
	}

	kvRows := []KeyValueRow{
		{"Name", conn.ConnName},
		{"Environment", conn.Environment},
		{"IP Addresses", conn.IPAddresses},
		{"Port", conn.Port},
		{"Username", username},
		{"Password", password},
	}

	// Inline test result — updated by the Test Connection button
	testResultContainer := container.NewVBox()

	// If a connection error was provided, pre-populate the test result area with it
	if connErr != nil {
		lines := strings.Split(strings.ReplaceAll("✗  Connection failed : "+connErr.Error(), ": ", "\n   "), "\n")
		var objects []fyne.CanvasObject
		for _, line := range lines {
			t := dp.errorText(line)
			objects = append(objects, t)
		}
		testResultContainer.Objects = objects
	}

	// Action buttons
	actionsLabel := widget.NewLabel("Actions")
	actionsLabel.TextStyle = fyne.TextStyle{Bold: true}

	testBtn := widget.NewButton("Test Connection", func() {
		runTestConnection(*conn, testResultContainer, dp.state, dp.setStatus)
	})
	testBtn.Importance = widget.HighImportance

	editBtn := widget.NewButton("Edit", func() {
		dp.ShowEditConnectionForm(conn)
	})

	renameBtn := widget.NewButton("Rename", func() {
		dp.ShowRenameConnectionForm(conn)
	})

	copyBtn := widget.NewButton("Copy", func() {
		existing := loadConnections()
		taken := make(map[string]bool, len(existing))
		for _, c := range existing {
			taken[c.ConnName] = true
		}
		newName := uniqueConnectionName(conn.ConnName, taken)
		newConn := *conn
		newConn.ConnName = newName
		if err := saveConnection(newConn); err != nil {
			dp.setStatus("Copy failed : " + err.Error())
			return
		}
		AppLog("Cassandra connection was cloned")
		dp.setStatus("Connection : cloned")
		dp.state.ReloadConnections()
		dp.refreshTree()
		if dp.selectTreeConnection != nil {
			dp.selectTreeConnection(newConn.ConnName)
		} else {
			dp.ShowConnectionDetails(&newConn)
		}
	})

	// Inline delete confirmation (hidden until Delete is clicked)
	var confirmSection *fyne.Container

	confirmCancelBtn := widget.NewButton("Cancel", func() {
		confirmSection.Hide()
	})

	confirmDeleteBtn := widget.NewButton("Confirm Delete", func() {
		err := deleteConnection(conn.ConnName)
		if err != nil {
			// replace confirm section contents with error — rare path
			confirmSection.Objects = []fyne.CanvasObject{
				widget.NewSeparator(),
				widget.NewLabel(""),
				dp.errorText("x  " + err.Error()),
				widget.NewLabel(""),
				container.NewHBox(confirmCancelBtn),
			}
			confirmSection.Refresh()
			return
		}
		AppLog("Cassandra connection was deleted")
		dp.setStatus("Connection : deleted")
		dp.state.ReloadConnections()
		dp.state.SetSelectedConnection(nil)
		dp.refreshTree()
		dp.ShowAbout()
	})
	confirmDeleteBtn.Importance = widget.DangerImportance

	confirmMsg := dp.errorText(fmt.Sprintf("Delete connection %q ? This action cannot be undone.", conn.ConnName))
	confirmMsg.TextStyle = fyne.TextStyle{Bold: true}
	confirmSection = container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabel(""),
		confirmMsg,
		widget.NewLabel(""),
		container.NewHBox(confirmDeleteBtn, confirmCancelBtn),
	)
	confirmSection.Hide()

	deleteBtn := widget.NewButton("Delete", func() {
		confirmSection.Show()
	})

	btnSize := fyne.NewSize(160, 40)
	pad := theme.Padding() * 3
	leftSpacer := canvas.NewRectangle(color.Transparent)
	leftSpacer.SetMinSize(fyne.NewSize(pad, 1))
	topSpacer := canvas.NewRectangle(color.Transparent)
	topSpacer.SetMinSize(fyne.NewSize(1, pad))
	btnRow := container.NewPadded(container.NewVBox(
		topSpacer,
		container.NewHBox(
			leftSpacer,
			container.NewGridWrap(btnSize, testBtn),
			container.NewGridWrap(btnSize, editBtn),
			container.NewGridWrap(btnSize, renameBtn),
			container.NewGridWrap(btnSize, copyBtn),
			container.NewGridWrap(btnSize, deleteBtn),
		),
	))

	testResultSpacer := canvas.NewRectangle(color.Transparent)
	testResultSpacer.SetMinSize(fyne.NewSize(pad, 1))
	testResultRow := container.NewHBox(testResultSpacer, testResultContainer)

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		kvTableSized(kvRows, 200, 500, 40, dp.state),
		widget.NewSeparator(),
		actionsLabel,
		widget.NewSeparator(),
		btnRow,
		testResultRow,
		confirmSection,
	)

	dp.setContent(content)
}

// ShowAbout displays the about information in the right panel
func (dp *DetailPanel) ShowAbout() {
	// === Header Section ===
	appNameLabel := canvas.NewText(AppDisplayName, nil)
	appNameLabel.TextSize = 22
	appNameLabel.TextStyle = fyne.TextStyle{Bold: true}

	byLabel := canvas.NewText(" by ", nil)
	byLabel.TextSize = 20

	oramadLabel := canvas.NewText("oramad", nil)
	oramadLabel.TextSize = 20

	titleRow := container.NewHBox(
		layout.NewSpacer(),
		appNameLabel,
		byLabel,
		oramadLabel,
		layout.NewSpacer(),
	)

	versionLabel := widget.NewLabelWithStyle(
		"Version "+AppVersion,
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)

	dateLabel := widget.NewLabelWithStyle(
		AppVersionDate,
		fyne.TextAlignCenter,
		fyne.TextStyle{Italic: true},
	)

	headerSection := container.NewVBox(
		titleRow,
		versionLabel,
		dateLabel,
	)

	// === Description Section ===
	descLabel := widget.NewLabelWithStyle(
		AppTagline,
		fyne.TextAlignCenter,
		fyne.TextStyle{},
	)
	descSection := container.NewCenter(descLabel)

	// === Built With Section ===
	builtWithTitle := widget.NewLabelWithStyle(
		"Built With",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)

	newBuiltWithItem := func(name, link string) *fyne.Container {
		lbl := widget.NewLabelWithStyle(name+" [ "+link+" ]", fyne.TextAlignCenter, fyne.TextStyle{})
		return container.NewCenter(lbl)
	}

	builtWithSection := container.NewVBox(
		builtWithTitle,
		newBuiltWithItem("Go Programming Language", "https://go.dev/"),
		newBuiltWithItem("Fyne UI Framework", "https://fyne.io/"),
		newBuiltWithItem("gocql Cassandra Driver", "https://github.com/gocql/gocql"),
		newBuiltWithItem("DuckDB", "https://duckdb.org/"),
	)

	// === GitHub Section ===
	githubTitle := widget.NewLabelWithStyle(
		"GitHub",
		fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true},
	)
	githubSection := container.NewVBox(
		githubTitle,
		container.NewCenter(widget.NewLabelWithStyle("https://github.com/Sarma1807/cassBrowser", fyne.TextAlignCenter, fyne.TextStyle{})),
	)

	// === Assemble Card Content ===
	cardContent := container.NewVBox(
		layout.NewSpacer(),
		headerSection,
		layout.NewSpacer(),
		widget.NewSeparator(),
		layout.NewSpacer(),
		descSection,
		layout.NewSpacer(),
		widget.NewSeparator(),
		layout.NewSpacer(),
		builtWithSection,
		layout.NewSpacer(),
		widget.NewSeparator(),
		layout.NewSpacer(),
		githubSection,
		layout.NewSpacer(),
	)

	// Wrap in padded container for card effect
	paddedCard := container.NewPadded(
		container.NewPadded(
			container.NewPadded(cardContent),
		),
	)

	content := container.NewVBox(
		widget.NewLabel(""),
		container.NewCenter(paddedCard),
	)

	dp.setContent(content)
}

// ShowAddConnectionForm displays the add connection form
func (dp *DetailPanel) ShowAddConnectionForm() {
	form := NewConnectionForm(nil, dp.window, dp.state, func(conn DecryptedConnection) {
		err := saveConnection(conn)
		if err != nil {
			dialog.ShowError(err, dp.window)
			return
		}
		AppLog("Cassandra connection was saved")
		dp.setStatus("New connection saved.")
		dp.state.ReloadConnections()
		dp.refreshTree()
		dp.ShowAbout()
	}, func() {
		dp.setStatus(AppTagline)
		dp.ShowAbout()
	}, dp.setStatus)
	dp.setContent(form.Container())
}

// ShowEditConnectionForm displays the edit connection form
func (dp *DetailPanel) ShowEditConnectionForm(conn *DecryptedConnection) {
	form := NewConnectionForm(conn, dp.window, dp.state, func(updated DecryptedConnection) {
		err := updateConnection(conn.ConnName, updated)
		if err != nil {
			dialog.ShowError(err, dp.window)
			return
		}
		AppLog("Cassandra connection was updated")
		dp.setStatus("Connection : updated")
		dp.state.ReloadConnections()
		dp.refreshTree()
		dp.ShowConnectionDetails(&updated)
	}, func() {
		dp.setStatus(AppTagline)
		dp.ShowConnectionDetails(conn)
	}, dp.setStatus)
	dp.setContent(form.Container())
}

// ShowRenameConnectionForm displays an inline rename form in the right panel
func (dp *DetailPanel) ShowRenameConnectionForm(conn *DecryptedConnection) {
	titleLabel := widget.NewLabel("Rename Connection")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	currentLabel := widget.NewLabel("Current Name : " + conn.ConnName)

	nameEntry := newFocusEntry()
	nameEntry.SetText(conn.ConnName)
	nameEntry.SetPlaceHolder("New connection name")

	errorText := dp.errorText("")
	errorText.TextStyle = fyne.TextStyle{Italic: true}

	showError := func(msg string) {
		errorText.Text = msg
		errorText.Refresh()
	}
	clearError := func() {
		errorText.Text = ""
		errorText.Refresh()
	}

	validate := func(name string) string {
		if len(name) < 5 || len(name) > 20 {
			return "x  Name must be 5–20 characters"
		}
		for _, ch := range name {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
				return "x  Only letters, digits, _ and - are allowed"
			}
		}
		return ""
	}

	checkDuplicate := func(name string) string {
		for _, c := range dp.state.GetConnections() {
			if c.ConnName == name && c.ConnName != conn.ConnName {
				return fmt.Sprintf("x  A connection named %q already exists", name)
			}
		}
		return ""
	}

	nameEntry.OnChanged = func(s string) {
		if msg := validate(strings.TrimSpace(s)); msg != "" {
			showError(msg)
		} else {
			clearError()
		}
	}

	nameEntry.onFocusLost = func() {
		trimmed := strings.TrimSpace(nameEntry.Text)
		if validate(trimmed) != "" {
			return
		}
		if msg := checkDuplicate(trimmed); msg != "" {
			showError(msg)
		}
	}

	saveBtn := widget.NewButton("Save", func() {
		newName := strings.TrimSpace(nameEntry.Text)
		if msg := validate(newName); msg != "" {
			showError(msg)
			return
		}
		if msg := checkDuplicate(newName); msg != "" {
			showError(msg)
			return
		}
		err := renameConnection(conn.ConnName, newName)
		if err != nil {
			showError("X     " + err.Error())
			return
		}
		AppLog("Cassandra connection was renamed")
		dp.setStatus("Connection : renamed")
		dp.state.ReloadConnections()
		dp.refreshTree()
		updated := *conn
		updated.ConnName = newName
		dp.ShowConnectionDetails(&updated)
	})
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", func() {
		dp.setStatus(AppTagline)
		dp.ShowConnectionDetails(conn)
	})

	spacer := func() fyne.CanvasObject { return widget.NewLabel("") }

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		currentLabel,
		spacer(),
		widget.NewLabel("New Name :"),
		constrainFormEntry(nameEntry),
		spacer(),
		errorText,
		spacer(),
		widget.NewSeparator(),
		spacer(),
		container.NewHBox(saveBtn, cancelBtn),
	)

	dp.setContent(content)
}

// ShowClusterInfoFromSession displays cluster information using an existing session
func (dp *DetailPanel) ShowClusterInfoFromSession(conn *DecryptedConnection, session *gocql.Session) {
	titleLabel := widget.NewLabel("Cluster Information")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	if session == nil {
		content := container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("Not connected to server"),
		)
		dp.setContent(content)
		return
	}

	// Fetch cluster info
	info, err := fetchClusterInfoWithSession(session)
	if err != nil {
		content := container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("Error fetching cluster info: "+err.Error()),
		)
		dp.setContent(content)
		return
	}

	clusterRows := []KeyValueRow{
		{"Cluster Name", info["cluster_name"]},
		{"Cassandra Version", info["release_version"]},
		{"Data Center", info["data_center"]},
		{"Rack", info["rack"]},
	}

	keyspaceRows := []KeyValueRow{
		{"Total Keyspaces", info["total_keyspaces"]},
		{"User Keyspaces", info["user_keyspaces"]},
	}

	nodeRows := []KeyValueRow{
		{"Nodes", info["node_count"]},
		{"Local Node IP", info["local_node_ip"]},
		{"Peer Nodes", info["peer_nodes"]},
	}

	sectionSpacer := func() fyne.CanvasObject {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(1, theme.Padding()/2))
		return r
	}

	connLabel := widget.NewLabel("Connection : " + conn.ConnName)
	connLabel.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		connLabel,
		sectionSpacer(),
		kvTableSized(clusterRows, 200, 500, 40, dp.state),
		sectionSpacer(),
		kvTableSized(keyspaceRows, 200, 500, 40, dp.state),
		sectionSpacer(),
		kvTableSized(nodeRows, 200, 500, 40, dp.state),
	)

	dp.setContent(content)
}

// ShowKeyspaceInfo displays keyspace information
func (dp *DetailPanel) ShowKeyspaceInfo(conn *DecryptedConnection, keyspace string, session *gocql.Session) {
	titleLabel := widget.NewLabel("Keyspace : " + keyspace)
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	if session == nil {
		content := container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("Not connected to server"),
		)
		dp.setContent(content)
		return
	}

	// Fetch keyspace info
	info, err := fetchKeyspaceInfo(session, keyspace)
	if err != nil {
		content := container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("Error fetching keyspace info: "+err.Error()),
		)
		dp.setContent(content)
		return
	}

	// Format replication class
	replication := info["replication"].(map[string]string)
	replicationClass := replication["class"]
	if strings.Contains(replicationClass, "SimpleStrategy") {
		replicationClass = "SimpleStrategy"
	} else if strings.Contains(replicationClass, "NetworkTopologyStrategy") {
		replicationClass = "NetworkTopologyStrategy"
	}

	durableWrites := info["durable_writes"].(bool)
	durableWritesStr := "false"
	if durableWrites {
		durableWritesStr = "true"
	}

	// Section 1: keyspace identity + replication
	configRows := []KeyValueRow{
		{"Keyspace", keyspace},
		{"Replication", replicationClass},
	}
	for k, v := range replication {
		if k != "class" {
			configRows = append(configRows, KeyValueRow{"  " + k, v})
		}
	}
	configRows = append(configRows, KeyValueRow{"Durable Writes", durableWritesStr})

	// Section 2: object counts
	objectRows := []KeyValueRow{
		{"Tables", fmt.Sprintf("%d", info["table_count"])},
		{"User Types", fmt.Sprintf("%d", info["udt_count"])},
		{"Indexes", fmt.Sprintf("%d", info["index_count"])},
		{"Materialized Views", fmt.Sprintf("%d", info["mv_count"])},
	}

	sectionSpacer := func() fyne.CanvasObject {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(1, theme.Padding()/2))
		return r
	}

	connLabel := widget.NewLabel("Connection : " + conn.ConnName)
	connLabel.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		connLabel,
		sectionSpacer(),
		kvTableSized(configRows, 250, 500, 40, dp.state),
		sectionSpacer(),
		kvTableSized(objectRows, 250, 500, 40, dp.state),
	)

	dp.setContent(content)
}

// ShowTableDetails displays table structure and data with tabs
func (dp *DetailPanel) ShowTableDetails(conn *DecryptedConnection, keyspace, table string, session *gocql.Session) {
	titleLabel := widget.NewLabel("Table: " + keyspace + "." + table)
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	if session == nil {
		content := container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("Not connected to server"),
		)
		dp.setContent(content)
		return
	}

	// Create Structure tab
	structureTab := dp.createStructureTab(session, keyspace, table)

	// Create Data tab
	dataTab := dp.createDataTab(session, keyspace, table)

	// Create Query tab
	queryTab := dp.createQueryTab(conn, session, keyspace, table)

	// Create DDL tab
	ddlTab := dp.createDDLTab(session, keyspace, table)

	// registerNavFn is set after tabs+content are built so createExtractTab can
	// call it at button-click time (when both variables are already initialised).
	var registerNavFn func()

	// Create Extract tab
	extractTab := dp.createExtractTab(conn, keyspace, table, session, &registerNavFn)

	// Create tabs container
	tabs := container.NewAppTabs(
		container.NewTabItem("Structure", structureTab),
		container.NewTabItem("DDL", ddlTab),
		container.NewTabItem("Sample Data", dataTab),
		container.NewTabItem("CQL Query", queryTab),
		container.NewTabItem("Extract Table Data", extractTab),
	)
	tabs.OnSelected = func(_ *container.TabItem) {
		dp.HideSpecialOutput()
	}

	content := container.NewBorder(
		container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
		),
		nil,
		nil, nil,
		tabs,
	)

	// Now that tabs and content exist, wire the nav registration function.
	// When called (at extraction start), it captures both so the menu item
	// can restore this exact panel and select the Extract tab.
	capturedTabs := tabs
	capturedContent := content
	registerNavFn = func() {
		dp.state.SetExtractionNavFn(func() {
			dp.setContent(capturedContent)
			capturedTabs.SelectIndex(4)
		})
	}

	dp.setContent(content)
}

// createStructureTab creates the table structure tab content
func (dp *DetailPanel) createStructureTab(session *gocql.Session, keyspace, table string) fyne.CanvasObject {
	columns, err := fetchColumns(session, keyspace, table)
	if err != nil {
		return widget.NewLabel("Error fetching columns: " + err.Error())
	}
	if len(columns) == 0 {
		return widget.NewLabel("No columns found")
	}

	pkCount, ckCount := 0, 0
	for _, col := range columns {
		if col.Kind == "partition_key" {
			pkCount++
		} else if col.Kind == "clustering" {
			ckCount++
		}
	}

	getKeyType := func(col BrowseColumn) string {
		switch col.Kind {
		case "partition_key":
			if pkCount > 1 {
				return fmt.Sprintf("Partition Key (%d)", col.Position+1)
			}
			return "Partition Key"
		case "clustering":
			if ckCount > 1 {
				return fmt.Sprintf("Clustering Column (%d)", col.Position+1)
			}
			return "Clustering Column"
		case "static":
			return "Static"
		default:
			return ""
		}
	}

	colNames := []string{"Column Name", "Data Type", "Key Type"}
	rows := make([]map[string]interface{}, len(columns))
	for i, col := range columns {
		rows[i] = map[string]interface{}{
			"Column Name": col.Name,
			"Data Type":   col.Type,
			"Key Type":    getKeyType(col),
		}
	}

	infoLabel := widget.NewLabel(fmt.Sprintf("%d columns", len(columns)))
	infoLabel.TextStyle = fyne.TextStyle{Italic: true}

	grid := NewDataGrid(colNames, rows, rowsPerPage, dp.state)

	return container.NewBorder(
		infoLabel,
		nil, nil, nil,
		grid.Container(),
	)
}

// createDataTab creates the table data tab content
func (dp *DetailPanel) createDataTab(session *gocql.Session, keyspace, table string) fyne.CanvasObject {
	columnNames, rows, err := fetchTableData(session, keyspace, table)
	if err != nil {
		return widget.NewLabel("Error fetching data: " + err.Error())
	}
	if len(rows) == 0 {
		return widget.NewLabel("No data in table")
	}

	infoLabel := widget.NewLabel("Showing only 20 rows.")
	infoLabel.TextStyle = fyne.TextStyle{Italic: true}

	grid := NewDataGrid(columnNames, rows, rowsPerPage, dp.state)

	return container.NewBorder(
		infoLabel,
		nil, nil, nil,
		grid.Container(),
	)
}

// buildDefaultQuery constructs a formatted default query for the table
func (dp *DetailPanel) buildDefaultQuery(session *gocql.Session, keyspace, table string) string {
	// Fetch columns to build the query
	columns, err := fetchColumns(session, keyspace, table)
	if err != nil || len(columns) == 0 {
		// Fallback to simple query
		return fmt.Sprintf("SELECT *\nFROM %s.%s\nLIMIT 20\n;", keyspace, table)
	}

	// Collect column names and partition keys
	var columnNames []string
	var partitionKeys []BrowseColumn

	for _, col := range columns {
		columnNames = append(columnNames, col.Name)
		if col.Kind == "partition_key" {
			partitionKeys = append(partitionKeys, col)
		}
	}

	// Sort partition keys by position
	for i := 0; i < len(partitionKeys)-1; i++ {
		for j := i + 1; j < len(partitionKeys); j++ {
			if partitionKeys[i].Position > partitionKeys[j].Position {
				partitionKeys[i], partitionKeys[j] = partitionKeys[j], partitionKeys[i]
			}
		}
	}

	// Build SELECT clause with all column names
	selectClause := "SELECT " + strings.Join(columnNames, ", ")

	// Build FROM clause
	fromClause := fmt.Sprintf("FROM %s.%s", keyspace, table)

	// Build WHERE clause with partition key placeholders (commented out)
	var whereClause string
	if len(partitionKeys) > 0 {
		var conditions []string
		for _, pk := range partitionKeys {
			conditions = append(conditions, fmt.Sprintf("%s = ?", pk.Name))
		}
		whereClause = "-- modify WHERE clause according to your requirement\n-- WHERE " + strings.Join(conditions, " AND ")
	}

	// Build the complete query
	var query string
	if whereClause != "" {
		query = fmt.Sprintf("%s\n%s\n%s\nLIMIT 20\n;", selectClause, fromClause, whereClause)
	} else {
		query = fmt.Sprintf("%s\n%s\nLIMIT 20\n;", selectClause, fromClause)
	}

	return query
}

// createQueryTab creates the query interface tab content
func (dp *DetailPanel) createQueryTab(conn *DecryptedConnection, session *gocql.Session, keyspace, table string) fyne.CanvasObject {
	// Fetch partition keys for warning checks
	var partitionKeyNames []string
	columns, _ := fetchColumns(session, keyspace, table)
	for _, col := range columns {
		if col.Kind == "partition_key" {
			partitionKeyNames = append(partitionKeyNames, strings.ToLower(col.Name))
		}
	}

	// Variables to store current results for save functionality
	var currentColumnNames []string
	var currentRows []map[string]interface{}
	var wasCapped bool

	// Build formatted default query
	defaultQuery := dp.buildDefaultQuery(session, keyspace, table)

	// Query input
	queryEntry := newSafeMultiLineEntry()
	queryEntry.Wrapping = fyne.TextWrapWord
	queryEntry.SetText(defaultQuery)
	queryEntry.SetPlaceHolder("Enter CQL query here ...")
	queryEntry.SetMinRowsVisible(6)

	// Results container - will hold either message or table
	resultsContainer := container.NewStack()

	// Initial message
	initialMsg := widget.NewLabel("Execute a query to see results")
	initialMsg.TextStyle = fyne.TextStyle{Italic: true}
	initialMsg.Alignment = fyne.TextAlignCenter
	resultsContainer.Objects = []fyne.CanvasObject{
		container.NewCenter(initialMsg),
	}

	// Controls row container - initially empty
	controlsRow := container.NewHBox()

	// Helper to generate save filename
	// Format: <environment>-<connection_name>-<keyspace_name>-<table_name>-<date:YYYYMMDD>_<time:HHMI>.<file_extension>
	generateFilename := func(ext string) string {
		now := time.Now()
		dateStr := now.Format("20060102")
		timeStr := now.Format("1504")
		// Sanitize names for filename - keep alphanumeric, underscore, hyphen
		safeName := func(s string) string {
			s = strings.ReplaceAll(s, " ", "_")
			s = strings.ReplaceAll(s, "/", "_")
			s = strings.ReplaceAll(s, "\\", "_")
			return s
		}
		return fmt.Sprintf("cass-%s-%s_%s.%s", safeName(table), dateStr, timeStr, ext)
	}

	// Helper to generate summary file content
	generateSummaryContent := func(format string, rowCount int, query string) string {
		now := time.Now()
		return fmt.Sprintf(`Date Time    : %s
File Format  : %s
Environment  : %s
Connection   : %s
Keyspace     : %s
Table        : %s
Row Count    : %d
Query        :
%s
`, now.Format("2006-01-02 15:04:05 MST"), format, conn.Environment, conn.ConnName, keyspace, table, rowCount, query)
	}

	// Save to JSON helper
	saveAsJSON := func() {
		if len(currentRows) == 0 {
			AppLog("cql query results failed to save to json file")
			dp.ShowSpecialOutputError("Save JSON", "No data to save.")
			return
		}

		workDir := dp.state.GetWorkingDirectory()
		if workDir == "" {
			AppLog("cql query results failed to save to json file")
			dp.ShowSpecialOutputError("Save JSON", "Working directory not configured.")
			return
		}

		dataDir := filepath.Join(workDir, "data")
		filename := generateFilename("json")
		filePath := filepath.Join(dataDir, filename)

		// Create JSON with column order preserved
		var orderedRows []map[string]interface{}
		for _, row := range currentRows {
			orderedRow := make(map[string]interface{})
			for _, col := range currentColumnNames {
				orderedRow[col] = row[col]
			}
			orderedRows = append(orderedRows, orderedRow)
		}

		data, err := json.MarshalIndent(orderedRows, "", "  ")
		if err != nil {
			AppLog("cql query results failed to save to json file")
			dp.ShowSpecialOutputError("Save JSON", "Failed to marshal data :\n"+err.Error())
			return
		}

		err = os.WriteFile(filePath, data, 0600)
		if err != nil {
			AppLog("cql query results failed to save to json file")
			dp.ShowSpecialOutputError("Save JSON", "Failed to write file :\n"+err.Error())
			return
		}

		// Save summary file
		summaryPath := filePath + ".txt"
		summaryContent := generateSummaryContent("json", len(currentRows), queryEntry.Text)
		if err := os.WriteFile(summaryPath, []byte(summaryContent), 0600); err != nil {
			AppLog("cql query results failed to save to json file")
			dp.ShowSpecialOutputError("Save JSON", "Failed to write summary :\n"+err.Error())
			return
		}

		AppLog("cql query results were saved to json file")
		dp.ShowSpecialOutputSuccess("Save Complete", fmt.Sprintf("Saved %d rows to :\n%s\n%s", len(currentRows), filePath, summaryPath))
	}

	// Save to CSV helper
	saveAsCSV := func() {
		if len(currentRows) == 0 {
			AppLog("cql query results failed to save to csv file")
			dp.ShowSpecialOutputError("Save CSV", "No data to save.")
			return
		}

		workDir := dp.state.GetWorkingDirectory()
		if workDir == "" {
			AppLog("cql query results failed to save to csv file")
			dp.ShowSpecialOutputError("Save CSV", "Working directory not configured.")
			return
		}

		dataDir := filepath.Join(workDir, "data")
		filename := generateFilename("csv")
		filePath := filepath.Join(dataDir, filename)

		file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			AppLog("cql query results failed to save to csv file")
			dp.ShowSpecialOutputError("Save CSV", "Failed to create file :\n"+err.Error())
			return
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		// Write header
		if err := writer.Write(currentColumnNames); err != nil {
			AppLog("cql query results failed to save to csv file")
			dp.ShowSpecialOutputError("Save CSV", "Failed to write header :\n"+err.Error())
			return
		}

		// Write rows
		for _, row := range currentRows {
			var record []string
			for _, col := range currentColumnNames {
				record = append(record, fmt.Sprintf("%v", row[col]))
			}
			if err := writer.Write(record); err != nil {
				AppLog("cql query results failed to save to csv file")
				dp.ShowSpecialOutputError("Save CSV", "Failed to write row :\n"+err.Error())
				return
			}
		}

		// Flush CSV writer before saving summary
		writer.Flush()

		// Save summary file
		summaryPath := filePath + ".txt"
		summaryContent := generateSummaryContent("csv", len(currentRows), queryEntry.Text)
		if err := os.WriteFile(summaryPath, []byte(summaryContent), 0600); err != nil {
			AppLog("cql query results failed to save to csv file")
			dp.ShowSpecialOutputError("Save CSV", "Failed to write summary :\n"+err.Error())
			return
		}

		AppLog("cql query results were saved to csv file")
		dp.ShowSpecialOutputSuccess("Save Complete", fmt.Sprintf("Saved %d rows to :\n%s\n%s", len(currentRows), filePath, summaryPath))
	}

	saveCSVBtn := widget.NewButton("Save to CSV", func() { saveAsCSV() })
	saveJSONBtn := widget.NewButton("Save to JSON", func() { saveAsJSON() })
	saveCSVBtn.Disable()
	saveJSONBtn.Disable()

	// Error display helper
	showError := func(errMsg string) {
		dp.setStatus("Query failed.")
		lines := strings.Split(strings.ReplaceAll("✗  ERROR : "+errMsg, " : ", "\n   "), "\n")
		errBox := container.NewVBox()
		for _, line := range lines {
			t := dp.errorText(line)
			errBox.Add(t)
		}
		resultsContainer.Objects = []fyne.CanvasObject{
			container.NewVScroll(errBox),
		}
		resultsContainer.Refresh()
		controlsRow.Objects = nil
		controlsRow.Refresh()
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
	}

	// Results display helper
	showResults := func(columnNames []string, rows []map[string]interface{}) {
		// Store results for save functionality
		currentColumnNames = columnNames
		currentRows = rows

		if len(rows) == 0 {
			dp.setStatus("Query executed. No rows returned.")
			noDataLabel := widget.NewLabel("Query returned no results")
			noDataLabel.TextStyle = fyne.TextStyle{Italic: true}
			noDataLabel.Alignment = fyne.TextAlignCenter
			resultsContainer.Objects = []fyne.CanvasObject{
				container.NewCenter(noDataLabel),
			}
			resultsContainer.Refresh()
			controlsRow.Objects = nil
			controlsRow.Refresh()
			saveCSVBtn.Disable()
			saveJSONBtn.Disable()
			return
		}

		dp.setStatus(fmt.Sprintf("Query executed. Returned %s row(s).", commaInt(len(rows))))

		// Create paginated DataGrid
		grid := NewDataGrid(columnNames, rows, rowsPerPage, dp.state)
		if wasCapped {
			grid.SetCappedNote("Displaying only 200 rows.")
		}

		saveCSVBtn.Enable()
		saveJSONBtn.Enable()

		// Update results container
		resultsContainer.Objects = []fyne.CanvasObject{grid.Container()}
		resultsContainer.Refresh()
	}

	// Inline confirmation container for ALLOW FILTERING warnings (initially empty)
	confirmContainer := container.NewVBox()

	// doExecute runs the actual Cassandra query execution
	doExecute := func(queryText string) {
		dp.HideSpecialOutput()

		loadingLabel := dp.successText("Executing query ...")
		loadingLabel.TextStyle = fyne.TextStyle{Italic: true}
		loadingLabel.Alignment = fyne.TextAlignCenter
		resultsContainer.Objects = []fyne.CanvasObject{
			container.NewVBox(widget.NewLabel(""), widget.NewLabel(""), container.NewCenter(loadingLabel)),
		}
		resultsContainer.Refresh()
		controlsRow.Objects = nil
		controlsRow.Refresh()

		originalQuery := queryText
		queryText = addSafetyLimit(session, queryText)
		wasCapped = queryText != originalQuery

		go func() {
			iter := session.Query(queryText).Iter()

			columns := iter.Columns()
			var columnNames []string
			for _, col := range columns {
				columnNames = append(columnNames, col.Name)
			}

			var rows []map[string]interface{}
			for {
				row := make(map[string]interface{})
				if !iter.MapScan(row) {
					break
				}
				rows = append(rows, row)
			}

			if err := iter.Close(); err != nil {
				errMsg := err.Error()
				if strings.Contains(strings.ToUpper(errMsg), "ALLOW FILTERING") {
					errText := dp.errorText("Error occurred while running this query.")
					errText.TextStyle = fyne.TextStyle{Bold: true}
					fyne.Do(func() {
						dp.setStatus("Query failed.")
						resultsContainer.Objects = []fyne.CanvasObject{
							container.NewCenter(errText),
						}
						resultsContainer.Refresh()
						controlsRow.Objects = nil
						controlsRow.Refresh()
						saveCSVBtn.Disable()
						saveJSONBtn.Disable()
					})
				} else {
					fyne.Do(func() { showError(errMsg) })
				}
				return
			}

			if strings.Contains(strings.ToUpper(queryText), "ALLOW FILTERING") {
				AppLog("executed cql query on Cassandra along with ALLOW FILTERING clause")
			} else {
				AppLog("executed cql query on Cassandra")
			}
			fyne.Do(func() { showResults(columnNames, rows) })
		}()
	}

	// Execute button
	executeBtn := widget.NewButton("Execute", func() {
		dp.HideSpecialOutput()
		queryText := strings.TrimSpace(queryEntry.Text)
		if queryText == "" {
			showError("Query cannot be empty")
			return
		}

		// Reject any statement that is not a SELECT
		if !isSelectOnlyQuery(queryText) {
			if len(confirmContainer.Objects) > 0 {
				confirmContainer.Objects = nil
				confirmContainer.Refresh()
			}
			dp.setStatus("Query failed.")
			errText := dp.errorText("Currently this query/statement is not supported.")
			errText.TextStyle = fyne.TextStyle{Bold: true}
			resultsContainer.Objects = []fyne.CanvasObject{container.NewCenter(errText)}
			resultsContainer.Refresh()
			controlsRow.Objects = nil
			controlsRow.Refresh()
			saveCSVBtn.Disable()
			saveJSONBtn.Disable()
			return
		}

		queryUpper := strings.ToUpper(queryText)
		queryLower := strings.ToLower(queryText)

		// ALLOW FILTERING: show inline confirmation before executing
		if strings.Contains(queryUpper, "ALLOW FILTERING") {
			dp.setStatus("WARNING : Query with ALLOW FILTERING clause ...")
			resultsContainer.Objects = nil
			resultsContainer.Refresh()
			controlsRow.Objects = nil
			controlsRow.Refresh()
			saveCSVBtn.Disable()
			saveJSONBtn.Disable()
			warningLines := []string{
				`WARNING :`,
				`This query uses "ALLOW FILTERING" clause.`,
				`In Cassandra, "ALLOW FILTERING" bypasses efficient partition-based access patterns.`,
				``,
				`This can force Cassandra to scan large amounts of data across partitions, causing slow queries,`,
				`high resource usage, and potential cluster instability.`,
			}
			var objs []fyne.CanvasObject
			for i, line := range warningLines {
				t := dp.errorText(line)
				if i == 0 {
					t.TextStyle = fyne.TextStyle{Bold: true}
				}
				objs = append(objs, t)
			}
			capturedQuery := queryText
			proceedBtn := widget.NewButton("Proceed", func() {
				confirmContainer.Objects = nil
				confirmContainer.Refresh()
				doExecute(capturedQuery)
			})
			proceedBtn.Importance = widget.DangerImportance
			cancelBtn := widget.NewButton("Cancel", func() {
				confirmContainer.Objects = nil
				confirmContainer.Refresh()
			})
			objs = append(objs, container.NewHBox(proceedBtn, cancelBtn))
			confirmContainer.Objects = objs
			confirmContainer.Refresh()
			return
		}

		// Clear any lingering confirmation
		if len(confirmContainer.Objects) > 0 {
			confirmContainer.Objects = nil
			confirmContainer.Refresh()
		}

		// Check for other SELECT query warnings
		if strings.HasPrefix(queryUpper, "SELECT") {
			var warnings []string

			hasWhere := strings.Contains(queryUpper, "WHERE")
			if !hasWhere {
				warnings = append(warnings, "• Query has no WHERE clause - will scan entire table")
			} else {
				var missingPKs []string
				for _, pk := range partitionKeyNames {
					whereIdx := strings.Index(queryLower, "where")
					if whereIdx != -1 {
						whereClause := queryLower[whereIdx:]
						if !strings.Contains(whereClause, pk) {
							missingPKs = append(missingPKs, pk)
						}
					}
				}
				if len(missingPKs) > 0 {
					warnings = append(warnings, "• Missing partition key(s) in WHERE clause: "+strings.Join(missingPKs, ", "))
				}
			}

			if len(warnings) > 0 {
				warningMsg := "Query Performance Warning:\n" + strings.Join(warnings, "\n")
				dp.ShowSpecialOutputWarning("Query Warning", warningMsg)
			}
		}

		doExecute(queryText)
	})

	clearBtn := widget.NewButton("Reset", func() {
		dp.HideSpecialOutput()
		dp.setStatus(AppTagline)
		queryEntry.SetText(defaultQuery)
		resultsContainer.Objects = []fyne.CanvasObject{container.NewCenter(initialMsg)}
		resultsContainer.Refresh()
		controlsRow.Objects = nil
		controlsRow.Refresh()
		currentColumnNames = nil
		currentRows = nil
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		confirmContainer.Objects = nil
		confirmContainer.Refresh()
	})

	executeBtn.Importance = widget.HighImportance
	buttonRow := container.NewHBox(executeBtn, clearBtn, saveCSVBtn, saveJSONBtn)

	return container.NewBorder(
		container.NewVBox(queryEntry, buttonRow, confirmContainer),
		nil, nil, nil,
		container.NewBorder(controlsRow, nil, nil, nil, resultsContainer),
	)
}

// createDDLTab creates the DDL tab showing a CREATE TABLE statement
func (dp *DetailPanel) createDDLTab(session *gocql.Session, keyspace, table string) fyne.CanvasObject {
	entry := newSafeMultiLineEntry()
	entry.Wrapping = fyne.TextWrapWord
	entry.SetText("Loading ...")
	entry.Disable()

	copyBtn := widget.NewButton("Copy to Clipboard", func() {
		dp.window.Clipboard().SetContent(entry.Text)
	})
	copyBtn.Disable()

	go func() {
		ddl, err := fetchTableDDL(session, keyspace, table)
		if err != nil {
			fyne.Do(func() { entry.SetText("ERROR : " + err.Error()) })
			return
		}
		fyne.Do(func() {
			entry.SetText(ddl)
			copyBtn.Enable()
		})
	}()

	scroll := container.NewVScroll(entry)
	scroll.SetMinSize(fyne.NewSize(0, 120))
	return container.NewBorder(container.NewPadded(container.NewHBox(copyBtn)), nil, nil, nil, scroll)
}

// createExtractTab creates the table data extraction tab content.
// The tab is split into two screens: screen 1 collects settings (Proceed button),
// screen 2 collects columns/WHERE clause (Next Step button).
func (dp *DetailPanel) createExtractTab(conn *DecryptedConnection, keyspace, table string, session *gocql.Session, onExtractionStart *func()) fyne.CanvasObject {
	// Fetch columns for pre-population and validation (shared between both screens)
	allColumns, _ := fetchColumns(session, keyspace, table)
	allColumnNames := make([]string, len(allColumns))
	for i, col := range allColumns {
		allColumnNames[i] = col.Name
	}
	validColumnSet := make(map[string]bool, len(allColumns))
	for _, col := range allColumns {
		validColumnSet[col.Name] = true
	}

	workDir := dp.state.GetWorkingDirectory()

	// Build shared text values
	var diskInfoText string
	if workDir == "" {
		diskInfoText = "Working Directory : Not configured\nPlease configure working directory in Settings menu."
	} else {
		total, free, err := getDiskSpaceInfo(workDir)
		if err != nil {
			diskInfoText = fmt.Sprintf("Working Directory : %s\nUnable to get disk space: %v", workDir, err)
		} else {
			var percentage float64
			if total > 0 {
				percentage = float64(free) / float64(total) * 100
			}
			diskInfoText = fmt.Sprintf("Working Directory : %s\nMount Point Space: Total: %s | Free: %s (%.1f%% free)",
				workDir, formatBytes(total), formatBytes(free), percentage)
		}
	}
	var outputInfoText string
	if workDir != "" {
		outputInfoText = fmt.Sprintf("%s/cass-%s-<timestamp>.parquet",
			filepath.Join(workDir, "data"), table)
	} else {
		outputInfoText = "Output Location : Working directory not configured"
	}
	noteText := `NOTE : Data extraction is a multi-step process that may take significant time depending on table size. This operation requires free disk space in both the data directory and temp directory configured during application initialization.
Data extraction process will :
  1. Query data in batches from Cassandra
  2. Write intermediate results to temp directory
  3. Generate final Parquet file in data directory`

	// screenHolder swaps between screen 1 and screen 2
	screenHolder := container.NewStack()

	// ── Screen 2 ─────────────────────────────────────────────────────────────
	s2KeyspaceLabel := widget.NewLabel("Keyspace :")
	s2KeyspaceLabel.TextStyle = fyne.TextStyle{Bold: true}
	s2KeyspaceValue := widget.NewLabel(keyspace)
	s2KeyspaceValue.TextStyle = fyne.TextStyle{Bold: true, Italic: true}
	s2PageLabel := widget.NewLabel("Step 2 of 3")

	s2TableLabel := widget.NewLabel("Table Name :")
	s2TableLabel.TextStyle = fyne.TextStyle{Bold: true}
	s2TableValue := widget.NewLabel(table)
	s2TableValue.TextStyle = fyne.TextStyle{Bold: true, Italic: true}

	s2ColumnsLabel := widget.NewLabel("Columns (comma-separated) :")
	s2ColumnsLabel.TextStyle = fyne.TextStyle{Bold: true}
	s2ColumnsEntry := newSafeMultiLineEntry()
	s2ColumnsEntry.Wrapping = fyne.TextWrapWord
	s2ColumnsEntry.SetMinRowsVisible(3)
	s2ColumnsEntry.SetText(strings.Join(allColumnNames, ", "))

	s2WhereLabel := widget.NewLabel("WHERE clause *** (optional) :")
	s2WhereLabel.TextStyle = fyne.TextStyle{Bold: true}
	s2WhereEntry := newSafeMultiLineEntry()
	s2WhereEntry.Wrapping = fyne.TextWrapWord
	s2WhereEntry.SetMinRowsVisible(3)
	s2WhereEntry.SetPlaceHolder("e.g. id = 123 AND status = 'active'")

	s2WhereNote := dp.errorText("*** NOTE : WHERE clause will execute a CQL query on Cassandra with ALLOW FILTERING clause ***")
	s2WhereNote.TextStyle = fyne.TextStyle{Bold: true}

	// navigateToScreen1/navigateToScreen3 are assigned after their Scrolls are created (forward references)
	var navigateToScreen1 func()
	var navigateToScreen3 func()

	// s3CQLText is declared here so "Next Step" can set it; assigned during Screen 3 construction
	var s3CQLText *widget.Label
	// s3SelectedCols and s3WhereClause are set by "Next Step" and read by Screen 3's extract button
	var s3SelectedCols []string
	var s3WhereClause string

	var cancelExtract context.CancelFunc
	var cancelMu sync.Mutex

	s2ErrorLabel := dp.errorText("")
	s2ErrorLabel.TextStyle = fyne.TextStyle{Bold: true}
	setS2Error := func(msg string) {
		s2ErrorLabel.Text = msg
		s2ErrorLabel.Refresh()
	}

	cancelBtn := widget.NewButton("Back", func() {
		if navigateToScreen1 != nil {
			navigateToScreen1()
		}
	})

	var extractBtn *widget.Button
	extractBtn = widget.NewButton("Next Step", func() {
		setS2Error("")
		if conn == nil {
			dp.setStatus("Extract : no connection available")
			return
		}
		var selectedCols []string
		seen := make(map[string]bool)
		for _, c := range strings.Split(s2ColumnsEntry.Text, ",") {
			c = strings.TrimSpace(c)
			if c == "" || seen[c] {
				continue
			}
			seen[c] = true
			selectedCols = append(selectedCols, c)
		}
		if len(selectedCols) == 0 {
			selectedCols = allColumnNames
			s2ColumnsEntry.SetText(strings.Join(allColumnNames, ", "))
		}
		var invalidCols []string
		for _, c := range selectedCols {
			if !validColumnSet[c] {
				invalidCols = append(invalidCols, c)
			}
		}
		if len(invalidCols) > 0 {
			msg := fmt.Sprintf("    Unknown column(s) : %s", strings.Join(invalidCols, ", "))
			dp.setStatus(msg)
			setS2Error(msg)
			return
		}

		if rawWhere := strings.TrimSpace(s2WhereEntry.Text); rawWhere != "" {
			candidates := whereClauseColumnCandidates(rawWhere)
			var invalidWhereCols []string
			for _, c := range candidates {
				if !validColumnSet[strings.ToLower(c)] {
					invalidWhereCols = append(invalidWhereCols, c)
				}
			}
			if len(invalidWhereCols) > 0 {
				msg := fmt.Sprintf("    Unknown column(s) in WHERE clause : %s", strings.Join(invalidWhereCols, ", "))
				dp.setStatus(msg)
				setS2Error(msg)
				return
			}
		}

		colList := strings.Join(selectedCols, ", ")
		cqlDisplay := fmt.Sprintf("SELECT %s\nFROM %s.%s", colList, keyspace, table)
		if wc := strings.TrimSpace(s2WhereEntry.Text); wc != "" {
			if !strings.HasPrefix(strings.ToUpper(wc), "WHERE ") {
				wc = "WHERE " + wc
			}
			cqlDisplay += "\n" + wc + "\nALLOW FILTERING"
		}
		cqlDisplay += " ;"

		s3SelectedCols = selectedCols
		s3WhereClause = s2WhereEntry.Text
		if s3CQLText != nil {
			s3CQLText.SetText(cqlDisplay)
		}
		if navigateToScreen3 != nil {
			navigateToScreen3()
		}
	})
	extractBtn.Importance = widget.HighImportance
	if workDir == "" {
		extractBtn.Disable()
	}

	s2ButtonRow := container.NewPadded(container.NewHBox(extractBtn, cancelBtn, s2ErrorLabel))

	s2BlockedLabel := dp.successText("Extract : another extraction job is already in progress ...")
	s2BlockedLabel.TextStyle = fyne.TextStyle{Bold: true}
	s2BlockedLabel.Hide()
	if dp.state.IsExtractionRunning() {
		extractBtn.Disable()
		cancelBtn.Disable()
		s2BlockedLabel.Show()
	}

	screen2Content := container.NewVBox(
		container.NewHBox(s2KeyspaceLabel, s2KeyspaceValue, layout.NewSpacer(), s2PageLabel),
		widget.NewSeparator(),
		container.NewHBox(s2TableLabel, s2TableValue),
		widget.NewSeparator(),
		s2ColumnsLabel,
		s2ColumnsEntry,
		s2WhereLabel,
		s2WhereEntry,
		s2WhereNote,
		widget.NewLabel(""),
		widget.NewLabel(""),
		widget.NewSeparator(),
		s2ButtonRow,
		s2BlockedLabel,
	)
	screen2Scroll := container.NewVScroll(screen2Content)

	navigateToScreen2 := func() {
		screenHolder.Objects = []fyne.CanvasObject{screen2Scroll}
		screenHolder.Refresh()
	}

	// ── Screen 3 ─────────────────────────────────────────────────────────────
	s3KeyspaceLabel := widget.NewLabel("Keyspace :")
	s3KeyspaceLabel.TextStyle = fyne.TextStyle{Bold: true}
	s3KeyspaceValue := widget.NewLabel(keyspace)
	s3KeyspaceValue.TextStyle = fyne.TextStyle{Bold: true, Italic: true}
	s3PageLabel := widget.NewLabel("Step 3 of 3")

	s3TableLabel := widget.NewLabel("Table Name :")
	s3TableLabel.TextStyle = fyne.TextStyle{Bold: true}
	s3TableValue := widget.NewLabel(table)
	s3TableValue.TextStyle = fyne.TextStyle{Bold: true, Italic: true}

	s3CQLPreviewLabel := widget.NewLabel("CQL query :")
	s3CQLPreviewLabel.TextStyle = fyne.TextStyle{Bold: true}
	s3CQLText = widget.NewLabel("")
	s3CQLText.Wrapping = fyne.TextWrapWord

	s3ResultContainer := container.NewVBox()
	setS3ExtractResult := func(msg string, makeText func(string, float32) *canvas.Text) {
		s3ResultContainer.Objects = nil
		fontSize := dp.getFontSize()
		for _, line := range strings.Split(msg, "\n") {
			t := makeText(line, fontSize)
			s3ResultContainer.Add(t)
		}
		s3ResultContainer.Refresh()
	}

	s3ProgressLabel := dp.successText("Data extraction is in progress ...")
	s3ProgressLabel.TextStyle = fyne.TextStyle{Bold: true}
	s3ProgressLabel.Hide()

	var s3ShowBackOnly func()
	var s3ShowResultOnly func()
	var s3ResetToReady func()
	var s3ShowCancelOnly func()

	s3BackBtn := widget.NewButton("Back", func() {
		navigateToScreen2()
	})
	s3BackBtn.Importance = widget.HighImportance

	s3CancelBtn := widget.NewButton("Back", func() {
		cancelMu.Lock()
		fn := cancelExtract
		cancelMu.Unlock()
		if fn != nil {
			fn()
		} else {
			navigateToScreen2()
		}
	})

	s3CancelExtractBtn := widget.NewButton("Cancel", func() {
		cancelMu.Lock()
		fn := cancelExtract
		cancelMu.Unlock()
		if fn != nil {
			fn()
		}
	})

	var s3StartBtn *widget.Button
	s3StartBtn = widget.NewButton("Start Table Data Extract", func() {
		if dp.state.IsExtractionRunning() {
			dp.setStatus("Extract : another extraction job is already in progress")
			return
		}
		if conn == nil {
			dp.setStatus("Extract : no connection available")
			return
		}
		now := time.Now()
		filename := fmt.Sprintf("cass-%s-%s.parquet", table, now.Format("20060102_1504"))
		dataDir := filepath.Join(workDir, "data")

		ctx, cancel := context.WithCancel(context.Background())
		cancelMu.Lock()
		cancelExtract = cancel
		cancelMu.Unlock()
		if onExtractionStart != nil && *onExtractionStart != nil {
			(*onExtractionStart)()
			if existing := dp.state.GetExtractionNavFn(); existing != nil {
				dp.state.SetExtractionNavFn(existing)
			}
		}
		AppLog("starting data extraction from Cassandra table into parquet file")
		dp.state.SetExtractionRunning(true)
		s3StartBtn.Disable()
		s3ShowCancelOnly()
		s3ResultContainer.Objects = nil
		s3ResultContainer.Refresh()
		s3ProgressLabel.Show()
		s3ProgressLabel.Refresh()
		dp.setStatus("Extract : connecting to Cassandra ...")
		go func() {
			result, err := extractTableToParquet(
				ctx,
				*conn, keyspace, table,
				s3SelectedCols,
				s3WhereClause,
				dataDir, filename,
				func(msg string) {
					fyne.Do(func() { dp.setStatus(msg) })
				},
			)
			cancelMu.Lock()
			cancelExtract = nil
			cancelMu.Unlock()
			if err != nil {
				AppLog("extraction of data from Cassandra table into parquet file has failed")
				fyne.Do(func() {
					dp.state.SetExtractionRunning(false)
					s3ProgressLabel.Hide()
					s3ProgressLabel.Refresh()
					dp.setStatus("Data extract : failed")
					s3ShowBackOnly()
					setS3ExtractResult("Extract failed :\n"+strings.ReplaceAll(err.Error(), ": ", ":\n"), newErrorText)
				})
				return
			}
			if result.Cancelled {
				AppLog("data from Cassandra table was being extracted into parquet file, but operation was cancelled by user and partial data was saved to parquet file")
				fyne.Do(func() {
					dp.state.SetExtractionRunning(false)
					s3ProgressLabel.Hide()
					s3ProgressLabel.Refresh()
					dp.setStatus("Data Extraction Was Canceled")
					s3ResetToReady()
					setS3ExtractResult("Data Extraction Was Canceled", newErrorText)
				})
				return
			}
			if strings.TrimSpace(s3WhereClause) != "" {
				AppLog("data from Cassandra table has been extracted into parquet file using ALLOW FILTERING clause")
			} else {
				AppLog("data from Cassandra table has been extracted into parquet file")
			}
			fyne.Do(func() {
				dp.state.SetExtractionRunning(false)
				s3ProgressLabel.Hide()
				s3ProgressLabel.Refresh()
				dp.setStatus("Data extracted successfully")
				s3ShowResultOnly()
				setS3ExtractResult(
					fmt.Sprintf("Extracted %s rows in %s\nOutput file : %s",
						formatRowCount(int64(result.RowCount)),
						formatDuration(result.Duration),
						result.FilePath),
					newSuccessText)
			})
		}()
	})
	s3StartBtn.Importance = widget.HighImportance
	if workDir == "" {
		s3StartBtn.Disable()
	}

	s3ButtonRow := container.NewPadded(container.NewHBox(s3StartBtn, s3CancelBtn))
	s3BackBtnRow := container.NewPadded(container.NewHBox(s3BackBtn))
	s3BackBtnRow.Hide()

	s3ResetBtn := widget.NewButton("Reset", func() {
		s2ColumnsEntry.SetText(strings.Join(allColumnNames, ", "))
		s2WhereEntry.SetText("")
		navigateToScreen1()
	})
	s3ResetBtn.Importance = widget.HighImportance
	s3ResetBtnRow := container.NewPadded(container.NewHBox(s3ResetBtn))
	s3ResetBtnRow.Hide()

	s3CancelExtractBtnRow := container.NewPadded(container.NewHBox(s3CancelExtractBtn))
	s3CancelExtractBtnRow.Hide()

	s3ShowBackOnly = func() {
		s3ButtonRow.Hide()
		s3CancelExtractBtnRow.Hide()
		s3BackBtnRow.Show()
	}
	s3ShowResultOnly = func() {
		s3ButtonRow.Hide()
		s3CancelExtractBtnRow.Hide()
		s3ResetBtnRow.Show()
	}
	s3ResetToReady = func() {
		s3StartBtn.Enable()
		s3CancelExtractBtnRow.Hide()
		s3ButtonRow.Show()
		s3BackBtnRow.Hide()
		s3ResetBtnRow.Hide()
	}
	s3ShowCancelOnly = func() {
		s3ButtonRow.Hide()
		s3BackBtnRow.Hide()
		s3ResetBtnRow.Hide()
		s3CancelExtractBtnRow.Show()
	}

	s3BlockedLabel := dp.successText("Extract : another extraction job is already in progress ...")
	s3BlockedLabel.TextStyle = fyne.TextStyle{Bold: true}
	s3BlockedLabel.Hide()
	if dp.state.IsExtractionRunning() {
		s3StartBtn.Disable()
		s3CancelBtn.Disable()
		s3BlockedLabel.Show()
	}

	screen3Content := container.NewVBox(
		container.NewHBox(s3KeyspaceLabel, s3KeyspaceValue, layout.NewSpacer(), s3PageLabel),
		widget.NewSeparator(),
		container.NewHBox(s3TableLabel, s3TableValue),
		widget.NewSeparator(),
		s3CQLPreviewLabel,
		s3CQLText,
		widget.NewLabel(""),
		widget.NewSeparator(),
		s3ButtonRow,
		s3CancelExtractBtnRow,
		s3BackBtnRow,
		s3ResetBtnRow,
		s3ProgressLabel,
		s3BlockedLabel,
		widget.NewLabel(""),
		s3ResultContainer,
	)
	screen3Scroll := container.NewVScroll(screen3Content)

	// ── Screen 1 ─────────────────────────────────────────────────────────────
	s1KeyspaceLabel := widget.NewLabel("Keyspace :")
	s1KeyspaceLabel.TextStyle = fyne.TextStyle{Bold: true}
	s1KeyspaceValue := widget.NewLabel(keyspace)
	s1KeyspaceValue.TextStyle = fyne.TextStyle{Bold: true, Italic: true}
	s1PageLabel := widget.NewLabel("Step 1 of 3")

	s1TableLabel := widget.NewLabel("Table Name :")
	s1TableLabel.TextStyle = fyne.TextStyle{Bold: true}
	s1TableValue := widget.NewLabel(table)
	s1TableValue.TextStyle = fyne.TextStyle{Bold: true, Italic: true}

	s1NoteLabel := widget.NewLabel(noteText)
	s1NoteLabel.Wrapping = fyne.TextWrapWord
	s1NoteCard := widget.NewCard("", "", s1NoteLabel)

	s1DiskLabel := widget.NewLabel(diskInfoText)
	s1DiskLabel.Wrapping = fyne.TextWrapWord

	s1OutputLabel := widget.NewLabel(outputInfoText)
	s1OutputLabel.Wrapping = fyne.TextWrapWord

	proceedBtn := widget.NewButton("Proceed", func() {
		if strings.TrimSpace(s2ColumnsEntry.Text) == "" {
			s2ColumnsEntry.SetText(strings.Join(allColumnNames, ", "))
		}
		screenHolder.Objects = []fyne.CanvasObject{screen2Scroll}
		screenHolder.Refresh()
	})
	proceedBtn.Importance = widget.HighImportance
	if workDir == "" {
		proceedBtn.Disable()
	}

	s1BlockedLabel := dp.successText("Extract : another extraction job is already in progress ...")
	s1BlockedLabel.TextStyle = fyne.TextStyle{Bold: true}
	s1BlockedLabel.Hide()
	if dp.state.IsExtractionRunning() {
		proceedBtn.Disable()
		s1BlockedLabel.Show()
	}

	screen1Content := container.NewVBox(
		container.NewHBox(s1KeyspaceLabel, s1KeyspaceValue, layout.NewSpacer(), s1PageLabel),
		widget.NewSeparator(),
		container.NewHBox(s1TableLabel, s1TableValue),
		widget.NewSeparator(),
		s1NoteCard,
		widget.NewSeparator(),
		widget.NewLabel("Disk Space Information :"),
		s1DiskLabel,
		widget.NewSeparator(),
		widget.NewLabel("Output File Information :"),
		s1OutputLabel,
		widget.NewSeparator(),
		container.NewPadded(container.NewHBox(proceedBtn)),
		s1BlockedLabel,
	)
	screen1Scroll := container.NewVScroll(screen1Content)

	navigateToScreen3 = func() {
		s3ResetToReady()
		s3ResultContainer.Objects = nil
		s3ResultContainer.Refresh()
		screenHolder.Objects = []fyne.CanvasObject{screen3Scroll}
		screenHolder.Refresh()
	}
	navigateToScreen1 = func() {
		extractBtn.Enable()
		s3StartBtn.Enable()
		s3ButtonRow.Show()
		s3CancelExtractBtnRow.Hide()
		s3BackBtnRow.Hide()
		s3ResetBtnRow.Hide()
		s3ResultContainer.Objects = nil
		s3ResultContainer.Refresh()
		screenHolder.Objects = []fyne.CanvasObject{screen1Scroll}
		screenHolder.Refresh()
	}

	screenHolder.Objects = []fyne.CanvasObject{screen1Scroll}
	return screenHolder
}

// ShowColorSettings displays the color/theme settings panel
func (dp *DetailPanel) ShowColorSettings() {
	titleLabel := widget.NewLabel("Color Settings")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	descLabel := widget.NewLabel("Select a theme to customize the application appearance.")
	descLabel.Wrapping = fyne.TextWrapWord

	// Get current theme
	currentTheme, _ := fyne.CurrentApp().Settings().Theme().(*CassBrowserTheme)
	currentSchemeName := ""
	currentMode := "dark"
	if currentTheme != nil && currentTheme.GetScheme() != nil {
		currentSchemeName = currentTheme.GetScheme().Name
		currentMode = currentTheme.GetScheme().Mode
	}

	// Helper to create 3x3 grid of theme buttons
	createThemeGrid := func(themes []ThemeScheme) *fyne.Container {
		col1 := container.NewVBox()
		col2 := container.NewVBox()
		col3 := container.NewVBox()

		buttonWidth := float32(180) // Fixed width for all buttons
		buttonHeight := float32(48) // Fixed height for all buttons

		for i, scheme := range themes {
			schemeCopy := scheme // Capture for closure
			btn := widget.NewButton("  "+scheme.Name+"  ", func() {
				dp.applyThemeScheme(&schemeCopy)
			})
			if scheme.Name == currentSchemeName {
				btn.Importance = widget.HighImportance
			}

			// Wrap button in fixed-size container
			btnContainer := container.NewGridWrap(fyne.NewSize(buttonWidth, buttonHeight), btn)

			// Distribute buttons: 0,1,2 -> col1; 3,4,5 -> col2; 6,7,8 -> col3
			switch i / 3 {
			case 0:
				col1.Add(btnContainer)
				col1.Add(widget.NewLabel("")) // Spacing
			case 1:
				col2.Add(btnContainer)
				col2.Add(widget.NewLabel("")) // Spacing
			case 2:
				col3.Add(btnContainer)
				col3.Add(widget.NewLabel("")) // Spacing
			}
		}

		return container.NewHBox(
			container.NewGridWrap(fyne.NewSize(30, 1), layout.NewSpacer()), // Left buffer
			col1,
			col2,
			col3,
			layout.NewSpacer(),
		)
	}

	// Dark Mode tab content - 3 columns with 3 buttons each
	darkTabContent := container.NewVBox(
		widget.NewLabel(""), // Top spacing
		createThemeGrid(DarkThemes),
	)
	darkTab := container.NewTabItem("Dark Mode", container.NewVScroll(darkTabContent))

	// Light Mode tab content - 3 columns with 3 buttons each
	lightTabContent := container.NewVBox(
		widget.NewLabel(""), // Top spacing
		createThemeGrid(LightThemes),
	)
	lightTab := container.NewTabItem("Light Mode", container.NewVScroll(lightTabContent))

	// Color Blind tab content - 3 columns with 3 buttons each
	cbTabContent := container.NewVBox(
		widget.NewLabel(""), // Top spacing
		createThemeGrid(ColorBlindThemes),
	)
	cbTab := container.NewTabItem("Color Safe", container.NewVScroll(cbTabContent))

	// Create tabs
	tabs := container.NewAppTabs(darkTab, lightTab, cbTab)
	if currentMode == "light" {
		tabs.Select(lightTab)
	} else if currentMode == "colorblind" {
		tabs.Select(cbTab)
	}

	content := container.NewBorder(
		container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			descLabel,
			widget.NewLabel(""),
		),
		nil,
		nil, nil,
		tabs,
	)

	dp.setContent(content)
}

// applyThemeScheme applies a theme scheme and refreshes the UI
func (dp *DetailPanel) applyThemeScheme(scheme *ThemeScheme) {
	// Update the theme
	newTheme := NewCassBrowserThemeWithScheme(scheme.Name)
	fyne.CurrentApp().Settings().SetTheme(newTheme)

	// Save to state
	dp.state.SetThemeScheme(scheme.Name)
	AppLog("app color changed")

	// Refresh the color settings panel to update button highlighting
	dp.ShowColorSettings()
}

// ShowFontSettings displays the font size settings panel.
func (dp *DetailPanel) ShowFontSettings() {
	titleLabel := widget.NewLabel("Font Size Settings")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	descLabel := widget.NewLabel("App Font Size affects all UI elements (menus, buttons, labels). Grid Font Size affects data tables and query results.")
	descLabel.Wrapping = fyne.TextWrapWord

	_, origAppSize, _, _ := dp.state.GetAppFontSettings()
	_, origGridSize, _, _ := dp.state.GetGridFontSettings()

	sizeOptions := []string{"10", "11", "12", "13", "14", "15", "16", "18", "20", "22", "24", "28", "32"}

	findSizeIndex := func(size float32, defaultIdx int) int {
		target := fmt.Sprintf("%.0f", size)
		for i, s := range sizeOptions {
			if s == target {
				return i
			}
		}
		return defaultIdx
	}

	appSizeSelect := widget.NewSelect(sizeOptions, nil)
	appSizeSelect.SetSelectedIndex(findSizeIndex(origAppSize, 7)) // default 18

	gridSizeSelect := widget.NewSelect(sizeOptions, nil)
	gridSizeSelect.SetSelectedIndex(findSizeIndex(origGridSize, 4)) // default 14

	// App font preview — updates live
	previewText := canvas.NewText(AppDisplayName+" — Sample Text 12345", nil)
	previewText.TextSize = origAppSize
	appSizeSelect.OnChanged = func(s string) {
		var size float32
		fmt.Sscanf(s, "%f", &size)
		previewText.TextSize = size
		previewText.Refresh()
	}

	// Grid font preview — small sample table, updates live
	gridPreviewHeaders := []string{"column_1", "column_2", "column_3"}
	gridPreviewData := [][]string{
		{"value_a", "12345", "text"},
		{"value_b", "67890", "sample"},
		{"value_c", "11111", "data"},
	}
	gridPreviewSize := origGridSize
	colW := float32(90)
	rowH := float32(28)

	gridPreviewTable := widget.NewTable(
		func() (int, int) { return len(gridPreviewData) + 1, len(gridPreviewHeaders) },
		func() fyne.CanvasObject {
			t := canvas.NewText("", theme.ForegroundColor())
			t.TextSize = gridPreviewSize
			return t
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			t := obj.(*canvas.Text)
			t.TextSize = gridPreviewSize
			t.Color = theme.ForegroundColor()
			if id.Row == 0 {
				t.Text = gridPreviewHeaders[id.Col]
				t.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				t.Text = gridPreviewData[id.Row-1][id.Col]
			}
			t.Refresh()
		},
	)
	for i := range gridPreviewHeaders {
		gridPreviewTable.SetColumnWidth(i, colW)
	}
	for i := 0; i <= len(gridPreviewData); i++ {
		gridPreviewTable.SetRowHeight(i, rowH)
	}
	gridPreviewFixed := container.NewGridWrap(
		fyne.NewSize(colW*float32(len(gridPreviewHeaders)), rowH*float32(len(gridPreviewData)+2)),
		gridPreviewTable,
	)
	gridSizeSelect.OnChanged = func(s string) {
		var size float32
		fmt.Sscanf(s, "%f", &size)
		gridPreviewSize = size
		gridPreviewTable.Refresh()
	}

	saveBtn := widget.NewButton("Save", func() {
		var appSize, gridSize float32
		fmt.Sscanf(appSizeSelect.Selected, "%f", &appSize)
		fmt.Sscanf(gridSizeSelect.Selected, "%f", &gridSize)

		if appSize != origAppSize {
			AppLog("app font size changed")
		}
		if gridSize != origGridSize {
			AppLog("grid font size changed")
		}

		dp.state.SetAppFontSize(appSize)
		dp.state.SetGridFontSize(gridSize)

		if th, ok := fyne.CurrentApp().Settings().Theme().(*CassBrowserTheme); ok {
			th.SetCustomFontSize(appSize)
			fyne.CurrentApp().Settings().SetTheme(th)
		}
		dp.setStatusReady()
		dp.goBack()
	})
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", func() {
		if th, ok := fyne.CurrentApp().Settings().Theme().(*CassBrowserTheme); ok {
			th.SetCustomFontSize(origAppSize)
			fyne.CurrentApp().Settings().SetTheme(th)
		}
		dp.setStatusReady()
		dp.goBack()
	})

	defaultsBtn := widget.NewButton("Defaults", func() {
		appSizeSelect.SetSelectedIndex(findSizeIndex(18, 7))
		gridSizeSelect.SetSelectedIndex(findSizeIndex(14, 4))
	})

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		descLabel,
		widget.NewLabel(""),
		container.NewHBox(widget.NewLabel("App Font Size :"), appSizeSelect),
		widget.NewLabel("App Font Preview :"),
		container.NewPadded(previewText),
		widget.NewLabel(""),
		container.NewHBox(widget.NewLabel("Grid Font Size :"), gridSizeSelect),
		widget.NewLabel("Grid Font Preview :"),
		container.NewPadded(gridPreviewFixed),
		widget.NewLabel(""),
		widget.NewSeparator(),
		container.NewHBox(defaultsBtn, saveBtn, cancelBtn),
	)

	dp.setContent(content)
}

// sanitizeFilename removes or replaces characters that are invalid in filenames.
func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	return re.ReplaceAllString(s, "")
}

// ShowWorkingDirectorySetup displays the working directory configuration panel
func (dp *DetailPanel) ShowWorkingDirectorySetup(isInitialSetup bool, onComplete func()) {
	titleLabel := widget.NewLabel("Working Directory Setup")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Description
	descText := "Specify a directory where " + AppDisplayName + " will store data files and temporary files.\n\n" +
		"The following sub-directories will be created :\n" +
		"  • data/  - for storing data files\n" +
		"  • temp/  - for temporary files"
	descLabel := widget.NewLabel(descText)
	descLabel.Wrapping = fyne.TextWrapWord

	// Get current or default path
	defaultPath := getDefaultWorkingDirectory()
	currentPath := dp.state.GetWorkingDirectory()
	if currentPath == "" {
		currentPath = defaultPath
	}

	// Path entry — validation fires on focus lost, not on every keystroke
	pathEntry := newFocusEntry()
	pathEntry.SetText(currentPath)
	pathEntry.SetPlaceHolder("Enter directory path")

	errorLabel := dp.errorText("")

	statusLabel := dp.successText("")

	// Validate function
	validatePath := func() bool {
		path := strings.TrimSpace(pathEntry.Text)
		if path == "" {
			errorLabel.Text = "Path cannot be empty"
			errorLabel.Refresh()
			statusLabel.Text = ""
			statusLabel.Refresh()
			return false
		}

		// Expand ~ to home directory
		if strings.HasPrefix(path, "~") {
			home := filepath.Dir(getDefaultWorkingDirectory())
			path = strings.Replace(path, "~", home, 1)
			pathEntry.SetText(path)
		}

		err := validateWorkingDirectory(path)
		if err != nil {
			errorLabel.Text = err.Error()
			errorLabel.Refresh()
			statusLabel.Text = ""
			statusLabel.Refresh()
			return false
		}

		if !isInitialSetup {
			oldPath := dp.state.GetWorkingDirectory()
			if oldPath != "" {
				cleanNew := filepath.Clean(path) + string(filepath.Separator)
				cleanOld := filepath.Clean(oldPath) + string(filepath.Separator)
				if strings.HasPrefix(cleanNew, cleanOld) {
					errorLabel.Text = "New directory cannot be a sub-folder of current working directory."
					errorLabel.Refresh()
					statusLabel.Text = ""
					statusLabel.Refresh()
					return false
				}
			}
		}

		errorLabel.Text = ""
		errorLabel.Refresh()
		statusLabel.Text = "Directory is valid"
		statusLabel.Refresh()
		return true
	}

	// formContent is assigned below after the container is built; the closure
	// captures it by reference so it is valid by the time the button is clicked.
	var formContent *fyne.Container

	// Browse button — opens an inline folder browser inside the right panel
	browseBtn := widget.NewButton("Browse ...", func() {
		startPath := strings.TrimSpace(pathEntry.Text)
		if startPath == "" {
			startPath = getDefaultWorkingDirectory()
		}
		dp.showInlineFolderBrowser(startPath,
			func(selected string) {
				pathEntry.SetText(selected)
				dp.setContent(formContent)
				validatePath()
			},
			func() {
				dp.setContent(formContent)
			},
		)
	})

	// Validate when focus leaves the entry (covers both tab-away and clicking buttons/elsewhere)
	pathEntry.onFocusLost = func() {
		validatePath()
	}

	// Path input row — constrained width so the entry doesn't span the full panel
	pathLabel := widget.NewLabel("Working Directory :")
	pathBox := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(formEntryWidth()*1.1, 52),
			container.NewBorder(nil, nil, nil, browseBtn, pathEntry),
		),
		layout.NewSpacer(),
	)

	// Save button
	saveBtn := widget.NewButton("Save", func() {
		path := strings.TrimSpace(pathEntry.Text)
		if !validatePath() {
			return
		}

		oldPath := dp.state.GetWorkingDirectory()

		// If changing directory (not initial setup) and old path exists, show migration panel
		if !isInitialSetup && oldPath != "" && oldPath != path {
			dp.showMigrationConfirm(oldPath, path, onComplete)
			return
		}

		// Ensure subdirectories exist
		if err := ensureWorkingSubDirectories(path); err != nil {
			errorLabel.Text = "Failed to create sub-directories : " + err.Error()
			errorLabel.Refresh()
			return
		}

		dp.state.SetWorkingDirectory(path)
		AppLog("selected new working directory")
		dp.setStatusReady()
		if onComplete != nil {
			onComplete()
		} else {
			dp.goBack()
		}
	})

	saveBtn.Importance = widget.HighImportance

	// Cancel/Back button
	cancelLabel := "Cancel"
	if isInitialSetup {
		cancelLabel = "Use Default"
	}
	cancelBtn := widget.NewButton(cancelLabel, func() {
		if isInitialSetup {
			// Use default path
			path := getDefaultWorkingDirectory()
			if err := validateWorkingDirectory(path); err == nil {
				ensureWorkingSubDirectories(path)
				dp.state.SetWorkingDirectory(path)
			}
		}
		dp.setStatusReady()
		if onComplete != nil {
			onComplete()
		} else {
			dp.goBack()
		}
	})

	// Button row — left-aligned
	buttonRow := container.NewHBox(saveBtn, cancelBtn)

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		descLabel,
		widget.NewLabel(""),
		pathLabel,
		pathBox,
		errorLabel,
		statusLabel,
		widget.NewLabel(""),
		widget.NewSeparator(),
		buttonRow,
	)
	formContent = content

	dp.setContent(formContent)

	// Initial validation
	validatePath()
}

// showInlineFolderBrowser replaces the right panel with a directory browser.
// onSelect is called with the chosen path; onCancel is called when the user
// dismisses without choosing. Both callbacks should restore the previous content.
func (dp *DetailPanel) showInlineFolderBrowser(startPath string, onSelect func(string), onCancel func()) {
	// Normalise: if startPath is not an accessible directory, walk up until one is.
	for startPath != filepath.Dir(startPath) {
		if info, err := os.Stat(startPath); err == nil && info.IsDir() {
			break
		}
		startPath = filepath.Dir(startPath)
	}

	currentDir := startPath
	var entries []string // visible subdirectory names in currentDir

	pathLabel := widget.NewLabel(currentDir)
	pathLabel.Wrapping = fyne.TextWrapWord

	errText := dp.errorText("")

	var dirList *widget.List

	loadDir := func(path string) {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			errText.Text = "Cannot access: " + path
			errText.Refresh()
			return
		}
		raw, err := os.ReadDir(path)
		entries = nil
		if err != nil {
			errText.Text = "Cannot read directory: " + err.Error()
			errText.Refresh()
		} else {
			errText.Text = ""
			for _, e := range raw {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					entries = append(entries, e.Name())
				}
			}
		}
		currentDir = path
		pathLabel.SetText(currentDir)
		errText.Refresh()
		if dirList != nil {
			dirList.Refresh()
			dirList.ScrollToTop()
		}
	}

	dirList = widget.NewList(
		func() int { return len(entries) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewIcon(theme.FolderIcon()),
				widget.NewLabel("placeholder"),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*fyne.Container).Objects[1].(*widget.Label).SetText(entries[id])
		},
	)
	dirList.OnSelected = func(id widget.ListItemID) {
		loadDir(filepath.Join(currentDir, entries[id]))
		dirList.UnselectAll()
	}

	loadDir(currentDir)

	titleLabel := widget.NewLabel("Browse for Directory")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	upBtn := widget.NewButton("Up", func() {
		parent := filepath.Dir(currentDir)
		if parent != currentDir {
			loadDir(parent)
		}
	})

	selectBtn := widget.NewButton("Select This Directory", func() { onSelect(currentDir) })
	selectBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", func() { onCancel() })

	locLabel := widget.NewLabel("Location :")
	locLabel.TextStyle = fyne.TextStyle{Bold: true}

	header := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		container.NewHBox(selectBtn, cancelBtn),
		widget.NewSeparator(),
		locLabel,
		pathLabel,
		container.NewHBox(upBtn),
		errText,
		widget.NewSeparator(),
	)

	dp.setContent(container.NewBorder(header, nil, nil, nil, dirList))
}

// showMigrationConfirm displays the migration confirmation panel
func (dp *DetailPanel) showMigrationConfirm(oldPath, newPath string, onComplete func()) {
	titleLabel := widget.NewLabel("Migrate Data ?")
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		widget.NewLabel("You are changing the working directory."),
		widget.NewLabel(""),
		widget.NewLabel("From: "+oldPath),
		widget.NewLabel("To: "+newPath),
		widget.NewLabel(""),
		widget.NewLabel("Do you want to migrate existing data to the new location ?"),
		widget.NewLabel(""),
	)

	// showMigrationResult displays an inline success or error panel after migration.
	showMigrationResult := func(success bool, message string) {
		var heading string
		var msgText *canvas.Text
		if success {
			heading = "Migration Complete"
			msgText = dp.successText(message)
		} else {
			heading = "Migration Failed"
			msgText = dp.errorText(message)
		}

		headingLabel := widget.NewLabel(heading)
		headingLabel.TextStyle = fyne.TextStyle{Bold: true}

		continueBtn := widget.NewButton("Continue", func() {
			dp.setStatusReady()
			if onComplete != nil {
				onComplete()
			} else {
				dp.goBack()
			}
		})
		continueBtn.Importance = widget.HighImportance

		dp.setContent(container.NewVBox(
			headingLabel,
			widget.NewSeparator(),
			widget.NewLabel(""),
			msgText,
			widget.NewLabel(""),
			widget.NewSeparator(),
			container.NewHBox(continueBtn),
		))
	}

	// Migrate button
	migrateBtn := widget.NewButton("Yes, Migrate", func() {
		// Verify the destination filesystem has enough free space
		oldSize, sizeErr := CalculateDirSize(oldPath)
		if sizeErr == nil && oldSize > 0 {
			// Walk up from newPath to find the nearest existing ancestor for the Statfs call
			checkPath := newPath
			for {
				if _, err := os.Stat(checkPath); err == nil {
					break
				}
				parent := filepath.Dir(checkPath)
				if parent == checkPath {
					break
				}
				checkPath = parent
			}
			_, freeSpace, diskErr := getDiskSpaceInfo(checkPath)
			if diskErr == nil && freeSpace < oldSize {
				headingLabel := widget.NewLabel("Insufficient Disk Space")
				headingLabel.TextStyle = fyne.TextStyle{Bold: true}

				reqText := dp.errorText("Required  : " + formatBytes(oldSize))

				freeText := dp.errorText("Available : " + formatBytes(freeSpace))

				backBtn := widget.NewButton("Go Back", func() {
					dp.ShowWorkingDirectorySetup(false, onComplete)
				})

				dp.setContent(container.NewVBox(
					headingLabel,
					widget.NewSeparator(),
					widget.NewLabel(""),
					reqText,
					freeText,
					widget.NewLabel(""),
					widget.NewSeparator(),
					container.NewHBox(backBtn),
				))
				return
			}
		}

		if err := ensureWorkingSubDirectories(newPath); err != nil {
			showMigrationResult(false, "Failed to create sub-directories : "+err.Error())
			return
		}

		_, appFontSize, _, _ := dp.state.GetAppFontSettings()
		statusText := canvas.NewText("Migrating data to new location, please wait ...", theme.ForegroundColor())
		statusText.TextSize = appFontSize

		progressLabel := widget.NewLabel("Migration in progress ...")
		progressLabel.TextStyle = fyne.TextStyle{Bold: true}

		dp.setContent(container.NewVBox(
			progressLabel,
			widget.NewSeparator(),
			widget.NewLabel(""),
			container.NewCenter(statusText),
		))

		go func() {
			err := MigrateWorkingDirectory(oldPath, newPath)
			if err != nil {
				AppLog("working directory migration failed")
				fyne.Do(func() { showMigrationResult(false, err.Error()) })
				return
			}
			dp.state.SetWorkingDirectory(newPath)
			AppLog("working directory migration completed")
			fyne.Do(func() { showMigrationResult(true, "Data has been migrated to the new location.") })
		}()
	})

	// Just change button
	justChangeBtn := widget.NewButton("No, Just Change Path", func() {
		if err := ensureWorkingSubDirectories(newPath); err != nil {
			dialog.ShowError(fmt.Errorf("Failed to create sub-directories : %v", err), dp.window)
			return
		}
		dp.state.SetWorkingDirectory(newPath)
		dp.setStatusReady()
		if onComplete != nil {
			onComplete()
		} else {
			dp.goBack()
		}
	})

	// Cancel button
	cancelBtn := widget.NewButton("Cancel", func() {
		// Go back to working directory setup
		dp.ShowWorkingDirectorySetup(false, onComplete)
	})

	migrateBtn.Importance = widget.HighImportance
	buttonRow := container.NewHBox(migrateBtn, justChangeBtn, cancelBtn)

	content.Add(widget.NewSeparator())
	content.Add(buttonRow)

	dp.setContent(content)
}

// ShowJoinLocalFileList displays a clickable list of all parquet files in the working data directory.
func (dp *DetailPanel) ShowJoinLocalFileList() {
	dp.showJoinStep1(nil)
}

// showJoinStep1 renders Step 1 of the join flow. preSelected restores prior checkbox state when
// returning from Step 2; pass nil for a fresh start.
func (dp *DetailPanel) showJoinStep1(preSelected map[string]string) {
	titleMain := widget.NewLabel("JOIN Data In Local Files")
	titleMain.TextStyle = fyne.TextStyle{Bold: true}
	titlePowered := widget.NewLabel(" powered by ")
	titlePowered.TextStyle = fyne.TextStyle{Italic: true}
	titleDuckDB := widget.NewLabel("DuckDB")
	titleDuckDB.TextStyle = fyne.TextStyle{Bold: true}
	titleLabel := container.NewHBox(titleMain, titlePowered, titleDuckDB)

	workDir := dp.state.GetWorkingDirectory()
	if workDir == "" {
		dp.setContent(container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("No working directory configured."),
		))
		return
	}

	dataDir := filepath.Join(workDir, "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		dp.setContent(container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel("Could not read data directory : "+err.Error()),
		))
		return
	}

	type fileEntry struct {
		name string
		path string
	}

	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".parquet") {
			continue
		}
		files = append(files, fileEntry{
			name: e.Name(),
			path: filepath.Join(dataDir, e.Name()),
		})
	}

	if len(files) == 0 {
		dp.setStatus("No parquet files are found.")
		errMsg := dp.errorText("No parquet files are found.")
		errMsg.TextStyle = fyne.TextStyle{Bold: true}
		dp.setContent(container.NewVBox(
			titleLabel,
			widget.NewSeparator(),
			widget.NewLabel(""),
			widget.NewLabel(""),
			errMsg,
		))
		return
	}

	selected := map[string]string{} // name -> path

	joinBtn := widget.NewButton("Join Selected Files", func() {
		dp.showJoinStep2(selected, nil)
	})
	joinBtn.Importance = widget.HighImportance
	joinBtn.Disable()

	var items []fyne.CanvasObject
	for _, f := range files {
		f := f
		var chk *widget.Check
		chk = widget.NewCheck(f.name, func(checked bool) {
			if checked {
				if len(selected) >= 5 {
					chk.SetChecked(false)
					return
				}
				selected[f.name] = f.path
			} else {
				delete(selected, f.name)
			}
			if len(selected) >= 2 {
				joinBtn.Enable()
			} else {
				joinBtn.Disable()
			}
		})
		if _, ok := preSelected[f.name]; ok {
			selected[f.name] = f.path
			chk.SetChecked(true)
		}
		items = append(items, chk)
	}

	if len(selected) >= 2 {
		joinBtn.Enable()
	}

	countLabel := canvas.NewText("Select up to 5 files to join ...", theme.ForegroundColor())
	countLabel.TextStyle = fyne.TextStyle{Italic: true}
	step1Label := widget.NewLabel("Step 1 of 4")
	step1Label.Alignment = fyne.TextAlignTrailing

	allItems := []fyne.CanvasObject{
		titleLabel,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, step1Label, countLabel),
		widget.NewSeparator(),
	}
	allItems = append(allItems, items...)
	allItems = append(allItems, widget.NewLabel(""))
	allItems = append(allItems, widget.NewSeparator(), container.NewPadded(container.NewHBox(joinBtn)))

	dp.setContent(container.NewStack(container.NewVScroll(container.NewVBox(allItems...))))
}

// showJoinStep2 displays Step 2 of the join flow: one column per selected file
// showing the file name as header and all parquet column names below it.
// preSelectedCols restores checkbox state when returning from Step 3; pass nil for a fresh start.
func (dp *DetailPanel) showJoinStep2(selected map[string]string, preSelectedCols map[string]map[string]bool) {
	titleMain := widget.NewLabel("JOIN Data In Local Files")
	titleMain.TextStyle = fyne.TextStyle{Bold: true}
	titlePowered := widget.NewLabel(" powered by ")
	titlePowered.TextStyle = fyne.TextStyle{Italic: true}
	titleDuckDB := widget.NewLabel("DuckDB")
	titleDuckDB.TextStyle = fyne.TextStyle{Bold: true}
	titleLabel := container.NewHBox(titleMain, titlePowered, titleDuckDB)

	step2Label := widget.NewLabel("Step 2 of 4")
	step2Label.Alignment = fyne.TextAlignTrailing

	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)

	// Phase 1: read all parquet schemas up front (one call per file).
	type cachedSchema struct {
		columns []ParquetColumnInfo
		err     error
	}
	schemas := make(map[string]*cachedSchema, len(names))
	for _, name := range names {
		preview, err := readParquetPreview(selected[name], 0)
		if err != nil {
			schemas[name] = &cachedSchema{err: err}
		} else {
			schemas[name] = &cachedSchema{columns: preview.Columns}
		}
	}

	// selectedCols tracks checked columns per table; colOrder stores schema order per table.
	selectedCols := make(map[string]map[string]bool, len(names))
	colOrder := make(map[string][]string, len(names))
	for _, name := range names {
		if preSelectedCols != nil && preSelectedCols[name] != nil {
			selectedCols[name] = preSelectedCols[name]
		} else {
			selectedCols[name] = map[string]bool{}
		}
	}

	// On a fresh start, auto-select any column that appears in 2+ tables,
	// but only in the tables that actually have it.
	if preSelectedCols == nil {
		colOwners := map[string][]string{} // col name -> tables that have it
		allValid := true
		for _, name := range names {
			if schemas[name].err != nil {
				allValid = false
				break
			}
			for _, col := range schemas[name].columns {
				colOwners[col.Name] = append(colOwners[col.Name], name)
			}
		}
		if allValid {
			for col, owners := range colOwners {
				if len(owners) >= 2 {
					for _, name := range owners {
						selectedCols[name][col] = true
					}
				}
			}
		}
	}

	nextBtn := widget.NewButton("Next Step", func() {
		dp.showJoinStep3(names, selected, selectedCols, colOrder, "")
	})
	nextBtn.Importance = widget.HighImportance
	nextBtn.Disable()

	updateNextBtn := func() {
		for _, name := range names {
			if len(selectedCols[name]) == 0 {
				nextBtn.Disable()
				return
			}
		}
		nextBtn.Enable()
	}

	tableCols := make([]fyne.CanvasObject, 0, len(names))
	for i, name := range names {
		name := name

		headerLabel := widget.NewLabel(name)
		headerLabel.TextStyle = fyne.TextStyle{Bold: true}

		aliasLabel := widget.NewLabel(fmt.Sprintf("(alias t%d)", i+1))

		colItems := []fyne.CanvasObject{headerLabel, aliasLabel, widget.NewSeparator()}

		s := schemas[name]
		if s.err != nil {
			colItems = append(colItems, widget.NewLabel("Error reading file"))
		} else {
			colOrder[name] = make([]string, len(s.columns))
			for j, col := range s.columns {
				colOrder[name][j] = col.Name
			}
			for _, col := range s.columns {
				col := col
				chk := widget.NewCheck(col.Name, func(checked bool) {
					if checked {
						selectedCols[name][col.Name] = true
					} else {
						delete(selectedCols[name], col.Name)
					}
					updateNextBtn()
				})
				if selectedCols[name][col.Name] {
					chk.SetChecked(true)
				}
				colItems = append(colItems, chk)
			}
		}

		tableCols = append(tableCols, container.NewVBox(colItems...))
	}
	updateNextBtn()

	backBtn := widget.NewButton("Back", func() {
		dp.showJoinStep1(selected)
	})

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, step2Label, func() fyne.CanvasObject {
			t := canvas.NewText("Select columns for query including join columns ...", theme.ForegroundColor())
			t.TextStyle = fyne.TextStyle{Italic: true}
			return t
		}()),
		widget.NewSeparator(),
		container.NewGridWithColumns(len(tableCols), tableCols...),
		widget.NewLabel(""),
		widget.NewSeparator(),
		container.NewPadded(container.NewHBox(backBtn, nextBtn)),
	)

	dp.setContent(container.NewStack(container.NewVScroll(content)))
}

// showJoinStep3 displays Step 3: a generated SQL SELECT query the user can edit.
// preQuery, when non-empty, is shown instead of the auto-generated query.
func (dp *DetailPanel) showJoinStep3(names []string, selected map[string]string, selectedCols map[string]map[string]bool, colOrder map[string][]string, preQuery string) {
	titleMain := widget.NewLabel("JOIN Data In Local Files")
	titleMain.TextStyle = fyne.TextStyle{Bold: true}
	titlePowered := widget.NewLabel(" powered by ")
	titlePowered.TextStyle = fyne.TextStyle{Italic: true}
	titleDuckDB := widget.NewLabel("DuckDB")
	titleDuckDB.TextStyle = fyne.TextStyle{Bold: true}
	titleLabel := container.NewHBox(titleMain, titlePowered, titleDuckDB)

	step3Label := widget.NewLabel("Step 3 of 4")
	step3Label.Alignment = fyne.TextAlignTrailing

	// Build alias map (alias -> path) and alias labels.
	aliases := make(map[string]string, len(names))
	var aliasItems []fyne.CanvasObject
	for i, name := range names {
		alias := fmt.Sprintf("t%d", i+1)
		aliases[alias] = selected[name]
		aliasItems = append(aliasItems, widget.NewLabel(fmt.Sprintf("%s  ( alias : %s )", name, alias)))
	}

	// Build SELECT clause in schema order, grouped by table.
	var selectParts []string
	for i, name := range names {
		alias := fmt.Sprintf("t%d", i+1)
		for _, col := range colOrder[name] {
			if selectedCols[name][col] {
				selectParts = append(selectParts, alias+"."+col)
			}
		}
	}

	// Build FROM/JOIN clause.
	// For each table ti (i >= 1) find columns that are selected in ti AND in any
	// earlier table tj (j < i), referencing the first such earlier table found.
	// This handles partial overlap where a column is shared by only some tables.
	fromLines := []string{"t1"}
	for i := 1; i < len(names); i++ {
		curAlias := fmt.Sprintf("t%d", i+1)
		var conditions []string
		for _, col := range colOrder[names[i]] {
			if !selectedCols[names[i]][col] {
				continue
			}
			for j := 0; j < i; j++ {
				if selectedCols[names[j]][col] {
					prevAlias := fmt.Sprintf("t%d", j+1)
					conditions = append(conditions, fmt.Sprintf("%s.%s = %s.%s", prevAlias, col, curAlias, col))
					break
				}
			}
		}
		onClause := strings.Join(conditions, " AND ")
		if onClause == "" {
			onClause = "<add_join_condition_for_" + curAlias + ">"
		}
		fromLines = append(fromLines, fmt.Sprintf("JOIN %s ON %s", curAlias, onClause))
	}
	fromClause := strings.Join(fromLines, "\n")

	generatedQuery := fmt.Sprintf("SELECT %s\nFROM %s\n-- modify WHERE clause according to your requirement\n-- WHERE <condition>\nLIMIT 100 ;",
		strings.Join(selectParts, ", "),
		fromClause)
	query := generatedQuery
	if preQuery != "" {
		query = preQuery
	}

	queryEntry := widget.NewMultiLineEntry()
	queryEntry.SetText(query)
	queryEntry.SetMinRowsVisible(14)
	queryEntry.Wrapping = fyne.TextWrapWord

	statusBox := container.NewStack()
	statusBox.Hide()
	setQueryStatus := func(msg string) {
		if msg == "" {
			statusBox.Objects = nil
			statusBox.Refresh()
			statusBox.Hide()
			return
		}
		statusBox.Objects = []fyne.CanvasObject{widget.NewLabel(msg)}
		statusBox.Refresh()
		statusBox.Show()
	}
	setQueryError := func(msg string) {
		errText := dp.errorText(msg)
		errText.TextStyle = fyne.TextStyle{Bold: true}
		statusBox.Objects = []fyne.CanvasObject{errText}
		statusBox.Refresh()
		statusBox.Show()
	}

	executingText3 := dp.successText("Executing query ...")
	executingLabel := container.NewVBox(widget.NewLabel(""), widget.NewLabel(""), executingText3)
	executingLabel.Hide()

	var runBtn *widget.Button
	backBtn := widget.NewButton("Back", func() {
		dp.showJoinStep2(selected, selectedCols)
	})

	runBtn = widget.NewButton("Run Query", func() {
		q := strings.TrimSpace(queryEntry.Text)
		if q == "" {
			setQueryStatus("ERROR : query cannot be empty.")
			return
		}
		if !isSelectOnlySQL(q) {
			setQueryError("Currently this query/statement is not supported.")
			return
		}
		setQueryStatus("")
		executingLabel.Show()
		runBtn.Disable()
		backBtn.Disable()
		dp.setStatus("Executing join query ...")

		go func() {
			cols, rows, stats, isPreview, err := executeDuckDBJoinQueryPreview(aliases, q)
			if err != nil {
				AppLog("failed to execute sql query with joins on parquet files")
				fyne.Do(func() {
					executingLabel.Hide()
					runBtn.Enable()
					backBtn.Enable()
					setQueryStatus("ERROR : " + err.Error())
					dp.setStatus("Join query failed.")
				})
				return
			}
			AppLog("executed sql query with joins on parquet files")
			perfInfo := fmt.Sprintf("Load : %s | Query : %s | Total : %s",
				formatQueryDuration(stats.LoadDuration),
				formatQueryDuration(stats.QueryDuration),
				formatQueryDuration(stats.LoadDuration+stats.QueryDuration))
			fyne.Do(func() {
				executingLabel.Hide()
				runBtn.Enable()
				backBtn.Enable()
				dp.showJoinStep4(selected, selectedCols, aliases, q, cols, rows, perfInfo, isPreview)
			})
		}()
	})
	runBtn.Importance = widget.HighImportance

	aliasBox := container.NewVBox(aliasItems...)

	content := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, step3Label, func() fyne.CanvasObject {
			t := canvas.NewText("SQL Query with JOINs ...", theme.ForegroundColor())
			t.TextStyle = fyne.TextStyle{Italic: true}
			return t
		}()),
		widget.NewSeparator(),
		aliasBox,
		widget.NewSeparator(),
		queryEntry,
		statusBox,
		executingLabel,
		widget.NewSeparator(),
		container.NewPadded(container.NewHBox(backBtn, runBtn)),
	)

	dp.setContent(container.NewStack(container.NewVScroll(content)))
}

// showJoinStep4 displays Step 4: the results of the executed join query.
func (dp *DetailPanel) showJoinStep4(selected map[string]string, selectedCols map[string]map[string]bool, aliases map[string]string, query string, cols []string, rows []map[string]interface{}, perfInfo string, isPreview bool) {
	titleMain := widget.NewLabel("JOIN Data In Local Files")
	titleMain.TextStyle = fyne.TextStyle{Bold: true}
	titlePowered := widget.NewLabel(" powered by ")
	titlePowered.TextStyle = fyne.TextStyle{Italic: true}
	titleDuckDB := widget.NewLabel("DuckDB")
	titleDuckDB.TextStyle = fyne.TextStyle{Bold: true}
	titleLabel := container.NewHBox(titleMain, titlePowered, titleDuckDB)

	step4Label := widget.NewLabel("Query Results")
	step4Label.Alignment = fyne.TextAlignTrailing

	backBtn := widget.NewButton("Back", func() {
		dp.showJoinStep3FromResult(selected, selectedCols, aliases, query)
	})

	saveCSVBtn := widget.NewButton("Save to CSV", nil)
	saveJSONBtn := widget.NewButton("Save to JSON", nil)
	saveParquetBtn := widget.NewButton("Save to Parquet", nil)
	if len(rows) == 0 {
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		saveParquetBtn.Disable()
	}

	var saveCancelFn context.CancelFunc
	saveProgressLabel := widget.NewLabel("")
	cancelSaveBtn := widget.NewButton("Cancel", func() {
		if saveCancelFn != nil {
			saveCancelFn()
		}
	})
	cancelSaveBtn.Importance = widget.DangerImportance
	saveProgressRow := container.NewHBox(saveProgressLabel, cancelSaveBtn)
	saveProgressRow.Hide()

	saveFunc := func(format string) {
		workDir := dp.state.GetWorkingDirectory()
		if workDir == "" {
			dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Working directory not configured.")
			return
		}
		now := time.Now()
		filename := fmt.Sprintf("join_query-%s.%s", now.Format("20060102_1504"), format)
		dataDir := filepath.Join(workDir, "data")
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to create data directory :\n"+err.Error())
			return
		}
		filePath := filepath.Join(dataDir, filename)

		ctx, cancel := context.WithCancel(context.Background())
		saveCancelFn = cancel

		dp.HideSpecialOutput()
		dp.setStatus(fmt.Sprintf("Saving join query results to %s file ...", strings.ToUpper(format)))
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		saveParquetBtn.Disable()
		backBtn.Disable()
		saveProgressLabel.SetText(fmt.Sprintf("Saving to %s ...   ", strings.ToUpper(format)))
		cancelSaveBtn.Hide()
		saveProgressRow.Show()
		saveProgressRow.Refresh()

		go func() {
			defer func() {
				cancel()
				saveCancelFn = nil
				fyne.Do(func() {
					saveProgressRow.Hide()
					saveProgressRow.Refresh()
					saveCSVBtn.Enable()
					saveJSONBtn.Enable()
					saveParquetBtn.Enable()
					backBtn.Enable()
				})
			}()

			if format == "parquet" {
				var pWriter *parquet.Writer
				var pFile *os.File
				var sortedCols []string
				var goToParquet func(interface{}, int) parquet.Value
				var rowsWritten int
				batchCount := 0

				_, _, streamErr := executeDuckDBJoinQueryStreamed(aliases, query, 10000,
					func(c []string) {
						sortedCols = make([]string, len(c))
						copy(sortedCols, c)
						sort.Strings(sortedCols)
						// Deduplicate: join queries can return duplicate column names.
						seen := make(map[string]bool, len(sortedCols))
						deduped := sortedCols[:0]
						for _, col := range sortedCols {
							if !seen[col] {
								seen[col] = true
								deduped = append(deduped, col)
							}
						}
						sortedCols = deduped
					},
					func(batch []map[string]interface{}) error {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						if batchCount == 0 {
							inferNode := func(colName string) parquet.Node {
								for _, row := range batch {
									val := row[colName]
									if val == nil {
										continue
									}
									switch val.(type) {
									case bool:
										return parquet.Leaf(parquet.BooleanType)
									case int8, int16, int32, int:
										return parquet.Int(32)
									case int64:
										return parquet.Int(64)
									case float32:
										return parquet.Leaf(parquet.FloatType)
									case float64:
										return parquet.Leaf(parquet.DoubleType)
									case time.Time:
										return parquet.Timestamp(parquet.Millisecond)
									case []byte:
										return parquet.Leaf(parquet.ByteArrayType)
									default:
										return parquet.String()
									}
								}
								return parquet.String()
							}
							group := parquet.Group{}
							for _, col := range sortedCols {
								group[col] = parquet.Optional(inferNode(col))
							}
							schema := parquet.NewSchema("row", group)
							var openErr error
							pFile, openErr = os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
							if openErr != nil {
								return openErr
							}
							pWriter = parquet.NewWriter(pFile, schema)
							goToParquet = func(val interface{}, colIdx int) parquet.Value {
								if val == nil {
									return parquet.NullValue().Level(0, 0, colIdx)
								}
								const def = 1
								switch v := val.(type) {
								case bool:
									return parquet.BooleanValue(v).Level(0, def, colIdx)
								case int8:
									return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
								case int16:
									return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
								case int32:
									return parquet.Int32Value(v).Level(0, def, colIdx)
								case int:
									return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
								case int64:
									return parquet.Int64Value(v).Level(0, def, colIdx)
								case float32:
									return parquet.FloatValue(v).Level(0, def, colIdx)
								case float64:
									return parquet.DoubleValue(v).Level(0, def, colIdx)
								case time.Time:
									return parquet.Int64Value(v.UnixMilli()).Level(0, def, colIdx)
								case []byte:
									return parquet.ByteArrayValue(v).Level(0, def, colIdx)
								default:
									return parquet.ByteArrayValue([]byte(fmt.Sprintf("%v", v))).Level(0, def, colIdx)
								}
							}
						}
						for _, row := range batch {
							pRow := make(parquet.Row, len(sortedCols))
							for i, col := range sortedCols {
								pRow[i] = goToParquet(row[col], i)
							}
							if _, err := pWriter.WriteRows([]parquet.Row{pRow}); err != nil {
								return err
							}
						}
						batchCount++
						rowsWritten += len(batch)
						isFirstBatch := batchCount == 1
						fyne.Do(func() {
							if isFirstBatch {
								cancelSaveBtn.Show()
								cancelSaveBtn.Refresh()
							}
							saveProgressLabel.SetText(fmt.Sprintf("Saving to parquet ... %s rows written", commaInt(rowsWritten)))
							saveProgressLabel.Refresh()
						})
						return nil
					},
				)

				if pWriter != nil {
					if err := pWriter.Close(); err != nil && streamErr == nil {
						streamErr = err
					}
				}
				if pFile != nil {
					pFile.Close()
				}
				if errors.Is(streamErr, context.Canceled) {
					AppLog("parquet join sql query save to parquet file operation was cancelled by user")
					fyne.Do(func() {
						dp.setStatus("Save Parquet cancelled — partial file kept at : " + filePath)
					})
					return
				}
				if streamErr != nil {
					if pFile != nil {
						os.Remove(filePath)
					}
					fyne.Do(func() {
						dp.ShowSpecialOutputError("Save Parquet", "Failed to write file :\n"+streamErr.Error())
					})
					return
				}

				summaryPath := filePath + ".txt"
				summaryContent := fmt.Sprintf(`Date Time    : %s
File Format  : PARQUET
Row Count    : %d
Query        :
%s
`, now.Format("2006-01-02 15:04:05 MST"), rowsWritten, strings.TrimSpace(query))
				_ = os.WriteFile(summaryPath, []byte(summaryContent), 0600)
				AppLog("parquet sql join query results were saved to parquet file")
				fyne.Do(func() {
					dp.setStatus(fmt.Sprintf("Saved %s rows to parquet file : %s", commaInt(rowsWritten), filepath.Base(filePath)))
					dp.ShowSpecialOutputSuccess("Save Complete", fmt.Sprintf("Saved %s rows to :\n%s\n%s", commaInt(rowsWritten), filePath, summaryPath))
				})
				return
			}

			// CSV or JSON
			file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
			if err != nil {
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to create file :\n"+err.Error())
				})
				return
			}

			if format == "json" {
				file.WriteString("[\n")
			}

			var streamCols []string
			var csvWriter *csv.Writer
			var rowsWritten int
			firstJSONRow := true

			_, _, streamErr := executeDuckDBJoinQueryStreamed(aliases, query, 10000,
				func(c []string) {
					streamCols = c
					if format == "csv" {
						csvWriter = csv.NewWriter(file)
						_ = csvWriter.Write(c)
					}
				},
				func(batch []map[string]interface{}) error {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if format == "csv" {
						for _, row := range batch {
							record := make([]string, len(streamCols))
							for i, col := range streamCols {
								record[i] = fmt.Sprintf("%v", row[col])
							}
							csvWriter.Write(record)
						}
					} else {
						for _, row := range batch {
							if !firstJSONRow {
								file.WriteString(",\n")
							}
							data, _ := json.Marshal(row)
							file.WriteString("  ")
							file.Write(data)
							firstJSONRow = false
						}
					}
					isFirst := rowsWritten == 0
					rowsWritten += len(batch)
					fyne.Do(func() {
						if isFirst {
							cancelSaveBtn.Show()
							cancelSaveBtn.Refresh()
						}
						saveProgressLabel.SetText(fmt.Sprintf("Saving to %s ... %s rows written", strings.ToUpper(format), commaInt(rowsWritten)))
						saveProgressLabel.Refresh()
					})
					return nil
				},
			)

			if format == "csv" {
				if csvWriter != nil {
					csvWriter.Flush()
					if flushErr := csvWriter.Error(); flushErr != nil && streamErr == nil {
						streamErr = flushErr
					}
				}
			} else {
				file.WriteString("\n]\n")
			}
			file.Close()

			if errors.Is(streamErr, context.Canceled) {
				os.Remove(filePath)
				AppLog("parquet join sql query save to " + format + " operation was cancelled by user")
				fyne.Do(func() {
					dp.setStatus(fmt.Sprintf("Save %s cancelled.", strings.ToUpper(format)))
				})
				return
			}
			if streamErr != nil {
				os.Remove(filePath)
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to write file :\n"+streamErr.Error())
				})
				return
			}

			summaryPath := filePath + ".txt"
			summaryContent := fmt.Sprintf(`Date Time    : %s
File Format  : %s
Row Count    : %d
Query        :
%s
`, now.Format("2006-01-02 15:04:05 MST"), strings.ToUpper(format), rowsWritten, strings.TrimSpace(query))
			_ = os.WriteFile(summaryPath, []byte(summaryContent), 0600)
			AppLog("parquet sql join query results were saved to " + format + " file")
			fyne.Do(func() {
				dp.setStatus(fmt.Sprintf("Saved %s rows to %s file : %s", commaInt(rowsWritten), strings.ToUpper(format), filepath.Base(filePath)))
				dp.ShowSpecialOutputSuccess("Save Complete", fmt.Sprintf("Saved %s rows to :\n%s\n%s", commaInt(rowsWritten), filePath, summaryPath))
			})
		}()
	}

	saveCSVBtn.OnTapped = func() { saveFunc("csv") }
	saveJSONBtn.OnTapped = func() { saveFunc("json") }
	saveParquetBtn.OnTapped = func() { saveFunc("parquet") }

	noRowsText4 := dp.errorText("Query executed successfully. No rows returned.")
	noRowsText4.TextStyle = fyne.TextStyle{Bold: true}
	noRowsLabel := container.NewVBox(widget.NewLabel(""), widget.NewLabel(""), noRowsText4)

	var resultContent fyne.CanvasObject
	if len(rows) == 0 {
		noRowsLabel.Show()
		resultContent = container.NewStack()
		dp.setStatus(fmt.Sprintf("Query executed successfully. No rows returned. (%s)", perfInfo))
	} else {
		noRowsLabel.Hide()
		grid := NewDataGrid(cols, rows, rowsPerPage, dp.state)
		resultContent = grid.Container()
		if isPreview {
			dp.setStatus(fmt.Sprintf("Showing first %d rows. (%s)", previewRowLimit, perfInfo))
		} else {
			dp.setStatus(fmt.Sprintf("Query executed successfully. Returned %s row(s). (%s)", commaInt(len(rows)), perfInfo))
		}
	}

	var previewNoteRow fyne.CanvasObject
	if isPreview {
		noteLabel := widget.NewLabel(fmt.Sprintf("Showing first %d rows — use Save buttons to export all data.", previewRowLimit))
		noteLabel.TextStyle = fyne.TextStyle{Italic: true}
		previewNoteRow = container.NewPadded(container.NewHBox(noteLabel))
	} else {
		previewNoteRow = container.NewHBox()
	}

	topSection := container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, step4Label, func() fyne.CanvasObject {
			t := canvas.NewText("Query Results ...", theme.ForegroundColor())
			t.TextStyle = fyne.TextStyle{Italic: true}
			return t
		}()),
		widget.NewSeparator(),
		container.NewPadded(container.NewHBox(backBtn, saveCSVBtn, saveJSONBtn, saveParquetBtn)),
		noRowsLabel,
		previewNoteRow,
		saveProgressRow,
		widget.NewSeparator(),
	)

	dp.setContent(container.NewBorder(topSection, nil, nil, nil, resultContent))
}

// showJoinStep3FromResult rebuilds Step 3 with the previously entered query text.
func (dp *DetailPanel) showJoinStep3FromResult(selected map[string]string, selectedCols map[string]map[string]bool, aliases map[string]string, query string) {
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)

	colOrder := make(map[string][]string, len(names))
	for _, name := range names {
		preview, err := readParquetPreview(selected[name], 0)
		if err == nil {
			colOrder[name] = make([]string, len(preview.Columns))
			for j, col := range preview.Columns {
				colOrder[name][j] = col.Name
			}
		}
	}

	dp.showJoinStep3(names, selected, selectedCols, colOrder, query)
}

// ShowLocalFileView loads and displays a parquet file's schema and data preview.
func (dp *DetailPanel) ShowLocalFileView(path string) {
	filename := filepath.Base(path)
	titleLabel := widget.NewLabel(filename)
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}
	virtualTableLabel := widget.NewLabel(`(Parquet file available as "virtual_table")`)
	titleRow := container.NewHBox(titleLabel, virtualTableLabel)

	loadingLabel := widget.NewLabel("Loading file ...")

	dp.setContent(container.NewVBox(
		titleRow,
		widget.NewSeparator(),
		loadingLabel,
	))

	go func() {
		preview, err := readParquetPreview(path, 20)
		if err != nil {
			fyne.Do(func() {
				dp.setContent(container.NewVBox(
					titleRow,
					widget.NewSeparator(),
					widget.NewLabel("Error reading file : "+err.Error()),
				))
			})
			return
		}

		summaryTab := dp.createLocalFileSummaryTab(path, filename, preview)
		schemaTab := dp.createLocalFileSchemaTab(preview)
		dataTab := dp.createLocalFileDataTab(preview)
		queryTab := dp.createLocalFileQueryTab(path, preview.Columns)
		renameTab := dp.createLocalFileRenameTab(path)
		deleteTab := dp.createLocalFileDeleteTab(path, filename)

		sqlQueryMsg := "You can execute very complex SQL queries efficiently using DuckDB."

		tabs := container.NewAppTabs(
			container.NewTabItem("File Summary", summaryTab),
			container.NewTabItem("Schema", schemaTab),
			container.NewTabItem("Sample Data", dataTab),
			container.NewTabItem("SQL Query", queryTab),
			container.NewTabItem("Rename File", renameTab),
			container.NewTabItem("Delete File", deleteTab),
		)
		tabs.OnSelected = func(tab *container.TabItem) {
			dp.HideSpecialOutput()
			if tab.Text == "SQL Query" {
				dp.setStatus(sqlQueryMsg)
			}
		}

		fyne.Do(func() {
			dp.setContent(container.NewBorder(
				container.NewVBox(
					titleRow,
					widget.NewSeparator(),
				),
				nil, nil, nil,
				tabs,
			))
		})
	}()
}

// createLocalFileSummaryTab builds the File Summary tab for a local parquet file.
func (dp *DetailPanel) createLocalFileSummaryTab(path, filename string, preview ParquetPreview) fyne.CanvasObject {
	rows := []KeyValueRow{
		{"File Name", filename},
		{"File Size", formatBytes(uint64(preview.FileSize))},
		{"Total Rows", formatRowCount(preview.TotalRows)},
		{"Columns", fmt.Sprintf("%d", len(preview.Columns))},
	}
	if createdTime, ok := getFileCreatedTime(path); ok {
		rows = append(rows, KeyValueRow{"File Created", createdTime.Format("2006-01-02  15:04:05")})
	}
	rows = append(rows, KeyValueRow{"Last Modified", preview.ModTime.Format("2006-01-02  15:04:05")})

	return kvTableSized(rows, 200, 500, 40, dp.state)
}

// createLocalFileSchemaTab builds the Schema tab for a local parquet file preview.
func (dp *DetailPanel) createLocalFileSchemaTab(preview ParquetPreview) fyne.CanvasObject {
	if len(preview.Columns) == 0 {
		return widget.NewLabel("No columns found")
	}

	appFontName, appFontSize, appFontBold, appFontItalic := dp.state.GetAppFontSettings()
	if appFontSize <= 0 {
		appFontSize = 18
	}
	appStyle := fyne.TextStyle{
		Monospace: appFontName == "monospace",
		Bold:      appFontBold,
		Italic:    appFontItalic,
	}
	appStyleBold := fyne.TextStyle{
		Monospace: appFontName == "monospace",
		Bold:      true,
		Italic:    appFontItalic,
	}

	// Show Cassandra source type column only if at least one column has the metadata.
	hasCassType := false
	for _, c := range preview.Columns {
		if c.CassandraType != "" {
			hasCassType = true
			break
		}
	}
	hasTryCast := false
	for _, c := range preview.Columns {
		if strings.Contains(c.CassandraType, "TRY_CAST(") {
			hasTryCast = true
			break
		}
	}

	numCols := 2
	headers := []string{"Column Name", "Parquet Type"}
	if hasCassType {
		numCols = 3
		headers = []string{"Column Name", "Parquet Type", "Source Cassandra Type"}
	}
	if hasTryCast {
		numCols = 4
		headers = []string{"Column Name", "Parquet Type", "Source Cassandra Type", "Type Cast Suggestion"}
	}
	// Measure text width to auto-fit each column to its content.
	cellPad := theme.Padding() * 2
	measureWidth := func(s string, size float32, style fyne.TextStyle) float32 {
		t := canvas.NewText(s, theme.ForegroundColor())
		t.TextSize = size
		t.TextStyle = style
		return t.MinSize().Width
	}
	getCellText := func(col ParquetColumnInfo, colIdx int) string {
		switch colIdx {
		case 0:
			return col.Name
		case 1:
			return col.Type
		case 2:
			if idx := strings.Index(col.CassandraType, " : TRY_CAST("); idx != -1 {
				return col.CassandraType[:idx]
			}
			return col.CassandraType
		case 3:
			if idx := strings.Index(col.CassandraType, "TRY_CAST("); idx != -1 {
				return col.CassandraType[idx:]
			}
		}
		return ""
	}
	colWidths := make([]float32, numCols)
	for colIdx, header := range headers {
		colWidths[colIdx] = measureWidth(header, appFontSize, appStyleBold) + cellPad*2
	}
	for _, col := range preview.Columns {
		for colIdx := 0; colIdx < numCols; colIdx++ {
			w := measureWidth(getCellText(col, colIdx), appFontSize, appStyle) + cellPad*2
			if w > colWidths[colIdx] {
				colWidths[colIdx] = w
			}
		}
	}
	schemaTable := widget.NewTable(
		func() (int, int) { return len(preview.Columns) + 1, numCols },
		func() fyne.CanvasObject {
			text := canvas.NewText("Template", theme.ForegroundColor())
			text.TextSize = appFontSize
			text.TextStyle = appStyle
			return container.NewPadded(text)
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			text := obj.(*fyne.Container).Objects[0].(*canvas.Text)
			text.TextSize = appFontSize
			text.Color = theme.ForegroundColor()
			if id.Row == 0 {
				text.Text = headers[id.Col]
				text.TextStyle = appStyleBold
			} else {
				col := preview.Columns[id.Row-1]
				text.Text = getCellText(col, id.Col)
				text.TextStyle = appStyle
			}
			text.Refresh()
		},
	)
	for i := 0; i < numCols; i++ {
		schemaTable.SetColumnWidth(i, colWidths[i])
	}

	infoLabel := widget.NewLabel(fmt.Sprintf("%d columns", len(preview.Columns)))
	infoLabel.TextStyle = fyne.TextStyle{Italic: true}

	controlsRow := container.NewHBox(
		layout.NewSpacer(),
		infoLabel,
	)

	return container.NewBorder(controlsRow, nil, nil, nil, schemaTable)
}

// createLocalFileDataTab builds the Data Preview tab for a local parquet file preview.
func (dp *DetailPanel) createLocalFileDataTab(preview ParquetPreview) fyne.CanvasObject {
	if len(preview.Rows) == 0 {
		return widget.NewLabel("No data rows found")
	}

	gridFontName, gridFontSize, gridFontBold, gridFontItalic := dp.state.GetGridFontSettings()
	if gridFontSize <= 0 {
		gridFontSize = 14
	}
	appStyle := fyne.TextStyle{
		Monospace: gridFontName == "monospace",
		Bold:      gridFontBold,
		Italic:    gridFontItalic,
	}
	appStyleBold := fyne.TextStyle{
		Monospace: gridFontName == "monospace",
		Bold:      true,
		Italic:    gridFontItalic,
	}

	numCols := len(preview.Columns)

	// Auto-fit column widths to content.
	cellPad := theme.Padding() * 2
	measureWidth := func(s string, size float32, style fyne.TextStyle) float32 {
		t := canvas.NewText(s, theme.ForegroundColor())
		t.TextSize = size
		t.TextStyle = style
		return t.MinSize().Width
	}
	colWidths := make([]float32, numCols)
	for colIdx, col := range preview.Columns {
		colWidths[colIdx] = measureWidth(col.Name, gridFontSize, appStyleBold) + cellPad*2
	}
	for _, row := range preview.Rows {
		for colIdx := 0; colIdx < numCols && colIdx < len(row); colIdx++ {
			w := measureWidth(row[colIdx], gridFontSize, appStyle) + cellPad*2
			if w > colWidths[colIdx] {
				colWidths[colIdx] = w
			}
		}
	}
	const maxColWidth = float32(300)
	for i := range colWidths {
		if colWidths[i] > maxColWidth {
			colWidths[i] = maxColWidth
		}
	}

	truncateText := func(s string, maxWidth float32, size float32, style fyne.TextStyle) string {
		if measureWidth(s, size, style) <= maxWidth {
			return s
		}
		ellipsisW := measureWidth("...", size, style)
		available := maxWidth - ellipsisW
		runes := []rune(s)
		lo, hi := 0, len(runes)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if measureWidth(string(runes[:mid]), size, style) <= available {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		return string(runes[:lo]) + "..."
	}

	dataTable := widget.NewTable(
		func() (int, int) { return len(preview.Rows) + 1, numCols },
		func() fyne.CanvasObject {
			text := canvas.NewText("Template", theme.ForegroundColor())
			text.TextSize = gridFontSize
			text.TextStyle = appStyle
			return container.NewPadded(text)
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			text := obj.(*fyne.Container).Objects[0].(*canvas.Text)
			text.TextSize = gridFontSize
			text.Color = theme.ForegroundColor()
			available := colWidths[id.Col] - cellPad*2
			if id.Row == 0 {
				if id.Col < len(preview.Columns) {
					text.Text = truncateText(preview.Columns[id.Col].Name, available, gridFontSize, appStyleBold)
				} else {
					text.Text = ""
				}
				text.TextStyle = appStyleBold
			} else {
				row := preview.Rows[id.Row-1]
				if id.Col < len(row) {
					text.Text = truncateText(row[id.Col], available, gridFontSize, appStyle)
				} else {
					text.Text = ""
				}
				text.TextStyle = appStyle
			}
			text.Refresh()
		},
	)
	for i := 0; i < numCols; i++ {
		dataTable.SetColumnWidth(i, colWidths[i])
	}

	previewLabel := widget.NewLabel("Showing only 20 rows.")
	previewLabel.TextStyle = fyne.TextStyle{Italic: true}

	controlsRow := container.NewHBox(
		layout.NewSpacer(),
		previewLabel,
	)

	return container.NewBorder(controlsRow, nil, nil, nil, dataTable)
}

// createLocalFileQueryTab builds the SQL Query tab for running DuckDB queries against a parquet file.
func (dp *DetailPanel) createLocalFileQueryTab(parquetPath string, columns []ParquetColumnInfo) fyne.CanvasObject {
	colExprs := make([]string, len(columns))
	for i, c := range columns {
		if idx := strings.Index(c.CassandraType, "TRY_CAST("); idx != -1 {
			colExprs[i] = c.CassandraType[idx:] + " " + c.Name
		} else {
			colExprs[i] = c.Name
		}
	}
	defaultQuery := "SELECT " + strings.Join(colExprs, ", ") + "\nFROM virtual_table\nLIMIT 100\n;"

	queryEntry := newSafeMultiLineEntry()
	queryEntry.SetPlaceHolder("Enter SQL query here ...")
	queryEntry.SetText(defaultQuery)
	queryEntry.Wrapping = fyne.TextWrapWord
	queryEntry.SetMinRowsVisible(5)

	errorStyle := widget.RichTextStyle{
		ColorName: theme.ColorNameError,
		TextStyle: fyne.TextStyle{Bold: true},
		Inline:    false,
	}
	statusLabel := widget.NewRichText()
	statusLabel.Wrapping = fyne.TextWrapWord
	statusLabel.Hide()
	setQueryStatus := func(msg string) {
		if msg == "" {
			statusLabel.Segments = nil
			statusLabel.Refresh()
			statusLabel.Hide()
		} else {
			statusLabel.Segments = []widget.RichTextSegment{
				&widget.TextSegment{Text: msg, Style: errorStyle},
			}
			statusLabel.Refresh()
			statusLabel.Show()
		}
	}
	executingText := dp.successText("Executing query ...")
	executingLabel := container.NewVBox(widget.NewLabel(""), widget.NewLabel(""), executingText)
	executingLabel.Hide()
	noRowsText := dp.errorText("Query executed successfully. No rows returned.")
	noRowsText.TextStyle = fyne.TextStyle{Bold: true}
	noRowsLabel := container.NewVBox(widget.NewLabel(""), widget.NewLabel(""), noRowsText)
	noRowsLabel.Hide()
	resultContent := container.NewStack()

	var lastCols []string
	var lastQuery string

	saveCSVBtn := widget.NewButton("Save to CSV", nil)
	saveJSONBtn := widget.NewButton("Save to JSON", nil)
	saveParquetBtn := widget.NewButton("Save to Parquet", nil)
	saveCSVBtn.Disable()
	saveJSONBtn.Disable()
	saveParquetBtn.Disable()

	previewNoteLabel := widget.NewLabel("")
	previewNoteLabel.TextStyle = fyne.TextStyle{Italic: true}
	previewNoteLabel.Hide()

	var saveCancelFn context.CancelFunc
	saveProgressLabel := widget.NewLabel("")
	cancelSaveBtn := widget.NewButton("Cancel", func() {
		if saveCancelFn != nil {
			saveCancelFn()
		}
	})
	cancelSaveBtn.Importance = widget.DangerImportance
	saveProgressRow := container.NewHBox(saveProgressLabel, cancelSaveBtn)
	saveProgressRow.Hide()

	var executeBtn *widget.Button
	var clearBtn *widget.Button

	saveFunc := func(format string) {
		if lastQuery == "" {
			return
		}

		workDir := dp.state.GetWorkingDirectory()
		if workDir == "" {
			AppLog("parquet sql query results failed to save to " + format + " file")
			dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Working directory not configured.")
			return
		}

		base := strings.TrimSuffix(filepath.Base(parquetPath), filepath.Ext(parquetPath))
		tsRe := regexp.MustCompile(`[-_]\d{8}_\d{4}$`)
		baseName := tsRe.ReplaceAllString(base, "")
		if baseName == "" {
			baseName = base
		}
		for strings.HasPrefix(baseName, "cass-") {
			baseName = strings.TrimPrefix(baseName, "cass-")
		}
		for strings.HasPrefix(baseName, "join_query-") {
			baseName = strings.TrimPrefix(baseName, "join_query-")
		}
		for strings.HasPrefix(baseName, "duckDB-") {
			baseName = strings.TrimPrefix(baseName, "duckDB-")
		}

		now := time.Now()
		filename := fmt.Sprintf("duckDB-%s-%s.%s", sanitizeFilename(baseName), now.Format("20060102_1504"), format)
		dataDir := filepath.Join(workDir, "data")
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			AppLog("parquet sql query results failed to save to " + format + " file")
			dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to create data directory :\n"+err.Error())
			return
		}
		filePath := filepath.Join(dataDir, filename)

		capturedCols := lastCols
		capturedQuery := lastQuery

		ctx, cancel := context.WithCancel(context.Background())
		saveCancelFn = cancel

		dp.HideSpecialOutput()
		dp.setStatus(fmt.Sprintf("Saving SQL query results to %s file ...", strings.ToUpper(format)))
		executeBtn.Disable()
		clearBtn.Disable()
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		saveParquetBtn.Disable()
		saveProgressLabel.SetText(fmt.Sprintf("Saving to %s ...", strings.ToUpper(format)))
		cancelSaveBtn.Hide()
		saveProgressRow.Show()
		saveProgressRow.Refresh()

		go func() {
			defer func() {
				cancel()
				saveCancelFn = nil
				fyne.Do(func() {
					saveProgressRow.Hide()
					saveProgressRow.Refresh()
					executeBtn.Enable()
					clearBtn.Enable()
					saveCSVBtn.Enable()
					saveJSONBtn.Enable()
					saveParquetBtn.Enable()
				})
			}()

			file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
			if err != nil {
				AppLog("parquet sql query results failed to save to " + format + " file")
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to create file :\n"+err.Error())
				})
				return
			}

			var csvWriter *csv.Writer
			if format == "csv" {
				csvWriter = csv.NewWriter(file)
				_ = csvWriter.Write(capturedCols)
			} else {
				file.WriteString("[\n")
			}

			var rowsWritten int
			firstJSONRow := true
			_, _, streamErr := executeDuckDBQueryStreamed(parquetPath, capturedQuery, 10000,
				func(_ []string) {},
				func(batch []map[string]interface{}) error {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if format == "csv" {
						for _, row := range batch {
							record := make([]string, len(capturedCols))
							for i, col := range capturedCols {
								record[i] = fmt.Sprintf("%v", row[col])
							}
							csvWriter.Write(record)
						}
					} else {
						for _, row := range batch {
							if !firstJSONRow {
								file.WriteString(",\n")
							}
							data, _ := json.Marshal(row)
							file.WriteString("  ")
							file.Write(data)
							firstJSONRow = false
						}
					}
					isFirst := rowsWritten == 0
					rowsWritten += len(batch)
					fyne.Do(func() {
						if isFirst {
							cancelSaveBtn.Show()
							cancelSaveBtn.Refresh()
						}
						saveProgressLabel.SetText(fmt.Sprintf("Saving to %s ... %s rows written", strings.ToUpper(format), commaInt(rowsWritten)))
						saveProgressLabel.Refresh()
					})
					return nil
				},
			)

			if format == "csv" {
				csvWriter.Flush()
				if flushErr := csvWriter.Error(); flushErr != nil && streamErr == nil {
					streamErr = flushErr
				}
			} else {
				file.WriteString("\n]\n")
			}
			file.Close()

			if errors.Is(streamErr, context.Canceled) {
				os.Remove(filePath)
				AppLog("parquet sql query save to " + format + " operation was cancelled by user")
				fyne.Do(func() {
					dp.setStatus(fmt.Sprintf("Save %s cancelled.", strings.ToUpper(format)))
				})
				return
			}
			if streamErr != nil {
				os.Remove(filePath)
				AppLog("parquet sql query results failed to save to " + format + " file")
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to write file :\n"+streamErr.Error())
				})
				return
			}

			summaryPath := filePath + ".txt"
			summaryContent := fmt.Sprintf(`Date Time    : %s
File Format  : %s
Source File  : %s
Table Name   : %s
Row Count    : %d
Query        :
%s
`, now.Format("2006-01-02 15:04:05 MST"), strings.ToUpper(format), filepath.Base(parquetPath), baseName, rowsWritten, strings.TrimSpace(capturedQuery))
			if err := os.WriteFile(summaryPath, []byte(summaryContent), 0600); err != nil {
				AppLog("parquet sql query results failed to save to " + format + " file")
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save "+strings.ToUpper(format), "Failed to write summary :\n"+err.Error())
				})
				return
			}

			AppLog("parquet sql query results were saved to " + format + " file")
			fyne.Do(func() {
				setQueryStatus("")
				dp.setStatus(fmt.Sprintf("Saved %s rows to %s file : %s", commaInt(rowsWritten), strings.ToUpper(format), filepath.Base(filePath)))
				dp.ShowSpecialOutputSuccess("Save Complete", fmt.Sprintf("Saved %s rows to :\n%s\n%s", commaInt(rowsWritten), filePath, summaryPath))
			})
		}()
	}

	saveCSVBtn.OnTapped = func() { saveFunc("csv") }
	saveJSONBtn.OnTapped = func() { saveFunc("json") }

	saveParquetBtn.OnTapped = func() {
		if lastQuery == "" {
			return
		}

		workDir := dp.state.GetWorkingDirectory()
		if workDir == "" {
			AppLog("parquet sql query results failed to save to parquet file")
			dp.ShowSpecialOutputError("Save Parquet", "Working directory not configured.")
			return
		}

		base := strings.TrimSuffix(filepath.Base(parquetPath), filepath.Ext(parquetPath))
		tsRe := regexp.MustCompile(`[-_]\d{8}_\d{4}$`)
		baseName := tsRe.ReplaceAllString(base, "")
		if baseName == "" {
			baseName = base
		}
		for strings.HasPrefix(baseName, "cass-") {
			baseName = strings.TrimPrefix(baseName, "cass-")
		}
		for strings.HasPrefix(baseName, "join_query-") {
			baseName = strings.TrimPrefix(baseName, "join_query-")
		}
		for strings.HasPrefix(baseName, "duckDB-") {
			baseName = strings.TrimPrefix(baseName, "duckDB-")
		}

		now := time.Now()
		filename := fmt.Sprintf("duckDB-%s-%s.parquet", sanitizeFilename(baseName), now.Format("20060102_1504"))
		dataDir := filepath.Join(workDir, "data")
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			AppLog("parquet sql query results failed to save to parquet file")
			dp.ShowSpecialOutputError("Save Parquet", "Failed to create data directory :\n"+err.Error())
			return
		}
		filePath := filepath.Join(dataDir, filename)

		capturedQuery := lastQuery

		ctx, cancel := context.WithCancel(context.Background())
		saveCancelFn = cancel

		dp.HideSpecialOutput()
		dp.setStatus("Saving SQL query results to parquet file ...")
		executeBtn.Disable()
		clearBtn.Disable()
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		saveParquetBtn.Disable()
		saveProgressLabel.SetText("Saving to parquet ...")
		cancelSaveBtn.Hide()
		saveProgressRow.Show()
		saveProgressRow.Refresh()

		go func() {
			defer func() {
				cancel()
				saveCancelFn = nil
				fyne.Do(func() {
					saveProgressRow.Hide()
					saveProgressRow.Refresh()
					executeBtn.Enable()
					clearBtn.Enable()
					saveCSVBtn.Enable()
					saveJSONBtn.Enable()
					saveParquetBtn.Enable()
				})
			}()

			var pWriter *parquet.Writer
			var pFile *os.File
			var sortedCols []string
			var goToParquet func(interface{}, int) parquet.Value
			var rowsWritten int
			batchCount := 0

			_, _, streamErr := executeDuckDBQueryStreamed(parquetPath, capturedQuery, 10000,
				func(cols []string) {
					sortedCols = make([]string, len(cols))
					copy(sortedCols, cols)
					sort.Strings(sortedCols)
				},
				func(batch []map[string]interface{}) error {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if batchCount == 0 {
						inferNode := func(colName string) parquet.Node {
							for _, row := range batch {
								val := row[colName]
								if val == nil {
									continue
								}
								switch val.(type) {
								case bool:
									return parquet.Leaf(parquet.BooleanType)
								case int8, int16, int32, int:
									return parquet.Int(32)
								case int64:
									return parquet.Int(64)
								case float32:
									return parquet.Leaf(parquet.FloatType)
								case float64:
									return parquet.Leaf(parquet.DoubleType)
								case time.Time:
									return parquet.Timestamp(parquet.Millisecond)
								case []byte:
									return parquet.Leaf(parquet.ByteArrayType)
								default:
									return parquet.String()
								}
							}
							return parquet.String()
						}
						group := parquet.Group{}
						for _, col := range sortedCols {
							group[col] = parquet.Optional(inferNode(col))
						}
						schema := parquet.NewSchema("row", group)
						var openErr error
						pFile, openErr = os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
						if openErr != nil {
							return openErr
						}
						pWriter = parquet.NewWriter(pFile, schema)
						goToParquet = func(val interface{}, colIdx int) parquet.Value {
							if val == nil {
								return parquet.NullValue().Level(0, 0, colIdx)
							}
							const def = 1
							switch v := val.(type) {
							case bool:
								return parquet.BooleanValue(v).Level(0, def, colIdx)
							case int8:
								return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
							case int16:
								return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
							case int32:
								return parquet.Int32Value(v).Level(0, def, colIdx)
							case int:
								return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
							case int64:
								return parquet.Int64Value(v).Level(0, def, colIdx)
							case float32:
								return parquet.FloatValue(v).Level(0, def, colIdx)
							case float64:
								return parquet.DoubleValue(v).Level(0, def, colIdx)
							case time.Time:
								return parquet.Int64Value(v.UnixMilli()).Level(0, def, colIdx)
							case []byte:
								return parquet.ByteArrayValue(v).Level(0, def, colIdx)
							default:
								return parquet.ByteArrayValue([]byte(fmt.Sprintf("%v", v))).Level(0, def, colIdx)
							}
						}
					}
					for _, row := range batch {
						pRow := make(parquet.Row, len(sortedCols))
						for i, col := range sortedCols {
							pRow[i] = goToParquet(row[col], i)
						}
						if _, err := pWriter.WriteRows([]parquet.Row{pRow}); err != nil {
							return err
						}
					}
					batchCount++
					rowsWritten += len(batch)
					isFirstBatch := batchCount == 1
					fyne.Do(func() {
						if isFirstBatch {
							cancelSaveBtn.Show()
							cancelSaveBtn.Refresh()
						}
						saveProgressLabel.SetText(fmt.Sprintf("Saving to parquet ... %s rows written", commaInt(rowsWritten)))
						saveProgressLabel.Refresh()
					})
					return nil
				},
			)

			if pWriter != nil {
				if err := pWriter.Close(); err != nil && streamErr == nil {
					streamErr = err
				}
			}
			if pFile != nil {
				pFile.Close()
			}

			if errors.Is(streamErr, context.Canceled) {
				AppLog("parquet sql query save to parquet file operation was cancelled by user")
				fyne.Do(func() {
					dp.setStatus("Save Parquet cancelled — partial file kept at : " + filePath)
				})
				return
			}
			if streamErr != nil {
				if pFile != nil {
					os.Remove(filePath)
				}
				AppLog("parquet sql query results failed to save to parquet file")
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save Parquet", "Failed to write file :\n"+streamErr.Error())
				})
				return
			}

			summaryPath := filePath + ".txt"
			summaryContent := fmt.Sprintf(`Date Time    : %s
File Format  : PARQUET
Source File  : %s
Table Name   : %s
Row Count    : %d
Query        :
%s
`, now.Format("2006-01-02 15:04:05 MST"), filepath.Base(parquetPath), baseName, rowsWritten, strings.TrimSpace(capturedQuery))
			if err := os.WriteFile(summaryPath, []byte(summaryContent), 0600); err != nil {
				AppLog("parquet sql query results failed to save to parquet file")
				fyne.Do(func() {
					dp.ShowSpecialOutputError("Save Parquet", "Failed to write summary :\n"+err.Error())
				})
				return
			}

			AppLog("parquet sql query results were saved to parquet file")
			fyne.Do(func() {
				setQueryStatus("")
				dp.setStatus(fmt.Sprintf("Saved %s rows to parquet file : %s", commaInt(rowsWritten), filepath.Base(filePath)))
				dp.ShowSpecialOutputSuccess("Save Complete", fmt.Sprintf("Saved %s rows to :\n%s\n%s", commaInt(rowsWritten), filePath, summaryPath))
			})
		}()
	}

	executeQuery := func() {
		query := strings.TrimSpace(queryEntry.Text)
		if query == "" {
			setQueryStatus("Query cannot be empty.")
			return
		}

		if !isSelectOnlySQL(query) {
			setQueryStatus("")
			dp.setStatus("You can execute very complex SQL queries efficiently using DuckDB.")
			errText := dp.errorText("Currently this query/statement is not supported.")
			errText.TextStyle = fyne.TextStyle{Bold: true}
			resultContent.Objects = []fyne.CanvasObject{container.NewCenter(errText)}
			resultContent.Refresh()
			saveCSVBtn.Disable()
			saveJSONBtn.Disable()
			saveParquetBtn.Disable()
			return
		}

		setQueryStatus("")
		dp.setStatus("Executing query ...")
		dp.HideSpecialOutput()
		noRowsLabel.Hide()
		previewNoteLabel.SetText("")
		previewNoteLabel.Hide()
		resultContent.Objects = nil
		resultContent.Refresh()
		executingLabel.Show()
		executeBtn.Disable()
		clearBtn.Disable()
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		saveParquetBtn.Disable()

		capturedQuery := query
		go func() {
			cols, rows, stats, isPreview, err := executeDuckDBQueryPreview(parquetPath, capturedQuery)
			if err != nil {
				AppLog("sql query execution on a parquet file failed")
				fyne.Do(func() {
					executingLabel.Hide()
					executeBtn.Enable()
					clearBtn.Enable()
					noRowsLabel.Hide()
					setQueryStatus("ERROR : " + err.Error())
					dp.setStatus("Query failed.")
					saveCSVBtn.Disable()
					saveJSONBtn.Disable()
					saveParquetBtn.Disable()
				})
				return
			}
			perfInfo := fmt.Sprintf("Load : %s | Query : %s | Total : %s",
				formatQueryDuration(stats.LoadDuration),
				formatQueryDuration(stats.QueryDuration),
				formatQueryDuration(stats.LoadDuration+stats.QueryDuration),
			)
			if len(rows) == 0 {
				AppLog("executed sql query on a parquet file")
				fyne.Do(func() {
					executingLabel.Hide()
					executeBtn.Enable()
					clearBtn.Enable()
					setQueryStatus("")
					previewNoteLabel.SetText("")
					previewNoteLabel.Hide()
					previewNoteLabel.Refresh()
					noRowsLabel.Show()
					noRowsLabel.Refresh()
					resultContent.Objects = nil
					resultContent.Refresh()
					dp.setStatus(fmt.Sprintf("Query executed successfully. No rows returned. (%s)", perfInfo))
					saveCSVBtn.Disable()
					saveJSONBtn.Disable()
					saveParquetBtn.Disable()
				})
				return
			}

			lastCols = cols
			lastQuery = capturedQuery
			AppLog("executed sql query on a parquet file")
			grid := NewDataGrid(cols, rows, rowsPerPage, dp.state)
			fyne.Do(func() {
				executingLabel.Hide()
				executeBtn.Enable()
				clearBtn.Enable()
				noRowsLabel.Hide()
				setQueryStatus("")
				if isPreview {
					previewNoteLabel.SetText(fmt.Sprintf("Showing first %d rows — use Save buttons to export all data.", previewRowLimit))
					previewNoteLabel.Show()
					dp.setStatus(fmt.Sprintf("Showing first %d rows. (%s)", previewRowLimit, perfInfo))
				} else {
					previewNoteLabel.SetText("")
					previewNoteLabel.Hide()
					dp.setStatus(fmt.Sprintf("Query executed successfully. Returned %s row(s). (%s)", commaInt(len(rows)), perfInfo))
				}
				previewNoteLabel.Refresh()
				resultContent.Objects = []fyne.CanvasObject{grid.Container()}
				resultContent.Refresh()
				saveCSVBtn.Enable()
				saveJSONBtn.Enable()
				saveParquetBtn.Enable()
			})
		}()
	}

	executeBtn = widget.NewButton("Execute", executeQuery)
	executeBtn.Importance = widget.HighImportance

	clearBtn = widget.NewButton("Reset", func() {
		queryEntry.SetText(defaultQuery)
		setQueryStatus("")
		executingLabel.Hide()
		dp.setStatus("You can execute very complex SQL queries efficiently using DuckDB.")
		dp.HideSpecialOutput()
		resultContent.Objects = nil
		resultContent.Refresh()
		lastCols = nil
		lastQuery = ""
		noRowsLabel.Hide()
		previewNoteLabel.SetText("")
		previewNoteLabel.Hide()
		saveCSVBtn.Disable()
		saveJSONBtn.Disable()
		saveParquetBtn.Disable()
	})

	duckdbTextPart := canvas.NewText("SQL Query powered by ", nil)
	duckdbTextPart.TextStyle = fyne.TextStyle{Italic: true}
	duckdbBoldPart := canvas.NewText("DuckDB", nil)
	duckdbBoldPart.TextStyle = fyne.TextStyle{Bold: true}
	buttonRow := container.NewHBox(executeBtn, clearBtn, saveCSVBtn, saveJSONBtn, saveParquetBtn, widget.NewLabel("  "), duckdbTextPart, duckdbBoldPart)

	return container.NewBorder(
		container.NewVBox(queryEntry, buttonRow, statusLabel, executingLabel, noRowsLabel, previewNoteLabel, saveProgressRow),
		nil, nil, nil,
		resultContent,
	)
}

// createLocalFileRenameTab builds the Rename File tab for a local parquet file.
func (dp *DetailPanel) createLocalFileRenameTab(path string) fyne.CanvasObject {
	dir := filepath.Dir(path)
	currentFilename := filepath.Base(path)
	currentBaseName := strings.TrimSuffix(currentFilename, ".parquet")

	currentNameKey := widget.NewLabel("Current Name : ")
	currentNameVal := widget.NewLabel(currentBaseName)
	currentNameVal.TextStyle = fyne.TextStyle{Bold: true}
	currentNameLabel := container.NewHBox(currentNameKey, currentNameVal)

	nameEntry := newFocusEntry()
	nameEntry.SetText(currentBaseName)
	nameEntry.SetPlaceHolder("New file name (without extension)")

	errorText := dp.errorText("")
	errorText.TextStyle = fyne.TextStyle{Bold: true, Italic: true}

	showError := func(msg string) {
		errorText.Text = msg
		errorText.Color = theme.ErrorColor()
		errorText.Refresh()
	}
	clearError := func() {
		errorText.Text = ""
		errorText.Refresh()
	}

	validate := func(name string) string {
		if strings.TrimSpace(name) == "" {
			return "x  File name cannot be empty"
		}
		for _, ch := range name {
			if ch < 32 || strings.ContainsRune(`/\:*?"<>|`, ch) {
				return "x  File name contains invalid characters"
			}
		}
		return ""
	}

	nameEntry.OnChanged = func(s string) {
		if msg := validate(strings.TrimSpace(s)); msg != "" {
			showError(msg)
		} else {
			clearError()
		}
	}

	nameEntry.onFocusLost = func() {
		newBaseName := strings.TrimSpace(nameEntry.Text)
		if validate(newBaseName) != "" {
			return
		}
		newFilename := newBaseName + ".parquet"
		if newFilename == currentFilename {
			return
		}
		newPath := filepath.Join(dir, newFilename)
		if _, err := os.Stat(newPath); err == nil {
			showError(fmt.Sprintf("x  File %q already exists", newFilename))
		}
	}

	renameBtn := widget.NewButton("Rename", func() {
		newBaseName := strings.TrimSpace(nameEntry.Text)
		if msg := validate(newBaseName); msg != "" {
			showError(msg)
			return
		}
		newFilename := newBaseName + ".parquet"
		if newFilename == currentFilename {
			showError("x  New name is the same as current name")
			return
		}
		newPath := filepath.Join(dir, newFilename)
		if _, err := os.Stat(newPath); err == nil {
			showError(fmt.Sprintf("x  File %q already exists", newFilename))
			return
		}
		if err := os.Rename(path, newPath); err != nil {
			showError("x  Rename failed: " + err.Error())
			return
		}
		AppLog("parquet file was renamed")
		if dp.setStatus != nil {
			dp.setStatus("File renamed : " + newFilename)
		}
		if dp.rescanLocalFiles != nil {
			dp.rescanLocalFiles()
		}
		dp.ShowLocalFileView(newPath)
		if dp.selectTreeLocalFile != nil {
			go func() {
				time.Sleep(150 * time.Millisecond)
				fyne.Do(func() { dp.selectTreeLocalFile(newFilename) })
			}()
		}
	})
	renameBtn.Importance = widget.HighImportance

	spacer := func() fyne.CanvasObject { return widget.NewLabel("") }

	return container.NewVBox(
		spacer(),
		spacer(),
		currentNameLabel,
		widget.NewLabel("New Name :"),
		container.NewHBox(constrainFormEntry(nameEntry), renameBtn),
		spacer(),
		errorText,
	)
}

// createLocalFileDeleteTab builds the Delete File tab for a local parquet file.
func (dp *DetailPanel) createLocalFileDeleteTab(path, filename string) fyne.CanvasObject {
	spacer := func() fyne.CanvasObject { return widget.NewLabel("") }

	// ── Initial screen ────────────────────────────────────────────────────────
	fileLabel := widget.NewLabel("File : " + filename)
	fileLabel.TextStyle = fyne.TextStyle{Bold: true}

	deleteBtn := widget.NewButton("Delete File", nil)
	deleteBtn.Importance = widget.DangerImportance

	// ── Confirmation screen ───────────────────────────────────────────────────
	confirmMsg := widget.NewLabel("Are you sure you want to permanently delete this file ?")
	confirmMsg.TextStyle = fyne.TextStyle{Bold: true}

	filenameConfirm := widget.NewLabel(filename)
	filenameConfirm.TextStyle = fyne.TextStyle{Bold: true, Italic: true}

	confirmNote := widget.NewLabel("This action cannot be undone.")

	confirmBtn := widget.NewButton("Yes, Delete File", nil)
	confirmBtn.Importance = widget.DangerImportance

	cancelBtn := widget.NewButton("Cancel", nil)

	confirmSection := container.NewVBox(
		spacer(),
		confirmMsg,
		filenameConfirm,
		confirmNote,
		spacer(),
		container.NewHBox(confirmBtn, cancelBtn),
	)
	confirmSection.Hide()

	// ── Wire up buttons ───────────────────────────────────────────────────────
	deleteBtn.OnTapped = func() {
		deleteBtn.Disable()
		confirmSection.Show()
		confirmSection.Refresh()
	}

	cancelBtn.OnTapped = func() {
		confirmSection.Hide()
		confirmSection.Refresh()
		deleteBtn.Enable()
	}

	confirmBtn.OnTapped = func() {
		if err := os.Remove(path); err != nil {
			confirmSection.Hide()
			confirmSection.Refresh()
			deleteBtn.Enable()
			dp.setStatus("Delete failed : " + err.Error())
			return
		}
		AppLog("parquet file was deleted")
		dp.setStatus("File deleted : " + filename)
		if dp.rescanLocalFiles != nil {
			dp.rescanLocalFiles()
		}
		dp.ShowAbout()
	}

	return container.NewVBox(
		spacer(),
		spacer(),
		fileLabel,
		spacer(),
		container.NewHBox(deleteBtn),
		confirmSection,
	)
}

// ShowAppStats displays the App Stats panel with three time-series charts.
func (dp *DetailPanel) ShowAppStats() {
	title := widget.NewLabel("App Stats")
	title.TextStyle = fyne.TextStyle{Bold: true}
	now := time.Now()
	dateLabel := widget.NewLabel(now.AddDate(0, 0, -1).Format("02-Jan-2006") + " – " + now.Format("02-Jan-2006"))
	dateLabel.TextStyle = fyne.TextStyle{Italic: true}

	workDir := dp.state.GetWorkingDirectory()
	var sd statsData
	if workDir != "" {
		sd = loadRecentStats(workDir)
	}

	amChart := NewLineChartWidget("App Memory", "bytes", color.RGBA{R: 76, G: 175, B: 80, A: 255})
	amChart.SetPoints(sd.am)

	acChart := NewLineChartWidget("App CPU %", "%", color.RGBA{R: 33, G: 150, B: 243, A: 255})
	acChart.SetPoints(sd.ac)

	dmChart := NewLineChartWidget("DuckDB Memory", "bytes", color.RGBA{R: 255, G: 152, B: 0, A: 255})
	dmChart.SetPoints(sd.dm)

	dp.setContent(container.NewVBox(
		container.NewHBox(title, dateLabel),
		widget.NewSeparator(),
		acChart,
		widget.NewSeparator(),
		amChart,
		widget.NewSeparator(),
		dmChart,
	))
}

// goBack returns to the appropriate previous panel
func (dp *DetailPanel) goBack() {
	conn := dp.state.GetSelectedConnection()
	if conn != nil {
		dp.ShowConnectionDetails(conn)
	} else {
		dp.ShowAbout()
	}
}
