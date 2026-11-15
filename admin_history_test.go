package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"rss-reader/globals"
	"testing"
	"time"
)

func TestAdminHistoryRequiresLogin(t *testing.T) {
	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	service.adminHistoryHandler(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/history", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d, 期望 401", recorder.Code)
	}
}

func TestAdminHistoryReturnsNewestFirst(t *testing.T) {
	globals.InitNotificationHistory("")
	defer globals.InitNotificationHistory("")
	globals.AppendNotification(globals.NotificationRecord{Time: time.Now(), Feed: "feed", Title: "first", Success: true})
	globals.AppendNotification(globals.NotificationRecord{Time: time.Now(), Feed: "feed", Title: "second", Success: false, Detail: "boom"})

	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	cookie, _ := loginForTest(t, service)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/history?limit=1", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	service.adminHistoryHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", recorder.Code)
	}

	var response struct {
		Records []globals.NotificationRecord `json:"records"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(response.Records) != 1 || response.Records[0].Title != "second" {
		t.Fatalf("limit=1 应只返回最新记录: %#v", response.Records)
	}
}
