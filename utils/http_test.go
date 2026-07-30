package utils

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestEndpointForLogRemovesCredentialsAndPath(t *testing.T) {
	rawURL := "https://user:password@example.com/private/token?api_key=secret"
	if got := endpointForLog(rawURL); got != "example.com" {
		t.Fatalf("endpointForLog() = %q, want example.com", got)
	}
}

func TestNetworkErrorRemovesSensitiveURL(t *testing.T) {
	sensitiveURL := "https://api.example.com/bot-secret-token/send"
	err := &url.Error{Op: "Post", URL: sensitiveURL, Err: errors.New("connection refused")}
	got := networkError(err).Error()
	if strings.Contains(got, "secret-token") || !strings.Contains(got, "connection refused") {
		t.Fatalf("networkError() = %q, want diagnostic without URL", got)
	}
}
