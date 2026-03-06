package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/terminal"
)

// TerminalPane is a single pane slot that hosts one agent's terminal output.
//
// On Linux with VTE enabled, this pane coordinates an embedded GTK terminal
// overlay window. Until VTE is wired, it shows the most recent captured
// terminal output (ANSI-stripped) in a scrollable text view so the user can
// see what the agent is doing.
type TerminalPane struct {
	paneIndex int
	manager   *agent.Manager
	pool      *terminal.Pool
	agentID   uuid.UUID

	header  *widget.Label
	content *widget.RichText
	outer   *fyne.Container
}

// NewTerminalPane creates a pane for the given pane index.
func NewTerminalPane(paneIndex int, mgr *agent.Manager, pool *terminal.Pool) *TerminalPane {
	tp := &TerminalPane{
		paneIndex: paneIndex,
		manager:   mgr,
		pool:      pool,
	}
	tp.build()
	return tp
}

func (tp *TerminalPane) build() {
	tp.header = widget.NewLabelWithStyle("No agent", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true, Bold: true})
	tp.content = widget.NewRichTextWithText("")
	tp.content.Wrapping = fyne.TextWrapWord

	scroll := container.NewScroll(tp.content)
	tp.outer = container.NewBorder(tp.header, nil, nil, nil, scroll)
}

// SetAgentID assigns an agent to this pane and refreshes the display.
func (tp *TerminalPane) SetAgentID(id uuid.UUID) {
	tp.agentID = id
	tp.Refresh()
}

// Refresh reloads content for the currently assigned agent.
func (tp *TerminalPane) Refresh() {
	if tp.agentID == (uuid.UUID{}) {
		tp.header.SetText("No agent")
		tp.content.ParseMarkdown("")
		return
	}
	a, ok := tp.manager.Agent(tp.agentID)
	if !ok {
		tp.header.SetText("Agent not found")
		tp.content.ParseMarkdown("")
		return
	}

	title := a.Name
	if a.TerminalTitle != "" {
		title += "  —  " + a.TerminalTitle
	}
	tp.header.SetText(title)

	// Display the last captured output as a code block.
	if tp.pool != nil {
		output := tp.pool.LastOutput(tp.agentID)
		if output != "" {
			tp.content.ParseMarkdown("```\n" + output + "\n```")
		} else {
			tp.content.ParseMarkdown("*Waiting for output…*")
		}
	}
}

// Widget returns the Fyne canvas object for this pane.
func (tp *TerminalPane) Widget() fyne.CanvasObject {
	return tp.outer
}
