# Engine 集成说明

## 已集成的功能

### 1. 订单簿缓存优化 ✅

**位置**: `internal/market/polymarket.go::getBestBidAsk()`

**实现**:
- 优先使用缓存订单簿（~50ms延迟）
- 如果缓存不可用，获取最新数据（~200ms延迟）
- 自动更新缓存

**效果**:
- 减少API调用次数
- 提高响应速度
- 适合15分钟市场的快速变化

### 2. 时间追踪 ✅

**位置**: `internal/engine/engine.go::handleEvent()`

**实现**:
- 追踪每个MarketTick事件的处理时间
- 记录关键步骤：tick_received, signals, risk, brain, oms
- 当处理时间>10ms时记录debug日志

**效果**:
- 识别性能瓶颈
- 监控系统响应速度
- 优化15分钟市场的执行速度

**日志示例**:
```
level=DEBUG msg="tick processing timing" total_ms=15.2 tick_received_ms=0.1 signals_ms=0.5 risk_ms=1.2 brain_ms=2.3 oms_ms=11.1 latency_from_tick_ms=15.2
```

### 3. 市场关闭检测 ✅

**位置**: `internal/market/polymarket_trading.go::PlaceOrder()`

**实现**:
- 下单前快速检查市场状态
- 检测404和"No orderbook exists"错误
- 立即跳过已关闭的市场

**效果**:
- 避免浪费资源
- 快速响应市场关闭
- 特别是在周期切换时

### 4. 最小订单大小处理 ✅

**位置**: `internal/oms/oms.go::sizeFromBudget()`

**实现**:
- 确保满足$1和5 shares的最小要求
- 自动提升到最小值

**效果**:
- 避免订单被拒绝
- 符合Polymarket要求

## 使用方式

### 启用策略执行器（可选）

如果需要使用订单簿深度分析和动态滑点管理：

```go
import (
    "polymarket-btc-bot/internal/strategy"
    "polymarket-btc-bot/internal/oms"
)

// 创建策略执行器
executor := strategy.New()

// 设置到OMS
omsInstance := oms.New()
omsInstance.SetStrategy(executor)
```

### 查看时间追踪

时间追踪会自动记录，当处理时间>10ms时会输出debug日志。

可以通过设置日志级别为DEBUG来查看：
```go
log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
```

## 性能优化

### 订单簿缓存
- **首次访问**: ~200ms（API调用）
- **缓存命中**: ~50ms（内存访问）
- **TTL**: 500ms（适合15分钟市场）

### 时间追踪开销
- **开销**: <0.1ms per tick
- **仅在>10ms时记录**: 避免日志刷屏

## 下一步

1. **测试**: 在小资金环境下测试所有功能
2. **监控**: 观察时间追踪数据，识别瓶颈
3. **调优**: 根据实际数据调整缓存TTL等参数
4. **集成策略执行器**: 如果需要订单簿深度分析，启用策略执行器

## 注意事项

1. **订单簿缓存**: 缓存TTL为500ms，对于15分钟市场来说足够快
2. **时间追踪**: 仅在debug级别记录，生产环境建议使用info级别
3. **市场关闭检测**: 2秒超时，避免阻塞
4. **最小订单大小**: 自动处理，无需手动调整

