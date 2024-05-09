package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

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

func main() {
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{
			Out: os.Stderr,
		},
	).With().Timestamp().Logger()

	flag.Parse()

	var err error

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

	sidetoneOsc, err := NewSidetoneOscillator(pc)
	if err != nil {
		log.Fatal().Err(err).Msg("NewSidetoneOscillator failed")
	}
	defer sidetoneOsc.Close()

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

	keyer, err := NewKeyer()
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	defer keyer.Close()

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
				if upd.Updated["speed"] != "" {
					wpm, err = strconv.Atoi(upd.CurrentState["speed"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					initialized |= 1
				}
				if upd.Updated["pitch"] != "" {
					pitch, err = strconv.Atoi(upd.CurrentState["pitch"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					initialized |= 2
				}
				if upd.Updated["mon_gain_cw"] != "" {
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
		case elms, ok := <-keyer.ch:
			if !ok {
				break LOOP
			}

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
