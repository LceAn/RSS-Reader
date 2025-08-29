package utils

import (
	"fmt"
	"github.com/mmcdole/gofeed"
	"rss-reader/globals"
	"rss-reader/models"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

var feedLogger = NewLogger("FEED")

func escapeMarkdownV2(text string) string {
    replacer := strings.NewReplacer(
        "_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(",
        "\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">",
        "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=",
        "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".",
        "\\.", "!", "\\!",
    )
    return replacer.Replace(text)
}

func UpdateFeeds() {
	var (
		tick = time.Tick(time.Duration(globals.RssUrls.ReFresh) * time.Minute)
	)
	for {
		formattedTime := time.Now().Format("2006-01-02 15:04:05")
		for _, url := range globals.RssUrls.Values {
			go UpdateFeed(url, formattedTime)
		}
		<-tick
	}
}

func UpdateFeed(url, formattedTime string) {
	feedLogger.Info("Start fetching feed url=%s", url)
	result, err := globals.Fp.ParseURL(url)
	if err != nil {
		feedLogger.Warn("Failed to fetch feed url=%s, error=%v", url, err)
		return
	}
	//feed内容无更新时无需更新缓存
	if cache, ok := globals.DbMap[url]; ok &&
		len(result.Items) > 0 &&
		len(cache.Items) > 0 &&
		result.Items[0].Link == cache.Items[0].Link {
		return
	}
	customFeed := models.Feed{
		Title:  result.Title,
		Link:   result.Link,
		Custom: map[string]string{"lastupdate": formattedTime},
		Items:  make([]models.Item, 0, len(result.Items)),
	}
	for _, v := range result.Items {
		customFeed.Items = append(customFeed.Items, models.Item{
			Link:        v.Link,
			Title:       v.Title,
			Description: v.Description,
		})
		Check(url, result, v)
	}
	globals.Lock.Lock()
	defer globals.Lock.Unlock()
	globals.DbMap[url] = customFeed
}

// GetFeeds 获取feeds列表
func GetFeeds() []models.Feed {
	feeds := make([]models.Feed, 0, len(globals.RssUrls.Values))
	for _, url := range globals.RssUrls.Values {
		globals.Lock.RLock()
		cache, ok := globals.DbMap[url]
		globals.Lock.RUnlock()
		if !ok {
			feedLogger.Warn("Error getting feed from db is null %v", url)
			continue
		}

		feeds = append(feeds, cache)
	}
	return feeds
}

func WatchConfigFileChanges(filePath string) {
	// 创建一个新的监控器
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		NewLogger("SYSTEM").Fatal("File watcher setup failed: %v", err)
	}
	defer watcher.Close()

	// 添加要监控的文件
	err = watcher.Add(filePath)
	if err != nil {
		NewLogger("SYSTEM").Error("File watcher error: %v", err)
	}

	// 启动一个 goroutine 来处理文件变化事件
	go func() {
		for {
			time.Sleep(7 * time.Second)
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					System("通道关闭1")
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					System("文件已修改")
					globals.Init()
					formattedTime := time.Now().Format("2006-01-02 15:04:05")
					for _, url := range globals.RssUrls.Values {
						go UpdateFeed(url, formattedTime)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					System("通道关闭2")
					return
				}
				System("错误:", err)
				return
			}
		}
	}()

	select {}
}

func Check(url string, result *gofeed.Feed, v *gofeed.Item) {
	cache, cacheOk := globals.DbMap[url]
	if !cacheOk || cache.Items[0].Link != result.Items[0].Link {

		link := v.Link
		link = strings.TrimSpace(link)
		linkStrSplitForParam := strings.Split(link, "?")
		if linkStrSplitForParam != nil && len(linkStrSplitForParam) != 0 {
			link = linkStrSplitForParam[0]
		}
		linkStrSplitForRoute := strings.Split(link, "#")
		if linkStrSplitForRoute != nil && len(linkStrSplitForRoute) != 0 {
			link = linkStrSplitForRoute[0]
		}

		_, fileCacheOk := globals.Hash[link]
		if fileCacheOk {
			return
		}
		// 匹配关键词
		MatchStr(v.Title, func(msg string) {
			_, fileCacheOk = globals.Hash[link]
			if fileCacheOk {
				return
			} else {
				globals.Hash[link] = 1
		
				// --- 修改开始 ---
		
				// 1. 为 Markdown V2 准备需要转义的文本
				title := escapeMarkdownV2(v.Title)
				source := escapeMarkdownV2(result.Title)
				link := v.Link // 链接URL本身不需要转义

				// 2. 【新增】获取并格式化文章的发布时间
				publishTime := v.PublishedParsed
				if publishTime == nil { // 如果文章没有提供发布时间，则使用当前时间
					now := time.Now()
					publishTime = &now
				}
				timeString := escapeMarkdownV2(publishTime.Format("2006-01-02 15:04"))

				// 3. 构建包含【时间】的完整 Markdown 消息
				content := fmt.Sprintf(
					"*%s*\n\n*来源*：%s\n*时间*：%s\n[点击阅读原文](%s)",
					title,
					source,
					timeString, // 将格式化后的时间添加到消息中
					link,
				)

				// 4. 发送通知
				go Notify(Message{
					Routes:   []string{FeiShuRoute, TelegramRoute, DingtalkRoute},
					Content:  content,
					FeedItem: *v,
				})
		
				// --- 修改结束 ---
		
				globals.WriteFile(globals.RssUrls.Archives, link)
			}
		})
	}
}
