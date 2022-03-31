// Package prlock implements a priority lock data structure.
package prlock

import "sync"

// Prlock implements a priority lock data structure.
type Prlock struct {
	data sync.RWMutex // Data mutex
	next sync.Mutex   // Next to access mutex
	low  sync.Mutex   // Low priority access mutex
}

// New creates a new priority queue.
func New() *Prlock {
	return &Prlock{}
}

// LockLow tries to get the lock for a low priority routine.
func (p *Prlock) LockLow() {
	p.low.Lock()
	p.next.Lock()
	p.data.Lock()
	p.next.Unlock()
}

// UnlockLow will release the lock obtained by a low priority routine.
func (p *Prlock) UnlockLow() {
	p.data.Unlock()
	p.low.Unlock()
}

// LockHigh tries to get the lock for a high priority routine.
func (p *Prlock) LockHigh() {
	p.next.Lock()
	p.data.Lock()
	p.next.Unlock()
}

// UnlockHigh will release the lock obtained by a high priority routine.
func (p *Prlock) UnlockHigh() {
	p.data.Unlock()
}

// LockRead tries to get the read lock for a routine.
func (p *Prlock) LockRead() {
	p.data.RLock()
}

// UnlockRead will release the read lock obtained by a routine.
func (p *Prlock) UnlockRead() {
	p.data.RUnlock()
}
