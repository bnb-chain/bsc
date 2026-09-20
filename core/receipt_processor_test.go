package core

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
)

func TestAsyncReceiptBloomGeneratorConcurrentClose(t *testing.T) {
	generator := NewAsyncReceiptBloomGenerator(1)

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			generator.Apply(new(types.Receipt))
		})
	}
	wg.Go(generator.Close)
	wg.Wait()

	// Closing more than once and applying after close must be safe.
	generator.Close()
	generator.Apply(new(types.Receipt))
}
