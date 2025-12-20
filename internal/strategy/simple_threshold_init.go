package strategy

import (
	"polymarket-btc-bot/internal/oms"
)

func init() {
	// 注册简单阈值策略
	RegisterStrategy("simple_threshold", NewSimpleThresholdStrategy())
	
	// 也注册为 "simple" 以便向后兼容
	RegisterStrategy("simple", NewSimpleThresholdStrategy())
}

// 确保 SimpleThresholdStrategy 实现了 StrategyExecutor 接口
var _ oms.StrategyExecutor = (*SimpleThresholdStrategy)(nil)

