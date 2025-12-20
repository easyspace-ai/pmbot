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
	// 15m BTC Up/Down 是同一市场里的两个 outcome token。
	// 这里用“每个方向每周期最多买一次”的状态，满足：
	// - UP 达到阈值就买 UP
	// - DOWN 达到阈值就买 DOWN
	// 且不会因为回报缺失/延迟而在同一方向反复下单。
	boughtYes bool // 本周期是否已触发买 YES(UP)
	boughtNo  bool // 本周期是否已触发买 NO(DOWN)

	// 仅用于调试/可视化
	entryPrice        float64   // 最近一次入场价格
	entrySide         types.Side // 最近一次入场方向
	pendingSellPrice  float64   // 预留字段：未来实现卖出时使用
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

	// 注意：
	// position.Truth 只会在 Fill 到达后更新 shares；回报缺失/延迟时 pos 可能长期为 0。
	// 因此这里“是否已经买过”完全用策略内部状态控制，避免重复下单。

	upPrice := tick.PYes
	downPrice := 1 - tick.PYes

	// 规则：不论 UP/DOWN 哪个到阈值都买（每方向每周期最多一次）
	if upPrice >= s.buyThreshold && !s.boughtYes {
		buyPrice := tick.BestAsk
		if buyPrice <= 0 {
			buyPrice = tick.PYes
		}
		if buyPrice < 0.01 || buyPrice > 0.99 {
			buyPrice = tick.PYes
		}

		s.entryPrice = buyPrice
		s.entrySide = types.SideYes
		s.boughtYes = true

		decisions = append(decisions, oms.OrderDecision{
			Side:   types.SideYes,
			Price:  buyPrice,
			Size:   s.orderSize,
			Reason: fmt.Sprintf("simple_threshold: UP价格=%.4f >= 阈值=%.4f, 买入YES(UP)", upPrice, s.buyThreshold),
		})
	}

	if downPrice >= s.buyThreshold && !s.boughtNo {
		// NO 的价格近似 = 1 - YES
		noPrice := 1 - tick.PYes
		buyPrice := noPrice
		if tick.BestBid > 0 {
			// 用 bestBid 推导 NO 侧价格（粗略）
			buyPrice = 1 - tick.BestBid
		}
		if buyPrice < 0.01 || buyPrice > 0.99 {
			buyPrice = noPrice
		}

		s.entryPrice = buyPrice
		s.entrySide = types.SideNo
		s.boughtNo = true

		decisions = append(decisions, oms.OrderDecision{
			Side:   types.SideNo,
			Price:  buyPrice,
			Size:   s.orderSize,
			Reason: fmt.Sprintf("simple_threshold: DOWN价格=%.4f >= 阈值=%.4f, 买入NO(DOWN)", downPrice, s.buyThreshold),
		})
	}

	return decisions
}

// Reset 重置策略状态（周期切换时调用）
func (s *SimpleThresholdStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.boughtYes = false
	s.boughtNo = false
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

