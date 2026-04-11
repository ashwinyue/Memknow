package workspace

import (
	"path/filepath"

	"github.com/ashwinyue/Memknow/internal/model"
)

func SessionTypeDir(workspaceDir, sessionType string) string {
	return filepath.Join(workspaceDir, "sessions", model.NormalizeSessionType(sessionType))
}

func SessionDir(workspaceDir, sessionType, sessionID string) string {
	return filepath.Join(SessionTypeDir(workspaceDir, sessionType), sessionID)
}

func SessionAttachmentsDir(workspaceDir, sessionType, sessionID string) string {
	return filepath.Join(SessionDir(workspaceDir, sessionType, sessionID), "attachments")
}

