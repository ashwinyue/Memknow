package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/ashwinyue/Memknow/internal/config"
)

// Dispatcher routes incoming messages to the session manager.
// Implemented by session.Manager.
type Dispatcher interface {
	Dispatch(ctx context.Context, msg *IncomingMessage) error
}

// IncomingMessage carries a normalized Feishu message ready for processing.
type IncomingMessage struct {
	AppID       string
	ChannelKey  string
	ChatType    string // p2p / group / topic_group
	ChatID      string
	ThreadID    string
	MentionsMe  bool
	RepliesToMe bool
	NamesMe     bool
	SenderID    string // open_id
	MessageID   string // Feishu message_id (for dedup)
	Prompt      string // text with local paths substituted for attachments
	ReceiveID   string // where to send the reply
	ReceiveType string // open_id / chat_id
}

// Receiver connects to Feishu WebSocket and dispatches messages.
type Receiver struct {
	appCfg     *config.AppConfig
	client     *lark.Client
	dispatcher Dispatcher
	sender     *Sender
	wsClient   *larkws.Client
	botOpenID  string
	botIDOnce  sync.Once
	botIDs     []string
	parentMessageLookup func(ctx context.Context, parentID string) (*larkim.Message, error)
}

// NewReceiver creates a Receiver for one Feishu app.
func NewReceiver(appCfg *config.AppConfig, botIDs []string, dispatcher Dispatcher) *Receiver {
	client := lark.NewClient(appCfg.FeishuAppID, appCfg.FeishuAppSecret)
	return &Receiver{
		appCfg:     appCfg,
		botIDs:     append([]string(nil), botIDs...),
		client:     client,
		dispatcher: dispatcher,
		sender:     NewSender(client),
	}
}

// LarkClient returns the underlying Feishu API client (used to build Sender).
func (r *Receiver) LarkClient() *lark.Client {
	return r.client
}

// Start connects to Feishu WebSocket and blocks until ctx is cancelled.
func (r *Receiver) Start(ctx context.Context) error {
	eventHandler := dispatcher.NewEventDispatcher(
		r.appCfg.FeishuVerificationToken,
		r.appCfg.FeishuEncryptKey,
	).OnP2MessageReceiveV1(r.handleMessage).
		OnP2MessageReadV1(r.handleMessageRead).
		OnP2MessageReactionCreatedV1(r.handleMessageReactionCreated).
		OnP2MessageReactionDeletedV1(r.handleMessageReactionDeleted).
		OnP2ChatMemberBotAddedV1(r.handleBotAddedToGroup).
		OnP2ChatMemberUserAddedV1(r.handleUserAddedToGroup)

	r.wsClient = larkws.NewClient(
		r.appCfg.FeishuAppID,
		r.appCfg.FeishuAppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)

	slog.Info("feishu WS client starting", "app_id", r.appCfg.ID)
	return r.wsClient.Start(ctx)
}

// handleMessageRead ignores read-receipt events to avoid noisy "not found handler" logs.
func (r *Receiver) handleMessageRead(ctx context.Context, event *larkim.P2MessageReadV1) error {
	return nil
}

// handleMessageReactionCreated ignores reaction-created events to avoid noisy
// "not found handler" logs from Feishu websocket dispatcher.
func (r *Receiver) handleMessageReactionCreated(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
	return nil
}

// handleMessageReactionDeleted ignores reaction-deleted events to avoid noisy
// "not found handler" logs from Feishu websocket dispatcher.
func (r *Receiver) handleMessageReactionDeleted(ctx context.Context, event *larkim.P2MessageReactionDeletedV1) error {
	return nil
}

// handleMessage is the callback for P2MessageReceiveV1 events.
func (r *Receiver) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil {
		return nil
	}
	msg := event.Event.Message
	sender := event.Event.Sender

	if msg == nil || msg.MessageId == nil {
		return nil
	}

	msgType := safeStr(msg.MessageType)
	chatType := safeStr(msg.ChatType)
	chatID := safeStr(msg.ChatId)
	threadID := safeStr(msg.ThreadId)
	messageID := safeStr(msg.MessageId)
	senderOpenID := ""
	if sender != nil && sender.SenderId != nil {
		senderOpenID = safeStr(sender.SenderId.OpenId)
	}

	// Check allowed chats.
	if !r.appCfg.AllowedChat(chatID) {
		slog.Debug("feishu: chat not allowed", "chat_id", chatID)
		return nil
	}
	// Determine reply target.
	receiveID, receiveType := replyTarget(chatType, chatID, senderOpenID)

	// Parse message content and download attachments.
	prompt, err := r.parseContent(ctx, msg, msgType, messageID, senderOpenID, receiveID)
	if err != nil {
		slog.Error("feishu: parse content", "err", err)
		return nil
	}
	if prompt == "" {
		return nil
	}

	mentionsMe, repliesToMe := r.directAddressFlags(ctx, msg, chatType)
	namesMe, namesOther := r.nameTargetFlags(prompt, chatType)
	if shouldIgnoreForOtherMention(msg, chatType, mentionsMe) {
		slog.Debug("feishu: group message ignored due to explicit mention of another target", "chat_id", chatID, "chat_type", chatType)
		return nil
	}
	if shouldIgnoreForOtherReply(msg, chatType, repliesToMe) {
		slog.Debug("feishu: group message ignored due to reply targeting another sender", "chat_id", chatID, "chat_type", chatType)
		return nil
	}
	if shouldIgnoreForOtherName(chatType, namesMe, namesOther) {
		slog.Debug("feishu: group message ignored due to app id naming another bot", "chat_id", chatID, "chat_type", chatType)
		return nil
	}

	// Build channel key.
	channelKey := BuildChannelKey(chatType, chatID, threadID, r.appCfg.ID)

	incoming := &IncomingMessage{
		AppID:       r.appCfg.ID,
		ChannelKey:  channelKey,
		ChatType:    chatType,
		ChatID:      chatID,
		ThreadID:    threadID,
		MentionsMe:  mentionsMe,
		RepliesToMe: repliesToMe,
		NamesMe:     namesMe,
		SenderID:    senderOpenID,
		MessageID:   messageID,
		Prompt:      prompt,
		ReceiveID:   receiveID,
		ReceiveType: receiveType,
	}

	if err := r.dispatcher.Dispatch(ctx, incoming); err != nil {
		slog.Error("feishu: dispatch", "err", err)
	}
	return nil
}

func (r *Receiver) directAddressFlags(ctx context.Context, msg *larkim.EventMessage, chatType string) (bool, bool) {
	if msg == nil {
		return false, false
	}
	switch chatType {
	case "p2p":
		return false, false
	case "group", "topic_group", "topic":
		botOpenID := r.resolveBotOpenID(ctx)
		return messageMentionsBot(msg, botOpenID), r.isReplyToBot(ctx, msg, botOpenID)
	default:
		return false, false
	}
}

func shouldIgnoreForOtherMention(msg *larkim.EventMessage, chatType string, mentionsMe bool) bool {
	if msg == nil || mentionsMe {
		return false
	}
	switch chatType {
	case "group", "topic_group", "topic":
		return len(normalizeMentions(msg.Mentions)) > 0
	default:
		return false
	}
}

func shouldIgnoreForOtherReply(msg *larkim.EventMessage, chatType string, repliesToMe bool) bool {
	if msg == nil || repliesToMe || msg.ParentId == nil || strings.TrimSpace(*msg.ParentId) == "" {
		return false
	}
	switch chatType {
	case "group", "topic_group", "topic":
		return true
	default:
		return false
	}
}

func shouldIgnoreForOtherName(chatType string, namesMe, namesOther bool) bool {
	if namesMe || !namesOther {
		return false
	}
	switch chatType {
	case "group", "topic_group", "topic":
		return true
	default:
		return false
	}
}

func (r *Receiver) nameTargetFlags(prompt, chatType string) (bool, bool) {
	switch chatType {
	case "group", "topic_group", "topic":
	default:
		return false, false
	}
	currentID := strings.TrimSpace(strings.ToLower(r.appCfg.ID))
	if currentID == "" {
		return false, false
	}
	namesMe := containsStandaloneBotID(prompt, currentID)
	namesOther := false
	for _, botID := range r.botIDs {
		normalized := strings.TrimSpace(strings.ToLower(botID))
		if normalized == "" || normalized == currentID {
			continue
		}
		if containsStandaloneBotID(prompt, normalized) {
			namesOther = true
			break
		}
	}
	return namesMe, namesOther
}

func containsStandaloneBotID(text, botID string) bool {
	text = strings.TrimSpace(text)
	botID = strings.ToLower(strings.TrimSpace(botID))
	if text == "" || botID == "" {
		return false
	}
	pattern := `(?i)(^|[^\p{L}\p{N}_-])` + regexp.QuoteMeta(botID) + `($|[^\p{L}\p{N}_-])`
	return regexp.MustCompile(pattern).MatchString(text)
}

func (r *Receiver) resolveBotOpenID(ctx context.Context) string {
	r.botIDOnce.Do(func() {
		if r == nil || r.client == nil {
			return
		}
		resp, err := r.client.Get(ctx, "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
		if err != nil {
			slog.Warn("feishu: resolve bot open_id failed", "app_id", r.appCfg.ID, "err", err)
			return
		}
		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Bot  struct {
				OpenID string `json:"open_id"`
			} `json:"bot"`
		}
		if err := json.Unmarshal(resp.RawBody, &body); err != nil {
			slog.Warn("feishu: parse bot info failed", "app_id", r.appCfg.ID, "err", err)
			return
		}
		if body.Code != 0 {
			slog.Warn("feishu: bot info returned error", "app_id", r.appCfg.ID, "code", body.Code, "msg", body.Msg)
			return
		}
		r.botOpenID = strings.TrimSpace(body.Bot.OpenID)
	})
	return r.botOpenID
}

func messageMentionsBot(msg *larkim.EventMessage, botOpenID string) bool {
	if msg == nil {
		return false
	}
	mentions := normalizeMentions(msg.Mentions)
	botOpenID = strings.TrimSpace(botOpenID)
	if botOpenID != "" {
		for _, mention := range mentions {
			if mention == botOpenID {
				return true
			}
		}
		return false
	}
	return len(mentions) > 0
}

func (r *Receiver) isReplyToBot(ctx context.Context, msg *larkim.EventMessage, botOpenID string) bool {
	if msg == nil || msg.ParentId == nil || strings.TrimSpace(*msg.ParentId) == "" {
		return false
	}
	parent, err := r.lookupParentMessage(ctx, strings.TrimSpace(*msg.ParentId))
	if err != nil || parent == nil || parent.Sender == nil {
		return false
	}
	senderType := safeStr(parent.Sender.SenderType)
	if senderType != "app" {
		return false
	}
	parentSenderID := strings.TrimSpace(safeStr(parent.Sender.Id))
	parentSenderIDType := strings.TrimSpace(safeStr(parent.Sender.IdType))
	switch parentSenderIDType {
	case "app_id":
		return parentSenderID != "" && parentSenderID == strings.TrimSpace(r.appCfg.FeishuAppID)
	case "open_id", "":
		if strings.TrimSpace(botOpenID) == "" {
			return parentSenderID != ""
		}
		return parentSenderID == strings.TrimSpace(botOpenID)
	default:
		return false
	}
}

func (r *Receiver) lookupParentMessage(ctx context.Context, parentID string) (*larkim.Message, error) {
	if r.parentMessageLookup != nil {
		return r.parentMessageLookup(ctx, parentID)
	}
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("feishu: client not configured")
	}
	req := larkim.NewGetMessageReqBuilder().
		MessageId(parentID).
		Build()
	resp, err := r.client.Im.Message.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil || !resp.Success() || resp.Data == nil || len(resp.Data.Items) == 0 {
		return nil, fmt.Errorf("feishu: parent message not found")
	}
	return resp.Data.Items[0], nil
}

func normalizeMentions(mentions []*larkim.MentionEvent) []string {
	if len(mentions) == 0 {
		return nil
	}
	result := make([]string, 0, len(mentions))
	for _, mention := range mentions {
		if mention == nil || mention.Id == nil || mention.Id.OpenId == nil {
			continue
		}
		openID := strings.TrimSpace(*mention.Id.OpenId)
		if openID != "" {
			result = append(result, openID)
		}
	}
	return result
}

func rewriteMentionPlaceholders(text string, mentions []*larkim.MentionEvent) string {
	if strings.TrimSpace(text) == "" || len(mentions) == 0 {
		return text
	}
	type replacement struct {
		key   string
		value string
	}
	items := make([]replacement, 0, len(mentions))
	for _, mention := range mentions {
		if mention == nil || mention.Key == nil {
			continue
		}
		key := strings.TrimSpace(*mention.Key)
		if key == "" {
			continue
		}
		items = append(items, replacement{
			key:   key,
			value: mentionDisplayName(mention),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return len(items[i].key) > len(items[j].key)
	})
	rewritten := text
	for _, item := range items {
		rewritten = strings.ReplaceAll(rewritten, item.key, item.value)
	}
	return rewritten
}

func mentionDisplayName(mention *larkim.MentionEvent) string {
	if mention != nil && mention.Name != nil {
		name := strings.TrimSpace(*mention.Name)
		if name != "" {
			if strings.HasPrefix(name, "@") {
				return name
			}
			return "@" + name
		}
	}
	return "@user"
}

// parseContent extracts text and downloads attachments from a Feishu message.
func (r *Receiver) parseContent(
	ctx context.Context,
	msg *larkim.EventMessage,
	msgType, messageID, senderOpenID, chatID string,
) (string, error) {
	content := safeStr(msg.Content)

	switch msgType {
	case "text":
		var v struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &v); err != nil {
			return "", fmt.Errorf("parse text: %w", err)
		}
		return rewriteMentionPlaceholders(v.Text, msg.Mentions), nil

	case "image":
		var v struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(content), &v); err != nil {
			return "", fmt.Errorf("parse image: %w", err)
		}
		localPath, err := r.downloadImageResource(ctx, messageID, v.ImageKey)
		if err != nil {
			slog.Warn("feishu: download image failed, skipping", "err", err)
			return "[图片下载失败]", nil
		}
		return fmt.Sprintf("[图片: %s]", localPath), nil

	case "file":
		var v struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(content), &v); err != nil {
			return "", fmt.Errorf("parse file: %w", err)
		}
		localPath, err := r.downloadFile(ctx, messageID, v.FileKey, v.FileName)
		if err != nil {
			slog.Warn("feishu: download file failed, skipping", "err", err)
			return fmt.Sprintf("[文件 %s 下载失败]", v.FileName), nil
		}
		return fmt.Sprintf("[文件: %s]", localPath), nil

	case "post":
		return r.parsePostContent(ctx, content, messageID)

	default:
		slog.Debug("feishu: unsupported message type", "type", msgType)
		return "", nil
	}
}

// downloadImageResource downloads an image resource from a message.
func (r *Receiver) downloadImageResource(ctx context.Context, messageID, imageKey string) (string, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(imageKey).
		Type("image").
		Build()

	resp, err := r.client.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get image resource: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("get image API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	// Save to a temp path (actual session dir unknown here; will be moved by worker).
	dir := os.TempDir()
	filename := fmt.Sprintf("feishu_img_%s_%d.jpg", imageKey, time.Now().UnixNano())
	localPath := filepath.Join(dir, filename)

	if err := saveBody(resp.File, localPath); err != nil {
		return "", err
	}
	return localPath, nil
}

// downloadFile downloads a file attachment from a message.
func (r *Receiver) downloadFile(ctx context.Context, messageID, fileKey, fileName string) (string, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(fileKey).
		Type("file").
		Build()

	resp, err := r.client.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get file resource: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("get file API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	dir := os.TempDir()
	localName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeFilename(fileName))
	localPath := filepath.Join(dir, localName)

	if err := saveBody(resp.File, localPath); err != nil {
		return "", err
	}
	return localPath, nil
}

// maxAttachmentBytes is the maximum size we will write from any single attachment (100 MiB).
const maxAttachmentBytes = 100 << 20

// saveBody writes an io.Reader to a local file, capping at maxAttachmentBytes.
func saveBody(body io.Reader, path string) error {
	if body == nil {
		return fmt.Errorf("empty response body")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// H-3: limit copy size to prevent runaway disk exhaustion.
	_, err = io.Copy(f, io.LimitReader(body, maxAttachmentBytes))
	return err
}

// DownloadURL downloads content from a URL (with context) and saves it locally.
// H-2: caller supplies context for cancellation / timeout.
func DownloadURL(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return saveBody(resp.Body, destPath)
}

// BuildChannelKey returns the stable channel identifier.
func BuildChannelKey(chatType, chatID, threadID, appID string) string {
	switch chatType {
	case "p2p":
		return fmt.Sprintf("p2p:%s:%s", chatID, appID)
	case "topic_group", "topic":
		return fmt.Sprintf("thread:%s:%s:%s", chatID, threadID, appID)
	default: // group
		return fmt.Sprintf("group:%s:%s", chatID, appID)
	}
}

// ReceiveIDType returns the Feishu receive_id_type for a target.
func ReceiveIDType(targetType, targetID string) string {
	if targetType == "p2p" {
		if strings.HasPrefix(targetID, "oc_") {
			return "chat_id"
		}
		return "open_id"
	}
	return "chat_id"
}

// replyTarget returns the receive_id and receive_id_type for sending a reply.
func replyTarget(chatType, chatID, senderOpenID string) (string, string) {
	if chatType == "p2p" {
		return senderOpenID, "open_id"
	}
	return chatID, "chat_id"
}

// extractPostText pulls plain text from a Feishu "post" (rich text) content blob.
func extractPostText(content string) string {
	var post struct {
		Title   string                     `json:"title"`
		Content [][]map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal([]byte(content), &post); err != nil {
		return content
	}
	var sb strings.Builder
	if post.Title != "" {
		sb.WriteString(post.Title + "\n")
	}
	for _, row := range post.Content {
		for _, elem := range row {
			if tag, _ := elem["tag"].(string); tag == "text" {
				if text, _ := elem["text"].(string); text != "" {
					sb.WriteString(text)
				}
			}
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// parsePostContent extracts text and downloads attachments from a Feishu "post" message.
func (r *Receiver) parsePostContent(ctx context.Context, content, messageID string) (string, error) {
	var post struct {
		Title   string                     `json:"title"`
		Content [][]map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal([]byte(content), &post); err != nil {
		return extractPostText(content), nil
	}

	var sb strings.Builder
	if post.Title != "" {
		sb.WriteString(post.Title + "\n")
	}

	for _, row := range post.Content {
		for _, elem := range row {
			tag, _ := elem["tag"].(string)
			switch tag {
			case "text", "a":
				if text, _ := elem["text"].(string); text != "" {
					sb.WriteString(text)
				}
			case "image", "img":
				imageKey, _ := elem["image_key"].(string)
				if imageKey != "" {
					localPath, err := r.downloadImageResource(ctx, messageID, imageKey)
					if err != nil {
						slog.Warn("feishu: download post image failed, skipping", "err", err)
						sb.WriteString("[图片下载失败]")
					} else {
						sb.WriteString(fmt.Sprintf("[图片: %s]", localPath))
					}
				}
			case "file":
				fileKey, _ := elem["file_key"].(string)
				fileName, _ := elem["file_name"].(string)
				if fileKey != "" {
					localPath, err := r.downloadFile(ctx, messageID, fileKey, fileName)
					if err != nil {
						slog.Warn("feishu: download post file failed, skipping", "err", err, "file_name", fileName)
						sb.WriteString(fmt.Sprintf("[文件 %s 下载失败]", fileName))
					} else {
						sb.WriteString(fmt.Sprintf("[文件: %s]", localPath))
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String()), nil
}

// handleBotAddedToGroup generates a Claude welcome when the bot is added to a group.
func (r *Receiver) handleBotAddedToGroup(ctx context.Context, event *larkim.P2ChatMemberBotAddedV1) error {
	if event.Event == nil || event.Event.ChatId == nil {
		return nil
	}
	chatID := safeStr(event.Event.ChatId)
	if !r.appCfg.AllowedChat(chatID) {
		return nil
	}
	groupName := safeStr(event.Event.Name)
	if groupName == "" {
		groupName = "本群"
	}
	msg := &IncomingMessage{
		AppID:       r.appCfg.ID,
		ChannelKey:  BuildChannelKey("group", chatID, "", r.appCfg.ID),
		ChatType:    "group",
		ChatID:      chatID,
		ReceiveID:   chatID,
		ReceiveType: "chat_id",
		Prompt: fmt.Sprintf(
			"你刚刚被添加到飞书群「%s」。请阅读 SOUL.md 了解你的角色定位，用中文写一条自我介绍欢迎消息，告诉群成员你是谁、能做什么、如何开始使用。直接输出消息内容，不要调用任何工具。",
			groupName,
		),
	}
	if err := r.dispatcher.Dispatch(ctx, msg); err != nil {
		slog.Warn("feishu: dispatch welcome on bot added", "chat_id", chatID, "err", err)
		if _, err2 := r.sender.SendCard(ctx, chatID, "chat_id", botAddedWelcome(r.appCfg.ID, safeStr(event.Event.Name))); err2 != nil {
			slog.Warn("feishu: welcome fallback failed", "chat_id", chatID, "err", err2)
		}
	}
	return nil
}

// handleUserAddedToGroup generates a Claude welcome when new users join a group.
func (r *Receiver) handleUserAddedToGroup(ctx context.Context, event *larkim.P2ChatMemberUserAddedV1) error {
	if event.Event == nil || event.Event.ChatId == nil {
		return nil
	}
	chatID := safeStr(event.Event.ChatId)
	if !r.appCfg.AllowedChat(chatID) {
		return nil
	}
	var names []string
	for _, u := range event.Event.Users {
		if u.Name != nil && *u.Name != "" {
			names = append(names, *u.Name)
		}
	}
	nameStr := "新成员"
	if len(names) > 0 {
		nameStr = strings.Join(names, "、")
	}
	msg := &IncomingMessage{
		AppID:       r.appCfg.ID,
		ChannelKey:  BuildChannelKey("group", chatID, "", r.appCfg.ID),
		ChatType:    "group",
		ChatID:      chatID,
		ReceiveID:   chatID,
		ReceiveType: "chat_id",
		Prompt: fmt.Sprintf(
			"「%s」刚刚加入了群组。请用中文写一条欢迎新成员的消息，热情迎接，并简单介绍你能为他们提供什么帮助。直接输出消息内容，不要调用任何工具。",
			nameStr,
		),
	}
	if err := r.dispatcher.Dispatch(ctx, msg); err != nil {
		slog.Warn("feishu: dispatch welcome on user added", "chat_id", chatID, "err", err)
		if _, err2 := r.sender.SendCard(ctx, chatID, "chat_id", userAddedWelcome(r.appCfg.ID, names)); err2 != nil {
			slog.Warn("feishu: welcome fallback failed", "chat_id", chatID, "err", err2)
		}
	}
	return nil
}

// botAddedWelcome returns the welcome message sent when the bot is added to a group.
func botAddedWelcome(appID, groupName string) string {
	if groupName == "" {
		groupName = "本群"
	}
	return fmt.Sprintf(
		"👋 大家好，我是 **%s**，一个长期记忆 AI Agent，很高兴加入 **%s**！\n\n**我能做什么：**\n• 💬 回答问题、撰写内容、分析数据\n• 🖼️ 阅读图片和文件\n• ⏰ 创建定时任务自动执行\n\n直接发消息给我即可开始，输入 `/new` 可开启全新会话，输入 `/mode` 可查看当前模式，`/work` 和 `/comp` 可快速切换。",
		appID, groupName,
	)
}

// userAddedWelcome returns the welcome message sent when users join a group.
func userAddedWelcome(appID string, names []string) string {
	nameStr := "新成员"
	if len(names) > 0 {
		nameStr = strings.Join(names, "、")
	}
	return fmt.Sprintf(
		"👋 欢迎 **%s** 加入！我是本群的 AI Agent **%s**，有任何问题都可以直接找我聊。",
		nameStr, appID,
	)
}

func safeStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	// Replace any path separators that might have slipped through.
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}
