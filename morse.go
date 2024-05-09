package main

import (
	"fmt"
	"time"

	log "github.com/rs/zerolog/log"
)

type State int

const (
	StateIdle State = iota
	StateDit
	StateDah
	StatePauseDit
	StatePauseDah
)

const (
	InitWPM int = 1 << iota
	InitPitch
	InitVolume
)

type MorseMachine struct {
	wpm         int
	pitch       int
	volume      int
	initialized int

	ditlen time.Duration

	pressed int
	queue   int
	state   State

	timer        *time.Timer
	timerPending bool

	sidetone *SidetoneOscillator
}

func NewMorseMachine(sidetone *SidetoneOscillator) *MorseMachine {
	return &MorseMachine{
		timer:        time.NewTimer(0),
		timerPending: true,
		sidetone:     sidetone,
	}
}

func (mm *MorseMachine) update() {
	mm.ditlen = (1200 * time.Millisecond) / time.Duration(mm.wpm)
	mm.sidetone.SetPitch(mm.pitch)
	mm.sidetone.SetVolume(mm.volume)
	mm.sidetone.SetRamp(mm.ditlen / 10)
	log.Info().Int("wpm", mm.wpm).Int("pitch", mm.pitch).Dur("ditlen", mm.ditlen).Int("volume", mm.volume).Send()
}

func (mm *MorseMachine) updateTimer(dur time.Duration) {
	timer := mm.timer
	if mm.timerPending && !timer.Stop() {
		<-timer.C
	}
	timer.Reset(dur)
	mm.timerPending = true
}

func (mm *MorseMachine) key(pressed bool) {
	sendFlexCW(pressed)
	mm.sidetone.SetKeyed(pressed)
}

func (mm *MorseMachine) Dit() {
	log.Info().Msg("dit")
	mm.state = StateDit
	mm.queue = 0
	mm.updateTimer(mm.ditlen)
	mm.key(true)
}

func (mm *MorseMachine) Dah() {
	log.Info().Msg("dah")
	mm.state = StateDah
	mm.queue = 0
	mm.updateTimer(3 * mm.ditlen)
	mm.key(true)
}

func (mm *MorseMachine) SetWpm(wpm int) {
	mm.wpm = wpm
	mm.initOne(InitWPM)
}

func (mm *MorseMachine) SetPitch(pitch int) {
	mm.pitch = pitch
	mm.initOne(InitPitch)
}

func (mm *MorseMachine) SetVolume(volume int) {
	mm.volume = volume
	mm.initOne(InitVolume)
}

func (mm *MorseMachine) initOne(flag int) {
	mm.initialized |= flag

	if mm.initialized == InitWPM|InitPitch|InitVolume {
		mm.update()
	}
}

func (mm *MorseMachine) VolumeDown() {
	mm.volume--
	fc.SendCmd(fmt.Sprintf("transmit set mon_gain_cw=%d", mm.volume))
	mm.update()
}

func (mm *MorseMachine) VolumeUp() {
	mm.volume++
	fc.SendCmd(fmt.Sprintf("transmit set mon_gain_cw=%d", mm.volume))
	mm.update()
}

func (mm *MorseMachine) SpeedDown() {
	mm.wpm--
	fc.SendCmd(fmt.Sprintf("cw wpm %d", mm.wpm))
	mm.update()
}

func (mm *MorseMachine) SpeedUp() {
	mm.wpm++
	fc.SendCmd(fmt.Sprintf("cw wpm %d", mm.wpm))
	mm.update()
}

func (mm *MorseMachine) PitchUp() {
	mm.pitch += 10
	fc.SendCmd(fmt.Sprintf("cw pitch %d", mm.pitch))
	mm.update()
}

func (mm *MorseMachine) PitchDown() {
	mm.pitch -= 10
	fc.SendCmd(fmt.Sprintf("cw pitch %d", mm.pitch))
	mm.update()
}

func (mm *MorseMachine) KeyerState(pressed int) {
	mm.pressed = pressed

	switch mm.state {
	case StateIdle:
		if pressed&Dah != 0 {
			// In the unlikely case we register both at once, dah wins
			mm.Dah()
		} else if pressed&Dit != 0 {
			mm.Dit()
		}
	case StateDit, StatePauseDit:
		if pressed&Dah != 0 {
			mm.queue = Dah
		}
	case StateDah, StatePauseDah:
		if pressed&Dit != 0 {
			mm.queue = Dit
		}
	}
}

func (mm *MorseMachine) TimerExpire() {
	mm.timerPending = false
	switch mm.state {
	case StateDit:
		mm.state = StatePauseDit
		mm.updateTimer(mm.ditlen)
		mm.key(false)
	case StateDah:
		mm.state = StatePauseDah
		mm.updateTimer(mm.ditlen)
		mm.key(false)
	case StatePauseDit:
		if mm.pressed&Dah != 0 || mm.queue&Dah != 0 {
			mm.Dah()
		} else if mm.pressed&Dit != 0 {
			mm.Dit()
		} else {
			mm.state = StateIdle
		}
	case StatePauseDah:
		if mm.pressed&Dit != 0 || mm.queue&Dit != 0 {
			mm.Dit()
		} else if mm.pressed&Dah != 0 {
			mm.Dah()
		} else {
			mm.state = StateIdle
		}
	}
}
