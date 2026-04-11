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
