package mamoru

import (
	"os"
	"strings"
	"sync"

	"github.com/Mamoru-Foundation/mamoru-sniffer-go/mamoru_sniffer"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/log"
)

var (
	sniffer            *mamoru_sniffer.Sniffer
	SnifferConnectFunc = mamoru_sniffer.Connect
)

type statusProgress interface {
	Progress() ethereum.SyncProgress
}

type Sniffer struct {
	mu     sync.Mutex
	status statusProgress
	synced bool
}

func NewSniffer() *Sniffer {
	return &Sniffer{}
}

var syncing bool

func (s *Sniffer) CheckSynced() bool {
	if s.status == nil {
		return false
	}

	progress := s.status.Progress()

	log.Info("Mamoru Sniffer sync", "syncing", s.synced, "diff", int64(progress.HighestBlock)-int64(progress.CurrentBlock))

	if progress.CurrentBlock < progress.HighestBlock {
		s.synced = false
	}
	if s.synced {
		return true
	}

	if progress.CurrentBlock > 0 && progress.HighestBlock > 0 && syncing {
		log.Info("Mamoru Sniffer sync", "current", progress.CurrentBlock)
		if int64(progress.HighestBlock)-int64(progress.CurrentBlock) <= 0 {
			s.synced = true
		}
		return true
	}

	return false
}

func (s *Sniffer) SetDownloader(downloader statusProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = downloader
}

func (s *Sniffer) IsSnifferEnable() bool {
	isEnable, ok := os.LookupEnv("MAMORU_SNIFFER_ENABLE")

	return ok && isEnable == "true"
}

func (s *Sniffer) Connect() bool {
	if sniffer != nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if sniffer == nil {
		sniffer, err = SnifferConnectFunc()
		if err != nil {
			erst := strings.Replace(err.Error(), "\t", "", -1)
			erst = strings.Replace(erst, "\n", "", -1)
			//	erst = strings.Replace(erst, " ", "", -1)
			log.Error("Mamoru Sniffer connect", "err", erst)
			return false
		}
	}
	return true
}
