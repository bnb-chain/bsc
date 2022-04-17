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
	_ ReceiptProcessor = (*ReceiptBloomGenerator)(nil)
	_ ReceiptProcessor = (*AsyncReceiptBloomGenerator)(nil)
)

func NewReceiptBloomGenerator() *ReceiptBloomGenerator {
	return &ReceiptBloomGenerator{}
}

type ReceiptBloomGenerator struct {
}

func (p *ReceiptBloomGenerator) Apply(receipt *types.Receipt) {
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
}

func NewAsyncReceiptBloomGenerator(txNums int) *AsyncReceiptBloomGenerator {
	generator := &AsyncReceiptBloomGenerator{
		receipts: make(chan *types.Receipt, txNums),
	}
	generator.startWorker()
	return generator
}

type AsyncReceiptBloomGenerator struct {
	receipts chan *types.Receipt
	wg       sync.WaitGroup
	isClosed bool
}

func (p *AsyncReceiptBloomGenerator) startWorker() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for receipt := range p.receipts {
			if receipt != nil && bytes.Equal(receipt.Bloom[:], types.EmptyBloom[:]) {
				receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
			}
		}
	}()
}

func (p *AsyncReceiptBloomGenerator) Apply(receipt *types.Receipt) {
	if !p.isClosed {
		p.receipts <- receipt
	}
}

func (p *AsyncReceiptBloomGenerator) Close() {
	close(p.receipts)
	p.isClosed = true
	p.wg.Wait()
}
