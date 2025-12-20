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

// BuildClobEip712Signature 构建 Polymarket CLOB EIP712 签名
func BuildClobEip712Signature(
	privateKey *ecdsa.PrivateKey,
	chainID types.Chain,
	timestamp int64,
	nonce int64,
) (string, error) {
	// 获取地址
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	// 构建 EIP712 域
	chainIDBig := big.NewInt(int64(chainID))
	domain := apitypes.TypedDataDomain{
		Name:    ClobDomainName,
		Version: ClobVersion,
		ChainId: math.NewHexOrDecimal256(chainIDBig.Int64()),
	}

	// 构建类型定义
	types := apitypes.Types{
		"EIP712Domain": {
			{Name: "name", Type: "string"},
			{Name: "version", Type: "string"},
			{Name: "chainId", Type: "uint256"},
		},
		"ClobAuth": {
			{Name: "address", Type: "address"},
			{Name: "timestamp", Type: "string"},
			{Name: "nonce", Type: "uint256"},
			{Name: "message", Type: "string"},
		},
	}

	// 构建消息值
	message := map[string]interface{}{
		"address":   address.Hex(),
		"timestamp": fmt.Sprintf("%d", timestamp),
		"nonce":     big.NewInt(nonce),
		"message":   MsgToSign,
	}

	// 构建 TypedData
	typedData := apitypes.TypedData{
		Types:       types,
		PrimaryType: "ClobAuth",
		Domain:      domain,
		Message:     message,
	}

	// 使用 go-ethereum 的标准方法计算哈希
	// 这会自动处理 EIP712 的完整流程
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return "", fmt.Errorf("计算域分隔符失败: %w", err)
	}

	// 计算消息哈希
	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return "", fmt.Errorf("计算消息哈希失败: %w", err)
	}

	// 构建最终哈希：\x19\x01 + domainSeparator + typedDataHash
	rawData := []byte("\x19\x01")
	rawData = append(rawData, domainSeparator...)
	rawData = append(rawData, typedDataHash...)
	hash := crypto.Keccak256Hash(rawData)

	// 签名（crypto.Sign 返回 65 字节：r(32) + s(32) + v(1)）
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return "", fmt.Errorf("签名失败: %w", err)
	}

	// crypto.Sign 返回的签名已经包含了恢复 ID（v），格式为 r + s + v
	// 直接转换为十六进制字符串（0x 前缀）
	return "0x" + common.Bytes2Hex(signature), nil
}

// GetAddressFromPrivateKey 从私钥获取地址
func GetAddressFromPrivateKey(privateKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privateKey.PublicKey)
}

// PrivateKeyFromHex 从十六进制字符串解析私钥
func PrivateKeyFromHex(hexKey string) (*ecdsa.PrivateKey, error) {
	return crypto.HexToECDSA(hexKey)
}

