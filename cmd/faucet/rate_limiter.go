package main

import (
	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	ips *lru.Cache // LRU cache to store IP addresses and their associated rate limiters
	r   rate.Limit // the rate limit, e.g., 5 requests per second
	b   int        // the burst size, e.g., allowing a burst of 10 requests at once. The rate limiter gets into action
	// only after this number exceeds
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

func (i *IPRateLimiter) addIP(ip string) *rate.Limiter {
	limiter := rate.NewLimiter(i.r, i.b)

	i.ips.Add(ip, limiter)

	return limiter
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	if limiter, exists := i.ips.Get(ip); exists {
		return limiter.(*rate.Limiter)
	}

	return i.addIP(ip)
}
