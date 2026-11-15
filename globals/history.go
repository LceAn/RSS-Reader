package globals

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// NotificationRecord 描述一次关键词命中后的通知投递结果。
type NotificationRecord struct {
	Time    time.Time `json:"time"`
	Feed    string    `json:"feed"`
	Title   string    `json:"title"`
	Link    string    `json:"link,omitempty"`
	Success bool      `json:"success"`
	Detail  string    `json:"detail,omitempty"`
}

// notificationHistoryMemory 限制内存中保留的最近记录数，历史全文仍在 JSONL 文件中。
const notificationHistoryMemory = 200

var (
	historyMu   sync.Mutex
	historyPath string
	history     []NotificationRecord
)

// InitNotificationHistory 设置推送历史落盘文件，并把文件中最近的记录加载进内存。
// 文件不存在视为首次运行，不报错。
func InitNotificationHistory(path string) {
	historyMu.Lock()
	defer historyMu.Unlock()

	historyPath = path
	history = nil

	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		var record NotificationRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		history = append(history, record)
		if len(history) > notificationHistoryMemory {
			history = history[len(history)-notificationHistoryMemory:]
		}
	}
}

// AppendNotification 记录一条推送结果：内存保留最近 200 条，并整行追加到 JSONL 文件。
func AppendNotification(record NotificationRecord) {
	historyMu.Lock()
	defer historyMu.Unlock()

	history = append(history, record)
	if len(history) > notificationHistoryMemory {
		history = history[len(history)-notificationHistoryMemory:]
	}

	if historyPath == "" {
		return
	}
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	file, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(line, '\n'))
}

// NotificationHistory 返回最近 limit 条记录，按新到旧排序。
func NotificationHistory(limit int) []NotificationRecord {
	historyMu.Lock()
	defer historyMu.Unlock()

	if limit <= 0 || limit > len(history) {
		limit = len(history)
	}
	out := make([]NotificationRecord, 0, limit)
	for i := len(history) - 1; i >= len(history)-limit; i-- {
		out = append(out, history[i])
	}
	return out
}
