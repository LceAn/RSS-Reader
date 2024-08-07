package globals

import (
	"bufio"
	"embed"
	"fmt"
	"github.com/mmcdole/gofeed"
	"log"
	"os"
	"rss-reader/models"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	DbMap     map[string]models.Feed
	RssUrls   models.Config
	Upgrader  = websocket.Upgrader{}
	Lock      sync.RWMutex
	MatchList = make([]string, 0)
	Hash      = make(map[string]int)
	Fp        = gofeed.NewParser()

	//go:embed static
	DirStatic embed.FS

	HtmlContent []byte
)

func Init() {
	conf, err := models.ParseConf()
	if err != nil {
		panic(err)
	}
	RssUrls = conf
	// 读取 index.html 内容
	HtmlContent, err = DirStatic.ReadFile("static/index.html")
	if err != nil {
		panic(err)
	}

	DbMap = make(map[string]models.Feed)

	for _, keyword := range conf.Keywords {
		MatchList = append(MatchList, keyword)
	}
	_, err = os.Open(conf.Archives)
	if err != nil {
		WriteFile(conf.Archives, "")
	}
	ReadFile(conf.Archives)
}

func ReadFile(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		log.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() { // 逐行扫描
		text := scanner.Text()
		text = strings.TrimSpace(text)
		if text != "" {
			Hash[text] = 1
		}
	}
}

func WriteFile(filepath string, s string) {
	// 打开文件，如果文件不存在则创建，如果存在则以追加模式打开
	writeFile, errOpen := os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if errOpen != nil {
		log.Fatalf("open result file err: %+v", errOpen)
	}

	_, errWrite := writeFile.WriteString(fmt.Sprintf("%s\n", s))
	if errWrite != nil {
		log.Println("writing to file error:", errWrite)
	}

	defer writeFile.Close()

}
