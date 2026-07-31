package globals

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"rss-reader/models"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

var (
	reloadMu sync.Mutex

	configMu sync.RWMutex
	config   models.Config

	feedMu      sync.RWMutex
	feeds       = make(map[string]models.Feed)
	feedVersion uint64

	archiveMu     sync.Mutex
	archivedLinks = make(map[string]struct{})
	pendingLinks  = make(map[string]struct{})

	Upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	//go:embed static
	DirStatic embed.FS
)

// Init loads the initial runtime configuration. Startup cannot continue with
// an invalid configuration, so initialization errors are deliberately fatal.
func Init() {
	if err := ReloadConfig(); err != nil {
		panic(err)
	}
}

// ReloadConfig validates a new configuration before atomically publishing it.
// Existing feed cache entries are retained when their URL is still configured.
func ReloadConfig() error {
	reloadMu.Lock()
	defer reloadMu.Unlock()

	newConfig, err := models.ParseConf()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	links, err := loadArchivedLinks(newConfig.Archives)
	if err != nil {
		return fmt.Errorf("load archives: %w", err)
	}

	configMu.Lock()
	config = newConfig.Clone()
	configMu.Unlock()

	configuredURLs := make(map[string]struct{}, len(newConfig.Values))
	for _, feedURL := range newConfig.Values {
		configuredURLs[feedURL] = struct{}{}
	}
	feedMu.Lock()
	for feedURL := range feeds {
		if _, keep := configuredURLs[feedURL]; !keep {
			delete(feeds, feedURL)
			atomic.AddUint64(&feedVersion, 1)
		}
	}
	feedMu.Unlock()

	archiveMu.Lock()
	archivedLinks = links
	if pendingLinks == nil {
		pendingLinks = make(map[string]struct{})
	}
	archiveMu.Unlock()

	return nil
}

func GetConfig() models.Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return config.Clone()
}

func GetFeed(feedURL string) (models.Feed, bool) {
	feedMu.RLock()
	defer feedMu.RUnlock()
	feed, ok := feeds[feedURL]
	return feed, ok
}

func SetFeed(feedURL string, feed models.Feed) {
	feedMu.Lock()
	feeds[feedURL] = feed
	feedMu.Unlock()
	atomic.AddUint64(&feedVersion, 1)
}

func GetFeeds(urls []string) []models.Feed {
	feedMu.RLock()
	defer feedMu.RUnlock()

	result := make([]models.Feed, 0, len(urls))
	for _, feedURL := range urls {
		if feed, ok := feeds[feedURL]; ok {
			result = append(result, feed)
		}
	}
	return result
}

func FeedVersion() uint64 {
	return atomic.LoadUint64(&feedVersion)
}

// ReserveLink prevents concurrent feed workers from notifying the same item.
// The reservation must later be committed or released.
func ReserveLink(link string) bool {
	archiveMu.Lock()
	defer archiveMu.Unlock()
	if _, exists := archivedLinks[link]; exists {
		return false
	}
	if _, exists := pendingLinks[link]; exists {
		return false
	}
	pendingLinks[link] = struct{}{}
	return true
}

func ReleaseLink(link string) {
	archiveMu.Lock()
	delete(pendingLinks, link)
	archiveMu.Unlock()
}

// CommitLink persists a successfully delivered item before marking it as
// archived in memory. A write failure releases the reservation for a retry.
func CommitLink(filepath, link string) error {
	archiveMu.Lock()
	defer archiveMu.Unlock()

	if _, exists := archivedLinks[link]; exists {
		delete(pendingLinks, link)
		return nil
	}

	file, err := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		delete(pendingLinks, link)
		return err
	}
	_, writeErr := fmt.Fprintf(file, "%s\n", link)
	closeErr := file.Close()
	if writeErr != nil {
		delete(pendingLinks, link)
		return writeErr
	}
	if closeErr != nil {
		delete(pendingLinks, link)
		return closeErr
	}

	archivedLinks[link] = struct{}{}
	delete(pendingLinks, link)
	return nil
}

func loadArchivedLinks(filepath string) (map[string]struct{}, error) {
	file, err := os.OpenFile(filepath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	links := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		link := strings.TrimSpace(scanner.Text())
		if link != "" {
			links[link] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return links, nil
}
