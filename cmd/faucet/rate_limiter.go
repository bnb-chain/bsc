package main

import (
	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	ips *lru.Cache
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
		r:   r,
		b:   b,
	}

	return i, nil
}

func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {

	limiter := rate.NewLimiter(i.r, i.b)

	i.ips.Add(ip, limiter)

	return limiter
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {

	if limiter, exists := i.ips.Get(ip); exists {
		return limiter.(*rate.Limiter)
	}

	return i.AddIP(ip)
}
