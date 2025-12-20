# PMBot vs GoBet 全面源码级对比报告

## 一、项目概述对比

### 1.1 基本信息

| 项目 | PMBot | GoBet |
|------|-------|-------|
| **模块名** | `polymarket-btc-bot` | `github.com/betbot/gobet` |
| **Go版本** | 1.24.0 | 1.23.0 |
| **主要定位** | 单线程事件驱动交易引擎 | BBGO风格多策略交易框架 |
| **架构模式** | 单线程状态机 + 事件总线 | 领域驱动设计 + BBGO架构 |
| **设计哲学** | 保守、安全、确定性 | 灵活、可扩展、策略化 |

### 1.2 核心依赖对比

**PMBot:**
- `github.com/ethereum/go-ethereum v1.12.2`
- `github.com/gorilla/websocket v1.4.2`
- `github.com/sirupsen/logrus v1.9.3`
- `gopkg.in/natefinch/lumberjack.v2 v2.0.0`

**GoBet:**
- `github.com/ethereum/go-ethereum v1.13.5` (更新)
- `github.com/gorilla/websocket v1.5.1` (更新)
- `github.com/sirupsen/logrus v1.9.3`
- `gopkg.in/natefinch/lumberjack.v2 v2.2.1` (更新)
- `gopkg.in/yaml.v3 v3.0.1` (额外：YAML配置支持)

---

## 二、架构设计对比

### 2.1 核心架构模式

#### PMBot: 单线程事件驱动引擎

```
┌─────────────────────────────────────────┐
│         Market Adapter (并发)            │
│    (WebSocket/Polling 发布事件)          │
└──────────────┬──────────────────────────┘
                │ 事件总线 (Bus)
                ▼
┌─────────────────────────────────────────┐
│      Engine (单线程事件循环)              │
│  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │ Signals  │  │  Brain   │  │  Risk │ │
│  └──────────┘  └──────────┘  └────────┘ │
│  ┌──────────┐  ┌──────────┐            │
│  │   OMS    │  │ Position │            │
│  └──────────┘  └──────────┘            │
└─────────────────────────────────────────┘
```

**特点:**
- **单线程执行**: 所有状态变更在 `Engine.Run()` 的单一 goroutine 中处理
- **确定性**: 事件按顺序处理，无竞态条件
- **简单状态机**: 状态转换完全由事件驱动
- **保守设计**: 优先安全，避免并发复杂性

#### GoBet: BBGO风格多策略框架

```
┌─────────────────────────────────────────┐
│         MarketScheduler                  │
│    (自动市场切换，周期管理)               │
└──────────────┬──────────────────────────┘
                │
┌───────────────▼──────────────────────────┐
│      ExchangeSession (会话层)              │
│  ┌──────────────┐  ┌──────────────┐    │
│  │MarketDataStream│  │UserDataStream│    │
│  └──────────────┘  └──────────────┘    │
└──────────────┬──────────────────────────┘
                │ 回调订阅
┌───────────────▼──────────────────────────┐
│         Trader (策略管理器)                │
│  ┌────────┐  ┌────────┐  ┌────────┐     │
│  │ Grid   │  │Arbitrage│  │Threshold│   │
│  └────────┘  └────────┘  └────────┘     │
└──────────────┬──────────────────────────┘
                │
┌───────────────▼──────────────────────────┐
│      TradingService (交易服务)             │
│      MarketDataService (市场数据)          │
└──────────────────────────────────────────┘
```

**特点:**
- **多策略并行**: 支持同时运行多个策略
- **会话管理**: 每个市场周期一个会话，自动切换
- **回调驱动**: 策略通过订阅回调响应价格变化
- **灵活扩展**: 策略可插拔，易于添加新策略

### 2.2 事件处理机制对比

#### PMBot: 统一事件总线

```go
// internal/bus/bus.go
type Bus struct {
    seq atomic.Uint64
    ch  chan types.Event
    // ...
}

// 事件类型
const (
    EventMarketSnapshot EventType = "market.snapshot"
    EventMarketTick     EventType = "market.tick"
    EventOrderUpdate    EventType = "order.update"
    EventFill           EventType = "trade.fill"
    EventRisk           EventType = "risk"
)
```

**特点:**
- 所有事件通过单一通道 (`Bus.Chan()`) 进入引擎
- 事件按序列号 (`Seq`) 严格排序
- 阻塞式发布，确保事件不丢失
- 单线程消费，保证顺序处理

#### GoBet: 直接回调模式

```go
// pkg/bbgo/session.go
type ExchangeSession struct {
    MarketDataStream MarketDataStream
    UserDataStream   UserDataStream
    priceChangeHandlers []PriceChangeHandler
    // ...
}

// 策略订阅回调
type ExchangeSessionSubscriber interface {
    Subscribe(session *ExchangeSession)
}
```

**特点:**
- 价格变化直接触发策略回调
- 支持防抖机制 (`DirectModeDebounce`)
- 多个策略可同时订阅同一事件
- 回调在各自goroutine中执行（可能并发）

### 2.3 状态管理对比

#### PMBot: 集中式状态管理

```go
// internal/engine/engine.go
type Engine struct {
    lastTick      *types.MarketTick
    lastRisk      types.RiskState
    currentMarketID string
    transitioning  bool
    pendingSnapshot *types.MarketSnapshot
    // ...
}
```

**状态特点:**
- 所有状态集中在 `Engine` 结构体中
- 状态变更在单线程中原子完成
- 周期切换时显式重置 (`applyNewCycle`)
- 状态一致性由单线程保证

#### GoBet: 分布式状态管理

```go
// internal/strategies/grid/state.go
type GridState struct {
    GridLevels    []GridLevel
    OpenOrders    map[string]*domain.Order
    Positions     []Position
    // ...
}

// 每个策略维护自己的状态
```

**状态特点:**
- 每个策略维护独立状态
- 状态通过持久化服务保存/加载
- 状态变更可能并发，需要同步机制
- 支持状态恢复和回放

---

## 三、核心组件对比

### 3.1 市场适配器 (Market Adapter)

#### PMBot: 简单轮询适配器

```12:180:internal/market/polymarket.go
// Polymarket is a minimal real-data adapter:
// - Market discovery via CLOB /markets
// - Price via polling CLOB /book?token_id=...
//
// Order placement/cancel is scaffolded but may require auth/signing details.
type Polymarket struct {
	log *logrus.Logger

	http *http.Client

	baseURL string

	// Market selection
	marketSlugRegex *regexp.Regexp
	// If set, skip discovery and use these directly
	yesTokenID string
	noTokenID  string
	marketID   string
	marketSlug string

	// Polling
	pollInterval time.Duration

	endDate time.Time

	// Market microstructure
	minTickSize  string
	minOrderSize float64
	negRisk      bool

	// Trading (CLOB L1/L2 auth + EIP712 order signing)
	tradingEnabled bool
	chainID        int64
	privateKey     string
	address        string
	funder         string
	signatureType  uint8
	apiCreds       *apiCreds
	clobClient     *clobclient.Client // CLOB客户端

	// L2 polling cursors/dedup
	ordersCursor string
}
```

**特点:**
- 基于HTTP轮询获取价格 (`pollInterval`)
- 市场发现通过 `/markets` API
- 订单簿通过 `/book` API 获取
- 支持缓存订单簿数据（通过CLOB客户端）

#### GoBet: WebSocket实时适配器

```go
// internal/infrastructure/websocket/market_stream.go
type MarketStream struct {
    ws        *websocket.Conn
    handlers  []PriceChangeHandler
    // ...
}

// 实时价格更新
func (ms *MarketStream) OnPriceChange(handler PriceChangeHandler)
```

**特点:**
- 基于WebSocket实时推送价格
- 支持RTDS (Real-Time Data Stream) 协议
- 自动重连机制
- 低延迟价格更新

### 3.2 订单管理系统 (OMS)

#### PMBot: 简单OMS

```1:443:internal/oms/oms.go
package oms

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"polymarket-btc-bot/internal/types"
)

var (
	ErrRiskBlocked = errors.New("risk blocked")
)

type OrderState string

const (
	StateNew        OrderState = "NEW"
	StateAck        OrderState = "ACK"
	StatePartFilled OrderState = "PART_FILLED"
	StateFilled     OrderState = "FILLED"
	StateCanceling  OrderState = "CANCELING"
	StateCanceled   OrderState = "CANCELED"
	StateRejected   OrderState = "REJECTED"
	StateUnknown    OrderState = "UNKNOWN"
)

type Order struct {
	ClientOrderID   string
	ExchangeOrderID string

	MarketID string
	Side     types.Side
	Price    float64
	Size     float64

	FilledSize float64
	State      OrderState

	CreatedAt time.Time
	UpdatedAt time.Time
}

type PlaceOrderRequest struct {
	ClientOrderID string
	MarketID      string
	Side          types.Side
	Price         float64
	Size          float64
}

type PlaceOrderResult struct {
	ExchangeOrderID string
	Accepted        bool
	Reason          string
}

type CancelOrderRequest struct {
	ClientOrderID   string
	ExchangeOrderID string
	MarketID        string
}

type CancelOrderResult struct {
	Ok     bool
	Reason string
}

// OrderUpdate represents any server-side update/ack for an order.
type OrderUpdate struct {
	ClientOrderID   string
	ExchangeOrderID string
	State           OrderState
	FilledSize      float64
	Ts              time.Time
	Reason          string
}

// Fill represents a trade fill.
type Fill struct {
	ClientOrderID   string
	ExchangeOrderID string
	MarketID        string
	Side            types.Side
	Price           float64
	Size            float64
	Ts              time.Time
}

type Execution interface {
	PlaceOrder(ctx context.Context, req PlaceOrderRequest) (PlaceOrderResult, error)
	CancelOrder(ctx context.Context, req CancelOrderRequest) (CancelOrderResult, error)
}

// StrategyExecutor converts Intent to order decisions.
// This is the "strategy layer" that Brain's control system lacks.
type StrategyExecutor interface {
	Execute(tick types.MarketTick, intent types.Intent, pos struct {
		YesShares  float64
		NoShares   float64
		Confidence float64
	}) []OrderDecision
}

// OrderDecision represents a concrete order to place.
type OrderDecision struct {
	Side      types.Side
	Price     float64
	Size      float64
	Reason    string
	CancelAll bool // If true, cancel all existing orders first
}

// OMS turns Intent into concrete order actions.
// It is intentionally conservative: prefer fewer open orders, strict idempotency,
// and safe cancel behavior under kill-switch.
type OMS struct {
	lastRisk types.RiskState

	orders map[string]*Order

	seq atomic.Uint64

	lastTick *types.MarketTick

	// Strategy executor: converts Intent to order decisions
	// If nil, falls back to simple desiredOrder logic
	strategy StrategyExecutor
}

func New() *OMS {
	return &OMS{orders: map[string]*Order{}}
}

// SetStrategy sets the strategy executor for converting Intent to orders.
func (o *OMS) SetStrategy(s StrategyExecutor) {
	o.strategy = s
}

// Reset clears all per-cycle state. Call when a new 15m cycle starts.
// NOTE: Engine should attempt to cancel outstanding orders before calling Reset.
func (o *OMS) Reset() {
	o.lastRisk = types.RiskState{}
	o.orders = map[string]*Order{}
	o.lastTick = nil
}

// OrderCount returns the number of locally tracked orders (for diagnostics/tests).
func (o *OMS) OrderCount() int { return len(o.orders) }

// OpenOrderCount returns number of orders that are still potentially live on exchange.
func (o *OMS) OpenOrderCount() int {
	n := 0
	for _, ord := range o.orders {
		switch ord.State {
		case StateNew, StateAck, StatePartFilled, StateCanceling:
			n++
		}
	}
	return n
}

func (o *OMS) HasOpenOrders() bool { return o.OpenOrderCount() > 0 }

// CancelAll attempts to cancel all open orders (best-effort).
func (o *OMS) CancelAll(ctx context.Context, exec Execution) {
	o.cancelAllOpen(ctx, exec)
}

func (o *OMS) OnRisk(ctx context.Context, r types.RiskState, exec Execution) {
	o.lastRisk = r
	if r.KillSwitch {
		o.cancelAllOpen(ctx, exec)
	}
}

func (o *OMS) OnTick(t types.MarketTick) {
	o.lastTick = &t
}

func (o *OMS) OnIntent(ctx context.Context, in types.Intent, exec Execution) {
	if o.lastRisk.KillSwitch {
		return
	}
	if in.Freeze {
		// Freeze forbids adding risk. Cancel any outstanding open orders.
		o.cancelAllOpen(ctx, exec)
		return
	}

	t := o.lastTick
	if t == nil || t.MarketID == "" {
		return
	}
	if t.MarketID != in.MarketID {
		// Ignore intents for a different market than the last observed tick.
		return
	}

	// Use strategy executor if available, otherwise fall back to simple logic
	if o.strategy != nil {
		o.executeWithStrategy(ctx, *t, in, exec)
		return
	}
	o.executeSimple(ctx, *t, in, exec)
}

// executeWithStrategy uses the strategy executor to convert Intent to orders.
// Note: Position info needs to be passed from Engine to avoid import cycles.
// For now, this is a placeholder - Engine should call strategy executor directly.
func (o *OMS) executeWithStrategy(ctx context.Context, tick types.MarketTick, intent types.Intent, exec Execution) {
	// TODO: Engine should call strategy executor directly with position info
	// For now, fall back to simple execution
	o.executeSimple(ctx, tick, intent, exec)
}

// executeSimple uses the original simple logic (backward compatibility).
func (o *OMS) executeSimple(ctx context.Context, tick types.MarketTick, intent types.Intent, exec Execution) {
	desiredSide, desiredPrice := desiredOrder(tick, intent)
	if desiredSide == types.SideUnknown {
		return
	}

	// Keep at most one open order. If existing open order conflicts, cancel it first.
	open := o.firstOpen()
	if open != nil {
		if open.Side != desiredSide {
			o.cancelOrder(ctx, exec, open)
			return
		}
		// If price drift is large, cancel and replace.
		if math.Abs(open.Price-desiredPrice) >= 0.01 {
			o.cancelOrder(ctx, exec, open)
			return
		}
		// Otherwise keep the existing order.
		return
	}

	// Convert risk budget into size (shares). Treat RiskDeltaMax as spend budget.
	size := sizeFromBudget(intent.RiskDeltaMax, desiredPrice)
	if size <= 0 {
		return
	}

	dec := OrderDecision{
		Side:   desiredSide,
		Price:  desiredPrice,
		Size:   size,
		Reason: "simple_mode",
	}
	o.placeOrderFromDecision(ctx, exec, tick.MarketID, dec)
}

// placeOrderFromDecision places an order from a decision.
func (o *OMS) placeOrderFromDecision(ctx context.Context, exec Execution, marketID string, dec OrderDecision) {
	cid := o.nextClientOrderID()
	req := PlaceOrderRequest{
		ClientOrderID: cid,
		MarketID:      marketID,
		Side:          dec.Side,
		Price:         dec.Price,
		Size:          dec.Size,
	}

	ord := &Order{
		ClientOrderID: cid,
		MarketID:      req.MarketID,
		Side:          req.Side,
		Price:         req.Price,
		Size:          req.Size,
		State:         StateNew,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	o.orders[cid] = ord

	res, err := exec.PlaceOrder(ctx, req)
	if err != nil || !res.Accepted {
		ord.State = StateRejected
		ord.UpdatedAt = time.Now().UTC()
		return
	}
	if res.ExchangeOrderID != "" {
		ord.ExchangeOrderID = res.ExchangeOrderID
	}
	ord.State = StateAck
	ord.UpdatedAt = time.Now().UTC()
}

func (o *OMS) OnOrderUpdate(upd OrderUpdate) {
	// If update is keyed by exchange order id (common for Polymarket polling),
	// try to find the local order by exchange id.
	var ord *Order
	if upd.ClientOrderID != "" {
		ord = o.orders[upd.ClientOrderID]
	} else if upd.ExchangeOrderID != "" {
		for _, candidate := range o.orders {
			if candidate.ExchangeOrderID == upd.ExchangeOrderID {
				ord = candidate
				break
			}
		}
	}

	// If still unknown, ignore; we only track orders created by this bot.
	if ord == nil {
		return
	}

	if upd.ExchangeOrderID != "" {
		ord.ExchangeOrderID = upd.ExchangeOrderID
	}
	if !upd.Ts.IsZero() {
		ord.UpdatedAt = upd.Ts
	} else {
		ord.UpdatedAt = time.Now().UTC()
	}
	if upd.FilledSize >= 0 {
		ord.FilledSize = upd.FilledSize
	}
	if upd.State != "" {
		ord.State = upd.State
	}
}

func (o *OMS) firstOpen() *Order {
	for _, ord := range o.orders {
		switch ord.State {
		case StateNew, StateAck, StatePartFilled, StateCanceling:
			return ord
		}
	}
	return nil
}

func (o *OMS) cancelAllOpen(ctx context.Context, exec Execution) {
	for _, ord := range o.orders {
		switch ord.State {
		case StateNew, StateAck, StatePartFilled:
			o.cancelOrder(ctx, exec, ord)
		}
	}
}

func (o *OMS) cancelOrder(ctx context.Context, exec Execution, ord *Order) {
	if ord == nil {
		return
	}
	if ord.State == StateCanceled || ord.State == StateFilled || ord.State == StateRejected {
		return
	}
	ord.State = StateCanceling
	ord.UpdatedAt = time.Now().UTC()

	_, _ = exec.CancelOrder(ctx, CancelOrderRequest{
		ClientOrderID:   ord.ClientOrderID,
		ExchangeOrderID: ord.ExchangeOrderID,
		MarketID:        ord.MarketID,
	})
}

func (o *OMS) nextClientOrderID() string {
	n := o.seq.Add(1)
	return fmt.Sprintf("c-%d-%d", time.Now().UTC().UnixMilli(), n)
}

func desiredOrder(t types.MarketTick, in types.Intent) (types.Side, float64) {
	// Convert intent bias to side.
	if math.Abs(in.BiasYes) < 0.1 {
		return types.SideUnknown, 0
	}
	if in.BiasYes > 0 {
		// Buy YES near ask.
		price := t.BestAsk
		if price <= 0 {
			price = t.PYes
		}
		return types.SideYes, clampPrice(price)
	}
	// Buy NO near (1 - YES) ask approximation.
	priceNo := 1 - t.PYes
	return types.SideNo, clampPrice(priceNo)
}

func clampPrice(p float64) float64 {
	// Keep within reasonable bounds to avoid division explosions.
	if p < 0.01 {
		return 0.01
	}
	if p > 0.99 {
		return 0.99
	}
	return p
}

// sizeFromBudget converts risk budget to order size (shares).
// Ensures minimum order requirements:
// - Minimum $1 USDC (Polymarket requirement)
// - Minimum 5 shares (Polymarket requirement)
func sizeFromBudget(budget, price float64) float64 {
	if budget <= 0 {
		return 0
	}
	price = clampPrice(price)
	if price <= 0 {
		return 0
	}

	// Calculate initial size
	size := budget / price

	// Check minimum order size requirements
	const minOrderUSDC = 1.0      // Polymarket minimum $1 order
	const minOrderShares = 5.0    // Polymarket minimum 5 shares

	// Calculate actual cost
	actualCost := size * price

	// If cost is below minimum, bump up to minimum
	if actualCost < minOrderUSDC {
		actualCost = minOrderUSDC
		size = actualCost / price
	}

	// If size is below minimum shares, bump up to minimum
	if size < minOrderShares {
		size = minOrderShares
		actualCost = size * price
	}

	// If we had to bump up, ensure we still meet minimum cost
	if actualCost < minOrderUSDC {
		actualCost = minOrderUSDC
		size = actualCost / price
	}

	return size
}
```

**特点:**
- 最多保持一个开放订单（保守策略）
- 支持策略执行器接口（可扩展）
- 简单的订单状态机
- 风险控制集成（KillSwitch时取消所有订单）

#### GoBet: 完整交易服务

```go
// internal/services/trading_service.go (推测结构)
type TradingService struct {
    clobClient     *client.Client
    orderEngine    *OrderEngine
    orderStatusSync *OrderStatusSync
    // ...
}

// 功能:
// - 订单队列管理
// - 订单状态同步（轮询）
// - 仓位跟踪
// - 订单去重和幂等性
```

**特点:**
- 完整的订单生命周期管理
- 订单状态自动同步
- 支持订单队列和批量处理
- 更复杂的订单状态跟踪

### 3.3 决策系统对比

#### PMBot: Brain + Signal 双层决策

```1:113:internal/brain/brain.go
package brain

import (
	"math"
	"time"

	"polymarket-btc-bot/internal/signal"
	"polymarket-btc-bot/internal/types"
)

// Controller maps signals to Intent.
// This is deliberately simple initially; it will be tuned via playback.
type Controller struct {
	// Risk budget scaling.
	MaxRiskPerTick float64

	// Time decay controls how quickly the system de-risks near settlement.
	TimeHalfLife time.Duration

	// Freeze thresholds.
	FreezeP       float64
	FreezeEntropy float64
	FreezeMinTime time.Duration
}

func New() *Controller {
	return &Controller{
		MaxRiskPerTick: 5.0,
		TimeHalfLife:   5 * time.Minute,
		FreezeP:        0.98,
		FreezeEntropy:  0.08,
		FreezeMinTime:  30 * time.Second,
	}
}

func (c *Controller) Decide(t types.MarketTick, s *signal.Layer, r types.RiskState) types.Intent {
	p := clamp01(t.PYes)
	H := signal.Entropy(p)

	// Time pressure: reduce aggressiveness as settlement approaches.
	decay := 1.0
	if t.TimeRemaining > 0 && c.TimeHalfLife > 0 {
		ratio := float64(minDur(t.TimeRemaining, c.TimeHalfLife)) / float64(c.TimeHalfLife)
		decay = clamp01(math.Sqrt(ratio))
	}
	if decay < 0.2 {
		decay = 0.2
	}

	freeze := r.Freeze
	if !freeze {
		if (p >= c.FreezeP || p <= 1-c.FreezeP) && H <= c.FreezeEntropy && t.TimeRemaining > c.FreezeMinTime {
			freeze = true
		}
	}

	// Bias: mean-reversion to 0.5 during high entropy; follow consensus during low entropy.
	// This is a placeholder that matches the design intent (continuous control),
	// and will be replaced by the tuned dual-channel controller.
	consensus := 2 * (p - 0.5) // [-1,1]
	anti := -consensus
	w := clamp01(H / 0.6931471805599453) // normalize by max entropy at p=0.5
	bias := w*anti + (1-w)*consensus

	// Mode mix: more shock when acceleration is high or entropy collapses.
	acc := 0.0
	if s != nil {
		acc = s.Acceleration()
	}
	modeMix := clamp01(1 - (math.Abs(acc)*5 + (1 - w)))

	riskDelta := c.MaxRiskPerTick * decay
	if freeze {
		riskDelta = 0
	}

	return types.Intent{
		MarketID:     t.MarketID,
		BiasYes:      clamp11(bias),
		RiskDeltaMax: riskDelta,
		ModeMix:      modeMix,
		Freeze:       freeze,
		Ts:           time.Now().UTC(),
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func clamp11(x float64) float64 {
	if x < -1 {
		return -1
	}
	if x > 1 {
		return 1
	}
	return x
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
```

**特点:**
- **Signal Layer**: 计算价格加速度、熵等信号
- **Brain Controller**: 将信号转换为交易意图 (`Intent`)
- **Intent**: 抽象的交易意图（方向、风险预算、模式混合）
- **时间衰减**: 接近结算时降低风险

#### GoBet: 策略驱动决策

```go
// internal/strategies/grid/strategy.go
type GridStrategy struct {
    // 策略直接决定订单
    OnPriceChanged(ctx context.Context, price domain.Price)
    OnOrderFilled(ctx context.Context, order *domain.Order)
    // ...
}
```

**特点:**
- 策略直接决定订单（无中间抽象层）
- 每个策略独立决策逻辑
- 支持复杂的策略状态机
- 策略可访问完整市场数据

### 3.4 风险控制对比

#### PMBot: 集中式风险监督

```go
// internal/risk/risk.go
type Supervisor struct {
    // 风险检查逻辑
    Evaluate(tick types.MarketTick, pos *position.Truth) types.RiskState
}

// RiskState
type RiskState struct {
    KillSwitch bool  // 完全停止交易
    Freeze     bool  // 冻结新订单
    Reason     string
    Ts         time.Time
}
```

**特点:**
- 集中式风险评估
- KillSwitch机制（完全停止）
- Freeze机制（停止新订单）
- 风险状态影响所有组件

#### GoBet: 策略级风险控制

```go
// 每个策略实现自己的风险控制
type GridStrategy struct {
    MaxTotalCost    float64
    MaxUnhedgedLoss float64
    // ...
}
```

**特点:**
- 风险控制分散在各策略中
- 策略可独立配置风险参数
- 更灵活但可能不一致

---

## 四、策略系统对比

### 4.1 PMBot: 策略执行器模式

```go
// internal/oms/oms.go
type StrategyExecutor interface {
    Execute(tick types.MarketTick, intent types.Intent, pos struct {
        YesShares  float64
        NoShares   float64
        Confidence float64
    }) []OrderDecision
}
```

**特点:**
- 策略作为OMS的插件
- 接收 `Intent` 并转换为订单决策
- 单一策略执行器
- 策略与Brain解耦

### 4.2 GoBet: 多策略并行模式

```go
// internal/strategies/registry.go
type Strategy interface {
    ID() string
    Initialize() error
    Subscribe(session *ExchangeSession)
    Run(ctx context.Context, orderExecutor OrderExecutor, session *ExchangeSession) error
}

// 已实现策略:
// - GridStrategy (网格交易)
// - ArbitrageStrategy (套利)
// - ThresholdStrategy (阈值)
// - DataRecorderStrategy (数据记录)
```

**特点:**
- 多个策略可同时运行
- 策略通过订阅机制响应事件
- 策略独立生命周期管理
- 策略可访问完整会话数据

---

## 五、周期切换处理对比

### 5.1 PMBot: 显式周期切换

```250:329:internal/engine/engine.go
func (e *Engine) maybeCompleteTransition(ctx context.Context) {
	if !e.transitioning || e.pendingSnapshot == nil {
		return
	}

	// Conditions:
	// 1) No locally tracked open orders
	// 2) No fills observed for a short quiet window
	// 3) Timeout protection
	now := time.Now().UTC()
	if now.Sub(e.transitionStart) > 12*time.Second {
		// Can't safely close; stop trading.
		e.lastRisk = types.RiskState{KillSwitch: true, Freeze: true, Reason: "cycle_transition_timeout", Ts: now}
		e.transitioning = false
		return
	}

	openOrders := false
	if e.oms != nil {
		openOrders = e.oms.HasOpenOrders()
	}
	quiet := e.lastFillTs.IsZero() || now.Sub(e.lastFillTs) > 2*time.Second

	if openOrders {
		// Keep attempting cancels while waiting.
		if e.oms != nil && e.market != nil {
			e.oms.CancelAll(ctx, e.market)
		}
		return
	}
	if !quiet {
		return
	}

	// Close-book finished; apply new cycle.
	e.applyNewCycle(ctx, *e.pendingSnapshot)
	e.transitioning = false
	e.pendingSnapshot = nil
}

func (e *Engine) applyNewCycle(ctx context.Context, snap types.MarketSnapshot) {
	// Capture any residual local position and trip kill-switch if non-zero.
	var residualYes, residualNo, residualConf float64
	if e.pos != nil {
		y, n, _, conf, _ := e.pos.Snapshot()
		residualYes, residualNo, residualConf = y, n, conf
	}

	if e.oms != nil {
		e.oms.Reset()
	}
	if e.pos != nil {
		e.pos.Reset()
	}
	if e.signals != nil {
		e.signals.Reset()
	}
	e.lastTick = nil
	e.lastRisk = types.RiskState{Ts: time.Now().UTC()}
	e.currentMarketID = snap.MarketID

	// If we had any residual local position, stop trading for safety.
	if residualYes != 0 || residualNo != 0 || residualConf != 0 {
		e.lastRisk = types.RiskState{
			KillSwitch: true,
			Freeze:     true,
			Reason:     "residual_position_on_cycle_switch",
			Ts:         time.Now().UTC(),
		}
	}

	e.log.WithFields(map[string]interface{}{
		"market_id":   snap.MarketID,
		"slug":        snap.MarketSlug,
		"cycle_start": snap.CycleStart.Format(time.RFC3339),
		"end":         snap.EndDate.Format(time.RFC3339),
	}).Info("cycle switched")

	_ = ctx
}
```

**特点:**
- **过渡状态**: `transitioning` 标志防止周期切换时交易
- **安全关闭**: 等待所有订单取消和成交完成
- **超时保护**: 12秒超时触发KillSwitch
- **状态重置**: 显式重置所有周期相关状态
- **残差检测**: 检测并处理周期切换时的残差仓位

### 5.2 GoBet: 市场调度器自动切换

```go
// pkg/bbgo/market_scheduler.go
type MarketScheduler struct {
    // 自动检测周期结束
    // 创建新会话
    // 重新注册策略
    OnSessionSwitch(func(oldSession, newSession *ExchangeSession, newMarket *domain.Market))
}
```

**特点:**
- 自动检测周期结束
- 创建新会话并切换
- 策略自动重新订阅
- 会话状态自动管理

---

## 六、配置管理对比

### 6.1 PMBot: 环境变量配置

```go
// cmd/bot/main.go
var adapter market.Adapter
if os.Getenv("POLY_ADAPTER") == "polymarket" {
    adapter = market.NewPolymarketFromEnv(log)
} else {
    adapter = market.NewDummy(15*time.Minute, log)
}
```

**特点:**
- 纯环境变量配置
- 简单直接
- 无配置文件
- 硬编码默认值

### 6.2 GoBet: YAML配置文件

```go
// pkg/config/config.go
type Config struct {
    Wallet      WalletConfig
    Strategies  StrategyConfig
    Proxy       *ProxyConfig
    LogLevel    string
    LogFile     string
    // ...
}

// 支持:
// - YAML配置文件
// - 环境变量覆盖
// - 默认值
```

**特点:**
- YAML配置文件支持
- 环境变量覆盖
- 配置验证
- 策略特定配置

---

## 七、日志系统对比

### 7.1 PMBot: 简单日志

```go
// internal/pkg/logger/logger.go
logger.InitDefault()
logger.StartLogRotationChecker(logger.Config{
    Level:         "info",
    OutputFile:    "logs/combined.log",
    MaxSize:       100,
    MaxBackups:    3,
    MaxAge:        7,
    Compress:      true,
    LogByCycle:    true,
    CycleDuration: 15 * time.Minute,
})
```

**特点:**
- 基于logrus
- 日志轮转
- 按周期命名日志文件
- 简单配置

### 7.2 GoBet: 增强日志

```go
// pkg/logger/logger.go
// 支持:
// - 多级别日志
// - 结构化日志
// - 日志文件管理
// - 控制台和文件输出
```

**特点:**
- 更丰富的日志功能
- 结构化日志字段
- 日志级别动态调整
- 更好的日志组织

---

## 八、CLOB客户端对比

### 8.1 PMBot: 内嵌CLOB客户端

```go
// internal/clob/client/client.go
// 内嵌在项目中
// 基本功能:
// - 订单簿获取
// - 订单操作
// - 缓存支持
```

**特点:**
- 内嵌在项目中
- 基本功能完整
- 缓存支持
- 简单直接

### 8.2 GoBet: 独立CLOB SDK

```go
// clob/client/client.go
// 独立的SDK，可复用
// 完整功能:
// - 认证 (L1/L2)
// - 订单操作
// - 市场数据
// - Gamma API集成
// - RTDS支持
// - 示例代码
```

**特点:**
- 独立SDK，可复用
- 功能更完整
- 更好的文档
- 示例代码丰富

---

## 九、性能特性对比

### 9.1 PMBot

**优势:**
- ✅ **单线程执行**: 无锁竞争，性能可预测
- ✅ **事件顺序保证**: 严格的事件顺序
- ✅ **低内存占用**: 简单状态管理
- ✅ **确定性**: 相同输入产生相同输出

**劣势:**
- ❌ **单线程瓶颈**: 所有处理在单线程中
- ❌ **轮询延迟**: HTTP轮询可能有延迟
- ❌ **扩展性限制**: 难以并行处理

### 9.2 GoBet

**优势:**
- ✅ **并发处理**: 多策略并行执行
- ✅ **实时更新**: WebSocket低延迟
- ✅ **可扩展**: 易于添加新策略
- ✅ **灵活**: 策略可独立优化

**劣势:**
- ❌ **并发复杂性**: 需要同步机制
- ❌ **状态一致性**: 多策略状态可能不一致
- ❌ **资源消耗**: 多goroutine可能消耗更多资源

---

## 十、代码质量对比

### 10.1 PMBot

**代码特点:**
- ✅ **简洁**: 代码量少，逻辑清晰
- ✅ **专注**: 单一职责，功能明确
- ✅ **安全**: 保守设计，优先安全
- ✅ **测试友好**: 单线程易于测试

**代码组织:**
```
pmbot/
├── cmd/bot/          # 入口
├── internal/
│   ├── engine/       # 核心引擎
│   ├── bus/          # 事件总线
│   ├── brain/        # 决策层
│   ├── oms/          # 订单管理
│   ├── risk/         # 风险控制
│   ├── signal/       # 信号处理
│   ├── position/     # 仓位管理
│   └── market/       # 市场适配器
└── doc/              # 文档
```

### 10.2 GoBet

**代码特点:**
- ✅ **完整**: 功能完整，覆盖全面
- ✅ **模块化**: 清晰的模块划分
- ✅ **可扩展**: 易于扩展新功能
- ✅ **文档丰富**: 大量文档和注释

**代码组织:**
```
gobet/
├── cmd/
│   ├── bot/          # 主程序
│   └── prepare-data/ # 工具
├── internal/
│   ├── domain/       # 领域模型
│   ├── strategies/   # 策略
│   ├── services/     # 服务层
│   └── infrastructure/ # 基础设施
├── pkg/
│   ├── bbgo/         # BBGO框架
│   ├── config/       # 配置
│   └── logger/       # 日志
└── clob/             # CLOB SDK
```

---

## 十一、适用场景对比

### 11.1 PMBot 适用场景

✅ **适合:**
- 需要确定性行为的交易系统
- 对安全性要求极高的场景
- 简单的交易策略
- 需要精确控制执行顺序
- 研究和回测场景

❌ **不适合:**
- 需要多策略并行
- 需要实时低延迟
- 需要复杂策略逻辑
- 需要快速迭代策略

### 11.2 GoBet 适用场景

✅ **适合:**
- 多策略交易系统
- 需要实时响应
- 复杂策略逻辑
- 需要灵活扩展
- 生产环境部署

❌ **不适合:**
- 需要严格确定性
- 对并发安全要求极高
- 简单单一策略
- 资源受限环境

---

## 十二、总结与建议

### 12.1 核心差异总结

| 维度 | PMBot | GoBet |
|------|-------|-------|
| **架构** | 单线程事件驱动 | BBGO多策略框架 |
| **执行模型** | 顺序执行 | 并发执行 |
| **状态管理** | 集中式 | 分布式 |
| **策略系统** | 单一策略执行器 | 多策略并行 |
| **市场数据** | HTTP轮询 | WebSocket实时 |
| **配置** | 环境变量 | YAML配置 |
| **CLOB客户端** | 内嵌 | 独立SDK |
| **周期切换** | 显式状态机 | 自动调度器 |
| **风险控制** | 集中式 | 策略级 |
| **代码复杂度** | 简单 | 复杂 |

### 12.2 选择建议

**选择 PMBot 如果:**
1. 需要确定性和可预测性
2. 优先考虑安全性
3. 策略逻辑相对简单
4. 需要精确控制执行顺序
5. 用于研究和回测

**选择 GoBet 如果:**
1. 需要多策略并行运行
2. 需要实时低延迟响应
3. 策略逻辑复杂
4. 需要灵活扩展和迭代
5. 用于生产环境

### 12.3 融合建议

可以考虑将两者的优势结合:

1. **PMBot的确定性 + GoBet的灵活性**
   - 使用PMBot的单线程引擎作为核心
   - 集成GoBet的策略系统作为插件

2. **PMBot的安全机制 + GoBet的实时性**
   - 保留PMBot的风险控制
   - 使用GoBet的WebSocket实时数据

3. **PMBot的简单性 + GoBet的完整性**
   - 保持PMBot的简洁架构
   - 借鉴GoBet的完整功能

---

## 附录：关键代码片段对比

### A.1 事件处理

**PMBot:**
```go
// 单线程事件循环
for {
    select {
    case ev, ok := <-e.bus.Chan():
        e.handleEvent(ctx, ev)
    }
}
```

**GoBet:**
```go
// 回调订阅
session.OnPriceChange(func(price domain.Price) {
    strategy.OnPriceChanged(ctx, price)
})
```

### A.2 订单管理

**PMBot:**
```go
// 最多一个开放订单
open := o.firstOpen()
if open != nil && open.Side != desiredSide {
    o.cancelOrder(ctx, exec, open)
    return
}
```

**GoBet:**
```go
// 支持多个订单
for _, level := range gridLevels {
    tradingService.PlaceOrder(...)
}
```

### A.3 周期切换

**PMBot:**
```go
// 显式状态机
if e.transitioning {
    e.maybeCompleteTransition(ctx)
    return
}
```

**GoBet:**
```go
// 自动调度
marketScheduler.OnSessionSwitch(func(old, new *ExchangeSession, market *domain.Market) {
    trader.Subscribe(ctx, new)
})
```

---

**文档生成时间**: 2025-01-20
**对比版本**: PMBot (最新) vs GoBet (最新)

