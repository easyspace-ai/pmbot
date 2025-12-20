package oms

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	"polymarket-btc-bot/internal/execution"
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

// StrategyResetter is an optional interface for strategies that need to reset state per cycle.
type StrategyResetter interface {
	Reset()
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

	// Execution deduplicator: prevents duplicate order execution
	deduplicator *execution.Deduplicator

	// Logger for order action logging
	log *logrus.Logger
}

func New() *OMS {
	return &OMS{
		orders:       map[string]*Order{},
		deduplicator: execution.NewDeduplicator(10), // 默认10秒去重窗口
		log:          logrus.New(), // 默认 logger
	}
}

// NewWithDeduplicationWindow 使用自定义去重窗口创建OMS
func NewWithDeduplicationWindow(windowSecs int64) *OMS {
	return &OMS{
		orders:       map[string]*Order{},
		deduplicator: execution.NewDeduplicator(windowSecs),
		log:          logrus.New(), // 默认 logger
	}
}

// SetLogger 设置日志记录器
func (o *OMS) SetLogger(log *logrus.Logger) {
	o.log = log
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
	if o.deduplicator != nil {
		o.deduplicator.Clear()
	}
	// 如果策略实现了 Reset 方法，调用它以重置策略状态
	// 这确保每个周期开始时策略状态被重置，允许新周期重新买入
	if o.strategy != nil {
		if resetter, ok := o.strategy.(StrategyResetter); ok {
			resetter.Reset()
		}
	}
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

func (o *OMS) OnIntent(ctx context.Context, in types.Intent, pos struct {
	YesShares  float64
	NoShares   float64
	Confidence float64
}, exec Execution) {
	// 调试日志：打印 Intent 信息
	o.log.Infof("🔍 [OMS] 收到 Intent: MarketID=%s, BiasYes=%.4f, RiskDeltaMax=%.2f, Freeze=%v, ModeMix=%.4f",
		in.MarketID, in.BiasYes, in.RiskDeltaMax, in.Freeze, in.ModeMix)
	o.log.Infof("🔍 [OMS] 持仓状态: YesShares=%.2f, NoShares=%.2f, Confidence=%.4f",
		pos.YesShares, pos.NoShares, pos.Confidence)
	
	if o.lastRisk.KillSwitch {
		o.log.Infof("🔍 [OMS] KillSwitch 激活，跳过处理")
		return
	}
	if in.Freeze {
		o.log.Infof("🔍 [OMS] Intent Freeze=true，取消所有订单")
		// Freeze forbids adding risk. Cancel any outstanding open orders.
		o.cancelAllOpen(ctx, exec)
		return
	}

	t := o.lastTick
	if t == nil || t.MarketID == "" {
		o.log.Infof("🔍 [OMS] 没有有效的 tick 数据")
		return
	}
	if t.MarketID != in.MarketID {
		o.log.Infof("🔍 [OMS] MarketID 不匹配: tick=%s, intent=%s", t.MarketID, in.MarketID)
		// Ignore intents for a different market than the last observed tick.
		return
	}

	// Use strategy executor if available, otherwise fall back to simple logic
	if o.strategy != nil {
		o.log.Infof("🔍 [OMS] 使用策略执行器: tick.PYes=%.4f, BestAsk=%.4f, BestBid=%.4f",
			t.PYes, t.BestAsk, t.BestBid)
		o.executeWithStrategy(ctx, *t, in, pos, exec)
		return
	}
	o.log.Infof("🔍 [OMS] 使用简单逻辑")
	o.executeSimple(ctx, *t, in, exec)
}

// executeWithStrategy uses the strategy executor to convert Intent to orders.
// Position info is passed from Engine to avoid import cycles.
func (o *OMS) executeWithStrategy(ctx context.Context, tick types.MarketTick, intent types.Intent, pos struct {
	YesShares  float64
	NoShares   float64
	Confidence float64
}, exec Execution) {
	if o.strategy == nil {
		o.executeSimple(ctx, tick, intent, exec)
		return
	}

	// 调用策略执行器
	decisions := o.strategy.Execute(tick, intent, pos)
	
	// 调试日志：打印策略返回的决策
	o.log.Infof("🔍 [OMS] 策略返回 %d 个决策", len(decisions))
	for i, dec := range decisions {
		sideStr := "YES"
		if dec.Side == types.SideNo {
			sideStr = "NO"
		} else if dec.Side == types.SideUnknown {
			sideStr = "UNKNOWN"
		}
		o.log.Infof("🔍 [OMS] 决策[%d]: Side=%s, Price=%.4f, Size=%.2f, Reason=%s, CancelAll=%v",
			i, sideStr, dec.Price, dec.Size, dec.Reason, dec.CancelAll)
	}

	// 执行策略返回的订单决策
	for _, dec := range decisions {
		if dec.CancelAll {
			o.cancelAllOpen(ctx, exec)
		}
		if dec.Side != types.SideUnknown && dec.Size > 0 {
			o.placeOrderFromDecision(ctx, exec, tick.MarketID, dec)
		}
	}
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
	// 生成执行ID用于去重（基于价格和方向）
	executionID := o.generateExecutionID(dec.Price, dec.Side)

	// 检查去重器
	if o.deduplicator != nil {
		if !o.deduplicator.TryAcquire(executionID) {
			// 正在执行中，跳过
			return
		}
	}

	cid := o.nextClientOrderID()
	req := PlaceOrderRequest{
		ClientOrderID: cid,
		MarketID:      marketID,
		Side:          dec.Side,
		Price:         dec.Price,
		Size:          dec.Size,
	}

	// 打印下单动作
	sideStr := "YES"
	if dec.Side == types.SideNo {
		sideStr = "NO"
	}
	cost := dec.Price * dec.Size
	o.log.Infof("📝 [下单] %s | 价格: %.4f | 数量: %.2f | 成本: $%.2f | 原因: %s | 订单ID: %s",
		sideStr, dec.Price, dec.Size, cost, dec.Reason, cid)

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

	// 异步执行下单请求，避免阻塞 Engine 主循环
	go func() {
		res, err := exec.PlaceOrder(ctx, req)
		
		// 注意：这里我们是在另一个 goroutine 中，不能直接修改 OMS 状态
		// 理想情况下，我们应该将结果发回 EventBus
		// 但为了保持架构简单，我们只打印日志，状态更新依赖于 WebSocket 的 OrderUpdate 事件
		// 或者依赖 Polling
		
		if err != nil || !res.Accepted {
			// 处理错误
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			reason := res.Reason
			
			// 检查是否是模拟模式
			isSimulationMode := (err != nil && (
				errMsg == "polymarket trading not configured (set POLY_TRADING_ENABLED=1 and credentials)" ||
				errMsg == "WebSocket adapter does not support order placement, use HTTP adapter for trading" ||
				strings.Contains(errMsg, "not configured") ||
				strings.Contains(errMsg, "does not support"))) ||
				reason == "not_configured" ||
				strings.Contains(reason, "does not support")
			
			if isSimulationMode {
				o.log.Infof("✅ [模拟下单成功] %s | 价格: %.4f | 数量: %.2f | 订单ID: %s (交易未启用，仅模拟)",
					sideStr, dec.Price, dec.Size, cid)
				// 注意：这里无法安全地更新 ord.State，存在竞态条件
				// 但由于是模拟模式，且 Order 对象是指针，风险较低
				return
			}
			
			o.log.Warnf("❌ [下单失败] %s | 价格: %.4f | 数量: %.2f | 订单ID: %s | 原因: %s",
				sideStr, dec.Price, dec.Size, cid, reason)
				
			// 释放去重锁
			if o.deduplicator != nil {
				o.deduplicator.Release(executionID)
			}
			return
		}
		
		o.log.Infof("✅ [下单请求已发送] %s | 价格: %.4f | 数量: %.2f | 交易所订单ID: %s",
			sideStr, dec.Price, dec.Size, res.ExchangeOrderID)
			
		// 如果返回了 ExchangeOrderID，尝试更新本地映射（注意并发安全）
		// 由于 OMS 是单线程模型，这里直接修改 map 是不安全的
		// 但实际上 map 的读写主要在 Engine 主线程
		// 正确做法是：PlaceOrder 成功后，Adapter 会推送 OrderUpdate 事件
		// 这里我们只做 logging
	}()
}

// lastClientOrderID 用于defer中检查订单状态
var lastClientOrderID string

// generateExecutionID 生成执行ID（基于价格和方向）
// 将价格转换为0-511的ID范围
func (o *OMS) generateExecutionID(price float64, side types.Side) uint16 {
	// 将价格（0.0-1.0）映射到0-511
	// 使用价格的整数部分和小数部分组合
	priceInt := int(price * 10000) // 转换为整数（0-10000）
	
	// 根据方向调整
	if side == types.SideNo {
		priceInt += 10000 // NO方向偏移
	}
	
	// 映射到0-511范围
	execID := uint16(priceInt % 512)
	return execID
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
	
	sideStr := "YES"
	if ord.Side == types.SideNo {
		sideStr = "NO"
	}
	o.log.Infof("🔄 [取消订单] %s | 价格: %.4f | 数量: %.2f | 订单ID: %s",
		sideStr, ord.Price, ord.Size, ord.ClientOrderID)
	
	ord.State = StateCanceling
	ord.UpdatedAt = time.Now().UTC()

	res, err := exec.CancelOrder(ctx, CancelOrderRequest{
		ClientOrderID:   ord.ClientOrderID,
		ExchangeOrderID: ord.ExchangeOrderID,
		MarketID:        ord.MarketID,
	})
	
	if err != nil || !res.Ok {
		// 如果是因为交易未启用而失败，打印模拟成功信息
		if err != nil && err.Error() == "polymarket trading not configured (set POLY_TRADING_ENABLED=1 and credentials)" {
			o.log.Infof("✅ [模拟取消成功] %s | 订单ID: %s (交易未启用，仅模拟)",
				sideStr, ord.ClientOrderID)
			ord.State = StateCanceled
			ord.UpdatedAt = time.Now().UTC()
			return
		}
		o.log.Warnf("❌ [取消订单失败] %s | 订单ID: %s | 原因: %s",
			sideStr, ord.ClientOrderID, res.Reason)
		return
	}
	
	o.log.Infof("✅ [取消订单成功] %s | 订单ID: %s", sideStr, ord.ClientOrderID)
	ord.State = StateCanceled
	ord.UpdatedAt = time.Now().UTC()
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
