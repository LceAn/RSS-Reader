package utils

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mmcdole/gofeed"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"rss-reader/globals"
	"strings"
	"time"
)

const (
	FeiShuRoute   = "feishu"
	DingtalkRoute = "dingding"
	TelegramRoute = "telegram"
	ContentType   = "application/json"
	TokenReplace  = "${token}"
)

type Message struct {
	Routes   []string    `json:"routes"`
	Content  string      `json:"content"`
	FeedItem gofeed.Item `json:"feedItem"`
}

type FeiShuMessage struct {
	MsgType string            `json:"msg_type"`
	Content FeiShuMessageText `json:"content"`
}

type FeiShuMessageText struct {
	Text string `json:"text"`
}

// type TelegramMessage struct {
// 	ChatId string `json:"chat_id"`
// 	Text   string `json:"text"`
// }

type TelegramMessage struct {
    ChatId    string `json:"chat_id"`
    Text      string `json:"text"`
    ParseMode string `json:"parse_mode,omitempty"`
}

type DingtalkMessage struct {
	Msgtype string              `json:"msgtype"`
	Link    DingtalkMessageLink `json:"link"`
}
type DingtalkMessageLink struct {
	MessageUrl string `json:"messageUrl"`
	PicUrl     string `json:"picUrl"`
	Text       string `json:"text"`
	Title      string `json:"title"`
}

func Notify(msg Message) {
	if msg.Routes == nil || len(msg.Routes) == 0 {
		return
	}
	for _, route := range msg.Routes {
		switch route {
		case FeiShuRoute:
			if globals.RssUrls.Notify.FeiShu.API != "" {
				sendToFeiShu(msg)
			}
		case TelegramRoute:
			if globals.RssUrls.Notify.Telegram.Token != "" && globals.RssUrls.Notify.Telegram.ChatId != "" {
				time.Sleep(1500)
				sendToTelegram(msg)
			}
		case DingtalkRoute:
			if globals.RssUrls.Notify.Dingtalk.Webhook != "" {
				time.Sleep(1500)
				sendToDingtalk(msg)
			}
		default:
			log.Println("without route")
		}
	}
}
func sendToTelegram(msg Message) {
    finalMsg, err := json.Marshal(
        TelegramMessage{
            ChatId:    globals.RssUrls.Notify.Telegram.ChatId,
            Text:      msg.Content,
            ParseMode: "MarkdownV2", // 告诉 Telegram 使用 Markdown V2 解析
        })
    if err != nil {
        log.Printf("json marshal err: %+v\n", err)
        return
    }
    api := strings.ReplaceAll(globals.RssUrls.Notify.Telegram.API, TokenReplace, globals.RssUrls.Notify.Telegram.Token)
    requestPost(api, finalMsg)
}

func sendToDingtalk(msg Message) {
	//签名
	encodedSign := ""
	var timestamp int64
	if globals.RssUrls.Notify.Dingtalk.Sign != "" {

		// 获取当前时间戳（毫秒）
		timestamp = time.Now().UnixNano() / int64(time.Millisecond)
		secret := globals.RssUrls.Notify.Dingtalk.Sign
		// 拼接字符串
		stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
		// 计算HMAC-SHA256签名
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		signData := mac.Sum(nil)
		// 进行Base64编码
		sign := base64.StdEncoding.EncodeToString(signData)
		// 对签名进行URL编码
		encodedSign = url.QueryEscape(sign)
	}

	finalMsg, err := json.Marshal(
		DingtalkMessage{
			Msgtype: "link",
			Link: DingtalkMessageLink{
				MessageUrl: msg.FeedItem.Link,
				Title:      msg.FeedItem.Title,
				Text:       msg.Content,
			},
		})
	if err != nil {
		log.Printf("json marshal err: %+v\n", err)
		return
	}
	api := globals.RssUrls.Notify.Dingtalk.Webhook
	if encodedSign != "" {
		api = fmt.Sprintf("%s&timestamp=%d&sign=%s", api, timestamp, encodedSign)
	}

	requestPost(api, finalMsg)
}

func sendToFeiShu(msg Message) {
	finalMsg, err := json.Marshal(
		FeiShuMessage{
			MsgType: "text",
			Content: FeiShuMessageText{
				Text: msg.Content,
			},
		})
	if err != nil {
		log.Printf("json marshal err: %+v\n", err)
		return
	}
	requestPost(globals.RssUrls.Notify.FeiShu.API, finalMsg)
}

func requestPost(url string, param []byte) {
	requestBody := bytes.NewBuffer(param)
	resp, err := http.Post(url, ContentType, requestBody)

	if err != nil {
		log.Printf("http post err: %+v\n", err)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("http body close err: %+v\n", err)
		}
	}(resp.Body)
	body, err := ioutil.ReadAll(resp.Body) // 读取响应内容
	if err != nil {
		log.Printf("http post read body err: %+v\n", err)
		return
	}
	log.Printf("response status: %s,response body:%s", string(body), resp.Status)
	//string(body)
	return
}
