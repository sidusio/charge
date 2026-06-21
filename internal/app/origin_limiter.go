package app

import "sync"

type OriginLimiter struct {
	mu     sync.Mutex
	counts map[string]int
	limit  int
}

func NewOriginLimiter(limit int) *OriginLimiter {
	return &OriginLimiter{
		counts: make(map[string]int),
		limit:  limit,
	}
}

func (ol *OriginLimiter) TryAcquire(origin string) bool {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	count := ol.counts[origin]
	if count >= ol.limit {
		return false
	}

	ol.counts[origin] = count + 1

	return true
}

func (ol *OriginLimiter) Release(origin string) {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	count, ok := ol.counts[origin]
	if !ok {
		return
	}

	if count <= 1 {
		delete(ol.counts, origin)
	} else {
		ol.counts[origin] = count - 1
	}
}
