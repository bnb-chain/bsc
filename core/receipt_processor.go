package core

import (
	"bytes"
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
)

type ReceiptProcessor interface {
	Apply(receipt *types.Receipt)
}

var (
	_ ReceiptProcessor = (*ReceiptBloomGenertor)(nil)
	_ ReceiptProcessor = (*AsyncReceiptBloomGenertor)(nil)
)

func NewReceiptBloomGenertor() *ReceiptBloomGenertor {
	return &ReceiptBloomGenertor{}
}

type ReceiptBloomGenertor struct {
}

func (p *ReceiptBloomGenertor) Apply(receipt *types.Receipt) {
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
}

func NewAsyncReceiptBloomGenertor(length, workerSize int) *AsyncReceiptBloomGenertor {
	generator := &AsyncReceiptBloomGenertor{
		receipts: make(chan *types.Receipt, length),
		wg:       sync.WaitGroup{},
	}
	generator.startWorker(workerSize)
	return generator
}

type AsyncReceiptBloomGenertor struct {
	receipts chan *types.Receipt
	wg       sync.WaitGroup
}

func (p *AsyncReceiptBloomGenertor) startWorker(workerSize int) {
	p.wg.Add(workerSize)
	for i := 0; i < workerSize; i++ {
		go func() {
			defer p.wg.Done()
			for receipt := range p.receipts {
				if receipt != nil && bytes.Equal(receipt.Bloom[:], types.EmptyBloom[:]) {
					receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
				}
			}
		}()
	}
}

func (p *AsyncReceiptBloomGenertor) Apply(receipt *types.Receipt) {
	p.receipts <- receipt
}

func (p *AsyncReceiptBloomGenertor) Close() {
	close(p.receipts)
	p.wg.Wait()
}