package eth

import (
	"testing"
	"time"
)

func TestStats(t *testing.T)  {
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
