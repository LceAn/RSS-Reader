package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
	maximumNotificationLength = 4096
)

type editableConfig struct {
	Feeds          []string              `json:"feeds"`
	Keywords       []string              `json:"keywords"`
	Refresh        int                   `json:"refresh"`
	AutoUpdatePush int                   `json:"autoUpdatePush"`
	ListHeight     int                   `json:"listHeight"`
	Notify         *editableNotifyConfig `json:"notify,omitempty"`
}

type editableNotifyConfig struct {
	FeiShu   editableFeiShuConfig   `json:"feishu"`
	Telegram editableTelegramConfig `json:"telegram"`
	Dingtalk editableDingtalkConfig `json:"dingtalk"`
}

type editableFeiShuConfig struct {
	Enabled           bool   `json:"enabled"`
	Webhook           string `json:"webhook,omitempty"`
	WebhookConfigured bool   `json:"webhookConfigured"`
	WebhookManaged    bool   `json:"webhookManaged"`
}

type editableTelegramConfig struct {
	Enabled          bool   `json:"enabled"`
	API              string `json:"api,omitempty"`
	Token            string `json:"token,omitempty"`
	ChatID           string `json:"chatId,omitempty"`
	APIConfigured    bool   `json:"apiConfigured"`
	TokenConfigured  bool   `json:"tokenConfigured"`
	ChatIDConfigured bool   `json:"chatIdConfigured"`
	APIManaged       bool   `json:"apiManaged"`
	TokenManaged     bool   `json:"tokenManaged"`
	ChatIDManaged    bool   `json:"chatIdManaged"`
}

type editableDingtalkConfig struct {
	Enabled           bool   `json:"enabled"`
	Webhook           string `json:"webhook,omitempty"`
	Sign              string `json:"sign,omitempty"`
	WebhookConfigured bool   `json:"webhookConfigured"`
	SignConfigured    bool   `json:"signConfigured"`
	WebhookManaged    bool   `json:"webhookManaged"`
	SignManaged       bool   `json:"signManaged"`
}

type notificationChanges struct {
	FeiShu   bool
	Telegram bool
	Dingtalk bool
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

	fileConfig, runtimeConfig, revision, err := loadConfigDocument(models.ConfigPath())
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
	changes, err := applyNotificationUpdate(&fileConfig, runtimeConfig, update.Notify)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

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
	if err := validateNotificationResult(runtimeCandidate.Notify, update.Notify, changes); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Only copy normalized non-secret fields back to the disk model. Notification
	// updates were applied directly to fileConfig, so environment credentials
	// remain memory-only.
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
		Notify: &editableNotifyConfig{
			FeiShu: editableFeiShuConfig{
				Enabled:           feishuEnabled(config.Notify),
				WebhookConfigured: config.Notify.FeiShu.API != "",
				WebhookManaged:    environmentManaged("RSS_READER_FEISHU_API"),
			},
			Telegram: editableTelegramConfig{
				Enabled:          telegramEnabled(config.Notify),
				APIConfigured:    config.Notify.Telegram.API != "",
				TokenConfigured:  config.Notify.Telegram.Token != "",
				ChatIDConfigured: config.Notify.Telegram.ChatId != "",
				APIManaged:       environmentManaged("RSS_READER_TELEGRAM_API"),
				TokenManaged:     environmentManaged("RSS_READER_TELEGRAM_TOKEN"),
				ChatIDManaged:    environmentManaged("RSS_READER_TELEGRAM_CHAT_ID"),
			},
			Dingtalk: editableDingtalkConfig{
				Enabled:           dingtalkEnabled(config.Notify),
				WebhookConfigured: config.Notify.Dingtalk.Webhook != "",
				SignConfigured:    config.Notify.Dingtalk.Sign != "",
				WebhookManaged:    environmentManaged("RSS_READER_DINGTALK_WEBHOOK"),
				SignManaged:       environmentManaged("RSS_READER_DINGTALK_SIGN"),
			},
		},
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
	if config.Notify != nil {
		if err := validateNotificationInput(*config.Notify); err != nil {
			return err
		}
	}
	return nil
}

func validateNotificationInput(config editableNotifyConfig) error {
	values := []struct {
		name  string
		value string
		isURL bool
	}{
		{name: "飞书 Webhook", value: config.FeiShu.Webhook, isURL: true},
		{name: "Telegram API", value: config.Telegram.API, isURL: true},
		{name: "Telegram Token", value: config.Telegram.Token},
		{name: "Telegram Chat ID", value: config.Telegram.ChatID},
		{name: "钉钉 Webhook", value: config.Dingtalk.Webhook, isURL: true},
		{name: "钉钉签名密钥", value: config.Dingtalk.Sign},
	}
	for _, item := range values {
		value := strings.TrimSpace(item.value)
		if len(value) > maximumNotificationLength {
			return fmt.Errorf("%s 不能超过 %d 个字符", item.name, maximumNotificationLength)
		}
		if item.isURL && value != "" {
			parsed, err := url.Parse(value)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("%s 地址无效", item.name)
			}
		}
	}
	return nil
}

func applyNotificationUpdate(fileConfig *models.Config, runtimeConfig models.Config, update *editableNotifyConfig) (notificationChanges, error) {
	if update == nil {
		return notificationChanges{}, nil
	}
	changes := notificationChanges{
		FeiShu:   update.FeiShu.Enabled != feishuEnabled(runtimeConfig.Notify) || strings.TrimSpace(update.FeiShu.Webhook) != "",
		Telegram: update.Telegram.Enabled != telegramEnabled(runtimeConfig.Notify) || strings.TrimSpace(update.Telegram.API) != "" || strings.TrimSpace(update.Telegram.Token) != "" || strings.TrimSpace(update.Telegram.ChatID) != "",
		Dingtalk: update.Dingtalk.Enabled != dingtalkEnabled(runtimeConfig.Notify) || strings.TrimSpace(update.Dingtalk.Webhook) != "" || strings.TrimSpace(update.Dingtalk.Sign) != "",
	}

	if changes.FeiShu {
		if !update.FeiShu.Enabled {
			fileConfig.Notify.FeiShu = models.FeiShu{}
		} else if value := strings.TrimSpace(update.FeiShu.Webhook); value != "" {
			if environmentManaged("RSS_READER_FEISHU_API") {
				return changes, fmt.Errorf("飞书 Webhook 由环境变量管理")
			}
			fileConfig.Notify.FeiShu.API = value
		}
	}

	if changes.Telegram {
		if !update.Telegram.Enabled {
			fileConfig.Notify.Telegram = models.Telegram{}
		} else {
			if value := strings.TrimSpace(update.Telegram.API); value != "" {
				if environmentManaged("RSS_READER_TELEGRAM_API") {
					return changes, fmt.Errorf("Telegram API 由环境变量管理")
				}
				fileConfig.Notify.Telegram.API = value
			}
			if value := strings.TrimSpace(update.Telegram.Token); value != "" {
				if environmentManaged("RSS_READER_TELEGRAM_TOKEN") {
					return changes, fmt.Errorf("Telegram Token 由环境变量管理")
				}
				fileConfig.Notify.Telegram.Token = value
			}
			if value := strings.TrimSpace(update.Telegram.ChatID); value != "" {
				if environmentManaged("RSS_READER_TELEGRAM_CHAT_ID") {
					return changes, fmt.Errorf("Telegram Chat ID 由环境变量管理")
				}
				fileConfig.Notify.Telegram.ChatId = value
			}
		}
	}

	if changes.Dingtalk {
		if !update.Dingtalk.Enabled {
			fileConfig.Notify.Dingtalk = models.Dingtalk{}
		} else {
			if value := strings.TrimSpace(update.Dingtalk.Webhook); value != "" {
				if environmentManaged("RSS_READER_DINGTALK_WEBHOOK") {
					return changes, fmt.Errorf("钉钉 Webhook 由环境变量管理")
				}
				fileConfig.Notify.Dingtalk.Webhook = value
			}
			if value := strings.TrimSpace(update.Dingtalk.Sign); value != "" {
				if environmentManaged("RSS_READER_DINGTALK_SIGN") {
					return changes, fmt.Errorf("钉钉签名密钥由环境变量管理")
				}
				fileConfig.Notify.Dingtalk.Sign = value
			}
		}
	}

	return changes, nil
}

func validateNotificationResult(config models.Notify, update *editableNotifyConfig, changes notificationChanges) error {
	if update == nil {
		return nil
	}
	checks := []struct {
		changed bool
		wanted  bool
		actual  bool
		enable  string
		disable string
	}{
		{changes.FeiShu, update.FeiShu.Enabled, feishuEnabled(config), "请填写飞书 Webhook", "飞书由环境变量启用，无法在网页关闭"},
		{changes.Telegram, update.Telegram.Enabled, telegramEnabled(config), "请完整填写 Telegram API、Token 和 Chat ID", "Telegram 由环境变量启用，无法在网页关闭"},
		{changes.Dingtalk, update.Dingtalk.Enabled, dingtalkEnabled(config), "请填写钉钉 Webhook", "钉钉由环境变量启用，无法在网页关闭"},
	}
	for _, check := range checks {
		if !check.changed || check.wanted == check.actual {
			continue
		}
		if check.wanted {
			return fmt.Errorf("%s", check.enable)
		}
		return fmt.Errorf("%s", check.disable)
	}
	return nil
}

func feishuEnabled(config models.Notify) bool {
	return strings.TrimSpace(config.FeiShu.API) != ""
}

func telegramEnabled(config models.Notify) bool {
	return strings.TrimSpace(config.Telegram.API) != "" && strings.TrimSpace(config.Telegram.Token) != "" && strings.TrimSpace(config.Telegram.ChatId) != ""
}

func dingtalkEnabled(config models.Notify) bool {
	return strings.TrimSpace(config.Dingtalk.Webhook) != ""
}

func environmentManaged(key string) bool {
	_, exists := os.LookupEnv(key)
	return exists
}

func quoteETag(revision string) string {
	return `"` + revision + `"`
}
