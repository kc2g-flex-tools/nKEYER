package main

import (
	"fmt"
	"os"

	log "github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	Dit int = 1 << iota
	Dah
)

type Keyer struct {
	file *os.File
	ch   chan int
}

func NewKeyer() (*Keyer, error) {
	keyDev, err := os.Open(cfg.KeyDev)
	if err != nil {
		return nil, fmt.Errorf("failed to open keyer device: %w", err)
	}

	fd := int(keyDev.Fd())
	ch := make(chan int, 10)

	go func(ch chan int) {
		defer func() { close(ch) }()
		// This is interrupted by the handle getting closed, causing an I/O error.
		for {
			err := unix.IoctlSetInt(fd, unix.TIOCMIWAIT, unix.TIOCM_CD|unix.TIOCM_CTS)
			if err != nil {
				log.Error().Err(err).Msg("ioctl TIOCMIWAIT")
				return
			}
			bits, err := unix.IoctlGetInt(fd, unix.TIOCMGET)
			if err != nil {
				log.Error().Err(err).Msg("ioctl TIOCMGET")
				return
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
	}(ch)
	return &Keyer{
		file: keyDev,
		ch:   ch,
	}, nil
}

func (k *Keyer) Close() {
	k.file.Close()
}
