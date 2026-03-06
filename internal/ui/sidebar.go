package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/persistence"
)

const (
	sidebarDefaultWidth float32 = 200
	sidebarMinWidth     float32 = 80
	sidebarMaxWidth     float32 = 400
)

// Sidebar lists non-companion agents for the active workspace.
type Sidebar struct {
	manager   *agent.Manager
	list      *widget.List
	container *fyne.Container
	collapsed bool
	width     float32

	// Set by App after construction (same package, so direct field access is fine).
	window fyne.Window
	store  *persistence.Store

	// Callbacks — set by App before the window is shown.
	OnAddAgent       func(a *models.Agent)
	OnRemoveAgent    func(id uuid.UUID)
	OnRestartAgent   func(id uuid.UUID)
	OnDuplicateAgent func(id uuid.UUID)
	OnShowHistory    func(id uuid.UUID)
	OnAddToBench     func(id uuid.UUID)
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
	s.list = widget.NewList(
		func() int { return len(s.manager.Agents()) },
		func() fyne.CanvasObject {
			return newAgentRow()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			agents := s.manager.Agents()
			if id >= len(agents) {
				return
			}
			row := obj.(*agentRow)
			row.agentID = agents[id].ID
			row.onSecondaryTap = s.showContextMenu
			row.update(agents[id], s.width < sidebarDefaultWidth)
		},
	)
	s.list.OnSelected = func(id widget.ListItemID) {
		agents := s.manager.Agents()
		if id >= len(agents) {
			return
		}
		ws := s.manager.ActiveWorkspace()
		if ws == nil {
			return
		}
		selectedID := agents[id].ID
		s.manager.UpdateWorkspace(ws.ID, func(w *models.Workspace) {
			if len(w.ActiveAgentIDs) == 0 {
				w.ActiveAgentIDs = []uuid.UUID{selectedID}
			} else {
				w.ActiveAgentIDs[w.FocusedPaneIndex] = selectedID
			}
		})
	}

	addBtn := widget.NewButton("+ New Agent", func() {
		s.OpenNewAgentSheet()
	})

	s.container = container.NewBorder(nil, addBtn, nil, nil, s.list)
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
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Remove", func() {
			if s.OnRemoveAgent != nil {
				s.OnRemoveAgent(agentID)
			}
		}),
	}
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, s.window.Canvas(), pos)
}

// Refresh rebuilds the list to reflect current state.
func (s *Sidebar) Refresh() {
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

type agentRow struct {
	widget.BaseWidget

	agentID        uuid.UUID
	avatar         *widget.Label
	name           *widget.Label
	status         *widget.Label
	gitStats       *widget.Label
	title          *widget.Label
	onSecondaryTap func(id uuid.UUID, pos fyne.Position)
}

func newAgentRow() *agentRow {
	r := &agentRow{
		avatar:   widget.NewLabel(""),
		name:     widget.NewLabel(""),
		status:   widget.NewLabel(""),
		gitStats: widget.NewLabel(""),
		title:    widget.NewLabel(""),
	}
	r.ExtendBaseWidget(r)
	return r
}

func (r *agentRow) update(a *models.Agent, compact bool) {
	r.avatar.SetText(a.Avatar)
	r.name.SetText(a.Name)
	r.status.SetText(statusIndicator(a.Status))
	r.gitStats.SetText(gitStatsText(a.GitStats))
	if compact {
		r.title.SetText("")
	} else {
		r.title.SetText(a.TerminalTitle)
	}
}

func (r *agentRow) CreateRenderer() fyne.WidgetRenderer {
	if r.gitStats.Text != "" {
		return widget.NewSimpleRenderer(container.NewVBox(
			container.NewHBox(r.status, r.avatar, r.name, r.gitStats),
			r.title,
		))
	}
	return widget.NewSimpleRenderer(container.NewHBox(r.status, r.avatar, r.name))
}

// SecondaryTapped handles right-click / secondary tap on an agent row.
func (r *agentRow) SecondaryTapped(ev *fyne.PointEvent) {
	if r.onSecondaryTap != nil {
		r.onSecondaryTap(r.agentID, ev.AbsolutePosition)
	}
}

func statusIndicator(s models.AgentStatus) string {
	switch s {
	case models.AgentStatusRunning:
		return "🟠"
	case models.AgentStatusInput:
		return "🔴"
	case models.AgentStatusError:
		return "🔴"
	default:
		return "🟢"
	}
}

func gitStatsText(gs models.GitStats) string {
	if gs.Files == 0 {
		return ""
	}
	return fmt.Sprintf("+%d -%d", gs.Insertions, gs.Deletions)
}
