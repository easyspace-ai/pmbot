package oms

import (
	"context"
	"errors"
	"time"

	"polymarket-btc-bot/internal/types"
)

var (
	ErrRiskBlocked = errors.New("risk blocked")
)

type OrderState string

const (
	StateNew          OrderState = "NEW"
	StateAck          OrderState = "ACK"
	StatePartFilled   OrderState = "PART_FILLED"
	StateFilled       OrderState = "FILLED"
	StateCanceling    OrderState = "CANCELING"
	StateCanceled     OrderState = "CANCELED"
	StateRejected     OrderState = "REJECTED"
	StateUnknown      OrderState = "UNKNOWN"
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
// For now it is a minimal skeleton; it will later implement slicing, replace, etc.
type OMS struct {
	lastRisk types.RiskState

	orders map[string]*Order
}

func New() *OMS {
	return &OMS{orders: map[string]*Order{}}
}

func (o *OMS) OnRisk(r types.RiskState) {
	o.lastRisk = r
}

func (o *OMS) OnIntent(ctx context.Context, in types.Intent, exec Execution) {
	if o.lastRisk.KillSwitch {
		return
	}
	if in.Freeze {
		return
	}
	// Placeholder: no-op until the execution policy is implemented.
	_ = ctx
	_ = exec
}

func (o *OMS) OnOrderUpdate(upd OrderUpdate) {
	ord := o.orders[upd.ClientOrderID]
	if ord == nil {
		ord = &Order{ClientOrderID: upd.ClientOrderID, ExchangeOrderID: upd.ExchangeOrderID, State: StateUnknown}
		o.orders[upd.ClientOrderID] = ord
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
