package main

import (
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	ips *lru.Cache
	mu  *sync.RWMutex
	r   rate.Limit
	b   int
}

func NewIPRateLimiter(r rate.Limit, b int, size int) (*IPRateLimiter, error) {
	cache, err := lru.New(size)
	if err != nil {
		return nil, err
	}

	i := &IPRateLimiter{
		ips: cache,
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}

	return i, nil
}

func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter := rate.NewLimiter(i.r, i.b)

	i.ips.Add(ip, limiter)

	return limiter
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	if limiter, exists := i.ips.Get(ip); exists {
		return limiter.(*rate.Limiter)
	}

	return i.AddIP(ip)
}
