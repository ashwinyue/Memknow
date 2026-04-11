package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwinyue/Memknow/internal/workspace"
)

// TestEndToEnd_SkillLoading verifies that static skills in the workspace
// are correctly indexed and included in the assembled system prompt.
func TestEndToEnd_SkillLoading(t *testing.T) {
	tmp := t.TempDir()
	workspaceDir := filepath.Join(tmp, "workspace")

	// 1. Bootstrap workspace using embedded templates (no external template dir)
	if err := workspace.Init(workspaceDir, "", "", "", "zh", "default"); err != nil {
		t.Fatalf("init workspace: %v", err)
	}

	// 2. Manually add a static skill (simulating a pre-written workspace skill)
	skillPath := filepath.Join(workspaceDir, "skills", "db-migration.md")
	skillContent := "# 数据库迁移标准流程\n\n1. 备份数据\n2. 执行迁移脚本\n3. 验证数据一致性\n"
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	// 3. Build the workspace system prompt (this loads all skills as an index)
	prompt := buildSystemPrompt(workspaceDir)
	if prompt == "" {
		t.Fatal("buildSystemPrompt returned empty string")
	}

	// 4. Verify the prompt contains expected sections
	if !strings.Contains(prompt, "核心信念") {
		t.Error("system prompt missing SOUL.md content")
	}

	// Built-in skill from template should be present in the index
	if !strings.Contains(prompt, "memory") {
		t.Error("system prompt missing built-in memory skill")
	}

	// The manually created skill should be present in the index
	if !strings.Contains(prompt, "db-migration") {
		t.Error("system prompt missing the static skill 'db-migration'")
	}
	if !strings.Contains(prompt, "可用技能索引") {
		t.Error("system prompt missing skill index header")
	}

	// Full skill body should NOT be injected (index-only)
	if strings.Contains(prompt, "备份数据") {
		t.Error("system prompt should NOT contain full skill body when using index-only injection")
	}
}
