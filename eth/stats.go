package eth

import (
	"github.com/ethereum/go-ethereum/log"
	"sort"
	"strconv"
	"sync"
	"time"
)

type stats struct {
	packets map[string]map[string]int
	accumulate map[string]map[string]int
	lock    *sync.RWMutex
}

func NewStats() *stats {
	return &stats{
		packets: 	make(map[string]map[string]int),
		accumulate: make(map[string]map[string]int),
		lock:		new(sync.RWMutex),
	}
}
func (s *stats) AddPacket(peerId string, name string)  {
	s.lock.Lock()
	defer  s.lock.Unlock()
	if _,ok := s.packets[peerId]; !ok {
		s.packets[peerId] = make(map[string]int)
	}
	p := s.packets[peerId]

	if count,ok := p[name]; ok {
		p[name] = count+1
	} else {
		p[name] = 1
	}
}
type kv struct {
	key string
	value map[string]int
}
func sumValue(m map[string]int) int {
	count := 0
	for _, v := range  m {
		count += v
	}
	return count
}
func (s *stats) Print(m map[string]map[string]int)  {
	var ss []kv

	for k, v := range m {
		ss = append(ss, kv{key:k, value:v})
	}
	sort.Slice(ss, func(i, j int) bool {
		v1 := ss[i].value
		v2 := ss[j].value
		count1 := sumValue(v1)
		count2 := sumValue(v2)
		return count1 >  count2
	})

	for _, v := range  ss {
		key := v.key
		p := s.packets[key]
		all := 0
		for name, count := range p {
			log.Warn(key + "::" + name + "=" + strconv.Itoa(count))
			all += count
		}
		log.Warn("-------" + key + "=" + strconv.Itoa(all) + "----------")

	}
}

func (s *stats) PrintAndReset()  {
	log.Warn("print one minute stats")
	s.lock.Lock()
	defer s.lock.Unlock()
	s.Print(s.packets)
	for key, _ := range s.packets {
		delete(s.packets, key)
	}
}
func (s *stats) Cron()  {
	log.Warn("start stats cron job, one minute")
	d := time.Minute*5
	t := time.NewTicker(d)
	defer t.Stop()

	for {
		<- t.C
		s.PrintAndReset()
	}
}
