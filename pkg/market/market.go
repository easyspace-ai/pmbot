package market

import (
	"polymarket-bot/pkg/types"
)

type MarketProvider interface {
	Subscribe() <-chan types.MarketData
	SubmitOrder(order types.OrderRequest) error
}

// MockMarket is a simple mock for testing
type MockMarket struct {
	DataCh chan types.MarketData
}

func NewMockMarket() *MockMarket {
	return &MockMarket{
		DataCh: make(chan types.MarketData, 100),
	}
}

func (m *MockMarket) Subscribe() <-chan types.MarketData {
	return m.DataCh
}

func (m *MockMarket) SubmitOrder(order types.OrderRequest) error {
	// Log or mock execution
	return nil
}
