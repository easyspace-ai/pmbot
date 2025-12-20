package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// BuildPolyHmacSignature 构建 Polymarket CLOB HMAC 签名
func BuildPolyHmacSignature(
	secret string,
	timestamp int64,
	method string,
	requestPath string,
	body *string,
) (string, error) {
	// 构建消息
	message := strconv.FormatInt(timestamp, 10) + method + requestPath
	if body != nil {
		message += *body
	}

	// 解码 base64 secret
	// 处理 base64url 格式（将 - 替换为 +，_ 替换为 /）
	sanitizedSecret := strings.ReplaceAll(secret, "-", "+")
	sanitizedSecret = strings.ReplaceAll(sanitizedSecret, "_", "/")
	
	// 移除非 base64 字符
	sanitizedSecret = strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || 
		   (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			return r
		}
		return -1
	}, sanitizedSecret)

	keyData, err := base64.StdEncoding.DecodeString(sanitizedSecret)
	if err != nil {
		return "", fmt.Errorf("解码 secret 失败: %w", err)
	}

	// 计算 HMAC-SHA256
	mac := hmac.New(sha256.New, keyData)
	mac.Write([]byte(message))
	signature := mac.Sum(nil)

	// 转换为 base64
	sigBase64 := base64.StdEncoding.EncodeToString(signature)

	// 转换为 URL 安全的 base64（但保留 = 后缀）
	// 将 '+' 替换为 '-'
	// 将 '/' 替换为 '_'
	sigURLSafe := strings.ReplaceAll(sigBase64, "+", "-")
	sigURLSafe = strings.ReplaceAll(sigURLSafe, "/", "_")

	return sigURLSafe, nil
}

