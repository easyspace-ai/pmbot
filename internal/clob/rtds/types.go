package rtds

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RTDSNumber is a custom type that can parse numbers or strings from RTDS
type RTDSNumber string

// UnmarshalJSON implements the json.Unmarshaler interface
func (rn *RTDSNumber) UnmarshalJSON(b []byte) error {
	// Try to unmarshal as number first
	var num json.Number
	if err := json.Unmarshal(b, &num); err == nil {
		*rn = RTDSNumber(num.String())
		return nil
	}

	// If that fails, try as string
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*rn = RTDSNumber(s)
		return nil
	}

	// If both fail, return error
	return fmt.Errorf("cannot unmarshal %s into RTDSNumber", string(b))
}

// MarshalJSON implements the json.Marshaler interface
func (rn RTDSNumber) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(rn))
}

// String returns the string representation
func (rn RTDSNumber) String() string {
	return string(rn)
}

// RTDSTime is a custom time type that can parse multiple time formats from RTDS
type RTDSTime time.Time

// UnmarshalJSON implements the json.Unmarshaler interface
func (rt *RTDSTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "null" || s == "" {
		*rt = RTDSTime(time.Time{})
		return nil
	}

	// Try different time formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
	}

	var err error
	var t time.Time
	for _, format := range formats {
		t, err = time.Parse(format, s)
		if err == nil {
			*rt = RTDSTime(t)
			return nil
		}
	}

	return err
}

// MarshalJSON implements the json.Marshaler interface
func (rt RTDSTime) MarshalJSON() ([]byte, error) {
	t := time.Time(rt)
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Format(time.RFC3339))
}

// Time returns the underlying time.Time value
func (rt RTDSTime) Time() time.Time {
	return time.Time(rt)
}

// String returns the string representation
func (rt RTDSTime) String() string {
	return rt.Time().String()
}

// RTDSWebSocketURL is the WebSocket URL for the Polymarket Real-Time Data Socket
const RTDSWebSocketURL = "wss://ws-live-data.polymarket.com"

// Logger defines an interface for logging
type Logger interface {
	Printf(format string, v ...interface{})
}

// DefaultLogger is a simple logger implementation using fmt.Printf
type DefaultLogger struct{}

func (l *DefaultLogger) Printf(format string, v ...interface{}) {
	fmt.Printf(format, v...)
}

// Message represents a message received from the RTDS WebSocket
type Message struct {
	Topic        string          `json:"topic"`
	Type         string          `json:"type"`
	Timestamp    int64           `json:"timestamp"`
	Payload      json.RawMessage `json:"payload"`
	ConnectionID string          `json:"connection_id,omitempty"` // Optional connection ID from server
}

// SubscriptionAction represents the action for subscription management
type SubscriptionAction string

const (
	ActionSubscribe   SubscriptionAction = "subscribe"
	ActionUnsubscribe SubscriptionAction = "unsubscribe"
)

// ClobAuth represents CLOB authentication credentials
type ClobAuth struct {
	Key        string `json:"key"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// GammaAuth represents Gamma authentication credentials
type GammaAuth struct {
	Address string `json:"address"`
}

// Subscription represents a subscription configuration
type Subscription struct {
	Topic     string     `json:"topic"`
	Type      string     `json:"type"`
	Filters   string     `json:"filters,omitempty"`
	ClobAuth  *ClobAuth  `json:"clob_auth,omitempty"`
	GammaAuth *GammaAuth `json:"gamma_auth,omitempty"`
}

// SubscriptionRequest represents a subscription/unsubscription request
type SubscriptionRequest struct {
	Action        SubscriptionAction `json:"action"`
	Subscriptions []Subscription     `json:"subscriptions"`
}

// CryptoPrice represents a cryptocurrency price update
type CryptoPrice struct {
	Symbol            string  `json:"symbol"`
	Timestamp         int64   `json:"timestamp"`
	Value             float64 `json:"value"`
	FullAccuracyValue string  `json:"full_accuracy_value,omitempty"` // Optional field for high-precision value
}

// CryptoPriceHistorical represents historical price data sent on initial connection
type CryptoPriceHistorical struct {
	Symbol string       `json:"symbol"`
	Data   []PricePoint `json:"data"`
}

// PricePoint represents a single price data point
type PricePoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

// Comment represents a comment payload
type Comment struct {
	Body             string   `json:"body"`
	CreatedAt        RTDSTime `json:"createdAt"`
	ID               string   `json:"id"`
	ParentCommentID  *string  `json:"parentCommentID,omitempty"`
	ParentEntityID   int      `json:"parentEntityID"`
	ParentEntityType string   `json:"parentEntityType"`
	Profile          Profile  `json:"profile"`
	ReactionCount    int      `json:"reactionCount"`
	ReplyAddress     string   `json:"replyAddress"`
	ReportCount      int      `json:"reportCount"`
	UserAddress      string   `json:"userAddress"`
}

// Profile represents a user profile
type Profile struct {
	BaseAddress           string `json:"baseAddress"`
	DisplayUsernamePublic bool   `json:"displayUsernamePublic"`
	Name                  string `json:"name"`
	ProxyWallet           string `json:"proxyWallet"`
	Pseudonym             string `json:"pseudonym"`
}

// Reaction represents a reaction to a comment
type Reaction struct {
	ID           string   `json:"id"`
	CommentID    int      `json:"commentID"`
	ReactionType string   `json:"reactionType"`
	Icon         string   `json:"icon"`
	UserAddress  string   `json:"userAddress"`
	CreatedAt    RTDSTime `json:"createdAt"`
}

// Trade represents a trade in the activity stream
type Trade struct {
	ID              string     `json:"id"`
	Market          string     `json:"market"`
	AssetID         string     `json:"asset_id"`
	Price           RTDSNumber `json:"price"`
	Size            RTDSNumber `json:"size"`
	Side            string     `json:"side"`
	Timestamp       int64      `json:"timestamp"`
	MakerAddress    string     `json:"maker_address"`
	TakerAddress    string     `json:"taker_address"`
	Outcome         string     `json:"outcome"`
	OrderHash       string     `json:"order_hash"`
	TransactionHash string     `json:"transaction_hash"`
}

// Order represents an order in the CLOB user stream
type Order struct {
	AssetID      string `json:"asset_id"`
	CreatedAt    string `json:"created_at"`
	Expiration   string `json:"expiration"`
	ID           string `json:"id"`
	MakerAddress string `json:"maker_address"`
	Market       string `json:"market"`
	OrderType    string `json:"order_type"`
	OriginalSize string `json:"original_size"`
	Outcome      string `json:"outcome"`
	Owner        string `json:"owner"`
	Price        string `json:"price"`
	Side         string `json:"side"`
	SizeMatched  string `json:"size_matched"`
	Status       string `json:"status"`
	Type         string `json:"type"`
}

// AggOrderbook represents aggregated order book data
type AggOrderbook struct {
	Asks         []OrderLevel `json:"asks"`
	AssetID      string       `json:"asset_id"`
	Bids         []OrderLevel `json:"bids"`
	Hash         string       `json:"hash"`
	Market       string       `json:"market"`
	MinOrderSize string       `json:"min_order_size"`
	NegRisk      bool         `json:"neg_risk"`
	TickSize     string       `json:"tick_size"`
	Timestamp    string       `json:"timestamp"`
}

// OrderLevel represents a price level in the order book
type OrderLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// LastTradePrice represents the last trade price for a market
type LastTradePrice struct {
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	Price     string `json:"price"`
	Timestamp string `json:"timestamp"`
}

// PriceChanges represents price changes for multiple markets
type PriceChanges struct {
	Markets map[string]PriceChange `json:"markets"`
}

// PriceChange represents price change information for a market
type PriceChange struct {
	Market    string `json:"market"`
	AssetID   string `json:"asset_id"`
	Price     string `json:"price"`
	Timestamp string `json:"timestamp"`
}

// ClobMarketInfo represents static market configuration
type ClobMarketInfo struct {
	Market       string   `json:"market"`
	AssetIDs     []string `json:"asset_ids"`
	MinOrderSize string   `json:"min_order_size"`
	TickSize     string   `json:"tick_size"`
	NegRisk      bool     `json:"neg_risk"`
}

// RFQRequest represents a Request for Quote
type RFQRequest struct {
	RequestID    string  `json:"requestId"`
	ProxyAddress string  `json:"proxyAddress"`
	Market       string  `json:"market"`
	Token        string  `json:"token"`
	Complement   string  `json:"complement"`
	State        string  `json:"state"`
	Side         string  `json:"side"`
	SizeIn       float64 `json:"sizeIn"`
	SizeOut      float64 `json:"sizeOut"`
	Price        float64 `json:"price"`
	Expiry       int64   `json:"expiry"`
}

// RFQQuote represents a quote response to an RFQ
type RFQQuote struct {
	QuoteID      string  `json:"quoteId"`
	RequestID    string  `json:"requestId"`
	ProxyAddress string  `json:"proxyAddress"`
	Market       string  `json:"market"`
	Token        string  `json:"token"`
	Complement   string  `json:"complement"`
	State        string  `json:"state"`
	Side         string  `json:"side"`
	SizeIn       float64 `json:"sizeIn"`
	SizeOut      float64 `json:"sizeOut"`
	Price        float64 `json:"price"`
	Expiry       int64   `json:"expiry"`
}

// MessageHandler is a function type for handling messages
type MessageHandler func(message *Message) error

// TopicTypeHandler is a function type for handling specific topic/type combinations
type TopicTypeHandler func(payload json.RawMessage) error

