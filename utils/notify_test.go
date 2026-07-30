package utils

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != ContentType {
			t.Fatalf("Content-Type = %q, want %q", got, ContentType)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := requestPost(context.Background(), server.URL, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("requestPost() error = %v", err)
	}
}

func TestRequestPostHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := requestPost(ctx, server.URL, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("requestPost() error = %v, want context.Canceled", err)
	}
}

func TestRouteLimiterHonorsCancellation(t *testing.T) {
	limiter := routeLimiter{lastSent: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.wait(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait() error = %v, want context.Canceled", err)
	}
}

func TestRequestPostRejectsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rejected", http.StatusBadRequest)
	}))
	defer server.Close()

	err := requestPost(context.Background(), server.URL, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("requestPost() error = %v, want status error", err)
	}
}
