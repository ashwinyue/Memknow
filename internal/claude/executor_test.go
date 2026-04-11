package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwinyue/Memknow/internal/config"
)

func TestChannelKeyToRoutingKey(t *testing.T) {
	tests := []struct {
		channelKey string
		want       string
	}{
		{"p2p:ou_abc:cli_app1", "p2p:ou_abc"},
		{"group:oc_xyz:cli_app1", "group:oc_xyz"},
		{"thread:oc_xyz:tid_123:cli_app1", "group:oc_xyz"},
		{"", ""},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := channelKeyToRoutingKey(tt.channelKey)
		if got != tt.want {
			t.Errorf("channelKeyToRoutingKey(%q) = %q, want %q", tt.channelKey, got, tt.want)
		}
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"ANTHROPIC_BASE_URL=https://old.example.com",
		"ANTHROPIC_AUTH_TOKEN=old-token",
		"ANTHROPIC_MODEL=old-model",
		"HOME=/root",
		"PATH=/usr/bin",
		"WORKSPACE_DIR=/tmp",
	}

	filtered := filterEnv(env, "ANTHROPIC_")

	if len(filtered) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(filtered), filtered)
	}
	for _, e := range filtered {
		if strings.HasPrefix(e, "ANTHROPIC_") {
			t.Errorf("ANTHROPIC_ var should be removed: %q", e)
		}
	}
	assertEnvContains(t, filtered, "HOME", "/root")
	assertEnvContains(t, filtered, "PATH", "/usr/bin")
	assertEnvContains(t, filtered, "WORKSPACE_DIR", "/tmp")
}

// assertEnvContains checks that envs contains "KEY=value".
func assertEnvContains(t *testing.T, envs []string, key, value string) {
	t.Helper()
	expected := key + "=" + value
	for _, e := range envs {
		if e == expected {
			return
		}
	}
	t.Errorf("env %q=%q not found in %v", key, value, envs)
}

func TestWriteSessionContext(t *testing.T) {
	tests := []struct {
		name      string
		req       *ExecuteRequest
		dbPath    string
		wantLines []string
	}{
		{
			name: "all fields written",
			req: &ExecuteRequest{
				AppConfig:    &config.AppConfig{ID: "app1"},
				WorkspaceDir: "/workspace/app1",
				SessionID:    "sess-123",
				ChannelKey:   "p2p:ou_abc:cli_app1",
			},
			dbPath: "/data/bot.db",
			wantLines: []string{
				"- App ID: app1",
				"- Home: /workspace/app1",
				"- Workspace: /workspace/app1",
				"- Session ID: sess-123",
				"- Channel key: p2p:ou_abc:cli_app1",
				"- DB path: /data/bot.db",
			},
		},
		{
			name: "group channel key",
			req: &ExecuteRequest{
				AppConfig:    &config.AppConfig{ID: "app2"},
				WorkspaceDir: "/workspace/app2",
				SessionID:    "sess-456",
				ChannelKey:   "group:oc_xyz:cli_app2",
			},
			dbPath: "/var/bot.db",
			wantLines: []string{
				"- Channel key: group:oc_xyz:cli_app2",
				"- DB path: /var/bot.db",
			},
		},
		{
			name: "empty channel key still written",
			req: &ExecuteRequest{
				AppConfig:    &config.AppConfig{ID: "app3"},
				WorkspaceDir: "/workspace/app3",
				SessionID:    "sess-789",
				ChannelKey:   "",
			},
			dbPath: "/data/bot.db",
			wantLines: []string{
				"- Channel key: ",
				"- DB path: /data/bot.db",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := writeSessionContext(dir, tt.req, tt.dbPath)
			if err != nil {
				t.Fatalf("writeSessionContext() error = %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "SESSION_CONTEXT.md"))
			if err != nil {
				t.Fatalf("read SESSION_CONTEXT.md: %v", err)
			}
			content := string(data)
			for _, want := range tt.wantLines {
				if !strings.Contains(content, want) {
					t.Errorf("SESSION_CONTEXT.md missing %q\ngot:\n%s", want, content)
				}
			}
		})
	}
}
