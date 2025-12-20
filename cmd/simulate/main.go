package main

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"polymarket-btc-bot/internal/audit"
	"polymarket-btc-bot/internal/brain"
	"polymarket-btc-bot/internal/engine"
	"polymarket-btc-bot/internal/market"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/risk"
	"polymarket-btc-bot/internal/signal"
	"polymarket-btc-bot/internal/strategy"
)

// 这是一个“模拟跑一轮”的最小可运行示例：
// - 使用 Dummy 市场适配器生成 tick（不接真实交易所）
// - Engine 单线程消费事件并驱动 Brain/Risk/OMS/Strategy
// - OMS 会通过 Dummy 适配器“下单/撤单”（只打印，不会产生真实成交）
//
// 运行：
//   go run ./cmd/simulate
func main() {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339Nano,
	})

	// 模拟运行 12 秒（避免长时间挂起）
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	// Dummy: 每 250ms 推送一次 tick，持续 60s（但我们的 ctx 会更早结束）
	adapter := market.NewDummy(60*time.Second, log)

	// 核心组件
	sig := signal.New()
	br := brain.New()
	rk := risk.New()
	o := oms.New()
	o.SetLogger(log)
	pos := position.New()

	// 选择一个“可解释”的策略：简单阈值（触发一次买入并挂止盈单）
	// 你也可以注释掉这行，让 OMS 走 executeSimple（由 Brain 的 BiasYes 驱动）
	// o.SetStrategy(strategy.NewSimpleThresholdStrategyWithConfig(0.60, 0.03, 10))
	
	// 使用 ArbitrageStrategy (Volume Bot)
	o.SetStrategy(strategy.NewArbitrageStrategy())

	eng := engine.New(
		log,
		engine.Config{BusBuffer: 2048},
		adapter,
		sig,
		br,
		rk,
		o,
		pos,
		audit.NopSink{},
	)

	err := eng.Run(ctx)
	log.WithField("err", err).Info("engine stopped")

	yes, no, cash, conf, updatedAt := pos.Snapshot()
	log.WithFields(map[string]any{
		"yes_shares":  yes,
		"no_shares":   no,
		"cash":        cash,
		"confidence":  conf,
		"updated_at":  updatedAt.Format(time.RFC3339Nano),
		"orders":      o.OrderCount(),
		"open_orders": o.OpenOrderCount(),
	}).Info("final snapshot")
}

