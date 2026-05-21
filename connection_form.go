package main

import (
	"fmt"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// formEntryWidth returns the standard max width for form text entries.
// Adjust this value to change all connection form entry widths at once.
func formEntryWidth() float32 {
	return 600
}

// constrainFormEntry wraps an entry so it renders at formEntryWidth() regardless
// of how wide the right panel grows. The trailing spacer absorbs leftover space.
func constrainFormEntry(e fyne.CanvasObject) fyne.CanvasObject {
	sized := container.NewGridWrap(fyne.NewSize(formEntryWidth(), e.MinSize().Height), e)
	return container.NewHBox(sized, layout.NewSpacer())
}

// focusEntry is a widget.Entry that fires onFocusLost when the field loses focus.
type focusEntry struct {
	widget.Entry
	onFocusLost func()
}

func newFocusEntry() *focusEntry {
	e := &focusEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *focusEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.onFocusLost != nil {
		e.onFocusLost()
	}
}

// ConnectionForm represents the add/edit connection form
type ConnectionForm struct {
	editMode     bool
	originalConn *DecryptedConnection
	window       fyne.Window
	state        *AppState
	onSave       func(DecryptedConnection)
	onCancel     func()
	setStatus    func(string)
	container    *fyne.Container

	// Form fields
	nameEntry           *focusEntry
	envEntry            *focusEntry
	ipEntry             *focusEntry
	portEntry           *focusEntry
	usernameEntry       *widget.Entry
	passwordEntry       *widget.Entry
	errorLabel          *canvas.Text
	testResultContainer *fyne.Container
	appFontSize         float32
}

// NewConnectionForm creates a new connection form
func NewConnectionForm(existing *DecryptedConnection, window fyne.Window, state *AppState, onSave func(DecryptedConnection), onCancel func(), setStatus func(string)) *ConnectionForm {
	cf := &ConnectionForm{
		editMode:     existing != nil,
		originalConn: existing,
		window:       window,
		state:        state,
		onSave:       onSave,
		onCancel:     onCancel,
		setStatus:    setStatus,
	}

	cf.createForm()
	return cf
}

// createForm builds the form UI
func (cf *ConnectionForm) createForm() {
	// Title
	title := "Add New Connection"
	if cf.editMode {
		title = "Edit Connection"
	}
	titleLabel := widget.NewLabel(title)
	titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Connection Name
	cf.nameEntry = newFocusEntry()
	cf.nameEntry.SetPlaceHolder("My Connection")
	cf.nameEntry.Validator = cf.validateName
	cf.nameEntry.onFocusLost = func() {
		if err := cf.validateName(cf.nameEntry.Text); err != nil {
			cf.setError("Connection Name : " + err.Error())
		} else {
			cf.clearError()
		}
	}
	nameItem := widget.NewFormItem("Connection Name *", constrainFormEntry(cf.nameEntry))

	// Environment
	cf.envEntry = newFocusEntry()
	cf.envEntry.SetPlaceHolder("PRODUCTION")
	cf.envEntry.Validator = cf.validateEnvironment
	cf.envEntry.OnChanged = func(s string) {
		cf.envEntry.SetText(strings.ToUpper(s))
	}
	cf.envEntry.onFocusLost = func() {
		if err := cf.validateEnvironment(cf.envEntry.Text); err != nil {
			cf.setError("Environment : " + err.Error())
		} else {
			cf.clearError()
		}
	}
	envItem := widget.NewFormItem("Environment *", constrainFormEntry(cf.envEntry))

	// IP Addresses
	cf.ipEntry = newFocusEntry()
	cf.ipEntry.SetPlaceHolder("192.168.1.1,192.168.1.2")
	cf.ipEntry.Validator = cf.validateIP
	cf.ipEntry.onFocusLost = func() {
		if err := cf.validateIP(cf.ipEntry.Text); err != nil {
			cf.setError("IP Addresses : " + err.Error())
		} else {
			cf.clearError()
		}
	}
	ipItem := widget.NewFormItem("IP Addresses *", constrainFormEntry(cf.ipEntry))

	// Port
	cf.portEntry = newFocusEntry()
	cf.portEntry.SetPlaceHolder("9042")
	cf.portEntry.SetText("9042")
	cf.portEntry.Validator = cf.validatePort
	cf.portEntry.onFocusLost = func() {
		if err := cf.validatePort(cf.portEntry.Text); err != nil {
			cf.setError("Port : " + err.Error())
		} else {
			cf.clearError()
		}
	}
	portItem := widget.NewFormItem("Port *", constrainFormEntry(cf.portEntry))

	// Username
	cf.usernameEntry = widget.NewEntry()
	cf.usernameEntry.SetPlaceHolder("cassandra")
	usernameItem := widget.NewFormItem("Username", constrainFormEntry(cf.usernameEntry))

	// Password
	cf.passwordEntry = widget.NewPasswordEntry()
	cf.passwordEntry.SetPlaceHolder("password")
	passwordItem := widget.NewFormItem("Password", constrainFormEntry(cf.passwordEntry))

	// Pre-populate if editing
	if cf.editMode && cf.originalConn != nil {
		cf.nameEntry.SetText(cf.originalConn.ConnName)
		cf.envEntry.SetText(cf.originalConn.Environment)
		cf.ipEntry.SetText(cf.originalConn.IPAddresses)
		cf.portEntry.SetText(cf.originalConn.Port)
		cf.usernameEntry.SetText(cf.originalConn.Username)
		cf.passwordEntry.SetText(cf.originalConn.Password)
	}

	// Create form
	form := widget.NewForm(
		nameItem,
		envItem,
		ipItem,
		portItem,
		usernameItem,
		passwordItem,
	)

	// Buttons
	testBtn := widget.NewButton("Test Connection", cf.testConnection)

	saveBtn := widget.NewButton("Save", cf.save)
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", cf.cancel)

	buttonBox := container.NewHBox(testBtn, saveBtn, cancelBtn)

	// Inline test result
	_, cf.appFontSize, _, _ = cf.state.GetAppFontSettings()
	cf.testResultContainer = container.NewVBox()
	cf.errorLabel = newErrorText("", cf.appFontSize)

	// Assemble form
	cf.container = container.NewVBox(
		titleLabel,
		widget.NewSeparator(),
		form,
		widget.NewSeparator(),
		cf.errorLabel,
		buttonBox,
		cf.testResultContainer,
	)
}

// Container returns the form container
func (cf *ConnectionForm) Container() *fyne.Container {
	return cf.container
}

// validateName validates the connection name
func (cf *ConnectionForm) validateName(s string) error {
	if len(s) < 5 {
		return fmt.Errorf("must be at least 5 characters")
	}
	if len(s) > 20 {
		return fmt.Errorf("must be at most 20 characters")
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, s)
	if !matched {
		return fmt.Errorf("only alphanumeric, underscore, hyphen allowed")
	}
	return nil
}

// validateEnvironment validates the environment field
func (cf *ConnectionForm) validateEnvironment(s string) error {
	if len(s) < 3 {
		return fmt.Errorf("must be at least 3 characters")
	}
	if len(s) > 20 {
		return fmt.Errorf("must be at most 20 characters")
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, s)
	if !matched {
		return fmt.Errorf("only alphanumeric, underscore, hyphen allowed")
	}
	return nil
}

// validateIP validates the IP addresses field
func (cf *ConnectionForm) validateIP(s string) error {
	if len(s) < 3 {
		return fmt.Errorf("must be at least 3 characters")
	}
	if len(s) > 100 {
		return fmt.Errorf("must be at most 100 characters")
	}
	return nil
}

// validatePort validates the port field
func (cf *ConnectionForm) validatePort(s string) error {
	if len(s) < 1 {
		return fmt.Errorf("port is required")
	}
	matched, _ := regexp.MatchString(`^[0-9]+$`, s)
	if !matched {
		return fmt.Errorf("must be a number")
	}
	return nil
}

// setError displays an error message
func (cf *ConnectionForm) setError(msg string) {
	cf.errorLabel.Text = msg
	cf.errorLabel.Refresh()
	cf.setStatus(msg)
}

// clearError clears the error message
func (cf *ConnectionForm) clearError() {
	cf.errorLabel.Text = ""
	cf.errorLabel.Refresh()
}

// validateAll performs full form validation
func (cf *ConnectionForm) validateAll() error {
	if err := cf.validateName(cf.nameEntry.Text); err != nil {
		return fmt.Errorf("Connection Name: %v", err)
	}
	if err := cf.validateEnvironment(cf.envEntry.Text); err != nil {
		return fmt.Errorf("Environment: %v", err)
	}
	if err := cf.validateIP(cf.ipEntry.Text); err != nil {
		return fmt.Errorf("IP Addresses: %v", err)
	}
	if err := cf.validatePort(cf.portEntry.Text); err != nil {
		return fmt.Errorf("Port: %v", err)
	}

	// Optional field validations
	if cf.usernameEntry.Text != "" && len(cf.usernameEntry.Text) < 3 {
		return fmt.Errorf("Username must be at least 3 characters")
	}
	if cf.passwordEntry.Text != "" && len(cf.passwordEntry.Text) < 3 {
		return fmt.Errorf("Password must be at least 3 characters")
	}

	return nil
}

// buildConnection creates a DecryptedConnection from form data
func (cf *ConnectionForm) buildConnection() DecryptedConnection {
	return DecryptedConnection{
		ConnName:       cf.nameEntry.Text,
		Environment:    cf.envEntry.Text,
		IPAddresses:    cf.ipEntry.Text,
		Port:           cf.portEntry.Text,
		Username:       cf.usernameEntry.Text,
		Password:       cf.passwordEntry.Text,
		PromptUsername: false,
		PromptPassword: false,
	}
}

// testConnection tests the connection with current form values
func (cf *ConnectionForm) testConnection() {
	cf.clearError()

	if err := cf.validateAll(); err != nil {
		cf.setError(err.Error())
		return
	}

	conn := cf.buildConnection()

	// Use entered credentials for test even if prompting is enabled
	conn.Username = cf.usernameEntry.Text
	conn.Password = cf.passwordEntry.Text

	runTestConnection(conn, cf.testResultContainer, cf.state, cf.setStatus)
}

// save validates and saves the connection
func (cf *ConnectionForm) save() {
	cf.clearError()

	if err := cf.validateAll(); err != nil {
		cf.setError(err.Error())
		return
	}

	conn := cf.buildConnection()

	if cf.onSave != nil {
		cf.onSave(conn)
	}
}

// cancel closes the form without saving
func (cf *ConnectionForm) cancel() {
	if cf.onCancel != nil {
		cf.onCancel()
	}
}
