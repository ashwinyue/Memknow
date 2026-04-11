# 搜索

需要联网查资料、找新闻、找官网、找文档时，优先使用 workspace 里的本地搜索入口：

```bash
$WORKSPACE_DIR/bin/web-search "你的查询"
```

## 规则

- 默认先用 `bin/web-search`，不要一上来就依赖 MCP。
- 脚本会自动优先走 Tavily；如果当前 bot 没配置 Tavily，就自动降级 DuckDuckGo。
- 输出是 JSON，包含 `query`、`provider`、`results`。
- 先读结果里的标题、URL、摘要；必要时再用其他工具打开具体页面。
- 如果本地搜索失败、结果明显不足、或需要更强的网页交互，再考虑 MCP。

## 例子

```bash
$WORKSPACE_DIR/bin/web-search "OpenAI Responses API structured outputs"
$WORKSPACE_DIR/bin/web-search "飞书 开放平台 事件订阅"
```
