package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEffectiveTemplateLanguage(t *testing.T) {
	tests := []struct {
		language     string
		templateName string
		want         string
	}{
		{"zh", "default", "zh"},
		{"en", "default", "en"},
		{"", "default", "zh"},
		{"zh", "product-assistant", "zh"},
		{"en", "product-assistant", "zh"},
		{"zh", "code-review", "zh"},
		{"en", "code-review", "zh"},
		{"en", "custom-template", "en"},
	}

	for _, tt := range tests {
		got := effectiveTemplateLanguage(tt.language, tt.templateName)
		if got != tt.want {
			t.Errorf("effectiveTemplateLanguage(%q, %q) = %q, want %q",
				tt.language, tt.templateName, got, tt.want)
		}
	}
}

func TestWriteEmbeddedTemplate_FallsBackToZhForDemoTemplatesWhenLanguageIsEn(t *testing.T) {
	for _, templateName := range []string{"product-assistant", "code-review"} {
		t.Run(templateName, func(t *testing.T) {
			dir := t.TempDir()
			workspaceDir := filepath.Join(dir, "workspace")

			if err := writeEmbeddedTemplate(workspaceDir, "en", templateName); err != nil {
				t.Fatalf("writeEmbeddedTemplate() error = %v", err)
			}

			// Verify IDENTITY.md was written and contains Chinese keywords.
			data, err := os.ReadFile(filepath.Join(workspaceDir, "IDENTITY.md"))
			if err != nil {
				t.Fatalf("read IDENTITY.md: %v", err)
			}

			content := string(data)
			if !strings.Contains(content, "搭档") {
				t.Errorf("IDENTITY.md does not contain expected Chinese content for %s template", templateName)
			}
		})
	}
}
