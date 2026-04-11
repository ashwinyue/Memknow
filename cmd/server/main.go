package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/cleanup"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/feishu"
	"github.com/ashwinyue/Memknow/internal/heartbeat"
	"github.com/ashwinyue/Memknow/internal/model"
	schedulepkg "github.com/ashwinyue/Memknow/internal/schedule"
	"github.com/ashwinyue/Memknow/internal/session"
	"github.com/ashwinyue/Memknow/internal/workspace"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	// ── Logging ──────────────────────────────────────────────────
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ── Singleton lock (prevents launchd/KeepAlive from spawning duplicates) ──
	lockFile, err := acquireSingletonLock("memknow.lock")
	if err != nil {
		slog.Error("singleton lock failed", "err", err)
		os.Exit(1)
	}
	defer lockFile.Close()

	// ── Config ───────────────────────────────────────────────────
	cfg, err := config.Load(*configPath, true)
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}
	// H-4: validate all required fields at startup.
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}

	// ── Database ──────────────────────────────────────────────────
	database, err := db.Open("bot.db")
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	if absDB, err := filepath.Abs("bot.db"); err == nil {
		cfg.DBPath = absDB
	}

	// ── Workspace init ────────────────────────────────────────────
	// Templates are embedded in the binary (internal/workspace/template).
	// External template directories can still be passed via the second argument.
	for _, appCfg := range cfg.Apps {
		if err := workspace.Init(appCfg.WorkspaceDir, "", appCfg.FeishuAppID, appCfg.FeishuAppSecret, cfg.Language, appCfg.NormalizedWorkspaceTemplate()); err != nil {
			slog.Error("init workspace", "app", appCfg.ID, "err", err)
			os.Exit(1)
		}
		slog.Info("workspace ready", "app", appCfg.ID, "dir", appCfg.WorkspaceDir, "lang", cfg.Language, "template", appCfg.NormalizedWorkspaceTemplate())
	}

	// Recover from previous unclean shutdown: archive any stale active system sessions.
	if err := database.Model(&model.Session{}).
		Where("type IN ? AND status = ?", []string{model.SessionTypeHeartbeat, model.SessionTypeSchedule}, "active").
		Update("status", "archived").Error; err != nil {
		slog.Warn("failed to archive stale system sessions", "err", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Claude executor ───────────────────────────────────────────
	var executor claude.ExecutorInterface = claude.NewInteractiveExecutor(cfg)
	slog.Info("using interactive claude executor")

	// ── Feishu receivers + senders ────────────────────────────────
	senders := make(map[string]*feishu.Sender, len(cfg.Apps))
	receivers := make([]*feishu.Receiver, 0, len(cfg.Apps))
	botIDs := make([]string, 0, len(cfg.Apps))
	for _, appCfg := range cfg.Apps {
		botIDs = append(botIDs, appCfg.ID)
	}

	// C-1: use atomic.Pointer to avoid the data race between the main goroutine
	// writing fwd.target and the WS receive goroutines reading it.
	// The pointer is set before any goroutine is launched, so the atomic store
	// is strictly for correctness documentation — Go's memory model already
	// guarantees visibility across the go statement, but atomic makes the
	// race detector happy and the intent explicit.
	fwd := &dispatchForwarder{}

	for i := range cfg.Apps {
		appCfg := &cfg.Apps[i]
		recv := feishu.NewReceiver(appCfg, botIDs, fwd)
		senders[appCfg.ID] = feishu.NewSender(recv.LarkClient())
		receivers = append(receivers, recv)
	}

	// ── Start executor background tasks (e.g. interactive session reaper) ─
	if starter, ok := executor.(interface{ Start(context.Context) }); ok {
		starter.Start(ctx)
	}

	// ── Session manager ───────────────────────────────────────────
	scheduleSenders := make(map[string]schedulepkg.Sender, len(senders))
	for appID, sender := range senders {
		scheduleSenders[appID] = sender
	}
	scheduleSvc, err := schedulepkg.NewService(cfg, database, executor, scheduleSenders)
	if err != nil {
		slog.Error("create schedule service", "err", err)
		os.Exit(1)
	}
	if err := scheduleSvc.Bootstrap(ctx); err != nil {
		slog.Error("bootstrap schedules", "err", err)
		os.Exit(1)
	}
	sessionMgr := session.NewManager(cfg, database, executor, senders, scheduleSvc)
	// Store BEFORE launching any goroutine (Go memory model guarantees visibility
	// across go statements; atomic.Store documents the ordering intent).
	fwd.target.Store(sessionMgr)

	if _, err := cleanup.NewService(database, cfg.Apps, cfg.Cleanup, scheduleSvc); err != nil {
		slog.Error("register cleanup job", "err", err)
		os.Exit(1)
	}

	heartbeatSenders := make(map[string]heartbeat.Sender, len(senders))
	for appID, sender := range senders {
		heartbeatSenders[appID] = sender
	}
	heartbeatSvc := heartbeat.NewService(cfg, database, executor, heartbeatSenders)
	heartbeatSvc.Start(ctx)
	config.RegisterHeartbeatChangeCallback(func(_ config.HeartbeatConfig) {
		heartbeatSvc.Restart(ctx)
	})

	// ── HTTP health check ─────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	// H-7: set read/write timeouts to prevent resource exhaustion.
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		slog.Info("HTTP server listening", "port", cfg.Server.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server", "err", err)
		}
	}()

	// ── Start Feishu WS clients ───────────────────────────────────
	for _, recv := range receivers {
		r := recv
		go func() {
			if err := r.Start(ctx); err != nil {
				slog.Error("feishu WS client error", "err", err)
			}
		}()
	}

	slog.Info("Memknow started", "apps", len(cfg.Apps))

	// ── Wait for shutdown signal ──────────────────────────────────
	<-ctx.Done()
	slog.Info("shutting down...")

	// H-6: wait for all session workers to finish their in-flight requests.
	sessionMgr.Wait()

	// Stop executor background tasks.
	if stopper, ok := executor.(interface{ Stop() }); ok {
		stopper.Stop()
	}
	scheduleSvc.Stop()

	// M-8: use a timeout context for HTTP shutdown and log any error.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown", "err", err)
	}

	slog.Info("bye")
}

// dispatchForwarder holds a pointer to session.Manager via atomic.Pointer.
// C-1: atomic load/store prevents any data race between the main goroutine
// (which stores after construction) and WS receive goroutines (which load on message).
type dispatchForwarder struct {
	target atomic.Pointer[session.Manager]
}

func (f *dispatchForwarder) Dispatch(ctx context.Context, msg *feishu.IncomingMessage) error {
	mgr := f.target.Load()
	if mgr == nil {
		return nil
	}
	return mgr.Dispatch(ctx, msg)
}

// acquireSingletonLock opens the given path and tries to acquire an exclusive
// flock. If another process already holds the lock, it returns an error.
func acquireSingletonLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another instance is already running")
		}
		return nil, fmt.Errorf("flock: %w", err)
	}
	return f, nil
}
