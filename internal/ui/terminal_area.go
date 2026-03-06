package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
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
	manager   *agent.Manager
	container *fyne.Container

	gitPanel      *GitPanel
	markdownPanel *MarkdownPanel
	mermaidPanel  *MermaidPanel

	showGit     bool
	showMarkdown bool
	showMermaid  bool
}

// NewTerminalArea creates the terminal area.
func NewTerminalArea(mgr *agent.Manager) *TerminalArea {
	ta := &TerminalArea{
		manager:       mgr,
		gitPanel:      NewGitPanel(mgr),
		markdownPanel: NewMarkdownPanel(),
		mermaidPanel:  NewMermaidPanel(),
	}
	ta.build()
	return ta
}

func (ta *TerminalArea) build() {
	ta.container = container.NewStack(ta.panes())
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

func (ta *TerminalArea) singlePane(ws *models.Workspace) fyne.CanvasObject {
	pane := NewTerminalPane(0, ta.manager)
	if len(ws.ActiveAgentIDs) > 0 {
		pane.SetAgentID(ws.ActiveAgentIDs[0])
	}
	return pane.Widget()
}

func (ta *TerminalArea) splitVertical(ws *models.Workspace) fyne.CanvasObject {
	left := NewTerminalPane(0, ta.manager)
	right := NewTerminalPane(1, ta.manager)
	if len(ws.ActiveAgentIDs) > 0 {
		left.SetAgentID(ws.ActiveAgentIDs[0])
	}
	if len(ws.ActiveAgentIDs) > 1 {
		right.SetAgentID(ws.ActiveAgentIDs[1])
	}
	split := container.NewHSplit(left.Widget(), right.Widget())
	split.Offset = ws.SplitRatio
	return split
}

func (ta *TerminalArea) splitHorizontal(ws *models.Workspace) fyne.CanvasObject {
	top := NewTerminalPane(0, ta.manager)
	bottom := NewTerminalPane(1, ta.manager)
	if len(ws.ActiveAgentIDs) > 0 {
		top.SetAgentID(ws.ActiveAgentIDs[0])
	}
	if len(ws.ActiveAgentIDs) > 1 {
		bottom.SetAgentID(ws.ActiveAgentIDs[1])
	}
	split := container.NewVSplit(top.Widget(), bottom.Widget())
	split.Offset = ws.SplitRatio
	return split
}

func (ta *TerminalArea) threePane(ws *models.Workspace) fyne.CanvasObject {
	left := NewTerminalPane(0, ta.manager)
	rightTop := NewTerminalPane(1, ta.manager)
	rightBottom := NewTerminalPane(2, ta.manager)
	if len(ws.ActiveAgentIDs) > 0 {
		left.SetAgentID(ws.ActiveAgentIDs[0])
	}
	if len(ws.ActiveAgentIDs) > 1 {
		rightTop.SetAgentID(ws.ActiveAgentIDs[1])
	}
	if len(ws.ActiveAgentIDs) > 2 {
		rightBottom.SetAgentID(ws.ActiveAgentIDs[2])
	}
	rightSplit := container.NewVSplit(rightTop.Widget(), rightBottom.Widget())
	rightSplit.Offset = ws.SplitRatioSecondary
	mainSplit := container.NewHSplit(left.Widget(), rightSplit)
	mainSplit.Offset = ws.SplitRatio
	return mainSplit
}

func (ta *TerminalArea) gridFourPane(ws *models.Workspace) fyne.CanvasObject {
	panes := make([]*TerminalPane, 4)
	for i := range panes {
		panes[i] = NewTerminalPane(i, ta.manager)
		if i < len(ws.ActiveAgentIDs) {
			panes[i].SetAgentID(ws.ActiveAgentIDs[i])
		}
	}
	topSplit := container.NewHSplit(panes[0].Widget(), panes[1].Widget())
	topSplit.Offset = ws.SplitRatio
	botSplit := container.NewHSplit(panes[2].Widget(), panes[3].Widget())
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

// Refresh rebuilds the layout.
func (ta *TerminalArea) Refresh() {
	ta.container.Objects = []fyne.CanvasObject{ta.panes()}
	ta.container.Refresh()
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
