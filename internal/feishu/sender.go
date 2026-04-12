package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	feishuCardMaxRunes = 8000
	feishuThinkingText = "Thinking..."
	feishuBusyReaction = "OK"
	feishuDoneReaction = "DONE"
)

var feishuCardHeadingPrefix = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
var postContentInvalidRe = regexp.MustCompile(`(?i)content format of the post type is incorrect`)
var markdownHintRe = regexp.MustCompile(`(?m)(^#{1,6}\s)|(^\s*[-*]\s)|(^\s*\d+\.\s)|(` + "```" + `)|(` + "`[^`\n]+`" + `)|(\*\*[^*\n].+?\*\*)|(\[[^\]]+\]\([^)]+\))|(^>\s)|(^\s*\|.+\|\s*$)`)
var zeroWidthRuneRe = regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{FEFF}]`)
var extraBlankLinesRe = regexp.MustCompile(`\n{3,}`)

// Sender sends messages and updates interactive cards via Feishu API.
type Sender struct {
	client *lark.Client
}

// NewSender creates a Sender with the given Feishu API client.
func NewSender(client *lark.Client) *Sender {
	return &Sender{client: client}
}

// buildCard builds an interactive card JSON string with normalized lark_md content.
// Returns an error so callers can decide how to handle serialization failures.
func buildCard(text string) (string, error) {
	body := normalizeFeishuCardText(text)
	card := map[string]interface{}{
		"schema": "2.0",
		"config": map[string]interface{}{
			"wide_screen_mode": true,
			"enable_forward":   true,
			"update_multi":     true,
		},
		"body": map[string]interface{}{
			"elements": []interface{}{
				map[string]interface{}{
					"tag":     "markdown",
					"content": body,
				},
			},
		},
	}
	b, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("marshal card: %w", err)
	}
	return string(b), nil
}

// SendCard sends an interactive card message and returns the card message ID.
func (s *Sender) SendCard(ctx context.Context, receiveID, receiveIDType, text string) (string, error) {
	cardJSON, err := buildCard(text)
	if err != nil {
		return "", err
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeInteractive).
			ReceiveId(receiveID).
			Content(cardJSON).
			Build()).
		Build()

	resp, err := s.client.Im.Message.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("send card: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("send card API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
}

// SendThinking sends an initial "thinking..." interactive card and returns the card message ID.
func (s *Sender) SendThinking(ctx context.Context, receiveID string, receiveIDType string) (string, error) {
	return s.SendCard(ctx, receiveID, receiveIDType, feishuThinkingText)
}

// UpdateCard patches an existing interactive card with new text.
func (s *Sender) UpdateCard(ctx context.Context, messageID string, text string) error {
	cardJSON, err := buildCard(text)
	if err != nil {
		return err
	}

	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardJSON).
			Build()).
		Build()

	resp, err := s.client.Im.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("patch card: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("patch card API error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// SendText sends a plain text message to a chat.
func (s *Sender) SendText(ctx context.Context, receiveID string, receiveIDType string, text string) (string, error) {
	msgType, content, err := buildReplyContent(text)
	if err != nil {
		return "", err
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(msgType).
			ReceiveId(receiveID).
			Content(content).
			Build()).
		Build()

	resp, err := s.client.Im.Message.Create(ctx, req)
	if err != nil {
		if msgType == larkim.MsgTypeInteractive {
			return s.sendPlainText(ctx, receiveID, receiveIDType, stripMarkdownToPlainText(text))
		}
		if msgType == larkim.MsgTypePost && postContentInvalidRe.MatchString(err.Error()) {
			return s.sendPlainText(ctx, receiveID, receiveIDType, stripMarkdownToPlainText(text))
		}
		return "", fmt.Errorf("send %s: %w", msgType, err)
	}
	if !resp.Success() {
		if msgType == larkim.MsgTypeInteractive {
			return s.sendPlainText(ctx, receiveID, receiveIDType, stripMarkdownToPlainText(text))
		}
		if msgType == larkim.MsgTypePost && postContentInvalidRe.MatchString(resp.Msg) {
			return s.sendPlainText(ctx, receiveID, receiveIDType, stripMarkdownToPlainText(text))
		}
		return "", fmt.Errorf("send %s API error: code=%d msg=%s", msgType, resp.Code, resp.Msg)
	}

	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
}

func (s *Sender) sendPlainText(ctx context.Context, receiveID, receiveIDType, text string) (string, error) {
	text = sanitizeOutboundText(text)
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", fmt.Errorf("marshal text content: %w", err)
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeText).
			ReceiveId(receiveID).
			Content(string(content)).
			Build()).
		Build()
	resp, err := s.client.Im.Message.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("send text: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("send text API error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
}

func buildReplyContent(content string) (msgType, body string, err error) {
	content = sanitizeOutboundText(content)
	if !containsMarkdown(content) {
		b, e := json.Marshal(map[string]string{"text": content})
		if e != nil {
			return "", "", fmt.Errorf("marshal text content: %w", e)
		}
		return larkim.MsgTypeText, string(b), nil
	}
	// 1) Code blocks / tables -> interactive card
	if hasComplexMarkdown(content) {
		card, e := buildCard(content)
		if e != nil {
			return "", "", e
		}
		return larkim.MsgTypeInteractive, card, nil
	}
	// 2) Rich markdown -> post md
	post, e := buildPostMdJSON(content)
	if e != nil {
		return "", "", e
	}
	return larkim.MsgTypePost, post, nil
}

func containsMarkdown(s string) bool {
	return markdownHintRe.MatchString(s)
}

func hasComplexMarkdown(s string) bool {
	if strings.Contains(s, "```") {
		return true
	}
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 1 && strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			return true
		}
	}
	return false
}

func buildPostMdJSON(content string) (string, error) {
	langBlock := map[string]any{
		"title": "",
		"content": [][]map[string]string{
			{
				{
					"tag":  "md",
					"text": content,
				},
			},
		},
	}
	post := map[string]any{
		"zh_cn": langBlock,
		"en_us": langBlock,
	}
	b, err := json.Marshal(post)
	if err != nil {
		return "", fmt.Errorf("marshal post content: %w", err)
	}
	return string(b), nil
}

func stripMarkdownToPlainText(s string) string {
	s = strings.ReplaceAll(s, "```", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

// AddProcessingReaction adds a transient busy reaction to the source message.
func (s *Sender) AddProcessingReaction(ctx context.Context, messageID string) (string, error) {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(strings.TrimSpace(messageID)).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(feishuBusyReaction).Build()).
			Build()).
		Build()

	resp, err := s.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("add reaction: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("add reaction API error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data != nil && resp.Data.ReactionId != nil {
		return strings.TrimSpace(*resp.Data.ReactionId), nil
	}
	return "", nil
}

// AddDoneReaction adds a DONE emoji reaction to signal processing completion.
func (s *Sender) AddDoneReaction(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}

	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(feishuDoneReaction).Build()).
			Build()).
		Build()

	resp, err := s.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("add done reaction: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("add done reaction API error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func normalizeFeishuCardText(text string) string {
	content := extractReadableFromJSON(text)
	content = sanitizeOutboundText(content)
	content = processFeishuCardMarkdown(content)
	content = strings.TrimSpace(content)
	if content == "" {
		return feishuThinkingText
	}
	if utf8.RuneCountInString(content) <= feishuCardMaxRunes {
		return content
	}
	rs := []rune(content)
	return "...\n" + string(rs[len(rs)-feishuCardMaxRunes:])
}

func sanitizeOutboundText(text string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = zeroWidthRuneRe.ReplaceAllString(text, "")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	text = strings.Join(lines, "\n")
	text = extraBlankLinesRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// extractReadableFromJSON tries to extract human-readable text from JSON-like content.
// Returns original text if not JSON or no useful field is found.
func extractReadableFromJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text
	}
	first := strings.TrimLeft(trimmed, " \t\n\r")
	if len(first) < 2 || (first[0] != '{' && first[0] != '[') {
		return text
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		for _, key := range []string{"text", "message", "content", "result", "output", "response", "answer"} {
			if v, ok := obj[key]; ok && v != nil {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
		return text
	}

	var arr []any
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
		if len(arr) > 0 {
			if s, ok := arr[0].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return text
}

// processFeishuCardMarkdown normalizes markdown for Feishu card lark_md.
func processFeishuCardMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = feishuCardHeadingPrefix.ReplaceAllStringFunc(s, func(m string) string {
		parts := feishuCardHeadingPrefix.FindStringSubmatch(m)
		if len(parts) == 2 {
			return "**" + strings.TrimSpace(parts[1]) + "**"
		}
		return m
	})
	return strings.TrimSpace(s)
}
