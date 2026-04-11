package session

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/feishu"
	"github.com/ashwinyue/Memknow/internal/model"
)

const duplicateWindow = 5 * time.Minute

// Manager maps channel keys to their Workers and lazily starts them.
type Manager struct {
	cfg         *config.Config
	appRegistry map[string]*config.AppConfig // app_id -> AppConfig
	db          *gorm.DB
	executor    claude.ExecutorInterface
	senders     map[string]sender // app_id -> Sender
	scheduleMgr scheduleManager
	summarizer  *Summarizer
	retriever   *Retriever

	workers sync.Map // channelKey -> *Worker

	// mu guards the check-then-act in getOrCreateWorker.
	// sync.Map is concurrent-safe for individual operations, but the Load+Store
	// pair needs to be atomic to prevent duplicate worker creation.
	mu sync.Mutex

	// wg tracks all running worker goroutines so Wait() can block until they exit.
	wg sync.WaitGroup

	// recentMsgIDs deduplicates Feishu re-delivery within a short window.
	recentMsgIDs map[string]time.Time
	recentMu     sync.RWMutex
}

// NewManager creates a Manager. senders maps app_id to feishu.Sender.
func NewManager(
	cfg *config.Config,
	db *gorm.DB,
	executor claude.ExecutorInterface,
	senders map[string]*feishu.Sender,
	scheduleMgr scheduleManager,
) *Manager {
	senderRegistry := make(map[string]sender, len(senders))
	for appID, s := range senders {
		senderRegistry[appID] = s
	}
	registry := make(map[string]*config.AppConfig, len(cfg.Apps))
	for i := range cfg.Apps {
		a := &cfg.Apps[i]
		registry[a.ID] = a
	}
	return &Manager{
		cfg:             cfg,
		appRegistry:     registry,
		db:              db,
		executor:        executor,
		senders:         senderRegistry,
		scheduleMgr:     scheduleMgr,
		summarizer:      NewSummarizer(db, cfg, executor),
		retriever:       NewRetriever(db),
		recentMsgIDs:    make(map[string]time.Time),
	}
}

// Dispatch routes an IncomingMessage to the appropriate Worker.
// It implements feishu.Dispatcher.
func (m *Manager) Dispatch(ctx context.Context, msg *feishu.IncomingMessage) error {
	if m.isDuplicate(msg.MessageID) {
		slog.Debug("session: duplicate message dropped", "message_id", msg.MessageID)
		return nil
	}
	if m.handleImmediateBuiltInCommand(ctx, msg) {
		return nil
	}

	worker := m.getOrCreateWorker(ctx, msg)
	if worker == nil {
		slog.Error("session: no worker for message", "channel", msg.ChannelKey)
		return nil
	}

	select {
	case worker.queue <- msg:
	default:
		slog.Warn("session: worker queue full, dropping message", "channel", msg.ChannelKey)
	}
	return nil
}

func (m *Manager) handleImmediateBuiltInCommand(ctx context.Context, msg *feishu.IncomingMessage) bool {
	if msg == nil {
		return false
	}
	switch strings.TrimSpace(msg.Prompt) {
	case "/stop":
		m.handleStop(ctx, msg)
		return true
	default:
		return false
	}
}

func (m *Manager) handleStop(ctx context.Context, msg *feishu.IncomingMessage) {
	reply := "当前没有正在执行的任务。"
	if w, ok := m.workers.Load(msg.ChannelKey); ok {
		if w.(*Worker).stopCurrentExecution() {
			reply = "已停止当前执行。"
		}
	}
	s, ok := m.senders[msg.AppID]
	if !ok {
		slog.Error("session: no sender for stop reply", "app_id", msg.AppID)
		return
	}
	if _, err := s.SendText(ctx, msg.ReceiveID, msg.ReceiveType, reply); err != nil {
		slog.Error("session: send stop reply failed", "err", err)
	}
}

// isDuplicate records the message ID and returns true if it was seen recently.
func (m *Manager) isDuplicate(msgID string) bool {
	m.recentMu.Lock()
	defer m.recentMu.Unlock()
	now := time.Now()
	if t, ok := m.recentMsgIDs[msgID]; ok && now.Sub(t) < duplicateWindow {
		return true
	}
	m.recentMsgIDs[msgID] = now
	// Simple reset to avoid unbounded growth under extreme load.
	if len(m.recentMsgIDs) > 10000 {
		m.recentMsgIDs = make(map[string]time.Time)
	}
	return false
}

// Wait blocks until all active session workers have exited.
// Call this after cancelling the context to achieve a clean shutdown.
func (m *Manager) Wait() {
	m.wg.Wait()
}

// getOrCreateWorker returns the existing Worker or starts a new one.
func (m *Manager) getOrCreateWorker(ctx context.Context, msg *feishu.IncomingMessage) *Worker {
	if w, ok := m.workers.Load(msg.ChannelKey); ok {
		return w.(*Worker)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring lock.
	if w, ok := m.workers.Load(msg.ChannelKey); ok {
		return w.(*Worker)
	}

	appCfg, ok := m.appRegistry[msg.AppID]
	if !ok {
		slog.Error("session: unknown app_id", "app_id", msg.AppID)
		return nil
	}

	sender, ok := m.senders[msg.AppID]
	if !ok {
		slog.Error("session: no sender for app", "app_id", msg.AppID)
		return nil
	}

	// Ensure the channel record exists so heartbeat/schedule can resolve notify targets.
	m.ensureChannel(msg)

	idleTimeout := time.Duration(m.cfg.Session.WorkerIdleTimeoutMinutes) * time.Minute
	worker := newWorker(msg.ChannelKey, appCfg, m.cfg, m.cfg.Session, m.db, m.executor, sender, idleTimeout, m.summarizer, m.retriever, m.scheduleMgr)

	m.workers.Store(msg.ChannelKey, worker)

	// H-6: track each worker goroutine so Wait() can block until all finish.
	m.wg.Add(1)
	go worker.run(ctx, func() {
		m.wg.Done()
		m.workers.Delete(msg.ChannelKey)
		slog.Info("session worker exited", "channel", msg.ChannelKey)
	})

	return worker
}

func (m *Manager) ensureChannel(msg *feishu.IncomingMessage) {
	ch := model.Channel{
		ChannelKey: msg.ChannelKey,
		AppID:      msg.AppID,
		ChatType:   msg.ChatType,
		ChatID:     msg.ChatID,
		ThreadID:   msg.ThreadID,
	}
	now := time.Now()
	if err := m.db.Where("channel_key = ?", msg.ChannelKey).Assign(map[string]any{
		"updated_at": now,
		"chat_type":  msg.ChatType,
		"chat_id":    msg.ChatID,
		"thread_id":  msg.ThreadID,
	}).FirstOrCreate(&ch).Error; err != nil {
		slog.Error("session: ensure channel failed", "channel", msg.ChannelKey, "err", err)
	}
}
