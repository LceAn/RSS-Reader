// in globals/status.go

package globals

import (
	"sync/atomic"
	"time"
)

// SystemStatus 定义了要推送到前端的系统状态信息
type SystemStatus struct {
	Type          string `json:"type"` // "status"
	TotalFeeds    int    `json:"totalFeeds"`
	Successful    int    `json:"successful"`
	Failed        int    `json:"failed"`
	Refreshing    bool   `json:"refreshing"`
	LastRefreshAt int64  `json:"lastRefreshAt,omitempty"`
	NextRefreshIn int    `json:"nextRefreshIn"` // 倒计时，单位：秒
}

type FetchSnapshot struct {
	Successful    int
	Failed        int
	Refreshing    bool
	Completed     bool
	LastRefreshAt int64
}

var (
	successfulFeedCount int32
	failedFeedCount     int32
	refreshing          int32
	fetchCompleted      int32
	lastRefreshUnix     int64
	nextRefreshUnix     int64
)

func BeginFetchCycle() {
	atomic.StoreInt32(&refreshing, 1)
}

func FinishFetchCycle(successful, failed int) {
	atomic.StoreInt32(&successfulFeedCount, int32(successful))
	atomic.StoreInt32(&failedFeedCount, int32(failed))
	atomic.StoreInt64(&lastRefreshUnix, time.Now().Unix())
	atomic.StoreInt32(&fetchCompleted, 1)
	atomic.StoreInt32(&refreshing, 0)
}

func GetFetchSnapshot() FetchSnapshot {
	return FetchSnapshot{
		Successful:    int(atomic.LoadInt32(&successfulFeedCount)),
		Failed:        int(atomic.LoadInt32(&failedFeedCount)),
		Refreshing:    atomic.LoadInt32(&refreshing) == 1,
		Completed:     atomic.LoadInt32(&fetchCompleted) == 1,
		LastRefreshAt: atomic.LoadInt64(&lastRefreshUnix),
	}
}

func SetNextRefreshTime(next time.Time) {
	if next.IsZero() {
		atomic.StoreInt64(&nextRefreshUnix, 0)
		return
	}
	atomic.StoreInt64(&nextRefreshUnix, next.Unix())
}

func NextRefreshIn() int {
	next := atomic.LoadInt64(&nextRefreshUnix)
	if next == 0 {
		return 0
	}
	remaining := int(time.Until(time.Unix(next, 0)).Seconds())
	if remaining < 0 {
		return 0
	}
	return remaining
}
