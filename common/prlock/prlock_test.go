package prlock

import (
	"sync"
	"testing"
	"time"
)

func TestPrlock(t *testing.T) {
	data := 0
	prlock := New()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		prlock.LockLow()
		data = 1
		time.Sleep(time.Duration(2) * time.Millisecond)
		prlock.UnlockLow()
		wg.Done()
	}()
	go func() {
		prlock.LockHigh()
		data = 10
		time.Sleep(time.Duration(2) * time.Millisecond)
		prlock.UnlockHigh()
		wg.Done()
	}()
	wg.Wait()

	prlock.LockRead()
	defer prlock.UnlockRead()
	if data != 1 {
		t.Fatal("priority lock does not work correctly")
	}
}
