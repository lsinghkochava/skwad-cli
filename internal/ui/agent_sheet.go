package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/git"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
)

// AgentSheet is the dialog for creating or editing an agent.
type AgentSheet struct {
	window   fyne.Window
	agent    *models.Agent // nil for new agent
	personas []models.Persona

	nameEntry    *widget.Entry
	avatarEntry  *widget.Entry
	folderEntry  *widget.Entry
	typeSelect   *widget.Select
	cmdEntry     *widget.Entry
	personaSelect *widget.Select

	OnSave func(a *models.Agent)
}

// NewAgentSheet creates a new agent creation sheet.
func NewAgentSheet(w fyne.Window, personas []models.Persona, onSave func(*models.Agent)) *AgentSheet {
	return &AgentSheet{
		window:   w,
		personas: personas,
		OnSave:   onSave,
	}
}

// EditAgentSheet creates an edit sheet pre-filled with the given agent.
func EditAgentSheet(w fyne.Window, a *models.Agent, personas []models.Persona, onSave func(*models.Agent)) *AgentSheet {
	s := NewAgentSheet(w, personas, onSave)
	s.agent = a
	return s
}

// Show presents the agent sheet in its own resizable window.
func (s *AgentSheet) Show() {
	s.nameEntry = widget.NewEntry()
	s.avatarEntry = widget.NewEntry()
	s.folderEntry = widget.NewEntry()
	s.cmdEntry = widget.NewEntry()

	agentTypes := []string{
		string(models.AgentTypeClaude),
		string(models.AgentTypeCodex),
		string(models.AgentTypeOpenCode),
		string(models.AgentTypeGemini),
		string(models.AgentTypeCopilot),
		string(models.AgentTypeCustom1),
		string(models.AgentTypeCustom2),
		string(models.AgentTypeShell),
	}
	s.typeSelect = widget.NewSelect(agentTypes, func(v string) {
		s.cmdEntry.Hidden = v != string(models.AgentTypeShell)
		s.cmdEntry.Refresh()
	})

	personaNames := []string{"None"}
	for _, p := range s.personas {
		if p.State != models.PersonaStateDeleted {
			personaNames = append(personaNames, p.Name)
		}
	}
	s.personaSelect = widget.NewSelect(personaNames, nil)

	if s.agent != nil {
		s.nameEntry.SetText(s.agent.Name)
		s.avatarEntry.SetText(s.agent.Avatar)
		s.folderEntry.SetText(s.agent.Folder)
		s.typeSelect.SetSelected(string(s.agent.AgentType))
		s.cmdEntry.SetText(s.agent.ShellCommand)
	} else {
		s.typeSelect.SetSelected(string(models.AgentTypeClaude))
	}

	folderRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(
			widget.NewButton("Browse…", s.browseFolder),
			widget.NewButton("New Worktree…", s.createWorktree),
		),
		s.folderEntry,
	)

	form := container.NewVBox(
		widget.NewLabel("Name"), s.nameEntry,
		widget.NewSeparator(),
		widget.NewLabel("Avatar (emoji)"), s.avatarEntry,
		widget.NewSeparator(),
		widget.NewLabel("Folder"), folderRow,
		widget.NewSeparator(),
		widget.NewLabel("Agent type"), s.typeSelect,
		widget.NewLabel("Shell command (shell type only)"), s.cmdEntry,
		widget.NewSeparator(),
		widget.NewLabel("Persona"), s.personaSelect,
	)

	title := "New Agent"
	if s.agent != nil {
		title = "Edit Agent"
	}
	win := fyne.CurrentApp().NewWindow(title)
	win.Resize(fyne.NewSize(520, 520))
	win.CenterOnScreen()

	saveBtn := widget.NewButton("Save", func() {
		s.save()
		win.Close()
	})
	cancelBtn := widget.NewButton("Cancel", func() { win.Close() })
	buttons := container.NewHBox(cancelBtn, saveBtn)

	scroll := container.NewScroll(form)
	content := container.NewBorder(nil, container.NewPadded(buttons), nil, nil, scroll)
	win.SetContent(content)
	win.Show()
}

// createWorktree shows a dialog to create a new git worktree and sets the folder.
func (s *AgentSheet) createWorktree() {
	// The source repo must be set in the folder field already.
	repoPath := s.folderEntry.Text
	if repoPath == "" {
		dialog.ShowInformation("No repo", "Enter or browse to the base repo folder first.", s.window)
		return
	}

	branchEntry := widget.NewEntry()
	branchEntry.SetPlaceHolder("e.g. feature/my-branch")

	form := container.NewVBox(
		widget.NewLabel("Branch name"), branchEntry,
	)
	dialog.ShowCustomConfirm("New Worktree", "Create", "Cancel", form, func(ok bool) {
		if !ok || branchEntry.Text == "" {
			return
		}
		branch := branchEntry.Text
		destPath := git.SuggestedPath(repoPath, branch)
		wm := git.NewWorktreeManager(repoPath)
		if err := wm.Create(branch, destPath); err != nil {
			dialog.ShowError(err, s.window)
			return
		}
		s.folderEntry.SetText(destPath)
	}, s.window)
}

func (s *AgentSheet) browseFolder() {
	dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err == nil && uri != nil {
			s.folderEntry.SetText(uri.Path())
		}
	}, s.window)
}

func (s *AgentSheet) save() {
	a := s.agent
	if a == nil {
		a = &models.Agent{ID: uuid.New()}
	}

	a.Name = s.nameEntry.Text
	a.Avatar = s.avatarEntry.Text
	a.Folder = s.folderEntry.Text
	a.AgentType = models.AgentType(s.typeSelect.Selected)
	a.ShellCommand = s.cmdEntry.Text

	// Resolve persona ID from selection.
	sel := s.personaSelect.Selected
	if sel != "" && sel != "None" {
		for _, p := range s.personas {
			if p.Name == sel {
				pid := p.ID
				a.PersonaID = &pid
				break
			}
		}
	} else {
		a.PersonaID = nil
	}

	if s.OnSave != nil {
		s.OnSave(a)
	}
}
