package shanhecaptcha

import (
	"math"
	"sync"
	"time"
)

// RateLimiter 按 key 限流。key 形如 "ch:1.2.3.0/24"（拉挑战）或 "vf:1.2.3.0/24"（校验）。
// 默认是进程内令牌桶，多实例部署时换成共享存储的实现，通过 Config.Limiter 传入。
type RateLimiter interface {
	Allow(key string) bool
}

type memoryRateLimiter struct {
	cfg RateLimitConfig

	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   time.Time
	seen   time.Time
}

func newMemoryRateLimiter(cfg RateLimitConfig) *memoryRateLimiter {
	if !cfg.Enabled {
		return &memoryRateLimiter{cfg: cfg}
	}
	return &memoryRateLimiter{
		cfg:     cfg,
		buckets: make(map[string]*tokenBucket),
	}
}

func (rl *memoryRateLimiter) Allow(key string) bool {
	if !rl.cfg.Enabled {
		return true
	}
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.buckets) >= rl.cfg.MaxKeys {
		rl.sweepLocked(now)
	}

	b, ok := rl.buckets[key]
	if !ok {
		// 建新桶前先清掉十分钟没动的，防止 map 一直变大。
		if len(rl.buckets) >= rl.cfg.MaxKeys {
			rl.evictOldestLocked()
		}
		b = &tokenBucket{tokens: float64(rl.cfg.Burst), last: now}
		rl.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(float64(rl.cfg.Burst), b.tokens+elapsed*rl.cfg.RefillPerSecond)
		b.last = now
	}
	b.seen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *memoryRateLimiter) sweepLocked(now time.Time) {
	for k, b := range rl.buckets {
		if now.Sub(b.seen) > 10*time.Minute {
			delete(rl.buckets, k)
		}
	}
}

func (rl *memoryRateLimiter) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for k, b := range rl.buckets {
		if oldestKey == "" || b.seen.Before(oldest) {
			oldestKey = k
			oldest = b.seen
		}
	}
	if oldestKey != "" {
		delete(rl.buckets, oldestKey)
	}
}
