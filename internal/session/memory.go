package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
func (w *Worker) retrieveMemories(query string) []string {
	var entries []memoryEntry

	// 1. Strip-style MEMORY.md
	entries = append(entries, loadMemoryFileEntries(
		filepath.Join(w.appCfg.WorkspaceDir, "MEMORY.md"), "")...)

	// 2. Long-form memory/*.md files
	memDir := filepath.Join(w.appCfg.WorkspaceDir, "memory")
	if files, err := os.ReadDir(memDir); err == nil {
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".md") {
				continue
			}
			name := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
			path := filepath.Join(memDir, f.Name())
			entries = append(entries, loadMemoryFileEntries(path, name)...)
		}
	}

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
