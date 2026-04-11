package heartbeat

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/model"
)

func TestPromptPath(t *testing.T) {
	app := config.AppConfig{
		ID:           "app1",
		WorkspaceDir: "/tmp/ws",
	}
	cfg := config.HeartbeatConfig{PromptFile: "HEARTBEAT.md"}

	got := promptPath(app, cfg)
	want := filepath.Join("/tmp/ws", "HEARTBEAT.md")
	if got != want {
		t.Fatalf("promptPath() = %q, want %q", got, want)
	}
}

func TestShouldEnable(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.HeartbeatConfig
		want bool
	}{
		{name: "disabled", cfg: config.HeartbeatConfig{Enabled: false, IntervalMinutes: 10}, want: false},
		{name: "enabled", cfg: config.HeartbeatConfig{Enabled: true, IntervalMinutes: 10}, want: true},
		{name: "zero interval", cfg: config.HeartbeatConfig{Enabled: true, IntervalMinutes: 0}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldEnable(tt.cfg); got != tt.want {
				t.Fatalf("shouldEnable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotifyTarget_UsesExplicitConfig(t *testing.T) {
	svc := &Service{
		cfg: &config.Config{
			Heartbeat: config.HeartbeatConfig{
				NotifyTargetType: "group",
				NotifyTargetID:   "oc_group_x",
			},
		},
	}

	typ, id, ok := svc.notifyTarget("app1")
	if !ok {
		t.Fatalf("notifyTarget() ok = false, want true")
	}
	if typ != "group" || id != "oc_group_x" {
		t.Fatalf("notifyTarget() = (%q,%q), want (group,oc_group_x)", typ, id)
	}
}

func TestNotifyTarget_EmptyConfig(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now()
	for _, ch := range []model.Channel{
		{ChannelKey: "p2p:ou_a:app1", AppID: "app1", ChatType: "p2p", ChatID: "ou_a", CreatedAt: now, UpdatedAt: now},
		{ChannelKey: "p2p:ou_b:app1", AppID: "app1", ChatType: "p2p", ChatID: "ou_b", CreatedAt: now, UpdatedAt: now.Add(time.Minute)},
		{ChannelKey: "group:oc_group:app1", AppID: "app1", ChatType: "group", ChatID: "oc_group", CreatedAt: now, UpdatedAt: now},
	} {
		if err := database.Create(&ch).Error; err != nil {
			t.Fatalf("create channel %s: %v", ch.ChannelKey, err)
		}
	}
	svc := &Service{
		cfg: &config.Config{Heartbeat: config.HeartbeatConfig{}},
		db:  database,
	}

	targets := svc.notifyTargets("app1")
	if len(targets) != 2 {
		t.Fatalf("notifyTargets() len = %d, want 2", len(targets))
	}
	if targets[0].ChatType != "p2p" || targets[0].ChatID != "ou_b" {
		t.Fatalf("targets[0] = %+v, want latest p2p ou_b", targets[0])
	}
	if targets[1].ChatType != "p2p" || targets[1].ChatID != "ou_a" {
		t.Fatalf("targets[1] = %+v, want second p2p ou_a", targets[1])
	}
}
