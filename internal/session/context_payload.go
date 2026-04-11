package session

import (
	"fmt"
	"strings"

	"github.com/ashwinyue/Memknow/internal/model"
)

// ContextPayload holds historical context to be injected into the prompt.
type ContextPayload struct {
	Summaries []model.SessionSummary
	Matches   []SearchMatch
}

// IsEmpty reports whether there is anything to inject.
func (p ContextPayload) IsEmpty() bool {
	return len(p.Summaries) == 0 && len(p.Matches) == 0
}

// ToPrompt formats the payload as a Markdown block suitable for prepending to the user prompt.
func (p ContextPayload) ToPrompt() string {
	if p.IsEmpty() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## 相关历史记录\n\n")

	for _, s := range p.Summaries {
		sb.WriteString(fmt.Sprintf("- [会话摘要] %s: %s\n", s.CreatedAt.Format("2006-01-02"), s.Content))
	}

	groups := groupMatchesBySession(p.Matches, 5, 3)
	if len(groups) > 0 {
		if len(p.Summaries) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("### 历史片段\n")
		for _, g := range groups {
			sb.WriteString(fmt.Sprintf("- %s\n", sessionDisplayName(g.SessionID, g.SessionTitle)))
			for _, m := range g.Items {
				prefix := m.Role
				if prefix == "" {
					prefix = "消息"
				}
				sb.WriteString(fmt.Sprintf("  - [%s] %s: %s\n", prefix, m.CreatedAt.Format("2006-01-02"), matchExcerpt(m, 200)))
			}
		}
	}

	sb.WriteString("\n")
	return sb.String()
}
