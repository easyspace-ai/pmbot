package strategy

import (
	"fmt"
	"sync"

	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

// SimpleThresholdStrategy 简单阈值策略
// 规则：
// 1. 当价格 > 60 分（0.60）时买入
// 2. 买入后，自动挂限价单卖出，价格为买入价 + 3 个点（0.03）
type SimpleThresholdStrategy struct {
	mu sync.RWMutex

	// 配置
	buyThreshold  float64 // 买入阈值（0.60 = 60分）
	profitTarget  float64 // 止盈目标（0.03 = 3分）
	orderSize     float64 // 订单大小

	// 状态
	entryPrice    float64 // 入场价格
	entrySide     types.Side // 入场方向
	hasPosition   bool    // 是否持仓
	pendingSellPrice float64 // 待挂卖出价格
}

// NewSimpleThresholdStrategy 创建简单阈值策略（使用默认配置）
func NewSimpleThresholdStrategy() *SimpleThresholdStrategy {
	return NewSimpleThresholdStrategyWithConfig(0.60, 0.03, 10.0)
}

// NewSimpleThresholdStrategyWithConfig 使用指定配置创建简单阈值策略
func NewSimpleThresholdStrategyWithConfig(buyThreshold, profitTarget, orderSize float64) *SimpleThresholdStrategy {
	return &SimpleThresholdStrategy{
		buyThreshold: buyThreshold,
		profitTarget: profitTarget,
		orderSize:    orderSize,
	}
}

// SetConfig 设置策略配置（用于运行时更新配置）
func (s *SimpleThresholdStrategy) SetConfig(buyThreshold, profitTarget, orderSize float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buyThreshold = buyThreshold
	s.profitTarget = profitTarget
	s.orderSize = orderSize
}

// Execute 执行策略
func (s *SimpleThresholdStrategy) Execute(tick types.MarketTick, intent types.Intent, pos struct {
	YesShares  float64
	NoShares   float64
	Confidence float64
}) []oms.OrderDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	var decisions []oms.OrderDecision

	// 调试日志：打印策略输入
	// 注意：这里没有 logger，我们会在 OMS 层面打印

	// 检查当前持仓状态
	hasYesPosition := pos.YesShares > 0
	hasNoPosition := pos.NoShares > 0
	currentHasPosition := hasYesPosition || hasNoPosition

	// 如果之前有持仓但现在没有了，重置状态（可能已卖出）
	if s.hasPosition && !currentHasPosition {
		s.hasPosition = false
		s.entryPrice = 0
		s.entrySide = types.SideUnknown
		s.pendingSellPrice = 0
	}

	// 策略逻辑：检查是否需要买入
	// 确保每个周期只买一次：检查是否已经有持仓（通过 position 或策略状态）
	if !currentHasPosition && !s.hasPosition {
		// 调试：检查价格条件
		upPrice := tick.PYes
		downPrice := 1 - tick.PYes
		// 检查 YES 价格是否超过阈值
		if tick.PYes >= s.buyThreshold {
			// 计算买入价格（使用 best ask）
			buyPrice := tick.BestAsk
			if buyPrice <= 0 {
				buyPrice = tick.PYes
			}
			// 确保价格在合理范围内
			if buyPrice < 0.01 || buyPrice > 0.99 {
				buyPrice = tick.PYes
			}

			// 计算卖出价格（买入价 + 3 分）
			sellPrice := buyPrice + s.profitTarget
			if sellPrice > 0.99 {
				sellPrice = 0.99 // 限制在合理范围内
			}

			// 记录状态
			s.entryPrice = buyPrice
			s.entrySide = types.SideYes
			s.hasPosition = true
			s.pendingSellPrice = sellPrice

			// 生成买入决策
			decisions = append(decisions, oms.OrderDecision{
				Side:   types.SideYes,
				Price:  buyPrice,
				Size:   s.orderSize,
				Reason: fmt.Sprintf("simple_threshold: UP价格=%.4f >= 阈值=%.4f, 买入YES", upPrice, s.buyThreshold),
			})

			// 同时挂卖出限价单（在买入价 + 3 分）
			decisions = append(decisions, oms.OrderDecision{
				Side:   types.SideNo, // 卖出 YES = 买入 NO
				Price:  sellPrice,
				Size:   s.orderSize,
				Reason: "simple_threshold: auto sell at entry + 3c",
			})
		} else if downPrice >= s.buyThreshold {
			// 检查 NO 价格是否超过阈值（1 - PYes）
			// 计算买入价格（NO 的价格 = 1 - YES 价格）
			noPrice := 1 - tick.PYes
			buyPrice := noPrice
			if tick.BestBid > 0 {
				// 使用 best bid 作为参考（买入 NO 时）
				buyPrice = 1 - tick.BestBid
			}
			if buyPrice < 0.01 || buyPrice > 0.99 {
				buyPrice = noPrice
			}

			// 计算卖出价格
			sellPrice := buyPrice + s.profitTarget
			if sellPrice > 0.99 {
				sellPrice = 0.99
			}

			// 记录状态
			s.entryPrice = buyPrice
			s.entrySide = types.SideNo
			s.hasPosition = true
			s.pendingSellPrice = sellPrice

			// 生成买入决策
			decisions = append(decisions, oms.OrderDecision{
				Side:   types.SideNo,
				Price:  buyPrice,
				Size:   s.orderSize,
				Reason: fmt.Sprintf("simple_threshold: DOWN价格=%.4f >= 阈值=%.4f, 买入NO", downPrice, s.buyThreshold),
			})

			// 同时挂卖出限价单
			decisions = append(decisions, oms.OrderDecision{
				Side:   types.SideYes, // 卖出 NO = 买入 YES
				Price:  sellPrice,
				Size:   s.orderSize,
				Reason: "simple_threshold: auto sell at entry + 3c",
			})
		} else {
			// 价格未达到阈值，不生成决策
			// 调试信息已在 OMS 层面打印
		}
	} else {
		// 已有持仓，检查是否需要更新卖出单
		// 如果当前价格已经超过预期的卖出价格，可能需要调整
		// 这里简化处理：如果价格变化较大，可以更新卖出价格
		if s.entrySide == types.SideYes {
			currentPrice := tick.PYes
			// 如果价格已经超过卖出价，说明可能已经成交或需要更新
			if currentPrice >= s.pendingSellPrice {
				// 价格已经达到或超过目标，不需要额外操作
				// 卖出单应该已经挂出或已成交
			}
		} else if s.entrySide == types.SideNo {
			currentPrice := 1 - tick.PYes
			if currentPrice >= s.pendingSellPrice {
				// 价格已经达到或超过目标
			}
		}
	}

	return decisions
}

// Reset 重置策略状态（周期切换时调用）
func (s *SimpleThresholdStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hasPosition = false
	s.entryPrice = 0
	s.entrySide = types.SideUnknown
	s.pendingSellPrice = 0
}

// SetOrderSize 设置订单大小
func (s *SimpleThresholdStrategy) SetOrderSize(size float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orderSize = size
}

// SetBuyThreshold 设置买入阈值
func (s *SimpleThresholdStrategy) SetBuyThreshold(threshold float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buyThreshold = threshold
}

// SetProfitTarget 设置止盈目标
func (s *SimpleThresholdStrategy) SetProfitTarget(target float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profitTarget = target
}

