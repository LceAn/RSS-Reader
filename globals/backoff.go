package globals

import (
	"sync"
	"time"
)

const (
	// backoffThreshold 是连续失败达到该次数后开始退避。
	backoffThreshold = 3
	// backoffCapMinutes 限制单源最长退避时间。
	backoffCapMinutes = 60 * time.Minute
)

type feedBackoff struct {
	consecutiveFailures int
	until               time.Time
}

var (
	backoffMu sync.Mutex
	backoffs  = make(map[string]*feedBackoff)
)

// RecordFetchSuccess 清除该订阅源的失败计数，恢复正常抓取节奏。
func RecordFetchSuccess(feedURL string) {
	backoffMu.Lock()
	delete(backoffs, feedURL)
	backoffMu.Unlock()
}

// RecordFetchFailure 累计连续失败次数。达到阈值后按
// refreshInterval * (failures - threshold + 1) 计算下次允许抓取的时间，封顶 60 分钟。
func RecordFetchFailure(feedURL string, refreshInterval time.Duration, now time.Time) {
	backoffMu.Lock()
	defer backoffMu.Unlock()

	entry, exists := backoffs[feedURL]
	if !exists {
		entry = &feedBackoff{}
		backoffs[feedURL] = entry
	}
	entry.consecutiveFailures++
	if entry.consecutiveFailures < backoffThreshold {
		return
	}

	delay := refreshInterval * time.Duration(entry.consecutiveFailures-backoffThreshold+1)
	if delay > backoffCapMinutes {
		delay = backoffCapMinutes
	}
	entry.until = now.Add(delay)
}

// ShouldFetch 报告该订阅源当前是否应当抓取：退避期内的源返回 false。
func ShouldFetch(feedURL string, now time.Time) bool {
	backoffMu.Lock()
	defer backoffMu.Unlock()

	entry, exists := backoffs[feedURL]
	if !exists {
		return true
	}
	return !now.Before(entry.until)
}

// ResetFeedBackoff 清空全部退避状态。配置变更触发强制刷新时调用，
// 让被移除或新加入的订阅立即按正常节奏参与抓取。
func ResetFeedBackoff() {
	backoffMu.Lock()
	backoffs = make(map[string]*feedBackoff)
	backoffMu.Unlock()
}

// BackoffCount 返回当前处于退避期内的订阅源数量，用于状态展示。
func BackoffCount(now time.Time) int {
	backoffMu.Lock()
	defer backoffMu.Unlock()

	count := 0
	for _, entry := range backoffs {
		if now.Before(entry.until) {
			count++
		}
	}
	return count
}
