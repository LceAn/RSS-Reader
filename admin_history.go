package main

import (
	"encoding/json"
	"net/http"
	"rss-reader/globals"
	"strconv"
)

const (
	historyDefaultLimit = 50
	historyMaximumLimit = 200
)

// adminHistoryHandler 返回最近的推送历史记录（新到旧），需要管理员会话。
// 记录内容只包含来源、标题、链接与投递结果，不含任何通知密钥。
func (service *adminService) adminHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := service.authenticate(r); !ok {
		writeAPIError(w, http.StatusUnauthorized, "请先登录")
		return
	}

	limit := historyDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= historyMaximumLimit {
			limit = parsed
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"records": globals.NotificationHistory(limit),
	})
}
