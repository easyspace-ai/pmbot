package market

import (
	"context"

	"polymarket-btc-bot/internal/bus"
	"polymarket-btc-bot/internal/oms"
)

// Adapter is the only module that talks to the exchange.
// It is responsible for emitting market/ack/fill events into the bus,
// and for executing order requests from OMS.
type Adapter interface {
	Start(ctx context.Context, bus *bus.Bus) error

	// Execution methods called by OMS.
	PlaceOrder(ctx context.Context, req oms.PlaceOrderRequest) (oms.PlaceOrderResult, error)
	CancelOrder(ctx context.Context, req oms.CancelOrderRequest) (oms.CancelOrderResult, error)

	// MergePositions attempts to merge YES+NO positions into collateral.
	// Returns txHash or error.
	MergePositions(ctx context.Context, amount float64) (string, error)
}
