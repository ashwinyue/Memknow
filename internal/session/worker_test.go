package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/feishu"
	"github.com/ashwinyue/Memknow/internal/model"
)

// ── replacePaths ─────────────────────────────────────────────────────────────

func TestReplacePaths_SingleAttachment(t *testing.T) {
	srcDir := t.TempDir()
	attachDir := filepath.Join(t.TempDir(), "attachments")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a source file to "move".
	src := filepath.Join(srcDir, "image.jpg")
	if err := os.WriteFile(src, []byte("imgdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := fmt.Sprintf("[图片: %s]", src)
	result := replacePaths(prompt, "[图片: ", attachDir)

	// The new path should be inside attachDir.
	if !strings.Contains(result, attachDir) {
		t.Errorf("result should contain attachDir, got: %s", result)
	}

	// Original file should be gone (renamed).
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file should have been moved")
	}

	// A file should now exist in attachDir.
	entries, _ := os.ReadDir(attachDir)
	if len(entries) == 0 {
		t.Error("attachDir should contain the moved file")
	}
}

func TestReplacePaths_MultipleAttachments(t *testing.T) {
	srcDir := t.TempDir()
	attachDir := filepath.Join(t.TempDir(), "attachments")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two source files.
	src1 := filepath.Join(srcDir, "a.jpg")
	src2 := filepath.Join(srcDir, "b.jpg")
	for _, p := range []string{src1, src2} {
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Add a small sleep to ensure different UnixNano timestamps in filenames.
	prompt := fmt.Sprintf("[图片: %s] some text [图片: %s]", src1, src2)
	time.Sleep(time.Millisecond)
	result := replacePaths(prompt, "[图片: ", attachDir)

	// Both should be relocated.
	if strings.Contains(result, srcDir) {
		t.Errorf("result should not contain srcDir, got: %s", result)
	}

	entries, _ := os.ReadDir(attachDir)
	if len(entries) != 2 {
		t.Errorf("expected 2 files in attachDir, got %d", len(entries))
	}
}

func TestReplacePaths_AlreadyMoved(t *testing.T) {
	attachDir := filepath.Join(t.TempDir(), "attachments")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// File is already inside attachDir — should not be moved again.
	existing := filepath.Join(attachDir, "already.jpg")
	if err := os.WriteFile(existing, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := fmt.Sprintf("[图片: %s]", existing)
	result := replacePaths(prompt, "[图片: ", attachDir)

	// Result should still reference the same path.
	if !strings.Contains(result, existing) {
		t.Errorf("already-moved path should be preserved, got: %s", result)
	}

	// Only one file should still be in attachDir (not duplicated).
	entries, _ := os.ReadDir(attachDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file in attachDir, got %d", len(entries))
	}
}

func TestReplacePaths_NoAttachments(t *testing.T) {
	attachDir := t.TempDir()
	prompt := "just a plain text message with no attachments"

	result := replacePaths(prompt, "[图片: ", attachDir)
	if result != prompt {
		t.Errorf("result = %q, want %q", result, prompt)
	}
}

func TestReplacePaths_MalformedReference(t *testing.T) {
	attachDir := t.TempDir()

	// Missing closing bracket — should emit rest verbatim.
	prompt := "[图片: /some/path"
	result := replacePaths(prompt, "[图片: ", attachDir)

	if result == "" {
		t.Error("result should not be empty for malformed input")
	}
	// Should not panic and should contain the remaining content.
	if !strings.Contains(result, "/some/path") {
		t.Errorf("malformed result should retain path text, got: %s", result)
	}
}

func TestReplacePaths_MissingSourceFile(t *testing.T) {
	attachDir := filepath.Join(t.TempDir(), "attachments")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Source file doesn't exist — Rename will fail, original path should be kept.
	missingPath := "/nonexistent/path/file.jpg"
	prompt := fmt.Sprintf("[图片: %s]", missingPath)

	result := replacePaths(prompt, "[图片: ", attachDir)

	if !strings.Contains(result, missingPath) {
		t.Errorf("on rename failure, original path should be kept, got: %s", result)
	}
}

func TestReplacePaths_FilePrefix(t *testing.T) {
	srcDir := t.TempDir()
	attachDir := filepath.Join(t.TempDir(), "attachments")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(srcDir, "doc.pdf")
	if err := os.WriteFile(src, []byte("pdfdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := fmt.Sprintf("[文件: %s]", src)
	result := replacePaths(prompt, "[文件: ", attachDir)

	if !strings.Contains(result, attachDir) {
		t.Errorf("file attachment should be relocated to attachDir, got: %s", result)
	}
}

// ── isAttachmentOnly ────────────────────────────────────────────────────────

func TestIsAttachmentOnly(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"single image", "[图片: /path/a.jpg]", true},
		{"single file", "[文件: /path/b.pdf]", true},
		{"image and file", "[图片: /path/a.jpg]\n[文件: /path/b.pdf]", true},
		{"multiple images", "[图片: /path/a.jpg] [图片: /path/b.png]", true},
		{"text with image", "请分析这张图 [图片: /path/a.jpg]", false},
		{"image with text after", "[图片: /path/a.jpg] 分析一下", false},
		{"plain text", "你好", false},
		{"empty string", "", true},
		{"whitespace only", "  \n  ", true},
		{"image with surrounding whitespace", "  [图片: /path/a.jpg]  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAttachmentOnly(tt.prompt)
			if got != tt.want {
				t.Errorf("isAttachmentOnly(%q) = %v, want %v", tt.prompt, got, tt.want)
			}
		})
	}
}

// ── attachmentReplyText ─────────────────────────────────────────────────────

func TestAttachmentReplyText(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{"image only", "[图片: /path/a.jpg]", "已收到图片，请描述你希望我做什么"},
		{"file only", "[文件: /path/b.pdf]", "已收到文件，请描述你希望我做什么"},
		{"image and file", "[图片: /path/a.jpg]\n[文件: /path/b.pdf]", "已收到图片/文件，请描述你希望我做什么"},
		{"multiple images", "[图片: /a.jpg] [图片: /b.png]", "已收到图片，请描述你希望我做什么"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := attachmentReplyText(tt.prompt)
			if got != tt.want {
				t.Errorf("attachmentReplyText(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}

type testCardUpdater struct {
	mu      sync.Mutex
	updates []string
	err     error
}

func (u *testCardUpdater) UpdateCard(_ context.Context, _ string, text string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.updates = append(u.updates, text)
	return u.err
}

func (u *testCardUpdater) all() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]string, len(u.updates))
	copy(out, u.updates)
	return out
}

func TestCompactCardStreamer_ThrottlesAndFlushes(t *testing.T) {
	updater := &testCardUpdater{}
	now := time.Unix(100, 0)
	s := newCompactCardStreamer(context.Background(), updater, "msg-1")
	s.minInterval = 500 * time.Millisecond
	s.debounceDelay = 0
	s.now = func() time.Time { return now }

	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressThinking, Text: "先分析"})
	now = now.Add(100 * time.Millisecond)
	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressToolUse, ToolName: "Read", ToolInput: `{"file":"README.md"}`})
	now = now.Add(100 * time.Millisecond)
	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressText, Text: "你好"})

	gotBeforeClose := updater.all()
	if len(gotBeforeClose) != 2 {
		t.Fatalf("updates before close = %d, want 2", len(gotBeforeClose))
	}
	if !strings.Contains(gotBeforeClose[0], "Running") || !strings.Contains(gotBeforeClose[0], "Thinking") {
		t.Fatalf("first update should include running+thinking details, got: %q", gotBeforeClose[0])
	}
	if !strings.Contains(gotBeforeClose[1], "Tool call") || !strings.Contains(gotBeforeClose[1], "Read") {
		t.Fatalf("second update should include tool call details, got: %q", gotBeforeClose[1])
	}

	s.Close()
	got := updater.all()
	if len(got) != 3 {
		t.Fatalf("updates after close = %d, want 3", len(got))
	}
	if !strings.Contains(got[2], "Current output") || !strings.Contains(got[2], "你好") {
		t.Fatalf("final update should include streamed text preview, got: %s", got[2])
	}
}

func TestCompactCardStreamer_NoDuplicateContent(t *testing.T) {
	updater := &testCardUpdater{}
	now := time.Unix(200, 0)
	s := newCompactCardStreamer(context.Background(), updater, "msg-2")
	s.minInterval = 0
	s.debounceDelay = 0
	s.now = func() time.Time { return now }

	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressThinking, Text: "同一条"})
	now = now.Add(time.Second)
	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressThinking, Text: "同一条"})
	s.Close()

	got := updater.all()
	if len(got) != 1 {
		t.Fatalf("updates len = %d, want 1", len(got))
	}
}

func TestCompactCardStreamer_NewlineForcesFlush(t *testing.T) {
	updater := &testCardUpdater{}
	now := time.Unix(300, 0)
	s := newCompactCardStreamer(context.Background(), updater, "msg-3")
	s.minInterval = 2 * time.Second
	s.debounceDelay = 0
	s.now = func() time.Time { return now }

	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressText, Text: "第一行"})
	now = now.Add(100 * time.Millisecond)
	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressText, Text: "\n第二行"})

	got := updater.all()
	if len(got) != 2 {
		t.Fatalf("updates len = %d, want 2", len(got))
	}
	if !strings.Contains(got[1], "第二行") {
		t.Fatalf("second update should contain second line, got: %s", got[1])
	}
}

func TestCompactCardStreamer_TextDebounce(t *testing.T) {
	updater := &testCardUpdater{}
	s := newCompactCardStreamer(context.Background(), updater, "msg-4")
	s.minInterval = 0
	s.debounceDelay = 40 * time.Millisecond

	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressText, Text: "你"})
	if len(updater.all()) != 0 {
		t.Fatalf("should not flush immediately for debounced text")
	}

	time.Sleep(20 * time.Millisecond)
	s.OnProgress(claude.ProgressEvent{Kind: claude.ProgressText, Text: "好"})
	time.Sleep(25 * time.Millisecond)
	if len(updater.all()) != 0 {
		t.Fatalf("should still debounce while new text arrives")
	}

	time.Sleep(30 * time.Millisecond)
	got := updater.all()
	if len(got) != 1 {
		t.Fatalf("updates len = %d, want 1", len(got))
	}
	if !strings.Contains(got[0], "你好") {
		t.Fatalf("debounced update should contain merged text, got: %s", got[0])
	}
}

type testSender struct {
	texts []string
}

func (s *testSender) SendThinking(context.Context, string, string) (string, error) { return "", nil }
func (s *testSender) UpdateCard(context.Context, string, string) error             { return nil }
func (s *testSender) SendText(_ context.Context, _, _ string, text string) (string, error) {
	s.texts = append(s.texts, text)
	return "msg", nil
}
func (s *testSender) AddProcessingReaction(context.Context, string) (string, error) {
	return "reaction-1", nil
}
func (s *testSender) AddDoneReaction(context.Context, string) error { return nil }

type streamSender struct {
	thinkingIDs []string
	updates     map[string][]string
	texts       []string
}

func (s *streamSender) SendThinking(context.Context, string, string) (string, error) {
	id := fmt.Sprintf("card-%d", len(s.thinkingIDs)+1)
	s.thinkingIDs = append(s.thinkingIDs, id)
	return id, nil
}

func (s *streamSender) UpdateCard(_ context.Context, messageID string, text string) error {
	if s.updates == nil {
		s.updates = make(map[string][]string)
	}
	s.updates[messageID] = append(s.updates[messageID], text)
	return nil
}

func (s *streamSender) SendText(_ context.Context, _, _ string, text string) (string, error) {
	s.texts = append(s.texts, text)
	return "msg", nil
}
func (s *streamSender) AddProcessingReaction(context.Context, string) (string, error) {
	return "reaction-1", nil
}
func (s *streamSender) AddDoneReaction(context.Context, string) error {
	return nil
}

type fakeModeStore struct {
	appID string
	mode  string
	err   error
}

func (f *fakeModeStore) SetAppWorkspaceMode(appID, mode string) error {
	f.appID = appID
	f.mode = mode
	return f.err
}

type fakeExecutor struct {
	events []claude.ProgressEvent
	result *claude.ExecuteResult
	reqs   []*claude.ExecuteRequest
}

func (f *fakeExecutor) Execute(_ context.Context, req *claude.ExecuteRequest) (*claude.ExecuteResult, error) {
	f.reqs = append(f.reqs, req)
	for _, evt := range f.events {
		if req.OnProgress != nil {
			req.OnProgress(evt)
		}
	}
	return f.result, nil
}

type fakeScheduleManager struct {
	ok      bool
	created *model.Schedule
	reply   string
}

func (f *fakeScheduleManager) CreateFromMessage(_ context.Context, _ *config.AppConfig, _ *feishu.IncomingMessage) (*model.Schedule, bool, error) {
	return f.created, f.ok, nil
}

func (f *fakeScheduleManager) ManageFromMessage(_ context.Context, _ *config.AppConfig, _ *feishu.IncomingMessage) (string, bool, error) {
	return f.reply, f.ok, nil
}

func TestWorker_Process_AutoCreatesSchedule(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sender := &testSender{}
	w := &Worker{
		channelKey: "p2p:oc_chat:app1",
		appCfg:     &config.AppConfig{ID: "app1", WorkspaceDir: t.TempDir()},
		db:         database,
		sender:     sender,
		scheduleMgr: &fakeScheduleManager{
			ok:      true,
			created: &model.Schedule{ID: "sched-1", Name: "每小时喝水提醒", CronExpr: "0 * * * *"},
		},
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "p2p",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_1",
		Prompt:      "每小时提醒我喝水",
		ReceiveID:   "ou_user",
		ReceiveType: "open_id",
	}

	w.process(context.Background(), msg)

	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "已创建定时任务") {
		t.Fatalf("sender texts = %#v, want created reply", sender.texts)
	}

	var msgs []model.Message
	if err := database.Where("role IN ?", []string{"user", "assistant"}).Find(&msgs).Error; err != nil {
		t.Fatalf("query messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2", len(msgs))
	}
}

func TestWorker_Process_RotatesCardOnToolUse(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		events: []claude.ProgressEvent{
			{Kind: claude.ProgressThinking, Text: "先分析"},
			{Kind: claude.ProgressToolUse, ToolName: "Read", ToolInput: `{"file":"README.md"}`},
			{Kind: claude.ProgressToolResult, ToolName: "Read", Text: "ok"},
			{Kind: claude.ProgressText, Text: "继续输出"},
		},
		result: &claude.ExecuteResult{Text: "最终答案"},
	}
	w := &Worker{
		channelKey: "p2p:oc_chat:app1",
		appCfg:     &config.AppConfig{ID: "app1", WorkspaceDir: t.TempDir()},
		db:         database,
		executor:   executor,
		sender:     sender,
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "p2p",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_2",
		Prompt:      "帮我读一下文件",
		ReceiveID:   "ou_user",
		ReceiveType: "open_id",
	}

	w.process(context.Background(), msg)

	if len(sender.thinkingIDs) != 2 {
		t.Fatalf("thinking cards = %d, want 2", len(sender.thinkingIDs))
	}
	if len(sender.updates[sender.thinkingIDs[0]]) == 0 {
		t.Fatalf("first card should have streaming updates")
	}
	secondUpdates := sender.updates[sender.thinkingIDs[1]]
	if len(secondUpdates) == 0 {
		t.Fatalf("second card should have updates")
	}
	last := secondUpdates[len(secondUpdates)-1]
	if !strings.Contains(last, "最终答案") {
		t.Fatalf("last update should contain final answer, got: %s", last)
	}
}

func TestWorker_Process_NoToolUsePreservesThinkingInFinalCard(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		events: []claude.ProgressEvent{
			{Kind: claude.ProgressThinking, Text: "先分析问题"},
			{Kind: claude.ProgressText, Text: "流式片段"},
		},
		result: &claude.ExecuteResult{Text: "最终答案"},
	}
	w := &Worker{
		channelKey: "p2p:oc_chat:app1",
		appCfg:     &config.AppConfig{ID: "app1", WorkspaceDir: t.TempDir()},
		db:         database,
		executor:   executor,
		sender:     sender,
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "p2p",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_3",
		Prompt:      "直接回答，不调用工具",
		ReceiveID:   "ou_user",
		ReceiveType: "open_id",
	}

	w.process(context.Background(), msg)

	if len(sender.thinkingIDs) != 1 {
		t.Fatalf("thinking cards = %d, want 1", len(sender.thinkingIDs))
	}
	updates := sender.updates[sender.thinkingIDs[0]]
	if len(updates) == 0 {
		t.Fatalf("card should have updates")
	}
	last := updates[len(updates)-1]
	if !strings.Contains(last, "Recent progress") || !strings.Contains(last, "Thinking") {
		t.Fatalf("final card should preserve thinking, got: %s", last)
	}
	if !strings.Contains(last, "Final output") || !strings.Contains(last, "最终答案") {
		t.Fatalf("final card should include final output, got: %s", last)
	}
}

func TestWorker_PersistResult_WritesStructuredToolCalls(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	w := &Worker{db: database}
	sess := &model.Session{ID: "sess-1"}

	w.persistResult(sess, &claude.ExecuteResult{
		Text: "ok",
		ToolUses: []claude.ToolUseRecord{
			{Name: "Read", Input: `{"file":"README.md"}`, Output: "done"},
			{Name: "Bash", Input: `{"command":"pwd"}`, Output: "/tmp"},
		},
	})

	var calls []model.MessageToolCall
	if err := database.Where("session_id = ?", sess.ID).Order("order_index ASC").Find(&calls).Error; err != nil {
		t.Fatalf("query message tool calls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("tool call rows = %d, want 2", len(calls))
	}
	if calls[0].Name != "Read" || calls[1].Name != "Bash" {
		t.Fatalf("unexpected call names: %+v", calls)
	}
	if calls[0].CallID != "call_0" || calls[1].CallID != "call_1" {
		t.Fatalf("unexpected call ids: %+v", calls)
	}
}

func TestBuildProbePrompt_PrefersSilenceWhenUncertain(t *testing.T) {
	prompt := buildProbePrompt("你们在聊啥")
	for _, want := range []string{
		"默认保持静默",
		"不确定是否该插话，输出 IGNORE",
		"只有明显值得你参与时才输出 RESPOND",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("buildProbePrompt() missing %q in %q", want, prompt)
		}
	}
}

func TestWorker_Process_GroupProbeIgnore_SkipsMainReply(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		result: &claude.ExecuteResult{Text: "IGNORE"},
	}
	w := &Worker{
		channelKey: "group:oc_chat:app1",
		appCfg: &config.AppConfig{
			ID:           "app1",
			WorkspaceDir: t.TempDir(),
		},
		db:       database,
		executor: executor,
		sender:   sender,
		sessionCfg: config.SessionConfig{Probe: config.SessionProbeConfig{Enabled: true}},
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "group",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_probe_ignore",
		Prompt:      "你们继续",
		ReceiveID:   "oc_chat",
		ReceiveType: "chat_id",
	}

	w.process(context.Background(), msg)

	if len(executor.reqs) != 1 {
		t.Fatalf("executor calls = %d, want 1 probe call", len(executor.reqs))
	}
	if !strings.HasPrefix(executor.reqs[0].SessionID, "probe:") {
		t.Fatalf("probe session id = %q, want probe:*", executor.reqs[0].SessionID)
	}
	if len(sender.thinkingIDs) != 0 {
		t.Fatalf("thinking cards = %d, want 0", len(sender.thinkingIDs))
	}
}

func TestWorker_Process_GroupProbeRespond_ActivatesMainReply(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		result: &claude.ExecuteResult{Text: "RESPOND"},
	}
	w := &Worker{
		channelKey: "group:oc_chat:app1",
		appCfg: &config.AppConfig{
			ID:           "app1",
			WorkspaceDir: t.TempDir(),
		},
		db:       database,
		executor: executor,
		sender:   sender,
		sessionCfg: config.SessionConfig{Probe: config.SessionProbeConfig{Enabled: true}},
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "group",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_probe_respond",
		Prompt:      "你怎么看",
		ReceiveID:   "oc_chat",
		ReceiveType: "chat_id",
	}

	w.process(context.Background(), msg)

	if len(executor.reqs) != 2 {
		t.Fatalf("executor calls = %d, want probe + main", len(executor.reqs))
	}
	if !strings.HasPrefix(executor.reqs[0].SessionID, "probe:") {
		t.Fatalf("probe session id = %q, want probe:*", executor.reqs[0].SessionID)
	}
	if strings.HasPrefix(executor.reqs[1].SessionID, "probe:") {
		t.Fatalf("main session id = %q, should not reuse probe session", executor.reqs[1].SessionID)
	}
	if len(sender.thinkingIDs) != 0 {
		t.Fatalf("thinking cards = %d, want 0", len(sender.thinkingIDs))
	}
	if len(sender.texts) != 1 || sender.texts[0] != "RESPOND" {
		t.Fatalf("group final texts = %#v, want [RESPOND]", sender.texts)
	}
}

func TestWorker_Process_GroupMention_SkipsProbe(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		result: &claude.ExecuteResult{Text: "正常回复"},
	}
	w := &Worker{
		channelKey: "group:oc_chat:app1",
		appCfg: &config.AppConfig{
			ID:           "app1",
			WorkspaceDir: t.TempDir(),
		},
		db:       database,
		executor: executor,
		sender:   sender,
		sessionCfg: config.SessionConfig{Probe: config.SessionProbeConfig{Enabled: true}},
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "group",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_probe_skip",
		Prompt:      "@Cloud 你怎么看",
		ReceiveID:   "oc_chat",
		ReceiveType: "chat_id",
		MentionsMe:  true,
	}

	w.process(context.Background(), msg)

	if len(executor.reqs) != 1 {
		t.Fatalf("executor calls = %d, want 1 main call", len(executor.reqs))
	}
	if strings.HasPrefix(executor.reqs[0].SessionID, "probe:") {
		t.Fatalf("main session id = %q, should not use probe session", executor.reqs[0].SessionID)
	}
	if len(sender.thinkingIDs) != 0 {
		t.Fatalf("thinking cards = %d, want 0", len(sender.thinkingIDs))
	}
	if len(sender.texts) != 1 || sender.texts[0] != "正常回复" {
		t.Fatalf("group final texts = %#v, want [正常回复]", sender.texts)
	}
}

func TestWorker_Process_GroupNameTarget_SkipsProbe(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		result: &claude.ExecuteResult{Text: "正常回复"},
	}
	w := &Worker{
		channelKey: "group:oc_chat:app1",
		appCfg: &config.AppConfig{
			ID:           "cloud",
			WorkspaceDir: t.TempDir(),
		},
		db:         database,
		executor:   executor,
		sender:     sender,
		sessionCfg: config.SessionConfig{Probe: config.SessionProbeConfig{Enabled: true}},
	}

	msg := &feishu.IncomingMessage{
		AppID:       "cloud",
		ChannelKey:  w.channelKey,
		ChatType:    "group",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_name_skip_probe",
		Prompt:      "cloud 你怎么看",
		ReceiveID:   "oc_chat",
		ReceiveType: "chat_id",
		NamesMe:     true,
	}

	w.process(context.Background(), msg)

	if len(executor.reqs) != 1 {
		t.Fatalf("executor calls = %d, want 1 main call", len(executor.reqs))
	}
	if strings.HasPrefix(executor.reqs[0].SessionID, "probe:") {
		t.Fatalf("main session id = %q, should not use probe session", executor.reqs[0].SessionID)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "正常回复" {
		t.Fatalf("group final texts = %#v, want [正常回复]", sender.texts)
	}
}

func TestWorker_Process_GroupSkipsVisibleProgressAndSendsFinalText(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	sender := &streamSender{}
	executor := &fakeExecutor{
		events: []claude.ProgressEvent{
			{Kind: claude.ProgressThinking, Text: "先分析"},
			{Kind: claude.ProgressToolUse, ToolName: "Read", ToolInput: `{"file":"README.md"}`},
			{Kind: claude.ProgressText, Text: "流式片段"},
		},
		result: &claude.ExecuteResult{Text: "群里最终答案"},
	}
	w := &Worker{
		channelKey: "group:oc_chat:app1",
		appCfg:     &config.AppConfig{ID: "app1", WorkspaceDir: t.TempDir()},
		db:         database,
		executor:   executor,
		sender:     sender,
	}

	msg := &feishu.IncomingMessage{
		AppID:       "app1",
		ChannelKey:  w.channelKey,
		ChatType:    "group",
		ChatID:      "oc_chat",
		SenderID:    "ou_user",
		MessageID:   "om_group_final_only",
		Prompt:      "@Cloud 说重点",
		ReceiveID:   "oc_chat",
		ReceiveType: "chat_id",
		MentionsMe:  true,
	}

	w.process(context.Background(), msg)

	if len(sender.thinkingIDs) != 0 {
		t.Fatalf("thinking cards = %d, want 0", len(sender.thinkingIDs))
	}
	if len(sender.updates) != 0 {
		t.Fatalf("card updates = %#v, want none", sender.updates)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "群里最终答案" {
		t.Fatalf("group final texts = %#v, want [群里最终答案]", sender.texts)
	}
}

func TestWorker_Run_IdleTimeoutKeepsActiveSession(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	appCfg := &config.AppConfig{ID: "app1", WorkspaceDir: t.TempDir()}
	w := &Worker{
		channelKey:  "p2p:oc_chat:app1",
		appCfg:      appCfg,
		db:          database,
		idleTimeout: 20 * time.Millisecond,
		queue:       make(chan *feishu.IncomingMessage, 1),
		stopCh:      make(chan struct{}),
	}

	sess, err := w.getOrCreateSession("ou_user")
	if err != nil {
		t.Fatalf("getOrCreateSession: %v", err)
	}

	exited := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.run(ctx, func() {
		close(exited)
	})

	select {
	case <-exited:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker did not exit on idle timeout")
	}

	var reloaded model.Session
	if err := database.First(&reloaded, "id = ?", sess.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if reloaded.Status != statusActive {
		t.Fatalf("session status = %q, want %q", reloaded.Status, statusActive)
	}
}

func TestWorker_ShouldShowVisibleProgress_DefaultWorkMode(t *testing.T) {
	w := &Worker{appCfg: &config.AppConfig{}}

	if !w.shouldShowVisibleProgress(&feishu.IncomingMessage{ChatType: "p2p"}) {
		t.Fatal("p2p should show visible progress in default work mode")
	}
	if w.shouldShowVisibleProgress(&feishu.IncomingMessage{ChatType: "group"}) {
		t.Fatal("group should not show visible progress in default work mode")
	}
}

func TestWorker_ShouldShowVisibleProgress_CompanionModeAlwaysHides(t *testing.T) {
	w := &Worker{appCfg: &config.AppConfig{WorkspaceMode: "companion"}}

	if w.shouldShowVisibleProgress(&feishu.IncomingMessage{ChatType: "p2p"}) {
		t.Fatal("p2p should not show visible progress in companion mode")
	}
	if w.shouldShowVisibleProgress(&feishu.IncomingMessage{ChatType: "group"}) {
		t.Fatal("group should not show visible progress in companion mode")
	}
}

func TestWorker_HandleBuiltInCommands_ModeSwitch(t *testing.T) {
	sender := &testSender{}
	modeStore := &fakeModeStore{}
	w := &Worker{
		appCfg: &config.AppConfig{ID: "code", WorkspaceMode: "work"},
		cfg:    modeStore,
		sender: sender,
	}
	msg := &feishu.IncomingMessage{ReceiveID: "ou_x", ReceiveType: "open_id"}

	if !w.handleBuiltInCommands(context.Background(), msg, "/comp") {
		t.Fatal("expected /comp to be handled")
	}
	if modeStore.appID != "code" || modeStore.mode != "companion" {
		t.Fatalf("modeStore = (%q, %q), want (code, companion)", modeStore.appID, modeStore.mode)
	}
	if w.appCfg.WorkspaceMode != "companion" {
		t.Fatalf("workspace mode = %q, want companion", w.appCfg.WorkspaceMode)
	}
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "已切换到 companion 模式") {
		t.Fatalf("texts = %#v, want mode switch confirmation", sender.texts)
	}
}

func TestWorker_HandleBuiltInCommands_ModeShow(t *testing.T) {
	sender := &testSender{}
	w := &Worker{
		appCfg: &config.AppConfig{ID: "code", WorkspaceMode: "companion"},
		sender: sender,
	}
	msg := &feishu.IncomingMessage{ReceiveID: "ou_x", ReceiveType: "open_id"}

	if !w.handleBuiltInCommands(context.Background(), msg, "/mode") {
		t.Fatal("expected /mode to be handled")
	}
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "当前模式：companion") {
		t.Fatalf("texts = %#v, want current mode reply", sender.texts)
	}
}

func TestWorker_HandleBuiltInCommands_ModeInvalidArg(t *testing.T) {
	sender := &testSender{}
	w := &Worker{
		appCfg: &config.AppConfig{ID: "code", WorkspaceMode: "work"},
		sender: sender,
	}
	msg := &feishu.IncomingMessage{ReceiveID: "ou_x", ReceiveType: "open_id"}

	if !w.handleBuiltInCommands(context.Background(), msg, "/mode weird") {
		t.Fatal("expected /mode weird to be handled")
	}
	if len(sender.texts) != 1 || !strings.Contains(sender.texts[0], "无效模式") {
		t.Fatalf("texts = %#v, want invalid mode reply", sender.texts)
	}
}

func TestWorker_StopCurrentExecution(t *testing.T) {
	w := &Worker{}
	runCtx, finish := w.beginCurrentRun(context.Background())
	defer finish()

	if !w.stopCurrentExecution() {
		t.Fatal("expected stopCurrentExecution to stop active run")
	}
	select {
	case <-runCtx.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("run context was not canceled")
	}
	if w.stopCurrentExecution() {
		t.Fatal("expected second stopCurrentExecution to report no active run")
	}
}
