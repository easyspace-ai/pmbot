package market

// CLOB API Response Structures

type ClobMarketResponse struct {
	ID            string   `json:"id"`            // Condition ID
	ConditionId   string   `json:"condition_id"`
	QuestionId    string   `json:"question_id"`
	MarketSlug    string   `json:"market_slug"`
	Question      string   `json:"question"`
	Description   string   `json:"description"`
	EndDate       string   `json:"end_date_iso"` // e.g. "2024-12-19T14:15:00Z"
	Tokens        []Token  `json:"tokens"`
	Active        bool     `json:"active"`
	Closed        bool     `json:"closed"`
}

type Token struct {
	TokenId string `json:"token_id"`
	Outcome string `json:"outcome"` // "Yes" or "No"
	Price   float64 `json:"price"`   // Last trade price or mid price
}

type OrderbookLevel struct {
	Price string `json:"price"` // API returns string
	Size  string `json:"size"`
}

type OrderbookResponse struct {
	Market string           `json:"market"`
	Bids   []OrderbookLevel `json:"bids"`
	Asks   []OrderbookLevel `json:"asks"`
	Hash   string           `json:"hash"`
}

// WebSocket Messages

type WsRequest struct {
	Action string      `json:"action"`
	Market string      `json:"market"` // Token ID (Asset ID)
	Assets []string    `json:"assets,omitempty"`
}

type WsResponse struct {
	Event     string             `json:"event"` // "book", "price_change", "order"
	Market    string             `json:"market"`
	Bids      []OrderbookLevel   `json:"bids"`
	Asks      []OrderbookLevel   `json:"asks"`
	Timestamp string             `json:"timestamp"`
	
	// Order Update Fields
	ID           string `json:"id"`
	clientOrderID string `json:"client_order_id"`
	Status       string `json:"status"` // OPEN, MATCHED, CANCELED
	Size         string `json:"size"`
	FilledSize   string `json:"size_matched"` // or filled_size
}
