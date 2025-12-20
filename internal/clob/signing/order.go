package signing

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"polymarket-btc-bot/internal/clob/types"
)

// BuildOrderSignature 构建订单的 EIP712 签名
func BuildOrderSignature(
	privateKey *ecdsa.PrivateKey,
	chainID types.Chain,
	exchangeAddress string,
	orderData *OrderData,
) (string, error) {

	// 构建 EIP712 域
	// 根据 lingebot 和官方实现，domain name 应该是 "Polymarket CTF Exchange"
	chainIDBig := big.NewInt(int64(chainID))
	domain := apitypes.TypedDataDomain{
		Name:              "Polymarket CTF Exchange",
		Version:           "1",
		ChainId:           math.NewHexOrDecimal256(chainIDBig.Int64()),
		VerifyingContract: exchangeAddress, // 直接使用字符串地址
	}

	// 构建类型定义
	typeDefs := apitypes.Types{
		"EIP712Domain": {
			{Name: "name", Type: "string"},
			{Name: "version", Type: "string"},
			{Name: "chainId", Type: "uint256"},
			{Name: "verifyingContract", Type: "address"},
		},
		"Order": {
			{Name: "salt", Type: "uint256"},
			{Name: "maker", Type: "address"},
			{Name: "signer", Type: "address"},
			{Name: "taker", Type: "address"},
			{Name: "tokenId", Type: "uint256"},
			{Name: "makerAmount", Type: "uint256"},
			{Name: "takerAmount", Type: "uint256"},
			{Name: "expiration", Type: "uint256"},
			{Name: "nonce", Type: "uint256"},
			{Name: "feeRateBps", Type: "uint256"},
			{Name: "side", Type: "uint8"},
			{Name: "signatureType", Type: "uint8"},
		},
	}

	// 转换 side 为 uint8
	// BUY = 0, SELL = 1（根据 @polymarket/order-utils）
	var sideUint8 uint8 = 1 // 默认 SELL
	if orderData.Side == types.SideBuy {
		sideUint8 = 0
	}

	// 构建消息值
	// 根据 lingebot 实现，地址使用字符串格式（.Hex()），side 和 signatureType 使用 big.Int
	message := map[string]interface{}{
		"salt":         big.NewInt(orderData.Salt),
		"maker":        common.HexToAddress(orderData.Maker).Hex(), // 使用字符串格式
		"signer":       common.HexToAddress(orderData.Signer).Hex(), // 使用字符串格式
		"taker":        common.HexToAddress(orderData.Taker).Hex(), // 使用字符串格式
		"tokenId":      orderData.TokenID,
		"makerAmount":  orderData.MakerAmount,
		"takerAmount":  orderData.TakerAmount,
		"expiration":   orderData.Expiration,
		"nonce":        orderData.Nonce,
		"feeRateBps":   orderData.FeeRateBps,
		"side":         big.NewInt(int64(sideUint8)), // 使用 big.Int
		"signatureType": big.NewInt(int64(orderData.SignatureType)), // 使用 big.Int
	}

	// 构建 TypedData
	typedData := apitypes.TypedData{
		Types:       typeDefs,
		PrimaryType: "Order",
		Domain:      domain,
		Message:     message,
	}

	// 使用 go-ethereum 的 TypedDataAndHash 方法计算哈希
	// 这个方法会自动处理 domain 和 message 的哈希计算
	// 返回的 hash 已经是 []byte 类型，不需要调用 .Bytes()
	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return "", fmt.Errorf("计算 EIP712 哈希失败: %w", err)
	}

	// 签名（hash 已经是 []byte，直接使用）
	signature, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return "", fmt.Errorf("签名失败: %w", err)
	}

	// 返回签名（0x 前缀）
	return "0x" + common.Bytes2Hex(signature), nil
}

// OrderData 订单数据（用于签名）
type OrderData struct {
	Salt         int64
	Maker        string
	Signer       string
	Taker        string
	TokenID      *big.Int
	MakerAmount  *big.Int
	TakerAmount  *big.Int
	Expiration   *big.Int
	Nonce        *big.Int
	FeeRateBps   *big.Int
	Side         types.Side
	SignatureType types.SignatureType
}

