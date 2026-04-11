package session

import (
	"fmt"
	"strings"
)

type groupedMatches struct {
	SessionID    string
	SessionTitle string
	Items        []SearchMatch
}

func groupMatchesBySession(matches []SearchMatch, maxSessions, maxItemsPerSession int) []groupedMatches {
	if maxSessions <= 0 || maxItemsPerSession <= 0 || len(matches) == 0 {
		return nil
	}

	order := make([]string, 0, maxSessions)
	groups := make(map[string]*groupedMatches, maxSessions)
	seen := make(map[string]map[string]struct{}, maxSessions)

	for _, m := range matches {
		sid := m.SessionID
		if sid == "" {
			sid = "_unknown"
		}
		g, ok := groups[sid]
		if !ok {
			if len(order) >= maxSessions {
				continue
			}
			order = append(order, sid)
			g = &groupedMatches{
				SessionID:    sid,
				SessionTitle: strings.TrimSpace(m.SessionTitle),
				Items:        make([]SearchMatch, 0, maxItemsPerSession),
			}
			groups[sid] = g
			seen[sid] = map[string]struct{}{}
		}
		if len(g.Items) >= maxItemsPerSession {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(m.Snippet))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(m.Content))
		}
		if key == "" {
			continue
		}
		if _, dup := seen[sid][key]; dup {
			continue
		}
		seen[sid][key] = struct{}{}
		g.Items = append(g.Items, m)
	}

	out := make([]groupedMatches, 0, len(order))
	for _, id := range order {
		g := groups[id]
		if len(g.Items) == 0 {
			continue
		}
		out = append(out, *g)
	}
	return out
}

func formatSearchResults(query string, matches []SearchMatch) string {
	groups := groupMatchesBySession(matches, 4, 3)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 搜索 **%s** 的结果（会话 %d / 命中 %d）:\n\n", query, len(groups), len(matches)))

	if len(groups) == 0 {
		sb.WriteString("未找到可展示的历史片段")
		return sb.String()
	}

	for i, g := range groups {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, sessionDisplayName(g.SessionID, g.SessionTitle)))
		for _, m := range g.Items {
			role := m.Role
			if role == "" {
				role = "消息"
			}
			sb.WriteString(fmt.Sprintf("  - [%s] `%s` %s\n", role, m.CreatedAt.Format("01-02 15:04"), matchExcerpt(m, 120)))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func sessionDisplayName(sessionID, title string) string {
	if sessionID == "_unknown" {
		return "历史消息"
	}
	if title != "" {
		return title
	}
	if len(sessionID) <= 8 {
		return "会话 " + sessionID
	}
	return "会话 " + sessionID[:8]
}

func matchExcerpt(m SearchMatch, limit int) string {
	text := strings.TrimSpace(m.Snippet)
	if text == "" {
		text = strings.TrimSpace(m.Content)
	}
	if text == "" {
		return "(空内容)"
	}
	return trimTo(text, limit)
}

func trimTo(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
