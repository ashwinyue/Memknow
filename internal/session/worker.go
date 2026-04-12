package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/feishu"
	"github.com/ashwinyue/Memknow/internal/model"
	"github.com/ashwinyue/Memknow/internal/workspace"
)

const (
	statusActive   = "active"
	statusArchived = "archived"
)

// Worker processes messages for a single channel serially.
// It is lazily started on first message and exits after idleTimeout.
type Worker struct {
	channelKey  string
	appCfg      *config.AppConfig
	sessionCfg  config.SessionConfig
	cfg         workspaceModeSetter
	db          *gorm.DB
	executor    claude.ExecutorInterface
	sender      sender
	idleTimeout time.Duration
	summarizer  *Summarizer
	retriever   *Retriever
	scheduleMgr scheduleManager

	queue  chan *feishu.IncomingMessage
	stopCh chan struct{}

	currentRunMu     sync.Mutex
	currentRunID     int64
	currentRunCancel context.CancelFunc

	// pendingAttachmentPrompts caches prompts from attachment-only messages.
	// When the next text message arrives, these are prepended to form a
	// combined prompt before sending to Claude.
	pendingAttachmentPrompts []string
}

type sender interface {
	SendThinking(ctx context.Context, receiveID string, receiveIDType string) (string, error)
	UpdateCard(ctx context.Context, messageID string, text string) error
	SendText(ctx context.Context, receiveID string, receiveIDType string, text string) (string, error)
	AddProcessingReaction(ctx context.Context, messageID string) (string, error)
	RemoveProcessingReaction(ctx context.Context, messageID, reactionID string) error
}

type scheduleManager interface {
	CreateFromMessage(ctx context.Context, appCfg *config.AppConfig, msg *feishu.IncomingMessage) (*model.Schedule, bool, error)
	ManageFromMessage(ctx context.Context, appCfg *config.AppConfig, msg *feishu.IncomingMessage) (string, bool, error)
}

type workspaceModeSetter interface {
	SetAppWorkspaceMode(appID, mode string) error
}

func newWorker(
	channelKey string,
	appCfg *config.AppConfig,
	cfg workspaceModeSetter,
	sessionCfg config.SessionConfig,
	db *gorm.DB,
	executor claude.ExecutorInterface,
	sender sender,
	idleTimeout time.Duration,
	summarizer *Summarizer,
	retriever *Retriever,
	scheduleMgr scheduleManager,
) *Worker {
	return &Worker{
		channelKey:  channelKey,
		appCfg:      appCfg,
		sessionCfg:  sessionCfg,
		cfg:         cfg,
		db:          db,
		executor:    executor,
		sender:      sender,
		idleTimeout: idleTimeout,
		summarizer:  summarizer,
		retriever:   retriever,
		scheduleMgr: scheduleMgr,
		queue:       make(chan *feishu.IncomingMessage, 64),
		stopCh:      make(chan struct{}),
	}
}

// run is the worker's main goroutine. It blocks until idle or ctx done.
func (w *Worker) run(ctx context.Context, onExit func()) {
	defer onExit()

	timer := time.NewTimer(w.idleTimeout)
	defer timer.Stop()

	slog.Info("session worker started", "channel", w.channelKey)

	for {
		select {
		case msg := <-w.queue:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.idleTimeout)
			w.process(ctx, msg)

		case <-timer.C:
			slog.Info("session worker idle timeout, exiting (session kept active)", "channel", w.channelKey)
			return

		case <-w.stopCh:
			return

		case <-ctx.Done():
			return
		}
	}
}

// isAttachmentOnly reports whether the prompt contains only attachment
// placeholders with no additional user text.
func isAttachmentOnly(prompt string) bool {
	cleaned := prompt
	for _, prefix := range []string{"[图片: ", "[文件: "} {
		for {
			idx := strings.Index(cleaned, prefix)
			if idx < 0 {
				break
			}
			end := strings.IndexByte(cleaned[idx:], ']')
			if end < 0 {
				break
			}
			cleaned = cleaned[:idx] + cleaned[idx+end+1:]
		}
	}
	return strings.TrimSpace(cleaned) == ""
}

// attachmentReplyText returns the acknowledgement text for an attachment-only message.
func attachmentReplyText(prompt string) string {
	hasImage := strings.Contains(prompt, "[图片: ")
	hasFile := strings.Contains(prompt, "[文件: ")
	switch {
	case hasImage && hasFile:
		return "已收到图片/文件，请描述你希望我做什么"
	case hasImage:
		return "已收到图片，请描述你希望我做什么"
	default:
		return "已收到文件，请描述你希望我做什么"
	}
}

// process handles a single incoming message.
func (w *Worker) process(ctx context.Context, msg *feishu.IncomingMessage) {
	trimmed := strings.TrimSpace(msg.Prompt)
	if w.handleBuiltInCommands(ctx, msg, trimmed) {
		return
	}

	// Add processing reaction as early feedback right after commands are handled.
	reactionID := ""
	if rid, rerr := w.sender.AddProcessingReaction(ctx, msg.MessageID); rerr != nil {
		slog.Warn("add processing reaction", "err", rerr)
	} else {
		reactionID = rid
	}
	defer func() {
		if reactionID != "" {
			if err := w.sender.RemoveProcessingReaction(ctx, msg.MessageID, reactionID); err != nil {
				slog.Warn("remove processing reaction", "err", err)
			}
		}
	}()

	if !w.shouldProceedAfterProbe(ctx, msg) {
		return
	}

	sess, originalPrompt, proceed := w.preparePromptWithContext(ctx, msg)
	if !proceed {
		return
	}

	if handled, err := w.interceptScheduleOps(ctx, sess, msg, originalPrompt); handled {
		if err != nil {
			w.replyError(ctx, msg, "", err)
		}
		return
	}

	cardMsgID, streamer, result, err := w.executeWithStreaming(ctx, sess, msg)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			w.replyCanceled(ctx, msg, cardMsgID, result.Text)
			return
		}
		w.replyError(ctx, msg, cardMsgID, err)
		return
	}
	w.finishAndSendResult(ctx, sess, msg, cardMsgID, streamer, result, originalPrompt)
}

func (w *Worker) beginCurrentRun(ctx context.Context) (context.Context, func()) {
	runCtx, cancel := context.WithCancel(ctx)
	w.currentRunMu.Lock()
	w.currentRunID++
	runID := w.currentRunID
	w.currentRunCancel = cancel
	w.currentRunMu.Unlock()
	return runCtx, func() {
		w.currentRunMu.Lock()
		if w.currentRunID == runID {
			w.currentRunCancel = nil
		}
		w.currentRunMu.Unlock()
		cancel()
	}
}

func (w *Worker) stopCurrentExecution() bool {
	w.currentRunMu.Lock()
	cancel := w.currentRunCancel
	if cancel != nil {
		w.currentRunCancel = nil
	}
	w.currentRunMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (w *Worker) shouldProceedAfterProbe(ctx context.Context, msg *feishu.IncomingMessage) bool {
	if !w.shouldProbe(msg) {
		return true
	}
	shouldRespond, err := w.runProbe(ctx, msg)
	if err != nil {
		slog.Warn("probe failed; falling back to main reply flow", "channel", w.channelKey, "message_id", msg.MessageID, "err", err)
		return true
	}
	return shouldRespond
}

func (w *Worker) shouldProbe(msg *feishu.IncomingMessage) bool {
	if msg == nil || !w.sessionCfg.Probe.Enabled {
		return false
	}
	switch msg.ChatType {
	case "group", "topic_group", "topic":
		return !msg.MentionsMe && !msg.RepliesToMe && !msg.NamesMe
	default:
		return false
	}
}

func (w *Worker) runProbe(ctx context.Context, msg *feishu.IncomingMessage) (bool, error) {
	sessionID := probeSessionID(msg)
	if ie, ok := w.executor.(*claude.InteractiveExecutor); ok {
		defer ie.RemoveSession(sessionID)
	}
	result, err := w.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:       buildProbePrompt(msg.Prompt),
		SessionID:    sessionID,
		SessionType:  model.SessionTypeChat,
		AppConfig:    w.appCfg,
		WorkspaceDir: w.appCfg.WorkspaceDir,
		ChannelKey:   w.channelKey,
		SenderID:     msg.SenderID,
	})
	if err != nil {
		return false, err
	}
	if result == nil {
		return false, nil
	}
	return strings.TrimSpace(result.Text) == "RESPOND", nil
}

func buildProbePrompt(userPrompt string) string {
	return strings.TrimSpace(`你正在一个小群里旁听。请判断这条最新群消息是否明显需要你主动参与。

规则：
- 不是每条消息都需要你回应，默认保持静默
- 只有明显值得你参与时才输出 RESPOND，比如有人在直接问你、明显需要 AI 介入、或你补充后会明显提高对话价值
- 如果只是普通闲聊、别人之间对话、只是顺手提到你、与您无关、或你不确定是否该插话，输出 IGNORE
- 当你拿不准时，选择 IGNORE，不要抢话
- 不要解释，不要输出别的内容，只能输出单个单词：RESPOND 或 IGNORE

最新群消息：
` + userPrompt)
}

func probeSessionID(msg *feishu.IncomingMessage) string {
	if msg == nil {
		return "probe:unknown"
	}
	return fmt.Sprintf("probe:%s:%s", msg.ChannelKey, msg.MessageID)
}

func (w *Worker) handleBuiltInCommands(ctx context.Context, msg *feishu.IncomingMessage, trimmed string) bool {
	if trimmed == "/new" {
		w.pendingAttachmentPrompts = nil
		w.handleNew(ctx, msg)
		return true
	}
	if w.handleModeCommand(ctx, msg, trimmed) {
		return true
	}
	if strings.HasPrefix(trimmed, "/search ") {
		w.handleSearch(ctx, msg, strings.TrimPrefix(trimmed, "/search "))
		return true
	}
	return false
}

func (w *Worker) handleModeCommand(ctx context.Context, msg *feishu.IncomingMessage, trimmed string) bool {
	mode, showOnly, validArg, ok := parseModeCommand(trimmed)
	if !ok {
		return false
	}
	if showOnly {
		w.sendModeReply(ctx, msg, "当前模式："+w.appCfg.NormalizedWorkspaceMode()+"\n可用命令：`/mode work`、`/mode companion`、`/work`、`/comp`")
		return true
	}
	if !validArg {
		w.sendModeReply(ctx, msg, "无效模式。可用命令：`/mode work`、`/mode companion`、`/work`、`/comp`")
		return true
	}
	if w.cfg == nil || w.appCfg == nil {
		w.sendModeReply(ctx, msg, "模式切换失败：当前实例未配置可写配置源。")
		return true
	}
	if err := w.cfg.SetAppWorkspaceMode(w.appCfg.ID, mode); err != nil {
		w.sendModeReply(ctx, msg, "模式切换失败："+err.Error())
		return true
	}
	w.appCfg.WorkspaceMode = mode
	w.sendModeReply(ctx, msg, "已切换到 "+mode+" 模式。")
	return true
}

func parseModeCommand(trimmed string) (mode string, showOnly bool, validArg bool, ok bool) {
	switch strings.TrimSpace(trimmed) {
	case "/mode":
		return "", true, true, true
	case "/work":
		return "work", false, true, true
	case "/comp":
		return "companion", false, true, true
	}
	if !strings.HasPrefix(trimmed, "/mode ") {
		return "", false, false, false
	}
	arg := strings.TrimSpace(strings.TrimPrefix(trimmed, "/mode "))
	normalized, valid := config.NormalizeWorkspaceMode(arg)
	if !valid {
		return "", false, false, true
	}
	return normalized, false, true, true
}

func (w *Worker) sendModeReply(ctx context.Context, msg *feishu.IncomingMessage, text string) {
	if _, err := w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, text); err != nil {
		slog.Error("send mode reply", "err", err)
	}
}

func (w *Worker) preparePromptWithContext(ctx context.Context, msg *feishu.IncomingMessage) (sess *model.Session, originalPrompt string, proceed bool) {
	var err error
	sess, err = w.getOrCreateSession(msg.SenderID)
	if err != nil {
		slog.Error("get/create session", "err", err, "channel", w.channelKey)
		return nil, "", false
	}

	msg.Prompt = w.moveAttachments(msg.Prompt, sess)
	w.recordUserMessage(sess, msg)

	// Auto-generate title from the first substantive user message.
	if sess.Title == "" && !isAttachmentOnly(msg.Prompt) {
		w.maybeSetTitle(sess, msg.Prompt)
	}

	// Cache attachment-only messages and reply with a prompt for description.
	if isAttachmentOnly(msg.Prompt) {
		w.pendingAttachmentPrompts = append(w.pendingAttachmentPrompts, msg.Prompt)
		reply := attachmentReplyText(msg.Prompt)
		if _, err := w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, reply); err != nil {
			slog.Error("send attachment ack", "err", err)
		}
		return nil, "", false
	}

	// Merge any pending attachment references into the current prompt.
	if len(w.pendingAttachmentPrompts) > 0 {
		combined := strings.Join(w.pendingAttachmentPrompts, "\n") + "\n" + msg.Prompt
		msg.Prompt = combined
		w.pendingAttachmentPrompts = nil
	}

	originalPrompt = msg.Prompt

	// Inject historical context from archived sessions.
	if w.retriever != nil {
		payload, rerr := w.retriever.Retrieve(w.channelKey, originalPrompt)
		if rerr != nil {
			slog.Warn("retrieve context failed", "err", rerr)
		} else if !payload.IsEmpty() {
			msg.Prompt = payload.ToPrompt() + originalPrompt
		}
	}

	// Inject relevant memories dynamically.
	memories := w.retrieveMemories(originalPrompt)
	if len(memories) > 0 {
		var mb strings.Builder
		mb.WriteString("## 相关记忆\n\n")
		for _, m := range memories {
			mb.WriteString("- ")
			mb.WriteString(m)
			mb.WriteString("\n")
		}
		mb.WriteString("\n")
		msg.Prompt = mb.String() + msg.Prompt
	}
	return sess, originalPrompt, true
}

func (w *Worker) interceptScheduleOps(ctx context.Context, sess *model.Session, msg *feishu.IncomingMessage, originalPrompt string) (bool, error) {
	if handled, err := w.maybeCreateSchedule(ctx, sess, msg, originalPrompt); handled {
		return true, err
	}
	// Use originalPrompt so injected context/memories do not trigger false schedule intents.
	saved := msg.Prompt
	msg.Prompt = originalPrompt
	handled, err := w.maybeManageSchedule(ctx, sess, msg)
	msg.Prompt = saved
	if handled {
		return true, err
	}
	return false, nil
}

func (w *Worker) executeWithStreaming(
	ctx context.Context,
	sess *model.Session,
	msg *feishu.IncomingMessage,
) (cardMsgID string, streamer *compactCardStreamer, result *claude.ExecuteResult, err error) {
	runCtx, finishRun := w.beginCurrentRun(ctx)
	defer finishRun()

	if w.shouldShowVisibleProgress(msg) {
		cardMsgID = w.sendThinkingCard(runCtx, msg)
		if cardMsgID != "" {
			streamer = newCompactCardStreamer(runCtx, w.sender, cardMsgID)
		}
	}
	result, err = w.runClaude(runCtx, sess, msg, msg.Prompt, func(evt claude.ProgressEvent) {
		if cardMsgID != "" && evt.Kind == claude.ProgressToolUse {
			if streamer != nil {
				streamer.Close()
			}
			if nextCardID := w.sendThinkingCard(runCtx, msg); nextCardID != "" {
				cardMsgID = nextCardID
			}
			streamer = newCompactCardStreamer(runCtx, w.sender, cardMsgID)
		}
		if streamer != nil {
			streamer.OnProgress(evt)
		}
	})
	if streamer != nil {
		streamer.Close()
	}
	return cardMsgID, streamer, result, err
}

func (w *Worker) shouldShowVisibleProgress(msg *feishu.IncomingMessage) bool {
	if msg == nil {
		return false
	}
	if w.appCfg != nil && w.appCfg.NormalizedWorkspaceMode() == "companion" {
		return false
	}
	switch msg.ChatType {
	case "group", "topic_group", "topic":
		return false
	default:
		return true
	}
}

func (w *Worker) finishAndSendResult(
	ctx context.Context,
	sess *model.Session,
	msg *feishu.IncomingMessage,
	cardMsgID string,
	streamer *compactCardStreamer,
	result *claude.ExecuteResult,
	originalPrompt string,
) {
	if result == nil {
		return
	}

	w.persistResult(sess, result)

	// Graceful overflow recovery: if claude returns empty text, auto-summarize,
	// start a new session transparently, and retry with the summary.
	if result.Text == "" && w.summarizer != nil {
		summary, serr := w.summarizer.Summarize(sess.ID)
		if serr == nil && summary != "" {
			w.db.Create(&model.SessionSummary{
				ID:         uuid.New().String(),
				SessionID:  sess.ID,
				ChannelKey: w.channelKey,
				Content:    summary,
				CreatedAt:  time.Now(),
			})
			newSess, nerr := w.handleNewSilent(ctx, msg.SenderID)
			if nerr == nil {
				retryPrompt := "## 前序会话摘要\n" + summary + "\n\n## 用户问题\n" + originalPrompt
				// Do not reuse the closed streamer for the retry; just send the final result.
				retryResult, rerr := w.runClaude(ctx, newSess, msg, retryPrompt, nil)
				if rerr == nil && retryResult.Text != "" {
					w.persistResult(newSess, retryResult)
					w.sendResult(ctx, msg, cardMsgID, retryResult.Text)
					return
				}
			}
		}
		w.replyError(ctx, msg, cardMsgID,
			fmt.Errorf("AI 返回为空，可能是会话上下文过长。请发送 /new 开启新会话后重试"))
		return
	}

	finalText := result.Text
	if cardMsgID != "" && streamer != nil && !streamer.sawToolUsage() {
		if composed := streamer.renderFinalWithProgress(result.Text); composed != "" {
			finalText = composed
		}
	}
	w.sendResult(ctx, msg, cardMsgID, finalText)
}

func (w *Worker) maybeCreateSchedule(ctx context.Context, sess *model.Session, msg *feishu.IncomingMessage, originalPrompt string) (bool, error) {
	if w.scheduleMgr == nil {
		return false, nil
	}
	tmp := *msg
	tmp.Prompt = originalPrompt
	created, ok, err := w.scheduleMgr.CreateFromMessage(ctx, w.appCfg, &tmp)
	if err != nil || !ok {
		return ok, err
	}
	reply := fmt.Sprintf("✅ 已创建定时任务「%s」\n执行频率：`%s`", created.Name, created.CronExpr)
	if _, sendErr := w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, reply); sendErr != nil {
		return true, sendErr
	}
	w.recordMessage(&model.Message{
		SessionID: sess.ID,
		Role:      "assistant",
		Content:   reply,
	})
	return true, nil
}

func (w *Worker) maybeManageSchedule(ctx context.Context, sess *model.Session, msg *feishu.IncomingMessage) (bool, error) {
	if w.scheduleMgr == nil {
		return false, nil
	}
	reply, ok, err := w.scheduleMgr.ManageFromMessage(ctx, w.appCfg, msg)
	if err != nil || !ok {
		return ok, err
	}
	if _, sendErr := w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, reply); sendErr != nil {
		return true, sendErr
	}
	w.recordMessage(&model.Message{
		SessionID: sess.ID,
		Role:      "assistant",
		Content:   reply,
	})
	return true, nil
}

// runClaude invokes the Claude executor and returns the result.
func (w *Worker) runClaude(
	ctx context.Context,
	sess *model.Session,
	msg *feishu.IncomingMessage,
	prompt string,
	onProgress func(claude.ProgressEvent),
) (*claude.ExecuteResult, error) {
	return w.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:          prompt,
		SessionID:       sess.ID,
		SessionType:     sess.Type,
		ClaudeSessionID: sess.ClaudeSessionID,
		AppConfig:       w.appCfg,
		WorkspaceDir:    w.appCfg.WorkspaceDir,
		ChannelKey:      w.channelKey,
		SenderID:        msg.SenderID,
		OnProgress:      onProgress,
	})
}

// sendThinkingCard posts the initial "thinking..." card and returns its message ID.
func (w *Worker) sendThinkingCard(ctx context.Context, msg *feishu.IncomingMessage) string {
	cardMsgID, err := w.sender.SendThinking(ctx, msg.ReceiveID, msg.ReceiveType)
	if err != nil {
		slog.Error("send thinking card", "err", err)
	}
	return cardMsgID
}

// persistResult saves the claude_session_id, assistant message, tool uses and session metadata.
func (w *Worker) persistResult(sess *model.Session, result *claude.ExecuteResult) {
	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}
	if result.ClaudeSessionID != "" {
		updates["claude_session_id"] = result.ClaudeSessionID
	}
	if result.InputTokens > 0 {
		updates["input_tokens"] = gorm.Expr("input_tokens + ?", result.InputTokens)
	}
	if result.OutputTokens > 0 {
		updates["output_tokens"] = gorm.Expr("output_tokens + ?", result.OutputTokens)
	}
	if result.CacheReadTokens > 0 {
		updates["cache_read_tokens"] = gorm.Expr("cache_read_tokens + ?", result.CacheReadTokens)
	}
	if result.CacheWriteTokens > 0 {
		updates["cache_write_tokens"] = gorm.Expr("cache_write_tokens + ?", result.CacheWriteTokens)
	}
	if result.CostUSD > 0 {
		updates["estimated_cost_usd"] = gorm.Expr("COALESCE(estimated_cost_usd, 0) + ?", result.CostUSD)
	}
	if result.Model != "" && sess.Model == "" {
		updates["model"] = result.Model
	}
	if len(updates) > 1 {
		if err := w.db.Model(sess).Updates(updates).Error; err != nil {
			slog.Error("update session metadata", "err", err)
		}
	}

	assistantMsgID := uuid.New().String()
	w.recordMessage(&model.Message{
		ID:           assistantMsgID,
		SessionID:    sess.ID,
		Role:         "assistant",
		Content:      result.Text,
		Reasoning:    result.Reasoning,
		TokenCount:   int(result.OutputTokens),
		FinishReason: result.FinishReason,
	})

	// Record individual tool result messages for completeness.
	for i, tu := range result.ToolUses {
		callID := fmt.Sprintf("call_%d", i)
		if err := w.db.Create(&model.MessageToolCall{
			ID:         uuid.New().String(),
			SessionID:  sess.ID,
			MessageID:  assistantMsgID,
			CallID:     callID,
			Name:       tu.Name,
			Input:      tu.Input,
			Output:     tu.Output,
			OrderIndex: i,
			CreatedAt:  time.Now(),
		}).Error; err != nil {
			slog.Warn("create message tool call", "session_id", sess.ID, "call_id", callID, "err", err)
		}
		if tu.Output != "" {
			w.recordMessage(&model.Message{
				ID:         uuid.New().String(),
				SessionID:  sess.ID,
				Role:       "tool",
				ToolName:   tu.Name,
				Content:    tu.Output,
				ToolCallID: callID,
			})
		}
	}
}

// sendResult updates the card or sends a plain text message with the final result.
func (w *Worker) sendResult(ctx context.Context, msg *feishu.IncomingMessage, cardMsgID, text string) {
	if text == "" {
		return // claude chose not to respond (expected in group chats)
	}
	if cardMsgID != "" {
		if err := w.sender.UpdateCard(ctx, cardMsgID, text); err != nil {
			slog.Error("update card", "err", err)
		}
		return
	}
	if _, err := w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, text); err != nil {
		slog.Error("send text", "err", err)
	}
}

// replyError surfaces execution errors to the user.
func (w *Worker) replyError(ctx context.Context, msg *feishu.IncomingMessage, cardMsgID string, err error) {
	slog.Error("claude execute", "err", err)
	reply := fmt.Sprintf("❌ 执行出错：%s", err.Error())
	if cardMsgID != "" {
		_ = w.sender.UpdateCard(ctx, cardMsgID, reply)
		return
	}
	_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, reply)
}

func (w *Worker) replyCanceled(ctx context.Context, msg *feishu.IncomingMessage, cardMsgID string, partialText string) {
	reply := "已停止当前执行。"
	if partialText != "" {
		reply = partialText + "\n\n〔已停止〕"
	}
	if cardMsgID != "" {
		_ = w.sender.UpdateCard(ctx, cardMsgID, reply)
		return
	}
	_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, reply)
}

// recordUserMessage writes a user message with full metadata.
func (w *Worker) recordUserMessage(sess *model.Session, msg *feishu.IncomingMessage) {
	w.recordMessage(&model.Message{
		ID:          uuid.New().String(),
		SessionID:   sess.ID,
		SenderID:    msg.SenderID,
		Role:        "user",
		Content:     msg.Prompt,
		FeishuMsgID: msg.MessageID,
	})
}

// recordMessage writes a message record to DB. Errors are logged, not propagated.
func (w *Worker) recordMessage(m *model.Message) {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	if err := w.db.Create(m).Error; err != nil {
		slog.Error("create message", "role", m.Role, "err", err)
	}
}

// maybeSetTitle generates a title from the first user message and updates the session.
func (w *Worker) maybeSetTitle(sess *model.Session, prompt string) {
	title := generateTitle(prompt)
	if title == "" {
		return
	}
	if err := w.db.Model(sess).Update("title", title).Error; err != nil {
		slog.Error("update session title", "err", err)
	} else {
		sess.Title = title
	}
}

// generateTitle extracts a concise title from a prompt (max 40 chars).
func generateTitle(prompt string) string {
	// Strip attachment markers.
	s := prompt
	for _, prefix := range []string{"[图片: ", "[文件: "} {
		for {
			idx := strings.Index(s, prefix)
			if idx < 0 {
				break
			}
			end := strings.IndexByte(s[idx:], ']')
			if end < 0 {
				break
			}
			s = s[:idx] + s[idx+end+1:]
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// First line only.
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	// Limit length.
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return s
}

// handleNew archives the current session and creates a new one, then acks the user.
// Summary generation is done asynchronously so the user gets instant feedback.
func (w *Worker) handleNew(ctx context.Context, msg *feishu.IncomingMessage) {
	var oldSess model.Session
	w.db.Where("channel_key = ? AND status = ?", w.channelKey, statusActive).
		Order("created_at DESC").
		First(&oldSess)

	if _, err := w.handleNewSilent(ctx, msg.SenderID); err != nil {
		slog.Error("handleNew failed", "err", err)
	}
	_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, "✅ 已开启新会话")

	if oldSess.ID != "" {
		go w.maybeAsyncSummarize(oldSess.ID)
	}
}

// maybeAsyncSummarize generates a summary in the background if the session has
// enough messages to be worth summarizing.
func (w *Worker) maybeAsyncSummarize(sessionID string) {
	if w.summarizer == nil {
		return
	}
	var count int64
	w.db.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&count)
	if count < 3 {
		w.cleanupInteractiveSession(sessionID)
		return
	}
	if summary, err := w.summarizer.Summarize(sessionID); err == nil && summary != "" {
		w.db.Create(&model.SessionSummary{
			ID:         uuid.New().String(),
			SessionID:  sessionID,
			ChannelKey: w.channelKey,
			Content:    summary,
			CreatedAt:  time.Now(),
		})
	}
	w.cleanupInteractiveSession(sessionID)
}

// handleNewSilent archives the current session, cleans up the interactive
// process, and creates a new active session. It returns the newly created session.
func (w *Worker) handleNewSilent(ctx context.Context, senderID string) (*model.Session, error) {
	var oldSess model.Session
	w.db.Where("channel_key = ? AND status = ?", w.channelKey, statusActive).
		Order("created_at DESC").
		First(&oldSess)

	if err := w.db.Model(&model.Session{}).
		Where("channel_key = ? AND status = ?", w.channelKey, statusActive).
		Updates(map[string]interface{}{
			"status":     statusArchived,
			"updated_at": time.Now(),
		}).Error; err != nil {
		slog.Error("archive session on /new", "err", err)
	}

	if oldSess.ID != "" {
		w.cleanupInteractiveSession(oldSess.ID)
	}

	newID := uuid.New().String()
	sessionDir := workspace.SessionDir(w.appCfg.WorkspaceDir, model.SessionTypeChat, newID)
	if err := os.MkdirAll(filepath.Join(sessionDir, "attachments"), 0o755); err != nil {
		slog.Error("create new session dir", "err", err)
	}

	newSess := &model.Session{
		ID:              newID,
		ChannelKey:      w.channelKey,
		Type:            model.SessionTypeChat,
		Status:          statusActive,
		CreatedBy:       senderID,
		ParentSessionID: oldSess.ID,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := w.db.Create(newSess).Error; err != nil {
		return nil, fmt.Errorf("create new session: %w", err)
	}
	return newSess, nil
}

// handleSearch runs an FTS5 search across this channel's message history.
func (w *Worker) handleSearch(ctx context.Context, msg *feishu.IncomingMessage, query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, "请输入搜索内容，例如：/search 部署脚本")
		return
	}

	matches, err := searchMessagesWithStatus(w.db, w.channelKey, query, "", model.SessionTypeChat, 5)
	if err != nil {
		slog.Error("search messages", "err", err)
		_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, "搜索失败，请稍后重试")
		return
	}
	if len(matches) < 5 {
		likeMatches, likeErr := searchMessagesLikeWithStatus(w.db, w.channelKey, query, "", model.SessionTypeChat, 5)
		if likeErr != nil {
			slog.Warn("search messages fallback like failed", "err", likeErr)
		} else {
			candidates := make([]scoredMatch, 0, len(matches)+len(likeMatches))
			for i, m := range matches {
				candidates = append(candidates, scoredMatch{match: m, score: 100 - i})
			}
			for i, m := range likeMatches {
				candidates = append(candidates, scoredMatch{match: m, score: 70 - i})
			}
			matches = fuseScoredMatches(candidates, 5)
		}
	}

	if len(matches) == 0 {
		_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, "未找到相关历史消息")
		return
	}

	_, _ = w.sender.SendText(ctx, msg.ReceiveID, msg.ReceiveType, formatSearchResults(query, matches))
}

// getOrCreateSession returns the active session for this channel, creating one if needed.
func (w *Worker) getOrCreateSession(senderID string) (*model.Session, error) {
	var sess model.Session
	result := w.db.Where("channel_key = ? AND status = ?", w.channelKey, statusActive).
		Order("created_at DESC").
		First(&sess)

	if result.Error == nil {
		return &sess, nil
	}
	// C-3: use errors.Is for GORM sentinel errors.
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}

	newID := uuid.New().String()
	sess = model.Session{
		ID:         newID,
		ChannelKey: w.channelKey,
		Type:       model.SessionTypeChat,
		Status:     statusActive,
		CreatedBy:  senderID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := w.db.Create(&sess).Error; err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	sessionDir := workspace.SessionDir(w.appCfg.WorkspaceDir, model.SessionTypeChat, newID)
	if err := os.MkdirAll(filepath.Join(sessionDir, "attachments"), 0o755); err != nil {
		slog.Warn("create session dir", "session_id", newID, "err", err)
	}
	return &sess, nil
}

// cleanupInteractiveSession removes the long-running Claude process for a session.
func (w *Worker) cleanupInteractiveSession(sessionID string) {
	if ie, ok := w.executor.(*claude.InteractiveExecutor); ok {
		ie.RemoveSession(sessionID)
	}
}

// moveAttachments moves temporary attachment files into the session attachments directory
// and replaces their paths in the prompt string accordingly.
// M-9: correctly handles multiple attachments per type using offset-based iteration.
func (w *Worker) moveAttachments(prompt string, sess *model.Session) string {
	attachDir := workspace.SessionAttachmentsDir(w.appCfg.WorkspaceDir, sess.Type, sess.ID)
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		slog.Warn("moveAttachments: mkdir failed", "err", err)
		return prompt
	}

	result := prompt
	for _, prefix := range []string{"[图片: ", "[文件: "} {
		result = replacePaths(result, prefix, attachDir)
	}
	return result
}

// replacePaths rewrites all occurrences of [prefix <path>] in s, moving each
// <path> into attachDir. Already-moved paths (inside attachDir) are left as-is.
func replacePaths(s, prefix, attachDir string) string {
	var out strings.Builder
	remaining := s

	for {
		idx := strings.Index(remaining, prefix)
		if idx < 0 {
			out.WriteString(remaining)
			break
		}

		// Write everything up to and including the prefix.
		out.WriteString(remaining[:idx+len(prefix)])
		remaining = remaining[idx+len(prefix):]

		// Find the closing bracket.
		end := strings.IndexByte(remaining, ']')
		if end < 0 {
			// Malformed reference — emit the rest verbatim.
			out.WriteString(remaining)
			break
		}

		oldPath := remaining[:end]
		remaining = remaining[end:] // retains the ']' for the next iteration

		if strings.HasPrefix(oldPath, attachDir) {
			// Already in the right place.
			out.WriteString(oldPath)
			continue
		}

		newPath := filepath.Join(attachDir,
			fmt.Sprintf("%s_%s", uuid.New().String(), filepath.Base(oldPath)))
		if err := os.Rename(oldPath, newPath); err != nil {
			slog.Warn("move attachment", "src", oldPath, "err", err)
			out.WriteString(oldPath) // keep original path on failure
		} else {
			out.WriteString(newPath)
		}
	}
	return out.String()
}
