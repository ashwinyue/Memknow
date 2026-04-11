package workspace

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/model"
)

// Init ensures the workspace directory exists and has the required subdirectories.
// If a template directory is provided, it copies templates on first init.
// feishuAppID and feishuAppSecret are kept in the signature for compatibility with
// existing call sites, but workspace-local feishu credentials are no longer written.
func Init(workspaceDir string, templateDir string, _, _ string, webSearch config.WebSearchConfig, language string, templateName string) error {
	// Create required subdirectories.
	dirs := []string{
		workspaceDir,
		filepath.Join(workspaceDir, "bin"),
		filepath.Join(workspaceDir, "skills"),
		filepath.Join(workspaceDir, "memory"),
		filepath.Join(workspaceDir, "sessions"),
		SessionTypeDir(workspaceDir, model.SessionTypeChat),
		SessionTypeDir(workspaceDir, model.SessionTypeHeartbeat),
		SessionTypeDir(workspaceDir, model.SessionTypeSchedule),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	// Create .memory.lock if it doesn't exist.
	memoryLockPath := filepath.Join(workspaceDir, ".memory.lock")
	if _, err := os.Stat(memoryLockPath); os.IsNotExist(err) {
		if err := os.WriteFile(memoryLockPath, nil, 0o644); err != nil {
			return fmt.Errorf("create memory lock: %w", err)
		}
	}

	// Create .skill.lock if it doesn't exist.
	skillLockPath := filepath.Join(workspaceDir, ".skill.lock")
	if _, err := os.Stat(skillLockPath); os.IsNotExist(err) {
		if err := os.WriteFile(skillLockPath, nil, 0o644); err != nil {
			return fmt.Errorf("create skill lock: %w", err)
		}
	}

	// Copy template files if template dir is set and workspace is empty.
	if templateDir != "" {
		lang := language
		if lang == "" {
			lang = "zh"
		}
		templateLangDir := filepath.Join(templateDir, lang)
		if _, err := os.Stat(templateLangDir); os.IsNotExist(err) {
			return fmt.Errorf("template language dir not found: %s", templateLangDir)
		}
		if err := copyTemplate(templateLangDir, workspaceDir); err != nil {
			return fmt.Errorf("copy template: %w", err)
		}
	}

	// Write embedded default template files if missing.
	// This ensures core files (SOUL.md, IDENTITY.md, USER.md, MEMORY.md, HEARTBEAT.md,
	// and all default skills) exist even when no external template dir is configured.
	if err := writeEmbeddedTemplate(workspaceDir, language, templateName); err != nil {
		return fmt.Errorf("write embedded template: %w", err)
	}
	if err := writeSearchConfig(workspaceDir, webSearch); err != nil {
		return fmt.Errorf("write search config: %w", err)
	}
	if err := writeSearchEntrypoint(workspaceDir); err != nil {
		return fmt.Errorf("write web-search entrypoint: %w", err)
	}

	return nil
}

// copyTemplate copies files from src to dst, skipping files that already exist.
func copyTemplate(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		// M-5: skip symlinks to prevent path traversal via crafted template dirs.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Skip if destination already exists.
		if _, err := os.Stat(dstPath); err == nil {
			return nil
		}

		return copyFile(path, dstPath)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func writeSearchConfig(workspaceDir string, cfg config.WebSearchConfig) error {
	type searchRuntimeConfig struct {
		Providers      []string `json:"providers"`
		TimeoutSeconds int      `json:"timeout_seconds"`
		Tavily         struct {
			APIKey  string `json:"api_key"`
			BaseURL string `json:"base_url"`
		} `json:"tavily"`
	}

	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}
	baseURL := cfg.TavilyBaseURL
	if baseURL == "" {
		baseURL = "https://api.tavily.com/search"
	}

	runtimeCfg := searchRuntimeConfig{
		Providers:      []string{"tavily", "duckduckgo"},
		TimeoutSeconds: timeout,
	}
	runtimeCfg.Tavily.APIKey = cfg.TavilyAPIKey
	runtimeCfg.Tavily.BaseURL = baseURL

	data, err := json.MarshalIndent(runtimeCfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(workspaceDir, ".search.json"), data, 0o600)
}

func writeSearchEntrypoint(workspaceDir string) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	quotedExec := shellQuote(execPath)
	script := fmt.Sprintf(`#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
WORKSPACE_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

exec %s web-search --config "$WORKSPACE_DIR/.search.json" "$@"
`, quotedExec)

	path := filepath.Join(workspaceDir, "bin", "web-search")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return err
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
