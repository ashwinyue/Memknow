package session

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ashwinyue/Memknow/internal/model"
	"gorm.io/gorm"
)

// Retriever fetches historical context (summaries + FTS5 matches) for a channel.
type Retriever struct {
	db *gorm.DB
}

// NewRetriever creates a retriever backed by the given DB.
func NewRetriever(db *gorm.DB) *Retriever {
	return &Retriever{db: db}
}

// Retrieve pulls recent summaries and archived message matches for the query.
func (r *Retriever) Retrieve(channelKey, query string) (ContextPayload, error) {
	payload := ContextPayload{}

	// 1. Recent session summaries for this channel.
	if err := r.db.Where("channel_key = ?", channelKey).
		Order("created_at DESC").
		Limit(5).
		Find(&payload.Summaries).Error; err != nil {
		return payload, fmt.Errorf("load summaries: %w", err)
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return payload, nil
	}

	var candidates []scoredMatch

	// Route A: FTS5 route (fast exact-ish recall).
	fts, err := searchMessagesWithStatus(r.db, channelKey, q, statusArchived, 12)
	if err != nil {
		return payload, fmt.Errorf("search archived messages by fts: %w", err)
	}
	for i, m := range fts {
		candidates = append(candidates, scoredMatch{match: m, score: 100 - i})
	}

	// Route B: LIKE fallback route (covers Chinese better when tokenizer is weak).
	likeMatches, err := searchMessagesLikeWithStatus(r.db, channelKey, q, statusArchived, 12)
	if err != nil {
		return payload, fmt.Errorf("search archived messages by like: %w", err)
	}
	for i, m := range likeMatches {
		candidates = append(candidates, scoredMatch{match: m, score: 70 - i})
	}

	// Route C: summary route (session-level recall even if message-level miss).
	summaryMatches, err := searchSummaryMatches(r.db, channelKey, q, 8)
	if err != nil {
		return payload, fmt.Errorf("search session summaries: %w", err)
	}
	for i, m := range summaryMatches {
		candidates = append(candidates, scoredMatch{match: m, score: 50 - i})
	}

	payload.Matches = fuseScoredMatches(candidates, 12)

	return payload, nil
}

type scoredMatch struct {
	match SearchMatch
	score int
}

func searchMessagesLikeWithStatus(db *gorm.DB, channelKey, query, statusFilter string, limit int) ([]SearchMatch, error) {
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, nil
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"

	sql := `
SELECT m.*, m.rowid AS rowid, s.id AS session_id, s.title AS session_title,
       substr(m.content, 1, 200) AS snippet
FROM messages m
JOIN sessions s ON s.id = m.session_id
WHERE s.channel_key = ? AND lower(m.content) LIKE ?`
	args := []any{channelKey, pattern}
	if statusFilter != "" {
		sql += " AND s.status = ?"
		args = append(args, statusFilter)
	}
	sql += `
ORDER BY m.created_at DESC
LIMIT ?`
	args = append(args, limit)

	var matches []SearchMatch
	rows, err := db.Raw(sql, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m SearchMatch
		if err := db.ScanRows(rows, &m); err != nil {
			continue
		}
		matches = append(matches, m)
	}
	return matches, nil
}

func searchSummaryMatches(db *gorm.DB, channelKey, query string, limit int) ([]SearchMatch, error) {
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, nil
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"

	var rows []struct {
		SessionID    string    `gorm:"column:session_id"`
		SessionTitle string    `gorm:"column:session_title"`
		Content      string    `gorm:"column:content"`
		CreatedAt    time.Time `gorm:"column:created_at"`
	}
	err := db.Raw(`
SELECT ss.session_id AS session_id, s.title AS session_title, ss.content AS content, ss.created_at AS created_at
FROM session_summaries ss
LEFT JOIN sessions s ON s.id = ss.session_id
WHERE ss.channel_key = ? AND lower(ss.content) LIKE ?
ORDER BY ss.created_at DESC
LIMIT ?`, channelKey, pattern, limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	out := make([]SearchMatch, 0, len(rows))
	for _, r := range rows {
		text := "会话摘要命中: " + strings.TrimSpace(r.Content)
		out = append(out, SearchMatch{
			Message: model.Message{
				Role:      "summary",
				Content:   text,
				CreatedAt: r.CreatedAt,
			},
			SessionID:    r.SessionID,
			SessionTitle: r.SessionTitle,
			Snippet:      trimTo(text, 200),
		})
	}
	return out, nil
}

func fuseScoredMatches(candidates []scoredMatch, limit int) []SearchMatch {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}

	best := make(map[string]scoredMatch, len(candidates))
	for _, c := range candidates {
		key := dedupeKey(c.match)
		if key == "" {
			continue
		}
		prev, ok := best[key]
		if !ok || c.score > prev.score || (c.score == prev.score && c.match.CreatedAt.After(prev.match.CreatedAt)) {
			best[key] = c
		}
	}

	merged := make([]scoredMatch, 0, len(best))
	for _, v := range best {
		merged = append(merged, v)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		return merged[i].match.CreatedAt.After(merged[j].match.CreatedAt)
	})

	perSession := map[string]int{}
	out := make([]SearchMatch, 0, min(limit, len(merged)))
	for _, c := range merged {
		sid := c.match.SessionID
		if sid != "" && perSession[sid] >= 3 {
			continue
		}
		if sid != "" {
			perSession[sid]++
		}
		out = append(out, c.match)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func dedupeKey(m SearchMatch) string {
	text := strings.ToLower(normalizeSnippet(strings.TrimSpace(m.Snippet)))
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(m.Content))
	}
	if text == "" {
		return ""
	}
	return m.SessionID + "|" + text
}

func normalizeSnippet(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(">>>", "", "<<<", "", "...", " ")
	s = replacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
