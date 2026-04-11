package cleanup

import (
	"log/slog"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/model"
	"github.com/ashwinyue/Memknow/internal/schedule"
	"github.com/ashwinyue/Memknow/internal/workspace"
)

// Service removes session attachments that exceed retention rules.
type Service struct {
	db   *gorm.DB
	apps []config.AppConfig
	cfg  config.CleanupConfig
}

// NewService creates a Service and registers the cleanup cron job with the shared scheduler.
func NewService(db *gorm.DB, apps []config.AppConfig, cfg config.CleanupConfig, sched *schedule.Service) (*Service, error) {
	c := &Service{db: db, apps: apps, cfg: cfg}
	if err := sched.AddSystemJob("attachment-cleanup", cfg.Schedule, c.Run); err != nil {
		return nil, err
	}
	return c, nil
}

// Run executes one cleanup pass across all configured apps.
func (c *Service) Run() {
	now := time.Now()
	retentionCutoff := now.Add(-time.Duration(c.cfg.AttachmentsRetentionDays) * 24 * time.Hour)
	maxCutoff := now.Add(-time.Duration(c.cfg.AttachmentsMaxDays) * 24 * time.Hour)

	total := 0
	for i := range c.apps {
		total += c.cleanApp(&c.apps[i], retentionCutoff, maxCutoff)
	}
	slog.Info("attachment cleanup complete", "deleted", total)
}

// cleanApp removes stale attachment dirs for one app and returns the count deleted.
func (c *Service) cleanApp(app *config.AppConfig, retentionCutoff, maxCutoff time.Time) int {
	var sessions []model.Session
	err := c.db.
		Joins("JOIN channels ON sessions.channel_key = channels.channel_key").
		Where("channels.app_id = ?", app.ID).
		Where(
			"(sessions.status = ? AND sessions.updated_at < ?) OR sessions.created_at < ?",
			"archived", retentionCutoff, maxCutoff,
		).
		Find(&sessions).Error
	if err != nil {
		slog.Error("cleanup: query sessions", "app_id", app.ID, "err", err)
		return 0
	}

	count := 0
	for _, sess := range sessions {
		attachDir := workspace.SessionAttachmentsDir(app.WorkspaceDir, sess.Type, sess.ID)

		if _, statErr := os.Stat(attachDir); os.IsNotExist(statErr) {
			continue
		}

		if err := os.RemoveAll(attachDir); err != nil {
			slog.Warn("cleanup: remove attachments", "session_id", sess.ID, "err", err)
			continue
		}
		count++
		slog.Info("cleanup: removed attachments", "session_id", sess.ID, "app_id", app.ID)
	}
	return count
}
