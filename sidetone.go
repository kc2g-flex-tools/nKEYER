package main

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jfreymuth/pulse"
)

type SidetoneOscillator struct {
	mu        sync.Mutex
	stream    *pulse.PlaybackStream
	pitch     int
	volume    float32
	keyed     bool
	phase     float64
	rampLen   int
	rampLevel int
}

func NewSidetoneOscillator(pc *pulse.Client) (*SidetoneOscillator, error) {
	st := &SidetoneOscillator{}

	playback, err := pc.NewPlayback(pulse.Float32Reader(st.Generate),
		pulse.PlaybackLatency(0.02),
		pulse.PlaybackSampleRate(48000),
	)
	if err != nil {
		return nil, fmt.Errorf("pulse.NewPlayback failed: %w", err)
	}
	playback.Start()
	st.stream = playback
	return st, nil
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
	rampDur := max(dur/10, 5*time.Millisecond)
	st.rampLen = int(math.Round(48000 * rampDur.Seconds()))
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

func (st *SidetoneOscillator) Close() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stream.Close()
}
