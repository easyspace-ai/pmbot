package client

// API 端点常量
const (
	// Server Time
	EndpointTime = "/time"

	// API Key endpoints
	EndpointCreateAPIKey      = "/auth/api-key"
	EndpointGetAPIKeys        = "/auth/api-keys"
	EndpointDeleteAPIKey      = "/auth/api-key"
	EndpointDeriveAPIKey      = "/auth/derive-api-key"
	EndpointClosedOnly        = "/auth/ban-status/closed-only"

	// Markets
	EndpointGetMarkets        = "/markets"
	EndpointGetMarket         = "/markets/"
	EndpointGetOrderBook      = "/book"
	EndpointGetOrderBooks     = "/books"
	EndpointGetMidpoint       = "/midpoint"
	EndpointGetMidpoints      = "/midpoints"
	EndpointGetPrice          = "/price"
	EndpointGetPrices         = "/prices"
	EndpointGetLastTradePrice = "/last-trade-price"

	// Order endpoints
	EndpointPostOrder         = "/order"
	EndpointPostOrders        = "/orders"
	EndpointCancelOrder       = "/order"
	EndpointCancelOrders      = "/orders"
	EndpointGetOrder          = "/data/order/"
	EndpointCancelAll         = "/cancel-all"
	EndpointCancelMarketOrders = "/cancel-market-orders"
	EndpointGetOpenOrders     = "/data/orders"
	EndpointGetTrades         = "/data/trades"
	EndpointGetOrderScoring   = "/data/order/scoring"
	EndpointGetOrdersScoring  = "/data/orders/scoring"

	// Balance
	EndpointGetBalanceAllowance = "/balance-allowance"
)

