package heartbeat

import (
	"context"
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
)

const heartbeatChannelChatID = "__heartbeat__"

// Sender matches the minimal interface needed to send text messages.
type Sender interface {
	SendText(ctx context.Context, receiveID string, receiveIDType string, text string) (string, error)
}

type notifyTarget struct {
	ChatType string
	ChatID   string
}

type Service struct {
	cfg      *config.Config
	db       *gorm.DB
	executor claude.ExecutorInterface
	senders  map[string]Sender
	mu       sync.Mutex
	loops    map[string]context.CancelFunc
}

func NewService(cfg *config.Config, db *gorm.DB, executor claude.ExecutorInterface, senders map[string]Sender) *Service {
	return &Service{cfg: cfg, db: db, executor: executor, senders: senders, loops: make(map[string]context.CancelFunc)}
}

func shouldEnable(cfg config.HeartbeatConfig) bool {
	return cfg.Enabled && cfg.IntervalMinutes > 0
}

func promptPath(app config.AppConfig, cfg config.HeartbeatConfig) string {
	return filepath.Join(app.WorkspaceDir, cfg.PromptFile)
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.cfg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Apps {
		app := &s.cfg.Apps[i]
		if !shouldEnable(s.cfg.Heartbeat) {
			continue
		}
		if _, ok := s.loops[app.ID]; ok {
			continue
		}
		loopCtx, cancel := context.WithCancel(ctx)
		s.loops[app.ID] = cancel
		go s.loop(loopCtx, app)
	}
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.loops {
		cancel()
	}
	s.loops = make(map[string]context.CancelFunc)
}

func (s *Service) Restart(ctx context.Context) {
	s.Stop()
	s.Start(ctx)
	slog.Info("heartbeat service restarted")
}

func (s *Service) loop(ctx context.Context, app *config.AppConfig) {
	ticker := time.NewTicker(time.Duration(s.cfg.Heartbeat.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	s.runOnce(ctx, app)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx, app)
		}
	}
}

func (s *Service) runOnce(ctx context.Context, app *config.AppConfig) {
	path := promptPath(*app, s.cfg.Heartbeat)
	promptBytes, err := os.ReadFile(path)
	if err != nil || len(promptBytes) == 0 {
		return
	}

	channelKey := "heartbeat:" + app.ID
	s.ensureChannel(channelKey, app.ID)

	sessionID := uuid.New().String()
	now := time.Now()
	s.createSession(sessionID, channelKey, now)
	defer s.archiveSession(sessionID)

	result, err := s.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:       string(promptBytes),
		SessionID:    sessionID,
		SessionType:  model.SessionTypeHeartbeat,
		AppConfig:    app,
		WorkspaceDir: app.WorkspaceDir,
		ChannelKey:   channelKey,
	})
	if err != nil {
		return
	}

	if ie, ok := s.executor.(*claude.InteractiveExecutor); ok {
		ie.RemoveSession(sessionID)
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		return
	}

	sender, ok := s.senders[app.ID]
	if !ok || sender == nil {
		slog.Error("heartbeat: no sender for app", "app_id", app.ID)
		return
	}

	targets := s.notifyTargets(app.ID)
	if len(targets) == 0 {
		slog.Info("heartbeat: no notify target configured, skip send", "app_id", app.ID)
		return
	}
	for _, target := range targets {
		receiveType := feishu.ReceiveIDType(target.ChatType, target.ChatID)
		if _, err := sender.SendText(ctx, target.ChatID, receiveType, text); err != nil {
			slog.Error("heartbeat: send failed", "app_id", app.ID, "chat_id", target.ChatID, "err", err)
		}
	}
}

// notifyTarget returns the configured heartbeat notification target.
// Empty config means heartbeat output is not sent to any chat.
func (s *Service) notifyTarget(appID string) (chatType, chatID string, ok bool) {
	_ = appID // reserved for possible per-app override in future.
	t := strings.TrimSpace(s.cfg.Heartbeat.NotifyTargetType)
	id := strings.TrimSpace(s.cfg.Heartbeat.NotifyTargetID)
	if t == "" || id == "" {
		return "", "", false
	}
	if t != "p2p" && t != "group" {
		slog.Warn("heartbeat: invalid notify_target_type, skip send", "type", t)
		return "", "", false
	}
	return t, id, true
}

func (s *Service) notifyTargets(appID string) []notifyTarget {
	if chatType, chatID, ok := s.notifyTarget(appID); ok {
		return []notifyTarget{{ChatType: chatType, ChatID: chatID}}
	}
	if s == nil || s.db == nil {
		return nil
	}
	var channels []model.Channel
	if err := s.db.
		Where("app_id = ? AND chat_type = ?", appID, "p2p").
		Order("updated_at desc").
		Find(&channels).Error; err != nil {
		slog.Warn("heartbeat: query p2p channels failed", "app_id", appID, "err", err)
		return nil
	}
	targets := make([]notifyTarget, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		if ch.ChatID == "" {
			continue
		}
		if _, ok := seen[ch.ChatID]; ok {
			continue
		}
		seen[ch.ChatID] = struct{}{}
		targets = append(targets, notifyTarget{ChatType: "p2p", ChatID: ch.ChatID})
	}
	return targets
}

func (s *Service) ensureChannel(channelKey, appID string) {
	ch := model.Channel{
		ChannelKey: channelKey,
		AppID:      appID,
		ChatType:   "system",
		ChatID:     heartbeatChannelChatID,
	}
	if err := s.db.Where("channel_key = ?", channelKey).FirstOrCreate(&ch).Error; err != nil {
		slog.Warn("heartbeat: ensure channel failed", "channel_key", channelKey, "err", err)
	}
}

func (s *Service) createSession(sessionID, channelKey string, now time.Time) {
	sess := &model.Session{
		ID:         sessionID,
		ChannelKey: channelKey,
		Type:       model.SessionTypeHeartbeat,
		Status:     "active",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.db.Create(sess).Error; err != nil {
		slog.Warn("heartbeat: create session failed", "session_id", sessionID, "err", err)
	}
}

func (s *Service) archiveSession(sessionID string) {
	if err := s.db.Model(&model.Session{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"status":     "archived",
			"updated_at": time.Now(),
		}).Error; err != nil {
		slog.Warn("heartbeat: archive session failed", "session_id", sessionID, "err", err)
	}
}
