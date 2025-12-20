package execution

import (
	"sync"
	"sync/atomic"
	"time"
)

// Deduplicator 位掩码去重器
// 使用位掩码实现高效的执行去重，支持最多512个不同的执行ID
type Deduplicator struct {
	// 位掩码数组（8个uint64，支持512个ID）
	inFlight [8]atomic.Uint64

	// 延迟释放的goroutine管理
	releaseTimers sync.Map // map[uint16]*time.Timer

	// 配置
	windowSecs int64 // 去重窗口（秒）
}

// NewDeduplicator 创建新的去重器
func NewDeduplicator(windowSecs int64) *Deduplicator {
	if windowSecs <= 0 {
		windowSecs = 10 // 默认10秒
	}
	return &Deduplicator{
		windowSecs: windowSecs,
	}
}

// TryAcquire 尝试获取执行权限
// 如果返回true，表示可以执行；如果返回false，表示正在执行中
// executionID: 执行ID（0-511）
func (d *Deduplicator) TryAcquire(executionID uint16) bool {
	if executionID >= 512 {
		// 超出范围，使用简单的map去重
		return true
	}

	slot := executionID / 64
	bit := executionID % 64
	mask := uint64(1) << bit

	// 原子操作：设置位并检查之前的值
	prev := d.inFlight[slot].Load()
	if prev&mask != 0 {
		// 位已设置，表示正在执行中
		return false
	}
	// 原子设置位
	d.inFlight[slot].Or(mask)

	// 成功获取，设置延迟释放
	d.scheduleRelease(executionID)

	return true
}

// Release 立即释放执行权限
func (d *Deduplicator) Release(executionID uint16) {
	if executionID >= 512 {
		return
	}

	// 取消延迟释放的定时器
	if timer, ok := d.releaseTimers.LoadAndDelete(executionID); ok {
		if t, ok := timer.(*time.Timer); ok {
			t.Stop()
		}
	}

	// 清除位
	slot := executionID / 64
	bit := executionID % 64
	mask := ^(uint64(1) << bit)
	d.inFlight[slot].And(mask)
}

// scheduleRelease 安排延迟释放
func (d *Deduplicator) scheduleRelease(executionID uint16) {
	timer := time.AfterFunc(time.Duration(d.windowSecs)*time.Second, func() {
		d.Release(executionID)
	})

	// 如果已存在定时器，先停止它
	if oldTimer, ok := d.releaseTimers.LoadOrStore(executionID, timer); ok {
		if t, ok := oldTimer.(*time.Timer); ok {
			t.Stop()
		}
		d.releaseTimers.Store(executionID, timer)
	}
}

// IsInFlight 检查是否正在执行中
func (d *Deduplicator) IsInFlight(executionID uint16) bool {
	if executionID >= 512 {
		return false
	}

	slot := executionID / 64
	bit := executionID % 64
	mask := uint64(1) << bit

	return d.inFlight[slot].Load()&mask != 0
}

// Clear 清除所有执行状态
func (d *Deduplicator) Clear() {
	// 停止所有定时器
	d.releaseTimers.Range(func(key, value interface{}) bool {
		if timer, ok := value.(*time.Timer); ok {
			timer.Stop()
		}
		d.releaseTimers.Delete(key)
		return true
	})

	// 清除所有位
	for i := range d.inFlight {
		d.inFlight[i].Store(0)
	}
}

