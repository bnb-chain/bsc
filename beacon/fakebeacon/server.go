package fakebeacon

import (
	"net/http"
	"strconv"

	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/gorilla/mux"
	"github.com/prysmaticlabs/prysm/v5/api/server/middleware"
)

const (
	DefaultAddr = "localhost"
	DefaultPort = 8686
)

type Config struct {
	Enable bool
	Addr   string
	Port   int
}

func defaultConfig() *Config {
	return &Config{
		Enable: false,
		Addr:   DefaultAddr,
		Port:   DefaultPort,
	}
}

type Service struct {
	cfg     *Config
	router  *mux.Router
	backend ethapi.Backend
}

func NewService(cfg *Config, backend ethapi.Backend) *Service {
	cfgs := defaultConfig()
	if cfg.Addr != "" {
		cfgs.Addr = cfg.Addr
	}
	if cfg.Port > 0 {
		cfgs.Port = cfg.Port
	}

	s := &Service{
		cfg:     cfgs,
		backend: backend,
	}
	router := s.newRouter()
	s.router = router
	return s
}

func (s *Service) Run() {
	_ = http.ListenAndServe(s.cfg.Addr+":"+strconv.Itoa(s.cfg.Port), s.router)
}

func (s *Service) newRouter() *mux.Router {
	r := mux.NewRouter()
	r.Use(middleware.NormalizeQueryValuesHandler)
	for _, e := range s.endpoints() {
		r.HandleFunc(e.path, e.handler).Methods(e.methods...)
	}
	return r
}

type endpoint struct {
	path    string
	handler http.HandlerFunc
	methods []string
}

func (s *Service) endpoints() []endpoint {
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
			handler: s.SidecarsMethod,
			methods: []string{http.MethodGet},
		},
	}
}
