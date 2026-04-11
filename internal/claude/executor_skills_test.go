package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSystemPrompt_InjectsSkills(t *testing.T) {
	tmp := t.TempDir()
	workspaceDir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write SOUL.md
	if err := os.WriteFile(filepath.Join(workspaceDir, "SOUL.md"), []byte("# SOUL\nBe helpful."), 0o644); err != nil {
		t.Fatalf("write SOUL.md error: %v", err)
	}

	// Write IDENTITY.md
	if err := os.WriteFile(filepath.Join(workspaceDir, "IDENTITY.md"), []byte("# IDENTITY\nName: Alex."), 0o644); err != nil {
		t.Fatalf("write IDENTITY.md error: %v", err)
	}

	// Write a flat skill file
	skillsDir := filepath.Join(workspaceDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillsDir, "deploy.md")
	if err := os.WriteFile(skillPath, []byte("# Deploy\nStep 1."), 0o644); err != nil {
		t.Fatalf("write skill error: %v", err)
	}

	// Call the function under test
	content := buildSystemPrompt(workspaceDir)

	// Assertions
	if !strings.Contains(content, "Be helpful") {
		t.Error("system prompt missing SOUL.md content")
	}
	if !strings.Contains(content, "Name: Alex") {
		t.Error("system prompt missing IDENTITY.md content")
	}
	if !strings.Contains(content, "deploy") {
		t.Error("system prompt missing skill name in index")
	}
	if strings.Contains(content, "# Deploy") {
		t.Error("system prompt should NOT contain full skill body when using index-only injection")
	}
	if !strings.Contains(content, "可用技能索引") {
		t.Error("system prompt missing skill index header")
	}
}
