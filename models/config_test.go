package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigNormalize(t *testing.T) {
	config := Config{
		Port:           8080,
		ReFresh:        5,
		AutoUpdatePush: 1,
		Values: []string{
			" https://example.com/feed.xml ",
			"https://example.com/feed.xml",
		},
		Keywords: []string{" Go ", "go", ""},
	}

	if err := config.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if config.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want loopback default", config.Host)
	}
	if config.ListHeight != 600 {
		t.Fatalf("ListHeight = %d, want 600", config.ListHeight)
	}
	if config.Archives != "archives.txt" {
		t.Fatalf("Archives = %q, want archives.txt", config.Archives)
	}
	if len(config.Values) != 1 || config.Values[0] != "https://example.com/feed.xml" {
		t.Fatalf("Values = %#v, want one normalized URL", config.Values)
	}
	if len(config.Keywords) != 1 || config.Keywords[0] != "Go" {
		t.Fatalf("Keywords = %#v, want one normalized keyword", config.Keywords)
	}
}

func TestConfigNormalizeRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "invalid port", config: Config{Port: 0, ReFresh: 5}},
		{name: "invalid refresh", config: Config{Port: 8080, ReFresh: 0}},
		{name: "negative push interval", config: Config{Port: 8080, ReFresh: 5, AutoUpdatePush: -1}},
		{name: "invalid feed URL", config: Config{Port: 8080, ReFresh: 5, Values: []string{"file:///tmp/feed.xml"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.config.Normalize(); err == nil {
				t.Fatal("Normalize() error = nil, want validation error")
			}
		})
	}
}

func TestConfigCloneDoesNotShareSlices(t *testing.T) {
	original := Config{Values: []string{"https://example.com/feed.xml"}, Keywords: []string{"go"}}
	clone := original.Clone()
	clone.Values[0] = "https://example.org/feed.xml"
	clone.Keywords[0] = "rss"

	if original.Values[0] != "https://example.com/feed.xml" || original.Keywords[0] != "go" {
		t.Fatalf("Clone() shares slice storage with original: %#v", original)
	}
}

func TestParseConfFileAppliesEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{
		"host":"127.0.0.1",
		"port":8080,
		"values":[],
		"refresh":5,
		"autoUpdatePush":1,
		"notify":{"telegram":{"api":"file-api","chat_id":"file-chat","token":"file-token"}},
		"archives":"archives.txt"
	}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("RSS_READER_HOST", "0.0.0.0")
	t.Setenv("RSS_READER_PORT", "9090")
	t.Setenv("RSS_READER_TELEGRAM_TOKEN", "environment-token")
	t.Setenv("RSS_READER_TELEGRAM_CHAT_ID", "environment-chat")

	config, err := ParseConfFile(path)
	if err != nil {
		t.Fatalf("ParseConfFile() error = %v", err)
	}
	if config.Host != "0.0.0.0" || config.Port != 9090 {
		t.Fatalf("listen address = %s:%d, want 0.0.0.0:9090", config.Host, config.Port)
	}
	if config.Notify.Telegram.Token != "environment-token" || config.Notify.Telegram.ChatId != "environment-chat" {
		t.Fatal("Telegram environment overrides were not applied")
	}
}

func TestParseConfFileRejectsInvalidEnvironmentPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"port":8080,"refresh":5,"values":[],"notify":{}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("RSS_READER_PORT", "not-a-number")
	if _, err := ParseConfFile(path); err == nil {
		t.Fatal("ParseConfFile() error = nil, want invalid environment error")
	}
}
