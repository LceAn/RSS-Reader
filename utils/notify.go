package utils

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mmcdole/gofeed"
	"io"
	"net/http"
	"net/url"
	"rss-reader/globals"
	"rss-reader/models"
	"strings"
	"sync"
	"time"
)

var (
	notifyLogger           = NewLogger("NOTIFY")
	notificationHTTPClient = &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	telegramLimiter       routeLimiter
	dingtalkLimiter       routeLimiter
	notificationWaitGroup sync.WaitGroup
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

type NotifyResult struct {
	Sent int
	Err  error
}

type routeLimiter struct {
	mu       sync.Mutex
	lastSent time.Time
}

func (limiter *routeLimiter) wait(ctx context.Context, minimumInterval time.Duration) error {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if waitFor := minimumInterval - time.Since(limiter.lastSent); waitFor > 0 {
		timer := time.NewTimer(waitFor)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	limiter.lastSent = time.Now()
	return nil
}

func dispatchNotification(ctx context.Context, callback func(context.Context)) {
	notificationWaitGroup.Add(1)
	go func() {
		defer notificationWaitGroup.Done()
		callback(ctx)
	}()
}

func WaitForNotifications(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		notificationWaitGroup.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
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

func Notify(ctx context.Context, msg Message) NotifyResult {
	if msg.Routes == nil || len(msg.Routes) == 0 {
		return NotifyResult{Err: fmt.Errorf("no notification routes requested")}
	}

	config := globals.GetConfig()
	var result NotifyResult
	errors := make([]string, 0)
	for _, route := range msg.Routes {
		var err error
		switch route {
		case FeiShuRoute:
			if config.Notify.FeiShu.API == "" {
				continue
			}
			err = sendToFeiShu(ctx, config, msg)
		case TelegramRoute:
			if config.Notify.Telegram.Token == "" && config.Notify.Telegram.ChatId == "" && config.Notify.Telegram.API == "" {
				continue
			}
			if err = telegramLimiter.wait(ctx, 1500*time.Millisecond); err == nil {
				err = sendToTelegram(ctx, config, msg)
			}
		case DingtalkRoute:
			if config.Notify.Dingtalk.Webhook == "" {
				continue
			}
			if err = dingtalkLimiter.wait(ctx, 1500*time.Millisecond); err == nil {
				err = sendToDingtalk(ctx, config, msg)
			}
		default:
			err = fmt.Errorf("unknown route %q", route)
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", route, err))
			continue
		}
		result.Sent++
	}
	if len(errors) > 0 {
		result.Err = fmt.Errorf("%s", strings.Join(errors, "; "))
	} else if result.Sent == 0 {
		result.Err = fmt.Errorf("no notification routes are configured")
	}
	return result
}

func sendToTelegram(ctx context.Context, config models.Config, msg Message) error {
	telegram := config.Notify.Telegram
	if telegram.API == "" || telegram.Token == "" || telegram.ChatId == "" {
		return fmt.Errorf("incomplete Telegram configuration")
	}
	finalMsg, err := json.Marshal(
		TelegramMessage{
			ChatId:    telegram.ChatId,
			Text:      msg.Content,
			ParseMode: "MarkdownV2",
		})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	api := strings.ReplaceAll(telegram.API, TokenReplace, telegram.Token)
	return requestPost(ctx, api, finalMsg)
}

func sendToDingtalk(ctx context.Context, config models.Config, msg Message) error {
	//签名
	encodedSign := ""
	var timestamp int64
	if config.Notify.Dingtalk.Sign != "" {

		// 获取当前时间戳（毫秒）
		timestamp = time.Now().UnixNano() / int64(time.Millisecond)
		secret := config.Notify.Dingtalk.Sign
		// 拼接字符串
		stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
		// 计算HMAC-SHA256签名
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		signData := mac.Sum(nil)
		// 进行Base64编码
		sign := base64.StdEncoding.EncodeToString(signData)
		encodedSign = sign
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
		return fmt.Errorf("marshal request: %w", err)
	}
	api := config.Notify.Dingtalk.Webhook
	if encodedSign != "" {
		parsedAPI, err := url.Parse(api)
		if err != nil {
			return fmt.Errorf("parse webhook URL: %w", err)
		}
		query := parsedAPI.Query()
		query.Set("timestamp", fmt.Sprintf("%d", timestamp))
		query.Set("sign", encodedSign)
		parsedAPI.RawQuery = query.Encode()
		api = parsedAPI.String()
	}

	return requestPost(ctx, api, finalMsg)
}

func sendToFeiShu(ctx context.Context, config models.Config, msg Message) error {
	finalMsg, err := json.Marshal(
		FeiShuMessage{
			MsgType: "text",
			Content: FeiShuMessageText{
				Text: msg.Content,
			},
		})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return requestPost(ctx, config.Notify.FeiShu.API, finalMsg)
}

func requestPost(ctx context.Context, endpoint string, param []byte) error {
	requestBody := bytes.NewBuffer(param)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return fmt.Errorf("invalid notification endpoint")
	}
	request.Header.Set("Content-Type", ContentType)
	resp, err := notificationHTTPClient.Do(request)

	if err != nil {
		return fmt.Errorf("HTTP POST request failed: %w", networkError(err))
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			notifyLogger.Error("http body close err: %+v\n", err)
		}
	}(resp.Body)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read HTTP response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("notification API returned status=%q, body=%s", resp.Status, string(body))
	}
	notifyLogger.Info("Notification API response success status=\"%s\"", resp.Status)
	return nil
}
