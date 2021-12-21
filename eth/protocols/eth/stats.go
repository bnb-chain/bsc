package eth

import (
	"github.com/ethereum/go-ethereum/log"
	"time"
)

type stats struct {
	packets map[string]map[uint64]int
	accumulate map[string]map[uint64]int
}

func NewStats() *stats {
	return &stats{
		packets: 	make(map[string]map[uint64]int),
		accumulate: make(map[string]map[uint64]int),
	}
}

func (s *stats) AddPeer(peer *Peer)  {
	s.packets[peer.ID()] = make(map[uint64]int)
}

func (s *stats) ClosePeer(peer *Peer)  {
	delete(s.packets, peer.ID())
}

func (s *stats) AddPacket(peer *Peer, code uint64)  {
	p := s.packets[peer.ID()]

	if count,ok := p[code]; ok {
		p[code] = count+1
	} else {
		p[code] = 1
	}
}

func (s *stats) Print(m map[string]map[uint64]int)  {
	for id, packets := range m {
		log.Warn("peer id:", id)
		for name, count := range packets {
			log.Warn(string(name), ":", count)
		}
	}
}

func (s *stats) Cron()  {
	d := time.Minute

	t := time.NewTicker(d)
	defer t.Stop()

	for {
		<- t.C
		log.Warn("print accumulate status")
		s.Print(s.accumulate)
		log.Warn("print one minute stats")
		s.Print(s.packets)
		for _, packets := range s.packets {
			for key, _ := range packets {
				packets[key] = 0
			}
		}
	}
}
