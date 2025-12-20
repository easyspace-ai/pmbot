# CopyTrader 交易细节分析

## 核心可借鉴点

### 1. 动态滑点管理（Dynamic Slippage）

**实现位置**: `getMaxSlippage()` (96-116行)

```go
func getMaxSlippage(traderPrice float64) float64 {
    switch {
    case traderPrice < 0.10:
        return 2.00 // 200% - 低价token波动大，允许更大滑点
    case traderPrice < 0.20:
        return 0.80 // 80%
    case traderPrice < 0.30:
        return 0.50 // 50%
    case traderPrice < 0.40:
        return 0.30 // 30%
    default:
        return 0.20 // 20% - 高价token更稳定
    }
}
```

**关键洞察**：
- 低价token（<0.10）波动性大，允许200%滑点（可支付3倍价格）
- 高价token（>0.40）更稳定，只允许20%滑点
- **这是对市场微观结构的深刻理解**

**pmbot应用**：
- 在策略执行器中，根据token价格动态调整价格容忍度
- 在`calculateOrderPrice()`中考虑滑点限制

### 2. 订单簿深度分析（Order Book Depth Analysis）

**实现位置**: `executeBuy()` (659-693行)

```go
// 逐层分析订单簿，计算可承受的流动性
for i, ask := range book.Asks {
    if askPrice > maxAllowedPrice {
        break // 超过最大价格，停止
    }
    
    levelCost := askPrice * askSize
    if affordableUSDC+levelCost <= remainingUSDC {
        // 可以吃下整个level
        affordableSize += askSize
        affordableUSDC += levelCost
    } else {
        // 部分填充
        remainingForLevel := remainingUSDC - affordableUSDC
        partialSize := remainingForLevel / askPrice
        affordableSize += partialSize
        affordableUSDC += remainingForLevel
        break
    }
}
```

**关键洞察**：
- 不是简单使用best ask，而是分析整个订单簿深度
- 计算在价格限制内可以买入的总量
- 支持部分填充（partial fill）

**pmbot应用**：
- 在`strategy/executor.go`的`generateOrders()`中实现订单簿分析
- 根据订单簿深度决定订单大小，而不是简单使用预算除以价格

### 3. 智能重试机制（Intelligent Retry）

**实现位置**: `executeBuy()` (610-760行)

```go
const maxRetryDuration = 3 * time.Minute
const retryInterval = 1 * time.Second

for remainingUSDC >= minUSDC && time.Since(startTime) < maxRetryDuration {
    // 获取订单簿
    book, err := ct.clobClient.GetOrderBook(ctx, tokenID)
    
    // 分析可承受的流动性
    affordableSize := calculateAffordableLiquidity(book, maxAllowedPrice)
    
    if affordableSize < 0.01 {
        // 价格太高，等待并重试
        time.Sleep(retryInterval)
        continue
    }
    
    // 有可承受的流动性，下单
    resp, err := userClient.PlaceMarketOrder(...)
    
    // 如果还有剩余预算，继续尝试填充
    if remainingUSDC < minUSDC {
        break
    }
}
```

**关键洞察**：
- 不是一次性下单，而是持续监控订单簿
- 等待价格回到可承受范围（最多3分钟）
- 支持分批填充（partial fills）
- 每30秒记录一次状态，避免日志刷屏

**pmbot应用**：
- 在OMS中实现类似的重试逻辑
- 特别是在15分钟市场，时间压力下需要快速执行，但也要避免过度滑点

### 4. 最小订单大小处理（Minimum Order Size Handling）

**实现位置**: `executeBotBuy()` (1087-1114行)

```go
const polymarketMinOrder = 1.0

// 确保满足Polymarket的最小订单要求（$1）
if totalCost < polymarketMinOrder {
    log.Printf("calculated $%.4f, bumping to $%.2f minimum", totalCost, polymarketMinOrder)
    totalCost = polymarketMinOrder
    // 根据平均价格重新计算size
    if avgPrice > 0 {
        totalSize = totalCost / avgPrice
    }
}
```

**关键洞察**：
- Polymarket要求最小$1订单
- 如果计算出的订单小于$1，自动提升到$1
- 需要重新计算size以匹配新的cost

**pmbot应用**：
- 在`OMS.sizeFromBudget()`中确保满足最小订单要求
- 检查最小share数量（通常5 shares）和最小金额（$1）

### 5. 订单簿缓存优化（Order Book Caching）

**实现位置**: 多处使用`GetCachedOrderBook()`

```go
// 第一次尝试使用缓存（快速）
if attempt == 1 {
    book, err = ct.clobClient.GetCachedOrderBook(ctx, tokenID)
} else {
    // 重试时使用新鲜数据（准确）
    book, err = ct.clobClient.GetOrderBook(ctx, tokenID)
}
```

**关键洞察**：
- 首次尝试使用缓存订单簿（~50ms延迟）
- 重试时使用新鲜订单簿（更准确）
- 平衡速度和准确性

**pmbot应用**：
- 在Polymarket适配器中实现订单簿缓存
- 在快速决策时使用缓存，在关键下单前使用新鲜数据

### 6. 详细的时间追踪（Detailed Timing Tracking）

**实现位置**: `executeBotBuy()` (887-1186行)

```go
timing := map[string]interface{}{
    "1_settings_ms":        float64(time.Since(settingsStart).Microseconds()) / 1000,
    "2_calculation_ms":     float64(time.Since(calcStart).Microseconds()) / 1000,
    "3_token_cache_ms":     float64(time.Since(cacheStart).Microseconds()) / 1000,
    "4_get_orderbook_ms":  float64(time.Since(orderBookStart).Microseconds()) / 1000,
    "5_analysis_ms":        float64(time.Since(analysisStart).Microseconds()) / 1000,
    "6_fill_calc_ms":       float64(time.Since(fillStart).Microseconds()) / 1000,
    "7_place_order_ms":     float64(time.Since(orderStart).Microseconds()) / 1000,
    "8_position_update_ms": float64(time.Since(positionStart).Microseconds()) / 1000,
    "total_ms":             float64(time.Since(startTime).Microseconds()) / 1000,
    "latency_from_trade_ms": float64(time.Since(trade.Timestamp).Milliseconds()),
}
```

**关键洞察**：
- 追踪每个步骤的耗时
- 识别性能瓶颈（通常是API调用）
- 记录从原始交易到执行的延迟

**pmbot应用**：
- 在Engine中添加类似的timing追踪
- 帮助优化15分钟市场的执行速度

### 7. 市场关闭检测（Market Closed Detection）

**实现位置**: 多处检查404错误

```go
if strings.Contains(err.Error(), "404") || 
   strings.Contains(err.Error(), "No orderbook exists") {
    log.Printf("market closed/resolved, skipping")
    return ct.logCopyTrade(..., "skipped", "market closed/resolved", ...)
}
```

**关键洞察**：
- 市场可能已经关闭或结算
- 立即跳过，不要重试
- 避免浪费时间和资源

**pmbot应用**：
- 在OMS下单前检查市场状态
- 特别是在周期切换时，确保市场仍然开放

### 8. 仓位管理（Position Management）

**实现位置**: `executeSell()` (790-882行)

```go
// 优先从API获取实际仓位（source of truth）
actualPositions, err := ct.client.GetOpenPositions(ctx, ct.myAddress)
if err == nil {
    for _, pos := range actualPositions {
        if pos.Asset == tokenID && pos.Size.Float64() > 0 {
            sellSize = pos.Size.Float64()
            break
        }
    }
}

// Fallback到本地跟踪
if sellSize <= 0 {
    position, err := ct.store.GetMyPosition(...)
    sellSize = position.Size
}
```

**关键洞察**：
- API仓位是source of truth
- 本地跟踪作为fallback
- 卖出时优先使用实际仓位

**pmbot应用**：
- 在`position/truth.go`中定期从API同步仓位
- 确保仓位信息的准确性

### 9. 复杂卖出策略（Complex Sell Strategy）

**实现位置**: `executeBotSell()` (1189-1519行)

```go
// 1. 首先尝试在10%范围内市价卖出
if totalSold > 0.01 {
    resp, err := userClient.PlaceMarketOrder(...)
}

// 2. 如果没有可接受的bid，创建限价单
// - 20% @ copied price
// - 40% @ -3%
// - 40% @ -5%
order1Price := copiedPrice
order2Price := copiedPrice * 0.97
order3Price := copiedPrice * 0.95

// 3. 等待最多3分钟
for time.Since(waitStart) < maxWaitTime {
    // 检查订单状态
    status, err := userClient.GetOrderStatus(ctx, orderID)
}

// 4. 取消未填充的订单，市价卖出剩余
if len(unfilledOrderIDs) > 0 {
    userClient.CancelOrders(ctx, unfilledOrderIDs)
    // 市价卖出剩余
    userClient.PlaceMarketOrder(...)
}
```

**关键洞察**：
- 卖出策略比买入更复杂
- 先尝试市价，再尝试限价，最后强制市价
- 分层限价单策略（20% @ 0%, 40% @ -3%, 40% @ -5%）

**pmbot应用**：
- 在策略执行器中实现类似的卖出逻辑
- 特别是在15分钟市场接近结算时，需要快速退出

### 10. 调试日志（Debug Logging）

**实现位置**: `executeBotBuy()` (900-1186行)

```go
debugLog := map[string]interface{}{
    "action":    "BUY",
    "timestamp": startTime.Format(time.RFC3339),
    "settings": map[string]interface{}{
        "multiplier": multiplier,
        "minUSDC":    minUSDC,
    },
    "orderBook": map[string]interface{}{
        "asksCount": len(book.Asks),
        "topAsks":   askSnapshot,
    },
    "affordableAsks": affordableAsksLog,
    "fillCalculation": map[string]interface{}{
        "totalSize": totalSize,
        "totalCost": totalCost,
    },
    "order": map[string]interface{}{
        "type":     "market",
        "side":     "BUY",
        "size":     totalSize,
        "cost":     totalCost,
        "avgPrice": avgPrice,
    },
}
```

**关键洞察**：
- 记录完整的决策过程
- 便于事后分析和调试
- 结构化数据，易于查询和分析

**pmbot应用**：
- 在audit系统中记录类似的debug信息
- 帮助回放和分析交易决策

## 针对15分钟市场的特殊考虑

### 1. 时间压力下的执行速度

CopyTrader的重试机制（最多3分钟）对15分钟市场来说太长了。pmbot需要：
- 更短的超时时间（如30秒）
- 更快的决策循环
- 优先使用缓存订单簿

### 2. 价格容忍度

15分钟市场的价格变化更快，需要：
- 更宽松的滑点容忍度（特别是在早期）
- 接近结算时更严格的滑点控制
- 动态调整（结合Brain的TimeDecay）

### 3. 仓位退出策略

15分钟市场接近结算时，需要快速退出：
- 优先市价单
- 减少限价单等待时间
- 强制退出机制

## 实施建议

### 优先级1：立即实施

1. **最小订单大小处理**
   - 在`OMS.sizeFromBudget()`中确保满足$1和5 shares要求

2. **市场关闭检测**
   - 在下单前检查市场状态
   - 404错误立即跳过

3. **订单簿缓存**
   - 在Polymarket适配器中实现缓存
   - 首次使用缓存，重试使用新鲜数据

### 优先级2：短期实施

4. **动态滑点管理**
   - 在策略执行器中实现`getMaxSlippage()`
   - 根据token价格调整价格容忍度

5. **订单簿深度分析**
   - 在`generateOrders()`中分析订单簿深度
   - 计算可承受的流动性总量

6. **详细时间追踪**
   - 在Engine中添加timing追踪
   - 识别性能瓶颈

### 优先级3：长期优化

7. **智能重试机制**
   - 实现类似的重试逻辑（但超时时间更短）
   - 支持分批填充

8. **复杂卖出策略**
   - 实现分层限价单策略
   - 快速退出机制

9. **仓位管理优化**
   - 定期从API同步仓位
   - 提高仓位信息的准确性

## 总结

CopyTrader的实现展现了**生产级交易系统**的成熟度：

- ✅ 对市场微观结构的深刻理解（动态滑点）
- ✅ 完善的错误处理和边界情况处理
- ✅ 性能优化（订单簿缓存）
- ✅ 详细的监控和调试能力
- ✅ 复杂的交易策略（分层限价单）

pmbot可以借鉴这些经验，但需要针对15分钟市场的特点进行调整：
- 更快的执行速度
- 更短的重试超时
- 时间压力感知的价格容忍度

