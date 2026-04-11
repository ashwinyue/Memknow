package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProcessFeishuCardMarkdown_HeadingToBold(t *testing.T) {
	in := "# 标题\\n正文"
	got := processFeishuCardMarkdown(in)
	if got != "**标题**\n正文" {
		t.Fatalf("processFeishuCardMarkdown() = %q, want %q", got, "**标题**\n正文")
	}
}

func TestExtractReadableFromJSON_Object(t *testing.T) {
	in := `{"result":"处理完成","other":"x"}`
	got := extractReadableFromJSON(in)
	if got != "处理完成" {
		t.Fatalf("extractReadableFromJSON() = %q, want %q", got, "处理完成")
	}
}

func TestExtractReadableFromJSON_Array(t *testing.T) {
	in := `["第一段", {"k":"v"}]`
	got := extractReadableFromJSON(in)
	if got != "第一段" {
		t.Fatalf("extractReadableFromJSON() = %q, want %q", got, "第一段")
	}
}

func TestNormalizeFeishuCardText_TrimAndFallback(t *testing.T) {
	if got := normalizeFeishuCardText("   "); got != feishuThinkingText {
		t.Fatalf("normalizeFeishuCardText(blank) = %q, want %q", got, feishuThinkingText)
	}
}

func TestBuildCard_ConfigAndBody(t *testing.T) {
	cardJSON, err := buildCard("# 小节\\n内容")
	if err != nil {
		t.Fatalf("buildCard() err = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(cardJSON), &payload); err != nil {
		t.Fatalf("unmarshal card json: %v", err)
	}

	cfg, ok := payload["config"].(map[string]any)
	if !ok {
		t.Fatalf("config missing")
	}
	if cfg["update_multi"] != true || cfg["enable_forward"] != true {
		t.Fatalf("config flags not set, got %#v", cfg)
	}

	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("body missing")
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("body elements missing")
	}
	first, _ := elements[0].(map[string]any)
	content, _ := first["content"].(string)
	if !strings.Contains(content, "**小节**") || !strings.Contains(content, "内容") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestBuildReplyContent_Text(t *testing.T) {
	msgType, body, err := buildReplyContent("你好")
	if err != nil {
		t.Fatalf("buildReplyContent err = %v", err)
	}
	if msgType != "text" {
		t.Fatalf("msgType = %q, want text", msgType)
	}
	if !strings.Contains(body, "你好") {
		t.Fatalf("body missing text: %q", body)
	}
}

func TestBuildReplyContent_CodeBlockToCard(t *testing.T) {
	msgType, body, err := buildReplyContent("```go\npackage main\n```")
	if err != nil {
		t.Fatalf("buildReplyContent err = %v", err)
	}
	if msgType != "interactive" {
		t.Fatalf("msgType = %q, want interactive", msgType)
	}
	if !strings.Contains(body, "\"schema\":\"2.0\"") {
		t.Fatalf("card json missing schema 2.0: %q", body)
	}
}

func TestBuildReplyContent_MarkdownToPost(t *testing.T) {
	msgType, body, err := buildReplyContent("## 标题\n普通段落")
	if err != nil {
		t.Fatalf("buildReplyContent err = %v", err)
	}
	if msgType != "post" {
		t.Fatalf("msgType = %q, want post", msgType)
	}
	if !strings.Contains(body, "\"tag\":\"md\"") {
		t.Fatalf("post body missing md tag: %q", body)
	}
	if !strings.Contains(body, "\"zh_cn\"") || !strings.Contains(body, "\"en_us\"") {
		t.Fatalf("post body should include zh_cn and en_us locales, got: %q", body)
	}
}

func TestContainsMarkdown_DoesNotTreatBracketAsMarkdown(t *testing.T) {
	if containsMarkdown("路径是 [tmp] 目录") {
		t.Fatalf("containsMarkdown should be false for plain bracket text")
	}
	if !containsMarkdown("这是链接 [文档](https://example.com)") {
		t.Fatalf("containsMarkdown should detect markdown link")
	}
}
