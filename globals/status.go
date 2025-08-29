// in globals/status.go

package globals

// SystemStatus 定义了要推送到前端的系统状态信息
type SystemStatus struct {
	Type          string `json:"type"` // "status"
	TotalFeeds    int    `json:"totalFeeds"`
	Successful    int    `json:"successful"`
	Failed        int    `json:"failed"`
	NextRefreshIn int    `json:"nextRefreshIn"` // 倒计时，单位：秒
}

// FailedFeedCount 使用原子操作来保证并发安全
var FailedFeedCount int32