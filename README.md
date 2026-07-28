# 🍃 RSS-Reader：您的智能信息过滤与推送中心

[![Go Version](https://img.shields.io/badge/Go-1.18+-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Made with](https://img.shields.io/badge/Made%20with-Go%20%26%20Vue-brightgreen)](https://v3.vuejs.org/)

一款高效、可配置的 RSS 聚合器。它不仅能自动抓取您订阅的内容，还能通过强大的关键词过滤系统，只将您真正关心的信息推送到指定渠道。搭配一个优雅、实时的 Web UI, 让信息获取变得前所未有的轻松和高效。

![Project Screenshot](https://raw.githubusercontent.com/lcean/rss-reader/main/img/Snipaste_2024-03-24_12-25-10.png)
*(动态、优雅的前端界面)*

---

## ✨ 核心特性

* **智能过滤**：为每个订阅源独立设置 `包含` 和 `排除` 关键词，实现精准的内容筛选。
* **多源聚合**：通过一份 `config.json` 即可管理所有 RSS 订阅源，信息尽在掌握。
* **多渠道推送**：无缝集成 **Telegram**, **飞书 (FeiShu)**, 和 **钉钉 (DingTalk)**，第一时间获取重要资讯。
* **优雅的 Web UI**：
    * 基于 Vue.js 和 Element Plus 构建，界面现代、美观。
    * 通过 WebSocket 实现后端数据实时推送，内容更新无需刷新。
    * 支持亮色/暗色模式自动切换，适应您的工作环境。
    * 拥有卡片入场动画、悬停特效和长标题自动滚动等丰富的动态效果。
* **自动化与高效率**：
    * 后端定时任务自动刷新，无需人工干预。
    * 增量更新机制，只处理新内容，性能卓越。
    * 专业的格式化日志系统，分级、分类、高亮输出，便于监控和排错。
* **配置热重载**：运行时可随时修改 `config.json`，应用会自动加载新配置，服务不中断。
* **持久化归档**：记录已推送的文章，防止重复打扰。

---

## 🛠️ 技术栈

* **后端**: Go, Gorilla WebSocket, Gocron
* **前端**: Vue.js 3, Element Plus, Three.js
* **数据处理**: Go-feed, Fsnotify

---

## 🚀 快速开始
### 方式一：本地直接运行

#### 1. 环境准备

* 确保您已安装 [Go](https://golang.org/dl/) (版本 >= 1.18)。
* 一个现代浏览器 (Chrome, Firefox, Edge, Safari)。

#### 2. 下载与配置

1.  克隆本项目到您的本地：
    ```bash
    git clone [https://github.com/lcean/rss-reader.git](https://github.com/lcean/rss-reader.git)
    cd rss-reader
    ```

2.  **核心配置**：复制或重命名 `config.json.example` 为 `config.json`，并根据您的需求修改其内容。这是一个配置示例：
    ```json
    {
      "ReFresh": 30, // 全局刷新频率（分钟）
      "Port": "8080", // Web 服务端口
      "Values": [
        {
          "Url": "[https://rss.nodeseek.com](https://rss.nodeseek.com)", // 订阅源地址
          "MustContain": ["VPS", "服务器"], // 必须包含的关键词
          "MustNotContain": ["测评", "教程"] // 必须排除的关键词
        }
      ],
      "Notify": {
        "Telegram": {
          "Token": "YOUR_TELEGRAM_BOT_TOKEN", // 你的 Telegram Bot Token
          "ChatId": "YOUR_TELEGRAM_CHAT_ID" // 你的 Telegram Chat ID
        }
        // ... 其他通知渠道配置
      }
    }
    ```
    * 详细的 `config.json` 配置说明，请参考 [**功能详解文档**](https://github.com/LceAn/RSS-Reader/tree/main/README/function.md)。

#### 3. 运行

在项目根目录下，执行以下命令：
```bash
go run main.go
````

当您在控制台看到类似以下的日志时，表示服务已成功启动：

```
2025-08-29 15:00:00 [INFO] [SYSTEM] - Server started on port: 8080
```

现在，打开您的浏览器并访问 `http://localhost:8080`，即可看到 RSS-Reader 的 Web 界面。


### 方式二：使用 Docker (敬请期待)
🐳 Docker 部署的相关 Dockerfile 和说明文档正在准备中，未来将支持一行命令快速启动，敬请期待！

-----

## 📂 项目文档

  * [**功能详解 & 配置指南**](https://github.com/LceAn/RSS-Reader/tree/main/README/function.md)：深入了解 `config.json` 的所有配置项和高级用法。
  * [**开发与更新日志**](https://github.com/LceAn/RSS-Reader/tree/main/README/update.md)：查看本项目从诞生至今的所有功能迭代和优化记录。

-----

## 📜 开源许可

本项目采用 [MIT License](https://www.google.com/search?q=LICENSE) 开源许可。

---

## 仓库结构

- `.vscode/`
- `LICENSE`
- `README.md`
- `README/`
- `archives.txt`
- `config.json`
- `globals/`
- `go.mod`
- `go.sum`
- `main.go`
- `models/`
- `utils/`

<!-- repo-readme-standard:v1 -->
## 仓库维护信息

- 项目类型：其他
- 当前状态：待复盘
- 可见性：public
- 维护节奏：按需要复盘
- 相关仓库：无已确认的重复仓库关系；如需合并请先核对功能边界。
- 维护边界：普通文档和代码更新可直接提交；归档、删除、历史重写或强制推送需单独确认。
