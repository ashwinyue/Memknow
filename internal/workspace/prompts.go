package workspace

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:template all:template_variants
var templateFS embed.FS

func effectiveTemplateLanguage(language, templateName string) string {
	lang := language
	if lang == "" {
		lang = "zh"
	}

	switch templateName {
	case "product-assistant", "code-review":
		return "zh"
	default:
		return lang
	}
}

// writeEmbeddedTemplate writes embedded default template files to the workspace
// if they do not already exist. External template directories can override these
// by being copied afterwards (which also skips existing files).
func writeEmbeddedTemplate(workspaceDir, language, templateName string) error {
	lang := effectiveTemplateLanguage(language, templateName)
	if templateName == "" {
		templateName = "default"
	}

	if templateName != "default" {
		variantRoot := filepath.Join("template_variants", templateName, lang)
		if err := writeEmbeddedTree(workspaceDir, variantRoot); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
	}

	return writeEmbeddedTree(workspaceDir, filepath.Join("template", lang))
}

func writeEmbeddedTree(workspaceDir, root string) error {
	return fs.WalkDir(templateFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root && errors.Is(err, fs.ErrNotExist) {
				return os.ErrNotExist
			}
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(workspaceDir, rel)

		if _, err := os.Stat(dstPath); err == nil {
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", rel, err)
		}

		data, err := templateFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", rel, err)
		}

		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return fmt.Errorf("write embedded %s: %w", rel, err)
		}
		return nil
	})
}
