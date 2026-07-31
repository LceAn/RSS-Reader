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
