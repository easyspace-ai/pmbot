package market

import (
	"strconv"
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
