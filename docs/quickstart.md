# 快速开始

> **核心前提**：本项目运行在**已安装并登录 Claude Code 的机器**上。每次处理消息时，框架会以子进程方式调用 `claude` CLI。

---

## 目录

- [安装依赖](#安装依赖)
- [克隆与构建](#克隆与构建)
- [配置](#配置)
- [初始化 Workspace](#初始化-workspace)
- [启动服务](#启动服务)

---

## 安装依赖

### Claude Code

```bash
npm install -g @anthropic-ai/claude-code
claude  # 首次运行，按提示完成认证
```

### Go

要求 **Go 1.23+**。

**macOS / Linux**

```bash
# macOS
brew install go

# Ubuntu / Debian
sudo apt update && sudo apt install -y golang-go

# 验证
go version
```

**Windows**

```powershell
winget install GoLang.Go
# 重新打开终端后验证
go version
```

---

## 克隆与构建

```bash
git clone https://github.com/ashwinyue/Memknow.git
cd Memknow
go mod download
go build -o server ./cmd/server
```

---

## 配置

```bash
cp config.yaml.template config.yaml
```

编辑 `config.yaml`，至少填写一个 app 的飞书凭证和 workspace 路径：

```yaml
apps:
  - id: "demo-bot"
    feishu_app_id: "cli_xxx"
    feishu_app_secret: "xxx"
    feishu_verification_token: "xxx"
    feishu_encrypt_key: ""
    workspace_dir: "./workspaces/demo-bot"
    workspace_mode: "work"
    claude:
      permission_mode: "acceptEdits"
      allowed_tools:
        - "Bash"
        - "Read"
        - "Edit"
        - "Write"
```

> ⚠️ `config.yaml` 包含 App Secret，已通过 `.gitignore` 排除在 git 追踪之外，**切勿提交**。

---

## 初始化 Workspace

首次运行 `go run ./cmd/server` 时，框架会自动使用内置模板创建 workspace 目录结构。

**使用 lark-cli 半自动初始化（可选）**

如果已安装 `lark-cli`，可以通过 Makefile 快速初始化：

```bash
make fs APP_ID=demo-bot
```

这会引导你完成 `lark-cli` 登录，并生成 `config.yaml` 模板。脚本完成后请手动补齐 `feishu_app_secret` 等敏感字段。

---

## 启动服务

**后台守护进程（推荐）**

```bash
./start.sh start      # 启动
./start.sh status     # 查看状态
./start.sh stop       # 优雅停止
./start.sh restart    # 重启
```

**前台运行（调试）**

```bash
./server
./server -config /path/to/config.yaml
```

**macOS launchd 开机自启**

```bash
make build
make daemon
make daemon-status
```

服务启动后，在飞书中向机器人发消息即可开始对话。
