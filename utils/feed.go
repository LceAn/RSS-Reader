package utils

import (
	"context"
	"fmt"
	"path/filepath"
	"rss-reader/globals"
	"rss-reader/models"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mmcdole/gofeed"
)

const (
	feedRequestTimeout   = 20 * time.Second
	maxConcurrentFetches = 8
)

var (
	feedLogger   = NewLogger("FEED")
	refreshFeeds = make(chan struct{}, 1)
)

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

func escapeMarkdownV2URL(rawURL string) string {
	return strings.NewReplacer("\\", "\\\\", ")", "\\)").Replace(rawURL)
}

func TriggerFeedRefresh() {
	select {
	case refreshFeeds <- struct{}{}:
	default:
	}
}

// UpdateFeeds runs one complete fetch cycle at a time. Waiting for all workers
// prevents overlapping cycles and makes the status counters meaningful.
func UpdateFeeds(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			globals.SetNextRefreshTime(time.Time{})
			return
		}
		config := globals.GetConfig()
		fetchAllFeeds(ctx, config)

		refreshAfter := time.Duration(config.ReFresh) * time.Minute
		globals.SetNextRefreshTime(time.Now().Add(refreshAfter))
		timer := time.NewTimer(refreshAfter)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			globals.SetNextRefreshTime(time.Time{})
			return
		case <-timer.C:
		case <-refreshFeeds:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func fetchAllFeeds(ctx context.Context, config models.Config) {
	globals.BeginFetchCycle()
	formattedTime := time.Now().Format("2006-01-02 15:04:05")
	var successful int32
	var failed int32

	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrentFetches)
	for _, feedURL := range config.Values {
		feedURL := feedURL
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			// Bound concurrency so a large subscription list cannot exhaust
			// sockets, memory or the remote servers at once.
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			if err := updateFeed(ctx, config, feedURL, formattedTime); err != nil {
				feedLogger.Warn("Failed to fetch feed host=%s, error=%v", endpointForLog(feedURL), networkError(err))
				atomic.AddInt32(&failed, 1)
				return
			}
			atomic.AddInt32(&successful, 1)
		}()
	}
	waitGroup.Wait()
	globals.FinishFetchCycle(int(successful), int(failed))
}

func updateFeed(ctx context.Context, config models.Config, feedURL, formattedTime string) error {
	feedLogger.Info("Start fetching feed host=%s", endpointForLog(feedURL))
	requestContext, cancel := context.WithTimeout(ctx, feedRequestTimeout)
	defer cancel()

	result, err := gofeed.NewParser().ParseURLWithContext(feedURL, requestContext)
	if err != nil {
		return err
	}

	cache, cacheExists := globals.GetFeed(feedURL)
	// Only treat the feed as unchanged when both sides have a comparable link:
	// two empty links would otherwise freeze the cache on stale content.
	if cacheExists && len(result.Items) > 0 && len(cache.Items) > 0 && result.Items[0].Link != "" && result.Items[0].Link == cache.Items[0].Link {
		return nil
	}

	customFeed := models.Feed{
		Title:  result.Title,
		Link:   result.Link,
		Custom: map[string]string{"lastupdate": formattedTime},
		Items:  make([]models.Item, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		customFeed.Items = append(customFeed.Items, models.Item{
			Link:        item.Link,
			Title:       item.Title,
			Description: item.Description,
		})
		checkAndNotify(ctx, config, result, item)
	}
	globals.SetFeed(feedURL, customFeed)
	return nil
}

func GetFeeds() []models.Feed {
	config := globals.GetConfig()
	return globals.GetFeeds(config.Values)
}

// WatchConfigFileChanges watches the containing directory so editors that
// replace config.json via rename still trigger a reload.
func WatchConfigFileChanges(ctx context.Context, filePath string) {
	absolutePath, err := filepath.Abs(filePath)
	if err != nil {
		NewLogger("SYSTEM").Error("Cannot resolve config path: %v", err)
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		NewLogger("SYSTEM").Error("File watcher setup failed: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(filepath.Dir(absolutePath)); err != nil {
		NewLogger("SYSTEM").Error("File watcher error: %v", err)
		return
	}

	reload := make(chan struct{}, 1)
	var debounceTimer *time.Timer
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			eventPath, pathErr := filepath.Abs(event.Name)
			if pathErr != nil || !strings.EqualFold(eventPath, absolutePath) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
				select {
				case reload <- struct{}{}:
				default:
				}
			})

		case <-reload:
			if err := globals.ReloadConfig(); err != nil {
				NewLogger("SYSTEM").Error("Config reload rejected: %v", err)
				continue
			}
			System("配置已重新加载")
			TriggerFeedRefresh()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			NewLogger("SYSTEM").Error("File watcher error: %v", err)
		}
	}
}

func checkAndNotify(ctx context.Context, config models.Config, result *gofeed.Feed, item *gofeed.Item) {
	if !Matches(item.Title, config.Keywords) {
		return
	}

	link := canonicalLink(item.Link)
	if link == "" || !globals.ReserveLink(link) {
		return
	}

	title := escapeMarkdownV2(item.Title)
	source := escapeMarkdownV2(result.Title)
	publishTime := item.PublishedParsed
	if publishTime == nil {
		now := time.Now()
		publishTime = &now
	}
	timeString := escapeMarkdownV2(publishTime.Format("2006-01-02 15:04"))
	content := fmt.Sprintf(
		"*%s*\n\n*来源*：%s\n*时间*：%s\n[点击阅读原文](%s)",
		title,
		source,
		timeString,
		escapeMarkdownV2URL(item.Link),
	)

	dispatchNotification(ctx, func(ctx context.Context) {
		result := Notify(ctx, Message{
			Routes:   []string{FeiShuRoute, TelegramRoute, DingtalkRoute},
			Content:  content,
			FeedItem: *item,
		})
		if result.Sent == 0 {
			globals.ReleaseLink(link)
			if result.Err != nil {
				notifyLogger.Error("Notification failed for host=%s: %v", endpointForLog(link), result.Err)
			}
			return
		}
		if result.Err != nil {
			notifyLogger.Warn("Notification partially failed for host=%s: %v", endpointForLog(link), result.Err)
		}
		if err := globals.CommitLink(config.Archives, link); err != nil {
			notifyLogger.Error("Cannot persist delivered link for host=%s: %v", endpointForLog(link), err)
		}
	})
}

func canonicalLink(link string) string {
	link = strings.TrimSpace(link)
	if fragment := strings.IndexByte(link, '#'); fragment >= 0 {
		link = link[:fragment]
	}
	return link
}
