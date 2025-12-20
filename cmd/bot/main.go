package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"polymarket-btc-bot/internal/audit"
	"polymarket-btc-bot/internal/brain"
	"polymarket-btc-bot/internal/engine"
	"polymarket-btc-bot/internal/market"
	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/position"
	"polymarket-btc-bot/internal/pkg/config"
	"polymarket-btc-bot/internal/pkg/logger"
	"polymarket-btc-bot/internal/risk"
	"polymarket-btc-bot/internal/strategy"
	signals "polymarket-btc-bot/internal/signal"
	
	// 导入策略包以触发 init() 函数注册策略
	_ "polymarket-btc-bot/internal/strategy"
	
	"github.com/sirupsen/logrus"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "", "配置文件路径（支持 .yaml, .yml, .json）")
	flag.Parse()

	// 初始化日志系统
	if err := logger.InitDefault(); err != nil {
		panic(err)
	}
	
	log := logger.Logger

	// 加载配置
	var cfg *config.Config
	var err error
	
	if *configPath != "" {
		config.SetConfigPath(*configPath)
		log.Infof("使用配置文件: %s", *configPath)
	} else {
		defaultConfigPath := "config.yaml"
		if _, err := os.Stat(defaultConfigPath); err == nil {
			config.SetConfigPath(defaultConfigPath)
			log.Infof("使用默认配置文件: %s", defaultConfigPath)
		} else {
			log.Warnf("未指定配置文件，且默认配置文件 %s 不存在，将使用环境变量和默认值", defaultConfigPath)
		}
	}
	
	cfg, err = config.Load()
	if err != nil {
		log.WithError(err).Error("加载配置失败")
		os.Exit(1)
	}

	// 使用配置重新初始化日志
	logConfig := logger.Config{
		Level:         cfg.LogLevel,
		OutputFile:    cfg.LogFile,
		MaxSize:       100,
		MaxBackups:    3,
		MaxAge:        7,
		Compress:      true,
		LogByCycle:    cfg.LogByCycle,
		CycleDuration: 15 * time.Minute,
	}
	if err := logger.Init(logConfig); err != nil {
		log.WithError(err).Error("重新初始化日志失败")
		os.Exit(1)
	}
	
	if cfg.LogByCycle {
		logger.StartLogRotationChecker(logConfig)
		log.Infof("日志按周期命名已启用，周期时长: %v", logConfig.CycleDuration)
	}

	// 设置 logrus 日志级别
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
		log.Warnf("无效的日志级别 %s，使用默认级别: info", cfg.LogLevel)
	}
	logrus.SetLevel(level)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 创建市场适配器（根据配置选择 WebSocket 或 HTTP 轮询）
	var adapter market.Adapter
	if cfg.Polymarket.UseWebSocket {
		// 使用 WebSocket 实时数据
		proxyURL := ""
		if cfg.Proxy != nil {
			proxyURL = fmt.Sprintf("http://%s:%d", cfg.Proxy.Host, cfg.Proxy.Port)
		} else if cfg.Polymarket.WebSocketProxy != "" {
			proxyURL = cfg.Polymarket.WebSocketProxy
		}
		
		wsAdapter := market.NewWebSocketAdapter(log, proxyURL)
		
		// 如果配置中提供了市场信息，直接设置
		if cfg.Polymarket.MarketID != "" && cfg.Polymarket.YesTokenID != "" && cfg.Polymarket.NoTokenID != "" {
			// 解析 endDate（需要从 market slug 或配置中获取）
			// 这里简化处理，实际应该从配置或 API 获取
			endDate := time.Now().Add(15 * time.Minute)
			wsAdapter.SetMarketInfo(
				cfg.Polymarket.MarketID,
				cfg.Polymarket.MarketSlugRegex, // 临时使用 regex 作为 slug
				cfg.Polymarket.YesTokenID,
				cfg.Polymarket.NoTokenID,
				endDate,
				"0.01", // 默认 minTickSize
				1.0,    // 默认 minOrderSize
				false,  // 默认 negRisk
			)
		}
		
		adapter = wsAdapter
		log.Info("使用 WebSocket 实时数据适配器")
	} else {
		// 使用 HTTP 轮询（原有方式）
		if os.Getenv("POLY_ADAPTER") == "polymarket" {
			adapter = market.NewPolymarketFromEnv(log)
		} else {
			// Default to dummy so the project runs out-of-box.
			adapter = market.NewDummy(15*time.Minute, log)
		}
		log.Info("使用 HTTP 轮询适配器")
	}

	// 创建 OMS（根据配置设置去重窗口）
	var omsInstance *oms.OMS
	if cfg.Execution != nil && cfg.Execution.DeduplicationEnabled {
		omsInstance = oms.NewWithDeduplicationWindow(cfg.Execution.DeduplicationWindowSecs)
		log.Infof("OMS 已启用去重，窗口: %d 秒", cfg.Execution.DeduplicationWindowSecs)
	} else {
		omsInstance = oms.New()
		log.Info("OMS 去重已禁用")
	}
	// 设置 OMS 的 logger
	omsInstance.SetLogger(log)
	
	// 加载并设置策略执行器（如果配置中启用了策略）
	if len(cfg.Strategies.EnabledStrategies) > 0 {
		log.Infof("启用的策略: %v", cfg.Strategies.EnabledStrategies)
		
		// 加载第一个启用的策略（PMBot 当前只支持单一策略）
		if len(cfg.Strategies.EnabledStrategies) > 0 {
			strategyID := cfg.Strategies.EnabledStrategies[0]
			
			// 如果是 simple_threshold 策略，使用配置创建
			if strategyID == "simple_threshold" || strategyID == "simple" {
				if cfg.Strategies.SimpleThreshold != nil {
					strategyExecutor := strategy.NewSimpleThresholdStrategyWithConfig(
						cfg.Strategies.SimpleThreshold.BuyThreshold,
						cfg.Strategies.SimpleThreshold.ProfitTarget,
						cfg.Strategies.SimpleThreshold.OrderSize,
					)
					omsInstance.SetStrategy(strategyExecutor)
					log.Infof("策略 %s 已加载并设置到 OMS (buy_threshold=%.2f, profit_target=%.2f, order_size=%.1f)",
						strategyID,
						cfg.Strategies.SimpleThreshold.BuyThreshold,
						cfg.Strategies.SimpleThreshold.ProfitTarget,
						cfg.Strategies.SimpleThreshold.OrderSize)
				} else {
					// 使用默认配置
					strategyExecutor, err := strategy.GetStrategy(strategyID)
					if err != nil {
						log.WithError(err).Warnf("策略 %s 未找到，将使用默认简单逻辑", strategyID)
					} else {
						omsInstance.SetStrategy(strategyExecutor)
						log.Infof("策略 %s 已加载并设置到 OMS（使用默认配置）", strategyID)
					}
				}
			} else {
				// 其他策略使用注册表
				strategyExecutor, err := strategy.GetStrategy(strategyID)
				if err != nil {
					log.WithError(err).Warnf("策略 %s 未找到，将使用默认简单逻辑", strategyID)
				} else {
					omsInstance.SetStrategy(strategyExecutor)
					log.Infof("策略 %s 已加载并设置到 OMS", strategyID)
				}
			}
		}
	} else {
		log.Info("未启用任何策略，OMS 将使用默认简单逻辑")
	}
	
	// 创建风险监控器（根据配置设置熔断器）
	var riskSupervisor *risk.Supervisor
	if cfg.CircuitBreaker != nil {
		cbConfig := &risk.CircuitBreakerConfig{
			Enabled:              cfg.CircuitBreaker.Enabled,
			MaxPositionPerMarket: cfg.CircuitBreaker.MaxPositionPerMarket,
			MaxTotalPosition:     cfg.CircuitBreaker.MaxTotalPosition,
			MaxDailyLoss:         cfg.CircuitBreaker.MaxDailyLoss,
			MaxConsecutiveErrors: cfg.CircuitBreaker.MaxConsecutiveErrors,
			CooldownSecs:         cfg.CircuitBreaker.CooldownSecs,
		}
		riskSupervisor = risk.NewWithConfig(cbConfig)
		log.Infof("风险监控器已启用熔断器: enabled=%v, max_daily_loss=%.2f, max_consecutive_errors=%d",
			cbConfig.Enabled, cbConfig.MaxDailyLoss, cbConfig.MaxConsecutiveErrors)
	} else {
		riskSupervisor = risk.New()
		log.Info("风险监控器使用默认配置（熔断器已启用）")
	}
	
	en := engine.New(
		log,
		engine.Config{BusBuffer: 4096},
		adapter,
		signals.New(),
		brain.New(),
		riskSupervisor,
		omsInstance,
		position.New(),
		audit.NopSink{},
	)

	if err := en.Run(ctx); err != nil {
		log.WithError(err).Error("engine stopped")
		os.Exit(1)
	}
}
