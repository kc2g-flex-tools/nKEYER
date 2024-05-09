package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kc2g-flex-tools/flexclient"
	log "github.com/rs/zerolog/log"
)

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

var idx int

func sendFlexCW(pressed bool) {
	ts := time.Now().UnixMilli() % 65536
	cwState := 0
	if pressed {
		cwState = 1
	}
	cmd := fmt.Sprintf("cw key %d time=0x%04X index=%d client_handle=%s", cwState, ts, idx, ClientID)
	fc.SendCmd(cmd)
	idx++
}
