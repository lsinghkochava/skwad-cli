package ui

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/persistence"
)

// SettingsWindow wraps the settings UI in its own Fyne window.
type SettingsWindow struct {
	fyneApp fyne.App
	store   *persistence.Store
	window  fyne.Window

	// OnDeployBenchAgent is called when the user clicks Deploy on a bench entry.
	OnDeployBenchAgent func(ag *models.Agent)
}

// NewSettingsWindow creates but does not show the settings window.
func NewSettingsWindow(app fyne.App, store *persistence.Store) *SettingsWindow {
	return &SettingsWindow{fyneApp: app, store: store}
}

// Show opens the settings window (or brings it to front if already open).
func (s *SettingsWindow) Show() {
	if s.window != nil {
		s.window.Show()
		return
	}
	s.window = s.fyneApp.NewWindow("Settings")
	s.window.SetContent(s.buildContent())
	s.window.Resize(fyne.NewSize(600, 500))
	s.window.SetOnClosed(func() { s.window = nil })
	s.window.Show()
}

func (s *SettingsWindow) buildContent() fyne.CanvasObject {
	settings := s.store.Settings()

	tabs := container.NewAppTabs(
		container.NewTabItem("General", s.generalTab(&settings)),
		container.NewTabItem("Terminal", s.terminalTab(&settings)),
		container.NewTabItem("Coding", s.codingTab(&settings)),
		container.NewTabItem("MCP Server", s.mcpTab(&settings)),
		container.NewTabItem("Autopilot", s.autopilotTab(&settings)),
		container.NewTabItem("Appearance", s.appearanceTab(&settings)),
		container.NewTabItem("Personas", s.personasTab()),
		container.NewTabItem("Bench", s.benchTab()),
	)

	saveBtn := widget.NewButton("Save", func() {
		_ = s.store.SaveSettings(settings)
		if s.window != nil {
			s.window.Hide()
		}
	})

	return container.NewBorder(nil, saveBtn, nil, nil, tabs)
}

func (s *SettingsWindow) generalTab(settings *models.AppSettings) fyne.CanvasObject {
	restoreCheck := widget.NewCheck("Restore layout on launch", func(v bool) {
		settings.RestoreLayoutOnLaunch = v
	})
	restoreCheck.Checked = settings.RestoreLayoutOnLaunch

	trayCheck := widget.NewCheck("Keep in system tray", func(v bool) {
		settings.KeepInTray = v
	})
	trayCheck.Checked = settings.KeepInTray

	sourceEntry := widget.NewEntry()
	sourceEntry.SetText(settings.SourceBaseFolder)
	sourceEntry.OnChanged = func(v string) { settings.SourceBaseFolder = v }

	return container.NewVBox(
		restoreCheck,
		trayCheck,
		widget.NewLabel("Source base folder"),
		sourceEntry,
	)
}

func (s *SettingsWindow) terminalTab(settings *models.AppSettings) fyne.CanvasObject {
	fontEntry := widget.NewEntry()
	fontEntry.SetText(settings.TerminalFontName)
	fontEntry.OnChanged = func(v string) { settings.TerminalFontName = v }

	sizeEntry := widget.NewEntry()
	sizeEntry.SetText(strconv.Itoa(settings.TerminalFontSize))
	sizeEntry.OnChanged = func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			settings.TerminalFontSize = n
		}
	}

	bgEntry := widget.NewEntry()
	bgEntry.SetText(settings.TerminalBgColor)
	bgEntry.OnChanged = func(v string) { settings.TerminalBgColor = v }

	fgEntry := widget.NewEntry()
	fgEntry.SetText(settings.TerminalFgColor)
	fgEntry.OnChanged = func(v string) { settings.TerminalFgColor = v }

	return container.NewVBox(
		widget.NewLabel("Font"), fontEntry,
		widget.NewLabel("Font size"), sizeEntry,
		widget.NewLabel("Background color (hex)"), bgEntry,
		widget.NewLabel("Foreground color (hex)"), fgEntry,
	)
}

func (s *SettingsWindow) codingTab(settings *models.AppSettings) fyne.CanvasObject {
	editorEntry := widget.NewEntry()
	editorEntry.SetText(settings.DefaultOpenWithApp)
	editorEntry.SetPlaceHolder("e.g. code, nvim, gedit")
	editorEntry.OnChanged = func(v string) { settings.DefaultOpenWithApp = v }

	claudeEntry := widget.NewEntry()
	claudeEntry.SetText(settings.AgentTypeOptions.ClaudeOptions)
	claudeEntry.OnChanged = func(v string) { settings.AgentTypeOptions.ClaudeOptions = v }

	codexEntry := widget.NewEntry()
	codexEntry.SetText(settings.AgentTypeOptions.CodexOptions)
	codexEntry.OnChanged = func(v string) { settings.AgentTypeOptions.CodexOptions = v }

	opencodeEntry := widget.NewEntry()
	opencodeEntry.SetText(settings.AgentTypeOptions.OpenCodeOptions)
	opencodeEntry.OnChanged = func(v string) { settings.AgentTypeOptions.OpenCodeOptions = v }

	geminiEntry := widget.NewEntry()
	geminiEntry.SetText(settings.AgentTypeOptions.GeminiOptions)
	geminiEntry.OnChanged = func(v string) { settings.AgentTypeOptions.GeminiOptions = v }

	custom1CmdEntry := widget.NewEntry()
	custom1CmdEntry.SetText(settings.AgentTypeOptions.Custom1Command)
	custom1CmdEntry.OnChanged = func(v string) { settings.AgentTypeOptions.Custom1Command = v }

	custom1OptEntry := widget.NewEntry()
	custom1OptEntry.SetText(settings.AgentTypeOptions.Custom1Options)
	custom1OptEntry.OnChanged = func(v string) { settings.AgentTypeOptions.Custom1Options = v }

	custom2CmdEntry := widget.NewEntry()
	custom2CmdEntry.SetText(settings.AgentTypeOptions.Custom2Command)
	custom2CmdEntry.OnChanged = func(v string) { settings.AgentTypeOptions.Custom2Command = v }

	custom2OptEntry := widget.NewEntry()
	custom2OptEntry.SetText(settings.AgentTypeOptions.Custom2Options)
	custom2OptEntry.OnChanged = func(v string) { settings.AgentTypeOptions.Custom2Options = v }

	return container.NewVBox(
		widget.NewLabel("Default editor (Cmd+Shift+O)"), editorEntry,
		widget.NewSeparator(),
		widget.NewLabel("Claude extra flags"), claudeEntry,
		widget.NewLabel("Codex extra flags"), codexEntry,
		widget.NewLabel("OpenCode extra flags"), opencodeEntry,
		widget.NewLabel("Gemini extra flags"), geminiEntry,
		widget.NewSeparator(),
		widget.NewLabel("Custom 1 — command"), custom1CmdEntry,
		widget.NewLabel("Custom 1 — extra flags"), custom1OptEntry,
		widget.NewLabel("Custom 2 — command"), custom2CmdEntry,
		widget.NewLabel("Custom 2 — extra flags"), custom2OptEntry,
	)
}

func (s *SettingsWindow) mcpTab(settings *models.AppSettings) fyne.CanvasObject {
	enableCheck := widget.NewCheck("Enable MCP server", func(v bool) {
		settings.MCPServerEnabled = v
	})
	enableCheck.Checked = settings.MCPServerEnabled

	portEntry := widget.NewEntry()
	portEntry.SetText(itoa(settings.MCPServerPort))
	portEntry.OnChanged = func(v string) {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			settings.MCPServerPort = p
		}
	}

	return container.NewVBox(
		enableCheck,
		widget.NewLabel("Port"), portEntry,
	)
}

func (s *SettingsWindow) autopilotTab(settings *models.AppSettings) fyne.CanvasObject {
	enableCheck := widget.NewCheck("Enable Autopilot", func(v bool) {
		settings.Autopilot.Enabled = v
	})
	enableCheck.Checked = settings.Autopilot.Enabled

	providerSelect := widget.NewSelect([]string{"openai", "anthropic", "google"}, func(v string) {
		settings.Autopilot.Provider = models.AutopilotProvider(v)
	})
	providerSelect.SetSelected(string(settings.Autopilot.Provider))

	apiKeyEntry := widget.NewPasswordEntry()
	apiKeyEntry.SetText(settings.Autopilot.APIKey)
	apiKeyEntry.OnChanged = func(v string) { settings.Autopilot.APIKey = v }

	actionSelect := widget.NewSelect([]string{"mark", "ask", "continue", "custom"}, func(v string) {
		settings.Autopilot.Action = models.AutopilotAction(v)
	})
	actionSelect.SetSelected(string(settings.Autopilot.Action))

	return container.NewVBox(
		enableCheck,
		widget.NewLabel("Provider"), providerSelect,
		widget.NewLabel("API key"), apiKeyEntry,
		widget.NewLabel("Action"), actionSelect,
	)
}

func (s *SettingsWindow) appearanceTab(settings *models.AppSettings) fyne.CanvasObject {
	modeSelect := widget.NewSelect([]string{"auto", "system", "light", "dark"}, func(v string) {
		settings.AppearanceMode = models.AppearanceMode(v)
	})
	modeSelect.SetSelected(string(settings.AppearanceMode))

	mermaidSelect := widget.NewSelect([]string{"auto", "light", "dark"}, func(v string) {
		settings.MermaidTheme = v
	})
	mermaidSelect.SetSelected(settings.MermaidTheme)

	return container.NewVBox(
		widget.NewLabel("Appearance mode"), modeSelect,
		widget.NewLabel("Mermaid theme"), mermaidSelect,
	)
}

func (s *SettingsWindow) personasTab() fyne.CanvasObject {
	personas, _ := s.store.LoadPersonas()

	var list *widget.List
	list = widget.NewList(
		func() int { return len(personas) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(widget.NewButton("Edit", nil), widget.NewButton("Delete", nil)),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(personas) {
				return
			}
			p := personas[id]
			row := obj.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(p.Name)
			btns := row.Objects[1].(*fyne.Container)

			editBtn := btns.Objects[0].(*widget.Button)
			editBtn.OnTapped = func() { s.editPersonaDialog(&personas[id], func() { list.Refresh() }) }
			editBtn.Refresh()

			delBtn := btns.Objects[1].(*widget.Button)
			if p.Type == models.PersonaTypeSystem {
				delBtn.Disable()
			} else {
				delBtn.Enable()
			}
			delBtn.OnTapped = func() {
				personas[id].State = models.PersonaStateDeleted
				_ = s.store.SavePersonas(personas)
				personas, _ = s.store.LoadPersonas()
				list.Refresh()
			}
			delBtn.Refresh()
		},
	)

	addBtn := widget.NewButton("+ New Persona", func() {
		p := models.Persona{ID: uuid.New(), Type: models.PersonaTypeUser, State: models.PersonaStateEnabled}
		s.editPersonaDialog(&p, func() {
			personas = append(personas, p)
			_ = s.store.SavePersonas(personas)
			personas, _ = s.store.LoadPersonas()
			list.Refresh()
		})
	})

	restoreBtn := widget.NewButton("Restore Defaults", func() {
		defaults := models.DefaultPersonas()
		// Keep user personas, restore system ones.
		var user []models.Persona
		for _, p := range personas {
			if p.Type == models.PersonaTypeUser {
				user = append(user, p)
			}
		}
		personas = append(defaults, user...)
		_ = s.store.SavePersonas(personas)
		list.Refresh()
	})

	return container.NewBorder(nil, container.NewHBox(addBtn, restoreBtn), nil, nil, list)
}

func (s *SettingsWindow) editPersonaDialog(p *models.Persona, onSave func()) {
	if s.window == nil {
		return
	}
	nameEntry := widget.NewEntry()
	nameEntry.SetText(p.Name)

	instrEntry := widget.NewMultiLineEntry()
	instrEntry.SetText(p.Instructions)
	instrEntry.SetMinRowsVisible(6)

	form := container.NewVBox(
		widget.NewLabel("Name"), nameEntry,
		widget.NewLabel("Instructions"), instrEntry,
	)
	dialog.ShowCustomConfirm("Persona", "Save", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		p.Name = nameEntry.Text
		p.Instructions = instrEntry.Text
		personas, _ := s.store.LoadPersonas()
		found := false
		for i := range personas {
			if personas[i].ID == p.ID {
				personas[i] = *p
				found = true
				break
			}
		}
		if !found {
			personas = append(personas, *p)
		}
		_ = s.store.SavePersonas(personas)
		if onSave != nil {
			onSave()
		}
	}, s.window)
}

func (s *SettingsWindow) benchTab() fyne.CanvasObject {
	bench, _ := s.store.LoadBench()

	var list *widget.List
	list = widget.NewList(
		func() int { return len(bench) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(widget.NewButton("Deploy", nil), widget.NewButton("Remove", nil)),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(bench) {
				return
			}
			b := bench[id]
			row := obj.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(b.Avatar + " " + b.Name)

			btns := row.Objects[1].(*fyne.Container)
			deployBtn := btns.Objects[0].(*widget.Button)
			deployBtn.OnTapped = func() {
				if s.OnDeployBenchAgent != nil {
					s.OnDeployBenchAgent(bench[id].ToAgent())
				}
			}
			deployBtn.Refresh()

			removeBtn := btns.Objects[1].(*widget.Button)
			removeBtn.OnTapped = func() {
				bench = append(bench[:id], bench[id+1:]...)
				_ = s.store.SaveBench(bench)
				list.Refresh()
			}
			removeBtn.Refresh()
		},
	)

	return container.NewBorder(nil, nil, nil, nil, list)
}
