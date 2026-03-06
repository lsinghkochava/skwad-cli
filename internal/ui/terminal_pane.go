package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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
	focused   bool

	// OnFocus is called when the user taps this pane's header to focus it.
	OnFocus func(paneIndex int)

	headerBg *canvas.Rectangle
	header   *widget.Label
	content  *widget.RichText
	outer    *fyne.Container
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

	tp.headerBg = canvas.NewRectangle(tp.headerColor())
	styledHeader := newTappableBox(
		container.NewStack(tp.headerBg, container.NewPadded(tp.header)),
		func() {
			if tp.OnFocus != nil {
				tp.OnFocus(tp.paneIndex)
			}
		},
	)

	scroll := container.NewScroll(tp.content)
	tp.outer = container.NewBorder(styledHeader, nil, nil, nil, scroll)
}

// SetFocused updates the focused state and refreshes the header color.
func (tp *TerminalPane) SetFocused(f bool) {
	tp.focused = f
	if tp.headerBg != nil {
		tp.headerBg.FillColor = tp.headerColor()
		tp.headerBg.Refresh()
	}
}

func (tp *TerminalPane) headerColor() color.NRGBA {
	if tp.focused {
		// Blue accent — indicates this pane receives sidebar agent selections
		return color.NRGBA{R: 30, G: 50, B: 100, A: 255}
	}
	return color.NRGBA{R: 26, G: 29, B: 46, A: 255}
}

// SetAgentID assigns an agent to this pane and refreshes the display.
func (tp *TerminalPane) SetAgentID(id uuid.UUID) {
	tp.agentID = id
	tp.Refresh()
}

// Refresh reloads content for the currently assigned agent.
func (tp *TerminalPane) Refresh() {
	if tp.agentID == (uuid.UUID{}) {
		tp.header.SetText("No agent — click here to focus, then select an agent in the sidebar")
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

// --- tappableBox: a container wrapper that responds to Tapped ---

type tappableBox struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTappableBox(content fyne.CanvasObject, onTap func()) *tappableBox {
	b := &tappableBox{content: content, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *tappableBox) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *tappableBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}
