package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	defaultMemoryMaxChars = 300
	defaultMemoryTopN     = 3
	memoryEntryDelimiter  = "\n§\n"
)

type memoryEntry struct {
	Source  string
	Content string
}

// retrieveMemories searches workspace memory files for entries relevant to query.
// When a database is available, it syncs files into memory_files and queries via
// keyword-matched LIKE over the cached content. Otherwise it falls back to the
// original in-memory keyword matching.
func (w *Worker) retrieveMemories(query string) []string {
	if w.db != nil {
		if err := w.syncMemoryFiles(); err == nil {
			return w.searchMemoriesDB(query)
		}
	}
	return w.retrieveMemoriesFallback(query)
}

// syncMemoryFiles scans the workspace memory files and syncs them into the
// memory_files table. It recursively walks the memory/ directory and also handles
// the top-level MEMORY.md file.
func (w *Worker) syncMemoryFiles() error {
	workspaceDir := w.appCfg.WorkspaceDir

	// Collect current files on disk.
	files := make(map[string]string) // relative path -> content

	memoryPath := filepath.Join(workspaceDir, "MEMORY.md")
	if data, err := os.ReadFile(memoryPath); err == nil {
		files["MEMORY.md"] = string(data)
	}

	memDir := filepath.Join(workspaceDir, "memory")
	_ = filepath.Walk(memDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil {
			files[rel] = string(data)
		}
		return nil
	})

	// Load existing records from DB.
	var existing []struct {
		Path    string
		Content string
	}
	if err := w.db.Raw("SELECT path, content FROM memory_files").Scan(&existing).Error; err != nil {
		return err
	}
	existingMap := make(map[string]string, len(existing))
	for _, e := range existing {
		existingMap[e.Path] = e.Content
	}

	// Sync within a transaction.
	return w.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		// Upsert changed or new files.
		for path, content := range files {
			if old, ok := existingMap[path]; ok && old == content {
				continue
			}
			if err := tx.Exec(`
				INSERT INTO memory_files (path, content, updated_at) VALUES (?, ?, ?)
				ON CONFLICT(path) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at
			`, path, content, now).Error; err != nil {
				return err
			}
		}

		// Delete removed files.
		for path := range existingMap {
			if _, ok := files[path]; !ok {
				if err := tx.Exec("DELETE FROM memory_files WHERE path = ?", path).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// searchMemoriesDB queries the cached memory_files table using per-keyword
// LIKE conditions and scores matches by the number of distinct keywords hit,
// preserving the same ranking semantics as the fallback path.
func (w *Worker) searchMemoriesDB(query string) []string {
	keywords := extractMemoryKeywords(query)
	if len(keywords) == 0 {
		return nil
	}

	conditions := make([]string, 0, len(keywords))
	args := make([]any, 0, len(keywords))
	for _, kw := range keywords {
		conditions = append(conditions, "lower(content) LIKE ?")
		args = append(args, "%"+kw+"%")
	}

	sql := fmt.Sprintf(
		"SELECT path, content FROM memory_files WHERE %s LIMIT 50",
		strings.Join(conditions, " OR "),
	)

	var rows []struct {
		Path    string
		Content string
	}
	if err := w.db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil
	}

	type scored struct {
		score int
		path  string
		text  string
	}
	var scoredList []scored

	for _, r := range rows {
		lower := strings.ToLower(r.Content)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}
		if score == 0 {
			continue
		}

		text := r.Content
		if len(text) > defaultMemoryMaxChars {
			text = text[:defaultMemoryMaxChars] + "..."
		}

		source := filepath.Base(r.Path)
		if source == "MEMORY.md" {
			source = ""
		} else {
			source = strings.TrimSuffix(source, filepath.Ext(source))
		}
		if source != "" {
			text = fmt.Sprintf("[%s] %s", source, text)
		}
		scoredList = append(scoredList, scored{score: score, path: r.Path, text: text})
	}

	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	seen := make(map[string]bool)
	var result []string
	for _, s := range scoredList {
		if seen[s.path] {
			continue
		}
		seen[s.path] = true
		result = append(result, s.text)
		if len(result) >= defaultMemoryTopN {
			break
		}
	}
	return result
}

// retrieveMemoriesFallback provides the original keyword-based matching when
// the database is unavailable (e.g. in isolated tests).
func (w *Worker) retrieveMemoriesFallback(query string) []string {
	var entries []memoryEntry

	// 1. Strip-style MEMORY.md
	entries = append(entries, loadMemoryFileEntries(
		filepath.Join(w.appCfg.WorkspaceDir, "MEMORY.md"), "")...)

	// 2. Long-form memory/*.md files (recursively)
	memDir := filepath.Join(w.appCfg.WorkspaceDir, "memory")
	_ = filepath.Walk(memDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		entries = append(entries, loadMemoryFileEntries(path, name)...)
		return nil
	})

	return rankMemoryEntries(entries, query, defaultMemoryTopN, defaultMemoryMaxChars)
}

func loadMemoryFileEntries(path, source string) []memoryEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return nil
	}

	var parts []string
	if filepath.Base(path) == "MEMORY.md" {
		parts = strings.Split(raw, memoryEntryDelimiter)
	} else {
		// Split by paragraphs for long-form memory files.
		parts = strings.Split(raw, "\n\n")
	}

	var entries []memoryEntry
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		entries = append(entries, memoryEntry{Source: source, Content: p})
	}
	return entries
}

func rankMemoryEntries(entries []memoryEntry, query string, topN, maxChars int) []string {
	keywords := extractMemoryKeywords(query)
	if len(keywords) == 0 {
		return nil
	}

	type scored struct {
		score int
		text  string
	}
	var scoredList []scored

	for _, e := range entries {
		lower := strings.ToLower(e.Content)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		text := e.Content
		if len(text) > maxChars {
			text = text[:maxChars] + "..."
		}
		if e.Source != "" {
			text = fmt.Sprintf("[%s] %s", e.Source, text)
		}
		scoredList = append(scoredList, scored{score: score, text: text})
	}

	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	var result []string
	for i := 0; i < len(scoredList) && i < topN; i++ {
		result = append(result, scoredList[i].text)
	}
	return result
}

func extractMemoryKeywords(query string) []string {
	replacer := strings.NewReplacer(
		"，", " ", "。", " ", "！", " ", "？", " ", "；", " ", "：", " ",
		"\"", " ", "'", " ", "（", " ", "）", " ", "【", " ", "】", " ",
		",", " ", ".", " ", "!", " ", "?", " ", ";", " ", ":", " ",
		"-", " ", "_", " ", "/", " ", "\\", " ",
	)
	cleaned := strings.ToLower(replacer.Replace(query))
	parts := strings.Fields(cleaned)
	seen := make(map[string]bool)
	var kws []string
	for _, p := range parts {
		if len(p) <= 1 {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		kws = append(kws, p)
	}
	return kws
}
