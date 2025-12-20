package rtds

import (
	"encoding/json"
	"fmt"
)

// ParseCryptoPrice parses a crypto price update from message payload
func ParseCryptoPrice(payload json.RawMessage) (*CryptoPrice, error) {
	var price CryptoPrice
	if err := json.Unmarshal(payload, &price); err != nil {
		return nil, fmt.Errorf("failed to parse crypto price: %w", err)
	}
	return &price, nil
}

// ParseCryptoPriceHistorical parses historical crypto price data from message payload
func ParseCryptoPriceHistorical(payload json.RawMessage) (*CryptoPriceHistorical, error) {
	var historical CryptoPriceHistorical
	if err := json.Unmarshal(payload, &historical); err != nil {
		return nil, fmt.Errorf("failed to parse crypto price historical: %w", err)
	}
	return &historical, nil
}

// ParseComment parses a comment from message payload
func ParseComment(payload json.RawMessage) (*Comment, error) {
	var comment Comment
	if err := json.Unmarshal(payload, &comment); err != nil {
		return nil, fmt.Errorf("failed to parse comment: %w", err)
	}
	return &comment, nil
}

// ParseReaction parses a reaction from message payload
func ParseReaction(payload json.RawMessage) (*Reaction, error) {
	var reaction Reaction
	if err := json.Unmarshal(payload, &reaction); err != nil {
		return nil, fmt.Errorf("failed to parse reaction: %w", err)
	}
	return &reaction, nil
}

// ParseTrade parses a trade from message payload
func ParseTrade(payload json.RawMessage) (*Trade, error) {
	var trade Trade
	if err := json.Unmarshal(payload, &trade); err != nil {
		return nil, fmt.Errorf("failed to parse trade: %w", err)
	}
	return &trade, nil
}

// ParseOrder parses an order from message payload
func ParseOrder(payload json.RawMessage) (*Order, error) {
	var order Order
	if err := json.Unmarshal(payload, &order); err != nil {
		return nil, fmt.Errorf("failed to parse order: %w", err)
	}
	return &order, nil
}

// ParseAggOrderbook parses aggregated order book data from message payload
func ParseAggOrderbook(payload json.RawMessage) (*AggOrderbook, error) {
	var orderbook AggOrderbook
	if err := json.Unmarshal(payload, &orderbook); err != nil {
		return nil, fmt.Errorf("failed to parse orderbook: %w", err)
	}
	return &orderbook, nil
}

// ParseLastTradePrice parses last trade price from message payload
func ParseLastTradePrice(payload json.RawMessage) (*LastTradePrice, error) {
	var price LastTradePrice
	if err := json.Unmarshal(payload, &price); err != nil {
		return nil, fmt.Errorf("failed to parse last trade price: %w", err)
	}
	return &price, nil
}

// ParsePriceChanges parses price changes from message payload
func ParsePriceChanges(payload json.RawMessage) (*PriceChanges, error) {
	var changes PriceChanges
	if err := json.Unmarshal(payload, &changes); err != nil {
		return nil, fmt.Errorf("failed to parse price changes: %w", err)
	}
	return &changes, nil
}

// ParseClobMarketInfo parses CLOB market info from message payload
func ParseClobMarketInfo(payload json.RawMessage) (*ClobMarketInfo, error) {
	var info ClobMarketInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		return nil, fmt.Errorf("failed to parse CLOB market info: %w", err)
	}
	return &info, nil
}

// ParseRFQRequest parses an RFQ request from message payload
func ParseRFQRequest(payload json.RawMessage) (*RFQRequest, error) {
	var request RFQRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("failed to parse RFQ request: %w", err)
	}
	return &request, nil
}

// ParseRFQQuote parses an RFQ quote from message payload
func ParseRFQQuote(payload json.RawMessage) (*RFQQuote, error) {
	var quote RFQQuote
	if err := json.Unmarshal(payload, &quote); err != nil {
		return nil, fmt.Errorf("failed to parse RFQ quote: %w", err)
	}
	return &quote, nil
}

// CreateCryptoPriceHandler creates a handler function for crypto price updates
func CreateCryptoPriceHandler(callback func(*CryptoPrice) error) MessageHandler {
	return func(msg *Message) error {
		price, err := ParseCryptoPrice(msg.Payload)
		if err != nil {
			return err
		}
		return callback(price)
	}
}

// CreateCommentHandler creates a handler function for comment events
func CreateCommentHandler(callback func(*Comment) error) MessageHandler {
	return func(msg *Message) error {
		comment, err := ParseComment(msg.Payload)
		if err != nil {
			return err
		}
		return callback(comment)
	}
}

// CreateReactionHandler creates a handler function for reaction events
func CreateReactionHandler(callback func(*Reaction) error) MessageHandler {
	return func(msg *Message) error {
		reaction, err := ParseReaction(msg.Payload)
		if err != nil {
			return err
		}
		return callback(reaction)
	}
}

// CreateTradeHandler creates a handler function for trade events
func CreateTradeHandler(callback func(*Trade) error) MessageHandler {
	return func(msg *Message) error {
		trade, err := ParseTrade(msg.Payload)
		if err != nil {
			return err
		}
		return callback(trade)
	}
}

// CreateOrderHandler creates a handler function for order events
func CreateOrderHandler(callback func(*Order) error) MessageHandler {
	return func(msg *Message) error {
		order, err := ParseOrder(msg.Payload)
		if err != nil {
			return err
		}
		return callback(order)
	}
}

// CreateAggOrderbookHandler creates a handler function for aggregated orderbook updates
func CreateAggOrderbookHandler(callback func(*AggOrderbook) error) MessageHandler {
	return func(msg *Message) error {
		orderbook, err := ParseAggOrderbook(msg.Payload)
		if err != nil {
			return err
		}
		return callback(orderbook)
	}
}

// CreateLastTradePriceHandler creates a handler function for last trade price updates
func CreateLastTradePriceHandler(callback func(*LastTradePrice) error) MessageHandler {
	return func(msg *Message) error {
		price, err := ParseLastTradePrice(msg.Payload)
		if err != nil {
			return err
		}
		return callback(price)
	}
}

// CreatePriceChangesHandler creates a handler function for price changes
func CreatePriceChangesHandler(callback func(*PriceChanges) error) MessageHandler {
	return func(msg *Message) error {
		changes, err := ParsePriceChanges(msg.Payload)
		if err != nil {
			return err
		}
		return callback(changes)
	}
}

// CreateRFQRequestHandler creates a handler function for RFQ requests
func CreateRFQRequestHandler(callback func(*RFQRequest) error) MessageHandler {
	return func(msg *Message) error {
		request, err := ParseRFQRequest(msg.Payload)
		if err != nil {
			return err
		}
		return callback(request)
	}
}

// CreateRFQQuoteHandler creates a handler function for RFQ quotes
func CreateRFQQuoteHandler(callback func(*RFQQuote) error) MessageHandler {
	return func(msg *Message) error {
		quote, err := ParseRFQQuote(msg.Payload)
		if err != nil {
			return err
		}
		return callback(quote)
	}
}
