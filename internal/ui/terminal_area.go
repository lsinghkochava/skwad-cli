package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/terminal"
)

// SVG icons for the layout toolbar — white outlines on transparent background.
var (
	iconLayoutSingle = fyne.NewStaticResource("layout-single.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
			`<rect x="1" y="1" width="18" height="18" rx="2" fill="none" stroke="white" stroke-width="1.5"/>` +
			`</svg>`))
	iconLayoutSplitV = fyne.NewStaticResource("layout-splitv.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
			`<rect x="1" y="1" width="8" height="18" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="11" y="1" width="8" height="18" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`</svg>`))
	iconLayoutSplitH = fyne.NewStaticResource("layout-splith.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
			`<rect x="1" y="1" width="18" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="1" y="11" width="18" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`</svg>`))
	iconLayoutThree = fyne.NewStaticResource("layout-three.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
			`<rect x="1" y="1" width="8" height="18" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="11" y="1" width="8" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="11" y="11" width="8" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`</svg>`))
	iconLayoutFour = fyne.NewStaticResource("layout-four.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
			`<rect x="1" y="1" width="8" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="11" y="1" width="8" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="1" y="11" width="8" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`<rect x="11" y="11" width="8" height="8" rx="1.5" fill="none" stroke="white" stroke-width="1.5"/>` +
			`</svg>`))
)

const (
	gitPanelSplitOffset      = 0.65 // 65% terminal, 35% git panel
	markdownPanelSplitOffset = 0.60 // 60% terminal, 40% markdown panel
	mermaidPanelSplitOffset  = 0.60 // 60% terminal, 40% mermaid panel
)

// TerminalArea manages the main content area with split-pane layout.
//
// NOTE on VTE embedding: because VTE widgets are native GTK widgets, they
// cannot be placed directly into a Fyne container. Instead, each TerminalPane
// holds a placeholder Fyne widget that tracks its position/size; the actual
// VTE window is a sibling X11 window kept in sync with those bounds.
// See internal/terminal/vte.go for the embedding strategy details.
type TerminalArea struct {
	manager        *agent.Manager
	pool           *terminal.Pool
	container      *fyne.Container // outer (toolbar + panes)
	panesContainer *fyne.Container // inner (refreshed on layout change)
	toolbarTitle   *canvas.Text    // updated on Refresh

	gitPanel      *GitPanel
	markdownPanel *MarkdownPanel
	mermaidPanel  *MermaidPanel

	showGit      bool
	showMarkdown bool
	showMermaid  bool
}

// NewTerminalArea creates the terminal area.
func NewTerminalArea(mgr *agent.Manager, pool *terminal.Pool) *TerminalArea {
	ta := &TerminalArea{
		manager:       mgr,
		pool:          pool,
		gitPanel:      NewGitPanel(mgr),
		markdownPanel: NewMarkdownPanel(),
		mermaidPanel:  NewMermaidPanel(),
	}
	ta.build()

	// Refresh the pane that shows this agent whenever new output arrives.
	if pool != nil {
		pool.OnRawOutput = func(agentID uuid.UUID) {
			ta.refreshPaneForAgent(agentID)
		}
	}
	return ta
}

func (ta *TerminalArea) build() {
	ta.panesContainer = container.NewStack(ta.panes())
	toolbar := ta.buildLayoutToolbar()
	ta.container = container.NewBorder(toolbar, nil, nil, nil, ta.panesContainer)
}

// buildLayoutToolbar returns a toolbar with layout-mode icon buttons.
func (ta *TerminalArea) buildLayoutToolbar() fyne.CanvasObject {
	ta.toolbarTitle = canvas.NewText(ta.activeAgentTitle(), color.NRGBA{R: 190, G: 195, B: 215, A: 255})
	ta.toolbarTitle.TextSize = 12

	layouts := []struct {
		icon fyne.Resource
		mode models.LayoutMode
	}{
		{iconLayoutSingle, models.LayoutModeSingle},
		{iconLayoutSplitV, models.LayoutModeSplitVertical},
		{iconLayoutSplitH, models.LayoutModeSplitHorizontal},
		{iconLayoutThree, models.LayoutModeThreePane},
		{iconLayoutFour, models.LayoutModeGridFourPane},
	}

	var btns []fyne.CanvasObject
	for _, l := range layouts {
		l := l // capture
		btn := widget.NewButtonWithIcon("", l.icon, func() {
			ws := ta.manager.ActiveWorkspace()
			if ws == nil {
				return
			}
			ta.manager.UpdateWorkspace(ws.ID, func(w *models.Workspace) {
				w.LayoutMode = l.mode
				// Ensure enough agent slots for the new pane count.
				need := l.mode.PaneCount()
				for len(w.ActiveAgentIDs) < need && len(w.AgentIDs) > len(w.ActiveAgentIDs) {
					w.ActiveAgentIDs = append(w.ActiveAgentIDs, w.AgentIDs[len(w.ActiveAgentIDs)])
				}
			})
			ta.Refresh()
		})
		btns = append(btns, btn)
	}

	bg := canvas.NewRectangle(color.NRGBA{R: 26, G: 29, B: 46, A: 255})
	bg.SetMinSize(fyne.NewSize(0, 34))
	titlePad := container.NewPadded(ta.toolbarTitle)
	row := container.NewBorder(nil, nil, titlePad, container.NewHBox(btns...), nil)
	return container.NewStack(bg, row)
}

// activeAgentTitle returns a display string for the focused agent.
func (ta *TerminalArea) activeAgentTitle() string {
	ws := ta.manager.ActiveWorkspace()
	if ws == nil || len(ws.ActiveAgentIDs) == 0 {
		return ""
	}
	a, ok := ta.manager.Agent(ws.ActiveAgentIDs[0])
	if !ok {
		return ""
	}
	if a.TerminalTitle != "" {
		return a.Name + "  →  " + a.TerminalTitle
	}
	return a.Name
}

// panes builds the full content tree: terminal layout optionally wrapped
// with the git panel (below), markdown panel (right), and/or mermaid panel (right).
func (ta *TerminalArea) panes() fyne.CanvasObject {
	ws := ta.manager.ActiveWorkspace()
	if ws == nil {
		return container.NewStack()
	}

	terminals := ta.buildLayout(ws)

	// Build right-side panel column: markdown and/or mermaid stacked vertically.
	var rightPanel fyne.CanvasObject
	if ta.showMarkdown && ta.showMermaid {
		rightPanel = container.NewVSplit(ta.markdownPanel.Widget(), ta.mermaidPanel.Widget())
	} else if ta.showMarkdown {
		rightPanel = ta.markdownPanel.Widget()
	} else if ta.showMermaid {
		rightPanel = ta.mermaidPanel.Widget()
	}

	var content fyne.CanvasObject
	if rightPanel != nil {
		split := container.NewHSplit(terminals, rightPanel)
		split.Offset = markdownPanelSplitOffset
		content = split
	} else {
		content = terminals
	}

	if ta.showGit {
		gitSplit := container.NewVSplit(content, ta.gitPanel.Widget())
		gitSplit.Offset = gitPanelSplitOffset
		return gitSplit
	}
	return content
}

// buildLayout returns the terminal pane layout for the given workspace.
func (ta *TerminalArea) buildLayout(ws *models.Workspace) fyne.CanvasObject {
	switch ws.LayoutMode {
	case models.LayoutModeSplitVertical:
		return ta.splitVertical(ws)
	case models.LayoutModeSplitHorizontal:
		return ta.splitHorizontal(ws)
	case models.LayoutModeThreePane:
		return ta.threePane(ws)
	case models.LayoutModeGridFourPane:
		return ta.gridFourPane(ws)
	default:
		return ta.singlePane(ws)
	}
}

// makePanes creates n TerminalPane instances wired for focus tracking.
func (ta *TerminalArea) makePanes(ws *models.Workspace, count int) []*TerminalPane {
	panes := make([]*TerminalPane, count)
	for i := 0; i < count; i++ {
		panes[i] = NewTerminalPane(i, ta.manager, ta.pool)
		panes[i].SetFocused(i == ws.FocusedPaneIndex)
		if i < len(ws.ActiveAgentIDs) {
			panes[i].SetAgentID(ws.ActiveAgentIDs[i])
		}
		idx := i // capture for closure
		wsID := ws.ID
		panes[i].OnFocus = func(paneIndex int) {
			_ = idx // suppress lint
			ta.manager.UpdateWorkspace(wsID, func(w *models.Workspace) {
				w.FocusedPaneIndex = paneIndex
			})
		}
	}
	return panes
}

func (ta *TerminalArea) singlePane(ws *models.Workspace) fyne.CanvasObject {
	p := ta.makePanes(ws, 1)
	return p[0].Widget()
}

func (ta *TerminalArea) splitVertical(ws *models.Workspace) fyne.CanvasObject {
	p := ta.makePanes(ws, 2)
	split := container.NewHSplit(p[0].Widget(), p[1].Widget())
	split.Offset = ws.SplitRatio
	return split
}

func (ta *TerminalArea) splitHorizontal(ws *models.Workspace) fyne.CanvasObject {
	p := ta.makePanes(ws, 2)
	split := container.NewVSplit(p[0].Widget(), p[1].Widget())
	split.Offset = ws.SplitRatio
	return split
}

func (ta *TerminalArea) threePane(ws *models.Workspace) fyne.CanvasObject {
	p := ta.makePanes(ws, 3)
	rightSplit := container.NewVSplit(p[1].Widget(), p[2].Widget())
	rightSplit.Offset = ws.SplitRatioSecondary
	mainSplit := container.NewHSplit(p[0].Widget(), rightSplit)
	mainSplit.Offset = ws.SplitRatio
	return mainSplit
}

func (ta *TerminalArea) gridFourPane(ws *models.Workspace) fyne.CanvasObject {
	p := ta.makePanes(ws, 4)
	topSplit := container.NewHSplit(p[0].Widget(), p[1].Widget())
	topSplit.Offset = ws.SplitRatio
	botSplit := container.NewHSplit(p[2].Widget(), p[3].Widget())
	botSplit.Offset = ws.SplitRatio
	mainSplit := container.NewVSplit(topSplit, botSplit)
	mainSplit.Offset = ws.SplitRatioSecondary
	return mainSplit
}

// focusedAgentID returns the ID of the agent in the focused pane, if any.
func (ta *TerminalArea) focusedAgentID() (uuid.UUID, bool) {
	ws := ta.manager.ActiveWorkspace()
	if ws == nil || len(ws.ActiveAgentIDs) == 0 {
		return uuid.UUID{}, false
	}
	idx := ws.FocusedPaneIndex
	if idx >= len(ws.ActiveAgentIDs) {
		idx = 0
	}
	return ws.ActiveAgentIDs[idx], true
}

// Refresh rebuilds the pane layout and updates the toolbar title.
func (ta *TerminalArea) Refresh() {
	if ta.toolbarTitle != nil {
		ta.toolbarTitle.Text = ta.activeAgentTitle()
		ta.toolbarTitle.Refresh()
	}
	ta.panesContainer.Objects = []fyne.CanvasObject{ta.panes()}
	ta.panesContainer.Refresh()
}

// refreshPaneForAgent rebuilds the layout if the given agent is currently displayed.
func (ta *TerminalArea) refreshPaneForAgent(agentID uuid.UUID) {
	ws := ta.manager.ActiveWorkspace()
	if ws == nil {
		return
	}
	for _, id := range ws.ActiveAgentIDs {
		if id == agentID {
			ta.Refresh()
			return
		}
	}
}

// Widget returns the terminal area widget.
func (ta *TerminalArea) Widget() fyne.CanvasObject {
	return ta.container
}

// ToggleGitPanel shows or hides the git panel, loading it for the focused agent.
func (ta *TerminalArea) ToggleGitPanel() {
	ta.showGit = !ta.showGit
	if ta.showGit {
		if id, ok := ta.focusedAgentID(); ok {
			ta.gitPanel.SetAgent(id)
		}
	}
	ta.Refresh()
}

// ToggleMarkdownPanel shows or hides the markdown panel.
func (ta *TerminalArea) ToggleMarkdownPanel() {
	ta.showMarkdown = !ta.showMarkdown
	ta.Refresh()
}

// ShowMarkdownFile opens a file in the markdown panel and makes it visible.
func (ta *TerminalArea) ShowMarkdownFile(path string) {
	ta.showMarkdown = true
	ta.markdownPanel.ShowFile(path)
	ta.Refresh()
}

// ShowMermaid renders a Mermaid diagram in the dedicated mermaid panel.
func (ta *TerminalArea) ShowMermaid(source, title string) {
	ta.showMermaid = true
	ta.mermaidPanel.Show(source, title)
	ta.Refresh()
}

// ToggleMermaidPanel shows or hides the mermaid panel.
func (ta *TerminalArea) ToggleMermaidPanel() {
	ta.showMermaid = !ta.showMermaid
	ta.Refresh()
}
