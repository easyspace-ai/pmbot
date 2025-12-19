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

// OMS turns Intent into concrete order actions.
// It is intentionally conservative: prefer fewer open orders, strict idempotency,
// and safe cancel behavior under kill-switch.
type OMS struct {
	lastRisk types.RiskState

	orders map[string]*Order

	seq atomic.Uint64

	lastTick *types.MarketTick
}

func New() *OMS {
	return &OMS{orders: map[string]*Order{}}
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

	desiredSide, desiredPrice := desiredOrder(*t, in)
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
	size := sizeFromBudget(in.RiskDeltaMax, desiredPrice)
	if size <= 0 {
		return
	}

	cid := o.nextClientOrderID()
	req := PlaceOrderRequest{
		ClientOrderID: cid,
		MarketID:      in.MarketID,
		Side:          desiredSide,
		Price:         desiredPrice,
		Size:          size,
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

func sizeFromBudget(budget, price float64) float64 {
	if budget <= 0 {
		return 0
	}
	price = clampPrice(price)
	return budget / price
}
