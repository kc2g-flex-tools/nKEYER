package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/eiannone/keyboard"
	"github.com/jfreymuth/pulse"
	"github.com/kc2g-flex-tools/flexclient"
	"github.com/rs/zerolog"
	log "github.com/rs/zerolog/log"
)

var cfg struct {
	RadioIP   string
	Station   string
	KeyDev    string
	LogLevel  string
	AudioSink string
}

func init() {
	flag.StringVar(&cfg.RadioIP, "radio", ":discover:", "radio IP address or discovery spec")
	flag.StringVar(&cfg.Station, "station", "Flex", "station name to bind to")
	flag.StringVar(&cfg.KeyDev, "port", "/dev/ttyUSB0", "keyer device to open")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "minimum level of messages to log to console")
	flag.StringVar(&cfg.AudioSink, "sink", "default", "audio sink for sidetone")
}

var fc *flexclient.FlexClient
var keyDev *os.File
var ClientID string
var ClientUUID string

func bindClient() {
	log.Info().Str("station", cfg.Station).Msg("Waiting for station")

	clients := make(chan flexclient.StateUpdate)
	sub := fc.Subscribe(flexclient.Subscription{"client ", clients})
	cmdResult := fc.SendNotify("sub client all")

	var found, cmdComplete bool

	for !found || !cmdComplete {
		select {
		case upd := <-clients:
			if upd.CurrentState["station"] == cfg.Station {
				ClientID = strings.TrimPrefix(upd.Object, "client ")
				ClientUUID = upd.CurrentState["client_id"]
				found = true
			}
		case <-cmdResult.C:
			cmdComplete = true
		}
	}
	cmdResult.Close()

	fc.Unsubscribe(sub)

	log.Info().Str("client_id", ClientID).Str("uuid", ClientUUID).Msg("Found client")

	fc.SendAndWait("client bind client_id=" + ClientUUID)
}

const (
	Dit int = 1 << iota
	Dah
)

func listenSerial(ctx context.Context, dev *os.File, ch chan int) {
	fd := int(dev.Fd())

	for {
		unix.IoctlSetInt(fd, unix.TIOCMIWAIT, unix.TIOCM_CD|unix.TIOCM_CTS)
		bits, err := unix.IoctlGetInt(fd, unix.TIOCMGET)
		if err != nil {
			log.Error().Err(err).Send()
			continue
		}
		var elms int
		if bits&unix.TIOCM_CTS != 0 {
			elms |= Dah
		}
		if bits&unix.TIOCM_CD != 0 {
			elms |= Dit
		}
		ch <- elms
	}
}

type SidetoneOscillator struct {
	mu        sync.Mutex
	pitch     int
	volume    float32
	keyed     bool
	phase     float64
	rampLen   int
	rampLevel int
}

func (st *SidetoneOscillator) SetPitch(pitch int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.pitch = pitch
}

func (st *SidetoneOscillator) SetVolume(volume int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.volume = float32(volume) / 100
}

func (st *SidetoneOscillator) SetRamp(dur time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.rampLen = int(math.Round(4800 * dur.Seconds()))
}

func (st *SidetoneOscillator) SetKeyed(keyed bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.keyed = keyed
}

func (st *SidetoneOscillator) Generate(out []float32) (int, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	phaseIncrement := float64(st.pitch) * 2 * math.Pi / 48000

	for i := range out {
		st.phase += phaseIncrement
		if st.phase > 2*math.Pi {
			st.phase -= 2 * math.Pi
		}
		if st.keyed {
			if st.rampLevel < st.rampLen {
				st.rampLevel++
			}
		} else if st.rampLevel > 0 {
			st.rampLevel--
		}

		if st.rampLevel > 0 {
			vol := st.volume
			if st.rampLevel < st.rampLen {
				rampProgress := float64(st.rampLevel) / float64(st.rampLen)
				sin := float32(math.Sin(math.Pi * (rampProgress - 0.5)))
				vol *= (1 + sin) / 2
			}
			out[i] = float32(math.Sin(st.phase)) * vol
		} else {
			out[i] = 0
		}
	}
	return len(out), nil
}

func main() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{
			Out: os.Stderr,
		},
	).With().Timestamp().Logger()

	flag.Parse()

	logLevel, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		log.Fatal().Str("level", cfg.LogLevel).Msg("Unknown log level")
	}

	zerolog.SetGlobalLevel(logLevel)

	fc, err = flexclient.NewFlexClient(cfg.RadioIP)
	if err != nil {
		log.Fatal().Err(err).Msg("NewFlexClient failed")
	}

	pc, err := pulse.NewClient(
		pulse.ClientApplicationName("nKEYER"),
	)

	if err != nil {
		log.Fatal().Err(err).Msg("pulse.NewClient failed")
	}

	sidetoneOsc := SidetoneOscillator{}
	sidetonePlayback, err := pc.NewPlayback(pulse.Float32Reader(sidetoneOsc.Generate),
		pulse.PlaybackLatency(0.02),
		pulse.PlaybackSampleRate(48000),
	)
	sidetonePlayback.Start()

	keyDev, err = os.Open(cfg.KeyDev)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open keyer device")
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		fc.Run()
		cancel()
	}()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		log.Info().Msg("Exit on SIGINT")
		cancel()
	}()

	bindClient()

	keypresses, err := keyboard.GetKeys(1)
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	defer keyboard.Close()

	keyer := make(chan int)
	go listenSerial(ctx, keyDev, keyer)

	txUpdates := make(chan flexclient.StateUpdate)
	_ = fc.Subscribe(flexclient.Subscription{"transmit", txUpdates})
	fc.SendCmd("sub tx all")

	type State int
	const (
		StateIdle State = iota
		StateDit
		StateDah
		StatePauseDit
		StatePauseDah
	)

	log.Info().Msg("entering main loop")

	var wpm, pitch, volume int
	var initialized int
	var ditlen time.Duration
	var pressed, queue int
	var state State
	var idx int
	timer := time.NewTimer(0)
	pending := true

	updateWpm := func() {
		ditlen = (1200 * time.Millisecond) / time.Duration(wpm)
		sidetoneOsc.SetPitch(pitch)
		sidetoneOsc.SetVolume(volume)
		sidetoneOsc.SetRamp(ditlen / 10)
		log.Info().Int("wpm", wpm).Int("pitch", pitch).Dur("ditlen", ditlen).Int("volume", volume).Send()
	}

	updateTimer := func(dur time.Duration) {
		if pending && !timer.Stop() {
			<-timer.C
		}
		timer.Reset(dur)
		pending = true
	}

	cwKey := func(pressed bool) {
		ts := time.Now().UnixMilli() % 65536
		cwState := 0
		if pressed {
			cwState = 1
		}
		cmd := fmt.Sprintf("cw key %d time=0x%04X index=%d client_handle=%s", cwState, ts, idx, ClientID)
		fc.SendCmd(cmd)
		idx++
		sidetoneOsc.SetKeyed(pressed)
	}

	sendDit := func() {
		log.Info().Msg("dit")
		state = StateDit
		queue = 0
		updateTimer(ditlen)
		cwKey(true)
	}

	sendDah := func() {
		log.Info().Msg("dah")
		state = StateDah
		queue = 0
		updateTimer(3 * ditlen)
		cwKey(true)
	}

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case upd := <-txUpdates:
			if upd.Object == "transmit" {
				if upd.CurrentState["speed"] != "" {
					wpm, err = strconv.Atoi(upd.CurrentState["speed"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					initialized |= 1
				}
				if upd.CurrentState["pitch"] != "" {
					pitch, err = strconv.Atoi(upd.CurrentState["pitch"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					initialized |= 2
				}
				if upd.CurrentState["mon_gain_cw"] != "" {
					volume, err = strconv.Atoi(upd.CurrentState["mon_gain_cw"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					initialized |= 4
				}
				if initialized == 7 {
					updateWpm()
				}
			}
		case key := <-keypresses:
			switch key.Rune {
			case '-':
				volume--
				fc.SendCmd(fmt.Sprintf("transmit set mon_gain_cw=%d", volume))
				updateWpm()
			case '=', '+':
				volume++
				fc.SendCmd(fmt.Sprintf("transmit set mon_gain_cw=%d", volume))
				updateWpm()
			case 0:
				switch key.Key {
				case keyboard.KeyEsc:
					cancel()
				case keyboard.KeyArrowUp:
					wpm++
					fc.SendCmd(fmt.Sprintf("cw wpm %d", wpm))
					updateWpm()
				case keyboard.KeyArrowDown:
					wpm--
					fc.SendCmd(fmt.Sprintf("cw wpm %d", wpm))
					updateWpm()
				case keyboard.KeyPgup:
					pitch += 10
					fc.SendCmd(fmt.Sprintf("cw pitch %d", pitch))
					updateWpm()
				case keyboard.KeyPgdn:
					pitch -= 10
					fc.SendCmd(fmt.Sprintf("cw pitch %d", pitch))
					updateWpm()
				}
			}
		case elms := <-keyer:
			pressed = elms
			switch state {
			case StateIdle:
				if pressed&Dah != 0 {
					// In the unlikely case we register both at once, dah wins
					sendDah()
				} else if pressed&Dit != 0 {
					sendDit()
				}
			case StateDit, StatePauseDit:
				if pressed&Dah != 0 {
					queue = Dah
				}
			case StateDah, StatePauseDah:
				if pressed&Dit != 0 {
					queue = Dit
				}
			}
		case <-timer.C:
			pending = false
			switch state {
			case StateDit:
				state = StatePauseDit
				updateTimer(ditlen)
				cwKey(false)
			case StateDah:
				state = StatePauseDah
				updateTimer(ditlen)
				cwKey(false)
			case StatePauseDit:
				if pressed&Dah != 0 || queue&Dah != 0 {
					sendDah()
				} else if pressed&Dit != 0 {
					sendDit()
				} else {
					state = StateIdle
				}
			case StatePauseDah:
				if pressed&Dit != 0 || queue&Dit != 0 {
					sendDit()
				} else if pressed&Dah != 0 {
					sendDah()
				} else {
					state = StateIdle
				}
			}
		}
	}

	_ = initialized
}
