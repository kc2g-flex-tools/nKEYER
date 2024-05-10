package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type State int

const (
	StateIdleChar State = iota
	StateIdle
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

var morseTable = map[string]string{
	".-":     "A",
	".-.-":   "Ä",
	"-...":   "B",
	"-.-.":   "C",
	"----":   "CH",
	"-..":    "D",
	".":      "E",
	"..-.":   "F",
	"--.":    "G",
	"....":   "H",
	"..":     "I",
	".---":   "J",
	"-.-":    "K",
	".-..":   "L",
	"--":     "M",
	"-.":     "N",
	"---":    "O",
	"---.":   "Ö",
	".--.":   "P",
	"--.-":   "Q",
	".-.":    "R",
	"...":    "S",
	"-":      "T",
	"..-":    "U",
	"..--":   "Ü",
	"...-":   "V",
	".--":    "W",
	"-..-":   "X",
	"-.--":   "Y",
	"--..":   "Z",
	"-----":  "0",
	".----":  "1",
	"..---":  "2",
	"...--":  "3",
	"....-":  "4",
	".....":  "5",
	"-....":  "6",
	"--...":  "7",
	"---..":  "8",
	"----.":  "9",
	".-.-.":  "+",
	"--..--": ",",
	"-....-": "-",
	".-.-.-": ".",
	"-..-.":  "/",
	"---...": ";",
	"-...-":  "=",
	"..--..": "?",
	".--.-.": "@",
	".-...":  "<AS>",
	"...-.-": "<SK>",
}

var morseRegex *regexp.Regexp

func init() {
	reStr := `(?:`
	first := true
	for key := range morseTable {
		if !first {
			reStr += `|`
		}
		first = false
		reStr += regexp.QuoteMeta(key)
	}
	reStr += `) $`
	morseRegex = regexp.MustCompile(reStr)
}

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

	decodebuffer string
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
	mm.state = StateDit
	mm.queue = 0
	mm.updateTimer(mm.ditlen)
	mm.decode(".")
	mm.key(true)
}

func (mm *MorseMachine) Dah() {
	mm.state = StateDah
	mm.queue = 0
	mm.updateTimer(3 * mm.ditlen)
	mm.decode("-")
	mm.key(true)
}

func (mm *MorseMachine) Idle() {
	mm.state = StateIdleChar
	mm.updateTimer(5 * mm.ditlen)
	mm.decode(" ")
}

func (mm *MorseMachine) IdleWord() {
	mm.state = StateIdle
	if !strings.HasSuffix(mm.decodebuffer, " ") {
		mm.decode(" ")
	}
}

func (mm *MorseMachine) decode(ch string) {
	mm.decodebuffer += ch
	mm.decodebuffer = morseRegex.ReplaceAllStringFunc(mm.decodebuffer, func(match string) string {
		return morseTable[strings.TrimSuffix(match, " ")]
	})

	if len(mm.decodebuffer) > 78 {
		mm.decodebuffer = mm.decodebuffer[len(mm.decodebuffer)-78:]
	}
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

func (mm *MorseMachine) KeyerState(pressed int) (ret string) {
	mm.pressed = pressed

	switch mm.state {
	case StateIdle, StateIdleChar:
		if pressed&Dah != 0 {
			// In the unlikely case we register both at once, dah wins
			mm.Dah()
			ret = mm.decodebuffer
		} else if pressed&Dit != 0 {
			mm.Dit()
			ret = mm.decodebuffer
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
	return
}

func (mm *MorseMachine) TimerExpire() (ret string) {
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
			ret = mm.decodebuffer
		} else if mm.pressed&Dit != 0 {
			mm.Dit()
			ret = mm.decodebuffer
		} else {
			mm.Idle()
			ret = mm.decodebuffer
		}
	case StatePauseDah:
		if mm.pressed&Dit != 0 || mm.queue&Dit != 0 {
			mm.Dit()
			ret = mm.decodebuffer
		} else if mm.pressed&Dah != 0 {
			mm.Dah()
			ret = mm.decodebuffer
		} else {
			mm.Idle()
			ret = mm.decodebuffer
		}
	case StateIdleChar:
		mm.IdleWord()
	}
	return
}
