# Windows 兼容性说明

> **Windows 仅通过 WSL2 支持**。本项目依赖 `syscall.Flock`、systemd / launchd 脚本以及大量 Unix Shell 工具，不支持 Windows 原生运行。

## 支持方案

### WSL2（推荐）

在 WSL2 Ubuntu/Debian 环境中可获得与 Linux 完全一致的兼容性：
- 所有 Shell 脚本（`daemon-linux.sh`、`setup_lark_bot.sh` 等）无需修改即可运行
- systemd 服务可用
- `flock(2)` 文件锁、`lsof` 等 Unix 工具齐全

```bash
# 在 WSL2 中按 Linux 方式构建和运行
go build -o server ./cmd/server
./daemon-linux.sh install --binary ./server --config ./config.yaml
```

### Git Bash / PowerShell / Windows CMD

**不支持**。以下原因导致原生 Windows 无法正常运行：
- `start.sh`、`daemon-*.sh` 等管理脚本依赖 Unix 特有命令（`flock`、`lsof`、`systemctl` / `launchctl`）
- `cmd/server` 使用 `syscall.Flock` 实现单例锁，Windows 上无法编译
- `daemon-linux.sh` 与 `daemon-mac.sh` 均通过 `uname -s` 主动拒绝非目标平台

## 跨平台工具

项目中唯一支持原生 Windows 编译的组件是 `cmd/filelock`（基于 `github.com/gofrs/flock`），它是为 workspace skill 文档示例提供的跨平台文件锁工具。但**该工具仅用于 skill 示例**，不能使服务端在 Windows 原生运行。

```powershell
# filelock 示例（PowerShell）
go build ./cmd/filelock
.\filelock.exe C:\temp\.lock 10 powershell -Command "Add-Content -Path 'C:\temp\data.txt' -Value 'new line'"
```

## 条件编译说明

代码库中存在少数 `//go:build windows` 文件（如 `internal/claude/executor_windows.go`），这些仅用于保证 `go build ./...` 在 Windows 上**编译通过**，不代表服务端可以在 Windows 原生运行。

## 注意事项

- 若使用 `lark-cli`，请在 WSL2 内安装并执行
- 所有守护进程管理请统一使用 `daemon-linux.sh`（WSL2 内）
