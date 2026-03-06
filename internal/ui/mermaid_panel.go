package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// MermaidPanel renders a Mermaid diagram source in a scrollable panel.
//
// Currently displays the source as a formatted code block.
// A WebView-backed renderer is a planned future enhancement.
type MermaidPanel struct {
	source string
	title  string

	content  *widget.RichText
	titleLbl *widget.Label
	closeBtn *widget.Button
	outer    *fyne.Container

	OnClose func()
}

// NewMermaidPanel creates a new Mermaid diagram panel.
func NewMermaidPanel() *MermaidPanel {
	mp := &MermaidPanel{}
	mp.build()
	return mp
}

func (mp *MermaidPanel) build() {
	mp.content = widget.NewRichText()
	mp.titleLbl = widget.NewLabel("")
	mp.closeBtn = widget.NewButton("×", func() {
		if mp.OnClose != nil {
			mp.OnClose()
		}
	})

	toolbar := container.NewBorder(nil, nil, nil, mp.closeBtn, mp.titleLbl)
	scroll := container.NewScroll(mp.content)
	mp.outer = container.NewBorder(toolbar, nil, nil, nil, scroll)
}

// Show renders the given Mermaid source with the provided title.
func (mp *MermaidPanel) Show(source, title string) {
	mp.source = source
	mp.title = title
	mp.titleLbl.SetText(title)
	mp.content.ParseMarkdown("**" + title + "**\n\n```\n" + source + "\n```\n")
	mp.content.Refresh()
}

// Widget returns the panel widget.
func (mp *MermaidPanel) Widget() fyne.CanvasObject { return mp.outer }
