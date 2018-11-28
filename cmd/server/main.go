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
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

var (
	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	listenHTTP  = flag.String("l", ":9000", "Address to listen on for HTTP")
	listenHTTPS = flag.String("ls", "", "Address to listen on for HTTPS")
	acmeDomain  = flag.String("ad", "", "Domain to use for acme")
	acmeEmail   = flag.String("ae", "", "Email to use for acme")
	acmeURL     = flag.String("au", acme.LetsEncryptURL, "URL of acme service")
	acmeCache   = flag.String("ac", "acme_cache", "Path to acme cache")
)

func fatal(v interface{}) {
	level.Error(logger).Log("msg", v)
	os.Exit(1)
}

func main() {
	manager := manager.NewManager(logger)
	rand.Seed(time.Now().UTC().UnixNano())

	var acm *autocert.Manager

	if *acmeDomain != "" {
		if *acmeEmail == "" {
			fatal("Setting -ad requires -ae too")
		}
		acm = &autocert.Manager{
			Client: &acme.Client{
				DirectoryURL: *acmeURL,
			},
			Cache:      autocert.DirCache(*acmeCache),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(*acmeDomain),
		}
	}
	api := api.New(logger, manager, acm)
	if *listenHTTPS != "" {
		go func() {
			if err := api.ListenAndServe(*listenHTTPS); err != nil {
				fatal(err)
			}
		}()
	}
	if err := api.ListenAndServe(*listenHTTP); err != nil {
		fatal(err)
	}
}
