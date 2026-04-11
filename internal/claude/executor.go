package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ashwinyue/Memknow/internal/config"
)

// ExecuteRequest holds all parameters for a claude CLI invocation.
type ExecuteRequest struct {
	Prompt          string
	SessionID       string
	SessionType     string
	ClaudeSessionID string // empty = new context (no --resume)
	AppConfig       *config.AppConfig
	WorkspaceDir    string
	ChannelKey      string // used to derive routing_key for feishu_ops
	SenderID        string // sender's open_id, for p2p feishu_ops calls
	OnProgress      func(ProgressEvent)
}

// ToolUseRecord captures a tool_use + its result from the stream.
type ToolUseRecord struct {
	Name   string
	Input  string // JSON or plain text
	Output string // result output
}

// ExecuteResult holds the output of a claude CLI invocation.
type ExecuteResult struct {
	Text             string
	ClaudeSessionID  string // extracted from stream-json system event
	CostUSD          float64
	DurationMS       int64
	Reasoning        string          // collected thinking blocks
	ToolUses         []ToolUseRecord // collected tool_use + tool_result pairs
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Model            string
	FinishReason     string
}

type ProgressKind string

const (
	ProgressThinking   ProgressKind = "thinking"
	ProgressToolUse    ProgressKind = "tool_use"
	ProgressToolResult ProgressKind = "tool_result"
	ProgressText       ProgressKind = "text"
	ProgressResult     ProgressKind = "result"
)

type ProgressEvent struct {
	Kind      ProgressKind
	Text      string
	ToolName  string
	ToolInput string
}

// ExecutorInterface abstracts claude execution via long-running interactive sessions.
type ExecutorInterface interface {
	Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error)
}

// writeSessionContext writes SESSION_CONTEXT.md so skills can resolve paths.
func writeSessionContext(sessionDir string, req *ExecuteRequest, dbPath string) error {
	soulPath := filepath.Join(req.WorkspaceDir, "SOUL.md")
	identityPath := filepath.Join(req.WorkspaceDir, "IDENTITY.md")
	userPath := filepath.Join(req.WorkspaceDir, "USER.md")
	memoryPath := filepath.Join(req.WorkspaceDir, "MEMORY.md")
	heartbeatPath := filepath.Join(req.WorkspaceDir, "HEARTBEAT.md")

	content := fmt.Sprintf(`# Session Context

- App ID: %s
- Current date: %s
- Home: %s
- Workspace: %s
- Memory dir: %s
- Memory lock: %s
- Tasks dir: %s
- Session ID: %s
- Session dir: %s
- Attachments dir: %s
- Channel key: %s
- DB path: %s

## Core file absolute paths
- SOUL: %s
- IDENTITY: %s
- USER: %s
- MEMORY: %s
- HEARTBEAT: %s
`,
		req.AppConfig.ID,
		time.Now().Format("2006-01-02"),
		req.WorkspaceDir,
		req.WorkspaceDir,
		filepath.Join(req.WorkspaceDir, "memory"),
		filepath.Join(req.WorkspaceDir, ".memory.lock"),
		filepath.Join(req.WorkspaceDir, "tasks"),
		req.SessionID,
		sessionDir,
		filepath.Join(sessionDir, "attachments"),
		req.ChannelKey,
		dbPath,
		soulPath,
		identityPath,
		userPath,
		memoryPath,
		heartbeatPath,
	)

	path := filepath.Join(sessionDir, "SESSION_CONTEXT.md")
	return os.WriteFile(path, []byte(content), 0o644)
}

// buildSystemPrompt assembles the workspace-specific system prompt from
// SOUL.md, IDENTITY.md, and a compact skills index.
func buildSystemPrompt(workspaceDir string) string {
	var sections []string

	soulPath := filepath.Join(workspaceDir, "SOUL.md")
	if data, err := os.ReadFile(soulPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		sections = append(sections, string(data))
	}

	identityPath := filepath.Join(workspaceDir, "IDENTITY.md")
	if data, err := os.ReadFile(identityPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		sections = append(sections, string(data))
	}

	if idx := buildFlatSkillIndex(workspaceDir); idx != "" {
		sections = append(sections, idx)
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n---\n\n")
}

// buildFlatSkillIndex scans workspaceDir/skills/*.md and returns a compact index.
func buildFlatSkillIndex(workspaceDir string) string {
	skillsDir := filepath.Join(workspaceDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("## 可用技能索引\n\n")
	sb.WriteString("需要时通过 `Read` 工具读取完整内容（例如 `Read`：`skills/<name>.md`）。\n\n")
	for _, n := range names {
		base := strings.TrimSuffix(n, filepath.Ext(n))
		sb.WriteString(fmt.Sprintf("- **%s** (`skills/%s`)\n", base, n))
	}
	return sb.String()
}

// channelKeyToRoutingKey converts a channel_key to a feishu_ops routing_key.
//
// channel_key formats (internal):
//
//	p2p:{chat_id}:{app_id}              → p2p:{chat_id}
//	group:{chat_id}:{app_id}            → group:{chat_id}
//	thread:{chat_id}:{thread_id}:{app_id} → group:{chat_id}  (send target is the chat)
func channelKeyToRoutingKey(channelKey string) string {
	parts := strings.SplitN(channelKey, ":", 4)
	switch parts[0] {
	case "p2p":
		if len(parts) >= 2 {
			return "p2p:" + parts[1]
		}
	case "group":
		if len(parts) >= 2 {
			return "group:" + parts[1]
		}
	case "thread":
		// thread:{chat_id}:{thread_id}:{app_id} → group:{chat_id}
		if len(parts) >= 2 {
			return "group:" + parts[1]
		}
	}
	return channelKey
}

func permissionMode(appCfg *config.AppConfig) string {
	if appCfg.Claude.PermissionMode != "" {
		return appCfg.Claude.PermissionMode
	}
	return "acceptEdits"
}

// filterEnv returns a copy of env with all entries whose key starts with
// the given prefix removed. This prevents inherited env vars from shadowing
// values we explicitly set for the subprocess.
func filterEnv(env []string, prefix string) []string {
	result := make([]string, 0, len(env))
	for _, e := range env {
		if k, _, ok := strings.Cut(e, "="); ok && strings.HasPrefix(k, prefix) {
			continue
		}
		result = append(result, e)
	}
	return result
}
