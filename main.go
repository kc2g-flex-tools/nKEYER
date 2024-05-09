package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"

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

	mm := NewMorseMachine(sidetoneOsc)

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case upd := <-txUpdates:
			if upd.Object == "transmit" {
				if upd.Updated["speed"] != "" {
					wpm, err := strconv.Atoi(upd.CurrentState["speed"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					mm.SetWpm(wpm)
				}
				if upd.Updated["pitch"] != "" {
					pitch, err := strconv.Atoi(upd.CurrentState["pitch"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					mm.SetPitch(pitch)
				}
				if upd.Updated["mon_gain_cw"] != "" {
					volume, err := strconv.Atoi(upd.CurrentState["mon_gain_cw"])
					if err != nil {
						log.Error().Err(err).Send()
						break
					}
					mm.SetVolume(volume)
				}
			}
		case key := <-keypresses:
			switch key.Rune {
			case '-':
				mm.VolumeDown()
			case '=', '+':
				mm.VolumeUp()
			case 0:
				switch key.Key {
				case keyboard.KeyEsc, 0x03:
					cancel()
				case keyboard.KeyArrowDown:
					mm.SpeedDown()
				case keyboard.KeyArrowUp:
					mm.SpeedUp()
				case keyboard.KeyPgdn:
					mm.PitchDown()
				case keyboard.KeyPgup:
					mm.PitchUp()
				}
			}
		case elms, ok := <-keyer.ch:
			if !ok {
				break LOOP
			}
			mm.KeyerState(elms)
		case <-mm.timer.C:
			mm.TimerExpire()
		}
	}
}
