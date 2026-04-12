package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/model"
	"github.com/ashwinyue/Memknow/internal/workspace"
)

// InteractiveExecutor manages long-running Claude CLI sessions keyed by Memknow session ID.
// It implements ExecutorInterface.
type InteractiveExecutor struct {
	cfg      *config.Config
	mu       sync.RWMutex
	sessions map[string]*interactiveSession // key = ExecuteRequest.SessionID

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewInteractiveExecutor creates the executor.
func NewInteractiveExecutor(cfg *config.Config) *InteractiveExecutor {
	return &InteractiveExecutor{
		cfg:      cfg,
		sessions: make(map[string]*interactiveSession),
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background reaper that cleans up dead sessions.
func (e *InteractiveExecutor) Start(ctx context.Context) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.reapDeadSessions()
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			}
		}
	}()
}

// Stop shuts down the background reaper and all managed sessions.
func (e *InteractiveExecutor) Stop() {
	close(e.stopCh)
	e.wg.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.sessions {
		s.stop()
	}
}

// Execute sends a user message to the long-running Claude session and returns the final result.
func (e *InteractiveExecutor) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error) {
	sess := e.getOrCreateSession(req)
	if sess == nil {
		return nil, fmt.Errorf("interactive session unavailable")
	}

	if err := sess.sendUserMessage(req.Prompt); err != nil {
		// Mark session as dead so the next attempt recreates it.
		sess.alive.Store(false)
		return nil, fmt.Errorf("send user message: %w", err)
	}

	return sess.collectResult(ctx, req.OnProgress)
}

// getOrCreateSession returns an existing healthy session or starts a new Claude process.
// If a session exists but is dead, it attempts to restart with --resume.
// Returns nil when the new session could not be started.
func (e *InteractiveExecutor) getOrCreateSession(req *ExecuteRequest) *interactiveSession {
	e.mu.Lock()
	defer e.mu.Unlock()

	if s, ok := e.sessions[req.SessionID]; ok {
		if s.alive.Load() {
			return s
		}
		// Dead session: attempt recovery with resume.
		var resumeID string
		if sid, ok := s.claudeSessionID.Load().(string); ok {
			resumeID = sid
		}
		s.stop()
		delete(e.sessions, req.SessionID)
		req.ClaudeSessionID = resumeID
		slog.Warn("interactiveSession: restarting dead session", "session_id", req.SessionID, "resume_id", resumeID, "reason", s.deadReason)
	}

	sess := newInteractiveSession(e.cfg, req)
	if sess == nil {
		return nil
	}
	e.sessions[req.SessionID] = sess
	return sess
}

// RemoveSession ends and deletes a session from the cache.
func (e *InteractiveExecutor) RemoveSession(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.sessions[sessionID]; ok {
		s.stop()
		delete(e.sessions, sessionID)
	}
}

// reapDeadSessions stops and removes sessions that have exited.
func (e *InteractiveExecutor) reapDeadSessions() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, s := range e.sessions {
		if !s.alive.Load() {
			s.stop()
			delete(e.sessions, id)
		}
	}
}

// interactiveSession mirrors cc-connect's claudeSession.
type interactiveSession struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	stdin   io.WriteCloser
	stdinMu sync.Mutex

	events chan ProgressEvent
	done   chan struct{}

	sessionID       string       // Memknow session ID
	claudeSessionID atomic.Value // string
	alive           atomic.Bool
	deadReason      string

	// Accumulated state for the current turn
	textBuf          strings.Builder
	reasoningBuf     strings.Builder
	toolUses         []ToolUseRecord
	inputTokens      int64
	outputTokens     int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	model            string
	finishReason     string
}

func newInteractiveSession(cfg *config.Config, req *ExecuteRequest) *interactiveSession {
	ctx, cancel := context.WithCancel(context.Background())

	workspaceDir := req.WorkspaceDir
	if abs, err := filepath.Abs(workspaceDir); err == nil {
		workspaceDir = abs
	}

	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--permission-prompt-tool", "stdio",
		"--verbose",
		"--permission-mode", permissionMode(req.AppConfig),
		"--max-turns", fmt.Sprintf("%d", cfg.Claude.MaxTurns),
	}

	if req.ClaudeSessionID != "" {
		args = append(args, "--resume", req.ClaudeSessionID)
	}

	if m := strings.TrimSpace(req.AppConfig.Claude.Model); m != "" {
		args = append(args, "--model", m)
	}

	if len(req.AppConfig.Claude.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(req.AppConfig.Claude.AllowedTools, " "))
	}

	sessionDir := workspace.SessionDir(workspaceDir, model.NormalizeSessionType(req.SessionType), req.SessionID)
	_ = os.MkdirAll(filepath.Join(sessionDir, "attachments"), 0o755)
	reqForContext := *req
	reqForContext.WorkspaceDir = workspaceDir
	if err := writeSessionContext(sessionDir, &reqForContext, cfg.DBPath); err != nil {
		slog.Error("interactiveSession: failed to write session context", "err", err)
	}

	// Build and append the workspace system prompt via CLI flag instead of
	// writing a per-session CLAUDE.md file. This avoids stale copies and
	// keeps session directories clean.
	memoryDir := filepath.Join(workspaceDir, "memory")
	attachmentsDir := filepath.Join(sessionDir, "attachments")
	contextPath := filepath.Join(sessionDir, "SESSION_CONTEXT.md")

	basePrompt := renderBasePrompt(req.SessionType, workspaceDir, sessionDir, memoryDir, attachmentsDir, contextPath)

	systemPrompt := basePrompt
	if wsPrompt := buildSystemPrompt(workspaceDir); wsPrompt != "" {
		systemPrompt += "\n---\n\n" + wsPrompt
	}
	if systemPrompt != "" {
		args = append(args, "--append-system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = sessionDir
	setProcAttrs(cmd)

	env := filterEnv(filterEnv(os.Environ(), "CLAUDECODE"), "CLAUDE_CODE_")
	cmd.Env = append(env,
		"TERM=xterm-256color",
		"FORCE_COLOR=0",
		"WORKSPACE_DIR="+workspaceDir,
		"MEMKNOW_SESSION_DIR="+sessionDir,
		"MEMKNOW_SESSION_CONTEXT="+filepath.Join(sessionDir, "SESSION_CONTEXT.md"),
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		slog.Error("interactiveSession: stdin pipe failed", "err", err)
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		slog.Error("interactiveSession: stdout pipe failed", "err", err)
		return nil
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		slog.Error("interactiveSession: start failed", "err", err)
		return nil
	}

	is := &interactiveSession{
		cmd:       cmd,
		cancel:    cancel,
		stdin:     stdin,
		events:    make(chan ProgressEvent, 64),
		done:      make(chan struct{}),
		sessionID: req.SessionID,
	}
	is.alive.Store(true)
	if req.ClaudeSessionID != "" {
		is.claudeSessionID.Store(req.ClaudeSessionID)
	}

	go is.readLoop(stdout, &stderrBuf)
	return is
}

// sendUserMessage writes a user event to Claude's stdin.
func (is *interactiveSession) sendUserMessage(prompt string) error {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
			},
		},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	is.stdinMu.Lock()
	defer is.stdinMu.Unlock()

	if _, err := fmt.Fprintln(is.stdin, string(b)); err != nil {
		return fmt.Errorf("stdin write: %w", err)
	}
	return nil
}

// collectResult drains events until a result event or timeout/cancellation.
func (is *interactiveSession) collectResult(ctx context.Context, onProgress func(ProgressEvent)) (*ExecuteResult, error) {
	result := &ExecuteResult{}
	is.resetTurnState()

	for {
		select {
		case evt, ok := <-is.events:
			if !ok {
				return result, fmt.Errorf("session closed unexpectedly")
			}

			switch evt.Kind {
			case ProgressText:
				result.Text += evt.Text
				is.textBuf.WriteString(evt.Text)
				if onProgress != nil {
					onProgress(evt)
				}
			case ProgressThinking:
				if is.reasoningBuf.Len() > 0 {
					is.reasoningBuf.WriteByte('\n')
				}
				is.reasoningBuf.WriteString(evt.Text)
				if onProgress != nil {
					onProgress(evt)
				}
			case ProgressToolUse:
				is.toolUses = append(is.toolUses, ToolUseRecord{
					Name:  evt.ToolName,
					Input: evt.ToolInput,
				})
				if onProgress != nil {
					onProgress(evt)
				}
			case ProgressToolResult:
				if len(is.toolUses) > 0 && is.toolUses[len(is.toolUses)-1].Output == "" {
					is.toolUses[len(is.toolUses)-1].Output = evt.Text
				}
				if onProgress != nil {
					onProgress(evt)
				}
			case ProgressResult:
				result.Reasoning = strings.TrimSpace(is.reasoningBuf.String())
				result.ToolUses = is.toolUses
				result.InputTokens = is.inputTokens
				result.OutputTokens = is.outputTokens
				result.CacheReadTokens = is.cacheReadTokens
				result.CacheWriteTokens = is.cacheWriteTokens
				result.Model = is.model
				result.FinishReason = is.finishReason
				if sid, ok := is.claudeSessionID.Load().(string); ok {
					result.ClaudeSessionID = sid
				}
				return result, nil
			}

		case <-is.done:
			result.Reasoning = strings.TrimSpace(is.reasoningBuf.String())
			result.ToolUses = is.toolUses
			result.InputTokens = is.inputTokens
			result.OutputTokens = is.outputTokens
			result.CacheReadTokens = is.cacheReadTokens
			result.CacheWriteTokens = is.cacheWriteTokens
			result.Model = is.model
			result.FinishReason = is.finishReason
			if sid, ok := is.claudeSessionID.Load().(string); ok {
				result.ClaudeSessionID = sid
			}
			return result, nil

		case <-ctx.Done():
			is.stop()
			for {
				select {
				case _, ok := <-is.events:
					if !ok {
						return result, ctx.Err()
					}
				case <-is.done:
					return result, ctx.Err()
				}
			}
		}
	}
}

func (is *interactiveSession) resetTurnState() {
	is.textBuf.Reset()
	is.reasoningBuf.Reset()
	is.toolUses = nil
	is.inputTokens = 0
	is.outputTokens = 0
	is.cacheReadTokens = 0
	is.cacheWriteTokens = 0
	is.model = ""
	is.finishReason = ""
}

// readLoop is heavily inspired by cc-connect/session.go.
func (is *interactiveSession) readLoop(stdout io.ReadCloser, stderrBuf *bytes.Buffer) {
	defer func() {
		is.alive.Store(false)
		if err := is.cmd.Wait(); err != nil {
			stderrMsg := strings.TrimSpace(stderrBuf.String())
			if stderrMsg != "" {
				is.deadReason = fmt.Sprintf("process exited: %v (stderr: %s)", err, stderrMsg)
				slog.Error("interactiveSession: process failed", "error", err, "stderr", stderrMsg)
			} else {
				is.deadReason = fmt.Sprintf("process exited: %v", err)
			}
		} else {
			is.deadReason = "process exited normally"
		}
		close(is.events)
		close(is.done)
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			slog.Debug("interactiveSession: non-JSON line", "line", line)
			continue
		}

		eventType, _ := raw["type"].(string)
		switch eventType {
		case "system":
			is.handleSystem(raw)
		case "assistant":
			is.handleAssistant(raw)
		case "user":
			is.handleUser(raw)
		case "result":
			is.handleResult(raw)
		case "control_request":
			is.handleControlRequest(raw)
		case "control_cancel_request":
			// Ignored for now.
		}
	}

	if err := scanner.Err(); err != nil {
		is.deadReason = fmt.Sprintf("scanner error: %v", err)
		slog.Error("interactiveSession: scanner error", "error", err)
	}
}

func (is *interactiveSession) handleSystem(raw map[string]any) {
	if sid, ok := raw["session_id"].(string); ok && sid != "" {
		is.claudeSessionID.Store(sid)
	}
}

func (is *interactiveSession) handleAssistant(raw map[string]any) {
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		return
	}
	contentArr, ok := msg["content"].([]any)
	if !ok {
		return
	}

	for _, item := range contentArr {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := itemMap["type"].(string)
		switch typ {
		case "text":
			if text, ok := itemMap["text"].(string); ok && text != "" {
				select {
				case is.events <- ProgressEvent{Kind: ProgressText, Text: text}:
				case <-is.done:
					return
				}
			}
		case "thinking":
			if thinking, ok := itemMap["thinking"].(string); ok && thinking != "" {
				select {
				case is.events <- ProgressEvent{Kind: ProgressThinking, Text: thinking}:
				case <-is.done:
					return
				}
			}
		case "tool_use":
			toolName, _ := itemMap["name"].(string)
			inputJSON, _ := json.Marshal(itemMap["input"])
			select {
			case is.events <- ProgressEvent{Kind: ProgressToolUse, ToolName: toolName, ToolInput: string(inputJSON)}:
			case <-is.done:
				return
			}
		}
	}
}

func (is *interactiveSession) handleUser(raw map[string]any) {
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		return
	}
	contentArr, ok := msg["content"].([]any)
	if !ok {
		return
	}

	for _, item := range contentArr {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := itemMap["type"].(string)
		if typ == "tool_result" {
			text := extractToolResultText(itemMap)
			select {
			case is.events <- ProgressEvent{Kind: ProgressToolResult, Text: text}:
			case <-is.done:
				return
			}
		}
	}
}

// extractToolResultText tries several known field/layouts for tool_result content.
func extractToolResultText(itemMap map[string]any) string {
	// 1. "output" field (used by Claude CLI in some stream-json variants).
	if out, ok := itemMap["output"]; ok {
		switch v := out.(type) {
		case string:
			return v
		default:
			b, _ := json.Marshal(v)
			return string(b)
		}
	}

	// 2. "content" as plain string.
	if c, ok := itemMap["content"].(string); ok {
		return c
	}

	// 3. "content" as array of text blocks (Anthropic API native shape).
	if arr, ok := itemMap["content"].([]any); ok {
		var parts []string
		for _, elem := range arr {
			switch v := elem.(type) {
			case string:
				parts = append(parts, v)
			case map[string]any:
				if t, ok := v["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "")
	}

	return ""
}

func (is *interactiveSession) handleResult(raw map[string]any) {
	if usage, ok := raw["usage"].(map[string]any); ok {
		if in, ok := usage["input_tokens"].(float64); ok {
			is.inputTokens = int64(in)
		}
		if out, ok := usage["output_tokens"].(float64); ok {
			is.outputTokens = int64(out)
		}
		// Try Anthropic-style cache keys first, then simplified variants.
		write := 0.0
		if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
			write = v
		} else if v, ok := usage["cache_write_tokens"].(float64); ok {
			write = v
		}
		is.cacheWriteTokens = int64(write)

		read := 0.0
		if v, ok := usage["cache_read_input_tokens"].(float64); ok {
			read = v
		} else if v, ok := usage["cache_read_tokens"].(float64); ok {
			read = v
		}
		is.cacheReadTokens = int64(read)
	}
	if m, ok := raw["model"].(string); ok {
		is.model = m
	}
	if sr, ok := raw["stop_reason"].(string); ok {
		is.finishReason = sr
	}
	select {
	case is.events <- ProgressEvent{Kind: ProgressResult}:
	case <-is.done:
	}
}

func (is *interactiveSession) handleControlRequest(raw map[string]any) {
	requestID, _ := raw["request_id"].(string)
	is.sendControlResponse(requestID, true, "")
}

func (is *interactiveSession) sendControlResponse(requestID string, approved bool, reason string) {
	if requestID == "" {
		return
	}
	behavior := "allow"
	if !approved {
		behavior = "deny"
	}
	resp := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response": map[string]any{
				"behavior": behavior,
				"message":  reason,
			},
		},
	}
	b, _ := json.Marshal(resp)
	is.stdinMu.Lock()
	defer is.stdinMu.Unlock()
	if is.stdin == nil {
		return
	}
	if _, err := fmt.Fprintln(is.stdin, string(b)); err != nil {
		slog.Warn("interactiveSession: control response write failed", "request_id", requestID, "error", err)
	}
}

func (is *interactiveSession) stop() {
	if is.cancel != nil {
		is.cancel()
	}
	if is.cmd != nil && is.cmd.Process != nil {
		_ = is.cmd.Process.Kill()
	}
}
