package market

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// EIP-712 Domain and Types

const (
	EIP712DomainName    = "Polymarket CTF Exchange"
	EIP712DomainVersion = "1"
)

type OrderStruct struct {
	Salt        *big.Int
	Maker       common.Address
	Signer      common.Address
	Taker       common.Address
	TokenId     *big.Int
	MakerAmount *big.Int
	TakerAmount *big.Int
	Expiration  *big.Int
	Nonce       *big.Int
	FeeRate     *big.Int
	Side        uint8 // 0 = Buy, 1 = Sell
	SideType    uint8 // 0 = Buy, 1 = Sell (Used for internal logic, mapped to Side)
}

// Signer handles EIP-712 signing and API Key signing
type Signer struct {
	PrivateKey    string
	ChainID       int64
	ExchangeAddr  common.Address
	ApiSecret     string
	ApiPassphrase string
	ApiKey        string
}

func NewSigner(pk, apiKey, apiSecret, apiPassphrase string, chainId int64, exchangeAddr string) *Signer {
	return &Signer{
		PrivateKey:    pk,
		ChainID:       chainId,
		ExchangeAddr:  common.HexToAddress(exchangeAddr),
		ApiKey:        apiKey,
		ApiSecret:     apiSecret,
		ApiPassphrase: apiPassphrase,
	}
}

// SignOrder generates the EIP-712 signature for an order
func (s *Signer) SignOrder(order OrderStruct) (string, error) {
	// Construct EIP-712 Data
	domain := apitypes.TypedDataDomain{
		Name:              EIP712DomainName,
		Version:           EIP712DomainVersion,
		ChainId:           math.NewHexOrDecimal256(s.ChainID),
		VerifyingContract: s.ExchangeAddr.Hex(),
	}

	types := apitypes.Types{
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
			{Name: "feeRate", Type: "uint256"},
			{Name: "side", Type: "uint8"},
		},
	}

	message := map[string]interface{}{
		"salt":        math.NewHexOrDecimal256(order.Salt.Int64()),
		"maker":       order.Maker.Hex(),
		"signer":      order.Signer.Hex(),
		"taker":       order.Taker.Hex(),
		"tokenId":     math.NewHexOrDecimal256(order.TokenId.Int64()),
		"makerAmount": math.NewHexOrDecimal256(order.MakerAmount.Int64()),
		"takerAmount": math.NewHexOrDecimal256(order.TakerAmount.Int64()),
		"expiration":  math.NewHexOrDecimal256(order.Expiration.Int64()),
		"nonce":       math.NewHexOrDecimal256(order.Nonce.Int64()),
		"feeRate":     math.NewHexOrDecimal256(order.FeeRate.Int64()),
		"side":        math.NewHexOrDecimal256(int64(order.Side)),
	}

	typedData := apitypes.TypedData{
		Types:       types,
		PrimaryType: "Order",
		Domain:      domain,
		Message:     message,
	}

	// 1. Hash the data
	// domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	// if err != nil { return "", err }
	// typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	// if err != nil { return "", err }
	
	// rawData := []byte(fmt.Sprintf("\x19\x01%s%s", domainSeparator, typedDataHash))
	// hash := crypto.Keccak256(rawData)
	
	// Easiest way using go-ethereum's signer
	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return "", fmt.Errorf("hashing failed: %v", err)
	}

	// 2. Sign
	pk, err := crypto.HexToECDSA(s.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %v", err)
	}

	signature, err := crypto.Sign(hash, pk)
	if err != nil {
		return "", fmt.Errorf("signing failed: %v", err)
	}

	// Adjust V (Eth specific)
	if signature[64] < 27 {
		signature[64] += 27
	}
	
	// Convert to Hex 
	// Note: API often expects r, s, v combined in hex "0x..."
	return "0x" + common.Bytes2Hex(signature), nil
}

// GenerateAuthHeaders creates the headers for CLOB API requests
// Headers: CLOB-API-KEY, CLOB-API-SIGNATURE, CLOB-API-TIMESTAMP, CLOB-API-PASSPHRASE
func (s *Signer) GenerateAuthHeaders(method, requestPath, body string) map[string]string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	
	// Signature = HMAC-SHA256(timestamp + method + requestPath + body, base64_decode(secret))
	// Result encoded as Base64
	
	message := timestamp + method + requestPath + body
	
	secretBytes, err := base64.StdEncoding.DecodeString(s.ApiSecret)
	if err != nil {
		// Fallback: maybe it's raw string? Polymarket usually uses Base64 encoded secrets for Clob keys
		secretBytes = []byte(s.ApiSecret)
	}

	h := hmac.New(sha256.New, secretBytes)
	h.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return map[string]string{
		"CLOB-API-KEY":        s.ApiKey,
		"CLOB-API-SIGNATURE":  signature,
		"CLOB-API-TIMESTAMP":  timestamp,
		"CLOB-API-PASSPHRASE": s.ApiPassphrase,
	}
}
