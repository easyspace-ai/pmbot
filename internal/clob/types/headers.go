package types

// L2HeaderArgs L2 认证头参数
type L2HeaderArgs struct {
	Method     string
	RequestPath string
	Body       *string
}

// L1PolyHeader L1 认证头（EIP712 签名验证）
type L1PolyHeader struct {
	PolyAddress  string `json:"POLY_ADDRESS"`
	PolySignature string `json:"POLY_SIGNATURE"`
	PolyTimestamp string `json:"POLY_TIMESTAMP"`
	PolyNonce     string `json:"POLY_NONCE"`
}

// L2PolyHeader L2 认证头（API 密钥验证）
type L2PolyHeader struct {
	PolyAddress   string `json:"POLY_ADDRESS"`
	PolySignature string `json:"POLY_SIGNATURE"`
	PolyTimestamp string `json:"POLY_TIMESTAMP"`
	PolyAPIKey    string `json:"POLY_API_KEY"`
	PolyPassphrase string `json:"POLY_PASSPHRASE"`
}

// L2WithBuilderHeader Builder API 密钥验证头
type L2WithBuilderHeader struct {
	L2PolyHeader
	PolyBuilderAPIKey    string `json:"POLY_BUILDER_API_KEY"`
	PolyBuilderTimestamp string `json:"POLY_BUILDER_TIMESTAMP"`
	PolyBuilderPassphrase string `json:"POLY_BUILDER_PASSPHRASE"`
	PolyBuilderSignature string `json:"POLY_BUILDER_SIGNATURE"`
}

