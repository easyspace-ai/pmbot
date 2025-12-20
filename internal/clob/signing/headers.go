package signing

import (
	"crypto/ecdsa"
	"fmt"
	"strconv"
	"time"

	"polymarket-btc-bot/internal/clob/types"
)

// CreateL1Headers 创建 L1 认证头（EIP712 签名验证）
func CreateL1Headers(
	privateKey *ecdsa.PrivateKey,
	chainID types.Chain,
	nonce *int64,
	timestamp *int64,
) (*types.L1PolyHeader, error) {
	// 获取时间戳
	ts := time.Now().Unix()
	if timestamp != nil {
		ts = *timestamp
	}

	// 获取 nonce
	n := int64(0)
	if nonce != nil {
		n = *nonce
	}

	// 构建签名
	sig, err := BuildClobEip712Signature(privateKey, chainID, ts, n)
	if err != nil {
		return nil, fmt.Errorf("构建 EIP712 签名失败: %w", err)
	}

	// 获取地址
	address := GetAddressFromPrivateKey(privateKey)

	// 构建头
	headers := &types.L1PolyHeader{
		PolyAddress:  address.Hex(),
		PolySignature: sig,
		PolyTimestamp: strconv.FormatInt(ts, 10),
		PolyNonce:     strconv.FormatInt(n, 10),
	}

	return headers, nil
}

// CreateL2Headers 创建 L2 认证头（API 密钥验证）
func CreateL2Headers(
	privateKey *ecdsa.PrivateKey,
	creds *types.ApiKeyCreds,
	l2HeaderArgs *types.L2HeaderArgs,
	timestamp *int64,
) (*types.L2PolyHeader, error) {
	// 获取时间戳
	ts := time.Now().Unix()
	if timestamp != nil {
		ts = *timestamp
	}

	// 获取地址
	address := GetAddressFromPrivateKey(privateKey)

	// 构建 HMAC 签名
	sig, err := BuildPolyHmacSignature(
		creds.Secret,
		ts,
		l2HeaderArgs.Method,
		l2HeaderArgs.RequestPath,
		l2HeaderArgs.Body,
	)
	if err != nil {
		return nil, fmt.Errorf("构建 HMAC 签名失败: %w", err)
	}

	// 构建头
	headers := &types.L2PolyHeader{
		PolyAddress:   address.Hex(),
		PolySignature: sig,
		PolyTimestamp:  strconv.FormatInt(ts, 10),
		PolyAPIKey:    creds.Key,
		PolyPassphrase: creds.Passphrase,
	}

	return headers, nil
}

// InjectBuilderHeaders 注入 Builder 头
func InjectBuilderHeaders(
	l2Header *types.L2PolyHeader,
	builderAPIKey string,
	builderTimestamp string,
	builderPassphrase string,
	builderSignature string,
) *types.L2WithBuilderHeader {
	return &types.L2WithBuilderHeader{
		L2PolyHeader: *l2Header,
		PolyBuilderAPIKey:    builderAPIKey,
		PolyBuilderTimestamp: builderTimestamp,
		PolyBuilderPassphrase: builderPassphrase,
		PolyBuilderSignature: builderSignature,
	}
}

