
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
