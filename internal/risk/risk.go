package risk

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/types"
)

// Supervisor decides whether the system is allowed to trade.
// It can be extended with multiple reporters (ws health, rest latency, drift, etc.).
type Supervisor struct {
	// DataQualityMin triggers kill-switch when adapter quality falls below this.
	DataQualityMin float64

	// FreezeP and FreezeEntropy are additional guardrails. Brain also detects freeze.
	FreezeP       float64
	FreezeEntropy float64

	// Circuit Breaker configuration
	cbConfig *CircuitBreakerConfig
	cb       *CircuitBreaker
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	Enabled              bool
	MaxPositionPerMarket int64   // 单市场最大仓位（合约数）
	MaxTotalPosition     int64   // 总最大仓位（合约数）
	MaxDailyLoss         float64 // 最大日亏损（美元）
	MaxConsecutiveErrors int32   // 最大连续错误数
	CooldownSecs         int64   // 冷却期（秒）
}

// DefaultCircuitBreakerConfig 返回默认熔断器配置
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		Enabled:              true,
		MaxPositionPerMarket: 50000,
		MaxTotalPosition:     100000,
		MaxDailyLoss:         500.0,
		MaxConsecutiveErrors: 5,
		CooldownSecs:         300, // 5分钟
	}
}

// CircuitBreaker 熔断器实现
type CircuitBreaker struct {
	config *CircuitBreakerConfig

	// 是否已熔断
	halted atomic.Bool

	// 熔断时间
	trippedAt   time.Time
	trippedAtMu sync.RWMutex

	// 熔断原因
	tripReason   string
	tripReasonMu sync.RWMutex

	// 连续错误计数
	consecutiveErrors atomic.Int32

	// 日P&L跟踪（美分）
	dailyPnlCents atomic.Int64

	// 每个市场的仓位
	positions   map[string]int64
	positionsMu sync.RWMutex

	// 日P&L重置时间（每天UTC 00:00重置）
	lastDailyReset time.Time
	resetMu        sync.Mutex
}

// NewCircuitBreaker 创建新的熔断器
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}
	return &CircuitBreaker{
		config:        config,
		positions:     make(map[string]int64),
		lastDailyReset: time.Now().UTC(),
	}
}

// CanExecute 检查是否可以执行交易
func (cb *CircuitBreaker) CanExecute(marketID string, contracts int64) error {
	if !cb.config.Enabled {
		return nil
	}

	// 检查是否已熔断
	if cb.halted.Load() {
		cb.tripReasonMu.RLock()
		reason := cb.tripReason
		cb.tripReasonMu.RUnlock()
		return &CircuitBreakerError{Reason: reason}
	}

	// 检查冷却期
	cb.trippedAtMu.RLock()
	trippedAt := cb.trippedAt
	cb.trippedAtMu.RUnlock()
	if !trippedAt.IsZero() {
		elapsed := time.Since(trippedAt).Seconds()
		if elapsed < float64(cb.config.CooldownSecs) {
			return &CircuitBreakerError{Reason: "cooldown_period"}
		}
		// 冷却期已过，自动重置
		cb.reset()
	}

	// 重置日P&L（如果跨天）
	cb.maybeResetDailyPnl()

	cb.positionsMu.RLock()
	defer cb.positionsMu.RUnlock()

	// 检查单市场仓位限制
	if pos, ok := cb.positions[marketID]; ok {
		newPosition := pos + contracts
		if newPosition > cb.config.MaxPositionPerMarket {
			return &CircuitBreakerError{
				Reason:  "max_position_per_market",
				Details: map[string]interface{}{
					"market":  marketID,
					"current": pos,
					"new":     newPosition,
					"limit":   cb.config.MaxPositionPerMarket,
				},
			}
		}
	}

	// 检查总仓位限制
	total := int64(0)
	for _, pos := range cb.positions {
		total += pos
	}
	if total+contracts > cb.config.MaxTotalPosition {
		return &CircuitBreakerError{
			Reason:  "max_total_position",
			Details: map[string]interface{}{
				"current": total,
				"new":     total + contracts,
				"limit":   cb.config.MaxTotalPosition,
			},
		}
	}

	// 检查日亏损限制
	dailyLoss := -float64(cb.dailyPnlCents.Load()) / 100.0
	if dailyLoss > cb.config.MaxDailyLoss {
		return &CircuitBreakerError{
			Reason:  "max_daily_loss",
			Details: map[string]interface{}{
				"loss":  dailyLoss,
				"limit": cb.config.MaxDailyLoss,
			},
		}
	}

	return nil
}

// RecordSuccess 记录成功执行
func (cb *CircuitBreaker) RecordSuccess(marketID string, contracts int64, pnl float64) {
	// 重置连续错误计数
	cb.consecutiveErrors.Store(0)

	// 更新P&L
	pnlCents := int64(pnl * 100.0)
	cb.dailyPnlCents.Add(pnlCents)

	// 更新仓位
	cb.positionsMu.Lock()
	cb.positions[marketID] += contracts
	cb.positionsMu.Unlock()
}

// RecordError 记录错误
func (cb *CircuitBreaker) RecordError() {
	errors := cb.consecutiveErrors.Add(1)
	if errors >= cb.config.MaxConsecutiveErrors {
		cb.Trip("consecutive_errors", map[string]interface{}{
			"count": errors,
			"limit": cb.config.MaxConsecutiveErrors,
		})
	}
}

// RecordPnl 记录P&L更新（不执行交易时）
func (cb *CircuitBreaker) RecordPnl(pnl float64) {
	pnlCents := int64(pnl * 100.0)
	cb.dailyPnlCents.Add(pnlCents)
}

// Trip 触发熔断
func (cb *CircuitBreaker) Trip(reason string, details map[string]interface{}) {
	if !cb.config.Enabled {
		return
	}

	cb.halted.Store(true)
	cb.trippedAtMu.Lock()
	cb.trippedAt = time.Now().UTC()
	cb.trippedAtMu.Unlock()

	cb.tripReasonMu.Lock()
	if details != nil {
		cb.tripReason = reason + " " + formatDetails(details)
	} else {
		cb.tripReason = reason
	}
	cb.tripReasonMu.Unlock()
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	cb.halted.Store(false)
	cb.trippedAtMu.Lock()
	cb.trippedAt = time.Time{}
	cb.trippedAtMu.Unlock()
	cb.tripReasonMu.Lock()
	cb.tripReason = ""
	cb.tripReasonMu.Unlock()
	cb.consecutiveErrors.Store(0)
}

// reset 内部重置（不重置日P&L）
func (cb *CircuitBreaker) reset() {
	cb.Reset()
}

// maybeResetDailyPnl 如果跨天则重置日P&L
func (cb *CircuitBreaker) maybeResetDailyPnl() {
	cb.resetMu.Lock()
	defer cb.resetMu.Unlock()

	now := time.Now().UTC()
	if now.Day() != cb.lastDailyReset.Day() || now.Month() != cb.lastDailyReset.Month() || now.Year() != cb.lastDailyReset.Year() {
		cb.dailyPnlCents.Store(0)
		cb.lastDailyReset = now
	}
}

// IsHalted 检查是否已熔断
func (cb *CircuitBreaker) IsHalted() bool {
	return cb.halted.Load()
}

// GetStatus 获取熔断器状态
func (cb *CircuitBreaker) GetStatus() CircuitBreakerStatus {
	cb.positionsMu.RLock()
	totalPosition := int64(0)
	marketCount := len(cb.positions)
	for _, pos := range cb.positions {
		totalPosition += pos
	}
	cb.positionsMu.RUnlock()

	cb.tripReasonMu.RLock()
	reason := cb.tripReason
	cb.tripReasonMu.RUnlock()

	return CircuitBreakerStatus{
		Enabled:           cb.config.Enabled,
		Halted:            cb.halted.Load(),
		TripReason:        reason,
		ConsecutiveErrors: cb.consecutiveErrors.Load(),
		DailyPnl:          float64(cb.dailyPnlCents.Load()) / 100.0,
		TotalPosition:     totalPosition,
		MarketCount:       marketCount,
	}
}

// CircuitBreakerStatus 熔断器状态
type CircuitBreakerStatus struct {
	Enabled           bool
	Halted            bool
	TripReason        string
	ConsecutiveErrors int32
	DailyPnl          float64
	TotalPosition     int64
	MarketCount       int
}

// CircuitBreakerError 熔断器错误
type CircuitBreakerError struct {
	Reason  string
	Details map[string]interface{}
}

func (e *CircuitBreakerError) Error() string {
	if e.Details != nil {
		return e.Reason + " " + formatDetails(e.Details)
	}
	return e.Reason
}

func formatDetails(details map[string]interface{}) string {
	// 简单格式化，实际可以使用更复杂的格式化
	return ""
}

func New() *Supervisor {
	cbConfig := DefaultCircuitBreakerConfig()
	return &Supervisor{
		DataQualityMin: 0.6,
		FreezeP:        0.99,
		FreezeEntropy:  0.05,
		cbConfig:       cbConfig,
		cb:             NewCircuitBreaker(cbConfig),
	}
}

// NewWithConfig 使用配置创建 Supervisor
func NewWithConfig(cbConfig *CircuitBreakerConfig) *Supervisor {
	if cbConfig == nil {
		cbConfig = DefaultCircuitBreakerConfig()
	}
	return &Supervisor{
		DataQualityMin: 0.6,
		FreezeP:        0.99,
		FreezeEntropy:  0.05,
		cbConfig:       cbConfig,
		cb:             NewCircuitBreaker(cbConfig),
	}
}

func (s *Supervisor) Evaluate(t types.MarketTick, pos *position.Truth) types.RiskState {
	rs := types.RiskState{Ts: time.Now().UTC()}

	if t.DataQuality > 0 && t.DataQuality < s.DataQualityMin {
		rs.KillSwitch = true
		rs.Reason = "data_quality_low"
		return rs
	}

	// Position truth must be trusted to trade.
	if pos != nil && !pos.Trusted() {
		rs.KillSwitch = true
		rs.Reason = "position_untrusted"
		return rs
	}

	// 检查熔断器
	if s.cb != nil && s.cb.IsHalted() {
		rs.KillSwitch = true
		status := s.cb.GetStatus()
		rs.Reason = "circuit_breaker: " + status.TripReason
		return rs
	}

	p := t.PYes
	H := entropy(p)
	if (p >= s.FreezeP || p <= 1-s.FreezeP) && H <= s.FreezeEntropy {
		rs.Freeze = true
		rs.Reason = "consensus_frozen"
	}

	return rs
}

// CheckCircuitBreaker 检查熔断器（在执行订单前调用）
func (s *Supervisor) CheckCircuitBreaker(marketID string, contracts int64) error {
	if s.cb == nil {
		return nil
	}
	return s.cb.CanExecute(marketID, contracts)
}

// RecordExecutionSuccess 记录执行成功
func (s *Supervisor) RecordExecutionSuccess(marketID string, contracts int64, pnl float64) {
	if s.cb != nil {
		s.cb.RecordSuccess(marketID, contracts, pnl)
	}
}

// RecordExecutionError 记录执行错误
func (s *Supervisor) RecordExecutionError() {
	if s.cb != nil {
		s.cb.RecordError()
	}
}

// GetCircuitBreakerStatus 获取熔断器状态
func (s *Supervisor) GetCircuitBreakerStatus() CircuitBreakerStatus {
	if s.cb == nil {
		return CircuitBreakerStatus{Enabled: false}
	}
	return s.cb.GetStatus()
}

func entropy(p float64) float64 {
	// keep local copy to avoid import cycles. Accuracy is enough for gatekeeping.
	if p <= 0 || p >= 1 {
		return 0
	}
	return -(p*math.Log(p) + (1-p)*math.Log(1-p))
}
