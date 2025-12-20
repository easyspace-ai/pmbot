package market

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// GetCurrent15MinTimestamp 获取当前 15 分钟周期的时间戳
// 返回当前周期的开始时间戳（Unix 秒）
func GetCurrent15MinTimestamp() int64 {
	now := time.Now()
	minutes := now.Minute()
	roundedMinutes := (minutes / 15) * 15

	periodStart := time.Date(now.Year(), now.Month(), now.Day(),
		now.Hour(), roundedMinutes, 0, 0, now.Location())

	return periodStart.Unix()
}

// Generate15MinSlug 生成 15 分钟周期的 slug
// 格式: btc-updown-15m-{timestamp}
func Generate15MinSlug(timestamp int64) string {
	return fmt.Sprintf("btc-updown-15m-%d", timestamp)
}

// ExtractTimestampFromSlug 从 slug 提取时间戳
func ExtractTimestampFromSlug(slug string) int64 {
	re := regexp.MustCompile(`-(\d+)$`)
	matches := re.FindStringSubmatch(slug)
	if len(matches) >= 2 {
		if ts, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
			return ts
		}
	}
	return time.Now().Unix()
}

// GetCycleEndTime 获取周期的结束时间
func GetCycleEndTime(timestamp int64) time.Time {
	return time.Unix(timestamp+900, 0) // 15 分钟 = 900 秒
}

// GetNextCycleTimestamp 获取下一个周期的开始时间戳
func GetNextCycleTimestamp(currentTimestamp int64) int64 {
	return currentTimestamp + 900 // 15 分钟 = 900 秒
}

// IsCycleExpired 检查周期是否已过期
func IsCycleExpired(timestamp int64) bool {
	now := time.Now().Unix()
	cycleEndTs := timestamp + 900 // 15 分钟 = 900 秒
	return now >= cycleEndTs
}

// GetRemainingSeconds 获取周期剩余秒数
func GetRemainingSeconds(timestamp int64) int64 {
	now := time.Now().Unix()
	cycleEndTs := timestamp + 900 // 15 分钟 = 900 秒
	remaining := cycleEndTs - now
	if remaining < 0 {
		return 0
	}
	return remaining
}

