package oms

import (
	"context"
	"testing"
	"time"

	"polymarket-btc-bot/internal/types"
)

func TestOMS_OnOrderUpdate_CreatesOrder(t *testing.T) {
	o := New()
	o.orders["c1"] = &Order{ClientOrderID: "c1", ExchangeOrderID: "e1", State: StateNew}
	o.OnOrderUpdate(OrderUpdate{ClientOrderID: "c1", ExchangeOrderID: "e1", State: StateAck, FilledSize: 0})
	if o.orders["c1"] == nil {
		t.Fatalf("order not created")
	}
	if o.orders["c1"].ExchangeOrderID != "e1" {
		t.Fatalf("unexpected exchange id")
	}
}

type fakeExec struct{}

func (fakeExec) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (PlaceOrderResult, error) {
	_ = ctx
	return PlaceOrderResult{ExchangeOrderID: "e-" + req.ClientOrderID, Accepted: true}, nil
}
func (fakeExec) CancelOrder(ctx context.Context, req CancelOrderRequest) (CancelOrderResult, error) {
	_ = ctx
	_ = req
	return CancelOrderResult{Ok: true}, nil
}

func TestOMS_KillSwitch_CancelsOpen(t *testing.T) {
	o := New()
	o.orders["c1"] = &Order{ClientOrderID: "c1", ExchangeOrderID: "e1", MarketID: "M", Side: types.SideYes, Price: 0.5, Size: 1, State: StateAck, CreatedAt: time.Now().UTC()}
	o.OnRisk(context.Background(), types.RiskState{KillSwitch: true}, fakeExec{})
	// CancelOrder 成功时会立即把本地状态标记为 CANCELED（不等待异步回报）。
	if o.orders["c1"].State != StateCanceled {
		t.Fatalf("expected canceled, got %s", o.orders["c1"].State)
	}
}
