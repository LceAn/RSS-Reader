package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"rss-reader/globals"
	"testing"
)

func TestConfigHandlerReturnsOnlyPublicFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	recorder := httptest.NewRecorder()
	configHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	for _, forbidden := range []string{"token", "api", "chat_id", "webhook", "sign"} {
		if _, exists := response[forbidden]; exists {
			t.Fatalf("response exposes forbidden field %q", forbidden)
		}
	}
	notify, ok := response["notify"].(map[string]interface{})
	if !ok {
		t.Fatalf("notify = %#v, want object", response["notify"])
	}
	for key, value := range notify {
		if _, ok := value.(bool); !ok {
			t.Fatalf("notify.%s = %#v, want boolean status", key, value)
		}
	}
	if _, ok := response["keywords"].([]interface{}); !ok {
		t.Fatalf("keywords = %#v, want array", response["keywords"])
	}
}

func TestConfigHandlerRejectsWrites(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/config", nil)
	recorder := httptest.NewRecorder()
	configHandler(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestPublicFeedHostsRemovesCredentialsAndPaths(t *testing.T) {
	got := publicFeedHosts([]string{
		"https://user:password@example.com/private/token?api_key=secret",
		"https://example.com/another-feed",
		"https://rss.example.org:8443/feed.xml",
	})
	want := []string{"example.com", "rss.example.org:8443"}
	if len(got) != len(want) {
		t.Fatalf("publicFeedHosts() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("publicFeedHosts()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("Content-Security-Policy is empty")
	}
}

func TestHealthAndReadyEndpoints(t *testing.T) {
	handler := newHandler(context.Background(), nil)

	healthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", healthRecorder.Code)
	}

	globals.FinishFetchCycle(0, 0)
	readyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readyRecorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if readyRecorder.Code != http.StatusOK {
		t.Fatalf("ready status = %d, want 200", readyRecorder.Code)
	}
}
