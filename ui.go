package main

import (
	"fmt"
	"io"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type UIModel struct {
	mm           *MorseMachine
	app          *tview.Application
	wpm          *tview.TextView
	volume       *tview.TextView
	pitch        *tview.TextView
	decoder      *tview.TextView
	logger       *tview.TextView
	LogWriter    io.Writer
	DecodeWriter io.Writer
}

func NewUI(mm *MorseMachine) UIModel {
	m := UIModel{
		mm:  mm,
		app: tview.NewApplication(),
	}

	makeTextView := func(title string) *tview.TextView {
		tv := tview.NewTextView().SetScrollable(false)
		tv.SetBorder(true).SetTitle(title)
		return tv
	}

	m.wpm = makeTextView("WPM")
	m.volume = makeTextView("Volume")
	m.pitch = makeTextView("Pitch")
	m.decoder = makeTextView("Decoder").SetChangedFunc(func() { m.app.Draw() })
	m.logger = makeTextView("Log").SetDynamicColors(true).SetChangedFunc(func() { m.app.Draw() })
	m.LogWriter = tview.ANSIWriter(m.logger)

	statusRow := tview.NewFlex()
	statusRow.AddItem(m.wpm, 0, 1, false)
	statusRow.AddItem(m.volume, 0, 1, false)
	statusRow.AddItem(m.pitch, 0, 1, false)

	mainPage := tview.NewFlex().SetDirection(tview.FlexRow).SetFullScreen(true)
	mainPage.AddItem(statusRow, 3, 0, false)
	mainPage.AddItem(m.decoder, 3, 0, false)
	mainPage.AddItem(m.logger, 0, 1, false)

	m.app.SetRoot(mainPage, true)
	m.app.SetInputCapture(m.InputEvent)

	return m
}

func (m UIModel) InputEvent(event *tcell.EventKey) *tcell.EventKey {
	key := event.Key()
	rune := event.Rune()

	switch {
	case key == tcell.KeyEscape, key == tcell.KeyCtrlC, rune == 'q':
		m.app.Stop()
		return nil

	case key == tcell.KeyUp:
		m.mm.SpeedUp()
		return nil
	case key == tcell.KeyDown:
		m.mm.SpeedDown()
		return nil

	case key == tcell.KeyPgUp:
		m.mm.PitchUp()
		return nil
	case key == tcell.KeyPgDn:
		m.mm.PitchDown()
		return nil

	case rune == '+', rune == '=':
		m.mm.VolumeUp()
		return nil
	case rune == '-':
		m.mm.VolumeDown()
		return nil

	default:
		return event
	}
}

func (m UIModel) Update() {
	m.app.QueueUpdateDraw(func() {
		m.wpm.SetText(fmt.Sprintf("%d", m.mm.wpm))
		m.pitch.SetText(fmt.Sprintf("%d", m.mm.pitch))
		m.volume.SetText(fmt.Sprintf("%d", m.mm.volume))
	})
}
