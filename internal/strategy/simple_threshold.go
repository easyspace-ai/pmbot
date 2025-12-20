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
// 2. 本项目的执行层当前仅实现“买入”（SideBuy），未实现卖出（SideSell）。
//    因此策略只负责触发一次买入信号；止盈/止损需要在执行层补齐卖出能力后再实现。
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

	// 注意：
	// position.Truth 只会在 Fill 到达后更新 shares。
	// 如果这里用“pos 仍为 0”来判定“已卖出/没持仓”，会在成交回报缺失或延迟时触发重复下单。
	// 为了安全起见：一旦本周期触发过入场（s.hasPosition=true），就不再自动重置，
	// 只在周期切换（Reset）时清空状态。

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

			// 记录状态
			s.entryPrice = buyPrice
			s.entrySide = types.SideYes
			s.hasPosition = true
			s.pendingSellPrice = 0

			// 生成买入决策
			decisions = append(decisions, oms.OrderDecision{
				Side:   types.SideYes,
				Price:  buyPrice,
				Size:   s.orderSize,
				Reason: fmt.Sprintf("simple_threshold: UP价格=%.4f >= 阈值=%.4f, 买入YES", upPrice, s.buyThreshold),
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

			// 记录状态
			s.entryPrice = buyPrice
			s.entrySide = types.SideNo
			s.hasPosition = true
			s.pendingSellPrice = 0

			// 生成买入决策
			decisions = append(decisions, oms.OrderDecision{
				Side:   types.SideNo,
				Price:  buyPrice,
				Size:   s.orderSize,
				Reason: fmt.Sprintf("simple_threshold: DOWN价格=%.4f >= 阈值=%.4f, 买入NO", downPrice, s.buyThreshold),
			})
		} else {
			// 价格未达到阈值，不生成决策
			// 调试信息已在 OMS 层面打印
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

