package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwinyue/Memknow/internal/config"
)

func TestInit_CreatesRequiredDirs(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	for _, sub := range []string{"skills", "memory", "sessions", "sessions/chat", "sessions/heartbeat", "sessions/schedule"} {
		path := filepath.Join(workspaceDir, sub)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Errorf("expected dir %s to exist", path)
		}
	}

	if _, err := os.Stat(filepath.Join(workspaceDir, "tasks")); !os.IsNotExist(err) {
		t.Errorf("expected legacy tasks dir to be absent, stat err = %v", err)
	}
}

func TestInit_CreatesMemoryLock(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	lockPath := filepath.Join(workspaceDir, ".memory.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("expected .memory.lock to exist: %v", err)
	}
}

func TestInit_CreatesSkillLock(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	lockPath := filepath.Join(workspaceDir, ".skill.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("expected .skill.lock to exist: %v", err)
	}
}

func TestInit_DoesNotOverwriteExistingLock(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(workspaceDir, ".memory.lock")
	if err := os.WriteFile(lockPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second Init should not erase the existing lock content.
	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("second Init() error = %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing" {
		t.Errorf("lock content = %q, want %q", string(data), "existing")
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	for range 3 {
		if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
			t.Fatalf("Init() error = %v", err)
		}
	}
}

func TestInit_CopiesTemplate(t *testing.T) {
	templateRoot := t.TempDir()
	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	// Init now looks for a language subdirectory inside templateDir.
	templateDir := filepath.Join(templateRoot, "zh")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create template files.
	if err := os.WriteFile(filepath.Join(templateDir, "SOUL.md"), []byte("template content"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(templateDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "memory.md"), []byte("skill content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Init(workspaceDir, templateRoot, "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	for _, rel := range []string{"SOUL.md", "skills/memory.md"} {
		data, err := os.ReadFile(filepath.Join(workspaceDir, rel))
		if err != nil {
			t.Errorf("expected %s to be copied: %v", rel, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("copied file %s is empty", rel)
		}
	}
}

func TestInit_TemplateDoesNotOverwriteExisting(t *testing.T) {
	templateRoot := t.TempDir()
	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write an existing file in workspace before Init.
	existing := filepath.Join(workspaceDir, "SOUL.md")
	if err := os.WriteFile(existing, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Template has same file with different content.
	templateDir := filepath.Join(templateRoot, "zh")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "SOUL.md"), []byte("template content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Init(workspaceDir, templateRoot, "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user content" {
		t.Errorf("existing file was overwritten: got %q", string(data))
	}
}

func TestInit_DoesNotWriteFeishuConfig(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	configPath := filepath.Join(workspaceDir, "skills", "feishu", "SKILL.md")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected feishu skill to be absent when template is empty, stat err = %v", err)
	}
}

func TestInit_WikiScaffoldFilesExist(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	for _, rel := range []string{
		"skills/wiki.md",
		"memory/index.md",
		"memory/log.md",
	} {
		p := filepath.Join(workspaceDir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestCopyTemplate_SkipsSymlinks(t *testing.T) {
	templateRoot := t.TempDir()
	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	// Init now looks for a language subdirectory.
	templateDir := filepath.Join(templateRoot, "zh")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a real file and a symlink in the template dir.
	real := filepath.Join(templateDir, "real.md")
	if err := os.WriteFile(real, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(templateDir, "link.md")); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	if err := Init(workspaceDir, templateRoot, "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// real.md should be copied; link.md should NOT be created.
	if _, err := os.Stat(filepath.Join(workspaceDir, "real.md")); err != nil {
		t.Error("real.md should have been copied")
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "link.md")); err == nil {
		t.Error("link.md (symlink) should NOT have been copied")
	}
}

func TestInit_ProductAssistantTemplateOverridesDefaultFiles(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "product-assistant"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspaceDir, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("read IDENTITY.md: %v", err)
	}
	if !containsAll(string(data), []string{"产品经理搭档", "需求", "决策", "风险"}) {
		t.Fatalf("IDENTITY.md does not look like product-assistant template:\n%s", string(data))
	}

	memoryData, err := os.ReadFile(filepath.Join(workspaceDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !containsAll(string(memoryData), []string{"增长看板", "国际化", "埋点"}) {
		t.Fatalf("MEMORY.md does not contain seeded product-assistant memory:\n%s", string(memoryData))
	}
}

func TestInit_WritesSearchConfigAndSkill(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	searchCfg := config.WebSearchConfig{
		TavilyAPIKey:   "tvly-secret",
		TavilyBaseURL:  "https://api.tavily.com/search",
		TimeoutSeconds: 12,
	}

	if err := Init(workspaceDir, "", "", "", searchCfg, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	scriptPath := filepath.Join(workspaceDir, "bin", "web-search")
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("expected search script to exist: %v", err)
	}
	if !strings.Contains(string(scriptData), "web-search --config") {
		t.Fatalf("search script should call server web-search subcommand, got:\n%s", string(scriptData))
	}

	skillPath := filepath.Join(workspaceDir, "skills", "search.md")
	skillData, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read search skill: %v", err)
	}
	if !strings.Contains(string(skillData), "bin/web-search") {
		t.Fatalf("search skill should mention bin/web-search, got:\n%s", string(skillData))
	}

	rawCfg, err := os.ReadFile(filepath.Join(workspaceDir, ".search.json"))
	if err != nil {
		t.Fatalf("read .search.json: %v", err)
	}
	var got struct {
		Tavily struct {
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
		} `json:"tavily"`
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(rawCfg, &got); err != nil {
		t.Fatalf("unmarshal .search.json: %v", err)
	}
	if got.Tavily.APIKey != "tvly-secret" {
		t.Fatalf("tavily api key = %q, want tvly-secret", got.Tavily.APIKey)
	}
	if got.Tavily.BaseURL != "https://api.tavily.com/search" {
		t.Fatalf("tavily base_url = %q, want https://api.tavily.com/search", got.Tavily.BaseURL)
	}
	if got.TimeoutSeconds != 12 {
		t.Fatalf("timeout_seconds = %d, want 12", got.TimeoutSeconds)
	}
}

func TestInit_WritesDDGFallbackWhenTavilyMissing(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "default"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	rawCfg, err := os.ReadFile(filepath.Join(workspaceDir, ".search.json"))
	if err != nil {
		t.Fatalf("read .search.json: %v", err)
	}
	var got struct {
		Tavily struct {
			APIKey string `json:"api_key"`
		} `json:"tavily"`
		Providers []string `json:"providers"`
	}
	if err := json.Unmarshal(rawCfg, &got); err != nil {
		t.Fatalf("unmarshal .search.json: %v", err)
	}
	if got.Tavily.APIKey != "" {
		t.Fatalf("expected empty tavily api key, got %q", got.Tavily.APIKey)
	}
	if len(got.Providers) != 2 || got.Providers[0] != "tavily" || got.Providers[1] != "duckduckgo" {
		t.Fatalf("providers = %v, want [tavily duckduckgo]", got.Providers)
	}
}

func TestInit_CodeReviewTemplateOverridesDefaultFiles(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "workspace")

	if err := Init(workspaceDir, "", "", "", config.WebSearchConfig{}, "zh", "code-review"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspaceDir, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("read IDENTITY.md: %v", err)
	}
	if !containsAll(string(data), []string{"代码评审搭档", "bug", "回归", "测试"}) {
		t.Fatalf("IDENTITY.md does not look like code-review template:\n%s", string(data))
	}

	skillData, err := os.ReadFile(filepath.Join(workspaceDir, "skills", "review.md"))
	if err != nil {
		t.Fatalf("read skills/review.md: %v", err)
	}
	if !containsAll(string(skillData), []string{"git rev-parse --show-toplevel", "git diff", "评分", "功能正确性与健壮性"}) {
		t.Fatalf("skills/review.md does not contain review workflow:\n%s", string(skillData))
	}

	fixtureDir := filepath.Join(workspaceDir, "review-fixtures")
	if _, err := os.Stat(fixtureDir); !os.IsNotExist(err) {
		t.Fatalf("expected code-review template not to create fixture repo, stat err = %v", err)
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
