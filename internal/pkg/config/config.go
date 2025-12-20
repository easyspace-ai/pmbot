package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// UserJSON 表示 user.json 文件的结构（参考 GoBet）
type UserJSON struct {
	PrivateKey       string `json:"private_key"`
	Proxy            string `json:"proxy"`
	Address          string `json:"address"`
	RecipientAddress string `json:"recipient_address"`
	ProxyAddress     string `json:"proxy_address"`
	// API 凭证（可选）
	APIKey        string `json:"api_key"`
	APISecret     string `json:"secret"`
	APIPassphrase string `json:"passphrase"`
}

// WalletConfig 钱包配置
type WalletConfig struct {
	PrivateKey    string
	FunderAddress string
}

// ProxyConfig 代理配置
type ProxyConfig struct {
	Host string
	Port int
}

// SimpleThresholdConfig 简单阈值策略配置
type SimpleThresholdConfig struct {
	BuyThreshold float64 // 买入阈值（0.60 = 60分）
	ProfitTarget float64 // 止盈目标（0.03 = 3分）
	OrderSize    float64 // 订单大小
}

// StrategyConfig 策略配置（简化版，适配PMBot）
type StrategyConfig struct {
	EnabledStrategies []string              // 启用的策略列表
	SimpleThreshold   *SimpleThresholdConfig // 简单阈值策略配置
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	Enabled              bool
	MaxPositionPerMarket int64
	MaxTotalPosition     int64
	MaxDailyLoss         float64
	MaxConsecutiveErrors int32
	CooldownSecs         int64
}

// ExecutionConfig 执行配置
type ExecutionConfig struct {
	DeduplicationEnabled bool
	DeduplicationWindowSecs int64
}

// Config 应用配置
type Config struct {
	Wallet    WalletConfig
	Proxy     *ProxyConfig
	Strategies StrategyConfig
	LogLevel  string
	LogFile   string
	LogByCycle bool
	
	// Polymarket 特定配置
	Polymarket PolymarketConfig

	// Circuit Breaker 配置
	CircuitBreaker *CircuitBreakerConfig

	// Execution 配置
	Execution *ExecutionConfig
}

// PolymarketConfig Polymarket 适配器配置
type PolymarketConfig struct {
	BaseURL         string
	MarketSlugRegex string
	YesTokenID      string
	NoTokenID       string
	MarketID        string
	PollInterval    string // 如 "300ms"
	
	TradingEnabled bool
	ChainID        int64
	PrivateKey     string
	Funder         string
	SignatureType  uint8
	
	APIKey        string
	APISecret     string
	APIPassphrase string
	
	// WebSocket 配置
	UseWebSocket   bool   // 是否使用 WebSocket（默认 false，使用轮询）
	WebSocketProxy string // WebSocket 代理 URL
}

// ConfigFile 配置文件结构（用于 YAML/JSON 解析）
// 注意：wallet 和 API 凭证配置已移除，这些信息从 user.json 加载
type ConfigFile struct {
	Proxy struct {
		Host string `yaml:"host" json:"host"`
		Port int    `yaml:"port" json:"port"`
	} `yaml:"proxy" json:"proxy"`
	Strategies struct {
		Enabled []string `yaml:"enabled" json:"enabled"`
		SimpleThreshold struct {
			BuyThreshold float64 `yaml:"buy_threshold" json:"buy_threshold"` // 买入阈值（0.60 = 60分）
			ProfitTarget float64 `yaml:"profit_target" json:"profit_target"` // 止盈目标（0.03 = 3分）
			OrderSize    float64 `yaml:"order_size" json:"order_size"`      // 订单大小
		} `yaml:"simple_threshold" json:"simple_threshold"`
	} `yaml:"strategies" json:"strategies"`
	LogLevel   string `yaml:"log_level" json:"log_level"`
	LogFile    string `yaml:"log_file" json:"log_file"`
	LogByCycle bool   `yaml:"log_by_cycle" json:"log_by_cycle"`
	Polymarket struct {
		BaseURL         string `yaml:"base_url" json:"base_url"`
		MarketSlugRegex string `yaml:"market_slug_regex" json:"market_slug_regex"`
		YesTokenID      string `yaml:"yes_token_id" json:"yes_token_id"`
		NoTokenID       string `yaml:"no_token_id" json:"no_token_id"`
		MarketID        string `yaml:"market_id" json:"market_id"`
		PollInterval    string `yaml:"poll_interval" json:"poll_interval"`
		
		TradingEnabled bool   `yaml:"trading_enabled" json:"trading_enabled"`
		ChainID        int64  `yaml:"chain_id" json:"chain_id"`
		SignatureType  uint8  `yaml:"signature_type" json:"signature_type"`
		
		UseWebSocket   bool   `yaml:"use_websocket" json:"use_websocket"`
		WebSocketProxy string `yaml:"websocket_proxy" json:"websocket_proxy"`
	} `yaml:"polymarket" json:"polymarket"`
	CircuitBreaker struct {
		Enabled              bool    `yaml:"enabled" json:"enabled"`
		MaxPositionPerMarket int64   `yaml:"max_position_per_market" json:"max_position_per_market"`
		MaxTotalPosition     int64   `yaml:"max_total_position" json:"max_total_position"`
		MaxDailyLoss         float64 `yaml:"max_daily_loss" json:"max_daily_loss"`
		MaxConsecutiveErrors int32   `yaml:"max_consecutive_errors" json:"max_consecutive_errors"`
		CooldownSecs         int64   `yaml:"cooldown_secs" json:"cooldown_secs"`
	} `yaml:"circuit_breaker" json:"circuit_breaker"`
	Execution struct {
		DeduplicationEnabled   bool  `yaml:"deduplication_enabled" json:"deduplication_enabled"`
		DeduplicationWindowSecs int64 `yaml:"deduplication_window_secs" json:"deduplication_window_secs"`
	} `yaml:"execution" json:"execution"`
}

var globalConfig *Config
var configFilePath string

// SetConfigPath 设置配置文件路径
func SetConfigPath(path string) {
	configFilePath = path
}

// GetConfigPath 获取配置文件路径
func GetConfigPath() string {
	return configFilePath
}

// Load 加载配置
func Load() (*Config, error) {
	return LoadFromFile(configFilePath)
}

// LoadFromFile 从指定文件加载配置
func LoadFromFile(filePath string) (*Config, error) {
	if globalConfig != nil && configFilePath == filePath {
		return globalConfig, nil
	}

	// 尝试加载配置文件
	var configFile *ConfigFile
	if filePath != "" {
		var err error
		configFile, err = loadConfigFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("加载配置文件失败 %s: %w", filePath, err)
		}
	}

	// 尝试加载 user.json（必须从 /pm/data/user.json 加载，参考 GoBet）
	userJSON, err := loadUserJSON()
	if err != nil {
		// user.json 不存在或解析失败会严重影响程序运行，返回错误
		return nil, fmt.Errorf("加载用户配置失败（必须从 /pm/data/user.json 加载）: %w", err)
	}
	if userJSON == nil {
		return nil, fmt.Errorf("用户配置为空，请检查 /pm/data/user.json 文件")
	}

	// 解析代理配置（优先级：配置文件 > 环境变量 > user.json > 默认值）
	proxyConfig := parseProxyConfigFromSources(configFile, userJSON)

	// 解析熔断器配置
	cbConfig := parseCircuitBreakerConfig(configFile)
	
	// 解析执行配置
	execConfig := parseExecutionConfig(configFile)

	// 构建配置（优先级：环境变量 > user.json > 配置文件 > 默认值）
	// 注意：钱包信息优先从 user.json 加载，配置文件中的钱包配置会被忽略
	config := &Config{
		Wallet: WalletConfig{
			PrivateKey:    getEnvOrUserJSON("POLY_PRIVATE_KEY", userJSON.PrivateKey, ""),
			FunderAddress: getEnvOrUserJSON("POLY_FUNDER", userJSON.ProxyAddress, userJSON.RecipientAddress, userJSON.Address, ""),
		},
		Proxy: proxyConfig,
		Strategies: StrategyConfig{
			EnabledStrategies: parseEnabledStrategies(configFile),
			SimpleThreshold: &SimpleThresholdConfig{
				BuyThreshold: getFloatEnvOrConfig("SIMPLE_THRESHOLD_BUY_THRESHOLD", configFile, func(cf *ConfigFile) float64 { return cf.Strategies.SimpleThreshold.BuyThreshold }, 0.60),
				ProfitTarget: getFloatEnvOrConfig("SIMPLE_THRESHOLD_PROFIT_TARGET", configFile, func(cf *ConfigFile) float64 { return cf.Strategies.SimpleThreshold.ProfitTarget }, 0.03),
				OrderSize:    getFloatEnvOrConfig("SIMPLE_THRESHOLD_ORDER_SIZE", configFile, func(cf *ConfigFile) float64 { return cf.Strategies.SimpleThreshold.OrderSize }, 10.0),
			},
		},
		LogLevel: getEnvOrConfig("LOG_LEVEL", configFile, func(cf *ConfigFile) string { return cf.LogLevel }, "info"),
		LogFile:  getEnvOrConfig("LOG_FILE", configFile, func(cf *ConfigFile) string { return cf.LogFile }, "logs/combined.log"),
		LogByCycle: getBoolEnvOrConfig("LOG_BY_CYCLE", configFile, func(cf *ConfigFile) bool { return cf.LogByCycle }, true),
		Polymarket: PolymarketConfig{
			BaseURL:         getEnvOrConfig("POLY_CLOB_BASE_URL", configFile, func(cf *ConfigFile) string { return cf.Polymarket.BaseURL }, "https://clob.polymarket.com"),
			MarketSlugRegex: getEnvOrConfig("POLY_MARKET_SLUG_REGEX", configFile, func(cf *ConfigFile) string { return cf.Polymarket.MarketSlugRegex }, ""),
			YesTokenID:      getEnvOrConfig("POLY_YES_TOKEN_ID", configFile, func(cf *ConfigFile) string { return cf.Polymarket.YesTokenID }, ""),
			NoTokenID:       getEnvOrConfig("POLY_NO_TOKEN_ID", configFile, func(cf *ConfigFile) string { return cf.Polymarket.NoTokenID }, ""),
			MarketID:        getEnvOrConfig("POLY_MARKET_ID", configFile, func(cf *ConfigFile) string { return cf.Polymarket.MarketID }, ""),
			PollInterval:    getEnvOrConfig("POLY_POLL_INTERVAL", configFile, func(cf *ConfigFile) string { return cf.Polymarket.PollInterval }, "300ms"),
			
			TradingEnabled: getBoolEnvOrConfig("POLY_TRADING_ENABLED", configFile, func(cf *ConfigFile) bool { return cf.Polymarket.TradingEnabled }, false),
			ChainID:        getInt64EnvOrConfig("POLY_CHAIN_ID", configFile, func(cf *ConfigFile) int64 { return cf.Polymarket.ChainID }, 137),
			PrivateKey:     getEnvOrUserJSON("POLY_PRIVATE_KEY", userJSON.PrivateKey, ""),
			Funder:         getEnvOrUserJSON("POLY_FUNDER", userJSON.ProxyAddress, userJSON.RecipientAddress, userJSON.Address, ""),
			SignatureType:  uint8(getIntEnvOrConfig("POLY_SIGNATURE_TYPE", configFile, func(cf *ConfigFile) int { return int(cf.Polymarket.SignatureType) }, 0)),
			
			// API 凭证从 user.json 加载（如果存在）
			APIKey:        getEnvOrUserJSON("POLY_API_KEY", userJSON.APIKey, ""),
			APISecret:     getEnvOrUserJSON("POLY_API_SECRET", userJSON.APISecret, ""),
			APIPassphrase: getEnvOrUserJSON("POLY_API_PASSPHRASE", userJSON.APIPassphrase, ""),
			
			UseWebSocket:   getBoolEnvOrConfig("POLY_USE_WEBSOCKET", configFile, func(cf *ConfigFile) bool { return cf.Polymarket.UseWebSocket }, false),
			WebSocketProxy: getEnvOrConfig("POLY_WEBSOCKET_PROXY", configFile, func(cf *ConfigFile) string { return cf.Polymarket.WebSocketProxy }, ""),
		},
		CircuitBreaker: cbConfig,
		Execution:      execConfig,
	}

	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	// 设置代理环境变量（供 HTTP 客户端使用）
	if config.Proxy != nil {
		proxyURL := fmt.Sprintf("http://%s:%d", config.Proxy.Host, config.Proxy.Port)
		os.Setenv("HTTP_PROXY", proxyURL)
		os.Setenv("HTTPS_PROXY", proxyURL)
		os.Setenv("http_proxy", proxyURL)
		os.Setenv("https_proxy", proxyURL)
	}

	globalConfig = config
	configFilePath = filePath
	return config, nil
}

// loadConfigFile 加载配置文件（支持 YAML 和 JSON）
func loadConfigFile(filePath string) (*ConfigFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var configFile ConfigFile
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &configFile); err != nil {
			return nil, fmt.Errorf("解析 YAML 配置文件失败: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &configFile); err != nil {
			return nil, fmt.Errorf("解析 JSON 配置文件失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的配置文件格式: %s (支持 .yaml, .yml, .json)", ext)
	}

	return &configFile, nil
}

// parseProxyConfigFromSources 从多个源解析代理配置（参考 GoBet）
func parseProxyConfigFromSources(configFile *ConfigFile, userJSON *UserJSON) *ProxyConfig {
	// 优先级：配置文件 > 环境变量 > user.json > 默认值
	var proxyHost, proxyPortStr string

	if configFile != nil && configFile.Proxy.Host != "" {
		proxyHost = configFile.Proxy.Host
		proxyPortStr = fmt.Sprintf("%d", configFile.Proxy.Port)
	} else {
		proxyHost = getEnv("PROXY_HOST", "")
		proxyPortStr = getEnv("PROXY_PORT", "")

		if proxyHost == "" && userJSON != nil && userJSON.Proxy != "" {
			// user.json 中的 proxy 格式为 "http://host:port"
			if strings.HasPrefix(userJSON.Proxy, "http://") {
				parts := strings.Split(strings.TrimPrefix(userJSON.Proxy, "http://"), ":")
				if len(parts) == 2 {
					proxyHost = parts[0]
					proxyPortStr = parts[1]
				}
			}
		}
	}

	// 如果仍未设置，使用默认值
	if proxyHost == "" {
		proxyHost = "127.0.0.1"
	}
	if proxyPortStr == "" {
		proxyPortStr = "15236"
	}

	proxyPort, err := strconv.Atoi(proxyPortStr)
	if err != nil {
		return nil
	}

	return &ProxyConfig{
		Host: proxyHost,
		Port: proxyPort,
	}
}

// parseEnabledStrategies 解析启用的策略列表
func parseEnabledStrategies(configFile *ConfigFile) []string {
	if configFile != nil && len(configFile.Strategies.Enabled) > 0 {
		return configFile.Strategies.Enabled
	}
	
	enabledStrategiesStr := getEnv("ENABLED_STRATEGIES", "")
	if enabledStrategiesStr == "" {
		return []string{} // 返回空列表
	}
	return parseStrategyList(enabledStrategiesStr)
}

// parseCircuitBreakerConfig 解析熔断器配置
func parseCircuitBreakerConfig(configFile *ConfigFile) *CircuitBreakerConfig {
	cbConfig := &CircuitBreakerConfig{
		Enabled:              getBoolEnvOrConfig("CB_ENABLED", configFile, func(cf *ConfigFile) bool { return cf.CircuitBreaker.Enabled }, true),
		MaxPositionPerMarket: getInt64EnvOrConfig("CB_MAX_POSITION_PER_MARKET", configFile, func(cf *ConfigFile) int64 { return cf.CircuitBreaker.MaxPositionPerMarket }, 50000),
		MaxTotalPosition:     getInt64EnvOrConfig("CB_MAX_TOTAL_POSITION", configFile, func(cf *ConfigFile) int64 { return cf.CircuitBreaker.MaxTotalPosition }, 100000),
		MaxDailyLoss:         getFloatEnvOrConfig("CB_MAX_DAILY_LOSS", configFile, func(cf *ConfigFile) float64 { return cf.CircuitBreaker.MaxDailyLoss }, 500.0),
		MaxConsecutiveErrors: int32(getIntEnvOrConfig("CB_MAX_CONSECUTIVE_ERRORS", configFile, func(cf *ConfigFile) int { return int(cf.CircuitBreaker.MaxConsecutiveErrors) }, 5)),
		CooldownSecs:         getInt64EnvOrConfig("CB_COOLDOWN_SECS", configFile, func(cf *ConfigFile) int64 { return cf.CircuitBreaker.CooldownSecs }, 300),
	}
	return cbConfig
}

// parseExecutionConfig 解析执行配置
func parseExecutionConfig(configFile *ConfigFile) *ExecutionConfig {
	execConfig := &ExecutionConfig{
		DeduplicationEnabled:  getBoolEnvOrConfig("EXEC_DEDUPLICATION_ENABLED", configFile, func(cf *ConfigFile) bool { return cf.Execution.DeduplicationEnabled }, true),
		DeduplicationWindowSecs: getInt64EnvOrConfig("EXEC_DEDUPLICATION_WINDOW_SECS", configFile, func(cf *ConfigFile) int64 { return cf.Execution.DeduplicationWindowSecs }, 10),
	}
	return execConfig
}

// parseStrategyList 解析策略列表（逗号分隔）
func parseStrategyList(str string) []string {
	if str == "" {
		return []string{}
	}
	parts := strings.Split(str, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.Polymarket.PrivateKey == "" && c.Polymarket.TradingEnabled {
		return fmt.Errorf("POLY_PRIVATE_KEY 未配置（交易已启用）")
	}
	return nil
}

// Get 获取全局配置（如果已加载）
func Get() *Config {
	return globalConfig
}

// 辅助函数
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvOrConfig(envKey string, configFile *ConfigFile, configGetter func(*ConfigFile) string, defaultValue string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	if configFile != nil {
		if configValue := configGetter(configFile); configValue != "" {
			return configValue
		}
	}
	return defaultValue
}

func getBoolEnvOrConfig(envKey string, configFile *ConfigFile, configGetter func(*ConfigFile) bool, defaultValue bool) bool {
	if value := os.Getenv(envKey); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	if configFile != nil {
		return configGetter(configFile)
	}
	return defaultValue
}

func getIntEnvOrConfig(envKey string, configFile *ConfigFile, configGetter func(*ConfigFile) int, defaultValue int) int {
	if value := os.Getenv(envKey); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	if configFile != nil {
		return configGetter(configFile)
	}
	return defaultValue
}

func getInt64EnvOrConfig(envKey string, configFile *ConfigFile, configGetter func(*ConfigFile) int64, defaultValue int64) int64 {
	if value := os.Getenv(envKey); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	if configFile != nil {
		return configGetter(configFile)
	}
	return defaultValue
}

// getFloatEnvOrConfig 从多个源获取浮点数值
func getFloatEnvOrConfig(envKey string, configFile *ConfigFile, configGetter func(*ConfigFile) float64, defaultValue float64) float64 {
	if value := os.Getenv(envKey); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	if configFile != nil {
		return configGetter(configFile)
	}
	return defaultValue
}

// loadUserJSON 加载 user.json 文件（参考 GoBet）
func loadUserJSON() (*UserJSON, error) {
	// 只从 /pm/data/user.json 加载（不再使用 botuser.json）
	possiblePaths := []string{
		"/pm/data/user.json", // 绝对路径（唯一路径）
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var userJSON UserJSON
			if err := json.Unmarshal(data, &userJSON); err != nil {
				return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
			}

			fmt.Printf("✅ 从 %s 加载钱包配置\n", path)
			return &userJSON, nil
		}
	}

	return nil, fmt.Errorf("未找到 /pm/data/user.json 文件")
}

// getEnvOrUserJSON 获取环境变量或 user.json 中的值，按优先级返回第一个非空值
func getEnvOrUserJSON(envKey string, userJSONValues ...string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	for _, v := range userJSONValues {
		if v != "" {
			return v
		}
	}
	return ""
}

