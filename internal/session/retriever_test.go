package session

import (
	"strings"
	"testing"
	"time"

	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/model"
	"github.com/google/uuid"
)

func TestRetriever_Retrieve(t *testing.T) {
	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	channelKey := "p2p:test:app1"
	archivedSessID := uuid.New().String()
	activeSessID := uuid.New().String()

	// Insert sessions
	testDB.Create(&model.Session{ID: archivedSessID, ChannelKey: channelKey, Status: statusArchived, CreatedAt: time.Now()})
	testDB.Create(&model.Session{ID: activeSessID, ChannelKey: channelKey, Status: statusActive, CreatedAt: time.Now()})

	// Insert a session summary for the archived session
	testDB.Create(&model.SessionSummary{
		ID:         uuid.New().String(),
		SessionID:  archivedSessID,
		ChannelKey: channelKey,
		Content:    "Summary of archived session",
		CreatedAt:  time.Now(),
	})

	// Insert messages: one in archived, one in active
	testDB.Create(&model.Message{ID: uuid.New().String(), SessionID: archivedSessID, Role: "user", Content: "deploy script location", CreatedAt: time.Now()})
	testDB.Create(&model.Message{ID: uuid.New().String(), SessionID: activeSessID, Role: "user", Content: "deploy script location", CreatedAt: time.Now()})

	r := NewRetriever(testDB)
	payload, err := r.Retrieve(channelKey, "deploy script")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(payload.Summaries) != 1 {
		t.Errorf("Summaries count = %d, want 1", len(payload.Summaries))
	}
	if len(payload.Matches) < 1 {
		t.Errorf("Matches count = %d, want >= 1", len(payload.Matches))
	}

	// Ensure we only got the archived message, not the active one.
	if len(payload.Matches) > 0 && payload.Matches[0].SessionID != archivedSessID {
		t.Errorf("Expected match from archived session %s, got %s", archivedSessID, payload.Matches[0].SessionID)
	}

	prompt := payload.ToPrompt()
	if !strings.Contains(prompt, "Summary of archived session") {
		t.Errorf("Prompt missing summary")
	}
	if !strings.Contains(strings.ToLower(prompt), "deploy") {
		t.Errorf("Prompt missing search keyword in match snippet")
	}
}

func TestRetriever_Retrieve_Empty(t *testing.T) {
	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	r := NewRetriever(testDB)
	payload, err := r.Retrieve("p2p:empty:app1", "nothing")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if !payload.IsEmpty() {
		t.Errorf("Expected empty payload for no data")
	}
}

func TestRetriever_Retrieve_FallbackLikeForChinese(t *testing.T) {
	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	channelKey := "p2p:test:cn"
	archivedSessID := uuid.New().String()
	testDB.Create(&model.Session{
		ID:         archivedSessID,
		ChannelKey: channelKey,
		Status:     statusArchived,
		CreatedAt:  time.Now(),
	})
	testDB.Create(&model.Message{
		ID:        uuid.New().String(),
		SessionID: archivedSessID,
		Role:      "user",
		Content:   "我喜欢吃西红柿，尤其是番茄炒蛋。",
		CreatedAt: time.Now(),
	})

	r := NewRetriever(testDB)
	payload, err := r.Retrieve(channelKey, "西红柿")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(payload.Matches) == 0 {
		t.Fatalf("expected fallback route to return matches for Chinese query")
	}
}

func TestRetriever_Retrieve_SummaryRecallWhenNoMessageHit(t *testing.T) {
	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	channelKey := "p2p:test:summary"
	archivedSessID := uuid.New().String()
	testDB.Create(&model.Session{
		ID:         archivedSessID,
		ChannelKey: channelKey,
		Status:     statusArchived,
		Title:      "提醒会话",
		CreatedAt:  time.Now(),
	})
	testDB.Create(&model.Message{
		ID:        uuid.New().String(),
		SessionID: archivedSessID,
		Role:      "user",
		Content:   "这条正文不包含关键词",
		CreatedAt: time.Now(),
	})
	testDB.Create(&model.SessionSummary{
		ID:         uuid.New().String(),
		SessionID:  archivedSessID,
		ChannelKey: channelKey,
		Content:    "用户要求记住喜欢吃西红柿。",
		CreatedAt:  time.Now(),
	})

	r := NewRetriever(testDB)
	payload, err := r.Retrieve(channelKey, "西红柿")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(payload.Matches) == 0 {
		t.Fatalf("expected summary recall to provide at least one match")
	}
	found := false
	for _, m := range payload.Matches {
		if strings.Contains(m.Snippet, "会话摘要命中") || strings.Contains(m.Content, "会话摘要命中") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected summary-derived match in payload")
	}
}

func TestRetriever_Retrieve_IgnoresNonChatSessions(t *testing.T) {
	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	channelKey := "group:test:mixed"
	chatSessID := uuid.New().String()
	scheduleSessID := uuid.New().String()

	testDB.Create(&model.Session{
		ID:         chatSessID,
		ChannelKey: channelKey,
		Type:       model.SessionTypeChat,
		Status:     statusArchived,
		Title:      "聊天会话",
		CreatedAt:  time.Now(),
	})
	testDB.Create(&model.Session{
		ID:         scheduleSessID,
		ChannelKey: channelKey,
		Type:       model.SessionTypeSchedule,
		Status:     statusArchived,
		Title:      "定时任务会话",
		CreatedAt:  time.Now(),
	})

	testDB.Create(&model.SessionSummary{
		ID:         uuid.New().String(),
		SessionID:  chatSessID,
		ChannelKey: channelKey,
		Content:    "聊天里提到 deploy script 在 scripts/deploy.sh。",
		CreatedAt:  time.Now(),
	})
	testDB.Create(&model.SessionSummary{
		ID:         uuid.New().String(),
		SessionID:  scheduleSessID,
		ChannelKey: channelKey,
		Content:    "定时任务执行过 deploy script 并广播结果。",
		CreatedAt:  time.Now(),
	})

	testDB.Create(&model.Message{
		ID:        uuid.New().String(),
		SessionID: chatSessID,
		Role:      "user",
		Content:   "deploy script 在 scripts/deploy.sh",
		CreatedAt: time.Now(),
	})
	testDB.Create(&model.Message{
		ID:        uuid.New().String(),
		SessionID: scheduleSessID,
		Role:      "assistant",
		Content:   "定时任务自动执行 deploy script 并推送提醒",
		CreatedAt: time.Now(),
	})

	r := NewRetriever(testDB)
	payload, err := r.Retrieve(channelKey, "deploy script")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(payload.Summaries) != 1 {
		t.Fatalf("Summaries count = %d, want 1", len(payload.Summaries))
	}
	if payload.Summaries[0].SessionID != chatSessID {
		t.Fatalf("summary session = %s, want chat session %s", payload.Summaries[0].SessionID, chatSessID)
	}

	if len(payload.Matches) == 0 {
		t.Fatalf("expected at least one match from chat session")
	}
	for _, m := range payload.Matches {
		if m.SessionID != chatSessID {
			t.Fatalf("unexpected non-chat match from session %s: %+v", m.SessionID, m)
		}
	}

	prompt := payload.ToPrompt()
	if strings.Contains(prompt, "定时任务执行过") || strings.Contains(prompt, "自动执行 deploy script") {
		t.Fatalf("prompt should exclude schedule session content, got: %s", prompt)
	}
}
