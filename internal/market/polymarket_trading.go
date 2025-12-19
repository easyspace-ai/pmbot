package market

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/shopspring/decimal"

	"polymarket-btc-bot/internal/oms"
	"polymarket-btc-bot/internal/types"
)

var (
	ErrTradingNotConfigured = errors.New("polymarket trading not configured (set POLY_TRADING_ENABLED=1 and credentials)")
)

type apiCreds struct {
	APIKey        string
	APISecret     string
	APIPassphrase string
}

func newApiCredsFromEnv(k, s, p string) *apiCreds {
	if k == "" || s == "" || p == "" {
		return nil
	}
	return &apiCreds{APIKey: k, APISecret: s, APIPassphrase: p}
}

func (p *Polymarket) initTrading(ctx context.Context) error {
	if p.privateKey == "" {
		return fmt.Errorf("%w: POLY_PRIVATE_KEY is required", ErrTradingNotConfigured)
	}
	priv, addr, err := parsePrivateKey(p.privateKey)
	if err != nil {
		return err
	}
	p.address = addr.Hex()
	if p.funder == "" {
		p.funder = p.address
	}

	if p.apiCreds == nil {
		creds, err := p.createOrDeriveAPIKey(ctx, priv)
		if err != nil {
			return err
		}
		p.apiCreds = creds
	}

	p.log.Info("polymarket trading initialized",
		"address", p.address,
		"funder", p.funder,
		"chain_id", p.chainID,
		"signature_type", p.signatureType,
		"has_api_creds", p.apiCreds != nil,
	)
	return nil
}

func (p *Polymarket) PlaceOrder(ctx context.Context, req oms.PlaceOrderRequest) (oms.PlaceOrderResult, error) {
	if !p.tradingEnabled {
		return oms.PlaceOrderResult{Accepted: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}
	if p.apiCreds == nil || p.address == "" || p.privateKey == "" {
		return oms.PlaceOrderResult{Accepted: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}

	tokenID := ""
	switch req.Side {
	case types.SideYes:
		tokenID = p.yesTokenID
	case types.SideNo:
		tokenID = p.noTokenID
	default:
		return oms.PlaceOrderResult{Accepted: false, Reason: "bad_side"}, fmt.Errorf("unsupported side %v", req.Side)
	}

	priv, _, err := parsePrivateKey(p.privateKey)
	if err != nil {
		return oms.PlaceOrderResult{Accepted: false, Reason: "bad_private_key"}, err
	}

	// NOTE: This bot uses "buy outcome token" for both YES/NO to build nonlinear payoff.
	signed, err := p.buildSignedBuyOrder(priv, tokenID, req.Price, req.Size)
	if err != nil {
		return oms.PlaceOrderResult{Accepted: false, Reason: "build_order_failed"}, err
	}

	body := postOrderBody{
		Order:     signed,
		Owner:     p.apiCreds.APIKey,
		OrderType: "GTC",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return oms.PlaceOrderResult{Accepted: false, Reason: "marshal_failed"}, err
	}

	headers := p.l2Headers("POST", "/order", raw)
	u := p.baseURL + "/order"
	resBody, status, err := p.do(ctx, http.MethodPost, u, headers, raw)
	if err != nil {
		return oms.PlaceOrderResult{Accepted: false, Reason: "http_failed"}, err
	}
	if status/100 != 2 {
		return oms.PlaceOrderResult{Accepted: false, Reason: "http_non_2xx"}, fmt.Errorf("http %d: %s", status, trunc(resBody, 512))
	}

	orderID := parseOrderID(resBody)
	return oms.PlaceOrderResult{
		ExchangeOrderID: orderID,
		Accepted:        true,
	}, nil
}

func (p *Polymarket) CancelOrder(ctx context.Context, req oms.CancelOrderRequest) (oms.CancelOrderResult, error) {
	if !p.tradingEnabled {
		return oms.CancelOrderResult{Ok: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}
	if p.apiCreds == nil || p.address == "" {
		return oms.CancelOrderResult{Ok: false, Reason: "not_configured"}, ErrTradingNotConfigured
	}

	orderID := req.ExchangeOrderID
	if orderID == "" {
		orderID = req.ClientOrderID
	}
	if orderID == "" {
		return oms.CancelOrderResult{Ok: false, Reason: "missing_order_id"}, fmt.Errorf("missing order id")
	}

	payload := map[string]string{"orderID": orderID}
	raw, err := json.Marshal(payload)
	if err != nil {
		return oms.CancelOrderResult{Ok: false, Reason: "marshal_failed"}, err
	}

	headers := p.l2Headers("DELETE", "/order", raw)
	u := p.baseURL + "/order"
	resBody, status, err := p.do(ctx, http.MethodDelete, u, headers, raw)
	if err != nil {
		return oms.CancelOrderResult{Ok: false, Reason: "http_failed"}, err
	}
	if status/100 != 2 {
		return oms.CancelOrderResult{Ok: false, Reason: "http_non_2xx"}, fmt.Errorf("http %d: %s", status, trunc(resBody, 512))
	}
	return oms.CancelOrderResult{Ok: true}, nil
}

// --- Auth (L1/L2) ---

func (p *Polymarket) createOrDeriveAPIKey(ctx context.Context, priv *ecdsa.PrivateKey) (*apiCreds, error) {
	// Create first; if it fails (e.g. already exists), derive.
	creds, err := p.createAPIKey(ctx, priv)
	if err == nil {
		return creds, nil
	}
	creds, err2 := p.deriveAPIKey(ctx, priv)
	if err2 == nil {
		return creds, nil
	}
	return nil, fmt.Errorf("create api key failed: %v; derive failed: %v", err, err2)
}

func (p *Polymarket) createAPIKey(ctx context.Context, priv *ecdsa.PrivateKey) (*apiCreds, error) {
	headers, err := p.l1Headers(priv, 0)
	if err != nil {
		return nil, err
	}
	u := p.baseURL + "/auth/api-key"
	resBody, status, err := p.do(ctx, http.MethodPost, u, headers, nil)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", status, trunc(resBody, 512))
	}
	return parseCreds(resBody)
}

func (p *Polymarket) deriveAPIKey(ctx context.Context, priv *ecdsa.PrivateKey) (*apiCreds, error) {
	headers, err := p.l1Headers(priv, 0)
	if err != nil {
		return nil, err
	}
	u := p.baseURL + "/auth/derive-api-key"
	resBody, status, err := p.do(ctx, http.MethodGet, u, headers, nil)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", status, trunc(resBody, 512))
	}
	return parseCreds(resBody)
}

func (p *Polymarket) l1Headers(priv *ecdsa.PrivateKey, nonce uint64) (map[string]string, error) {
	ts := time.Now().Unix()
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	sig, err := signClobAuth(priv, p.chainID, addr, ts, nonce)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"POLY_ADDRESS":   addr,
		"POLY_SIGNATURE": sig,
		"POLY_TIMESTAMP": fmt.Sprintf("%d", ts),
		"POLY_NONCE":     fmt.Sprintf("%d", nonce),
	}, nil
}

func (p *Polymarket) l2Headers(method, requestPath string, body []byte) map[string]string {
	ts := time.Now().Unix()
	sig := buildHMACSignature(p.apiCreds.APISecret, ts, method, requestPath, body)
	return map[string]string{
		"POLY_ADDRESS":    p.address,
		"POLY_SIGNATURE":  sig,
		"POLY_TIMESTAMP":  fmt.Sprintf("%d", ts),
		"POLY_API_KEY":    p.apiCreds.APIKey,
		"POLY_PASSPHRASE": p.apiCreds.APIPassphrase,
	}
}

func buildHMACSignature(secretBase64URL string, timestamp int64, method, requestPath string, body []byte) string {
	// Python client uses urlsafe_b64decode for secret.
	key, err := base64.URLEncoding.DecodeString(secretBase64URL)
	if err != nil {
		// Fallback: some environments store padded/standard base64; try StdEncoding too.
		key, _ = base64.StdEncoding.DecodeString(secretBase64URL)
	}

	msg := fmt.Sprintf("%d%s%s", timestamp, method, requestPath)
	if len(body) > 0 {
		msg += string(body)
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msg))
	sum := mac.Sum(nil)
	return base64.URLEncoding.EncodeToString(sum)
}

// --- Order building & signing ---

const (
	clobAuthMsgToSign = "This message attests that I control the given wallet"
)

type postOrderBody struct {
	Order     signedOrderJSON `json:"order"`
	Owner     string          `json:"owner"`
	OrderType string          `json:"orderType"`
}

type signedOrderJSON struct {
	Salt          uint64 `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	Taker         string `json:"taker"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Expiration    string `json:"expiration"`
	Nonce         string `json:"nonce"`
	FeeRateBps    string `json:"feeRateBps"`
	Side          string `json:"side"` // "BUY" or "SELL"
	SignatureType uint8  `json:"signatureType"`
	Signature     string `json:"signature"`
}

func (p *Polymarket) buildSignedBuyOrder(priv *ecdsa.PrivateKey, tokenID string, price, size float64) (signedOrderJSON, error) {
	if p.minTickSize == "" {
		p.minTickSize = "0.01"
	}

	// Validate and compute amounts similar to official python builder.
	makerAmt, takerAmt, err := computeBuyAmounts(p.minTickSize, price, size)
	if err != nil {
		return signedOrderJSON{}, err
	}

	salt, err := randUint64()
	if err != nil {
		return signedOrderJSON{}, err
	}

	ex := exchangeAddress(p.chainID, p.negRisk)
	if ex == "" {
		return signedOrderJSON{}, fmt.Errorf("unsupported chain_id %d", p.chainID)
	}

	msg := map[string]any{
		"salt":          fmt.Sprintf("%d", salt),
		"maker":         p.funder,
		"signer":        p.address,
		"taker":         zeroAddress,
		"tokenId":       tokenID,
		"makerAmount":   makerAmt.String(),
		"takerAmount":   takerAmt.String(),
		"expiration":    "0",
		"nonce":         "0",
		"feeRateBps":    "0",
		"side":          "0", // BUY
		"signatureType": fmt.Sprintf("%d", p.signatureType),
	}

	sig, err := signEIP712Order(priv, p.chainID, ex, msg)
	if err != nil {
		return signedOrderJSON{}, err
	}

	return signedOrderJSON{
		Salt:          salt,
		Maker:         p.funder,
		Signer:        p.address,
		Taker:         zeroAddress,
		TokenID:       tokenID,
		MakerAmount:   makerAmt.String(),
		TakerAmount:   takerAmt.String(),
		Expiration:    "0",
		Nonce:         "0",
		FeeRateBps:    "0",
		Side:          "BUY",
		SignatureType: p.signatureType,
		Signature:     sig,
	}, nil
}

// computeBuyAmounts implements the BUY branch of py-clob-client order builder using exact decimal math.
func computeBuyAmounts(tickSize string, price, size float64) (*big.Int, *big.Int, error) {
	rc, ok := roundingConfig[tickSize]
	if !ok {
		// Default to 0.01 behavior (most markets).
		rc = roundingConfig["0.01"]
	}

	p := decimal.NewFromFloat(price)
	sz := decimal.NewFromFloat(size)

	// price validity
	tick, err := decimal.NewFromString(tickSize)
	if err != nil {
		return nil, nil, fmt.Errorf("bad tick size: %s", tickSize)
	}
	if p.LessThan(tick) || p.GreaterThan(decimal.NewFromInt(1).Sub(tick)) {
		return nil, nil, fmt.Errorf("price out of bounds for tick_size=%s", tickSize)
	}

	rawPrice := roundNormal(p, rc.PriceDecimals)
	rawTaker := roundDown(sz, rc.SizeDecimals)
	rawMaker := rawTaker.Mul(rawPrice)

	if decimalPlaces(rawMaker) > rc.AmountDecimals {
		rawMaker = roundUp(rawMaker, rc.AmountDecimals+4)
		if decimalPlaces(rawMaker) > rc.AmountDecimals {
			rawMaker = roundDown(rawMaker, rc.AmountDecimals)
		}
	}

	maker := toTokenDecimals(rawMaker)
	taker := toTokenDecimals(rawTaker)
	return maker, taker, nil
}

type roundCfg struct {
	PriceDecimals  int32
	SizeDecimals   int32
	AmountDecimals int32
}

var roundingConfig = map[string]roundCfg{
	"0.1":    {PriceDecimals: 1, SizeDecimals: 2, AmountDecimals: 3},
	"0.01":   {PriceDecimals: 2, SizeDecimals: 2, AmountDecimals: 4},
	"0.001":  {PriceDecimals: 3, SizeDecimals: 2, AmountDecimals: 5},
	"0.0001": {PriceDecimals: 4, SizeDecimals: 2, AmountDecimals: 6},
}

func roundDown(x decimal.Decimal, places int32) decimal.Decimal   { return x.Truncate(places) }
func roundNormal(x decimal.Decimal, places int32) decimal.Decimal { return x.Round(places) }
func roundUp(x decimal.Decimal, places int32) decimal.Decimal {
	shifted := x.Shift(places)
	return shifted.Ceil().Shift(-places)
}

func decimalPlaces(x decimal.Decimal) int32 {
	exp := x.Exponent()
	if exp >= 0 {
		return 0
	}
	return int32(-exp)
}

func toTokenDecimals(x decimal.Decimal) *big.Int {
	f := x.Mul(decimal.NewFromInt(1_000_000))
	if decimalPlaces(f) > 0 {
		f = f.Round(0)
	}
	return f.BigInt()
}

const zeroAddress = "0x0000000000000000000000000000000000000000"

func exchangeAddress(chainID int64, negRisk bool) string {
	// From official py-clob-client config.py
	switch chainID {
	case 137:
		if negRisk {
			return "0xC5d563A36AE78145C45a50134d48A1215220f80a"
		}
		return "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	case 80002:
		if negRisk {
			return "0xd91E80cF2E7be2e162c6513ceD06f1dD0dA35296"
		}
		return "0xdFE02Eb6733538f8Ea35D585af8DE5958AD99E40"
	default:
		return ""
	}
}

func signClobAuth(priv *ecdsa.PrivateKey, chainID int64, address string, timestamp int64, nonce uint64) (string, error) {
	typed := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
			},
			"ClobAuth": []apitypes.Type{
				{Name: "address", Type: "address"},
				{Name: "timestamp", Type: "string"},
				{Name: "nonce", Type: "uint256"},
				{Name: "message", Type: "string"},
			},
		},
		PrimaryType: "ClobAuth",
		Domain: apitypes.TypedDataDomain{
			Name:    "ClobAuthDomain",
			Version: "1",
			ChainId: ethmath.NewHexOrDecimal256(chainID),
		},
		Message: apitypes.TypedDataMessage{
			"address":   address,
			"timestamp": fmt.Sprintf("%d", timestamp),
			"nonce":     fmt.Sprintf("%d", nonce),
			"message":   clobAuthMsgToSign,
		},
	}
	return signTypedData(priv, typed)
}

func signEIP712Order(priv *ecdsa.PrivateKey, chainID int64, exchangeAddr string, msg map[string]any) (string, error) {
	typed := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Order": []apitypes.Type{
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
		},
		PrimaryType: "Order",
		Domain: apitypes.TypedDataDomain{
			Name:              "Polymarket CTF Exchange",
			Version:           "1",
			ChainId:           ethmath.NewHexOrDecimal256(chainID),
			VerifyingContract: exchangeAddr,
		},
		Message: apitypes.TypedDataMessage(msg),
	}
	return signTypedData(priv, typed)
}

func signTypedData(priv *ecdsa.PrivateKey, typed apitypes.TypedData) (string, error) {
	raw, _, err := apitypes.TypedDataAndHash(typed)
	if err != nil {
		return "", err
	}
	sig, err := crypto.Sign(raw, priv)
	if err != nil {
		return "", err
	}
	// go-ethereum returns v in {0,1}; Polymarket clients expect {27,28}.
	sig[64] += 27
	return hexutil.Encode(sig), nil
}

func parsePrivateKey(hexKey string) (*ecdsa.PrivateKey, common.Address, error) {
	k := strings.TrimSpace(hexKey)
	k = strings.TrimPrefix(k, "0x")
	priv, err := crypto.HexToECDSA(k)
	if err != nil {
		return nil, common.Address{}, err
	}
	return priv, crypto.PubkeyToAddress(priv.PublicKey), nil
}

func randUint64() (uint64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return new(big.Int).SetBytes(b[:]).Uint64(), nil
}

func parseCreds(body []byte) (*apiCreds, error) {
	var raw struct {
		APIKey     string `json:"apiKey"`
		Secret     string `json:"secret"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.APIKey == "" || raw.Secret == "" || raw.Passphrase == "" {
		return nil, fmt.Errorf("missing creds fields")
	}
	return &apiCreds{APIKey: raw.APIKey, APISecret: raw.Secret, APIPassphrase: raw.Passphrase}, nil
}

func (p *Polymarket) do(ctx context.Context, method, url string, headers map[string]string, body []byte) ([]byte, int, error) {
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "polymarket-btc-bot/0.1")
	if len(body) > 0 {
		req.Header.Set("content-type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := p.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return b, res.StatusCode, nil
}

func parseOrderID(body []byte) string {
	// Best-effort extraction (server responses differ by endpoint/version).
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if v, ok := m["orderID"].(string); ok {
		return v
	}
	if v, ok := m["orderId"].(string); ok {
		return v
	}
	if v, ok := m["id"].(string); ok {
		return v
	}
	return ""
}

func trunc(b []byte, n int) string {
	s := string(bytes.TrimSpace(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ensure go-ethereum imports are used
var _ = hexutil.Encode
