package safety

import (
	"fmt"
	"sync"
)

type RiskConfig struct {
	MaxDrawdownDaily float64 // e.g. 0.05 (5%)
	MaxPositionSize  float64 // e.g. 1000 USDC
	MaxOpenOrders    int     // e.g. 10
	MinSpread        float64 // e.g. 0.005 (0.5%) - Don't trade if spread is too tight/wide? No, avoid crossing wide spread.
	MaxSlippage      float64 // e.g. 0.02 (2%)
}

type RiskManager struct {
	config RiskConfig
	mu     sync.RWMutex

	// State
	startEquity    float64
	currentEquity  float64
	dailyLoss      float64
	openOrderCount int
}

func NewRiskManager(cfg RiskConfig) *RiskManager {
	return &RiskManager{
		config:      cfg,
		startEquity: 10000.0, // Should be synced with wallet balance
	}
}

func (rm *RiskManager) UpdateEquity(equity float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.currentEquity = equity
	rm.dailyLoss = rm.startEquity - rm.currentEquity
}

// CheckTrade verifies if a specific trade is safe
func (rm *RiskManager) CheckTrade(side string, size float64, price float64, bestBid, bestAsk float64) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// 1. Check Daily Drawdown
	if rm.dailyLoss/rm.startEquity > rm.config.MaxDrawdownDaily {
		return fmt.Errorf("daily drawdown limit reached: %.2f%%", (rm.dailyLoss/rm.startEquity)*100)
	}

	// 2. Check Position Size Limit
	// Simplified: Assuming size is roughly USD value for now
	if size > rm.config.MaxPositionSize {
		return fmt.Errorf("position size limit exceeded: %.2f > %.2f", size, rm.config.MaxPositionSize)
	}

	// 3. Check Spread / Liquidity
	spread := bestAsk - bestBid
	if spread <= 0 {
		return fmt.Errorf("invalid spread: %.4f", spread)
	}
	
	// If we are crossing the spread (Taker), check slippage impact
	// Simple check: Don't buy if Ask is too far from fair value?
	// Or simply, check if spread is too wide to be profitable
	if spread > 0.05 { // 5 cents spread
		return fmt.Errorf("spread too wide to trade: %.4f", spread)
	}

	return nil
}
