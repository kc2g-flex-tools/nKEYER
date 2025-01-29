package main

import (
	"context"
	"flag"
	"os"
	"strconv"

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
	Sidetone  bool
}

func init() {
	flag.StringVar(&cfg.RadioIP, "radio", ":discover:", "radio IP address or discovery spec")
	flag.StringVar(&cfg.Station, "station", "Flex", "station name to bind to")
	flag.StringVar(&cfg.KeyDev, "port", "/dev/ttyUSB0", "keyer device to open")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "minimum level of messages to log to console")
	flag.StringVar(&cfg.AudioSink, "sink", "default", "audio sink for sidetone")
	flag.BoolVar(&cfg.Sidetone, "sidetone", true, "whether to have local sidetone")
}

type Decoded string

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

	var sidetoneOsc Sidetoner

	if cfg.Sidetone {
		pc, err := pulse.NewClient(
			pulse.ClientApplicationName("nKEYER"),
		)

		if err != nil {
			log.Fatal().Err(err).Msg("pulse.NewClient failed")
		}

		sidetoneOsc, err = NewSidetoneOscillator(pc)
		if err != nil {
			log.Fatal().Err(err).Msg("NewSidetoneOscillator failed")
		}
		defer sidetoneOsc.Close()
	} else {
		sidetoneOsc = DummySidetone{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		fc.Run()
		cancel()
	}()

	bindClient()

	keyer, err := NewKeyer()
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	defer keyer.Close()

	txUpdates := make(chan flexclient.StateUpdate)
	_ = fc.Subscribe(flexclient.Subscription{"transmit", txUpdates})
	fc.SendCmd("sub tx all")

	mm := NewMorseMachine(sidetoneOsc)

	ui := NewUI(mm)
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{
			Out: ui.LogWriter,
		},
	).With().Timestamp().Logger()

	go func() {
	LOOP:
		for {
			select {
			case <-ctx.Done():
				ui.app.Stop()
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
						ui.Update()
					}
					if upd.Updated["pitch"] != "" {
						pitch, err := strconv.Atoi(upd.CurrentState["pitch"])
						if err != nil {
							log.Error().Err(err).Send()
							break
						}
						mm.SetPitch(pitch)
						ui.Update()
					}
					if upd.Updated["mon_gain_cw"] != "" {
						volume, err := strconv.Atoi(upd.CurrentState["mon_gain_cw"])
						if err != nil {
							log.Error().Err(err).Send()
							break
						}
						mm.SetVolume(volume)
						ui.Update()
					}
				}
			case elms, ok := <-keyer.ch:
				if !ok {
					break LOOP
				}
				decoded := mm.KeyerState(elms)
				if decoded != "" {
					ui.decoder.SetText(decoded)
				}
			case <-mm.timer.C:
				decoded := mm.TimerExpire()
				if decoded != "" {
					ui.decoder.SetText(decoded)
				}
			}
		}
	}()
	if err := ui.app.Run(); err != nil {
		log.Logger = zerolog.New(
			zerolog.ConsoleWriter{
				Out: os.Stderr,
			},
		).With().Timestamp().Logger()

		log.Fatal().Err(err).Send()
	}
	cancel()
}
