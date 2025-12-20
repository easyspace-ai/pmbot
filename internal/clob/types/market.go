package types

// MarketPrice 市场价格
type MarketPrice struct {
	Timestamp int64   `json:"t"` // 时间戳
	Price     float64 `json:"p"`  // 价格
}

// PriceHistoryInterval 价格历史间隔
type PriceHistoryInterval string

const (
	PriceHistoryIntervalMax    PriceHistoryInterval = "max"
	PriceHistoryIntervalOneWeek PriceHistoryInterval = "1w"
	PriceHistoryIntervalOneDay  PriceHistoryInterval = "1d"
	PriceHistoryIntervalSixHours PriceHistoryInterval = "6h"
	PriceHistoryIntervalOneHour   PriceHistoryInterval = "1h"
)

// PriceHistoryFilterParams 价格历史过滤参数
type PriceHistoryFilterParams struct {
	Market    *string
	StartTs   *int64
	EndTs     *int64
	Fidelity  *int
	Interval  *PriceHistoryInterval
}

// OrderBookSummary 订单簿摘要
type OrderBookSummary struct {
	Market       string         `json:"market"`
	AssetID      string         `json:"asset_id"`
	Timestamp    string         `json:"timestamp"`
	Bids         []OrderSummary `json:"bids"`
	Asks         []OrderSummary `json:"asks"`
	MinOrderSize string         `json:"min_order_size"`
	TickSize     string         `json:"tick_size"`
	NegRisk      bool           `json:"neg_risk"`
	Hash         string         `json:"hash"`
}

// OrderSummary 订单摘要
type OrderSummary struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// BookParams 订单簿查询参数
type BookParams struct {
	TokenID string
	Side    Side
}

// MarketTradeEvent 市场交易事件
type MarketTradeEvent struct {
	EventType string `json:"event_type"`
	Market    struct {
		ConditionID string `json:"condition_id"`
		AssetID     string `json:"asset_id"`
		Question    string `json:"question"`
		Icon        string `json:"icon"`
		Slug        string `json:"slug"`
	} `json:"market"`
	User struct {
		Address                  string `json:"address"`
		Username                 string `json:"username"`
		ProfilePicture           string `json:"profile_picture"`
		OptimizedProfilePicture  string `json:"optimized_profile_picture"`
		Pseudonym                string `json:"pseudonym"`
	} `json:"user"`
	Side          Side   `json:"side"`
	Size          string `json:"size"`
	FeeRateBps    string `json:"fee_rate_bps"`
	Price         string `json:"price"`
	Outcome       string `json:"outcome"`
	OutcomeIndex  int    `json:"outcome_index"`
	TransactionHash string `json:"transaction_hash"`
	Timestamp     string `json:"timestamp"`
}

// TradeParams 交易查询参数
type TradeParams struct {
	ID          *string
	MakerAddress *string
	Market      *string
	AssetID     *string
	Before      *string
	After       *string
}

// MakerOrder Maker 订单
type MakerOrder struct {
	OrderID     string `json:"order_id"`
	Owner       string `json:"owner"`
	MakerAddress string `json:"maker_address"`
	MatchedAmount string `json:"matched_amount"`
	Price       string `json:"price"`
	FeeRateBps  string `json:"fee_rate_bps"`
	AssetID     string `json:"asset_id"`
	Outcome     string `json:"outcome"`
	Side        Side   `json:"side"`
}

// Trade 交易
type Trade struct {
	ID              string      `json:"id"`
	TakerOrderID    string      `json:"taker_order_id"`
	Market          string      `json:"market"`
	AssetID         string      `json:"asset_id"`
	Side            Side        `json:"side"`
	Size            string      `json:"size"`
	FeeRateBps      string      `json:"fee_rate_bps"`
	Price           string      `json:"price"`
	Status          string      `json:"status"`
	MatchTime       string      `json:"match_time"`
	LastUpdate      string      `json:"last_update"`
	Outcome         string      `json:"outcome"`
	BucketIndex     int         `json:"bucket_index"`
	Owner           string      `json:"owner"`
	MakerAddress    string      `json:"maker_address"`
	MakerOrders     []MakerOrder `json:"maker_orders"`
	TransactionHash string      `json:"transaction_hash"`
	TraderSide      string      `json:"trader_side"` // "TAKER" | "MAKER"
}

// BalanceAllowanceParams 余额和授权查询参数
type BalanceAllowanceParams struct {
	AssetType     AssetType
	TokenID       *string
	SignatureType *SignatureType // 可选：签名类型（0=EOA, 1=Magic, 2=GnosisSafe）
}

// BalanceAllowanceResponse 余额和授权响应
type BalanceAllowanceResponse struct {
	Balance            string            `json:"balance"`
	Allowance          string            `json:"allowance"`
	CollateralBalance  string            `json:"collateralBalance,omitempty"`  // 代理钱包余额
	CollateralAllowance string            `json:"collateralAllowance,omitempty"` // 代理钱包授权
	Allowances         map[string]string `json:"allowances,omitempty"`         // 多个授权（代理钱包可能使用）
}

// Notification 通知
type Notification struct {
	Type    int         `json:"type"`
	Owner   string      `json:"owner"`
	Payload interface{} `json:"payload"`
}

// DropNotificationParams 删除通知参数
type DropNotificationParams struct {
	IDs []string
}

// PaginationPayload 分页载荷
type PaginationPayload struct {
	Limit     int         `json:"limit"`
	Count     int         `json:"count"`
	NextCursor string     `json:"next_cursor"`
	Data      interface{} `json:"data"`
}

// TickSizes 价格精度映射
type TickSizes map[string]TickSize

// NegRisk 负风险映射
type NegRisk map[string]bool

// FeeRates 手续费率映射
type FeeRates map[string]int

// RoundConfig 舍入配置
type RoundConfig struct {
	Price  float64
	Size   float64
	Amount float64
}

