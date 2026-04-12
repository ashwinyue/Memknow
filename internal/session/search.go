package session

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ashwinyue/Memknow/internal/model"
	"gorm.io/gorm"
)

// ContextMsg is a single message in the surrounding context of a search match.
type ContextMsg struct {
	Role    string
	Content string
}

// SearchMatch is a single FTS5 search result with snippet and surrounding context.
type SearchMatch struct {
	model.Message
	SessionID    string       `gorm:"column:session_id"`
	SessionTitle string       `gorm:"column:session_title"`
	RowID        int64        `gorm:"column:rowid"`
	RankScore    float64      `gorm:"column:rank_score"`
	Snippet      string       `gorm:"column:snippet"`
	Context      []ContextMsg `gorm:"-"`
}

// searchMessages performs an FTS5 search across messages for a given channel.
func searchMessages(db *gorm.DB, channelKey, query string, limit int) ([]SearchMatch, error) {
	return searchMessagesWithStatus(db, channelKey, query, "", "", limit)
}

// searchMessagesWithStatus performs an FTS5 search with optional session status and type filter.
// Empty statusFilter or sessionType means do not filter on that field.
func searchMessagesWithStatus(db *gorm.DB, channelKey, query, statusFilter, sessionType string, limit int) ([]SearchMatch, error) {
	sanitized := sanitizeFTS5Query(query)
	if sanitized == "" {
		return nil, nil
	}

	var conditions []string
	var args []any

	conditions = append(conditions, "messages_fts MATCH ?", "s.channel_key = ?")
	args = append(args, sanitized, channelKey)

	if statusFilter != "" {
		conditions = append(conditions, "s.status = ?")
		args = append(args, statusFilter)
	}
	if sessionType != "" {
		conditions = append(conditions, "s.type = ?")
		args = append(args, sessionType)
	}

	sql := fmt.Sprintf(`
SELECT m.*, m.rowid AS rowid, s.id AS session_id, s.title AS session_title,
       bm25(messages_fts) AS rank_score,
       snippet(messages_fts, 0, '>>>', '<<<', '...', 32) AS snippet
FROM messages_fts
JOIN messages m ON m.rowid = messages_fts.rowid
JOIN sessions s ON s.id = m.session_id
WHERE %s
ORDER BY rank_score ASC
LIMIT ?`, strings.Join(conditions, " AND "))
	args = append(args, limit)

	var matches []SearchMatch
	rows, err := db.Raw(sql, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("fts5 query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var match SearchMatch
		if err := db.ScanRows(rows, &match); err != nil {
			continue
		}
		matches = append(matches, match)
	}

	// Load surrounding context (1 message before and after each match).
	for i := range matches {
		loadMatchContext(db, &matches[i])
	}

	return matches, nil
}

// loadMatchContext fetches 1 message before and after the matched row.
func loadMatchContext(db *gorm.DB, match *SearchMatch) {
	var msgs []struct {
		Role    string
		Content string
	}
	err := db.Raw(
		`SELECT role, content FROM messages
		 WHERE session_id = ? AND rowid >= ? - 1 AND rowid <= ? + 1
		 ORDER BY rowid`,
		match.SessionID, match.RowID, match.RowID,
	).Scan(&msgs).Error
	if err != nil {
		return
	}
	match.Context = make([]ContextMsg, 0, len(msgs))
	for _, m := range msgs {
		content := m.Content
		if len(content) > 200 {
			content = content[:200]
		}
		match.Context = append(match.Context, ContextMsg{
			Role:    m.Role,
			Content: content,
		})
	}
}

var (
	reFTS5Quoted          = regexp.MustCompile(`"[^"]*"`)
	reFTS5Special         = regexp.MustCompile(`[+{}()\"^]`)
	reFTS5Asterisk        = regexp.MustCompile(`\*+`)
	reFTS5LeadingAsterisk = regexp.MustCompile(`(^|\s)\*`)
	reFTS5LeadingBool     = regexp.MustCompile(`(?i)^(AND|OR|NOT)\b\s*`)
	reFTS5TrailingBool    = regexp.MustCompile(`(?i)\s+(AND|OR|NOT)\s*$`)
	reFTS5Hyphenated      = regexp.MustCompile(`\b(\w+(?:[.-]\w+)+)\b`)
)

// sanitizeFTS5Query sanitizes user input for safe use in FTS5 MATCH queries.
func sanitizeFTS5Query(query string) string {
	if strings.TrimSpace(query) == "" {
		return ""
	}

	// Step 1: Extract balanced double-quoted phrases and protect them.
	var quotedParts []string
	sanitized := reFTS5Quoted.ReplaceAllStringFunc(query, func(s string) string {
		quotedParts = append(quotedParts, s)
		return fmt.Sprintf("\x00Q%d\x00", len(quotedParts)-1)
	})

	// Step 2: Strip remaining (unmatched) FTS5-special characters.
	sanitized = reFTS5Special.ReplaceAllString(sanitized, " ")

	// Step 3: Collapse repeated * and remove leading *.
	sanitized = reFTS5Asterisk.ReplaceAllString(sanitized, "*")
	sanitized = reFTS5LeadingAsterisk.ReplaceAllString(sanitized, "${1}")

	// Step 4: Remove dangling boolean operators at start/end.
	sanitized = reFTS5LeadingBool.ReplaceAllString(strings.TrimSpace(sanitized), "")
	sanitized = reFTS5TrailingBool.ReplaceAllString(strings.TrimSpace(sanitized), "")

	// Step 5: Wrap unquoted hyphenated and dotted terms in double quotes.
	sanitized = reFTS5Hyphenated.ReplaceAllString(sanitized, `"$1"`)

	// Step 6: Restore preserved quoted phrases.
	for i, q := range quotedParts {
		sanitized = strings.ReplaceAll(sanitized, fmt.Sprintf("\x00Q%d\x00", i), q)
	}

	sanitized = strings.TrimSpace(sanitized)
	if !hasHanRune(sanitized) {
		return sanitized
	}

	expanded := buildCJKBigramQuery(sanitized)
	if expanded == "" {
		return sanitized
	}
	return expanded
}

func hasHanRune(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func buildCJKBigramQuery(s string) string {
	var cjkRunes []rune
	var nonCJKWords []string
	var buf strings.Builder

	flushNonCJK := func() {
		if buf.Len() == 0 {
			return
		}
		w := strings.TrimSpace(buf.String())
		if w != "" {
			nonCJKWords = append(nonCJKWords, w)
		}
		buf.Reset()
	}

	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			flushNonCJK()
			cjkRunes = append(cjkRunes, r)
		case unicode.IsSpace(r) || r == '|':
			flushNonCJK()
		case r == '"' || r == '*' || r == '(' || r == ')' || r == '{' || r == '}' || r == '+' || r == '^':
			flushNonCJK()
		default:
			buf.WriteRune(r)
		}
	}
	flushNonCJK()

	var parts []string
	if len(cjkRunes) == 1 {
		parts = append(parts, `"`+string(cjkRunes[0])+`"`)
	} else {
		for i := 0; i < len(cjkRunes)-1; i++ {
			parts = append(parts, `"`+string(cjkRunes[i])+string(cjkRunes[i+1])+`"`)
		}
	}
	for _, w := range nonCJKWords {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		w = strings.ReplaceAll(w, `"`, `""`)
		parts = append(parts, `"`+w+`"`)
	}

	return strings.Join(parts, " OR ")
}
