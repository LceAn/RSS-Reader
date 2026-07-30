<div align="center">

# RSS Reader

轻量、自托管的 RSS 聚合与关键词通知服务

[![CI](https://github.com/LceAn/RSS-Reader/actions/workflows/ci.yml/badge.svg)](https://github.com/LceAn/RSS-Reader/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](Dockerfile)
[![License](https://img.shields.io/badge/License-MIT-2f855a.svg)](LICENSE)

</div>

RSS Reader 定时聚合多个 RSS、Atom 或 JSON Feed，在文章标题命中监听关键词后，通过 Telegram、飞书或钉钉发送通知。服务同时提供简洁的响应式阅读页面，实时展示内容、抓取状态和经过脱敏的运行配置。

## 核心能力

- 并发抓取多个订阅源，同一时刻只运行一轮刷新任务。
- 全局关键词匹配，不区分大小写，自动忽略空值和重复项。
- 支持 Telegram、飞书、钉钉，可同时启用多个通知渠道。
- 仅在至少一个渠道投递成功后归档链接，避免误去重和重复通知。
- Vue 3 + WebSocket 实时界面，无 Node.js、打包器或外部 CDN 依赖。
- `config.json` 热重载；新配置无效时继续使用上一份有效配置。
- 配置接口自动隐藏 Token、Webhook、Chat ID 和订阅 URL 路径。
- 支持环境变量注入密钥、健康检查、优雅退出和非 root Docker 部署。

## 快速开始

需要 Go 1.25 或更高版本。

```bash
git clone https://github.com/LceAn/RSS-Reader.git
cd RSS-Reader
cp config.example.json config.json
go run .
```

PowerShell：

```powershell
git clone https://github.com/LceAn/RSS-Reader.git
Set-Location RSS-Reader
Copy-Item config.example.json config.json
go run .
```

启动后访问 [http://127.0.0.1:8080](http://127.0.0.1:8080)。先在 `config.json` 的 `values` 中加入订阅地址；修改订阅源、关键词、刷新周期或通知设置后，服务会自动重载。

## 配置说明

完整模板见 [`config.example.json`](config.example.json)。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `host` | string | HTTP 监听地址；留空时使用 `127.0.0.1` |
| `port` | number | HTTP 监听端口，范围 `1-65535` |
| `values` | string[] | RSS、Atom 或 JSON Feed 地址 |
| `refresh` | number | 后端抓取周期，单位为分钟，必须大于 `0` |
| `autoUpdatePush` | number | 页面内容检查周期，单位为分钟；`0` 表示关闭后续推送 |
| `listHeight` | number | 阅读页中每个订阅列表的高度，单位为像素 |
| `keywords` | string[] | 文章标题监听关键词，命中任意一个即进入通知流程 |
| `notify` | object | Telegram、飞书和钉钉通知配置 |
| `archives` | string | 已成功通知链接的本地归档路径 |
| `webTitle` / `webDes` | string | 页面标题与描述 |
| `github_*` | string | 页脚项目与作者链接 |

`host` 和 `port` 的修改需要重启服务，其余常用字段支持热重载。保存的新配置必须通过完整校验，否则不会替换当前运行配置。

### 环境变量

环境变量优先于 JSON 配置，建议用它们提供所有敏感值。

| 环境变量 | 用途 |
| --- | --- |
| `RSS_READER_CONFIG` | 配置文件路径，默认 `config.json` |
| `RSS_READER_HOST` | 覆盖 `host` |
| `RSS_READER_PORT` | 覆盖 `port` |
| `RSS_READER_ARCHIVES` | 覆盖归档文件路径 |
| `RSS_READER_FEISHU_API` | 飞书 Webhook |
| `RSS_READER_TELEGRAM_API` | Telegram API 模板 |
| `RSS_READER_TELEGRAM_TOKEN` | Telegram Bot Token |
| `RSS_READER_TELEGRAM_CHAT_ID` | Telegram Chat ID |
| `RSS_READER_DINGTALK_WEBHOOK` | 钉钉 Webhook |
| `RSS_READER_DINGTALK_SIGN` | 钉钉签名密钥 |

PowerShell 示例：

```powershell
$env:RSS_READER_TELEGRAM_TOKEN = "YOUR_BOT_TOKEN"
$env:RSS_READER_TELEGRAM_CHAT_ID = "YOUR_CHAT_ID"
go run .
```

### Telegram 示例

```json
{
  "api": "https://api.telegram.org/bot${token}/sendMessage",
  "chat_id": "YOUR_CHAT_ID",
  "token": "YOUR_BOT_TOKEN"
}
```

发送时会把 `${token}` 替换为实际 Bot Token。

## Docker 部署

```bash
cp config.example.json config.json
docker compose up --build -d
```

Compose 只把服务映射到宿主机的 `127.0.0.1:8080`。容器使用非 root 用户运行，配置以只读方式挂载，归档数据保存在 `rss-reader-data` 命名卷中。

```bash
docker compose logs -f
docker compose down
```

## HTTP 接口

| 接口 | 说明 |
| --- | --- |
| `GET /` | RSS 阅读页面 |
| `GET /feeds` | 当前内存中的 Feed 数据 |
| `GET /ws` | WebSocket 状态与内容推送 |
| `GET /api/config` | 脱敏配置，仅展示订阅域名和通知渠道状态 |
| `GET /healthz` | 进程存活检查 |
| `GET /readyz` | 首轮抓取完成后的就绪检查 |

所有接口均为只读；配置修改只通过本地配置文件或环境变量完成。

## 安全建议

`config.json` 和 `archives.txt` 是本地运行数据，不应提交到 Git。仓库已忽略这两个文件，并提供不含真实凭据的 `config.example.json`。

如果旧提交中曾包含 Token、Webhook、Chat ID 或带认证参数的订阅地址，仅添加 `.gitignore` 无法清除 Git 历史。请先轮换凭据，再根据需要清理历史记录。

服务默认只监听回环地址。需要远程访问时，建议通过带 TLS 和身份验证的反向代理开放，不要直接监听公网网卡。

## 开发与验证

```bash
go test ./...
go vet ./...
go test -race ./...
go build ./...
```

`-race` 需要本机启用 CGO，并安装可用的 C 编译器。GitHub Actions 会执行单元测试、竞态检测、静态检查、漏洞检查和 Docker 构建。

## 项目结构

```text
main.go                 HTTP、模板、WebSocket 与退出流程
globals/                线程安全运行时状态和嵌入式静态资源
models/                 配置与 Feed 数据模型
utils/feed.go           抓取调度、热重载、匹配和通知流程
utils/notify.go         通知渠道与 HTTP 投递
globals/static/         Vue 3 阅读界面
config.example.json     安全配置模板
Dockerfile              非 root 容器镜像
compose.yaml            本地容器编排
.github/workflows/      自动化验证
```

更多说明：[`运行机制`](README/function.md) · [`更新记录`](README/update.md)

本项目采用 [MIT License](LICENSE)。

---

<!-- repo-readme-standard:v1 -->
## 仓库维护信息

- 项目类型：其他
- 当前状态：待复盘
- 可见性：public
- 维护节奏：按需要复盘
- 相关仓库：无已确认的重复仓库关系；如需合并请先核对功能边界。
- 维护边界：普通文档和代码更新可直接提交；归档、删除、历史重写或强制推送需单独确认。
