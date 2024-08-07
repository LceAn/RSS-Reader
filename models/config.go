package models

import (
	"encoding/json"
	"os"
)

func ParseConf() (Config, error) {
	var conf Config
	data, err := os.ReadFile("config.json")
	if err != nil {
		return conf, err
	}
	// 解析JSON数据到Config结构体
	err = json.Unmarshal(data, &conf)

	return conf, err
}

type Config struct {
	Values []string `json:"values"`
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
	data, err := json.MarshalIndent(newConfig, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile("config.json", data, 0644)
}
