package globals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotificationHistoryNewestFirst(t *testing.T) {
	InitNotificationHistory("")
	AppendNotification(NotificationRecord{Time: time.Now(), Feed: "a", Title: "first", Success: true})
	AppendNotification(NotificationRecord{Time: time.Now(), Feed: "a", Title: "second", Success: false, Detail: "boom"})

	records := NotificationHistory(0)
	if len(records) != 2 {
		t.Fatalf("记录数 = %d, 期望 2", len(records))
	}
	if records[0].Title != "second" || records[1].Title != "first" {
		t.Fatalf("排序错误: [%s, %s]", records[0].Title, records[1].Title)
	}
	if records[0].Success || records[0].Detail != "boom" {
		t.Fatalf("最新记录字段错误: %#v", records[0])
	}
}

func TestNotificationHistoryMemoryCap(t *testing.T) {
	InitNotificationHistory("")
	for i := 0; i < notificationHistoryMemory+5; i++ {
		AppendNotification(NotificationRecord{Time: time.Now(), Title: "item"})
	}
	if got := len(NotificationHistory(0)); got != notificationHistoryMemory {
		t.Fatalf("内存记录数 = %d, 期望 %d", got, notificationHistoryMemory)
	}
}

func TestNotificationHistoryPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.jsonl")
	InitNotificationHistory(path)

	AppendNotification(NotificationRecord{Time: time.Now(), Feed: "feed", Title: "persisted", Link: "https://example.com/a", Success: true})
	AppendNotification(NotificationRecord{Time: time.Now(), Feed: "feed", Title: "persisted-2", Success: false, Detail: "err"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取历史文件失败: %v", err)
	}
	if strings.Count(string(data), "\n") != 2 {
		t.Fatalf("文件行数错误:\n%s", data)
	}

	// 模拟重启：重新加载同一文件。
	InitNotificationHistory(path)
	records := NotificationHistory(1)
	if len(records) != 1 || records[0].Title != "persisted-2" {
		t.Fatalf("重启后加载错误: %#v", records)
	}

	// 清理全局状态，避免影响其他测试。
	InitNotificationHistory("")
}
