package claude

import (
	"strings"
	"testing"

	"github.com/ashwinyue/Memknow/internal/model"
)

func TestRenderBasePromptDefaultsToChatMode(t *testing.T) {
	got := renderBasePrompt("unknown", "/workspace", "/session", "/memory", "/attachments", "/context")

	if !strings.Contains(got, "workspace in **chat mode**") {
		t.Fatalf("expected chat mode prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "Read `/workspace/USER.md` and `/workspace/MEMORY.md` at the start of every conversation.") {
		t.Fatalf("expected chat-specific behavior, got:\n%s", got)
	}
	if !strings.Contains(got, "When the user asks for code review, commit review, diff inspection, or change review, read `skills/review.md` before taking action.") {
		t.Fatalf("expected review-skill trigger rule, got:\n%s", got)
	}
}

func TestRenderBasePromptHeartbeatIncludesHeartbeatSpecificBehavior(t *testing.T) {
	got := renderBasePrompt(model.SessionTypeHeartbeat, "/workspace", "/session", "/memory", "/attachments", "/context")

	if !strings.Contains(got, "workspace in **heartbeat mode**") {
		t.Fatalf("expected heartbeat mode prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "reply with exactly `HEARTBEAT_OK`.") {
		t.Fatalf("expected HEARTBEAT_OK instruction, got:\n%s", got)
	}
	if !strings.Contains(got, "Edit `/workspace/HEARTBEAT.md` freely when the checklist needs adjustment.") {
		t.Fatalf("expected heartbeat checklist editing instruction, got:\n%s", got)
	}
}

func TestRenderBasePromptScheduleIncludesScheduleSpecificBehavior(t *testing.T) {
	got := renderBasePrompt(model.SessionTypeSchedule, "/workspace", "/session", "/memory", "/attachments", "/context")

	if !strings.Contains(got, "workspace in **schedule mode**") {
		t.Fatalf("expected schedule mode prompt, got:\n%s", got)
	}
	if !strings.Contains(got, "Keep the output concise and directly useful to the receiver.") {
		t.Fatalf("expected schedule visibility instruction, got:\n%s", got)
	}
	if !strings.Contains(got, "When appropriate, update memory after meaningful schedule executions.") {
		t.Fatalf("expected schedule-specific behavior, got:\n%s", got)
	}
}

func TestRenderBasePromptIncludesSharedSafetyRulesForAllModes(t *testing.T) {
	sessionTypes := []string{
		model.SessionTypeChat,
		model.SessionTypeHeartbeat,
		model.SessionTypeSchedule,
	}

	for _, sessionType := range sessionTypes {
		got := renderBasePrompt(sessionType, "/workspace", "/session", "/memory", "/attachments", "/context")

		if !strings.Contains(got, "## Safety") {
			t.Fatalf("expected shared safety section for %s, got:\n%s", sessionType, got)
		}
		if !strings.Contains(got, "Do not use guessed project-relative prefixes like `workspaces/...`.") {
			t.Fatalf("expected shared path rule for %s, got:\n%s", sessionType, got)
		}
		if !strings.Contains(got, "All long-term memory must be written to the current workspace file system (`/memory`).") {
			t.Fatalf("expected shared memory rule for %s, got:\n%s", sessionType, got)
		}
	}
}

func TestRenderBasePromptIncludesSilentOperationsProtocolForAllModes(t *testing.T) {
	sessionTypes := []string{
		model.SessionTypeChat,
		model.SessionTypeHeartbeat,
		model.SessionTypeSchedule,
	}

	for _, sessionType := range sessionTypes {
		got := renderBasePrompt(sessionType, "/workspace", "/session", "/memory", "/attachments", "/context")

		if !strings.Contains(got, "## Silent Operations") {
			t.Fatalf("expected silent operations section for %s, got:\n%s", sessionType, got)
		}
		if !strings.Contains(got, "Tool calls and file writes are invisible to the user.") {
			t.Fatalf("expected invisible-operations rule for %s, got:\n%s", sessionType, got)
		}
		if !strings.Contains(got, "Never narrate memory writes, file edits, background processing, or skill execution to the user.") {
			t.Fatalf("expected no meta-operations narration rule for %s, got:\n%s", sessionType, got)
		}
		if !strings.Contains(got, "Do the user-visible reply first. Then perform any memory/file updates silently.") {
			t.Fatalf("expected reply-then-write ordering for %s, got:\n%s", sessionType, got)
		}
	}
}
