package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"rss-reader/globals"
	"rss-reader/models"
	"rss-reader/utils"
	"strings"
	"sync/atomic"
	"time"
)

func init() {
	globals.Init()
}

func main() {
	go utils.UpdateFeeds()
	go utils.WatchConfigFileChanges("config.json")

	http.HandleFunc("/", tplHandler)
	http.Handle("/static/", http.FileServer(http.FS(globals.DirStatic)))
	http.HandleFunc("/feeds", getFeedsHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/api/config", configHandler)

	port := globals.RssUrls.Port
	listenAddress := fmt.Sprintf(":%d", port)

	utils.System("服务启动中...")
	utils.System("网页监听地址为: http://localhost:%d", port)
	utils.System("按 CTRL+C 退出程序.")

	if err := http.ListenAndServe(listenAddress, nil); err != nil {
		utils.NewLogger("SYSTEM").Fatal("启动服务器失败: %v", err)
	}
}

func tplHandler(w http.ResponseWriter, r *http.Request) {
	tmplInstance := template.New("index.html").Delims("<<", ">>")
	funcMap := template.FuncMap{
		"inc": func(i int) int {
			return i + 1
		},
	}
	tmpl, err := tmplInstance.Funcs(funcMap).ParseFS(globals.DirStatic, "static/index.html")
	if err != nil {
		utils.NewLogger("SYSTEM").Error("模板加载错误: %v", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	data := struct {
		Keywords              string
		RssDataList           []models.Feed
		AutoUpdatePush        int
		ListHeight            int
		WebTitle              string
		WebDes                string
		Github_project_url      string
		Github_project_url_name string
		Github_author_url       string
		Github_author_url_name  string
	}{
		Keywords:              getKeywords(),
		RssDataList:           utils.GetFeeds(),
		AutoUpdatePush:        globals.RssUrls.AutoUpdatePush,
		ListHeight:            globals.RssUrls.ListHeight,
		WebTitle:              globals.RssUrls.WebTitle,
		WebDes:                globals.RssUrls.WebDes,
		Github_project_url:      globals.RssUrls.Github_project_url,
		Github_project_url_name: globals.RssUrls.Github_project_url_name,
		Github_author_url:       globals.RssUrls.Github_author_url,
		Github_author_url_name:  globals.RssUrls.Github_author_url_name,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		utils.NewLogger("SYSTEM").Error("模板渲染错误: %v", err)
	}
}

func getKeywords() string {
	var words []string
	for _, url := range globals.RssUrls.Values {
		globals.Lock.RLock()
		cache, ok := globals.DbMap[url]
		globals.Lock.RUnlock()
		if !ok {
			continue
		}
		if cache.Title != "" {
			words = append(words, cache.Title)
		}
	}
	return strings.Join(words, ",")
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := globals.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.NewLogger("WEBSOCKET").Error("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	wsLogger := utils.NewLogger("WEBSOCKET")
	wsLogger.Info("Connection opened from %s", r.RemoteAddr)

	// --- 修正开始 ---

	// 1. 立即发送一次初始 Feed 数据
	initialFeeds := utils.GetFeeds()
	if len(initialFeeds) > 0 {
		if err := conn.WriteJSON(map[string]interface{}{
			"type": "feeds",
			"data": initialFeeds,
		}); err != nil {
			wsLogger.Warn("Failed to write initial feeds JSON: %v. Closing connection.", err)
			return
		}
	}
	lastSentFeeds := initialFeeds

	// --- 修正结束 ---

	statusTicker := time.NewTicker(1 * time.Second)
	defer statusTicker.Stop()

	refreshDuration := time.Duration(globals.RssUrls.AutoUpdatePush) * time.Minute
	if globals.RssUrls.AutoUpdatePush == 0 {
		refreshDuration = 24 * time.Hour
	}
	feedUpdateTicker := time.NewTicker(refreshDuration)
	defer feedUpdateTicker.Stop()

	nextRefreshTime := time.Now().Add(refreshDuration)

	for {
		select {
		case <-statusTicker.C:
			totalFeeds := len(globals.RssUrls.Values)
			failedCount := int(atomic.LoadInt32(&globals.FailedFeedCount))
			successfulCount := totalFeeds - failedCount
			if successfulCount < 0 {
				successfulCount = 0
			}
			remainingSeconds := int(time.Until(nextRefreshTime).Seconds())
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}
			status := globals.SystemStatus{
				Type:          "status",
				TotalFeeds:    totalFeeds,
				Successful:    successfulCount,
				Failed:        failedCount,
				NextRefreshIn: remainingSeconds,
			}
			if err := conn.WriteJSON(status); err != nil {
				wsLogger.Warn("Failed to write status JSON: %v. Closing connection.", err)
				return
			}

		case <-feedUpdateTicker.C:
			if globals.RssUrls.AutoUpdatePush > 0 {
				nextRefreshTime = time.Now().Add(refreshDuration)
				currentFeeds := utils.GetFeeds()

				if len(currentFeeds) > 0 && !feedsAreEqual(lastSentFeeds, currentFeeds) {
					if err := conn.WriteJSON(map[string]interface{}{
						"type": "feeds",
						"data": currentFeeds,
					}); err != nil {
						wsLogger.Warn("Failed to write feeds JSON: %v. Closing connection.", err)
						return
					}
					lastSentFeeds = currentFeeds
				}
			}
		}
	}
}

func feedsAreEqual(a, b []models.Feed) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Link != b[i].Link || len(a[i].Items) != len(b[i].Items) {
			return false
		}
		if len(a[i].Items) > 0 && len(b[i].Items) > 0 {
			if a[i].Items[0].Link != b[i].Items[0].Link {
				return false
			}
		}
	}
	return true
}

func getFeedsHandler(w http.ResponseWriter, r *http.Request) {
	feeds := utils.GetFeeds()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(feeds)
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		handleConfigGet(w, r)
	case "POST":
		handleConfigPost(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleConfigGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(globals.RssUrls)
	if err != nil {
		http.Error(w, "Error marshaling config", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func handleConfigPost(w http.ResponseWriter, r *http.Request) {
	var config models.Config
	err := json.NewDecoder(r.Body).Decode(&config)
	if err != nil {
		http.Error(w, "Error decoding config", http.StatusBadRequest)
		return
	}
	globals.RssUrls = config
	w.WriteHeader(http.StatusOK)
}