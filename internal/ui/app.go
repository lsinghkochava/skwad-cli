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
	"fyne.io/fyne/v2/theme"
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
	a.applyAppearanceMode()
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
	a.terminalArea = NewTerminalArea(a.manager, a.pool)
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
	a.sidebar.OnAddToBench = func(id uuid.UUID) {
		ag, ok := a.manager.Agent(id)
		if !ok {
			return
		}
		bench, _ := a.store.LoadBench()
		bench = append(bench, models.BenchAgent{
			ID:           ag.ID,
			Name:         ag.Name,
			Avatar:       ag.Avatar,
			Folder:       ag.Folder,
			AgentType:    ag.AgentType,
			ShellCommand: ag.ShellCommand,
			PersonaID:    ag.PersonaID,
		})
		_ = a.store.SaveBench(bench)
	}
	a.sidebar.OnEditAgent = func(id uuid.UUID) {
		ag, ok := a.manager.Agent(id)
		if !ok {
			return
		}
		personas, _ := a.store.LoadPersonas()
		sheet := EditAgentSheet(a.window, ag, personas, func(updated *models.Agent) {
			a.manager.UpdateAgent(id, func(a *models.Agent) {
				a.Name = updated.Name
				a.Avatar = updated.Avatar
				a.Folder = updated.Folder
				a.AgentType = updated.AgentType
				a.ShellCommand = updated.ShellCommand
				a.PersonaID = updated.PersonaID
			})
		})
		sheet.Show()
	}
	a.sidebar.OnForkSession = func(id uuid.UUID) {
		ag, ok := a.manager.Agent(id)
		if !ok || ag.SessionID == "" {
			return
		}
		a.manager.ForkAgent(id, ag.SessionID)
		a.pool.Restart(id)
	}
	a.sidebar.OnMoveToWorkspace = func(agentID, workspaceID uuid.UUID) {
		a.manager.MoveAgent(agentID, workspaceID)
	}
	a.sidebar.OnRegisterAgent = func(id uuid.UUID) {
		a.pool.ForceRegistration(id)
	}
	a.sidebar.OnAddShellCompanion = func(creatorID uuid.UUID) {
		creator, ok := a.manager.Agent(creatorID)
		if !ok {
			return
		}
		companion := &models.Agent{
			ID:          uuid.New(),
			Name:        creator.Name + " shell",
			Avatar:      "🐚",
			Folder:      creator.Folder,
			AgentType:   models.AgentTypeShell,
			IsCompanion: true,
			CreatedBy:   &creatorID,
		}
		companion.Metadata = make(map[string]string)
		a.manager.AddAgent(companion, &creatorID)
		a.pool.Spawn(companion)
	}

	a.settingsWindow.window = a.window
	a.settingsWindow.OnDeployBenchAgent = func(ag *models.Agent) {
		a.manager.AddAgent(ag, nil)
		a.pool.Spawn(ag)
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
	// Restore the persisted split ratio if the user has moved it before.
	if saved := a.store.LoadSidebarSplitOffset(); saved > 0 {
		a.mainSplit.Offset = saved
	} else {
		a.mainSplit.Offset = sidebarSplitOffset
	}

	a.window.SetContent(a.mainSplit)
	a.setupKeyboardShortcuts()
	a.setupSystemTray()

	// Persist split ratio when the window closes.
	settings := a.store.Settings()
	if settings.KeepInTray {
		// Hide to tray instead of quitting.
		a.window.SetCloseIntercept(func() {
			a.store.SaveSidebarSplitOffset(a.mainSplit.Offset)
			a.window.Hide()
		})
	} else {
		a.window.SetCloseIntercept(func() {
			a.store.SaveSidebarSplitOffset(a.mainSplit.Offset)
			a.pool.StopAll()
			a.manager.Shutdown()
			a.fyneApp.Quit()
		})
	}
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
	// Ctrl/Cmd+Ctrl+1..9: switch to workspace by index
	ctrl := fyne.KeyModifierControl
	for i, key := range []fyne.KeyName{
		fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5,
		fyne.Key6, fyne.Key7, fyne.Key8, fyne.Key9,
	} {
		idx := i // capture
		a.window.Canvas().AddShortcut(
			&desktop.CustomShortcut{KeyName: key, Modifier: mod | ctrl},
			func(_ fyne.Shortcut) { a.selectWorkspaceByIndex(idx) },
		)
	}
	// Ctrl/Cmd+Shift+O: open focused agent's folder in default editor
	a.window.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: mod | shift},
		func(_ fyne.Shortcut) { a.openWithDefaultEditor() },
	)
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

// applyAppearanceMode sets the Fyne theme according to the stored appearance setting.
func (a *App) applyAppearanceMode() {
	settings := a.store.Settings()
	switch settings.AppearanceMode {
	case models.AppearanceModeDark:
		a.fyneApp.Settings().SetTheme(theme.DarkTheme())
	case models.AppearanceModeLight:
		a.fyneApp.Settings().SetTheme(theme.LightTheme())
	// "auto" and "system" let Fyne follow the OS default — no override needed.
	}
}

// selectWorkspaceByIndex switches to the workspace at 0-based index.
func (a *App) selectWorkspaceByIndex(idx int) {
	workspaces := a.manager.Workspaces()
	if idx >= len(workspaces) {
		return
	}
	a.manager.SetActiveWorkspace(workspaces[idx].ID)
}

// openWithDefaultEditor opens the focused agent's folder in the configured editor.
func (a *App) openWithDefaultEditor() {
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
	settings := a.store.Settings()
	if settings.DefaultOpenWithApp != "" {
		_ = runDetached(settings.DefaultOpenWithApp, ag.Folder)
	} else {
		openFileExternal(ag.Folder)
	}
}

// openHistoryPanel shows a session history popup for the given agent.
// Supports Resume, Fork, and Delete per session.
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

	var popup *widget.PopUp
	dismiss := func() {
		if popup != nil {
			popup.Hide()
			popup = nil
		}
	}

	var list *widget.List
	list = widget.NewList(
		func() int { return len(sessions) },
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("")
			resumeBtn := widget.NewButton("Resume", nil)
			forkBtn := widget.NewButton("Fork", nil)
			deleteBtn := widget.NewButton("Delete", nil)
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(resumeBtn, forkBtn, deleteBtn),
				lbl,
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(sessions) {
				return
			}
			s := sessions[id]
			row := obj.(*fyne.Container)
			date := s.Timestamp.Format("Jan 2 15:04")
			row.Objects[0].(*widget.Label).SetText(
				fmt.Sprintf("%s  (%d msgs, %s)", s.Title, s.MessageCount, date),
			)
			btns := row.Objects[1].(*fyne.Container)

			sessionID := s.SessionID
			btns.Objects[0].(*widget.Button).OnTapped = func() {
				dismiss()
				a.manager.ResumeAgent(agentID, sessionID)
				a.pool.Restart(agentID)
			}
			btns.Objects[1].(*widget.Button).OnTapped = func() {
				dismiss()
				a.manager.ForkAgent(agentID, sessionID)
				a.pool.Restart(agentID)
			}
			btns.Objects[2].(*widget.Button).OnTapped = func() {
				_ = a.historySvc.DeleteSession(ag.AgentType, sessionID)
				// Remove from local slice and refresh.
				sessions = append(sessions[:id], sessions[id+1:]...)
				list.Refresh()
			}
			for _, b := range btns.Objects {
				b.(*widget.Button).Refresh()
			}
		},
	)

	popup = widget.NewModalPopUp(list, a.window.Canvas())
	popup.Resize(fyne.NewSize(700, 420))
	popup.Show()
}

// setupSystemTray configures the system tray icon and menu when KeepInTray is enabled.
func (a *App) setupSystemTray() {
	settings := a.store.Settings()
	if !settings.KeepInTray {
		return
	}
	dt, ok := a.fyneApp.(desktop.App)
	if !ok {
		return
	}

	dt.SetSystemTrayIcon(theme.ComputerIcon())
	dt.SetSystemTrayMenu(fyne.NewMenu("Skwad",
		fyne.NewMenuItem("Show Skwad", func() {
			a.window.Show()
			a.window.RequestFocus()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			a.store.SaveSidebarSplitOffset(a.mainSplit.Offset)
			a.pool.StopAll()
			a.manager.Shutdown()
			a.fyneApp.Quit()
		}),
	))
}

// Run starts the Fyne event loop (blocks until window is closed).
func (a *App) Run() {
	a.window.ShowAndRun()
}
