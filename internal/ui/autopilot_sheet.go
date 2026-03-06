package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/google/uuid"
)

// ShowAutopilotDecision presents a modal sheet for the autopilot "ask" action.
// It shows the agent's last terminal output and lets the user approve, decline,
// or send a custom reply. onReply is called with the chosen response text;
// passing an empty string means the user dismissed without responding.
func (a *App) ShowAutopilotDecision(agentID uuid.UUID, lastOutput string, onReply func(response string)) {
	ag, ok := a.manager.Agent(agentID)
	if !ok {
		return
	}

	nameLabel := widget.NewLabelWithStyle(
		ag.Avatar+" "+ag.Name+" — needs a decision",
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
	)

	outputEntry := widget.NewMultiLineEntry()
	outputEntry.SetText(lastOutput)
	outputEntry.Disable()
	outputEntry.SetMinRowsVisible(6)

	replyEntry := widget.NewMultiLineEntry()
	replyEntry.SetPlaceHolder("Type a custom reply…")
	replyEntry.SetMinRowsVisible(3)

	var popup *widget.PopUp
	dismiss := func() {
		if popup != nil {
			popup.Hide()
			popup = nil
		}
	}

	respond := func(reply string) {
		dismiss()
		if onReply != nil {
			onReply(reply)
		}
	}

	yesBtn := widget.NewButton("Yes / Continue", func() { respond("yes, continue") })
	noBtn := widget.NewButton("No / Stop", func() { respond("no") })
	sendBtn := widget.NewButton("Send Reply", func() {
		if replyEntry.Text != "" {
			respond(replyEntry.Text)
		}
	})
	cancelBtn := widget.NewButton("Dismiss", func() { dismiss() })

	content := container.NewVBox(
		nameLabel,
		widget.NewSeparator(),
		widget.NewLabel("Agent output:"),
		container.NewScroll(outputEntry),
		widget.NewSeparator(),
		widget.NewLabel("Reply:"),
		replyEntry,
		container.NewHBox(yesBtn, noBtn, sendBtn, cancelBtn),
	)

	popup = widget.NewModalPopUp(content, a.window.Canvas())
	popup.Resize(fyne.NewSize(580, 500))
	popup.Show()
}
