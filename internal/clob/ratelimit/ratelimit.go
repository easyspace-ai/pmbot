package ratelimit

import (
	"context"
	"sync"
	"time"
)

// RateLimiter 速率限制器接口
type RateLimiter interface {
	Wait(ctx context.Context) error
	Allow() bool
	GetRemaining() int
	GetResetTime() time.Time
}

// TokenBucket 令牌桶速率限制器
type TokenBucket struct {
	capacity     int           // 桶容量
	tokens       int           // 当前令牌数
	refillRate   int           // 每秒补充的令牌数
	windowSize   time.Duration // 时间窗口大小
	lastRefill   time.Time     // 上次补充时间
	mu           sync.Mutex
}

// NewTokenBucket 创建新的令牌桶
func NewTokenBucket(capacity, refillRate int, windowSize time.Duration) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		windowSize: windowSize,
		lastRefill: time.Now(),
	}
}

// refill 补充令牌
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	
	// 计算应该补充的令牌数
	tokensToAdd := int(elapsed.Seconds()) * tb.refillRate
	if tokensToAdd > 0 {
		tb.tokens = min(tb.capacity, tb.tokens+tokensToAdd)
		tb.lastRefill = now
	}
}

// Allow 检查是否允许请求
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	
	tb.refill()
	
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

// Wait 等待直到允许请求
func (tb *TokenBucket) Wait(ctx context.Context) error {
	for {
		if tb.Allow() {
			return nil
		}
		
		// 计算需要等待的时间
		tb.mu.Lock()
		tb.refill()
		waitTime := time.Duration(0)
		if tb.tokens == 0 && tb.refillRate > 0 {
			waitTime = time.Second / time.Duration(tb.refillRate)
		}
		tb.mu.Unlock()
		
		if waitTime > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		} else {
			// 如果 refillRate 为 0，等待一个时间窗口
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(tb.windowSize):
				continue
			}
		}
	}
}

// GetRemaining 获取剩余令牌数
func (tb *TokenBucket) GetRemaining() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

// GetResetTime 获取重置时间
func (tb *TokenBucket) GetResetTime() time.Time {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens < tb.capacity {
		// 计算需要多长时间才能填满
		needed := tb.capacity - tb.tokens
		if tb.refillRate > 0 {
			seconds := float64(needed) / float64(tb.refillRate)
			return time.Now().Add(time.Duration(seconds) * time.Second)
		}
	}
	return time.Now()
}

// SlidingWindow 滑动窗口速率限制器
type SlidingWindow struct {
	limit      int           // 限制数量
	windowSize time.Duration // 窗口大小
	requests   []time.Time   // 请求时间戳
	mu         sync.Mutex
}

// NewSlidingWindow 创建新的滑动窗口速率限制器
func NewSlidingWindow(limit int, windowSize time.Duration) *SlidingWindow {
	return &SlidingWindow{
		limit:      limit,
		windowSize: windowSize,
		requests:   make([]time.Time, 0),
	}
}

// Allow 检查是否允许请求
func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	
	now := time.Now()
	cutoff := now.Add(-sw.windowSize)
	
	// 移除窗口外的请求
	validRequests := make([]time.Time, 0)
	for _, req := range sw.requests {
		if req.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}
	sw.requests = validRequests
	
	// 检查是否超过限制
	if len(sw.requests) >= sw.limit {
		return false
	}
	
	// 添加当前请求
	sw.requests = append(sw.requests, now)
	return true
}

// Wait 等待直到允许请求
func (sw *SlidingWindow) Wait(ctx context.Context) error {
	for {
		if sw.Allow() {
			return nil
		}
		
		// 计算需要等待的时间
		sw.mu.Lock()
		oldest := time.Now()
		if len(sw.requests) > 0 {
			oldest = sw.requests[0]
		}
		waitTime := sw.windowSize - time.Since(oldest)
		sw.mu.Unlock()
		
		if waitTime > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}
	}
}

// GetRemaining 获取剩余请求数
func (sw *SlidingWindow) GetRemaining() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	
	now := time.Now()
	cutoff := now.Add(-sw.windowSize)
	
	validCount := 0
	for _, req := range sw.requests {
		if req.After(cutoff) {
			validCount++
		}
	}
	
	return max(0, sw.limit-validCount)
}

// GetResetTime 获取重置时间
func (sw *SlidingWindow) GetResetTime() time.Time {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	
	if len(sw.requests) == 0 {
		return time.Now()
	}
	
	oldest := sw.requests[0]
	return oldest.Add(sw.windowSize)
}

// RateLimitManager 速率限制管理器
type RateLimitManager struct {
	limiters map[string]RateLimiter
	mu       sync.RWMutex
}

// NewRateLimitManager 创建新的速率限制管理器
func NewRateLimitManager() *RateLimitManager {
	manager := &RateLimitManager{
		limiters: make(map[string]RateLimiter),
	}
	
	// 初始化常用端点的速率限制器
	manager.initDefaultLimiters()
	
	return manager
}

// initDefaultLimiters 初始化默认的速率限制器
func (rlm *RateLimitManager) initDefaultLimiters() {
	// CLOB API 限制
	rlm.limiters["clob:order:post"] = NewTokenBucket(2400, 240, 10*time.Second) // 2400/10s, 240/s
	rlm.limiters["clob:order:delete"] = NewTokenBucket(2400, 240, 10*time.Second)
	rlm.limiters["clob:orders:post"] = NewTokenBucket(800, 80, 10*time.Second) // 800/10s, 80/s
	rlm.limiters["clob:orders:delete"] = NewTokenBucket(800, 80, 10*time.Second)
	rlm.limiters["clob:orders:get"] = NewSlidingWindow(150, 10*time.Second) // 150/10s
	rlm.limiters["clob:trades:get"] = NewSlidingWindow(150, 10*time.Second) // 150/10s
	rlm.limiters["clob:book:get"] = NewSlidingWindow(200, 10*time.Second) // 200/10s
	rlm.limiters["clob:price:get"] = NewSlidingWindow(200, 10*time.Second) // 200/10s
	
	// Gamma API 限制
	rlm.limiters["gamma:markets:get"] = NewSlidingWindow(125, 10*time.Second) // 125/10s
	rlm.limiters["gamma:events:get"] = NewSlidingWindow(100, 10*time.Second) // 100/10s
	rlm.limiters["gamma:general"] = NewSlidingWindow(750, 10*time.Second) // 750/10s
	
	// Data API 限制
	rlm.limiters["data:general"] = NewSlidingWindow(200, 10*time.Second) // 200/10s
	rlm.limiters["data:trades:get"] = NewSlidingWindow(75, 10*time.Second) // 75/10s
}

// GetLimiter 获取指定端点的速率限制器
func (rlm *RateLimitManager) GetLimiter(endpoint string) RateLimiter {
	rlm.mu.RLock()
	defer rlm.mu.RUnlock()
	
	if limiter, exists := rlm.limiters[endpoint]; exists {
		return limiter
	}
	
	// 如果没有找到，返回通用限制器
	if limiter, exists := rlm.limiters["clob:general"]; exists {
		return limiter
	}
	
	// 默认限制器（5000/10s）
	return NewSlidingWindow(5000, 10*time.Second)
}

// Wait 等待直到允许请求
func (rlm *RateLimitManager) Wait(ctx context.Context, endpoint string) error {
	limiter := rlm.GetLimiter(endpoint)
	return limiter.Wait(ctx)
}

// Allow 检查是否允许请求
func (rlm *RateLimitManager) Allow(endpoint string) bool {
	limiter := rlm.GetLimiter(endpoint)
	return limiter.Allow()
}

// GetRemaining 获取剩余请求数
func (rlm *RateLimitManager) GetRemaining(endpoint string) int {
	limiter := rlm.GetLimiter(endpoint)
	return limiter.GetRemaining()
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

