package main

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/gocql/gocql"
)

// ConnectionTree represents the left panel connection tree
type ConnectionTree struct {
	state                *AppState
	tree                 *widget.Tree
	container            *fyne.Container
	onSelect             func(*DecryptedConnection)
	onAddConnection      func()
	onDisconnectAll      func()
	onTableSelected      func(conn *DecryptedConnection, keyspace, table string)
	onConnectionExpanded func(conn *DecryptedConnection, session *gocql.Session)
	onKeyspaceSelected   func(conn *DecryptedConnection, keyspace string, session *gocql.Session)
	environmentOrder     []string
	connectionsByEnv     map[string][]DecryptedConnection

	// Connection sessions and data
	mu              sync.RWMutex
	sessions        map[string]*gocql.Session     // connName -> session
	keyspaces       map[string][]string           // connName -> keyspace names
	tables          map[string][]string           // connName:keyspace -> table names
	expandedConns   map[string]bool               // connName -> expanded
	expandedKS      map[string]bool               // connName:keyspace -> expanded
	connectingConns map[string]bool               // connName -> currently connecting
	loadingKS       map[string]bool               // connName:keyspace -> currently loading tables
	connCancels     map[string]context.CancelFunc // connName -> cancel for in-flight connect
	localCancel     context.CancelFunc            // cancel for in-flight local-file scan
	refreshTimer    *time.Timer
	refreshMu       sync.Mutex

	// Local file browser
	localFiles           []string // sorted parquet filenames
	localFilesLoaded     bool
	onLocalFileSelected  func(string)
	onJoinLocalSelected  func()
	onBrowseLocalNoFiles func()
	onAppStatsSelected   func()

	onConnectionError    func(*DecryptedConnection, error)
	onEnvironmentToggled func()
	selectedConnName     string // tracks which conn cell should show a selection background
}

// Tree node IDs
const (
	rootID          = ""
	addConnectionID = "add_connection"
	disconnectAllID = "disconnect_all"
	browseLocalID   = "browse_local"
	joinLocalID     = "join_local"
	appStatsID      = "app_stats"
)

// NewConnectionTree creates a new connection tree widget
func NewConnectionTree(state *AppState, onSelect func(*DecryptedConnection), onAddConnection func(), onDisconnectAll func()) *ConnectionTree {
	ct := &ConnectionTree{
		state:           state,
		onSelect:        onSelect,
		onAddConnection: onAddConnection,
		onDisconnectAll: onDisconnectAll,
		sessions:        make(map[string]*gocql.Session),
		keyspaces:       make(map[string][]string),
		tables:          make(map[string][]string),
		expandedConns:   make(map[string]bool),
		expandedKS:      make(map[string]bool),
		connectingConns: make(map[string]bool),
		loadingKS:       make(map[string]bool),
		connCancels:     make(map[string]context.CancelFunc),
		localCancel:     func() {},
		localFiles:      []string{},
	}

	ct.buildConnectionMap()
	ct.createTree()

	// Wrap tree in a scroll container
	ct.container = container.NewStack(
		container.NewVScroll(ct.tree),
	)

	return ct
}

// SetOnTableSelected sets the callback for table selection
func (ct *ConnectionTree) SetOnTableSelected(callback func(conn *DecryptedConnection, keyspace, table string)) {
	ct.onTableSelected = callback
}

// SetOnConnectionExpanded sets the callback for when a connection is expanded (connected)
func (ct *ConnectionTree) SetOnConnectionExpanded(callback func(conn *DecryptedConnection, session *gocql.Session)) {
	ct.onConnectionExpanded = callback
}

// SetOnKeyspaceSelected sets the callback for keyspace selection
func (ct *ConnectionTree) SetOnKeyspaceSelected(callback func(conn *DecryptedConnection, keyspace string, session *gocql.Session)) {
	ct.onKeyspaceSelected = callback
}

// SetOnLocalFileSelected sets the callback for local parquet file selection
func (ct *ConnectionTree) SetOnLocalFileSelected(callback func(path string)) {
	ct.onLocalFileSelected = callback
}

// SetOnJoinLocalSelected sets the callback for when the JOIN Data In Local Files node is clicked
func (ct *ConnectionTree) SetOnJoinLocalSelected(callback func()) {
	ct.onJoinLocalSelected = callback
}

// SetOnBrowseLocalNoFiles sets the callback fired when Data In Local Files is opened but no parquet files exist
func (ct *ConnectionTree) SetOnBrowseLocalNoFiles(callback func()) {
	ct.onBrowseLocalNoFiles = callback
}

// SetOnAppStatsSelected sets the callback for when the App Stats node is clicked
func (ct *ConnectionTree) SetOnAppStatsSelected(callback func()) {
	ct.onAppStatsSelected = callback
}

// SetOnConnectionError sets the callback for connection errors during expansion
func (ct *ConnectionTree) SetOnConnectionError(callback func(conn *DecryptedConnection, err error)) {
	ct.onConnectionError = callback
}

// SetOnEnvironmentToggled sets the callback fired when an environment branch is expanded or collapsed.
func (ct *ConnectionTree) SetOnEnvironmentToggled(callback func()) {
	ct.onEnvironmentToggled = callback
}

// buildConnectionMap organizes connections by environment
func (ct *ConnectionTree) buildConnectionMap() {
	connections := ct.state.GetConnections()

	ct.connectionsByEnv = make(map[string][]DecryptedConnection)
	envSet := make(map[string]bool)

	for _, conn := range connections {
		ct.connectionsByEnv[conn.Environment] = append(ct.connectionsByEnv[conn.Environment], conn)
		envSet[conn.Environment] = true
	}

	// Get sorted environment names
	ct.environmentOrder = make([]string, 0, len(envSet))
	for env := range envSet {
		ct.environmentOrder = append(ct.environmentOrder, env)
	}
	sort.Strings(ct.environmentOrder)
}

// createTree initializes the tree widget
func (ct *ConnectionTree) createTree() {
	ct.tree = widget.NewTree(
		ct.childUIDs,
		ct.isBranch,
		ct.createNode,
		ct.updateNode,
	)

	ct.tree.OnSelected = ct.onNodeSelected
	ct.tree.OnBranchOpened = ct.onBranchOpened
	ct.tree.OnBranchClosed = ct.onBranchClosed
}

// childUIDs returns the child UIDs for a given tree node
func (ct *ConnectionTree) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	if uid == rootID {
		// Root level: "Add Connection" + "Disconnect All" + environments + "Data In Local Files"
		children := []widget.TreeNodeID{addConnectionID, disconnectAllID}
		for _, env := range ct.environmentOrder {
			children = append(children, "env:"+env)
		}
		children = append(children, browseLocalID)
		children = append(children, joinLocalID)
		if ct.state.GetCaptureAppStats() {
			children = append(children, appStatsID)
		}
		return children
	}

	// Data In Local Files -> flat list of parquet files
	if uid == browseLocalID {
		ct.mu.RLock()
		files := ct.localFiles
		ct.mu.RUnlock()
		children := make([]widget.TreeNodeID, 0, len(files))
		for _, f := range files {
			children = append(children, "lf_file:"+f)
		}
		return children
	}

	// Environment node -> connections
	if strings.HasPrefix(uid, "env:") {
		env := strings.TrimPrefix(uid, "env:")
		connections := ct.connectionsByEnv[env]
		children := make([]widget.TreeNodeID, 0, len(connections))
		for _, conn := range connections {
			children = append(children, "conn:"+conn.ConnName)
		}
		return children
	}

	// Connection node -> keyspaces
	if strings.HasPrefix(uid, "conn:") {
		connName := strings.TrimPrefix(uid, "conn:")
		ct.mu.RLock()
		ks := ct.keyspaces[connName]
		ct.mu.RUnlock()
		children := make([]widget.TreeNodeID, 0, len(ks))
		for _, k := range ks {
			children = append(children, "ks:"+connName+":"+k)
		}
		return children
	}

	// Keyspace node -> tables
	if strings.HasPrefix(uid, "ks:") {
		parts := strings.SplitN(strings.TrimPrefix(uid, "ks:"), ":", 2)
		if len(parts) == 2 {
			connName := parts[0]
			keyspace := parts[1]
			ct.mu.RLock()
			tbl := ct.tables[connName+":"+keyspace]
			ct.mu.RUnlock()
			children := make([]widget.TreeNodeID, 0, len(tbl))
			for _, t := range tbl {
				children = append(children, "tbl:"+connName+":"+keyspace+":"+t)
			}
			return children
		}
	}

	return nil
}

// isBranch returns whether a node is a branch (has children)
func (ct *ConnectionTree) isBranch(uid widget.TreeNodeID) bool {
	if uid == rootID {
		return true
	}
	if uid == addConnectionID {
		return false
	}
	if uid == disconnectAllID {
		return false
	}
	if uid == browseLocalID {
		ct.mu.RLock()
		loaded := ct.localFilesLoaded
		count := len(ct.localFiles)
		ct.mu.RUnlock()
		return !loaded || count > 0
	}
	if uid == joinLocalID {
		return false
	}
	if uid == appStatsID {
		return false
	}
	if strings.HasPrefix(uid, "env:") {
		return true
	}
	if strings.HasPrefix(uid, "conn:") {
		return true
	}
	if strings.HasPrefix(uid, "ks:") {
		return true
	}
	if strings.HasPrefix(uid, "tbl:") {
		return false
	}
	if strings.HasPrefix(uid, "lf_file:") {
		return false
	}
	return false
}

// createNode creates a new tree node widget
func (ct *ConnectionTree) createNode(branch bool) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.Transparent)
	return container.NewStack(
		bg,
		container.NewHBox(
			widget.NewIcon(theme.FolderIcon()),
			widget.NewLabel("Template"),
		),
	)
}

// updateNode updates a tree node with actual data
func (ct *ConnectionTree) updateNode(uid widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
	stack := obj.(*fyne.Container)
	bg := stack.Objects[0].(*canvas.Rectangle)
	box := stack.Objects[1].(*fyne.Container)
	icon := box.Objects[0].(*widget.Icon)
	label := box.Objects[1].(*widget.Label)

	// Default: no custom background
	bg.FillColor = color.Transparent

	if uid == addConnectionID {
		icon.SetResource(theme.ContentAddIcon())
		label.SetText("Add New Connection")
		label.TextStyle = fyne.TextStyle{Bold: true}
		return
	}

	if uid == disconnectAllID {
		ct.mu.RLock()
		hasActive := len(ct.sessions) > 0
		ct.mu.RUnlock()
		icon.SetResource(theme.CancelIcon())
		label.SetText("Disconnect All")
		label.TextStyle = fyne.TextStyle{Bold: hasActive}
		return
	}

	if uid == browseLocalID {
		icon.SetResource(theme.FolderOpenIcon())
		label.SetText("Data In Local Files")
		label.TextStyle = fyne.TextStyle{Bold: true}
		return
	}

	if uid == joinLocalID {
		icon.SetResource(theme.SearchIcon())
		label.SetText("JOIN Data In Local Files")
		label.TextStyle = fyne.TextStyle{Bold: true}
		return
	}

	if uid == appStatsID {
		icon.SetResource(theme.BrokenImageIcon())
		label.SetText("App Stats")
		label.TextStyle = fyne.TextStyle{Bold: true}
		return
	}

	if strings.HasPrefix(uid, "lf_file:") {
		filename := strings.TrimPrefix(uid, "lf_file:")
		icon.SetResource(theme.DocumentIcon())
		label.SetText(filename)
		label.TextStyle = fyne.TextStyle{}
		return
	}

	// Environment node
	if strings.HasPrefix(uid, "env:") {
		env := strings.TrimPrefix(uid, "env:")
		icon.SetResource(theme.VisibilityIcon())
		connCount := len(ct.connectionsByEnv[env])
		if connCount < 10 {
			label.SetText(env + " (" + string(rune('0'+connCount)) + ")")
		} else {
			label.SetText(env)
		}
		label.TextStyle = fyne.TextStyle{Bold: true}
		return
	}

	// Connection node — custom selection background owned entirely by us
	if strings.HasPrefix(uid, "conn:") {
		connName := strings.TrimPrefix(uid, "conn:")
		ct.mu.RLock()
		_, connected := ct.sessions[connName]
		connecting := ct.connectingConns[connName]
		isSelected := connName == ct.selectedConnName
		ct.mu.RUnlock()

		if connecting {
			icon.SetResource(theme.ViewRefreshIcon())
			label.SetText(connName + " (connecting ...)")
		} else if connected {
			icon.SetResource(theme.VisibilityIcon())
			label.SetText(connName)
		} else {
			icon.SetResource(theme.VisibilityIcon())
			label.SetText(connName)
		}
		label.TextStyle = fyne.TextStyle{}
		if isSelected {
			bg.FillColor = theme.SelectionColor()
		}
		return
	}

	// Keyspace node
	if strings.HasPrefix(uid, "ks:") {
		parts := strings.SplitN(strings.TrimPrefix(uid, "ks:"), ":", 2)
		if len(parts) == 2 {
			keyspace := parts[1]
			icon.SetResource(theme.StorageIcon())
			label.SetText(keyspace)
			label.TextStyle = fyne.TextStyle{}
		}
		return
	}

	// Table node
	if strings.HasPrefix(uid, "tbl:") {
		parts := strings.SplitN(strings.TrimPrefix(uid, "tbl:"), ":", 3)
		if len(parts) == 3 {
			tableName := parts[2]
			icon.SetResource(theme.ListIcon())
			label.SetText(tableName)
			label.TextStyle = fyne.TextStyle{}
		}
		return
	}
}

// onNodeSelected handles tree node selection
func (ct *ConnectionTree) onNodeSelected(uid widget.TreeNodeID) {
	if uid == addConnectionID {
		ct.tree.UnselectAll()
		if ct.onAddConnection != nil {
			ct.onAddConnection()
		}
		return
	}

	if uid == disconnectAllID {
		ct.tree.UnselectAll()
		ct.mu.RLock()
		hasActive := len(ct.sessions) > 0
		ct.mu.RUnlock()
		if hasActive && ct.onDisconnectAll != nil {
			ct.onDisconnectAll()
		}
		return
	}

	if uid == joinLocalID {
		ct.tree.UnselectAll()
		if ct.onJoinLocalSelected != nil {
			ct.onJoinLocalSelected()
		}
		return
	}

	if uid == appStatsID {
		ct.tree.UnselectAll()
		if ct.onAppStatsSelected != nil {
			ct.onAppStatsSelected()
		}
		return
	}

	// Data In Local Files — toggle expand/collapse, or rescan if no files known
	if uid == browseLocalID {
		ct.tree.UnselectAll()
		ct.mu.RLock()
		loaded := ct.localFilesLoaded
		count := len(ct.localFiles)
		ct.mu.RUnlock()
		if loaded && count == 0 {
			// Leaf state: trigger a fresh scan (fires onBrowseLocalNoFiles if still empty)
			ct.mu.Lock()
			ct.localCancel()
			ctx, localCancel := context.WithCancel(context.Background())
			ct.localCancel = localCancel
			ct.mu.Unlock()
			go ct.scanAndLoadLocalFiles(ctx)
			return
		}
		if ct.tree.IsBranchOpen(browseLocalID) {
			ct.tree.CloseBranch(browseLocalID)
		} else {
			ct.tree.OpenBranch(browseLocalID)
		}
		return
	}

	// Environment node — toggle expand/collapse
	if strings.HasPrefix(uid, "env:") {
		ct.tree.UnselectAll()
		if ct.tree.IsBranchOpen(uid) {
			ct.tree.CloseBranch(uid)
		} else {
			ct.tree.OpenBranch(uid)
		}
		if ct.onEnvironmentToggled != nil {
			ct.onEnvironmentToggled()
		}
		return
	}

	// Connection selection
	if strings.HasPrefix(uid, "conn:") {
		connName := strings.TrimPrefix(uid, "conn:")
		ct.mu.Lock()
		ct.selectedConnName = connName
		ct.mu.Unlock()
		for _, conn := range ct.state.GetConnections() {
			if conn.ConnName == connName {
				connCopy := conn
				if ct.onSelect != nil {
					ct.onSelect(&connCopy)
				}
				return
			}
		}
	}

	// Keyspace selection
	if strings.HasPrefix(uid, "ks:") {
		parts := strings.SplitN(strings.TrimPrefix(uid, "ks:"), ":", 2)
		if len(parts) == 2 {
			connName := parts[0]
			keyspace := parts[1]
			ct.mu.RLock()
			session := ct.sessions[connName]
			ct.mu.RUnlock()
			for _, conn := range ct.state.GetConnections() {
				if conn.ConnName == connName {
					connCopy := conn
					if ct.onKeyspaceSelected != nil {
						ct.onKeyspaceSelected(&connCopy, keyspace, session)
					}
					return
				}
			}
		}
	}

	// Local file selection
	if strings.HasPrefix(uid, "lf_file:") {
		filename := strings.TrimPrefix(uid, "lf_file:")
		workDir := ct.state.GetWorkingDirectory()
		if workDir != "" && ct.onLocalFileSelected != nil {
			ct.onLocalFileSelected(filepath.Join(workDir, "data", filename))
		}
		return
	}

	// Table selection
	if strings.HasPrefix(uid, "tbl:") {
		parts := strings.SplitN(strings.TrimPrefix(uid, "tbl:"), ":", 3)
		if len(parts) == 3 {
			connName := parts[0]
			keyspace := parts[1]
			tableName := parts[2]
			for _, conn := range ct.state.GetConnections() {
				if conn.ConnName == connName {
					connCopy := conn
					if ct.onTableSelected != nil {
						ct.onTableSelected(&connCopy, keyspace, tableName)
					}
					return
				}
			}
		}
	}
}

// onBranchOpened handles branch expansion
func (ct *ConnectionTree) onBranchOpened(uid widget.TreeNodeID) {
	// Data In Local Files expansion — collapse all cassandra env branches, then scan files
	if uid == browseLocalID {
		for _, env := range ct.environmentOrder {
			ct.tree.CloseBranch("env:" + env)
		}
		ct.mu.Lock()
		ct.localCancel()
		ctx, localCancel := context.WithCancel(context.Background())
		ct.localCancel = localCancel
		ct.mu.Unlock()
		go ct.scanAndLoadLocalFiles(ctx)
		return
	}

	// Environment expansion
	if strings.HasPrefix(uid, "env:") {
		env := strings.TrimPrefix(uid, "env:")
		ct.state.mu.Lock()
		ct.state.ExpandedEnvironments[env] = true
		ct.state.mu.Unlock()
		return
	}

	// Connection expansion - connect and load keyspaces
	if strings.HasPrefix(uid, "conn:") {
		connName := strings.TrimPrefix(uid, "conn:")
		ct.mu.Lock()
		if ct.expandedConns[connName] || ct.connectingConns[connName] {
			ct.mu.Unlock()
			return
		}
		ct.connectingConns[connName] = true
		ctx, cancel := context.WithCancel(context.Background())
		ct.connCancels[connName] = cancel
		ct.mu.Unlock()

		go ct.connectAndLoadKeyspaces(connName, ctx)
		return
	}

	// Keyspace expansion - load tables
	if strings.HasPrefix(uid, "ks:") {
		parts := strings.SplitN(strings.TrimPrefix(uid, "ks:"), ":", 2)
		if len(parts) == 2 {
			connName := parts[0]
			keyspace := parts[1]
			key := connName + ":" + keyspace

			ct.mu.Lock()
			if ct.expandedKS[key] || ct.loadingKS[key] {
				ct.mu.Unlock()
				return
			}
			ct.loadingKS[key] = true
			ct.mu.Unlock()

			go ct.loadTables(connName, keyspace)
		}
		return
	}
}

// onBranchClosed handles branch collapse
func (ct *ConnectionTree) onBranchClosed(uid widget.TreeNodeID) {
	if strings.HasPrefix(uid, "env:") {
		env := strings.TrimPrefix(uid, "env:")
		ct.state.mu.Lock()
		ct.state.ExpandedEnvironments[env] = false
		ct.state.mu.Unlock()
	}

	if strings.HasPrefix(uid, "conn:") {
		connName := strings.TrimPrefix(uid, "conn:")
		ct.mu.Lock()
		if cancel, ok := ct.connCancels[connName]; ok {
			delete(ct.connCancels, connName)
			ct.mu.Unlock()
			cancel()
		} else {
			ct.mu.Unlock()
		}
		ct.mu.Lock()
		ct.connectingConns[connName] = false
		ct.mu.Unlock()
	}
}

// connectAndLoadKeyspaces connects to a server and loads keyspaces
func (ct *ConnectionTree) connectAndLoadKeyspaces(connName string, ctx context.Context) {
	defer func() {
		ct.mu.Lock()
		if cancel, ok := ct.connCancels[connName]; ok {
			delete(ct.connCancels, connName)
			ct.mu.Unlock()
			cancel()
		} else {
			ct.mu.Unlock()
		}
	}()
	// Find the connection
	var conn *DecryptedConnection
	for _, c := range ct.state.GetConnections() {
		if c.ConnName == connName {
			connCopy := c
			conn = &connCopy
			break
		}
	}

	if conn == nil {
		ct.mu.Lock()
		ct.connectingConns[connName] = false
		ct.mu.Unlock()
		ct.scheduleRefresh()
		return
	}

	// Create session
	session, err := createBrowseSession(*conn)
	if err != nil {
		AppLog("connection to Cassandra cluster failed")
		ct.mu.Lock()
		ct.connectingConns[connName] = false
		ct.mu.Unlock()
		ct.scheduleRefresh()

		if ct.onConnectionError != nil {
			connRef, errRef := conn, err
			fyne.Do(func() { ct.onConnectionError(connRef, errRef) })
		}
		fyne.Do(func() { ct.tree.CloseBranch("conn:" + connName) })
		return
	}

	// Branch may have been closed while the connection was being established.
	if ctx.Err() != nil {
		closeBrowseSession(session)
		ct.mu.Lock()
		ct.connectingConns[connName] = false
		ct.mu.Unlock()
		ct.scheduleRefresh()
		return
	}

	// Fetch keyspaces
	keyspaces, err := fetchKeyspaces(session)
	if err != nil {
		closeBrowseSession(session)
		ct.mu.Lock()
		ct.connectingConns[connName] = false
		ct.mu.Unlock()
		ct.scheduleRefresh()

		if ct.onConnectionError != nil {
			connRef, errRef := conn, err
			fyne.Do(func() { ct.onConnectionError(connRef, errRef) })
		}
		fyne.Do(func() { ct.tree.CloseBranch("conn:" + connName) })
		return
	}

	// Store session and keyspaces — but abort if CloseAllSessions fired while
	// we were connecting (it resets connectingConns, so the flag is gone).
	ct.mu.Lock()
	if !ct.connectingConns[connName] {
		ct.mu.Unlock()
		closeBrowseSession(session)
		return
	}
	ct.sessions[connName] = session
	ct.keyspaces[connName] = make([]string, len(keyspaces))
	for i, ks := range keyspaces {
		ct.keyspaces[connName][i] = ks.Name
	}
	ct.expandedConns[connName] = true
	ct.connectingConns[connName] = false
	ct.mu.Unlock()

	ct.scheduleRefresh()

	// Notify that connection was expanded
	if ct.onConnectionExpanded != nil && conn != nil {
		connRef := conn
		fyne.Do(func() { ct.onConnectionExpanded(connRef, session) })
	}
}

// loadTables loads tables for a keyspace
func (ct *ConnectionTree) loadTables(connName, keyspace string) {
	ct.mu.RLock()
	session := ct.sessions[connName]
	ct.mu.RUnlock()

	if session == nil {
		return
	}

	key := connName + ":" + keyspace

	tables, err := fetchTables(session, keyspace)
	if err != nil {
		ct.mu.Lock()
		ct.loadingKS[key] = false
		ct.mu.Unlock()
		if ct.state.MainWindow != nil {
			dialog.ShowError(err, ct.state.MainWindow)
		}
		return
	}

	ct.mu.Lock()
	ct.tables[key] = make([]string, len(tables))
	for i, t := range tables {
		ct.tables[key][i] = t.Name
	}
	ct.expandedKS[key] = true
	ct.loadingKS[key] = false
	ct.mu.Unlock()

	ct.scheduleRefresh()
}

// scheduleRefresh coalesces rapid back-to-back tree redraws triggered by
// goroutines (connect, loadTables, file scan) into a single call. Multiple
// completions within the 50ms window produce one refresh instead of N.
func (ct *ConnectionTree) scheduleRefresh() {
	ct.refreshMu.Lock()
	defer ct.refreshMu.Unlock()
	if ct.refreshTimer != nil {
		ct.refreshTimer.Stop()
	}
	ct.refreshTimer = time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(func() {
			ct.tree.Refresh()
		})
	})
}

// Container returns the tree container
func (ct *ConnectionTree) Container() fyne.CanvasObject {
	return ct.container
}

// Refresh rebuilds the tree with current data
func (ct *ConnectionTree) Refresh() {
	ct.buildConnectionMap()
	ct.tree.Refresh()
}

// SelectConnection selects a connection in the tree
func (ct *ConnectionTree) SelectConnection(connName string) {
	ct.mu.Lock()
	ct.selectedConnName = connName
	ct.mu.Unlock()
	// Clear Fyne's built-in selection highlight so it doesn't stack with ours
	ct.tree.UnselectAll()
	ct.tree.Refresh()
	// Fire the selection callback directly — tree.Select() is not used here because
	// it can leave stale highlight pixels on the previously selected cell
	for _, c := range ct.state.GetConnections() {
		if c.ConnName == connName {
			connCopy := c
			if ct.onSelect != nil {
				ct.onSelect(&connCopy)
			}
			break
		}
	}
}

// CloseAllSessions closes all open Cassandra sessions
func (ct *ConnectionTree) CloseAllSessions() {
	ct.mu.Lock()

	// Collect sessions and expanded-conn names, then reset all state before
	// releasing the lock. connectingConns is cleared here so that any goroutine
	// still mid-handshake will detect the cleared flag and discard its session.
	toClose := make([]*gocql.Session, 0, len(ct.sessions))
	for _, session := range ct.sessions {
		toClose = append(toClose, session)
	}
	openedConns := make([]string, 0, len(ct.expandedConns))
	for connName := range ct.expandedConns {
		openedConns = append(openedConns, connName)
	}
	ct.sessions = make(map[string]*gocql.Session)
	ct.keyspaces = make(map[string][]string)
	ct.tables = make(map[string][]string)
	ct.expandedConns = make(map[string]bool)
	ct.expandedKS = make(map[string]bool)
	ct.connectingConns = make(map[string]bool)
	ct.loadingKS = make(map[string]bool)
	ct.selectedConnName = ""

	cancels := make([]context.CancelFunc, 0, len(ct.connCancels))
	for _, cancel := range ct.connCancels {
		cancels = append(cancels, cancel)
	}
	ct.connCancels = make(map[string]context.CancelFunc)
	localCancel := ct.localCancel
	ct.localCancel = func() {}

	ct.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	localCancel()

	// Close sessions after releasing the mutex — session.Close() blocks until
	// in-flight requests drain and must not be called while holding the lock.
	for _, session := range toClose {
		closeBrowseSession(session)
	}

	for _, connName := range openedConns {
		ct.tree.CloseBranch("conn:" + connName)
	}
}

// CloseSession closes a specific connection's session
func (ct *ConnectionTree) CloseSession(connName string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if session, ok := ct.sessions[connName]; ok {
		closeBrowseSession(session)
		delete(ct.sessions, connName)
	}
	delete(ct.keyspaces, connName)
	delete(ct.expandedConns, connName)

	// Remove tables for this connection
	for key := range ct.tables {
		if strings.HasPrefix(key, connName+":") {
			delete(ct.tables, key)
		}
	}
	for key := range ct.expandedKS {
		if strings.HasPrefix(key, connName+":") {
			delete(ct.expandedKS, key)
		}
	}
	for key := range ct.loadingKS {
		if strings.HasPrefix(key, connName+":") {
			delete(ct.loadingKS, key)
		}
	}
}

// CollapseAllEnvBranches closes every environment branch in the tree.
func (ct *ConnectionTree) CollapseAllEnvBranches() {
	for _, env := range ct.environmentOrder {
		ct.tree.CloseBranch("env:" + env)
	}
}

// HasActiveSessions reports whether any sessions are currently open.
func (ct *ConnectionTree) HasActiveSessions() bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.sessions) > 0
}

// GetSession returns the session for a connection (if connected)
func (ct *ConnectionTree) GetSession(connName string) *gocql.Session {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.sessions[connName]
}

// RescanLocalFiles cancels any in-progress file scan and starts a fresh one.
func (ct *ConnectionTree) RescanLocalFiles() {
	ct.mu.Lock()
	ct.localCancel()
	ctx, localCancel := context.WithCancel(context.Background())
	ct.localCancel = localCancel
	ct.mu.Unlock()
	go ct.scanAndLoadLocalFiles(ctx)
}

// SelectLocalFile highlights a local parquet file by filename in the tree.
func (ct *ConnectionTree) SelectLocalFile(filename string) {
	ct.tree.Select("lf_file:" + filename)
}

// scanAndLoadLocalFiles scans <workDir>/data/ for *.parquet files and groups them by table name.
func (ct *ConnectionTree) scanAndLoadLocalFiles(ctx context.Context) {
	workDir := ct.state.GetWorkingDirectory()
	if workDir == "" {
		return
	}
	dataDir := filepath.Join(workDir, "data")
	entries, err := os.ReadDir(dataDir)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		ct.mu.Lock()
		ct.localFiles = []string{}
		ct.localFilesLoaded = true
		ct.mu.Unlock()
		ct.scheduleRefresh()
		return
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".parquet") {
			files = append(files, name)
		}
	}
	sort.Strings(files)

	if ctx.Err() != nil {
		return
	}

	ct.mu.Lock()
	ct.localFiles = files
	ct.localFilesLoaded = true
	ct.mu.Unlock()
	ct.scheduleRefresh()

	if len(files) == 0 && ct.onBrowseLocalNoFiles != nil {
		fyne.Do(func() { ct.onBrowseLocalNoFiles() })
	}
}
