package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	// Logger 全局日志实例
	Logger *logrus.Logger
	// currentLogFile 当前日志文件路径
	currentLogFile string
	// baseLogFile 基础日志文件路径（配置中的原始路径）
	baseLogFile string
	// savedConfig 保存的日志配置（用于日志轮转）
	savedConfig Config
	// currentPeriod 当前周期时间戳（市场周期时间戳，如果为0则使用15分钟对齐）
	currentPeriod int64
	// currentMarketTimestamp 当前市场周期时间戳（从市场 slug 提取）
	currentMarketTimestamp int64
	// logMu 日志文件切换锁
	logMu sync.Mutex
	// cycleDuration 周期时长（默认15分钟）
	cycleDuration = 15 * time.Minute
)

// Config 日志配置
type Config struct {
	Level         string        // 日志级别: debug, info, warn, error
	OutputFile    string        // 日志文件路径（可选，为空则只输出到控制台）
	MaxSize       int           // 日志文件最大大小（MB）
	MaxBackups    int           // 保留的旧日志文件数量
	MaxAge        int           // 保留旧日志文件的天数
	Compress      bool          // 是否压缩旧日志文件
	LogByCycle    bool          // 是否按周期命名日志文件
	CycleDuration time.Duration // 周期时长（默认15分钟）
}

// getCurrentPeriod 获取当前周期的时间戳
// 如果设置了市场周期时间戳，优先使用；否则使用15分钟对齐时间
func getCurrentPeriod(cycleDuration time.Duration) int64 {
	// 如果设置了市场周期时间戳，优先使用
	if currentMarketTimestamp > 0 {
		return currentMarketTimestamp
	}
	// 否则使用15分钟对齐时间
	now := time.Now()
	periodStart := now.Truncate(cycleDuration)
	return periodStart.Unix()
}

// SetMarketTimestamp 设置当前市场周期时间戳（从市场 slug 提取）
// 例如：btc-updown-15m-1765985400 -> 1765985400
func SetMarketTimestamp(timestamp int64) {
	logMu.Lock()
	defer logMu.Unlock()
	currentMarketTimestamp = timestamp
}

// getLogFileName 根据周期生成日志文件名
func getLogFileName(basePath string, period int64) string {
	// 如果设置了市场周期时间戳，使用市场格式：btc-updown-15m-{timestamp}.log
	if currentMarketTimestamp > 0 && period == currentMarketTimestamp {
		// 如果 basePath 包含目录，保留目录结构
		dir := filepath.Dir(basePath)
		ext := filepath.Ext(basePath)
		
		// 格式: btc-updown-15m-{timestamp}.log
		logFileName := fmt.Sprintf("btc-updown-15m-%d%s", period, ext)
		
		if dir == "." || dir == "" {
			return logFileName
		}
		return filepath.Join(dir, logFileName)
	}
	
	// 否则使用日期时间格式：logs/2025-12-17_22-30.log
	periodTime := time.Unix(period, 0)
	periodStr := periodTime.Format("2006-01-02_15-04")
	
	dir := filepath.Dir(basePath)
	baseName := filepath.Base(basePath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	
	if dir == "." || dir == "" {
		return fmt.Sprintf("%s_%s%s", nameWithoutExt, periodStr, ext)
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%s%s", nameWithoutExt, periodStr, ext))
}

// Init 初始化日志系统
func Init(config Config) error {
	logMu.Lock()
	defer logMu.Unlock()

	logger := logrus.New()

	// 设置日志级别
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// 设置日志格式
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "06-01-02 15:04:05", // 格式: yy-mm-dd HH:MM:ss
		ForceColors:     true,
	})

	// 设置输出
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	// 如果配置了日志文件，添加文件输出
	if config.OutputFile != "" {
		var logFilePath string
		
		// 保存基础日志文件路径和配置
		baseLogFile = config.OutputFile
		savedConfig = config
		
		// 如果启用按周期命名，生成周期日志文件名
		if config.LogByCycle {
			if config.CycleDuration == 0 {
				config.CycleDuration = cycleDuration
			}
			cycleDuration = config.CycleDuration
			period := getCurrentPeriod(config.CycleDuration)
			currentPeriod = period
			logFilePath = getLogFileName(config.OutputFile, period)
		} else {
			logFilePath = config.OutputFile
		}
		
		// 确保日志目录存在
		logDir := filepath.Dir(logFilePath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return err
		}

		// 配置日志轮转
		fileWriter := &lumberjack.Logger{
			Filename:   logFilePath,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		writers = append(writers, fileWriter)
		currentLogFile = logFilePath
	}

	// 使用 MultiWriter 同时输出到控制台和文件
	multiWriter := io.MultiWriter(writers...)
	logger.SetOutput(multiWriter)

	// 同时设置全局 logrus 的输出，确保所有使用 logrus 的地方都能写入文件
	// 这样策略中使用 logrus.WithField() 创建的 logger 也能写入文件
	logrus.SetOutput(multiWriter)
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "06-01-02 15:04:05", // 格式: yy-mm-dd HH:MM:ss
		ForceColors:     true,
	})

	Logger = logger
	return nil
}

// CheckAndRotateLog 检查并切换日志文件（如果周期变化）
// forceRotate: 如果为 true，强制切换日志文件（即使周期未变化）
func CheckAndRotateLog(config Config) error {
	return CheckAndRotateLogWithForce(config, false)
}

// CheckAndRotateLogWithForce 检查并切换日志文件（如果周期变化或强制切换）
func CheckAndRotateLogWithForce(config Config, forceRotate bool) error {
	if !config.LogByCycle {
		return nil
	}

	logMu.Lock()
	defer logMu.Unlock()

	// 使用基础日志文件路径（如果配置中提供了，优先使用；否则使用保存的基础路径）
	basePath := config.OutputFile
	if basePath == "" {
		basePath = baseLogFile
	}
	if basePath == "" {
		return nil // 没有基础路径，无法切换
	}

	// 合并配置（使用传入的配置覆盖保存的配置）
	mergedConfig := savedConfig
	if config.Level != "" {
		mergedConfig.Level = config.Level
	}
	if config.CycleDuration > 0 {
		mergedConfig.CycleDuration = config.CycleDuration
	}
	if config.MaxSize > 0 {
		mergedConfig.MaxSize = config.MaxSize
	}
	if config.MaxBackups > 0 {
		mergedConfig.MaxBackups = config.MaxBackups
	}
	if config.MaxAge > 0 {
		mergedConfig.MaxAge = config.MaxAge
	}

	period := getCurrentPeriod(mergedConfig.CycleDuration)
	
	// 如果设置了市场时间戳，且当前周期不等于市场时间戳，需要切换
	// 或者强制切换，或者周期变化
	shouldRotate := forceRotate || period != currentPeriod || 
		(currentMarketTimestamp > 0 && period != currentMarketTimestamp)
	
	if !shouldRotate {
		return nil // 不需要切换
	}

	// 周期变化，切换日志文件
	logFilePath := getLogFileName(basePath, period)
	
	// 如果新文件路径和当前文件路径相同，且不是强制切换，不需要切换
	if logFilePath == currentLogFile && !forceRotate {
		return nil
	}
	
	oldLogFile := currentLogFile
	currentPeriod = period
	
	// 记录切换信息（使用 fmt.Printf 避免依赖 Logger，因为可能正在切换）
	if oldLogFile != "" {
		fmt.Printf("[日志切换] %s -> %s (市场时间戳=%d, period=%d, forceRotate=%v)\n", 
			oldLogFile, logFilePath, currentMarketTimestamp, period, forceRotate)
	}

	// 重新初始化日志输出
	logger := logrus.New()

	// 设置日志级别
	level, err := logrus.ParseLevel(mergedConfig.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// 设置日志格式
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "06-01-02 15:04:05", // 格式: yy-mm-dd HH:MM:ss
		ForceColors:     true,
	})

	// 设置输出
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	// 确保日志目录存在
	logDir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// 配置新的日志文件
	fileWriter := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    mergedConfig.MaxSize,
		MaxBackups: mergedConfig.MaxBackups,
		MaxAge:     mergedConfig.MaxAge,
		Compress:   mergedConfig.Compress,
	}
	writers = append(writers, fileWriter)
	currentLogFile = logFilePath

	// 使用 MultiWriter 同时输出到控制台和文件
	multiWriter := io.MultiWriter(writers...)
	logger.SetOutput(multiWriter)

	// 同时设置全局 logrus 的输出，确保所有使用 logrus 的地方都能写入文件
	// 这样策略中使用 logrus.WithField() 创建的 logger 也能写入文件
	logrus.SetOutput(multiWriter)
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "06-01-02 15:04:05", // 格式: yy-mm-dd HH:MM:ss
		ForceColors:     true,
	})

	Logger = logger
	Logger.Infof("日志文件已切换到新周期: %s", logFilePath)
	return nil
}

// InitDefault 使用默认配置初始化日志系统
func InitDefault() error {
	return Init(Config{
		Level:         "info",
		OutputFile:    "logs/combined.log",
		MaxSize:       100, // 100MB
		MaxBackups:    3,
		MaxAge:        7, // 7天
		Compress:      true,
		LogByCycle:    true, // 默认按周期命名
		CycleDuration: 15 * time.Minute,
	})
}

// StartLogRotationChecker 启动日志轮转检查器（后台任务）
func StartLogRotationChecker(config Config) {
	if !config.LogByCycle || config.OutputFile == "" {
		return
	}

	go func() {
		ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
		defer ticker.Stop()

		for range ticker.C {
			if err := CheckAndRotateLog(config); err != nil {
				if Logger != nil {
					Logger.Errorf("检查日志轮转失败: %v", err)
				}
			}
		}
	}()
}

// Debug 记录 DEBUG 级别日志
func Debug(args ...interface{}) {
	if Logger != nil {
		Logger.Debug(args...)
	}
}

// Debugf 记录格式化的 DEBUG 级别日志
func Debugf(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Debugf(format, args...)
	}
}

// Info 记录 INFO 级别日志
func Info(args ...interface{}) {
	if Logger != nil {
		Logger.Info(args...)
	}
}

// Infof 记录格式化的 INFO 级别日志
func Infof(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Infof(format, args...)
	}
}

// Warn 记录 WARN 级别日志
func Warn(args ...interface{}) {
	if Logger != nil {
		Logger.Warn(args...)
	}
}

// Warnf 记录格式化的 WARN 级别日志
func Warnf(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Warnf(format, args...)
	}
}

// Error 记录 ERROR 级别日志
func Error(args ...interface{}) {
	if Logger != nil {
		Logger.Error(args...)
	}
}

// Errorf 记录格式化的 ERROR 级别日志
func Errorf(format string, args ...interface{}) {
	if Logger != nil {
		Logger.Errorf(format, args...)
	}
}

// WithField 添加字段到日志上下文
func WithField(key string, value interface{}) *logrus.Entry {
	if Logger != nil {
		return Logger.WithField(key, value)
	}
	return logrus.NewEntry(logrus.New())
}

// WithFields 添加多个字段到日志上下文
func WithFields(fields logrus.Fields) *logrus.Entry {
	if Logger != nil {
		return Logger.WithFields(fields)
	}
	return logrus.NewEntry(logrus.New())
}

// GetCurrentLogFile 获取当前日志文件路径
func GetCurrentLogFile() string {
	logMu.Lock()
	defer logMu.Unlock()
	return currentLogFile
}

