package workspace

import (
	"path/filepath"
	"testing"
)

func TestSessionDir_ByType(t *testing.T) {
	workspaceDir := "/tmp/ws"

	tests := []struct {
		name        string
		sessionType string
		sessionID   string
		want        string
	}{
		{name: "chat", sessionType: "chat", sessionID: "s1", want: filepath.Join(workspaceDir, "sessions", "chat", "s1")},
		{name: "heartbeat", sessionType: "heartbeat", sessionID: "s2", want: filepath.Join(workspaceDir, "sessions", "heartbeat", "s2")},
		{name: "schedule", sessionType: "schedule", sessionID: "s3", want: filepath.Join(workspaceDir, "sessions", "schedule", "s3")},
		{name: "default", sessionType: "", sessionID: "s4", want: filepath.Join(workspaceDir, "sessions", "chat", "s4")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionDir(workspaceDir, tt.sessionType, tt.sessionID)
			if got != tt.want {
				t.Errorf("SessionDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

