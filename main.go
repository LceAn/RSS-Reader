package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"rss-reader/globals"
	"rss-reader/models"

	"rss-reader/utils"
	"time"

	"github.com/gorilla/websocket"
)

func init() {
	globals.Init()
}

func main() {

	go utils.UpdateFeeds()
	go utils.WatchConfigFileChanges("config.json")
	http.HandleFunc("/feeds", getFeedsHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/api/config", configHandler) // 新增路由处理函数
	// http.HandleFunc("/", serveHome)
	http.HandleFunc("/", tplHandler)

	//加载静态文件
	fs := http.FileServer(http.FS(globals.DirStatic))
	http.Handle("/static/", fs)
	port := globals.RssUrls.Port
	serve := fmt.Sprintf("%s%d", ":", port)
	utils.System("服务启动中...")
    utils.System("网页监听地址为: http://localhost:%d", port)
    utils.System("按 CTRL+C 退出程序.")

	if err := http.ListenAndServe(serve, nil); err != nil {
		utils.NewLogger("SYSTEM").Fatal("启动服务器失败: %v", err)
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.Write(globals.HtmlContent)
}

func tplHandler(w http.ResponseWriter, r *http.Request) {
	// 创建一个新的模板，并设置自定义分隔符为<< >>，避免与Vue的语法冲突
	tmplInstance := template.New("index.html").Delims("<<", ">>")
	//添加加法函数计数
	funcMap := template.FuncMap{
		"inc": func(i int) int {
			return i + 1
		},
	}
	// 加载模板文件
	tmpl, err := tmplInstance.Funcs(funcMap).ParseFS(globals.DirStatic, "static/index.html")
	if err != nil {
		utils.NewLogger("SYSTEM").Error("模板加载错误:", err)
		return
	}

	// 定义一个数据对象
	data := struct {
		Keywords                string
		RssDataList             []models.Feed
		AutoUpdatePush          int
		ListHeight              int
		WebTitle                string
		WebDes                  string
		Github_project_url      string
		Github_project_url_name string
		Github_author_url       string
		Github_author_url_name  string
	}{
		Keywords:                getKeywords(),
		RssDataList:             utils.GetFeeds(),
		AutoUpdatePush:          globals.RssUrls.AutoUpdatePush,
		ListHeight:              globals.RssUrls.ListHeight,
		WebTitle:                globals.RssUrls.WebTitle,
		WebDes:                  globals.RssUrls.WebDes,
		Github_project_url:      globals.RssUrls.Github_project_url,
		Github_project_url_name: globals.RssUrls.Github_project_url_name,
		Github_author_url:       globals.RssUrls.Github_author_url,
		Github_author_url_name:  globals.RssUrls.Github_author_url_name,
	}

	// 渲染模板并将结果写入响应
	err = tmpl.Execute(w, data)
	if err != nil {
		utils.NewLogger("SYSTEM").Error("模板渲染错误:", err)
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := globals.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.NewLogger("SYSTEM").Error("Upgrade failed: %v", err)
		return
	}

	defer conn.Close()
	for {
		for _, url := range globals.RssUrls.Values {
			globals.Lock.RLock()
			cache, ok := globals.DbMap[url]
			globals.Lock.RUnlock()
			if !ok {
				utils.NewLogger("SYSTEM").Error("Error getting feed from db is null %v", url)
				continue
			}
			data, err := json.Marshal(cache)
			if err != nil {
				utils.NewLogger("SYSTEM").Error("json marshal failure: %s", err.Error())
				continue
			}

			err = conn.WriteMessage(websocket.TextMessage, data)
			//错误直接关闭更新
			if err != nil {
				utils.NewLogger("SYSTEM").Error("Error sending message or Connection closed: %v", err)
				return
			}
		}
		//如果未配置则不自动更新
		if globals.RssUrls.AutoUpdatePush == 0 {
			return
		}
		time.Sleep(time.Duration(globals.RssUrls.AutoUpdatePush) * time.Minute)
	}
}

// 获取关键词也就是title
// 获取feeds列表
func getKeywords() string {
	words := ""
	for _, url := range globals.RssUrls.Values {
		globals.Lock.RLock()
		cache, ok := globals.DbMap[url]
		globals.Lock.RUnlock()
		if !ok {
			utils.NewLogger("SYSTEM").Error("Error getting feed from db is null %v", url)
			continue
		}
		if cache.Title != "" {
			words += cache.Title + ","
		}
	}
	return words
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
	// 更新配置逻辑，可能需要验证更新内容的有效性
	globals.RssUrls = config
	// 可以添加保存到文件的逻辑
	w.WriteHeader(http.StatusOK)
}
