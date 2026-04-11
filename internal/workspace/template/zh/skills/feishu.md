# 飞书

本项目使用 `lark-cli` 进行飞书操作。

## 路由

框架内部 channel key 格式（用于 session 路由）：
- `p2p:{chat_id}:{app_id}`
- `group:{chat_id}:{app_id}`
- `thread:{chat_id}:{thread_id}:{app_id}`

给 `lark-cli` 用的 routing_key（去掉 `:{app_id}` 后缀）：
- `p2p:{chat_id}`
- `group:{chat_id}`

## 常用命令

```bash
# 发送纯文本
lark-cli im +messages-send --as bot --chat-id "<chat_id>" --text "消息内容"

# 发送 markdown
lark-cli im +messages-send --as bot --chat-id "<chat_id>" --markdown $'## 标题\n正文'

# 获取文档
lark-cli docs +fetch --url "<文档链接>"

# 读取表格
lark-cli sheets +read --url "<表格链接>" --range "A1:D10"
```

优先使用 `--as bot`。仅在访问用户私有资源时使用 `--as user`。
如果你已经通过 `lark-cli` 发送过消息，最终回复尽量简短。
