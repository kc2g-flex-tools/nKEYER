package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type NumberWidget struct {
	title    string
	value    int
	viewport viewport.Model
}

func NewNumberWidget(width, height int, title string, initVal int) NumberWidget {
	vp := viewport.New(width, height)
	vp.Style = lipgloss.NewStyle().
		Align(lipgloss.Center).
		Margin(0, 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#00aa00"))

	return NumberWidget{
		title:    title,
		value:    initVal,
		viewport: vp,
	}
}

func (w NumberWidget) Init() tea.Cmd {
	return nil
}

func (w NumberWidget) Update(msg tea.Msg) (NumberWidget, tea.Cmd) {
	var cmd tea.Cmd

	w.viewport.SetContent(fmt.Sprintf("%d", w.value))
	w.viewport, cmd = w.viewport.Update(msg)
	return w, cmd
}

func (w NumberWidget) View() string {
	return w.title + "\n" + w.viewport.View()
}

func NewLogWidget(width, height int, title string) LogWidget {
	vp := viewport.New(width, height)
	return LogWidget{
		title:    title,
		viewport: vp,
	}
}

type LogWidget struct {
	title    string
	content  string
	viewport viewport.Model
}

func (w LogWidget) Init() tea.Cmd {
	return nil
}

func (w LogWidget) Update(msg tea.Msg) (LogWidget, tea.Cmd) {
	var cmd tea.Cmd
	w.viewport, cmd = w.viewport.Update(msg)
	return w, cmd
}

func (w LogWidget) View() string {
	w.viewport.SetContent(w.content)
	return w.title + "\n" + w.viewport.View()
}

type UIModel struct {
	mm      *MorseMachine
	wpm     NumberWidget
	volume  NumberWidget
	pitch   NumberWidget
	decoder LogWidget
}

func NewUI(mm *MorseMachine) UIModel {
	return UIModel{
		wpm:     NewNumberWidget(6, 1, " WPM", 0),
		pitch:   NewNumberWidget(6, 1, " Pitch", 0),
		volume:  NewNumberWidget(6, 1, " Volume", 0),
		decoder: NewLogWidget(80, 1, " CW"),
		mm:      mm,
	}
}

func (m UIModel) Init() tea.Cmd {
	return nil
}

func (m UIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case Decoded:
		m.decoder.content = string(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit

		case "up":
			m.mm.SpeedUp()
		case "down":
			m.mm.SpeedDown()

		case "pgup":
			m.mm.PitchUp()
		case "pgdown":
			m.mm.PitchDown()

		case "+", "=":
			m.mm.VolumeUp()
		case "-":
			m.mm.VolumeDown()
		}
	}

	m.wpm.value = m.mm.wpm
	m.wpm, cmd = m.wpm.Update(msg)
	cmds = append(cmds, cmd)

	m.pitch.value = m.mm.pitch
	m.pitch, cmd = m.pitch.Update(msg)
	cmds = append(cmds, cmd)

	m.volume.value = m.mm.volume
	m.volume, cmd = m.volume.Update(msg)
	cmds = append(cmds, cmd)

	m.decoder, cmd = m.decoder.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m UIModel) View() string {
	return lipgloss.JoinHorizontal(lipgloss.Top, m.wpm.View(), m.pitch.View(), m.volume.View()) + "\n" + m.decoder.View()
}
