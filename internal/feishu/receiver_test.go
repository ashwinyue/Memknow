package feishu

import (
	"context"
	"strings"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/ashwinyue/Memknow/internal/config"
)

type recordingDispatcher struct {
	msgs []*IncomingMessage
}

func (d *recordingDispatcher) Dispatch(_ context.Context, msg *IncomingMessage) error {
	d.msgs = append(d.msgs, msg)
	return nil
}

func TestBuildChannelKey(t *testing.T) {
	tests := []struct {
		chatType string
		chatID   string
		threadID string
		appID    string
		want     string
	}{
		{"p2p", "chat_001", "", "app-a", "p2p:chat_001:app-a"},
		{"group", "oc_abc", "", "app-a", "group:oc_abc:app-a"},
		{"topic_group", "oc_abc", "tid_1", "app-a", "thread:oc_abc:tid_1:app-a"},
		{"topic", "oc_abc", "tid_2", "app-b", "thread:oc_abc:tid_2:app-b"},
		{"unknown", "oc_xyz", "", "app-c", "group:oc_xyz:app-c"},
	}
	for _, tt := range tests {
		got := BuildChannelKey(tt.chatType, tt.chatID, tt.threadID, tt.appID)
		if got != tt.want {
			t.Errorf("BuildChannelKey(%q,%q,%q,%q) = %q, want %q",
				tt.chatType, tt.chatID, tt.threadID, tt.appID, got, tt.want)
		}
	}
}

func TestReplyTarget(t *testing.T) {
	tests := []struct {
		chatType     string
		chatID       string
		senderOpenID string
		wantID       string
		wantType     string
	}{
		{"p2p", "chat_001", "ou_user1", "ou_user1", "open_id"},
		{"group", "oc_abc", "ou_user1", "oc_abc", "chat_id"},
		{"topic_group", "oc_abc", "ou_user1", "oc_abc", "chat_id"},
	}
	for _, tt := range tests {
		gotID, gotType := replyTarget(tt.chatType, tt.chatID, tt.senderOpenID)
		if gotID != tt.wantID || gotType != tt.wantType {
			t.Errorf("replyTarget(%q,%q,%q) = (%q,%q), want (%q,%q)",
				tt.chatType, tt.chatID, tt.senderOpenID,
				gotID, gotType, tt.wantID, tt.wantType)
		}
	}
}

func TestExtractPostText(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "extracts text from post",
			content: `{
				"title": "标题",
				"content": [[{"tag":"text","text":"第一行"},{"tag":"a","text":"链接"}],[{"tag":"text","text":"第二行"}]]
			}`,
			want: "标题\n第一行\n第二行",
		},
		{
			name:    "invalid json returns original",
			content: "not-json",
			want:    "not-json",
		},
		{
			name:    "no title",
			content: `{"content":[[{"tag":"text","text":"hello"}]]}`,
			want:    "hello",
		},
		{
			name:    "non-text tags are ignored",
			content: `{"content":[[{"tag":"image","image_key":"k1"},{"tag":"text","text":"ok"}]]}`,
			want:    "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPostText(tt.content)
			if got != tt.want {
				t.Errorf("extractPostText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSafeStr(t *testing.T) {
	s := "hello"
	if safeStr(&s) != "hello" {
		t.Error("safeStr with value")
	}
	if safeStr(nil) != "" {
		t.Error("safeStr with nil should return empty string")
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{"file/with/slashes.txt", "slashes.txt"},
		// On Linux, backslash is not a path separator; it is replaced with _.
		{"file\\with\\backslash.txt", "file_with_backslash.txt"},
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestWelcomeMessageContent verifies welcome messages contain expected identifiers.
// The handlers build messages with fmt.Sprintf using appID and group name;
// this table validates the resulting content shape.
func TestWelcomeMessageContent(t *testing.T) {
	appID := "my-assistant"

	t.Run("bot added to group includes appID and group name", func(t *testing.T) {
		groupName := "产品团队"
		msg := botAddedWelcome(appID, groupName)
		if !strings.Contains(msg, appID) {
			t.Errorf("message missing appID %q: %s", appID, msg)
		}
		if !strings.Contains(msg, groupName) {
			t.Errorf("message missing group name %q: %s", groupName, msg)
		}
		if !strings.Contains(msg, "/new") {
			t.Error("message should mention /new command")
		}
		if !strings.Contains(msg, "/mode") || !strings.Contains(msg, "/comp") {
			t.Error("message should mention mode commands")
		}
	})

	t.Run("bot added falls back to 本群 when name empty", func(t *testing.T) {
		msg := botAddedWelcome(appID, "")
		if !strings.Contains(msg, "本群") {
			t.Errorf("should use 本群 as fallback, got: %s", msg)
		}
	})

	t.Run("user added message includes user names", func(t *testing.T) {
		names := []string{"张三", "李四"}
		msg := userAddedWelcome(appID, names)
		for _, name := range names {
			if !strings.Contains(msg, name) {
				t.Errorf("message missing user name %q: %s", name, msg)
			}
		}
		if !strings.Contains(msg, appID) {
			t.Errorf("message missing appID %q: %s", appID, msg)
		}
	})

	t.Run("user added falls back to 新成员 when no names", func(t *testing.T) {
		msg := userAddedWelcome(appID, nil)
		if !strings.Contains(msg, "新成员") {
			t.Errorf("should use 新成员 as fallback, got: %s", msg)
		}
	})
}

func TestHandleMessageRead_NoOp(t *testing.T) {
	r := &Receiver{}
	if err := r.handleMessageRead(context.Background(), &larkim.P2MessageReadV1{}); err != nil {
		t.Fatalf("handleMessageRead() error = %v, want nil", err)
	}
}

func TestHandleMessage_GroupWithoutMention_DispatchedForProbe(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg: &config.AppConfig{ID: "app-a"},
		dispatcher: dispatcher,
		botOpenID: "ou_bot",
		botIDs: []string{"app-a", "cloud", "oii"},
	}

	event := newTextMessageEvent("group", "oc_group", "", "om_1", "ou_user", "hello", nil)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.msgs))
	}
	if dispatcher.msgs[0].MentionsMe {
		t.Fatal("MentionsMe = true, want false")
	}
	if dispatcher.msgs[0].RepliesToMe {
		t.Fatal("RepliesToMe = true, want false")
	}
}

func TestHandleMessage_GroupMentioningDifferentBot_Dropped(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg: &config.AppConfig{ID: "app-a"},
		dispatcher: dispatcher,
		botOpenID: "ou_bot",
		botIDs: []string{"app-a", "cloud", "oii"},
	}

	mentions := []*larkim.MentionEvent{
		larkim.NewMentionEventBuilder().
			Key("@_user_1").
			Id(larkim.NewUserIdBuilder().OpenId("ou_other_bot").Build()).
			Name("Other Bot").
			Build(),
	}
	event := newTextMessageEvent("group", "oc_group", "", "om_2", "ou_user", "@_user_1 hello", mentions)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 0 {
		t.Fatalf("dispatch count = %d, want 0", len(dispatcher.msgs))
	}
}

func TestHandleMessage_GroupNamingCurrentBot_Dispatched(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg:      &config.AppConfig{ID: "cloud"},
		dispatcher:  dispatcher,
		botOpenID:   "ou_bot",
		botIDs:      []string{"cloud", "oii"},
	}

	event := newTextMessageEvent("group", "oc_group", "", "om_name_me", "ou_user", "cloud 你怎么看", nil)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.msgs))
	}
	if !dispatcher.msgs[0].NamesMe {
		t.Fatal("NamesMe = false, want true")
	}
}

func TestHandleMessage_GroupNamingOtherBot_Dropped(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg:      &config.AppConfig{ID: "cloud"},
		dispatcher:  dispatcher,
		botOpenID:   "ou_bot",
		botIDs:      []string{"cloud", "oii"},
	}

	event := newTextMessageEvent("group", "oc_group", "", "om_name_other", "ou_user", "oii 你呢", nil)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 0 {
		t.Fatalf("dispatch count = %d, want 0", len(dispatcher.msgs))
	}
}

func TestHandleMessage_GroupNamingCurrentBot_WithPunctuation_Dispatched(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg:      &config.AppConfig{ID: "cloud"},
		dispatcher:  dispatcher,
		botOpenID:   "ou_bot",
		botIDs:      []string{"cloud", "oii"},
	}

	event := newTextMessageEvent("group", "oc_group", "", "om_name_punct", "ou_user", "cloud，你呢？", nil)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.msgs))
	}
	if !dispatcher.msgs[0].NamesMe {
		t.Fatal("NamesMe = false, want true")
	}
}

func TestHandleMessage_GroupMentioningCurrentBot_Dispatched(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg: &config.AppConfig{ID: "app-a"},
		dispatcher: dispatcher,
		botOpenID: "ou_bot",
		botIDs: []string{"app-a", "cloud", "oii"},
	}

	mentions := []*larkim.MentionEvent{
		larkim.NewMentionEventBuilder().
			Key("@_user_1").
			Id(larkim.NewUserIdBuilder().OpenId("ou_bot").Build()).
			Name("Memknow").
			Build(),
	}
	event := newTextMessageEvent("group", "oc_group", "", "om_3", "ou_user", "@_user_1 hello", mentions)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.msgs))
	}
	if !dispatcher.msgs[0].MentionsMe {
		t.Fatal("MentionsMe = false, want true")
	}
	if dispatcher.msgs[0].RepliesToMe {
		t.Fatal("RepliesToMe = true, want false")
	}
	if got := dispatcher.msgs[0].Prompt; got != "@Memknow hello" {
		t.Fatalf("prompt = %q, want %q", got, "@Memknow hello")
	}
}

func TestHandleMessage_P2PWithoutMention_Dispatched(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg: &config.AppConfig{ID: "app-a"},
		dispatcher: dispatcher,
		botOpenID: "ou_bot",
		botIDs: []string{"app-a", "cloud", "oii"},
	}

	event := newTextMessageEvent("p2p", "oc_p2p", "", "om_4", "ou_user", "hello", nil)
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.msgs))
	}
	if dispatcher.msgs[0].RepliesToMe {
		t.Fatal("RepliesToMe = true, want false")
	}
	if dispatcher.msgs[0].MentionsMe {
		t.Fatal("MentionsMe = true, want false")
	}
}

func TestHandleMessage_GroupReplyingToBot_Dispatched(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg: &config.AppConfig{ID: "app-a", FeishuAppID: "cli_bot_app"},
		dispatcher: dispatcher,
		botOpenID: "ou_bot",
		botIDs: []string{"app-a", "cloud", "oii"},
		parentMessageLookup: func(context.Context, string) (*larkim.Message, error) {
			return larkim.NewMessageBuilder().
				MessageId("om_parent").
				Sender(larkim.NewSenderBuilder().
					Id("cli_bot_app").
					IdType("app_id").
					SenderType("app").
					Build()).
				Build(), nil
		},
	}

	event := newTextMessageEvent("group", "oc_group", "", "om_6", "ou_user", "收到", nil)
	event.Event.Message.ParentId = stringPtr("om_parent")
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.msgs))
	}
}

func TestHandleMessage_GroupReplyingToNonBot_Dropped(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	r := &Receiver{
		appCfg: &config.AppConfig{ID: "app-a", FeishuAppID: "cli_bot_app"},
		dispatcher: dispatcher,
		botOpenID: "ou_bot",
		botIDs: []string{"app-a", "cloud", "oii"},
		parentMessageLookup: func(context.Context, string) (*larkim.Message, error) {
			return larkim.NewMessageBuilder().
				MessageId("om_parent").
				Sender(larkim.NewSenderBuilder().
					Id("cli_other_app").
					IdType("app_id").
					SenderType("app").
					Build()).
				Build(), nil
		},
	}

	event := newTextMessageEvent("group", "oc_group", "", "om_7", "ou_user", "收到", nil)
	event.Event.Message.ParentId = stringPtr("om_parent")
	if err := r.handleMessage(context.Background(), event); err != nil {
		t.Fatalf("handleMessage() error = %v", err)
	}
	if len(dispatcher.msgs) != 0 {
		t.Fatalf("dispatch count = %d, want 0", len(dispatcher.msgs))
	}
}

func TestParseContent_RewritesMentionPlaceholders(t *testing.T) {
	r := &Receiver{}
	msg := larkim.NewEventMessageBuilder().
		MessageType("text").
		Content(`{"text":"@_user_1 帮我看下"}`).
		Mentions([]*larkim.MentionEvent{
			larkim.NewMentionEventBuilder().
				Key("@_user_1").
				Id(larkim.NewUserIdBuilder().OpenId("ou_bot").Build()).
				Name("Cloud").
				Build(),
		}).
		Build()

	got, err := r.parseContent(context.Background(), msg, "text", "om_5", "ou_user", "oc_group")
	if err != nil {
		t.Fatalf("parseContent() error = %v", err)
	}
	if got != "@Cloud 帮我看下" {
		t.Fatalf("parseContent() = %q, want %q", got, "@Cloud 帮我看下")
	}
}

func newTextMessageEvent(chatType, chatID, threadID, messageID, senderOpenID, text string, mentions []*larkim.MentionEvent) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: larkim.NewEventSenderBuilder().
				SenderId(larkim.NewUserIdBuilder().OpenId(senderOpenID).Build()).
				Build(),
			Message: larkim.NewEventMessageBuilder().
				MessageId(messageID).
				ChatId(chatID).
				ChatType(chatType).
				ThreadId(threadID).
				MessageType("text").
				Content(`{"text":"` + text + `"}`).
				Mentions(mentions).
				Build(),
		},
	}
}

func stringPtr(v string) *string {
	return &v
}

func TestParsePostContent_TextAndLink(t *testing.T) {
	r := &Receiver{}
	content := `{"title":"标题","content":[[{"tag":"text","text":"第一行"},{"tag":"a","text":"链接"}],[{"tag":"text","text":"第二行"}]]}`
	got, err := r.parsePostContent(context.Background(), content, "msg_123")
	if err != nil {
		t.Fatalf("parsePostContent() error = %v", err)
	}
	want := "标题\n第一行链接\n第二行"
	if got != want {
		t.Errorf("parsePostContent() = %q, want %q", got, want)
	}
}

func TestParsePostContent_InvalidJSON(t *testing.T) {
	r := &Receiver{}
	content := "not-json"
	got, err := r.parsePostContent(context.Background(), content, "msg_123")
	if err != nil {
		t.Fatalf("parsePostContent() error = %v", err)
	}
	if got != content {
		t.Errorf("parsePostContent() = %q, want %q", got, content)
	}
}
