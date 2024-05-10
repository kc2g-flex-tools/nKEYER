package main

import (
	"context"
	"flag"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

type TeaShim struct {
	p *tea.Program
}

func (t TeaShim) Write(val []byte) (int, error) {
	text := strings.TrimRight(string(val), "\n")
	go t.p.Printf("%s", text)
	return len(val), nil
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
	p := tea.NewProgram(ui, tea.WithFPS(15))
	log.Logger = zerolog.New(
		zerolog.ConsoleWriter{
			Out: TeaShim{p},
		},
	).With().Timestamp().Logger()

	go func() {
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
						p.Send(struct{}{})
					}
					if upd.Updated["pitch"] != "" {
						pitch, err := strconv.Atoi(upd.CurrentState["pitch"])
						if err != nil {
							log.Error().Err(err).Send()
							break
						}
						mm.SetPitch(pitch)
						p.Send(struct{}{})
					}
					if upd.Updated["mon_gain_cw"] != "" {
						volume, err := strconv.Atoi(upd.CurrentState["mon_gain_cw"])
						if err != nil {
							log.Error().Err(err).Send()
							break
						}
						mm.SetVolume(volume)
						p.Send(struct{}{})
					}
				}
			case elms, ok := <-keyer.ch:
				if !ok {
					break LOOP
				}
				decoded := mm.KeyerState(elms)
				if decoded != "" {
					p.Send(Decoded(decoded))
				}
			case <-mm.timer.C:
				decoded := mm.TimerExpire()
				if decoded != "" {
					p.Send(Decoded(decoded))
				}
			}
		}
	}()
	if _, err := p.Run(); err != nil {
		log.Logger = zerolog.New(
			zerolog.ConsoleWriter{
				Out: os.Stderr,
			},
		).With().Timestamp().Logger()

		log.Fatal().Err(err).Send()
	}
	cancel()
}
