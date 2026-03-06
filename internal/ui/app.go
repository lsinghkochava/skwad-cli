// Package ui contains all Fyne-based UI components for Skwad.
// It assembles the main window, sidebar, terminal panes, git panel,
// markdown preview, file finder, agent sheet, and settings window.
package ui

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/history"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/persistence"
	"github.com/Jared-Boschmann/skwad-linux/internal/terminal"
)

const (
	appID    = "com.jared-boschmann.skwad"
	appTitle = "Skwad"

	minWidth  = 800
	minHeight = 600

	// sidebarSplitOffset is the initial fraction of the window width
	// occupied by [workspaceBar + sidebar]. ~20% ≈ 160-200px on a 1000px window.
	sidebarSplitOffset = 0.20
)

// App is the top-level Fyne application wrapper.
type App struct {
	fyneApp    fyne.App
	window     fyne.Window
	manager    *agent.Manager
	coord      *agent.Coordinator
	store      *persistence.Store
	pool       *terminal.Pool
	historySvc *history.Service

	workspaceBar   *WorkspaceBar
	sidebar        *Sidebar
	terminalArea   *TerminalArea
	settingsWindow *SettingsWindow
	mainSplit      *container.Split
}

// NewApp creates and configures the Fyne app.
func NewApp(mgr *agent.Manager, coord *agent.Coordinator, store *persistence.Store, pool *terminal.Pool) *App {
	a := &App{
		fyneApp:    app.NewWithID(appID),
		historySvc: history.New(),
		manager: mgr,
		coord:   coord,
		store:   store,
		pool:    pool,
	}
	a.buildWindow()
	// Spawn sessions for all agents that were persisted from the last run.
	// Runs after buildWindow so OnAgentChanged is already registered.
	for _, ag := range mgr.AllAgents() {
		pool.Spawn(ag)
	}
	return a
}

func (a *App) buildWindow() {
	a.window = a.fyneApp.NewWindow(appTitle)
	a.window.Resize(fyne.NewSize(minWidth, minHeight))
	a.window.SetMaster()

	a.workspaceBar = NewWorkspaceBar(a.manager)
	a.sidebar = NewSidebar(a.manager)
	a.terminalArea = NewTerminalArea(a.manager)
	a.settingsWindow = NewSettingsWindow(a.fyneApp, a.store)

	// Wire new-agent creation: manager.AddAgent + pool.Spawn.
	// Called OUTSIDE manager lock so pool.Spawn can safely read manager state.
	a.sidebar.OnAddAgent = func(ag *models.Agent) {
		a.manager.AddAgent(ag, nil)
		a.pool.Spawn(ag)
	}
	a.sidebar.OnRemoveAgent = func(id uuid.UUID) {
		a.pool.Kill(id)
		a.manager.RemoveAgent(id)
	}
	a.sidebar.OnRestartAgent = func(id uuid.UUID) {
		a.pool.Restart(id)
	}
	a.sidebar.window = a.window
	a.sidebar.store = a.store
	a.workspaceBar.window = a.window

	a.sidebar.OnDuplicateAgent = func(id uuid.UUID) {
		dup := a.manager.DuplicateAgent(id)
		if dup != nil {
			a.pool.Spawn(dup)
		}
	}
	a.sidebar.OnShowHistory = func(id uuid.UUID) {
		a.openHistoryPanel(id)
	}

	// Wire manager callbacks (called while manager lock is held — do NOT call
	// manager methods or pool.Spawn from inside these callbacks).
	a.manager.OnAgentChanged = func(_ uuid.UUID) {
		a.sidebar.Refresh()
		a.terminalArea.Refresh()
		a.workspaceBar.Refresh()
	}
	a.manager.OnWorkspaceChanged = func() {
		a.workspaceBar.Refresh()
		a.sidebar.Refresh()
		a.terminalArea.Refresh()
	}

	// Main layout:
	//   [ workspaceBar (fixed 48px) ][ sidebar ]  |  [ terminal area ]
	// The HSplit drag handle lets the user resize the sidebar.
	leftPanel := container.NewBorder(nil, nil, a.workspaceBar.Widget(), nil, a.sidebar.Widget())
	a.mainSplit = container.NewHSplit(leftPanel, a.terminalArea.Widget())
	a.mainSplit.Offset = sidebarSplitOffset

	a.window.SetContent(a.mainSplit)
	a.setupKeyboardShortcuts()

	// Persist split ratio when the window closes.
	a.window.SetCloseIntercept(func() {
		a.store.SaveSidebarSplitOffset(a.mainSplit.Offset)
		a.pool.StopAll()
		a.manager.Shutdown()
		a.fyneApp.Quit()
	})
}

func (a *App) setupKeyboardShortcuts() {
	// fyne.KeyModifierShortcutDefault = Ctrl on Linux/Windows, Cmd on macOS.
	mod := fyne.KeyModifierShortcutDefault
	shift := fyne.KeyModifierShift

	// Ctrl/Cmd+N: new agent
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: mod},
		func(_ fyne.Shortcut) { a.sidebar.OpenNewAgentSheet() },
	)
	// Ctrl/Cmd+,: settings
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: mod},
		func(_ fyne.Shortcut) { a.settingsWindow.Show() },
	)
	// Ctrl/Cmd+G: toggle git panel
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyG, Modifier: mod},
		func(_ fyne.Shortcut) { a.terminalArea.ToggleGitPanel() },
	)
	// Ctrl/Cmd+\: toggle sidebar
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyBackslash, Modifier: mod},
		func(_ fyne.Shortcut) {
			a.sidebar.Toggle()
			leftPanel := container.NewBorder(nil, nil, a.workspaceBar.Widget(), nil, a.sidebar.Widget())
			a.mainSplit.Leading = leftPanel
			a.mainSplit.Refresh()
		},
	)
	// Ctrl/Cmd+P: file finder
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: mod},
		func(_ fyne.Shortcut) { a.openFileFinder() },
	)
	// Ctrl/Cmd+]: next agent
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyRightBracket, Modifier: mod},
		func(_ fyne.Shortcut) { a.selectAdjacentAgent(1) },
	)
	// Ctrl/Cmd+[: previous agent
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyLeftBracket, Modifier: mod},
		func(_ fyne.Shortcut) { a.selectAdjacentAgent(-1) },
	)
	// Ctrl/Cmd+Shift+]: next workspace
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyRightBracket, Modifier: mod | shift},
		func(_ fyne.Shortcut) { a.selectAdjacentWorkspace(1) },
	)
	// Ctrl/Cmd+Shift+[: previous workspace
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyLeftBracket, Modifier: mod | shift},
		func(_ fyne.Shortcut) { a.selectAdjacentWorkspace(-1) },
	)
	// Ctrl/Cmd+1..9: select agent by index
	for i, key := range []fyne.KeyName{
		fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5,
		fyne.Key6, fyne.Key7, fyne.Key8, fyne.Key9,
	} {
		idx := i // capture
		a.window.Canvas().AddShortcut(
			&desktop.CustomShortcut{KeyName: key, Modifier: mod},
			func(_ fyne.Shortcut) { a.selectAgentByIndex(idx) },
		)
	}
}

// selectAdjacentAgent moves the focused pane to the next or previous agent.
func (a *App) selectAdjacentAgent(delta int) {
	ws := a.manager.ActiveWorkspace()
	if ws == nil {
		return
	}
	agents := a.manager.Agents()
	if len(agents) == 0 {
		return
	}
	var curID uuid.UUID
	if len(ws.ActiveAgentIDs) > ws.FocusedPaneIndex {
		curID = ws.ActiveAgentIDs[ws.FocusedPaneIndex]
	}
	idx := 0
	for i, ag := range agents {
		if ag.ID == curID {
			idx = i
			break
		}
	}
	next := (idx + delta + len(agents)) % len(agents)
	nextID := agents[next].ID
	a.manager.UpdateWorkspace(ws.ID, func(w *models.Workspace) {
		if len(w.ActiveAgentIDs) == 0 {
			w.ActiveAgentIDs = []uuid.UUID{nextID}
		} else {
			w.ActiveAgentIDs[w.FocusedPaneIndex] = nextID
		}
	})
}

// selectAgentByIndex selects the agent at the given 0-based index.
func (a *App) selectAgentByIndex(idx int) {
	ws := a.manager.ActiveWorkspace()
	if ws == nil {
		return
	}
	agents := a.manager.Agents()
	if idx >= len(agents) {
		return
	}
	nextID := agents[idx].ID
	a.manager.UpdateWorkspace(ws.ID, func(w *models.Workspace) {
		if len(w.ActiveAgentIDs) == 0 {
			w.ActiveAgentIDs = []uuid.UUID{nextID}
		} else {
			w.ActiveAgentIDs[w.FocusedPaneIndex] = nextID
		}
	})
}

// selectAdjacentWorkspace switches to the next or previous workspace.
func (a *App) selectAdjacentWorkspace(delta int) {
	workspaces := a.manager.Workspaces()
	if len(workspaces) == 0 {
		return
	}
	active := a.manager.ActiveWorkspace()
	idx := 0
	for i, ws := range workspaces {
		if active != nil && ws.ID == active.ID {
			idx = i
			break
		}
	}
	next := (idx + delta + len(workspaces)) % len(workspaces)
	a.manager.SetActiveWorkspace(workspaces[next].ID)
}

// openFileFinder shows the fuzzy file finder overlay for the focused agent's folder.
func (a *App) openFileFinder() {
	ws := a.manager.ActiveWorkspace()
	if ws == nil || len(ws.ActiveAgentIDs) == 0 {
		return
	}
	agentID := ws.ActiveAgentIDs[0]
	if ws.FocusedPaneIndex < len(ws.ActiveAgentIDs) {
		agentID = ws.ActiveAgentIDs[ws.FocusedPaneIndex]
	}
	ag, ok := a.manager.Agent(agentID)
	if !ok || ag.Folder == "" {
		return
	}

	ff := NewFileFinder(ag.Folder)
	ff.IndexFolder(ag.Folder)

	var popup *widget.PopUp
	ff.OnSelect = func(absPath string) {
		popup.Hide()
		// Use the configured editor if set, otherwise open externally.
		settings := a.store.Settings()
		if settings.DefaultOpenWithApp != "" {
			_ = runDetached(settings.DefaultOpenWithApp, absPath)
		} else {
			openFileExternal(absPath)
		}
	}
	ff.OnClose = func() { popup.Hide() }

	popup = widget.NewModalPopUp(ff.Widget(), a.window.Canvas())
	popup.Resize(fyne.NewSize(560, 420))
	popup.Show()
	// Focus the search input after the popup appears.
	ff.FocusInput(a.window.Canvas())
}

// ShowMarkdownFile opens a markdown file in the preview panel (called by MCP).
func (a *App) ShowMarkdownFile(filePath string) {
	if !filepath.IsAbs(filePath) {
		// Resolve relative paths against the focused agent's folder.
		ws := a.manager.ActiveWorkspace()
		if ws != nil && len(ws.ActiveAgentIDs) > 0 {
			if ag, ok := a.manager.Agent(ws.ActiveAgentIDs[0]); ok {
				filePath = filepath.Join(ag.Folder, filePath)
			}
		}
	}
	a.terminalArea.ShowMarkdownFile(filePath)
}

// ShowMermaid renders a Mermaid diagram (called by MCP).
func (a *App) ShowMermaid(source, title string) {
	a.terminalArea.ShowMermaid(source, title)
}

// openHistoryPanel shows a session history popup for the given agent.
func (a *App) openHistoryPanel(agentID uuid.UUID) {
	ag, ok := a.manager.Agent(agentID)
	if !ok {
		return
	}
	if !a.historySvc.Supports(ag.AgentType) {
		return
	}
	sessions, err := a.historySvc.ListSessions(ag.AgentType, ag.Folder)
	if err != nil || len(sessions) == 0 {
		return
	}

	// Build list items: "Title  (N messages  date)"
	items := make([]string, len(sessions))
	for i, s := range sessions {
		date := s.Timestamp.Format("Jan 2 15:04")
		items[i] = fmt.Sprintf("%s  (%d msgs, %s)", s.Title, s.MessageCount, date)
	}

	list := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(items) {
				obj.(*widget.Label).SetText(items[id])
			}
		},
	)

	var popup *widget.PopUp
	list.OnSelected = func(id widget.ListItemID) {
		if id >= len(sessions) {
			return
		}
		sessionID := sessions[id].SessionID
		popup.Hide()
		a.manager.ResumeAgent(agentID, sessionID)
		a.pool.Restart(agentID)
	}

	popup = widget.NewModalPopUp(list, a.window.Canvas())
	popup.Resize(fyne.NewSize(600, 400))
	popup.Show()
}

// Run starts the Fyne event loop (blocks until window is closed).
func (a *App) Run() {
	a.window.ShowAndRun()
}
