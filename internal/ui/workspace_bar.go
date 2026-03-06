package ui

import (
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
)

const (
	workspaceBarWidth float32 = 52
	wsBadgeSize       float32 = 32
	wsDotSize         float32 = 8
)

// WorkspaceBar is the vertical strip on the far left showing workspace badges.
type WorkspaceBar struct {
	manager    *agent.Manager
	window     fyne.Window
	vbox       *fyne.Container
	container  *fyne.Container
	OnSettings func() // called when the settings gear is tapped
}

// NewWorkspaceBar creates the workspace bar.
func NewWorkspaceBar(mgr *agent.Manager) *WorkspaceBar {
	wb := &WorkspaceBar{manager: mgr}
	wb.build()
	return wb
}

func (wb *WorkspaceBar) build() {
	bg := canvas.NewRectangle(color.NRGBA{R: 20, G: 22, B: 35, A: 255})
	bg.SetMinSize(fyne.NewSize(workspaceBarWidth, 1)) // forces the Border layout to allocate full bar width
	wb.vbox = container.NewVBox(wb.items()...)
	wb.container = container.NewStack(bg, wb.vbox)
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
			func(id uuid.UUID) { wb.manager.SetActiveWorkspace(id) },
			func(id uuid.UUID, pos fyne.Position) { wb.showContextMenu(id, pos) },
		)
		items = append(items, badge)
	}

	items = append(items, newAddWorkspaceButton(func() { wb.showNewWorkspaceDialog() }))

	// Settings gear at the very bottom, pinned via a spacer.
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, 8))
	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		if wb.OnSettings != nil {
			wb.OnSettings()
		}
	})
	items = append(items, spacer, settingsBtn)
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
		fyne.NewMenuItem("Rename…", func() { wb.showRenameDialog(wsID) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Delete", func() { wb.confirmDelete(wsID) }),
	}
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, wb.window.Canvas(), pos)
}

func (wb *WorkspaceBar) showRenameDialog(wsID uuid.UUID) {
	if wb.window == nil {
		return
	}
	var current string
	for _, ws := range wb.manager.Workspaces() {
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
	wb.vbox.Objects = wb.items()
	wb.vbox.Refresh()
}

// Widget returns the Fyne widget for embedding in the layout.
func (wb *WorkspaceBar) Widget() fyne.CanvasObject {
	return wb.container
}

// --- workspaceBadge ---

// workspaceBadge is a circular tappable badge for one workspace.
type workspaceBadge struct {
	widget.BaseWidget

	wsID     uuid.UUID
	name     string
	colorHex string
	active   bool
	agents   []*models.Agent

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
	b := &workspaceBadge{
		wsID:     ws.ID,
		name:     ws.Name,
		colorHex: ws.ColorHex,
		active:   active,
		agents:   agents,
		onTap:    onTap,
		onMenu:   onMenu,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *workspaceBadge) MinSize() fyne.Size {
	return fyne.NewSize(workspaceBarWidth, workspaceBarWidth)
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
	bgColor := parseHexColor(b.colorHex)
	if !b.active {
		bgColor = color.NRGBA{R: 132, G: 140, B: 175, A: 255} // #848CAF
	}

	circle := canvas.NewCircle(bgColor)

	lbl := canvas.NewText(initials(b.name), color.White)
	lbl.TextSize = 14
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	lbl.Alignment = fyne.TextAlignCenter

	dot := canvas.NewCircle(workspaceStatusColor(b.agents))

	return &workspaceBadgeRenderer{badge: b, circle: circle, label: lbl, dot: dot}
}

type workspaceBadgeRenderer struct {
	badge  *workspaceBadge
	circle *canvas.Circle
	label  *canvas.Text
	dot    *canvas.Circle
}

func (r *workspaceBadgeRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.circle, r.label, r.dot}
}

func (r *workspaceBadgeRenderer) Layout(size fyne.Size) {
	cx := (size.Width - wsBadgeSize) / 2
	cy := (size.Height - wsBadgeSize) / 2

	r.circle.Move(fyne.NewPos(cx, cy))
	r.circle.Resize(fyne.NewSize(wsBadgeSize, wsBadgeSize))

	r.label.Move(fyne.NewPos(cx, cy))
	r.label.Resize(fyne.NewSize(wsBadgeSize, wsBadgeSize))

	// Status dot at bottom-right of the circle, offset outward by 2px
	r.dot.Move(fyne.NewPos(cx+wsBadgeSize-wsDotSize+2, cy+wsBadgeSize-wsDotSize+2))
	r.dot.Resize(fyne.NewSize(wsDotSize, wsDotSize))
}

func (r *workspaceBadgeRenderer) MinSize() fyne.Size {
	return fyne.NewSize(workspaceBarWidth, workspaceBarWidth)
}

func (r *workspaceBadgeRenderer) Refresh() {
	bgColor := parseHexColor(r.badge.colorHex)
	if !r.badge.active {
		bgColor = color.NRGBA{R: 132, G: 140, B: 175, A: 255}
	}
	r.circle.FillColor = bgColor
	r.label.Text = initials(r.badge.name)
	r.dot.FillColor = workspaceStatusColor(r.badge.agents)
	r.circle.Refresh()
	r.label.Refresh()
	r.dot.Refresh()
}

func (r *workspaceBadgeRenderer) Destroy() {}

// --- addWorkspaceButton ---

type addWorkspaceButton struct {
	widget.BaseWidget
	onTap func()
}

func newAddWorkspaceButton(onTap func()) *addWorkspaceButton {
	b := &addWorkspaceButton{onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *addWorkspaceButton) MinSize() fyne.Size {
	return fyne.NewSize(workspaceBarWidth, workspaceBarWidth)
}

func (b *addWorkspaceButton) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *addWorkspaceButton) CreateRenderer() fyne.WidgetRenderer {
	circle := canvas.NewCircle(color.NRGBA{R: 50, G: 55, B: 75, A: 180})
	lbl := canvas.NewText("+", color.NRGBA{R: 180, G: 185, B: 210, A: 255})
	lbl.TextSize = 20
	lbl.Alignment = fyne.TextAlignCenter
	return &addBtnRenderer{circle: circle, label: lbl}
}

type addBtnRenderer struct {
	circle *canvas.Circle
	label  *canvas.Text
}

func (r *addBtnRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.circle, r.label}
}

func (r *addBtnRenderer) Layout(size fyne.Size) {
	cx := (size.Width - wsBadgeSize) / 2
	cy := (size.Height - wsBadgeSize) / 2
	r.circle.Move(fyne.NewPos(cx, cy))
	r.circle.Resize(fyne.NewSize(wsBadgeSize, wsBadgeSize))
	r.label.Move(fyne.NewPos(cx, cy))
	r.label.Resize(fyne.NewSize(wsBadgeSize, wsBadgeSize))
}

func (r *addBtnRenderer) MinSize() fyne.Size {
	return fyne.NewSize(workspaceBarWidth, workspaceBarWidth)
}

func (r *addBtnRenderer) Refresh() {}
func (r *addBtnRenderer) Destroy() {}

// --- helpers ---

func initials(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return "?"
	}
	return strings.ToUpper(string(runes[0:1]))
}

// parseHexColor converts an "#RRGGBB" string to color.NRGBA.
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

// workspaceStatusColor returns the status dot color for the worst agent status.
func workspaceStatusColor(agents []*models.Agent) color.NRGBA {
	switch models.WorstStatus(agents) {
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
