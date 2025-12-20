package strategy

import (
	"fmt"
	"sync"

	"polymarket-btc-bot/internal/oms"
)

var (
	// RegisteredStrategies 已注册的策略映射（使用 OMS 的 StrategyExecutor 接口）
	RegisteredStrategies = make(map[string]oms.StrategyExecutor)
	registeredMu         sync.RWMutex
)

// RegisterStrategy 注册策略
// 策略应该在 init() 函数中调用此函数进行注册
func RegisterStrategy(id string, strategy oms.StrategyExecutor) {
	registeredMu.Lock()
	defer registeredMu.Unlock()

	if _, exists := RegisteredStrategies[id]; exists {
		panic(fmt.Errorf("策略 %s 已注册", id))
	}

	RegisteredStrategies[id] = strategy
}

// GetStrategy 获取已注册的策略
func GetStrategy(id string) (oms.StrategyExecutor, error) {
	registeredMu.RLock()
	defer registeredMu.RUnlock()

	strategy, exists := RegisteredStrategies[id]
	if !exists {
		return nil, fmt.Errorf("策略 %s 未找到", id)
	}

	return strategy, nil
}

// ListStrategies 列出所有已注册的策略
func ListStrategies() []string {
	registeredMu.RLock()
	defer registeredMu.RUnlock()

	ids := make([]string, 0, len(RegisteredStrategies))
	for id := range RegisteredStrategies {
		ids = append(ids, id)
	}
	return ids
}

