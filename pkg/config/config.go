package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Polymarket API Credentials
	ApiKey        string
	ApiSecret     string
	ApiPassphrase string

	// Wallet Credentials
	PrivateKey string // Hex string without 0x
	Address    string

	// Trading Config
	PolygonRpcUrl string
	ChainID       int64
	ExchangeAddr  string // CTF Exchange Contract Address
}

func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, relying on environment variables")
	}

	return &Config{
		ApiKey:        getEnv("POLY_API_KEY", ""),
		ApiSecret:     getEnv("POLY_API_SECRET", ""),
		ApiPassphrase: getEnv("POLY_API_PASSPHRASE", ""),
		PrivateKey:    getEnv("POLY_PRIVATE_KEY", ""),
		Address:       getEnv("POLY_WALLET_ADDRESS", ""),
		PolygonRpcUrl: getEnv("POLYGON_RPC_URL", "https://polygon-rpc.com"),
		ChainID:       137,
		// Standard Polymarket CTF Exchange Address on Polygon
		ExchangeAddr: getEnv("POLY_EXCHANGE_ADDR", "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int64) int64 {
	if value, ok := os.LookupEnv(key); ok {
		i, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return i
		}
	}
	return fallback
}
