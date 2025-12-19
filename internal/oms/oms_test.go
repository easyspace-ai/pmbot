package oms

import "testing"

func TestOMS_OnOrderUpdate_CreatesOrder(t *testing.T) {
	o := New()
	o.OnOrderUpdate(OrderUpdate{ClientOrderID: "c1", ExchangeOrderID: "e1", State: StateAck, FilledSize: 0})
	if o.orders["c1"] == nil {
		t.Fatalf("order not created")
	}
	if o.orders["c1"].ExchangeOrderID != "e1" {
		t.Fatalf("unexpected exchange id")
	}
}
