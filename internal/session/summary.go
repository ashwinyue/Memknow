package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/model"
	"gorm.io/gorm"
)

// Summarizer generates session summaries using a Claude executor.
type Summarizer struct {
	db       *gorm.DB
	cfg      *config.Config
	executor claude.ExecutorInterface
}

// NewSummarizer creates a summarizer backed by the given DB, config and executor.
func NewSummarizer(db *gorm.DB, cfg *config.Config, executor claude.ExecutorInterface) *Summarizer {
	return &Summarizer{
		db:       db,
		cfg:      cfg,
		executor: executor,
	}
}

// Summarize reads all messages for the session and returns a concise summary.
func (s *Summarizer) Summarize(sessionID string) (string, error) {
	var sess model.Session
	if err := s.db.Select("id", "channel_key").Where("id = ?", sessionID).First(&sess).Error; err != nil {
		return "", fmt.Errorf("load session: %w", err)
	}

	var messages []model.Message
	if err := s.db.Where("session_id = ?", sessionID).Order("created_at ASC").Find(&messages).Error; err != nil {
		return "", fmt.Errorf("load messages: %w", err)
	}
	if len(messages) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "user":
			sb.WriteString("User: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			sb.WriteString(m.Content)
			if m.Reasoning != "" {
				sb.WriteString("\n[Reasoning: ")
				sb.WriteString(m.Reasoning)
				sb.WriteString("]")
			}
			sb.WriteString("\n")
		case "tool":
			sb.WriteString("Tool (")
			sb.WriteString(m.ToolName)
			sb.WriteString("): ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}

	dialogue := sb.String()
	if len(dialogue) > 12000 {
		dialogue = dialogue[:12000] + "\n...[truncated]"
	}

	prompt := fmt.Sprintf(`请用 1-3 句话总结以下对话的核心内容和结论。不要包含寒暄，只保留事实和决策：

%s`, dialogue)

	appID := appIDFromChannelKey(sess.ChannelKey)
	if appID == "" {
		return "", fmt.Errorf("resolve app id from channel key: %q", sess.ChannelKey)
	}
	appCfg := s.findAppConfig(appID)
	if appCfg == nil {
		return "", fmt.Errorf("app config not found for app_id=%s", appID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := s.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:       prompt,
		SessionID:    sessionID + "-summary",
		AppConfig:    appCfg,
		WorkspaceDir: appCfg.WorkspaceDir,
	})
	if err != nil {
		return "", fmt.Errorf("summarize execute: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

func (s *Summarizer) findAppConfig(appID string) *config.AppConfig {
	for i := range s.cfg.Apps {
		if s.cfg.Apps[i].ID == appID {
			return &s.cfg.Apps[i]
		}
	}
	return nil
}

func appIDFromChannelKey(channelKey string) string {
	ck := strings.TrimSpace(channelKey)
	if ck == "" {
		return ""
	}
	if strings.HasPrefix(ck, "heartbeat:") {
		return strings.TrimPrefix(ck, "heartbeat:")
	}
	parts := strings.Split(ck, ":")
	if len(parts) < 3 {
		return ""
	}
	return parts[len(parts)-1]
}
