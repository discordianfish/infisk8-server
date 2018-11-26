package main

import (
	"flag"
	"math/rand"
	"os"
	"time"

	"github.com/discordianfish/infisk8-server/api"
	"github.com/discordianfish/infisk8-server/manager"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

var (
	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	listenAddr = flag.String("l", ":9000", "Address to listen on")
)

func fatal(v interface{}) {
	level.Error(logger).Log("msg", v)
	os.Exit(1)
}

func main() {
	manager := manager.NewManager(logger)
	rand.Seed(time.Now().UTC().UnixNano())
	api := api.New(logger, manager)
	if err := api.ListenAndServe(*listenAddr); err != nil {
		fatal(err)
	}
}
