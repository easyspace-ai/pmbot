package types

// Side 订单方向
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType 订单类型
type OrderType string

const (
	OrderTypeGTC OrderType = "GTC" // Good Till Cancel - 一直有效直到取消
	OrderTypeFOK OrderType = "FOK" // Fill or Kill - 全部成交或全部取消
	OrderTypeGTD OrderType = "GTD" // Good Till Date - 指定日期前有效
	OrderTypeFAK OrderType = "FAK" // Fill and Kill - 部分成交，剩余取消
)

// Chain 区块链网络
type Chain int

const (
	ChainPolygon Chain = 137
	ChainAmoy    Chain = 80002
)

// SignatureType 签名类型
type SignatureType int

const (
	SignatureTypeBrowser    SignatureType = 0 // EOA - Standard Ethereum wallet (MetaMask)
	SignatureTypeMagic      SignatureType = 1 // POLY_PROXY - Magic Link email/Google login
	SignatureTypeGnosisSafe SignatureType = 2 // GNOSIS_SAFE - Gnosis Safe multisig proxy wallet (most common)
)

// AssetType 资产类型
type AssetType string

const (
	AssetTypeCollateral  AssetType = "COLLATERAL"
	AssetTypeConditional AssetType = "CONDITIONAL"
)

// TickSize 价格精度
type TickSize string

const (
	TickSize01    TickSize = "0.1"
	TickSize001   TickSize = "0.01"
	TickSize0001  TickSize = "0.001"
	TickSize00001 TickSize = "0.0001"
)

// ApiKeyCreds API 密钥凭证
type ApiKeyCreds struct {
	Key        string
	Secret     string
	Passphrase string
}

// ApiKeyRaw 原始 API 密钥（API 返回格式）
type ApiKeyRaw struct {
	ApiKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

