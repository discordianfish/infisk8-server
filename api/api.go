package api

import (
	"encoding/base64"
	"encoding/json"
	"crypto/tls"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/discordianfish/infisk8-server/manager"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/go-kit/kit/log/level"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/cors"
	"golang.org/x/crypto/acme/autocert"
)

const (
	sdMaxLen = 10240
)

type API struct {
	logger  log.Logger
	manager *manager.Manager
	handler http.Handler
	acm     *autocert.Manager
}

func New(logger log.Logger, manager *manager.Manager, acm *autocert.Manager) *API {
	a := &API{
		logger:  logger,
		manager: manager,
		acm:     acm,
	}

	router := httprouter.New()
	router.GET("/pools", a.HandlePools)
	router.PUT("/pool/:pool", a.HandleCreate)
	router.POST("/pool/:pool/join/:id", a.HandleJoin)
	router.Handler("GET", "/metrics", promhttp.Handler())
	a.handler = a.acm.HTTPHandler(cors.Default().Handler(router))
	return a
}

func (a *API) ListenAndServe(addr string) error {
	level.Info(a.logger).Log("msg", "Listening", "addr", addr, "proto", "http")
	return http.ListenAndServe(addr, a.handler)
}

func (a *API) ListenAndServeTLS(addr string) error {
	tlsConfig := &tls.Config{
		GetCertificate: a.acm.GetCertificate,
	}
	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: a.handler}
	level.Info(a.logger).Log("msg", "Listening", "addr", addr, "proto", "https")
	return server.Serve(ln)
}

type poolsResponse struct {
	Pools []Pool `json:"pools"`
}

type Pool struct {
	Name string `json:"name"`
}

func (a *API) HandlePools(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	pr := &poolsResponse{
		Pools: []Pool{},
	}
	for _, pn := range a.manager.Pools() {
		pr.Pools = append(pr.Pools, Pool{Name: pn})
	}
	json.NewEncoder(w).Encode(pr)
}

func (a *API) HandleCreate(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	pool, err := a.manager.NewPool(ps.ByName("pool"))
	if err != nil {
		level.Warn(a.logger).Log("msg", "Couldn't create pool", "error", err)
		http.Error(w, "Couldn't create pool", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(pool)
}

func (a *API) HandleJoin(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	level.Debug(a.logger).Log("msg", r.Method, "path", r.URL.Path)
	pool, err := a.manager.Pool(ps.ByName("pool"))
	if err != nil {
		level.Warn(a.logger).Log("msg", "Couldn't join pool", "error", err)
		http.Error(w, "Couldn't join pool", http.StatusInternalServerError)
		return
	}

	sd, err := ioutil.ReadAll(base64.NewDecoder(base64.StdEncoding, &io.LimitedReader{R: r.Body, N: sdMaxLen}))
	if err != nil {
		level.Debug(a.logger).Log("msg", "Invalid base64", "err", err, "sd", sd)
		http.Error(w, "Invalid base64", http.StatusBadRequest)
		return
	}

	answer, err := pool.NewSession(sd, ps.ByName("id"))
	if err != nil {
		level.Debug(a.logger).Log("msg", "Error creating session", "err", err, "sd", sd)
		http.Error(w, "Invalid SD", http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(answer)
}
