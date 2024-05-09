package main

import (
	"os"
	"strings"

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
