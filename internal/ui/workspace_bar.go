package ui

import (
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
)

const workspaceBarWidth float32 = 48

// WorkspaceBar is the vertical strip on the far left showing workspace badges.
type WorkspaceBar struct {
	manager   *agent.Manager
	window    fyne.Window // set by App after construction
	container *fyne.Container
}

// NewWorkspaceBar creates the workspace bar.
func NewWorkspaceBar(mgr *agent.Manager) *WorkspaceBar {
	wb := &WorkspaceBar{manager: mgr}
	wb.build()
	return wb
}

func (wb *WorkspaceBar) build() {
	wb.container = container.NewVBox(wb.items()...)
}

func (wb *WorkspaceBar) items() []fyne.CanvasObject {
	workspaces := wb.manager.Workspaces()
	activeWS := wb.manager.ActiveWorkspace()

	var items []fyne.CanvasObject
	for _, ws := range workspaces {
		ws := ws // capture
		active := activeWS != nil && ws.ID == activeWS.ID
		agents := wb.wsAgents(ws)
		badge := newWorkspaceBadge(ws, active, agents,
			func(id uuid.UUID) {
				wb.manager.SetActiveWorkspace(id)
			},
			func(id uuid.UUID, pos fyne.Position) {
				wb.showContextMenu(id, pos)
			},
		)
		items = append(items, badge)
	}

	addBtn := widget.NewButton("+", func() {
		wb.showNewWorkspaceDialog()
	})
	items = append(items, addBtn)
	return items
}

// showNewWorkspaceDialog prompts the user to create a workspace.
func (wb *WorkspaceBar) showNewWorkspaceDialog() {
	if wb.window == nil {
		return
	}
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Workspace name")
	dialog.ShowCustomConfirm("New Workspace", "Create", "Cancel", entry,
		func(ok bool) {
			if !ok || strings.TrimSpace(entry.Text) == "" {
				return
			}
			colorIdx := len(wb.manager.Workspaces()) % len(models.WorkspaceColors)
			ws := &models.Workspace{
				ID:                  uuid.New(),
				Name:                strings.TrimSpace(entry.Text),
				ColorHex:            models.WorkspaceColors[colorIdx],
				LayoutMode:          models.LayoutModeSingle,
				SplitRatio:          0.5,
				SplitRatioSecondary: 0.5,
			}
			wb.manager.AddWorkspace(ws)
			wb.manager.SetActiveWorkspace(ws.ID)
		}, wb.window)
}

// showContextMenu shows a right-click menu for a workspace badge.
func (wb *WorkspaceBar) showContextMenu(wsID uuid.UUID, pos fyne.Position) {
	if wb.window == nil {
		return
	}
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Rename…", func() {
			wb.showRenameDialog(wsID)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Delete", func() {
			wb.confirmDelete(wsID)
		}),
	}
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, wb.window.Canvas(), pos)
}

func (wb *WorkspaceBar) showRenameDialog(wsID uuid.UUID) {
	if wb.window == nil {
		return
	}
	workspaces := wb.manager.Workspaces()
	var current string
	for _, ws := range workspaces {
		if ws.ID == wsID {
			current = ws.Name
			break
		}
	}
	entry := widget.NewEntry()
	entry.SetText(current)
	dialog.ShowCustomConfirm("Rename Workspace", "Rename", "Cancel", entry,
		func(ok bool) {
			if !ok || strings.TrimSpace(entry.Text) == "" {
				return
			}
			wb.manager.UpdateWorkspace(wsID, func(w *models.Workspace) {
				w.Name = strings.TrimSpace(entry.Text)
			})
		}, wb.window)
}

func (wb *WorkspaceBar) confirmDelete(wsID uuid.UUID) {
	if wb.window == nil {
		return
	}
	if len(wb.manager.Workspaces()) <= 1 {
		dialog.ShowInformation("Cannot Delete", "You must have at least one workspace.", wb.window)
		return
	}
	dialog.ShowConfirm("Delete Workspace",
		"Delete this workspace? Agents will not be removed.",
		func(ok bool) {
			if ok {
				wb.manager.RemoveWorkspace(wsID)
			}
		}, wb.window)
}

// wsAgents returns all agents belonging to the given workspace.
func (wb *WorkspaceBar) wsAgents(ws *models.Workspace) []*models.Agent {
	agents := make([]*models.Agent, 0, len(ws.AgentIDs))
	for _, id := range ws.AgentIDs {
		if a, ok := wb.manager.Agent(id); ok {
			agents = append(agents, a)
		}
	}
	return agents
}

// Refresh rebuilds the workspace bar to reflect current state.
func (wb *WorkspaceBar) Refresh() {
	wb.container.Objects = wb.items()
	wb.container.Refresh()
}

// Widget returns the Fyne widget for embedding in the layout.
func (wb *WorkspaceBar) Widget() fyne.CanvasObject {
	return wb.container
}

// --- workspaceBadge custom widget ---

// workspaceBadge is a tappable badge widget for one workspace entry.
// SecondaryTapped opens the rename/delete context menu.
type workspaceBadge struct {
	widget.BaseWidget

	wsID     uuid.UUID
	name     string
	colorHex string
	active   bool
	agents   []*models.Agent

	bg    *canvas.Rectangle
	label *canvas.Text
	dot   *canvas.Circle

	onTap  func(uuid.UUID)
	onMenu func(uuid.UUID, fyne.Position)
}

func newWorkspaceBadge(
	ws *models.Workspace,
	active bool,
	agents []*models.Agent,
	onTap func(uuid.UUID),
	onMenu func(uuid.UUID, fyne.Position),
) *workspaceBadge {
	bgColor := parseHexColor(ws.ColorHex)
	if !active {
		bgColor = color.NRGBA{R: 70, G: 70, B: 70, A: 255}
	}

	bg := canvas.NewRectangle(bgColor)
	bg.SetMinSize(fyne.NewSize(workspaceBarWidth-8, workspaceBarWidth-8))
	bg.CornerRadius = 6

	lbl := canvas.NewText(initials(ws.Name), color.White)
	lbl.TextSize = 13
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	lbl.Alignment = fyne.TextAlignCenter

	dot := canvas.NewCircle(workspaceStatusColor(agents))
	dot.Resize(fyne.NewSize(6, 6))

	b := &workspaceBadge{
		wsID:     ws.ID,
		name:     ws.Name,
		colorHex: ws.ColorHex,
		active:   active,
		agents:   agents,
		bg:       bg,
		label:    lbl,
		dot:      dot,
		onTap:    onTap,
		onMenu:   onMenu,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *workspaceBadge) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap(b.wsID)
	}
}

func (b *workspaceBadge) SecondaryTapped(ev *fyne.PointEvent) {
	if b.onMenu != nil {
		b.onMenu(b.wsID, ev.AbsolutePosition)
	}
}

func (b *workspaceBadge) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(
		container.NewStack(b.bg, container.NewCenter(b.label)),
	)
}

// --- helpers ---

func initials(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return "?"
	}
	return strings.ToUpper(string(runes[0:1]))
}

// parseHexColor converts an "#RRGGBB" string to color.NRGBA.
// Returns a fallback blue if the string is malformed.
func parseHexColor(hex string) color.NRGBA {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return color.NRGBA{R: 74, G: 144, B: 217, A: 255}
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return color.NRGBA{R: 74, G: 144, B: 217, A: 255}
	}
	return color.NRGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 255,
	}
}

// workspaceStatusColor returns the dot color for the worst agent status.
func workspaceStatusColor(agents []*models.Agent) color.NRGBA {
	switch models.WorstStatus(agents) {
	case models.AgentStatusRunning:
		return color.NRGBA{R: 255, G: 165, B: 0, A: 255}
	case models.AgentStatusInput:
		return color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	case models.AgentStatusError:
		return color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	default:
		return color.NRGBA{R: 100, G: 210, B: 80, A: 180}
	}
}
