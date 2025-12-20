package market

import (
	"crypto/ecdsa"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func parseInt64Default(s string, def int64) int64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return v
}

func fmtTickSize(x float64) string {
	// Expected by Polymarket tick size config: "0.1", "0.01", "0.001", "0.0001"
	// strconv with -1 keeps minimal decimals (no trailing zeros).
	return strconv.FormatFloat(x, 'f', -1, 64)
}

// parsePrivateKeyHelper 解析私钥辅助函数
func parsePrivateKeyHelper(hexKey string) (*ecdsa.PrivateKey, common.Address, error) {
	k := strings.TrimSpace(hexKey)
	k = strings.TrimPrefix(k, "0x")
	priv, err := crypto.HexToECDSA(k)
	if err != nil {
		return nil, common.Address{}, err
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	return priv, addr, nil
}

// truncHelper 辅助函数
func truncHelper(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// 以下函数从gobet的order_utils.go集成，用于订单处理

// CalculateOptimalFill 根据订单簿计算最优成交价格和数量（用于市价单）
// 这个函数已经在clob/client/order_utils.go中实现，这里保留作为参考
// 实际使用时应该调用 clobclient.CalculateOptimalFill

// RoundToTickSize 将价格舍入到tick size
// 这个函数已经在clob/client/order_utils.go中实现，这里保留作为参考
// 实际使用时应该调用 clobclient.RoundToTickSize

// ValidateFOKPrecision 验证FOK/FAK订单的精度要求
// 这个函数已经在clob/client/order_utils.go中实现，这里保留作为参考
// 实际使用时应该调用 clobclient.ValidateFOKPrecision
