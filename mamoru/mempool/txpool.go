package mempool

import (
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/event"
)

type LightTxPool interface {
	SubscribeNewTxsEvent(chan<- core.NewTxsEvent) event.Subscription
}

type BcTxPool interface {
	SubscribeNewTxsEvent(chan<- core.NewTxsEvent) event.Subscription
	//SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription
}
