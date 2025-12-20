package types

// UserOrder 用户订单（简化版）
type UserOrder struct {
	// TokenID 条件代币资产 ID
	TokenID string

	// Price 订单价格
	Price float64

	// Size 条件代币的数量
	Size float64

	// Side 订单方向
	Side Side

	// FeeRateBps 手续费率（基点），可选
	FeeRateBps *int

	// Nonce 用于链上取消订单的 nonce，可选
	Nonce *int

	// Expiration 订单过期时间戳（秒），可选
	Expiration *int64

	// Taker 订单接受者地址，零地址表示公开订单，可选
	Taker *string
}

// UserMarketOrder 用户市价订单
type UserMarketOrder struct {
	// TokenID 条件代币资产 ID
	TokenID string

	// Price 订单价格（可选，如果不存在则使用市价）
	Price *float64

	// Amount 数量
	// BUY 订单: 美元金额
	// SELL 订单: 份额数量
	Amount float64

	// Side 订单方向
	Side Side

	// FeeRateBps 手续费率（基点），可选
	FeeRateBps *int

	// Nonce 用于链上取消订单的 nonce，可选
	Nonce *int

	// Taker 订单接受者地址，零地址表示公开订单，可选
	Taker *string

	// OrderType 订单执行类型（仅支持 FOK 或 FAK）
	OrderType *OrderType
}

// SignedOrder 已签名的订单
type SignedOrder struct {
	Salt         int64  `json:"salt"`
	Maker        string `json:"maker"`
	Signer       string `json:"signer"`
	Taker        string `json:"taker"`
	TokenID      string `json:"tokenId"`
	MakerAmount  string `json:"makerAmount"`
	TakerAmount  string `json:"takerAmount"`
	Expiration   string `json:"expiration"`
	Nonce        string `json:"nonce"`
	FeeRateBps   string `json:"feeRateBps"`
	Side         Side   `json:"side"`
	SignatureType int   `json:"signatureType"`
	Signature    string `json:"signature"`
}

// NewOrder 新订单（包含订单类型）
type NewOrder struct {
	Order     SignedOrder `json:"order"`
	Owner     string      `json:"owner"`
	OrderType OrderType   `json:"orderType"`
	DeferExec bool        `json:"deferExec"`
}

// PostOrdersArgs 批量提交订单参数
type PostOrdersArgs struct {
	Order     SignedOrder
	OrderType OrderType
}

// OrderPayload 订单响应载荷
type OrderPayload struct {
	OrderID string `json:"orderID"`
}

// OrderResponse 订单响应
type OrderResponse struct {
	Success          bool     `json:"success"`
	ErrorMsg         string   `json:"errorMsg"`
	OrderID          string   `json:"orderID"`
	TransactionHashes []string `json:"transactionsHashes"`
	Status           string   `json:"status"`
	TakingAmount     string   `json:"takingAmount"`
	MakingAmount     string   `json:"makingAmount"`
}

// OpenOrder 开放订单
type OpenOrder struct {
	ID              string   `json:"id"`
	Status          string   `json:"status"`
	Owner           string   `json:"owner"`
	MakerAddress    string   `json:"maker_address"`
	Market          string   `json:"market"`
	AssetID         string   `json:"asset_id"`
	Side            string   `json:"side"`
	OriginalSize    string   `json:"original_size"`
	SizeMatched     string   `json:"size_matched"`
	Price           string   `json:"price"`
	AssociateTrades []string `json:"associate_trades"`
	Outcome         string   `json:"outcome"`
	CreatedAt       int64    `json:"created_at"`
	Expiration      string   `json:"expiration"`
	OrderType       string   `json:"order_type"`
}

// OpenOrdersResponse 开放订单列表响应
type OpenOrdersResponse []OpenOrder

// OpenOrdersAPIResponse API 返回的开放订单响应结构
type OpenOrdersAPIResponse struct {
	Data       []OpenOrder `json:"data"`
	NextCursor string      `json:"next_cursor"`
	Limit      int         `json:"limit"`
	Count      int         `json:"count"`
}

// OpenOrderParams 查询开放订单参数
type OpenOrderParams struct {
	ID       *string
	Market   *string
	AssetID  *string
}

// CreateOrderOptions 创建订单选项
type CreateOrderOptions struct {
	TickSize TickSize
	NegRisk  *bool
}

// OrderScoringParams 订单评分参数
type OrderScoringParams struct {
	OrderID string
}

// OrderScoring 订单评分
type OrderScoring struct {
	Scoring bool
}

// OrdersScoringParams 批量订单评分参数
type OrdersScoringParams struct {
	OrderIDs []string
}

// OrdersScoring 批量订单评分结果
type OrdersScoring map[string]bool

// OrderMarketCancelParams 取消市场订单参数
type OrderMarketCancelParams struct {
	Market  *string
	AssetID *string
}

