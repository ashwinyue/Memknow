package session

import (
	"strings"
	"testing"
	"time"

	"github.com/ashwinyue/Memknow/internal/model"
)

func TestGroupMatchesBySession(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	matches := []SearchMatch{
		{
			Message:      model.Message{Role: "user", Content: "deploy script path", CreatedAt: now},
			SessionID:    "s1",
			SessionTitle: "部署会话",
			Snippet:      "deploy script path",
		},
		{
			Message:      model.Message{Role: "assistant", Content: "same", CreatedAt: now},
			SessionID:    "s1",
			SessionTitle: "部署会话",
			Snippet:      "deploy script path", // duplicate snippet, should dedupe
		},
		{
			Message:      model.Message{Role: "user", Content: "release checklist", CreatedAt: now},
			SessionID:    "s2",
			SessionTitle: "发布会话",
			Snippet:      "release checklist",
		},
	}

	groups := groupMatchesBySession(matches, 4, 3)
	if len(groups) != 2 {
		t.Fatalf("groups len = %d, want 2", len(groups))
	}
	if groups[0].SessionID != "s1" || len(groups[0].Items) != 1 {
		t.Fatalf("first group unexpected: %+v", groups[0])
	}
	if groups[1].SessionID != "s2" || len(groups[1].Items) != 1 {
		t.Fatalf("second group unexpected: %+v", groups[1])
	}
}

func TestFormatSearchResults(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	text := formatSearchResults("部署", []SearchMatch{
		{
			Message:      model.Message{Role: "user", Content: "deploy script path", CreatedAt: now},
			SessionID:    "s1",
			SessionTitle: "部署会话",
			Snippet:      "deploy script path",
		},
	})

	wantContains := []string{"搜索 **部署**", "部署会话", "[user]", "deploy script path"}
	for _, want := range wantContains {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted text missing %q:\n%s", want, text)
		}
	}
}

