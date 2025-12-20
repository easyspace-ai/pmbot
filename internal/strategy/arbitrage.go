package strategy

import (
	"fmt"
	"sync"
	"time"

	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

// ArbitrageStrategy 实现无风险套利策略 (Volume Bot)
// 监控 Orderbook，当 Ask(Yes) + Ask(No) < 1.0 - minProfit 时同时买入。
// 这种策略被称为 "Straddle Arbitrage" 或 "Risk-Free Arbitrage"。
//
// 逻辑：
// 1. 监听 MarketTick，获取 BestAsk(Yes) 和 BestAsk(No)。
// 2. 计算合成成本 Cost = Ask(Yes) + Ask(No)。
// 3. 如果 Cost < 1.0 (考虑手续费和最小利润)，则存在套利空间。
// 4. 同时发送 Buy(Yes) 和 Buy(No) 订单。
// 5. 持有到期，其中一边归0，另一边归1。总收入=1。
// 6. 净利润 = 1 - Cost。
type ArbitrageStrategy struct {
	mu sync.RWMutex

	// 配置
	minProfitSpread float64 // 最小利润空间 (例如 0.005 = 0.5%)
	orderSizeUSDC   float64 // 每次下单的 USDC 金额 (注意：是每条腿的金额还是总金额？这里指总金额的一半，即每条腿的买入金额)
	
	// 状态
	lastTradeTime time.Time
	cooldown      time.Duration
}

// NewArbitrageStrategy 创建套利策略
func NewArbitrageStrategy() *ArbitrageStrategy {
	return &ArbitrageStrategy{
		minProfitSpread: 0.002, // 0.2% 利润空间 (Polymarket 通常免 Maker 费，Taker 费视情况而定，保守起见设个阈值)
		orderSizeUSDC:   5.0,   // 每次单边投入 $5 (总投入 $10)，符合 Volume Bot "小单高频" 的特点
		cooldown:        500 * time.Millisecond, // 冷却时间，防止短时间内重复下单
	}
}

// Reset 重置策略状态
func (s *ArbitrageStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 套利策略是无状态的（除了 cooldown），重置时不需要做太多操作
}

// Execute 执行策略
func (s *ArbitrageStrategy) Execute(tick types.MarketTick, intent types.Intent, pos struct {
	YesShares  float64
	NoShares   float64
	Confidence float64
}) []oms.OrderDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. 数据完整性检查
	// 需要同时有 Yes 和 No 的 Ask 价格
	if tick.BestAsk <= 0 || tick.BestAskNo <= 0 {
		return nil
	}

	// 2. 冷却检查
	if time.Since(s.lastTradeTime) < s.cooldown {
		return nil
	}

	// 3. 计算套利空间
	// Cost = 买入 Yes 的价格 + 买入 No 的价格
	cost := tick.BestAsk + tick.BestAskNo
	
	// 理论价值 = 1.0
	// 预期利润 = 1.0 - cost
	spread := 1.0 - cost
	
	// 4. 判断是否执行
	if spread >= s.minProfitSpread {
		// 发现套利机会！
		s.lastTradeTime = time.Now()
		
		// 计算数量
		// Size = USDC / Price
		sizeYes := s.orderSizeUSDC / tick.BestAsk
		sizeNo := s.orderSizeUSDC / tick.BestAskNo
		
		reason := fmt.Sprintf("Arb_Opp: Cost=%.4f, Spread=%.4f", cost, spread)

		// 生成双向订单
		// 注意：这假设 OMS 会尽快执行这两个订单
		return []oms.OrderDecision{
			{
				Side:   types.SideYes,
				Price:  tick.BestAsk, // 直接吃单 (Taker)
				Size:   sizeYes,
				Reason: reason,
			},
			{
				Side:   types.SideNo,
				Price:  tick.BestAskNo, // 直接吃单 (Taker)
				Size:   sizeNo,
				Reason: reason,
			},
		}
	}

	return nil
}
