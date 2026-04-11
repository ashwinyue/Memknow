package claude

import (
	_ "embed"
	"strings"

	"github.com/ashwinyue/Memknow/internal/model"
)

//go:embed prompts/base.md
var basePromptTemplate string

//go:embed prompts/chat.md
var chatPromptTemplate string

//go:embed prompts/heartbeat.md
var heartbeatPromptTemplate string

//go:embed prompts/schedule.md
var schedulePromptTemplate string

func renderBasePrompt(sessionType, workspaceDir, sessionDir, memoryDir, attachmentsDir, contextPath string) string {
	var modePrompt string
	switch model.NormalizeSessionType(sessionType) {
	case model.SessionTypeHeartbeat:
		modePrompt = heartbeatPromptTemplate
	case model.SessionTypeSchedule:
		modePrompt = schedulePromptTemplate
	default:
		modePrompt = chatPromptTemplate
	}

	s := strings.TrimSpace(modePrompt) + "\n\n" + strings.TrimSpace(basePromptTemplate)
	s = strings.ReplaceAll(s, "{{WORKSPACE_DIR}}", workspaceDir)
	s = strings.ReplaceAll(s, "{{SESSION_DIR}}", sessionDir)
	s = strings.ReplaceAll(s, "{{MEMORY_DIR}}", memoryDir)
	s = strings.ReplaceAll(s, "{{ATTACHMENTS_DIR}}", attachmentsDir)
	s = strings.ReplaceAll(s, "{{CONTEXT_PATH}}", contextPath)
	return s
}
