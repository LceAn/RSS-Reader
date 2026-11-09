package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"rss-reader/globals"
	"rss-reader/models"
	"rss-reader/utils"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	globals.Init()
	if err := run(); err != nil {
		utils.NewLogger("SYSTEM").Error("服务异常退出: %v", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	admin, err := newAdminServiceFromEnvironment()
	if err != nil {
		return fmt.Errorf("configure admin access: %w", err)
	}

	configPath := models.ConfigPath()
	var background sync.WaitGroup
	background.Add(2)
	go func() {
		defer background.Done()
		utils.UpdateFeeds(ctx)
	}()
	go func() {
		defer background.Done()
		utils.WatchConfigFileChanges(ctx, configPath)
	}()

	mux := newHandler(ctx, admin)
	config := globals.GetConfig()
	listenAddress := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	utils.System("服务启动中...")
	utils.System("网页监听地址为: http://%s", listenAddress)
	utils.System("按 CTRL+C 退出程序.")

	serverErrors := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	var runError error
	select {
	case runError = <-serverErrors:
		stop()
	case <-ctx.Done():
		utils.System("正在安全关闭服务...")
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil && runError == nil {
		runError = fmt.Errorf("shutdown HTTP server: %w", err)
	}
	background.Wait()
	if err := utils.WaitForNotifications(shutdownContext); err != nil && runError == nil {
		runError = fmt.Errorf("wait for notifications: %w", err)
	}
	return runError
}

func newHandler(ctx context.Context, admin *adminService) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", tplHandler)
	mux.Handle("/static/", cacheRevalidator(staticETag, http.FileServer(http.FS(globals.DirStatic))))
	mux.HandleFunc("/feeds", getFeedsHandler)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandler(ctx, w, r)
	})
	mux.HandleFunc("/api/config", configHandler)
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", readyHandler)
	if admin == nil {
		admin, _ = newAdminService("")
	}
	admin.register(mux)
	return mux
}

// wsWriteTimeout bounds each websocket frame write so a stalled client cannot
// block the per-connection push loop forever.
const wsWriteTimeout = 10 * time.Second

// indexTemplate parses once at startup: the page template ships inside the
// binary and never changes while the process is running.
var indexTemplate = template.Must(
	template.New("index.html").Delims("<<", ">>").
		Funcs(template.FuncMap{
			// xmltime renders the local "2006-01-02 15:04:05" stamp as an
			// HTML5 <time datetime> value.
			"xmltime": func(stamp string) string {
				return strings.ReplaceAll(stamp, " ", "T")
			},
		}).
		ParseFS(globals.DirStatic, "static/index.html"),
)

// staticETag changes on every restart so browsers revalidate after an upgrade,
// while revalidation within one process lifetime resolves to 304.
var staticETag = func() string {
	summary := sha256.Sum256([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	return `"` + hex.EncodeToString(summary[:8]) + `"`
}()

func tplHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	config := globals.GetConfig()

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
		AutoUpdatePush:          config.AutoUpdatePush,
		ListHeight:              config.ListHeight,
		WebTitle:                config.WebTitle,
		WebDes:                  config.WebDes,
		Github_project_url:      config.Github_project_url,
		Github_project_url_name: config.Github_project_url_name,
		Github_author_url:       config.Github_author_url,
		Github_author_url_name:  config.Github_author_url_name,
	}

	if err := indexTemplate.Execute(w, data); err != nil {
		utils.NewLogger("SYSTEM").Error("模板渲染错误: %v", err)
	}
}

func getKeywords() string {
	var words []string
	config := globals.GetConfig()
	for _, url := range config.Values {
		cache, ok := globals.GetFeed(url)
		if !ok {
			continue
		}
		if cache.Title != "" {
			words = append(words, cache.Title)
		}
	}
	return strings.Join(words, ",")
}

func wsHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	conn, err := globals.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.NewLogger("WEBSOCKET").Error("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	wsLogger := utils.NewLogger("WEBSOCKET")
	wsLogger.Info("Connection opened from %s", r.RemoteAddr)

	initialFeeds := utils.GetFeeds()
	if len(initialFeeds) > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		if err := conn.WriteJSON(map[string]interface{}{
			"type": "feeds",
			"data": trimFeeds(initialFeeds),
		}); err != nil {
			wsLogger.Warn("Failed to write initial feeds JSON: %v. Closing connection.", err)
			return
		}
	}
	lastSentFeeds := initialFeeds
	initialFeedsSent := len(initialFeeds) > 0
	lastSentVersion := globals.FeedVersion()

	statusTicker := time.NewTicker(1 * time.Second)
	defer statusTicker.Stop()

	config := globals.GetConfig()
	pushMinutes := config.AutoUpdatePush
	nextFeedPush := nextPushTime(pushMinutes)

	for {
		select {
		case <-ctx.Done():
			return
		case <-statusTicker.C:
		}
		config = globals.GetConfig()
		if config.AutoUpdatePush != pushMinutes {
			pushMinutes = config.AutoUpdatePush
			nextFeedPush = nextPushTime(pushMinutes)
		}

		totalFeeds := len(config.Values)
		fetchStatus := globals.GetFetchSnapshot()
		status := globals.SystemStatus{
			Type:          "status",
			TotalFeeds:    totalFeeds,
			Successful:    fetchStatus.Successful,
			Failed:        fetchStatus.Failed,
			Refreshing:    fetchStatus.Refreshing,
			LastRefreshAt: fetchStatus.LastRefreshAt,
			NextRefreshIn: globals.NextRefreshIn(),
		}
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		if err := conn.WriteJSON(status); err != nil {
			wsLogger.Warn("Failed to write status JSON: %v. Closing connection.", err)
			return
		}

		currentVersion := globals.FeedVersion()
		versionChanged := currentVersion != lastSentVersion
		shouldSendInitial := !initialFeedsSent && versionChanged
		shouldCheckUpdate := pushMinutes > 0 && !time.Now().Before(nextFeedPush)
		if shouldSendInitial || (shouldCheckUpdate && versionChanged) {
			currentFeeds := utils.GetFeeds()
			if len(currentFeeds) > 0 && !feedsAreEqual(lastSentFeeds, currentFeeds) {
				_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				if err := conn.WriteJSON(map[string]interface{}{
					"type": "feeds",
					"data": trimFeeds(currentFeeds),
				}); err != nil {
					wsLogger.Warn("Failed to write feeds JSON: %v. Closing connection.", err)
					return
				}
				lastSentFeeds = currentFeeds
			}
			initialFeedsSent = len(currentFeeds) > 0
			lastSentVersion = currentVersion
		}
		if shouldCheckUpdate {
			nextFeedPush = nextPushTime(pushMinutes)
		}
	}
}

func nextPushTime(minutes int) time.Time {
	if minutes <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(minutes) * time.Minute)
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

// feedItemPayload strips the Description field from the wire format:
// descriptions can be large and the UI never renders them.
type feedItemPayload struct {
	Title string `json:"title"`
	Link  string `json:"link"`
}

type feedPayload struct {
	Title  string            `json:"title,omitempty"`
	Link   string            `json:"link"`
	Custom map[string]string `json:"custom,omitempty"`
	Items  []feedItemPayload `json:"items,omitempty"`
}

func trimFeeds(feeds []models.Feed) []feedPayload {
	out := make([]feedPayload, 0, len(feeds))
	for _, feed := range feeds {
		items := make([]feedItemPayload, 0, len(feed.Items))
		for _, item := range feed.Items {
			items = append(items, feedItemPayload{Title: item.Title, Link: item.Link})
		}
		out = append(out, feedPayload{Title: feed.Title, Link: feed.Link, Custom: feed.Custom, Items: items})
	}
	return out
}

func getFeedsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	feeds := utils.GetFeeds()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(trimFeeds(feeds)); err != nil {
		utils.NewLogger("HTTP").Warn("Failed to encode feeds: %v", err)
	}
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	handleConfigGet(w, r)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	status := globals.GetFetchSnapshot()
	if !status.Completed {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func handleConfigGet(w http.ResponseWriter, r *http.Request) {
	type notifyStatus struct {
		FeiShu   bool `json:"feishu"`
		Telegram bool `json:"telegram"`
		Dingtalk bool `json:"dingtalk"`
	}
	type publicConfig struct {
		Host           string       `json:"host"`
		Port           int          `json:"port"`
		Refresh        int          `json:"refresh"`
		AutoUpdatePush int          `json:"autoUpdatePush"`
		ListHeight     int          `json:"listHeight"`
		Keywords       []string     `json:"keywords"`
		Feeds          []string     `json:"feeds"`
		Notify         notifyStatus `json:"notify"`
	}

	config := globals.GetConfig()
	view := publicConfig{
		Host:           config.Host,
		Port:           config.Port,
		Refresh:        config.ReFresh,
		AutoUpdatePush: config.AutoUpdatePush,
		ListHeight:     config.ListHeight,
		Keywords:       append(make([]string, 0, len(config.Keywords)), config.Keywords...),
		Feeds:          publicFeedHosts(config.Values),
		Notify: notifyStatus{
			FeiShu:   config.Notify.FeiShu.API != "",
			Telegram: config.Notify.Telegram.API != "" && config.Notify.Telegram.Token != "" && config.Notify.Telegram.ChatId != "",
			Dingtalk: config.Notify.Dingtalk.Webhook != "",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(view); err != nil {
		utils.NewLogger("HTTP").Warn("Failed to encode public config: %v", err)
	}
}

func publicFeedHosts(values []string) []string {
	hosts := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			continue
		}
		host := parsed.Host
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

// cacheRevalidator serves embedded assets with an ETag that changes on every
// restart: clients revalidate cheaply (304) instead of caching stale files
// across upgrades.
func cacheRevalidator(etag string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		// The bundled Vue build compiles templates in the browser and therefore
		// requires unsafe-eval. All script origins remain restricted to self.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; img-src 'self' data:; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}
