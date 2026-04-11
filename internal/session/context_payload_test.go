package session

import (
	"strings"
	"testing"
	"time"

	"github.com/ashwinyue/Memknow/internal/model"
)

func TestContextPayload_IsEmpty(t *testing.T) {
	cases := []struct {
		name     string
		payload  ContextPayload
		expected bool
	}{
		{"empty", ContextPayload{}, true},
		{"summaries only", ContextPayload{Summaries: []model.SessionSummary{{Content: "x"}}}, false},
		{"matches only", ContextPayload{Matches: []SearchMatch{{Message: model.Message{Content: "x"}}}}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.payload.IsEmpty(); got != c.expected {
				t.Errorf("IsEmpty() = %v, want %v", got, c.expected)
			}
		})
	}
}

func TestContextPayload_ToPrompt(t *testing.T) {
	cases := []struct {
		name     string
		payload  ContextPayload
		contains []string
	}{
		{
			name:     "empty returns empty string",
			payload:  ContextPayload{},
			contains: []string{},
		},
		{
			name: "summary and match",
			payload: ContextPayload{
				Summaries: []model.SessionSummary{
					{Content: "Discussed bug fix", CreatedAt: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)},
				},
				Matches: []SearchMatch{
					{Message: model.Message{Role: "user", Content: "hello world", CreatedAt: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)}},
				},
			},
			contains: []string{"## 相关历史记录", "[会话摘要] 2026-04-08: Discussed bug fix", "[user] 2026-04-08: hello world"},
		},
		{
			name: "long content truncated",
			payload: ContextPayload{
				Matches: []SearchMatch{
					{Message: model.Message{Role: "assistant", Content: strings.Repeat("a", 300), CreatedAt: time.Now()}},
				},
			},
			contains: []string{"[assistant]", "..."},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.payload.ToPrompt()
			if len(c.contains) == 0 {
				if got != "" {
					t.Errorf("ToPrompt() = %q, want empty string", got)
				}
				return
			}
			for _, want := range c.contains {
				if !strings.Contains(got, want) {
					t.Errorf("ToPrompt() missing %q in:\n%s", want, got)
				}
			}
		})
	}
}
