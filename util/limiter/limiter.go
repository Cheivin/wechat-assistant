package limiter

import (
	"golang.org/x/time/rate"
	"sync"
)

type Limiter struct {
	limiterMap map[string]*rate.Limiter
	mu         *sync.RWMutex
	r          rate.Limit
	b          int
}

func NewLimiter(r rate.Limit, b int) *Limiter {
	i := &Limiter{
		limiterMap: make(map[string]*rate.Limiter),
		mu:         &sync.RWMutex{},
		r:          r,
		b:          b,
	}

	return i
}

func (i *Limiter) Add(key string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.add(key)
}

func (i *Limiter) add(key string) *rate.Limiter {
	limiter := rate.NewLimiter(i.r, i.b)
	i.limiterMap[key] = limiter
	return limiter
}

func (i *Limiter) Get(key string) *rate.Limiter {
	i.mu.Lock()
	limiter, exists := i.limiterMap[key]
	if !exists {
		i.mu.Unlock()
		return i.Add(key)
	}
	i.mu.Unlock()
	return limiter
}

func (i *Limiter) GetOrAdd(key string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()
	if limiter, exists := i.limiterMap[key]; exists {
		return limiter
	} else {
		return i.add(key)
	}
}
