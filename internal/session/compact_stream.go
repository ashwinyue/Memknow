package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ashwinyue/Memknow/internal/claude"
)

const (
	compactCardMaxChars          = 6000
	compactCardPreviewChars      = 1400
	compactCardMaxEntries        = 6
	compactCardMaxEntryChars     = 220
	compactCardToolInputMaxChars = 180
	compactCardToolOutputMaxChar = 320
	compactCardToolOutputMaxLine = 6
	compactCardAPITimeout        = 15 * time.Second
)

var leadingLineNo = regexp.MustCompile(`^\d+\s+`)

type cardUpdater interface {
	UpdateCard(ctx context.Context, messageID string, text string) error
}

type progressEntry struct {
	title string
	body  string
}

type compactCardStreamer struct {
	ctx       context.Context
	updater   cardUpdater
	messageID string

	minInterval   time.Duration
	debounceDelay time.Duration
	now           func() time.Time

	mu         sync.Mutex
	failed     bool
	closed     bool
	entries    []progressEntry
	answer     strings.Builder
	sawToolUse bool
	lastSent   string
	lastSentAt time.Time
	pending    string
	flushTimer *time.Timer
}

func newCompactCardStreamer(ctx context.Context, updater cardUpdater, messageID string) *compactCardStreamer {
	return &compactCardStreamer{
		ctx:           ctx,
		updater:       updater,
		messageID:     messageID,
		minInterval:   2 * time.Second,
		debounceDelay: 600 * time.Millisecond,
		now:           time.Now,
	}
}

func (s *compactCardStreamer) OnProgress(evt claude.ProgressEvent) {
	select {
	case <-s.ctx.Done():
		return
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed || s.closed || s.messageID == "" {
		return
	}

	switch evt.Kind {
	case claude.ProgressThinking:
		if t := strings.TrimSpace(evt.Text); t != "" {
			s.appendEntry("Thinking", truncateRunes(collapseWhitespace(t), compactCardMaxEntryChars))
		}
	case claude.ProgressToolUse:
		s.sawToolUse = true
		title := "Tool call"
		if name := strings.TrimSpace(evt.ToolName); name != "" {
			title += " `" + name + "`"
		}
		body := ""
		if in := strings.TrimSpace(evt.ToolInput); in != "" {
			body = formatToolInputPreview(evt.ToolName, in)
		}
		s.appendEntry(title, body)
	case claude.ProgressToolResult:
		title := "Tool result"
		if name := strings.TrimSpace(evt.ToolName); name != "" {
			title += " `" + name + "`"
		}
		body := ""
		if out := strings.TrimSpace(evt.Text); out != "" {
			body = formatToolOutputPreview(out)
		}
		s.appendEntry(title, body)
	case claude.ProgressText:
		if evt.Text != "" {
			s.answer.WriteString(evt.Text)
		}
	}

	render := s.renderLocked()
	if render == "" || render == s.lastSent {
		return
	}
	if evt.Kind == claude.ProgressText && !shouldForceFlush(evt) && s.debounceDelay > 0 {
		s.pending = render
		s.scheduleDebounceFlushLocked(s.debounceDelay)
		return
	}
	s.stopDebounceFlushLocked()
	now := s.now()
	if !s.lastSentAt.IsZero() && s.minInterval > 0 && now.Sub(s.lastSentAt) < s.minInterval && !shouldForceFlush(evt) {
		s.pending = render
		return
	}
	s.pending = ""
	s.sendLocked(render, now)
}

func (s *compactCardStreamer) sawToolUsage() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sawToolUse
}

func (s *compactCardStreamer) renderFinalWithProgress(finalText string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	finalText = strings.TrimSpace(finalText)
	if finalText == "" {
		return ""
	}
	if len(s.entries) == 0 {
		return finalText
	}

	var b strings.Builder
	b.WriteString("✅ **Done**")
	b.WriteString("\n\nRecent progress:\n")
	for i, entry := range s.entries {
		b.WriteString("**")
		b.WriteString(entry.title)
		b.WriteString("**")
		if entry.body != "" {
			b.WriteString("\n")
			b.WriteString(entry.body)
		}
		if i != len(s.entries)-1 {
			b.WriteString("\n\n")
		}
	}
	b.WriteString("\n\nFinal output:\n")
	b.WriteString(finalText)
	return trimTailRunes(b.String(), compactCardMaxChars)
}

func (s *compactCardStreamer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed || s.closed || s.messageID == "" {
		return
	}
	s.closed = true
	s.stopDebounceFlushLocked()
	now := s.now()
	if s.pending != "" && s.pending != s.lastSent {
		s.sendLocked(s.pending, now)
		s.pending = ""
	}
	render := s.renderLocked()
	if render != "" && render != s.lastSent {
		s.sendLocked(render, now)
	}
}

func (s *compactCardStreamer) scheduleDebounceFlushLocked(delay time.Duration) {
	if delay <= 0 {
		return
	}
	if s.flushTimer == nil {
		s.flushTimer = time.AfterFunc(delay, s.flushPendingDebounced)
		return
	}
	s.flushTimer.Reset(delay)
}

func (s *compactCardStreamer) stopDebounceFlushLocked() {
	if s.flushTimer == nil {
		return
	}
	s.flushTimer.Stop()
	s.flushTimer = nil
}

func (s *compactCardStreamer) flushPendingDebounced() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	if s.failed || s.closed || s.messageID == "" {
		return
	}
	if s.pending == "" || s.pending == s.lastSent {
		return
	}
	now := s.now()
	if !s.lastSentAt.IsZero() && s.minInterval > 0 && now.Sub(s.lastSentAt) < s.minInterval {
		wait := s.minInterval - now.Sub(s.lastSentAt)
		if wait > 0 {
			s.scheduleDebounceFlushLocked(wait)
		}
		return
	}
	content := s.pending
	s.pending = ""
	s.sendLocked(content, now)
}

func (s *compactCardStreamer) appendEntry(title, body string) {
	title = truncateRunes(collapseWhitespace(title), compactCardMaxEntryChars)
	if title == "" {
		return
	}
	body = strings.TrimSpace(body)
	entry := progressEntry{title: title, body: body}
	if len(s.entries) > 0 && s.entries[len(s.entries)-1] == entry {
		return
	}
	s.entries = append(s.entries, entry)
	if len(s.entries) > compactCardMaxEntries {
		s.entries = s.entries[len(s.entries)-compactCardMaxEntries:]
	}
}

func (s *compactCardStreamer) renderLocked() string {
	var b strings.Builder
	b.WriteString("⏳ **Running...**")

	if len(s.entries) > 0 {
		b.WriteString("\n\nRecent progress:\n")
		for i, entry := range s.entries {
			b.WriteString("**")
			b.WriteString(entry.title)
			b.WriteString("**")
			if entry.body != "" {
				b.WriteString("\n")
				b.WriteString(entry.body)
			}
			if i != len(s.entries)-1 {
				b.WriteString("\n\n")
			}
		}
	}

	answer := strings.TrimSpace(s.answer.String())
	if answer != "" {
		b.WriteString("\n\nCurrent output (streaming):\n")
		b.WriteString(trimTailRunes(answer, compactCardPreviewChars))
	}
	return trimTailRunes(b.String(), compactCardMaxChars)
}

func shouldForceFlush(evt claude.ProgressEvent) bool {
	switch evt.Kind {
	case claude.ProgressToolUse, claude.ProgressToolResult:
		return true
	case claude.ProgressText:
		return strings.Contains(evt.Text, "\n")
	default:
		return false
	}
}

func (s *compactCardStreamer) sendLocked(content string, now time.Time) {
	callCtx, cancel := s.withAPITimeout()
	err := s.updater.UpdateCard(callCtx, s.messageID, content)
	cancel()
	if err != nil {
		s.failed = true
		slog.Warn("compact streamer: UpdateCard failed", "messageID", s.messageID, "error", err)
		return
	}
	s.lastSent = content
	s.lastSentAt = now
}

func (s *compactCardStreamer) withAPITimeout() (context.Context, context.CancelFunc) {
	if _, hasDeadline := s.ctx.Deadline(); hasDeadline {
		return s.ctx, func() {}
	}
	return context.WithTimeout(s.ctx, compactCardAPITimeout)
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	rs := []rune(s)
	return string(rs[:maxRunes])
}

func trimTailRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	rs := []rune(s)
	tail := strings.TrimLeft(string(rs[len(rs)-maxRunes:]), "\n")
	return fmt.Sprintf("…\n%s", tail)
}

func collapseWhitespace(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func formatToolInputPreview(toolName, input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	// Prefer semantically useful fields over raw JSON blob.
	var obj map[string]any
	if strings.HasPrefix(input, "{") && strings.HasSuffix(input, "}") {
		if err := json.Unmarshal([]byte(input), &obj); err == nil {
			for _, key := range []string{"command", "cmd", "path", "file_path", "url", "query"} {
				if v, ok := obj[key]; ok {
					if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
						return formatToolContentBlock(toolName, s, compactCardToolInputMaxChars)
					}
				}
			}
		}
	}
	return formatToolContentBlock(toolName, input, compactCardToolInputMaxChars)
}

func formatToolOutputPreview(output string) string {
	raw := strings.TrimSpace(output)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	kept := make([]string, 0, compactCardToolOutputMaxLine)
	for _, line := range lines {
		line = leadingLineNo.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, line)
		if len(kept) >= compactCardToolOutputMaxLine {
			break
		}
	}
	if len(kept) == 0 {
		return ""
	}
	body := strings.Join(kept, "\n")
	more := 0
	for _, line := range lines {
		if strings.TrimSpace(leadingLineNo.ReplaceAllString(line, "")) != "" {
			more++
		}
	}
	body = truncateRunes(body, compactCardToolOutputMaxChar)
	block := fmt.Sprintf("```text\n%s\n```", body)
	if more > len(kept) {
		block += fmt.Sprintf("\n_... (+%d lines)_", more-len(kept))
	}
	return block
}

func formatToolContentBlock(toolName, content string, maxChars int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if isBashToolName(toolName) {
		return fmt.Sprintf("```bash\n%s\n```", truncateRunes(content, maxChars))
	}
	if strings.Contains(content, "\n") {
		return fmt.Sprintf("```text\n%s\n```", truncateRunes(content, maxChars))
	}
	return "`" + truncateRunes(collapseWhitespace(content), maxChars) + "`"
}

func isBashToolName(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "shell", "run_shell_command":
		return true
	default:
		return false
	}
}
