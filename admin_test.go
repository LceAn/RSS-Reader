package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"rss-reader/globals"
	"rss-reader/models"
	"strings"
	"testing"
)

const testAdminPassword = "correct horse battery staple"

func TestAdminLoginSessionAndLogout(t *testing.T) {
	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	cookie, csrf := loginForTest(t, service)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie flags = HttpOnly:%v SameSite:%v", cookie.HttpOnly, cookie.SameSite)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	sessionRequest.AddCookie(cookie)
	sessionRecorder := httptest.NewRecorder()
	service.sessionHandler(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("session status = %d, want 200", sessionRecorder.Code)
	}
	var state sessionResponse
	if err := json.Unmarshal(sessionRecorder.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if !state.Enabled || !state.Authenticated || state.CSRFToken != csrf {
		t.Fatalf("session response = %#v", state)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRequest.Host = "example.com"
	logoutRequest.Header.Set("Origin", "http://example.com")
	logoutRequest.Header.Set("X-CSRF-Token", csrf)
	logoutRequest.AddCookie(cookie)
	logoutRecorder := httptest.NewRecorder()
	service.logoutHandler(logoutRecorder, logoutRequest)
	if logoutRecorder.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", logoutRecorder.Code)
	}
	if _, ok := service.authenticate(sessionRequest); ok {
		t.Fatal("session remains valid after logout")
	}
}

func TestAdminLoginRateLimit(t *testing.T) {
	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	for attempt := 0; attempt < loginFailureLimit; attempt++ {
		request := loginRequest(`{"password":"wrong password"}`)
		recorder := httptest.NewRecorder()
		service.loginHandler(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt+1, recorder.Code)
		}
	}
	request := loginRequest(`{"password":"wrong password"}`)
	recorder := httptest.NewRecorder()
	service.loginHandler(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, want 429", recorder.Code)
	}
}

func TestAdminConfigUpdatePreservesEnvironmentSecrets(t *testing.T) {
	path := writeAdminTestConfig(t)
	t.Setenv("RSS_READER_CONFIG", path)
	t.Setenv("RSS_READER_TELEGRAM_TOKEN", "environment-only-token")
	if err := globals.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}

	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	cookie, csrf := loginForTest(t, service)

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	service.adminConfigHandler(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorizedRecorder.Code)
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	getRequest.AddCookie(cookie)
	getRecorder := httptest.NewRecorder()
	service.adminConfigHandler(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET admin config status = %d, body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	if bytes.Contains(getRecorder.Body.Bytes(), []byte("environment-only-token")) {
		t.Fatal("GET admin config exposed an environment-managed token")
	}
	var visibleConfig editableConfig
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &visibleConfig); err != nil {
		t.Fatalf("decode editable config: %v", err)
	}
	if visibleConfig.Notify == nil || !visibleConfig.Notify.Telegram.TokenConfigured || !visibleConfig.Notify.Telegram.TokenManaged {
		t.Fatalf("Telegram credential status = %#v", visibleConfig.Notify)
	}
	etag := getRecorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET admin config did not return ETag")
	}

	update := editableConfig{
		Feeds:          []string{"https://example.org/feed.xml"},
		Keywords:       []string{"Go", "RSS"},
		Refresh:        10,
		AutoUpdatePush: 2,
		ListHeight:     720,
	}
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	putRequest := httptest.NewRequest(http.MethodPut, "/api/admin/config", bytes.NewReader(body))
	putRequest.Host = "example.com"
	putRequest.Header.Set("Origin", "http://example.com")
	putRequest.Header.Set("X-CSRF-Token", csrf)
	putRequest.Header.Set("If-Match", etag)
	putRequest.AddCookie(cookie)
	putRecorder := httptest.NewRecorder()
	service.adminConfigHandler(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT admin config status = %d, body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	fileConfig, data, err := models.ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile() error = %v", err)
	}
	if bytes.Contains(data, []byte("environment-only-token")) || fileConfig.Notify.Telegram.Token != "" {
		t.Fatal("environment-only Telegram token was persisted")
	}
	if fileConfig.Notify.Telegram.API != "https://api.telegram.org/bot${token}/sendMessage" {
		t.Fatal("legacy update without notify changed the Telegram API")
	}
	if fileConfig.ReFresh != 10 || fileConfig.ListHeight != 720 || len(fileConfig.Values) != 1 {
		t.Fatalf("saved file config = %#v", fileConfig)
	}
	if got := globals.GetConfig().Notify.Telegram.Token; got != "environment-only-token" {
		t.Fatalf("runtime Telegram token = %q, want environment override", got)
	}

	staleRequest := httptest.NewRequest(http.MethodPut, "/api/admin/config", bytes.NewReader(body))
	staleRequest.Host = "example.com"
	staleRequest.Header.Set("Origin", "http://example.com")
	staleRequest.Header.Set("X-CSRF-Token", csrf)
	staleRequest.Header.Set("If-Match", etag)
	staleRequest.AddCookie(cookie)
	staleRecorder := httptest.NewRecorder()
	service.adminConfigHandler(staleRecorder, staleRequest)
	if staleRecorder.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale update status = %d, want 412", staleRecorder.Code)
	}

	managedUpdate, managedETag, _ := getEditableConfigForTest(t, service, cookie)
	managedUpdate.Notify.Telegram.Enabled = true
	managedUpdate.Notify.Telegram.Token = "browser-token"
	managedRecorder := putEditableConfigForTest(t, service, cookie, csrf, managedETag, managedUpdate)
	if managedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("environment-managed update status = %d, want 400; body=%s", managedRecorder.Code, managedRecorder.Body.String())
	}
	fileConfig, data, err = models.ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile() after rejected update error = %v", err)
	}
	if bytes.Contains(data, []byte("browser-token")) || fileConfig.Notify.Telegram.Token != "" {
		t.Fatal("environment-managed Telegram token was overwritten")
	}
}

func TestAdminConfigNotificationSecretsAndUpdates(t *testing.T) {
	path := writeAdminTestConfig(t)
	fileConfig, _, err := models.ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile() error = %v", err)
	}
	fileConfig.Notify.FeiShu.API = "https://open.feishu.cn/open-apis/bot/v2/hook/private-feishu"
	fileConfig.Notify.Telegram.API = "https://api.telegram.org/bot${token}/sendMessage"
	fileConfig.Notify.Telegram.Token = "private-telegram-token"
	fileConfig.Notify.Telegram.ChatId = "private-chat-id"
	fileConfig.Notify.Dingtalk.Webhook = "https://oapi.dingtalk.com/robot/send?access_token=private-dingtalk"
	fileConfig.Notify.Dingtalk.Sign = "private-dingtalk-sign"
	if err := models.WriteConfigFileAtomic(path, &fileConfig); err != nil {
		t.Fatalf("WriteConfigFileAtomic() error = %v", err)
	}
	t.Setenv("RSS_READER_CONFIG", path)
	if err := globals.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}

	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	cookie, csrf := loginForTest(t, service)

	current, etag, responseBody := getEditableConfigForTest(t, service, cookie)
	for _, secret := range []string{
		"private-feishu",
		"private-telegram-token",
		"private-chat-id",
		"private-dingtalk",
		"private-dingtalk-sign",
	} {
		if bytes.Contains(responseBody, []byte(secret)) {
			t.Fatalf("GET admin config exposed %q", secret)
		}
	}
	if current.Notify == nil || !current.Notify.FeiShu.Enabled || !current.Notify.Telegram.Enabled || !current.Notify.Dingtalk.Enabled {
		t.Fatalf("notification status = %#v", current.Notify)
	}

	keepRecorder := putEditableConfigForTest(t, service, cookie, csrf, etag, current)
	if keepRecorder.Code != http.StatusOK {
		t.Fatalf("blank credential update status = %d, body=%s", keepRecorder.Code, keepRecorder.Body.String())
	}
	assertStoredNotificationSecrets(t, path,
		"https://open.feishu.cn/open-apis/bot/v2/hook/private-feishu",
		"private-telegram-token",
		"private-chat-id",
		"https://oapi.dingtalk.com/robot/send?access_token=private-dingtalk",
		"private-dingtalk-sign",
	)

	replacement, replacementETag, _ := getEditableConfigForTest(t, service, cookie)
	replacement.Notify.FeiShu.Webhook = "https://open.feishu.cn/open-apis/bot/v2/hook/replacement"
	replacement.Notify.Telegram.Token = "replacement-token"
	replacement.Notify.Dingtalk.Sign = "replacement-sign"
	replaceRecorder := putEditableConfigForTest(t, service, cookie, csrf, replacementETag, replacement)
	if replaceRecorder.Code != http.StatusOK {
		t.Fatalf("credential replacement status = %d, body=%s", replaceRecorder.Code, replaceRecorder.Body.String())
	}
	assertStoredNotificationSecrets(t, path,
		"https://open.feishu.cn/open-apis/bot/v2/hook/replacement",
		"replacement-token",
		"private-chat-id",
		"https://oapi.dingtalk.com/robot/send?access_token=private-dingtalk",
		"replacement-sign",
	)

	disabled, disabledETag, _ := getEditableConfigForTest(t, service, cookie)
	disabled.Notify.FeiShu.Enabled = false
	disabled.Notify.Telegram.Enabled = false
	disabled.Notify.Dingtalk.Enabled = false
	disableRecorder := putEditableConfigForTest(t, service, cookie, csrf, disabledETag, disabled)
	if disableRecorder.Code != http.StatusOK {
		t.Fatalf("notification disable status = %d, body=%s", disableRecorder.Code, disableRecorder.Body.String())
	}
	stored, _, err := models.ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile() after disable error = %v", err)
	}
	if stored.Notify != (models.Notify{}) {
		t.Fatalf("disabled notification config = %#v, want empty", stored.Notify)
	}
}

func TestAdminConfigRejectsMissingCSRF(t *testing.T) {
	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	cookie, _ := loginForTest(t, service)
	request := httptest.NewRequest(http.MethodPut, "/api/admin/config", strings.NewReader(`{}`))
	request.Host = "example.com"
	request.Header.Set("Origin", "http://example.com")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	service.adminConfigHandler(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", recorder.Code)
	}
}

func loginForTest(t *testing.T, service *adminService) (*http.Cookie, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"password": testAdminPassword})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	request := loginRequest(string(payload))
	recorder := httptest.NewRecorder()
	service.loginHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	response := recorder.Result()
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	var state sessionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return cookies[0], state.CSRFToken
}

func getEditableConfigForTest(t *testing.T, service *adminService, cookie *http.Cookie) (editableConfig, string, []byte) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	service.adminConfigHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET admin config status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var config editableConfig
	if err := json.Unmarshal(recorder.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode admin config: %v", err)
	}
	return config, recorder.Header().Get("ETag"), append([]byte(nil), recorder.Body.Bytes()...)
}

func putEditableConfigForTest(t *testing.T, service *adminService, cookie *http.Cookie, csrf, etag string, update editableConfig) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/admin/config", bytes.NewReader(body))
	request.Host = "example.com"
	request.Header.Set("Origin", "http://example.com")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("If-Match", etag)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	service.adminConfigHandler(recorder, request)
	return recorder
}

func assertStoredNotificationSecrets(t *testing.T, path, feishu, telegramToken, telegramChatID, dingtalkWebhook, dingtalkSign string) {
	t.Helper()
	config, _, err := models.ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile() error = %v", err)
	}
	if config.Notify.FeiShu.API != feishu ||
		config.Notify.Telegram.Token != telegramToken ||
		config.Notify.Telegram.ChatId != telegramChatID ||
		config.Notify.Dingtalk.Webhook != dingtalkWebhook ||
		config.Notify.Dingtalk.Sign != dingtalkSign {
		t.Fatalf("stored notification secrets = %#v", config.Notify)
	}
}

func loginRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.Host = "example.com"
	request.RemoteAddr = "192.0.2.1:12345"
	request.Header.Set("Origin", "http://example.com")
	return request
}

func writeAdminTestConfig(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	archive := filepath.Join(directory, "archives.txt")
	data := []byte(`{
  "host": "127.0.0.1",
  "port": 8080,
  "values": [],
  "refresh": 5,
  "autoUpdatePush": 1,
  "listHeight": 600,
  "keywords": [],
  "notify": {
    "feishu": {"api": ""},
    "dingtalk": {"webhook": "", "sign": ""},
    "telegram": {
      "api": "https://api.telegram.org/bot${token}/sendMessage",
      "chat_id": "",
      "token": ""
    }
  },
  "archives": "` + filepath.ToSlash(archive) + `"
}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
