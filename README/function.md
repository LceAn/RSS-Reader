
# 运行方式

## 通知功能
通知功能是由 **`main.go`、`utils/feed.go`、`utils/match.go` 和 `utils/notify.go`** 这四个文件协同完成的。

其核心逻辑是：**定时抓取 RSS 源 -\> 对比新旧内容 -\> 筛选出新内容 -\> 检查新内容是否匹配关键词 -\> 如果匹配则发送通知**。

下面是详细的分解步骤：

### 1\. 触发抓取任务 (入口)

  * **文件**: `main.go`
  * **逻辑**:
      * 在 `main` 函数中，您使用了一个定时任务库 `gocron` 来周期性地执行 `utils.FetchAllFeeds()` 这个函数。
      * `FetchAllFeeds` 是整个通知流程的起点，它会遍历您在 `config.json` 中配置的所有 RSS 订阅源地址。

### 2\. 获取并解析 RSS 源内容

  * **文件**: `utils/feed.go`
  * **逻辑**:
      * `FetchAllFeeds` 函数会为每一个 RSS 链接调用 `FetchFeed` 函数。
      * `FetchFeed` 函数负责通过 HTTP 请求获取 RSS 源的 XML 数据，并使用 `gofeed` 库将其解析成结构化的 Go 对象（`gofeed.Feed`）。
      * 在解析完成后，它会调用 `CompareAndNotify` 函数，把新抓取到的内容和程序之前保存的旧内容进行比较。

### 3\. 对比新旧内容，并进行关键词匹配

  * **文件**: `utils/feed.go` 和 `utils/match.go`
  * **逻辑**:
      * `CompareAndNotify` 函数是关键，它会找出本次抓取到的、但不存在于旧记录中的 **新文章** (`gofeed.Item`)。
      * 对于每一篇新文章，它会调用 `match.Match` 函数来进行关键词匹配。
      * `match.Match` 函数会读取 `config.json` 中您为每个订阅源配置的 `MustContain`（必须包含）和 `MustNotContain`（必须不包含）这两个关键词列表。
      * 它会检查新文章的标题 (`item.Title`) 是否满足您设定的关键词规则。只有当文章标题**包含了至少一个 `MustContain` 的词**，并且**没有包含任何 `MustNotContain` 的词**时，才算匹配成功。

### 4\. 发送通知

  * **文件**: `utils/notify.go`
  * **逻辑**:
      * 如果 `match.Match` 函数返回 `true`（即关键词匹配成功），`CompareAndNotify` 就会调用 `SendFeedsToTg` 函数来发送通知。
      * `SendFeedsToTg` 函数负责构建发送给 Telegram Bot 的消息内容，主要是文章的标题和链接。
      * 最后，它会调用 `send` 函数，该函数通过向 `https://api.telegram.org/bot<token>/sendMessage` 这个地址发送 POST 请求，最终将消息推送到您指定的 Telegram 聊天中。

-----

### 逻辑流程图

为了更直观，整个流程可以总结如下：

```
[ main.go ]         [ utils/feed.go ]            [ utils/match.go ]        [ utils/notify.go ]
gocron 定时器  ──>  FetchAllFeeds()      
                      │
                      └─> FetchFeed()
                            │
                            └─> CompareAndNotify()  ────> Match() ────┐
                                  (如果匹配成功) │                      │ (返回 true/false)
                                                 └───────────────────────> SendFeedsToTg()
                                                                             │
                                                                             └─> send()
                                                                                   (HTTP POST to Telegram API)
```

### 通知样式
Telegram的通知样式
“”“
titlele
来源：源名称
时间：时间
链接
”“”


## 日志输出
项目主要使用 Go 语言内置的 `log` 包来输出日志。这个包的功能直接、高效，会将日志信息打印到标准错误流（`stderr`），运行程序时看到的控制台输出。

整个日志输出的流程可以分为以下几个关键环节：

### 1\. 日志记录的“源头”

日志信息主要在以下几个文件中通过调用 `log.Printf()` 或 `log.Println()` 来生成：

  * **`utils/feed.go`**: 这是日志最密集的文件，记录了 RSS 处理过程中的关键事件。

      * `log.Printf("timer exec get: %s\n", url)`: **【周期性任务】** 当定时器触发，开始抓取一个新的 RSS 源时，会打印这条日志，告诉您正在处理哪个 URL。
      * `log.Printf("Error fetching feed: %v | %v", url, err)`: **【错误】** 如果在通过 HTTP 请求获取或解析 RSS 源时发生任何网络错误或格式错误，会打印这条日志。
      * `log.Printf("Error getting feed from db is null %v", url)`: **【警告】** 在尝试从内存缓存中获取一个订阅源的数据，但没有找到时，会打印这条日志。
      * `log.Println("文件已修改")`, `log.Println("错误:", err)`: **【文件监控】** 当 `config.json` 配置文件发生变化或监控出错时，会打印这些日志。

  * **`utils/notify.go`**: 这个文件负责记录与发送通知相关的日志。

      * `log.Printf("json marshal err: %+v\n", err)`: **【错误】** 在将要发送的消息内容序列化成 JSON 格式时如果失败，会打印此错误。
      * `log.Printf("http post err: %+v\n", err)`, `log.Printf("http post read body err: %+v\n", err)`: **【网络错误】** 在向飞书、钉钉或 Telegram 的 API 发送 HTTP POST 请求时，如果出现网络问题或读取响应失败，会记录这些错误。
      * `log.Printf("response status: %s,response body:%s", string(body), resp.Status)`: **【成功/失败回执】** 这是**最重要**的通知日志。无论发送成功还是失败，它都会打印出通知服务 API 返回的**状态码和具体响应内容**。如果您发现通知没收到，检查这条日志可以最快地定位问题。

  * **`main.go`**: 作为程序的入口，它主要记录一些启动和初始化阶段的日志。

      * `log.Println("WebSocket 链接已打开")`, `log.Println("WebSocket 链接已关闭")`: **【WebSocket 状态】** 记录了前端页面与后端 WebSocket 连接的建立与断开事件。
      * `log.Printf("模板加载错误: %v", err)`: **【严重错误】** 如果 `index.html` 模板文件加载失败，会导致前端页面无法渲染，这条日志会记录相关错误。
      * `log.Printf("Server started on :%s", globals.Conf.Port)`: **【启动信息】** 程序成功启动并开始监听指定端口时，会打印这条日志。

### 2\. 日志输出流程图

为了更直观地理解，整个流程可以概括为：

```
+-------------------+      +-------------------------+      +-----------------------+
|     main.go       |----->|      utils/feed.go      |----->|     utils/match.go    |
| (程序启动、Web服务) |      | (定时抓取、解析、比较)    |      | (关键词匹配，无日志)  |
+-------------------+      +-------------------------+      +-----------------------+
        |                            |                                |
        | (WebSocket连接)            | (发现新内容且匹配成功)           |
        |                            ▼                                |
        |                  +-------------------------+                |
        └─────────────────>|     utils/notify.go     |<───────────────┘
                           | (构建消息、HTTP发送、记录结果) |
                           +-------------------------+
                                      |
                                      ▼
                           +-------------------------+
                           |      控制台/终端输出      |
                           +-------------------------+
```

### 总结

后台日志输出流程：

1.  **起点**：由 `main.go` 中的定时器 `gocron` 触发，或由 `fsnotify` 文件监控器触发。
2.  **核心处理**：`utils/feed.go` 执行 RSS 的抓取和内容比对，并在此过程中记录关键的操作和错误日志。
3.  **通知与结果**：当 `feed.go` 中的 `Check` 函数确认有新内容且关键词匹配成功后，会调用 `utils/notify.go` 中的函数。
4.  **最终输出**：`notify.go` 在尝试发送通知后，会**将远程 API 的返回结果打印成日志**，这是判断通知是否成功发出的关键依据。
5.  **所有日志**：最终都会通过 Go 内置的 `log` 包输出到您运行程序的控制台界面。

## 持久化归档功能

`archives.txt` 文件是项目的**持久化归档数据库**，它的核心作用是**防止程序在重启后，对同一篇文章发送重复的通知**。

简单来说，它就是一个“**已发送通知历史记录**”文件。

-----

### 它是如何工作的？

整个流程涉及到两个文件：`globals/global.go`（负责读写文件）和 `utils/feed.go`（负责业务逻辑）。

#### 1\. 程序启动阶段：加载历史记录

  * **文件**: `globals/global.go`
  * **函数**: `Init()`
  * **逻辑**: 当您的程序一启动，`globals.Init()` 函数就会被调用。在这个函数里，程序会读取 `archives.txt` 文件的**全部内容**。然后，它会把文件里的**每一行（也就是每一个已发送过的文章链接）**，都加载到一个名为 `globals.Hash` 的内存哈希表（map）中。
  * **结果**: 这样，程序在内存里就有了一份快速、高效的“已发送链接”的临时数据库。

#### 2\. 核心处理阶段：检查与追加

  * **文件**: `utils/feed.go`
  * **函数**: `Check(...)`
  * **逻辑**: 这是整个流程最关键的地方。
    1.  当 `UpdateFeeds` 定时任务抓取到一篇新文章时，会调用 `Check` 函数来处理它。
    2.  在 `Check` 函数内部，程序首先会清理这篇文章的链接（`v.Link`），去除网址参数等无关信息。
    3.  然后，它会立即检查这个清理后的链接是否存在于我们第一步加载到内存的 `globals.Hash` 哈希表中 (`_, fileCacheOk := globals.Hash[link]`)。
    4.  **如果链接已存在** (`if fileCacheOk`)，说明这篇文章我们过去已经发送过通知了。程序会立刻 `return`，**停止后续所有操作**，从而避免了重复发送。
    5.  **如果链接不存在**，程序会继续进行关键词匹配 (`MatchStr`)。
    6.  如果关键词匹配成功，程序会执行以下两个核心操作：
          * **发送通知**：`go Notify(...)`
          * **写入归档**：`globals.WriteFile(globals.RssUrls.Archives, link)`

#### 3\. 写入文件阶段：追加内容

  * **文件**: `globals/global.go`
  * **函数**: `WriteFile(...)`
  * **逻辑**:
      * `utils/feed.go` 调用的 `globals.WriteFile` 函数，会以\*\*追加模式（append）\*\*打开 `archives.txt` 文件。
      * 然后，它会将刚刚成功发送通知的那篇文章的链接，作为**新的一行**写入到文件的末尾。
      * 同时，这个链接也会被添加到内存中的 `globals.Hash` 哈希表里，确保在程序运行期间也不会重复处理。

**代码摘要 (`utils/feed.go`):**

```go
        // 匹配关键词
        MatchStr(v.Title, func(msg string) {
            _, fileCacheOk = globals.Hash[link]
            if fileCacheOk {
                return // 如果内存中已存在，则不处理
            } else {
                globals.Hash[link] = 1 // 加入内存缓存
                // 发送通知
                go Notify(Message{
                    Routes:   []string{FeiShuRoute, TelegramRoute, DingtalkRoute},
                    Content:  fmt.Sprintf("%s\n%s", msg, v.Link),
                    FeedItem: *v,
                })
                // 将链接写入 archives.txt 文件
                globals.WriteFile(globals.RssUrls.Archives, link)
            }
        })
```

**总结一下**：`archives.txt` 是项目实现“幂等性”（即多次执行相同操作，结果都一样）的关键。没有它，每次重启程序，都会把所有 RSS 源里的文章当成新的来处理一遍，从而导致通知泛滥。

