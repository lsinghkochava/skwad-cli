package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// Mode represents the TUI input mode.
type Mode int

const (
	ModeNavigation Mode = iota
	ModeInsert
)

var helpStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#888888")).
	Padding(1, 2)

const helpText = `Keyboard Shortcuts

Navigation Mode:
  q          quit
  i          enter insert mode
  j / ↓      next agent
  k / ↑      previous agent
  tab        cycle next
  shift+tab  cycle previous
  r          restart agent
  ?          toggle help

Insert Mode:
  ESC        back to navigation
  all keys   forwarded to agent`

// App is the root Bubble Tea model for the Skwad TUI.
type App struct {
	daemon    *daemon.Daemon
	sidebar   *Sidebar
	termPane  *TerminalPane
	statusBar *StatusBar

	mode     Mode
	width    int
	height   int
	showHelp bool

	statusCh chan StatusMsg

	quitting bool
}

// New creates a new TUI App wired to the daemon.
func New(d *daemon.Daemon, agents []*models.Agent) *App {
	statusCh := make(chan StatusMsg, 64)

	mcpURL := ""
	if d.MCPServer != nil {
		mcpURL = d.MCPServer.URL()
	}

	sidebar := NewSidebar(agents)
	statusBar := NewStatusBar(mcpURL, len(agents))

	// Use reasonable defaults until WindowSizeMsg arrives.
	termWidth := 80
	termHeight := 24

	termPane := NewTerminalPane(agents, d.Pool, termWidth, termHeight)

	if len(agents) > 0 {
		statusBar.SetActive(agents[0].Name)
	}

	app := &App{
		daemon:    d,
		sidebar:   sidebar,
		termPane:  termPane,
		statusBar: statusBar,
		statusCh:  statusCh,
	}

	// Wire Pool callbacks — non-blocking sends.
	d.Pool.OutputSubscriber = func(agentID uuid.UUID, name string, data []byte) {
		// Feed directly to terminal pane's buffered channel (non-blocking).
		termPane.FeedOutput(agentID, data)
	}

	d.Pool.OnStatusChanged = func(agentID uuid.UUID, status models.AgentStatus) {
		select {
		case statusCh <- StatusMsg{AgentID: agentID, Status: status}:
		default:
		}
	}

	return app
}

// Init returns the initial commands.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.listenForStatus(),
		a.termPane.Init(),
	)
}

func (a *App) listenForStatus() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-a.statusCh
		if !ok {
			return nil
		}
		return msg
	}
}

// Update is the main event handler.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		sidebarWidth := 28
		termWidth := a.width - sidebarWidth
		statusHeight := 1
		termHeight := a.height - statusHeight

		if termWidth < 10 {
			termWidth = 10
		}
		if termHeight < 5 {
			termHeight = 5
		}

		cmd := a.termPane.Resize(termWidth, termHeight)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		// Help overlay dismissal.
		if a.showHelp {
			if msg.String() == "?" || msg.String() == "esc" {
				a.showHelp = false
			}
			return a, nil
		}

		if a.mode == ModeInsert {
			// ESC exits insert mode.
			if msg.String() == "esc" {
				a.mode = ModeNavigation
				a.statusBar.SetMode(ModeNavigation)
				a.termPane.SetMode(ModeNavigation)
				return a, nil
			}
			// Forward everything else to terminal pane.
			cmd := a.termPane.Update(msg)
			return a, cmd
		}

		// Navigation mode.
		switch msg.String() {
		case "q":
			a.quitting = true
			close(a.statusCh)
			a.termPane.Close()
			return a, tea.Quit
		case "i":
			a.mode = ModeInsert
			a.statusBar.SetMode(ModeInsert)
			a.termPane.SetMode(ModeInsert)
			return a, nil
		case "j", "k", "up", "down", "tab", "shift+tab":
			a.sidebar.Update(msg)
			if a.sidebar.SelectionChanged() {
				a.termPane.SetActive(a.sidebar.SelectedAgent())
				a.statusBar.SetActive(a.sidebar.SelectedName())
			}
			return a, nil
		case "r":
			agentID := a.sidebar.SelectedAgent()
			if agentID != (uuid.UUID{}) {
				a.daemon.Pool.Restart(agentID)
			}
			return a, nil
		case "?":
			a.showHelp = true
			return a, nil
		}

	case StatusMsg:
		a.sidebar.Update(msg)
		return a, a.listenForStatus()

	case centralTickMsg:
		cmd := a.termPane.Update(msg)
		return a, cmd

	default:
		// Forward bubbleterm internal messages (terminalOutputMsg, etc.).
		cmd := a.termPane.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)
	}

	return a, nil
}

// View renders the full TUI.
func (a *App) View() tea.View {
	if a.quitting {
		return tea.NewView("shutting down...")
	}

	sidebarWidth := 28
	termWidth := a.width - sidebarWidth
	statusHeight := 1
	termHeight := a.height - statusHeight

	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 5 {
		termHeight = 5
	}

	sidebarView := a.sidebar.View(sidebarWidth, termHeight)

	var termView string
	if a.showHelp {
		// Replace terminal pane with help overlay.
		helpBox := helpStyle.Render(helpText)
		helpLines := strings.Split(helpBox, "\n")
		// Center vertically.
		pad := (termHeight - len(helpLines)) / 2
		if pad < 0 {
			pad = 0
		}
		var centered []string
		for i := 0; i < pad; i++ {
			centered = append(centered, strings.Repeat(" ", termWidth))
		}
		for _, l := range helpLines {
			// Center horizontally.
			lineW := lipgloss.Width(l)
			leftPad := (termWidth - lineW) / 2
			if leftPad < 0 {
				leftPad = 0
			}
			centered = append(centered, strings.Repeat(" ", leftPad)+l)
		}
		for len(centered) < termHeight {
			centered = append(centered, strings.Repeat(" ", termWidth))
		}
		termView = strings.Join(centered[:termHeight], "\n")
	} else {
		termView = a.termPane.View(termWidth, termHeight)
	}

	statusView := a.statusBar.View(a.width)

	mainArea := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, termView)

	// Ensure mainArea has correct height by trimming excess lines.
	lines := strings.Split(mainArea, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	mainArea = strings.Join(lines, "\n")

	full := lipgloss.JoinVertical(lipgloss.Left, mainArea, statusView)

	var v tea.View
	v.SetContent(full)
	v.AltScreen = true
	return v
}
