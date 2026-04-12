package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/db"
)

func TestRetrieveMemories_MEMORY_md(t *testing.T) {
	tmp := t.TempDir()
	w := &Worker{appCfg: &config.AppConfig{WorkspaceDir: tmp}}

	memoryPath := filepath.Join(tmp, "MEMORY.md")
	content := "我喜欢喝咖啡\n§\n\n我每天早上八点起床\n§\n\n我的猫叫咪咪"
	if err := os.WriteFile(memoryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	memories := w.retrieveMemories("咖啡")
	if len(memories) == 0 {
		t.Fatal("expected at least one memory match")
	}
	if memories[0] != "我喜欢喝咖啡" {
		t.Errorf("unexpected match: %s", memories[0])
	}
}

func TestRetrieveMemories_MemoryDir(t *testing.T) {
	tmp := t.TempDir()
	w := &Worker{appCfg: &config.AppConfig{WorkspaceDir: tmp}}

	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "健身.md"), []byte("每周跑步三次\n\n每次五公里"), 0o644); err != nil {
		t.Fatal(err)
	}

	memories := w.retrieveMemories("跑步")
	if len(memories) == 0 {
		t.Fatal("expected at least one memory match")
	}
	if memories[0] != "[健身] 每周跑步三次" {
		t.Errorf("unexpected match: %s", memories[0])
	}
}

func TestRetrieveMemories_Ranking(t *testing.T) {
	tmp := t.TempDir()
	w := &Worker{appCfg: &config.AppConfig{WorkspaceDir: tmp}}

	memoryPath := filepath.Join(tmp, "MEMORY.md")
	content := "apple banana\n§\napple cherry date\n§\napple banana cherry date egg"
	if err := os.WriteFile(memoryPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	memories := w.retrieveMemories("apple banana")
	if len(memories) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(memories))
	}
	// The top results should be the ones matching both keywords (score 2).
	if !contains(memories[0], "apple") || !contains(memories[0], "banana") {
		t.Errorf("expected top result to contain both keywords, got: %s", memories[0])
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || strings.Contains(s, substr))
}

func TestRetrieveMemories_NoMatch(t *testing.T) {
	tmp := t.TempDir()
	w := &Worker{appCfg: &config.AppConfig{WorkspaceDir: tmp}}

	memoryPath := filepath.Join(tmp, "MEMORY.md")
	if err := os.WriteFile(memoryPath, []byte(" unrelated content "), 0o644); err != nil {
		t.Fatal(err)
	}

	memories := w.retrieveMemories("xyz123")
	if len(memories) != 0 {
		t.Errorf("expected no matches, got %d", len(memories))
	}
}

func TestExtractMemoryKeywords(t *testing.T) {
	kws := extractMemoryKeywords("你好，世界！Hello, world!")
	expected := map[string]bool{"你好": true, "世界": true, "hello": true, "world": true}
	if len(kws) != len(expected) {
		t.Fatalf("expected %d keywords, got %d: %v", len(expected), len(kws), kws)
	}
	for _, kw := range kws {
		if !expected[kw] {
			t.Errorf("unexpected keyword: %s", kw)
		}
	}
}

// TestRetrieveMemories_FTS5Ranking verifies the FTS5 path including recursive
// subdirectories and BM25 ranking when a database is available.
func TestRetrieveMemories_FTS5Ranking(t *testing.T) {
	tmp := t.TempDir()
	database, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	w := &Worker{
		appCfg: &config.AppConfig{WorkspaceDir: tmp},
		db:     database,
	}

	// MEMORY.md with multiple strips.
	memoryPath := filepath.Join(tmp, "MEMORY.md")
	if err := os.WriteFile(memoryPath, []byte(
		"我喜欢喝拿铁咖啡\n§\n我每天早上八点起床\n§\n我的猫叫咪咪",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	// Subdirectory inside memory/.
	memDir := filepath.Join(tmp, "memory", "cases")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "project-x.md"), []byte(
		"Project X 使用了 Go 和 SQLite\n\n团队每周五进行代码评审",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	// Query that should hit both MEMORY.md and the nested file.
	memories := w.retrieveMemories("咖啡 Go 项目")
	if len(memories) == 0 {
		t.Fatal("expected at least one memory match via FTS5")
	}

	foundCoffee := false
	foundProjectX := false
	for _, m := range memories {
		if strings.Contains(m, "咖啡") {
			foundCoffee = true
		}
		if strings.Contains(m, "project-x") || strings.Contains(m, "Project X") {
			foundProjectX = true
		}
	}
	if !foundCoffee {
		t.Errorf("expected MEMORY.md coffee match in FTS5 results, got: %v", memories)
	}
	if !foundProjectX {
		t.Errorf("expected nested project-x.md match in FTS5 results, got: %v", memories)
	}

	// Verify sync removes deleted files.
	if err := os.Remove(filepath.Join(memDir, "project-x.md")); err != nil {
		t.Fatal(err)
	}
	memories = w.retrieveMemories("Project X")
	for _, m := range memories {
		if strings.Contains(m, "project-x") || strings.Contains(m, "Project X") {
			t.Fatalf("expected deleted file to be removed from FTS5, got: %v", memories)
		}
	}
}
