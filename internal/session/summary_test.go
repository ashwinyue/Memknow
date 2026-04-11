package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/model"
	"github.com/google/uuid"
)

type fakeSummaryExecutor struct {
	lastReq *claude.ExecuteRequest
	result  string
}

func (f *fakeSummaryExecutor) Execute(_ context.Context, req *claude.ExecuteRequest) (*claude.ExecuteResult, error) {
	f.lastReq = req
	return &claude.ExecuteResult{Text: f.result}, nil
}

func TestSummarizer_UsesAppConfigFromSessionChannelKey(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	cfg := &config.Config{
		Apps: []config.AppConfig{
			{ID: "app1", WorkspaceDir: "/tmp/ws-app1"},
			{ID: "fish", WorkspaceDir: "/tmp/ws-fish"},
		},
	}
	exec := &fakeSummaryExecutor{result: "summary"}
	s := NewSummarizer(database, cfg, exec)

	sessionID := uuid.New().String()
	if err := database.Create(&model.Session{
		ID:         sessionID,
		ChannelKey: "group:oc_chat:fish",
		Type:       model.SessionTypeChat,
		Status:     "archived",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := database.Create(&model.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      "user",
		Content:   "请总结这段对话",
		CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	got, err := s.Summarize(sessionID)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if got != "summary" {
		t.Fatalf("Summarize() = %q, want summary", got)
	}
	if exec.lastReq == nil || exec.lastReq.AppConfig == nil {
		t.Fatalf("executor request not recorded")
	}
	if exec.lastReq.AppConfig.ID != "fish" {
		t.Fatalf("app id = %q, want fish", exec.lastReq.AppConfig.ID)
	}
}

func TestAppIDFromChannelKey(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{key: "p2p:oc_xxx:app1", want: "app1"},
		{key: "group:oc_xxx:fish", want: "fish"},
		{key: "thread:oc_xxx:th_1:fish", want: "fish"},
		{key: "heartbeat:fish", want: "fish"},
		{key: "invalid", want: ""},
	}

	for _, c := range cases {
		if got := appIDFromChannelKey(c.key); got != c.want {
			t.Fatalf("appIDFromChannelKey(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

