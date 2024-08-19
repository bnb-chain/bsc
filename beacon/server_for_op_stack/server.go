package server_for_op_stack

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prysmaticlabs/prysm/v5/api/server"
)

type Config struct {
	BSCNodeURL string
	HostPort   string
}

type Service struct {
	cfg    Config
	router *mux.Router
}

func NewService(ctx context.Context, cfg *Config) *Service {
	Init(cfg.BSCNodeURL)
	router := newRouter()

	return &Service{
		cfg:    *cfg,
		router: router,
	}
}

func (s *Service) Run() {
	_ = http.ListenAndServe(s.cfg.HostPort, s.router)
}

func newRouter() *mux.Router {
	r := mux.NewRouter()
	r.Use(server.NormalizeQueryValuesHandler)
	for _, e := range endpoints() {
		r.HandleFunc(e.path, e.handler).Methods(e.methods...)
	}
	return r
}

type endpoint struct {
	path    string
	handler http.HandlerFunc
	methods []string
}

func endpoints() []endpoint {
	return []endpoint{
		{
			path:    versionMethod,
			handler: VersionMethod,
			methods: []string{http.MethodGet},
		},
		{
			path:    specMethod,
			handler: SpecMethod,
			methods: []string{http.MethodGet},
		},
		{
			path:    genesisMethod,
			handler: GenesisMethod,
			methods: []string{http.MethodGet},
		},
		{
			path:    sidecarsMethodPrefix,
			handler: SidecarsMethod,
			methods: []string{http.MethodGet},
		},
	}
}
