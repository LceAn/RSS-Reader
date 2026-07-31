package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"rss-reader/globals"
	"rss-reader/models"
	"rss-reader/utils"
	"strings"
)

const (
	maximumEditableFeeds      = 200
	maximumEditableKeywords   = 200
	maximumFeedURLLength      = 2048
	maximumKeywordLength      = 128
	maximumRefreshMinutes     = 10080
	minimumEditableListHeight = 240
	maximumEditableListHeight = 1600
)

type editableConfig struct {
	Feeds          []string `json:"feeds"`
	Keywords       []string `json:"keywords"`
	Refresh        int      `json:"refresh"`
	AutoUpdatePush int      `json:"autoUpdatePush"`
	ListHeight     int      `json:"listHeight"`
}

func (service *adminService) adminConfigHandler(w http.ResponseWriter, r *http.Request) {
	session, ok := service.authenticate(r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "请先登录")
		return
	}

	switch r.Method {
	case http.MethodGet:
		service.getEditableConfig(w)
	case http.MethodPut:
		if !requestHasSameOrigin(r) || !csrfMatches(r.Header.Get("X-CSRF-Token"), session.CSRFToken) {
			writeAPIError(w, http.StatusForbidden, "安全校验失败")
			return
		}
		service.putEditableConfig(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func (service *adminService) getEditableConfig(w http.ResponseWriter) {
	service.configMu.Lock()
	defer service.configMu.Unlock()

	_, runtimeConfig, revision, err := loadConfigDocument(models.ConfigPath())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "无法读取配置")
		return
	}
	w.Header().Set("ETag", quoteETag(revision))
	writeJSON(w, http.StatusOK, editableConfigFromModel(runtimeConfig))
}

func (service *adminService) putEditableConfig(w http.ResponseWriter, r *http.Request) {
	expectedRevision := strings.TrimSpace(r.Header.Get("If-Match"))
	if expectedRevision == "" {
		writeAPIError(w, http.StatusPreconditionRequired, "缺少配置版本")
		return
	}

	var update editableConfig
	if err := decodeJSON(w, r, 256<<10, &update); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := validateEditableConfig(update); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
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

	originalConfig := fileConfig.Clone()
	fileConfig.Values = append([]string(nil), update.Feeds...)
	fileConfig.Keywords = append([]string(nil), update.Keywords...)
	fileConfig.ReFresh = update.Refresh
	fileConfig.AutoUpdatePush = update.AutoUpdatePush
	fileConfig.ListHeight = update.ListHeight

	runtimeCandidate := fileConfig.Clone()
	if err := runtimeCandidate.ApplyEnvironment(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "环境变量配置无效")
		return
	}
	if err := runtimeCandidate.Normalize(); err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("配置无效: %v", err))
		return
	}
	if err := validateEditableConfig(editableConfigFromModel(runtimeCandidate)); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Only copy normalized editable fields back to the disk model. Environment
	// overrides, especially notification credentials, remain memory-only.
	fileConfig.Values = append([]string(nil), runtimeCandidate.Values...)
	fileConfig.Keywords = append([]string(nil), runtimeCandidate.Keywords...)
	fileConfig.ReFresh = runtimeCandidate.ReFresh
	fileConfig.AutoUpdatePush = runtimeCandidate.AutoUpdatePush
	fileConfig.ListHeight = runtimeCandidate.ListHeight

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

	_, savedConfig, savedRevision, err := loadConfigDocument(configPath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "配置已保存，但无法读取新版本")
		return
	}
	w.Header().Set("ETag", quoteETag(savedRevision))
	writeJSON(w, http.StatusOK, editableConfigFromModel(savedConfig))
}

func loadConfigDocument(path string) (models.Config, models.Config, string, error) {
	fileConfig, data, err := models.ReadConfigFile(path)
	if err != nil {
		return models.Config{}, models.Config{}, "", err
	}
	runtimeConfig := fileConfig.Clone()
	if err := runtimeConfig.ApplyEnvironment(); err != nil {
		return models.Config{}, models.Config{}, "", err
	}
	if err := runtimeConfig.Normalize(); err != nil {
		return models.Config{}, models.Config{}, "", err
	}
	digest := sha256.Sum256(data)
	return fileConfig, runtimeConfig, hex.EncodeToString(digest[:]), nil
}

func editableConfigFromModel(config models.Config) editableConfig {
	return editableConfig{
		Feeds:          append(make([]string, 0, len(config.Values)), config.Values...),
		Keywords:       append(make([]string, 0, len(config.Keywords)), config.Keywords...),
		Refresh:        config.ReFresh,
		AutoUpdatePush: config.AutoUpdatePush,
		ListHeight:     config.ListHeight,
	}
}

func validateEditableConfig(config editableConfig) error {
	if len(config.Feeds) > maximumEditableFeeds {
		return fmt.Errorf("订阅源不能超过 %d 个", maximumEditableFeeds)
	}
	if len(config.Keywords) > maximumEditableKeywords {
		return fmt.Errorf("关键词不能超过 %d 个", maximumEditableKeywords)
	}
	if config.Refresh < 1 || config.Refresh > maximumRefreshMinutes {
		return fmt.Errorf("刷新频率必须在 1 到 %d 分钟之间", maximumRefreshMinutes)
	}
	if config.AutoUpdatePush < 0 || config.AutoUpdatePush > maximumRefreshMinutes {
		return fmt.Errorf("前端推送必须在 0 到 %d 分钟之间", maximumRefreshMinutes)
	}
	if config.ListHeight < minimumEditableListHeight || config.ListHeight > maximumEditableListHeight {
		return fmt.Errorf("列表高度必须在 %d 到 %d px 之间", minimumEditableListHeight, maximumEditableListHeight)
	}
	for _, feedURL := range config.Feeds {
		if len(feedURL) > maximumFeedURLLength {
			return fmt.Errorf("订阅地址不能超过 %d 个字符", maximumFeedURLLength)
		}
	}
	for _, keyword := range config.Keywords {
		if len(keyword) > maximumKeywordLength {
			return fmt.Errorf("关键词不能超过 %d 个字符", maximumKeywordLength)
		}
	}
	return nil
}

func quoteETag(revision string) string {
	return `"` + revision + `"`
}
