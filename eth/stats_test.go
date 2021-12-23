package eth

import (
	"github.com/ethereum/go-ethereum/log"
	"testing"
	"time"
)

func TestStats(t *testing.T)  {

	log.Warn(time.Now().Format("2006-01-02 15:04:05.000"))
	s := NewStats()
	s.AddPacket("0x123", "NewPooledTransaction")
	s.AddPacket("0x123", "NewPooledTransaction")
	s.AddPacket("0x123", "Transaction")
	s.AddPacket("0xabc", "Transaction")
	s.AddPacket("0xxxx", "Transaction")
	s.AddPacket("0xxxx", "Transaction")
	s.AddPacket("0xxxx", "Transaction")
	s.AddPacket("0xxxx", "Transaction")
	go s.Cron()
//	s.Print(s.packets)
	time.Sleep(time.Millisecond*2500)
}
