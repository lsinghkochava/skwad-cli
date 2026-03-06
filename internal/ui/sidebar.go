package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/persistence"
)

const (
	sidebarDefaultWidth float32 = 220
	sidebarMinWidth     float32 = 80
	sidebarMaxWidth     float32 = 400

	// sidebar background color (#1A1D2E)
	sidebarBgR uint8 = 26
	sidebarBgG uint8 = 29
	sidebarBgB uint8 = 46
)

// Sidebar lists non-companion agents for the active workspace.
type Sidebar struct {
	manager     *agent.Manager
	list        *widget.List
	headerLabel *canvas.Text
	container   *fyne.Container
	collapsed   bool
	width       float32

	// Set by App after construction (same package, so direct field access is fine).
	window fyne.Window
	store  *persistence.Store

	// Callbacks — set by App before the window is shown.
	OnAddAgent          func(a *models.Agent)
	OnRemoveAgent       func(id uuid.UUID)
	OnRestartAgent      func(id uuid.UUID)
	OnDuplicateAgent    func(id uuid.UUID)
	OnShowHistory       func(id uuid.UUID)
	OnAddToBench        func(id uuid.UUID)
	OnEditAgent         func(id uuid.UUID)
	OnForkSession       func(id uuid.UUID)
	OnMoveToWorkspace   func(agentID, workspaceID uuid.UUID)
	OnRegisterAgent     func(id uuid.UUID)
	OnAddShellCompanion func(id uuid.UUID)
}

// NewSidebar creates the agent list sidebar.
func NewSidebar(mgr *agent.Manager) *Sidebar {
	s := &Sidebar{
		manager: mgr,
		width:   sidebarDefaultWidth,
	}
	s.build()
	return s
}

func (s *Sidebar) build() {
	// Header: workspace name in uppercase
	ws := s.manager.ActiveWorkspace()
	headerName := "WORKSPACE"
	if ws != nil {
		headerName = strings.ToUpper(ws.Name)
	}
	s.headerLabel = canvas.NewText(headerName, color.NRGBA{R: 140, G: 145, B: 170, A: 255})
	s.headerLabel.TextSize = 10
	s.headerLabel.TextStyle = fyne.TextStyle{Bold: true}

	header := container.NewPadded(s.headerLabel)

	// Agent list
	s.list = widget.NewList(
		func() int { return len(s.manager.Agents()) },
		func() fyne.CanvasObject { return newAgentRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			agents := s.manager.Agents()
			if id >= len(agents) {
				return
			}
			row := obj.(*agentRow)
			row.onSecondaryTap = s.showContextMenu
			row.update(agents[id])
		},
	)
	s.list.OnSelected = func(id widget.ListItemID) {
		agents := s.manager.Agents()
		if id >= len(agents) {
			return
		}
		activeWS := s.manager.ActiveWorkspace()
		if activeWS == nil {
			return
		}
		selectedID := agents[id].ID
		s.manager.UpdateWorkspace(activeWS.ID, func(w *models.Workspace) {
			idx := w.FocusedPaneIndex
			switch {
			case len(w.ActiveAgentIDs) == 0:
				w.ActiveAgentIDs = []uuid.UUID{selectedID}
			case idx < len(w.ActiveAgentIDs):
				w.ActiveAgentIDs[idx] = selectedID
			default:
				// FocusedPaneIndex is beyond current slice — grow to fit
				for len(w.ActiveAgentIDs) <= idx {
					w.ActiveAgentIDs = append(w.ActiveAgentIDs, uuid.Nil)
				}
				w.ActiveAgentIDs[idx] = selectedID
			}
		})
	}

	addBtn := widget.NewButton("+ New Agent", func() { s.OpenNewAgentSheet() })

	bg := canvas.NewRectangle(color.NRGBA{R: sidebarBgR, G: sidebarBgG, B: sidebarBgB, A: 255})
	content := container.NewBorder(header, addBtn, nil, nil, s.list)
	s.container = container.NewStack(bg, content)
}

// OpenNewAgentSheet opens the agent creation dialog.
func (s *Sidebar) OpenNewAgentSheet() {
	if s.window == nil {
		return
	}
	var personas []models.Persona
	if s.store != nil {
		personas, _ = s.store.LoadPersonas()
	}
	sheet := NewAgentSheet(s.window, personas, func(ag *models.Agent) {
		if s.OnAddAgent != nil {
			s.OnAddAgent(ag)
		}
	})
	sheet.Show()
}

// showContextMenu displays a right-click context menu for an agent row.
func (s *Sidebar) showContextMenu(agentID uuid.UUID, pos fyne.Position) {
	if s.window == nil {
		return
	}

	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Edit…", func() {
			if s.OnEditAgent != nil {
				s.OnEditAgent(agentID)
			}
		}),
		fyne.NewMenuItem("Restart", func() {
			if s.OnRestartAgent != nil {
				s.OnRestartAgent(agentID)
			}
		}),
		fyne.NewMenuItem("Duplicate", func() {
			if s.OnDuplicateAgent != nil {
				s.OnDuplicateAgent(agentID)
			}
		}),
		fyne.NewMenuItem("Add Shell Companion", func() {
			if s.OnAddShellCompanion != nil {
				s.OnAddShellCompanion(agentID)
			}
		}),
		fyne.NewMenuItem("Add to Bench", func() {
			if s.OnAddToBench != nil {
				s.OnAddToBench(agentID)
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("History…", func() {
			if s.OnShowHistory != nil {
				s.OnShowHistory(agentID)
			}
		}),
		fyne.NewMenuItem("Fork Session", func() {
			if s.OnForkSession != nil {
				s.OnForkSession(agentID)
			}
		}),
		fyne.NewMenuItemSeparator(),
	}

	// "Move to workspace" submenu
	workspaces := s.manager.Workspaces()
	active := s.manager.ActiveWorkspace()
	var moveItems []*fyne.MenuItem
	for _, ws := range workspaces {
		if active != nil && ws.ID == active.ID {
			continue
		}
		wsID := ws.ID
		wsName := ws.Name
		moveItems = append(moveItems, fyne.NewMenuItem(wsName, func() {
			if s.OnMoveToWorkspace != nil {
				s.OnMoveToWorkspace(agentID, wsID)
			}
		}))
	}
	if len(moveItems) > 0 {
		moveMenu := fyne.NewMenuItem("Move to Workspace", nil)
		moveMenu.ChildMenu = fyne.NewMenu("", moveItems...)
		items = append(items, moveMenu)
	}

	items = append(items,
		fyne.NewMenuItem("Register", func() {
			if s.OnRegisterAgent != nil {
				s.OnRegisterAgent(agentID)
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Remove", func() {
			if s.OnRemoveAgent != nil {
				s.OnRemoveAgent(agentID)
			}
		}),
	)

	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, s.window.Canvas(), pos)
}

// Refresh rebuilds the list and header to reflect current state.
func (s *Sidebar) Refresh() {
	if ws := s.manager.ActiveWorkspace(); ws != nil && s.headerLabel != nil {
		s.headerLabel.Text = strings.ToUpper(ws.Name)
		s.headerLabel.Refresh()
	}
	s.list.Refresh()
}

// Widget returns the sidebar widget.
func (s *Sidebar) Widget() fyne.CanvasObject {
	if s.collapsed {
		return container.NewWithoutLayout()
	}
	return s.container
}

// Toggle collapses or expands the sidebar.
func (s *Sidebar) Toggle() {
	s.collapsed = !s.collapsed
}

// --- agent row ---

const (
	agentAvatarSize float32 = 40
	agentRowDot     float32 = 8
	agentRowH       float32 = 56
	agentRowPad     float32 = 8
)

type agentRow struct {
	widget.BaseWidget

	agentID      uuid.UUID
	avatarCircle *canvas.Circle
	avatarText   *canvas.Text
	nameText     *canvas.Text
	metaText     *canvas.Text
	statusDot    *canvas.Circle

	onSecondaryTap func(id uuid.UUID, pos fyne.Position)
}

func newAgentRow() *agentRow {
	r := &agentRow{
		avatarCircle: canvas.NewCircle(color.NRGBA{R: 55, G: 60, B: 90, A: 255}),
		avatarText:   canvas.NewText("", color.White),
		nameText:     canvas.NewText("", color.White),
		metaText:     canvas.NewText("", color.NRGBA{R: 140, G: 145, B: 175, A: 255}),
		statusDot:    canvas.NewCircle(color.NRGBA{R: 100, G: 210, B: 80, A: 200}),
	}
	r.avatarText.Alignment = fyne.TextAlignCenter
	r.avatarText.TextSize = 18
	r.nameText.TextSize = 13
	r.nameText.TextStyle = fyne.TextStyle{Bold: true}
	r.metaText.TextSize = 11
	r.ExtendBaseWidget(r)
	return r
}

func (r *agentRow) update(a *models.Agent) {
	r.agentID = a.ID
	r.avatarText.Text = a.Avatar
	r.nameText.Text = a.Name
	r.metaText.Text = agentSubtitle(a)
	r.statusDot.FillColor = agentStatusDotColor(a.Status)
	r.Refresh()
}

func (r *agentRow) MinSize() fyne.Size {
	return fyne.NewSize(0, agentRowH)
}

func (r *agentRow) CreateRenderer() fyne.WidgetRenderer {
	return &agentRowRenderer{row: r}
}

func (r *agentRow) SecondaryTapped(ev *fyne.PointEvent) {
	if r.onSecondaryTap != nil {
		r.onSecondaryTap(r.agentID, ev.AbsolutePosition)
	}
}

type agentRowRenderer struct {
	row *agentRow
}

func (rr *agentRowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{
		rr.row.avatarCircle,
		rr.row.avatarText,
		rr.row.nameText,
		rr.row.metaText,
		rr.row.statusDot,
	}
}

func (rr *agentRowRenderer) Layout(size fyne.Size) {
	p := agentRowPad
	s := agentAvatarSize

	// Avatar circle, vertically centered
	avatarY := (size.Height - s) / 2
	rr.row.avatarCircle.Move(fyne.NewPos(p, avatarY))
	rr.row.avatarCircle.Resize(fyne.NewSize(s, s))

	// Avatar emoji centered in the circle
	rr.row.avatarText.Move(fyne.NewPos(p, avatarY))
	rr.row.avatarText.Resize(fyne.NewSize(s, s))

	// Status dot, vertically centered on the right
	dotX := size.Width - agentRowDot - p
	dotY := (size.Height - agentRowDot) / 2
	rr.row.statusDot.Move(fyne.NewPos(dotX, dotY))
	rr.row.statusDot.Resize(fyne.NewSize(agentRowDot, agentRowDot))

	// Name + meta text stacked in the middle
	textX := p + s + p
	textW := dotX - textX - p
	nameH := float32(16)
	metaH := float32(13)
	gap := float32(2)
	totalH := nameH + gap + metaH
	nameY := (size.Height - totalH) / 2

	rr.row.nameText.Move(fyne.NewPos(textX, nameY))
	rr.row.nameText.Resize(fyne.NewSize(textW, nameH))

	rr.row.metaText.Move(fyne.NewPos(textX, nameY+nameH+gap))
	rr.row.metaText.Resize(fyne.NewSize(textW, metaH))
}

func (rr *agentRowRenderer) MinSize() fyne.Size {
	return fyne.NewSize(0, agentRowH)
}

func (rr *agentRowRenderer) Refresh() {
	rr.row.avatarCircle.Refresh()
	rr.row.avatarText.Refresh()
	rr.row.nameText.Refresh()
	rr.row.metaText.Refresh()
	rr.row.statusDot.Refresh()
}

func (rr *agentRowRenderer) Destroy() {}

// --- helpers ---

func agentStatusDotColor(s models.AgentStatus) color.NRGBA {
	switch s {
	case models.AgentStatusRunning:
		return color.NRGBA{R: 255, G: 165, B: 0, A: 255}
	case models.AgentStatusInput:
		return color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	case models.AgentStatusError:
		return color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	default:
		return color.NRGBA{R: 100, G: 210, B: 80, A: 200}
	}
}

// agentSubtitle builds the second line of the agent row.
// Shows cwd last-component, falling back to working folder.
func agentSubtitle(a *models.Agent) string {
	cwd := a.WorkingFolder
	if cwd == "" {
		cwd = a.Folder
	}
	parts := []string{}
	if cwd != "" {
		parts = append(parts, filepath.Base(cwd))
	}
	if a.TerminalTitle != "" && a.TerminalTitle != cwd {
		parts = append(parts, a.TerminalTitle)
	}
	return strings.Join(parts, "  •  ")
}

func gitStatsText(gs models.GitStats) string {
	if gs.Files == 0 {
		return ""
	}
	return fmt.Sprintf("+%d -%d", gs.Insertions, gs.Deletions)
}

// shortenPath returns the last two path components.
func shortenPath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(strings.TrimRight(p, "/"), "/")
	if len(parts) <= 2 {
		return p
	}
	return "…/" + parts[len(parts)-2] + "/" + parts[len(parts)-1]
}
