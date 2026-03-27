package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/terminal"
	"github.com/taigrr/bubbleterm"
)

// centralTickMsg drives the 30fps render loop for all terminal emulators.
type centralTickMsg struct{}

var (
	borderNav = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555555"))

	borderIns = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00CC00"))
)

// poolWriter implements io.WriteCloser and forwards writes to Pool.SendText.
type poolWriter struct {
	pool    *terminal.Pool
	agentID uuid.UUID
}

func (pw *poolWriter) Write(p []byte) (int, error) {
	pw.pool.SendText(pw.agentID, string(p))
	return len(p), nil
}

func (pw *poolWriter) Close() error { return nil }

// TerminalPane manages one bubbleterm.Model per agent and feeds data via io.Pipe.
type TerminalPane struct {
	terminals map[uuid.UUID]*bubbleterm.Model
	writers   map[uuid.UUID]io.Writer // pipeWriter per agent
	dataChs   map[uuid.UUID]chan []byte
	active    uuid.UUID
	pool      *terminal.Pool
	width     int
	height    int
	mode      Mode

	mu sync.Mutex
}

// NewTerminalPane creates a terminal pane with one bubbleterm per agent.
func NewTerminalPane(agents []*models.Agent, pool *terminal.Pool, width, height int) *TerminalPane {
	// Account for border (2 chars width, 2 chars height).
	contentW := width - 2
	contentH := height - 2
	if contentW < 10 {
		contentW = 10
	}
	if contentH < 5 {
		contentH = 5
	}

	tp := &TerminalPane{
		terminals: make(map[uuid.UUID]*bubbleterm.Model),
		writers:   make(map[uuid.UUID]io.Writer),
		dataChs:   make(map[uuid.UUID]chan []byte),
		pool:      pool,
		width:     width,
		height:    height,
	}

	for _, a := range agents {
		pr, pw := io.Pipe()
		dataCh := make(chan []byte, 256)

		// Buffer goroutine: reads from channel, writes to pipe.
		go func(ch chan []byte, w *io.PipeWriter) {
			for data := range ch {
				_, _ = w.Write(data)
			}
			w.Close()
		}(dataCh, pw)

		writer := &poolWriter{pool: pool, agentID: a.ID}
		model, err := bubbleterm.NewWithPipes(contentW, contentH, pr, writer)
		if err != nil {
			continue
		}
		model.SetAutoPoll(false)

		tp.terminals[a.ID] = model
		tp.writers[a.ID] = pw
		tp.dataChs[a.ID] = dataCh
	}

	if len(agents) > 0 {
		tp.active = agents[0].ID
		if t, ok := tp.terminals[tp.active]; ok {
			t.Focus()
		}
	}

	return tp
}

// Init returns the init commands for all terminal models.
func (tp *TerminalPane) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range tp.terminals {
		cmds = append(cmds, t.Init())
	}
	// Start the centralized tick for rendering.
	cmds = append(cmds, tea.Tick(time.Millisecond*33, func(time.Time) tea.Msg {
		return centralTickMsg{}
	}))
	return tea.Batch(cmds...)
}

// FeedOutput sends data to the correct agent's buffer channel (non-blocking).
func (tp *TerminalPane) FeedOutput(agentID uuid.UUID, data []byte) {
	tp.mu.Lock()
	ch, ok := tp.dataChs[agentID]
	tp.mu.Unlock()
	if !ok {
		return
	}
	// Copy data — the underlying buffer is reused by readLoop.
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	select {
	case ch <- dataCopy:
	default:
		// Drop if full — display buffer, not recording.
	}
}

// SetActive switches the focused terminal.
func (tp *TerminalPane) SetActive(agentID uuid.UUID) {
	if tp.active == agentID {
		return
	}
	// Blur old.
	if t, ok := tp.terminals[tp.active]; ok {
		t.Blur()
	}
	tp.active = agentID
	// Focus new.
	if t, ok := tp.terminals[tp.active]; ok {
		t.Focus()
	}
}

// ActiveAgent returns the currently active agent ID.
func (tp *TerminalPane) ActiveAgent() uuid.UUID {
	return tp.active
}

// SetMode updates the border style indicator.
func (tp *TerminalPane) SetMode(m Mode) {
	tp.mode = m
}

// Update handles messages for the active terminal.
func (tp *TerminalPane) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg.(type) {
	case centralTickMsg:
		// Poll all terminals for new frames.
		for id, t := range tp.terminals {
			cmd := t.UpdateTerminal()
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			_ = id
		}
		// Schedule next tick.
		cmds = append(cmds, tea.Tick(time.Millisecond*33, func(time.Time) tea.Msg {
			return centralTickMsg{}
		}))
		return tea.Batch(cmds...)

	case tea.KeyPressMsg, tea.KeyMsg:
		// Forward keyboard to active terminal only.
		t, ok := tp.terminals[tp.active]
		if !ok {
			return nil
		}
		updated, cmd := t.Update(msg)
		tp.terminals[tp.active] = updated.(*bubbleterm.Model)
		return cmd
	}

	// Forward other bubbleterm messages to all terminals.
	for id, t := range tp.terminals {
		updated, cmd := t.Update(msg)
		tp.terminals[id] = updated.(*bubbleterm.Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Resize updates terminal dimensions for all agents.
func (tp *TerminalPane) Resize(width, height int) tea.Cmd {
	tp.width = width
	tp.height = height

	// Content area inside borders.
	contentW := width - 2
	contentH := height - 2
	if contentW < 10 {
		contentW = 10
	}
	if contentH < 5 {
		contentH = 5
	}

	var cmds []tea.Cmd
	for id, t := range tp.terminals {
		cmds = append(cmds, t.Resize(contentW, contentH))
		// Also resize the actual PTY for all agents.
		tp.pool.Resize(id, uint16(contentW), uint16(contentH))
	}
	return tea.Batch(cmds...)
}

// View renders the active terminal with a mode-aware border.
func (tp *TerminalPane) View(width, height int) string {
	t, ok := tp.terminals[tp.active]
	if !ok {
		return ""
	}

	v := t.View()
	content := v.Content

	// Truncate content lines to fit inside the border.
	contentH := height - 2
	if contentH < 1 {
		contentH = 1
	}
	lines := strings.Split(content, "\n")
	if len(lines) > contentH {
		lines = lines[:contentH]
	}
	content = strings.Join(lines, "\n")

	// Choose border style based on mode.
	style := borderNav
	modeLabel := " NAV "
	if tp.mode == ModeInsert {
		style = borderIns
		modeLabel = " INS "
	}

	rendered := style.Render(content)

	// Inject mode label into top-right of border.
	renderedLines := strings.Split(rendered, "\n")
	if len(renderedLines) > 0 {
		topLine := renderedLines[0]
		labelStyled := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#555555")).
			Render(modeLabel)
		if tp.mode == ModeInsert {
			labelStyled = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#00CC00")).
				Render(modeLabel)
		}
		// Place label near the right end of the top border.
		insertPos := len(topLine) - len(modeLabel) - 2
		if insertPos > 0 && insertPos < len(topLine) {
			renderedLines[0] = topLine[:insertPos] + labelStyled + topLine[insertPos+len(modeLabel):]
		}
	}

	return strings.Join(renderedLines, "\n")
}

// ModeLabel returns the current mode label for display purposes.
func (tp *TerminalPane) ModeLabel() string {
	if tp.mode == ModeInsert {
		return fmt.Sprintf("[INS]")
	}
	return fmt.Sprintf("[NAV]")
}

// Close shuts down all terminal models and data channels.
func (tp *TerminalPane) Close() {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for id, ch := range tp.dataChs {
		close(ch)
		delete(tp.dataChs, id)
	}
	for _, t := range tp.terminals {
		t.Close()
	}
}
