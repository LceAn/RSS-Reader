package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ConfigPathEnvironment = "RSS_READER_CONFIG"
	DefaultConfigPath     = "config.json"
)

func ParseConf() (Config, error) {
	return ParseConfFile(ConfigPath())
}

func ConfigPath() string {
	if path := strings.TrimSpace(os.Getenv(ConfigPathEnvironment)); path != "" {
		return path
	}
	return DefaultConfigPath
}

func ParseConfFile(path string) (Config, error) {
	conf, _, err := ReadConfigFile(path)
	if err != nil {
		return conf, err
	}
	if err = conf.ApplyEnvironment(); err != nil {
		return conf, err
	}
	if err = conf.Normalize(); err != nil {
		return conf, err
	}

	return conf, nil
}

// ReadConfigFile decodes the file without applying environment overrides.
// Admin updates use this form so environment-only secrets are never persisted.
func ReadConfigFile(path string) (Config, []byte, error) {
	var conf Config
	data, err := os.ReadFile(path)
	if err != nil {
		return conf, nil, err
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		return conf, nil, err
	}
	return conf, data, nil
}

// ApplyEnvironment keeps credentials out of config.json and makes container
// deployments configurable without rewriting mounted files.
func (c *Config) ApplyEnvironment() error {
	overrideString(&c.Host, "RSS_READER_HOST")
	overrideString(&c.Archives, "RSS_READER_ARCHIVES")
	overrideString(&c.Notify.FeiShu.API, "RSS_READER_FEISHU_API")
	overrideString(&c.Notify.Telegram.API, "RSS_READER_TELEGRAM_API")
	overrideString(&c.Notify.Telegram.Token, "RSS_READER_TELEGRAM_TOKEN")
	overrideString(&c.Notify.Telegram.ChatId, "RSS_READER_TELEGRAM_CHAT_ID")
	overrideString(&c.Notify.Dingtalk.Webhook, "RSS_READER_DINGTALK_WEBHOOK")
	overrideString(&c.Notify.Dingtalk.Sign, "RSS_READER_DINGTALK_SIGN")

	if value, exists := os.LookupEnv("RSS_READER_PORT"); exists {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("RSS_READER_PORT must be an integer: %w", err)
		}
		c.Port = port
	}
	return nil
}

func overrideString(target *string, key string) {
	if value, exists := os.LookupEnv(key); exists {
		*target = strings.TrimSpace(value)
	}
}

type Config struct {
	Values []string `json:"values"`
	Host   string   `json:"host,omitempty"`
	Port   int      `json:"port"`

	ReFresh        int `json:"refresh"`
	AutoUpdatePush int `json:"autoUpdatePush"`

	ListHeight              int    `json:"listHeight"`
	WebTitle                string `json:"webTitle"`
	WebDes                  string `json:"webDes"`
	Github_project_url      string `json:"github_project_url"`
	Github_project_url_name string `json:"github_project_url_name"`
	Github_author_url       string `json:"github_author_url"`
	Github_author_url_name  string `json:"github_author_url_name"`

	Keywords []string `json:"keywords"` // 关键词
	Notify   Notify   `json:"notify"`   // 通知方式
	Archives string   `json:"archives"` // 通知方式

}

// Normalize applies safe defaults, removes duplicate entries, and validates
// values that would otherwise make a background goroutine panic or hang.
func (c *Config) Normalize() error {
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if c.ReFresh <= 0 {
		return fmt.Errorf("refresh must be greater than zero")
	}
	if c.AutoUpdatePush < 0 {
		return fmt.Errorf("autoUpdatePush cannot be negative")
	}
	if c.ListHeight <= 0 {
		c.ListHeight = 600
	}
	if strings.TrimSpace(c.Archives) == "" {
		c.Archives = "archives.txt"
	}

	seenURLs := make(map[string]struct{}, len(c.Values))
	values := make([]string, 0, len(c.Values))
	for _, rawURL := range c.Values {
		feedURL := strings.TrimSpace(rawURL)
		if feedURL == "" {
			continue
		}
		parsed, err := url.Parse(feedURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("invalid feed URL %q", feedURL)
		}
		if _, exists := seenURLs[feedURL]; exists {
			continue
		}
		seenURLs[feedURL] = struct{}{}
		values = append(values, feedURL)
	}
	c.Values = values

	seenKeywords := make(map[string]struct{}, len(c.Keywords))
	keywords := make([]string, 0, len(c.Keywords))
	for _, rawKeyword := range c.Keywords {
		keyword := strings.TrimSpace(rawKeyword)
		if keyword == "" {
			continue
		}
		key := strings.ToLower(keyword)
		if _, exists := seenKeywords[key]; exists {
			continue
		}
		seenKeywords[key] = struct{}{}
		keywords = append(keywords, keyword)
	}
	c.Keywords = keywords

	return nil
}

// Clone returns a copy whose slices can be safely used without sharing the
// backing arrays held by the runtime configuration.
func (c Config) Clone() Config {
	c.Values = append([]string(nil), c.Values...)
	c.Keywords = append([]string(nil), c.Keywords...)
	return c
}

// Notify 通知方式
type Notify struct {
	FeiShu   FeiShu   `json:"feishu"`
	Telegram Telegram `json:"telegram"`
	Dingtalk Dingtalk `json:"dingtalk"`
}

// FeiShu 飞书
type FeiShu struct {
	//Text string `json:"text"`
	API string `json:"api"`
}

// Dingtalk 钉钉
type Dingtalk struct {
	//Text string `json:"text"`
	Webhook string `json:"webhook"`
	Sign    string `json:"sign"`
}

// Telegram 电报
type Telegram struct {
	ChatId string `json:"chat_id"`
	//Text   string `json:"text"`
	API   string `json:"api"`
	Token string `json:"token"`
}

func (older Config) GetIncrement(newer Config) []string {
	var (
		urlMap    = make(map[string]struct{})
		increment = make([]string, 0, len(newer.Values))
	)
	for _, item := range older.Values {
		urlMap[item] = struct{}{}
	}

	for _, item := range newer.Values {
		if _, ok := urlMap[item]; ok {
			continue
		}
		increment = append(increment, item)
	}

	return increment
}

func UpdateConfig(newConfig *Config) error {
	return UpdateConfigFile(ConfigPath(), newConfig)
}

func UpdateConfigFile(path string, newConfig *Config) error {
	config := newConfig.Clone()
	if err := config.Normalize(); err != nil {
		return err
	}
	return WriteConfigFileAtomic(path, &config)
}

// WriteConfigFileAtomic writes a complete JSON document through a temporary
// file so readers never observe a partially-written configuration.
func WriteConfigFileAtomic(path string, config *Config) error {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(absolutePath), ".rss-reader-config-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, absolutePath)
}
