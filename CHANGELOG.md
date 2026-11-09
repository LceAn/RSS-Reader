# 更新日志

本文件记录 RSS Reader 的重要变更，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [未发布] - 2026-11-10
### 新增
- 设置抽屉的“订阅源”区新增 OPML 导入/导出按钮，补齐管理端 OPML 接口的网页入口：导出直接下载 OPML 2.0 文件；导入经文件选择器上传，与配置编辑共用 CSRF、`If-Match` 并发保护和会话失效处理，有未保存编辑时暂停导入以保护草稿，导入成功后自动同步编辑器列表与配置版本。

## [未发布] - 2026-08-31
### 新增
- 管理端新增 OPML 2.0 导入/导出接口（`GET/POST /api/admin/opml`）：导出当前订阅（标题优先取已抓取的 Feed 标题）；导入支持分组嵌套 outline、自动去重、非 http(s) 链接跳过、遵守订阅上限，并复用配置编辑的事务保存与回滚流程。配套 3 个测试函数覆盖导出、导入去重、鉴权/CSRF/版本冲突路径。



### 安全
- Go 工具链从 1.25.12 升级到 1.25.13，修复标准库漏洞 GO-2026-6218（net/url）、GO-2026-6091（html/template）、GO-2026-6090（crypto/tls）、GO-2026-6089（net/http）；govulncheck CI 恢复绿色。

## [未发布] - 2026-08-30

### 新增
- 新增 `CHANGELOG.md` 与 `ROADMAP.md`。
- README 增加文档索引。

### 说明
- 本次为文档整理，服务端与前端代码未改动。

## 历史概要

- 2026-08-04 `fix: align responsive list sizing` — 响应式列表尺寸对齐。
- 2026-08-03 `fix: polish responsive layout and settings` — 响应式布局与设置页打磨。
- 2026-08-02 `fix: refine settings and status interactions` — 设置与状态交互优化。
- 更早：项目基于原作者的 RSS-Reader 开源项目二次优化（见仓库简介），逐步加入多通知渠道、配置热重载与脱敏配置接口。
