package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"rss-reader/globals"
	"rss-reader/models"
	"rss-reader/utils"
	"strings"
	"time"
)

const maximumOPMLBodyBytes = 512 << 10

type opmlOutline struct {
	Text     string        `xml:"text,attr,omitempty"`
	Title    string        `xml:"title,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline"`
}

type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title       string `xml:"title,omitempty"`
		DateCreated string `xml:"dateCreated,omitempty"`
	} `xml:"head"`
	Body struct {
		Outlines []opmlOutline `xml:"outline"`
	} `xml:"body"`
}

func (service *adminService) adminOPMLHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := service.authenticate(r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "请先登录")
		return
	}

	switch r.Method {
	case http.MethodGet:
		service.exportOPML(w)
	case http.MethodPost:
		if !requestHasSameOrigin(r) || !csrfMatches(r.Header.Get("X-CSRF-Token"), session.CSRFToken) {
			writeAPIError(w, http.StatusForbidden, "安全校验失败")
			return
		}
		service.importOPML(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

// exportOPML 把当前订阅列表导出为 OPML 2.0。标题优先取运行时已抓取的 Feed 标题，
// 尚未抓取的订阅以 URL 作为占位标题。
func (service *adminService) exportOPML(w http.ResponseWriter) {
	service.configMu.Lock()
	fileConfig, _, _, err := loadConfigDocument(models.ConfigPath())
	service.configMu.Unlock()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "无法读取配置")
		return
	}

	doc := opmlDocument{Version: "2.0"}
	doc.Head.Title = globals.GetConfig().WebTitle
	doc.Head.DateCreated = time.Now().UTC().Format(time.RFC1123Z)

	for _, feedURL := range fileConfig.Values {
		title := feedURL
		if feed, ok := globals.GetFeed(feedURL); ok && strings.TrimSpace(feed.Title) != "" {
			title = strings.TrimSpace(feed.Title)
		}
		doc.Body.Outlines = append(doc.Body.Outlines, opmlOutline{
			Text:   title,
			Type:   "rss",
			XMLURL: feedURL,
		})
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "生成 OPML 失败")
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="rss-reader.opml"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

type opmlImportResult struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Total    int      `json:"total"`
	Feeds    []string `json:"feeds,omitempty"`
}

// importOPML 解析上传的 OPML，取其中所有带 xmlUrl 的 outline（含分组内嵌套项），
// 与现有订阅去重后追加，并按 putEditableConfig 相同的事务模式持久化。
func (service *adminService) importOPML(w http.ResponseWriter, r *http.Request) {
	expectedRevision := strings.TrimSpace(r.Header.Get("If-Match"))
	if expectedRevision == "" {
		writeAPIError(w, http.StatusPreconditionRequired, "缺少配置版本")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maximumOPMLBodyBytes+1))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "读取上传内容失败")
		return
	}
	if len(body) > maximumOPMLBodyBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "OPML 文件过大")
		return
	}

	var doc opmlDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		writeAPIError(w, http.StatusBadRequest, "OPML 解析失败")
		return
	}
	incoming := collectOPMLFeedURLs(doc.Body.Outlines)
	if len(incoming) == 0 {
		writeAPIError(w, http.StatusBadRequest, "OPML 中没有可识别的订阅")
		return
	}

	service.configMu.Lock()
	defer service.configMu.Unlock()

	fileConfig, _, revision, err := loadConfigDocument(models.ConfigPath())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "无法读取配置")
		return
	}
	if expectedRevision != quoteETag(revision) {
		writeAPIError(w, http.StatusPreconditionFailed, "配置已在其他位置更新，请重新加载")
		return
	}

	existing := make(map[string]struct{}, len(fileConfig.Values))
	for _, value := range fileConfig.Values {
		existing[value] = struct{}{}
	}

	var added []string
	seen := make(map[string]struct{}, len(incoming))
	skipped := 0
	for _, raw := range incoming {
		feedURL := strings.TrimSpace(raw)
		if len(feedURL) > maximumFeedURLLength {
			skipped++
			continue
		}
		parsed, err := url.Parse(feedURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			skipped++
			continue
		}
		if _, dup := seen[feedURL]; dup {
			skipped++
			continue
		}
		seen[feedURL] = struct{}{}
		if _, dup := existing[feedURL]; dup {
			skipped++
			continue
		}
		if len(fileConfig.Values)+len(added) >= maximumEditableFeeds {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("订阅数量已达上限 %d，本次未保存", maximumEditableFeeds))
			return
		}
		added = append(added, feedURL)
	}

	originalConfig := fileConfig.Clone()
	fileConfig.Values = append(fileConfig.Values, added...)

	runtimeCandidate := fileConfig.Clone()
	if err := runtimeCandidate.ApplyEnvironment(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "环境变量配置无效")
		return
	}
	if err := runtimeCandidate.Normalize(); err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("配置无效: %v", err))
		return
	}
	fileConfig.Values = append([]string(nil), runtimeCandidate.Values...)

	configPath := models.ConfigPath()
	if err := models.WriteConfigFileAtomic(configPath, &fileConfig); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "配置文件不可写")
		return
	}
	if err := globals.ReloadConfig(); err != nil {
		_ = models.WriteConfigFileAtomic(configPath, &originalConfig)
		_ = globals.ReloadConfig()
		writeAPIError(w, http.StatusInternalServerError, "配置加载失败，已恢复原配置")
		return
	}
	utils.TriggerFeedRefresh()

	response := opmlImportResult{Imported: len(added), Skipped: skipped, Total: len(fileConfig.Values)}
	if len(added) > 0 {
		response.Feeds = added
	}
	writeJSON(w, http.StatusOK, response)
}

// collectOPMLFeedURLs 递归收集所有带 xmlUrl 的 outline。
func collectOPMLFeedURLs(outlines []opmlOutline) []string {
	var urls []string
	var walk func(items []opmlOutline)
	walk = func(items []opmlOutline) {
		for _, item := range items {
			if strings.TrimSpace(item.XMLURL) != "" {
				urls = append(urls, item.XMLURL)
			}
			if len(item.Outlines) > 0 {
				walk(item.Outlines)
			}
		}
	}
	walk(outlines)
	return urls
}
