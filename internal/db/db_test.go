package db_test

import (
	"path/filepath"
	"testing"

	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/model"
)

func TestOpen_CreatesTablesAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Verify all expected tables exist by checking GORM can create records.
	ch := model.Channel{ChannelKey: "p2p:u1:a1", AppID: "a1", ChatType: "p2p", ChatID: "u1"}
	if err := database.Create(&ch).Error; err != nil {
		t.Errorf("create Channel: %v", err)
	}

	sess := model.Session{ID: "sess-1", ChannelKey: "p2p:u1:a1", Status: "active"}
	if err := database.Create(&sess).Error; err != nil {
		t.Errorf("create Session: %v", err)
	}

	msg := model.Message{ID: "msg-1", SessionID: "sess-1", Role: "user", Content: "hi"}
	if err := database.Create(&msg).Error; err != nil {
		t.Errorf("create Message: %v", err)
	}

	schedule := model.Schedule{ID: "schedule-1", AppID: "a1", Name: "test", CronExpr: "* * * * *", TargetType: "p2p", TargetID: "u1", Command: "提醒我喝水", Enabled: true}
	if err := database.Create(&schedule).Error; err != nil {
		t.Errorf("create Schedule: %v", err)
	}

	var count int64
	if err := database.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", "tasks").Scan(&count).Error; err != nil {
		t.Fatalf("check tasks table: %v", err)
	}
	if count != 0 {
		t.Fatalf("legacy tasks table should be absent, found %d", count)
	}
}

func TestOpen_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db1, err := db.Open(path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	_ = db1

	// Second open should also succeed (AutoMigrate is idempotent).
	db2, err := db.Open(path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	_ = db2
}

func TestOpen_InvalidPath(t *testing.T) {
	// A path in a non-existent directory should fail.
	_, err := db.Open("/nonexistent/dir/test.db")
	if err == nil {
		t.Error("Open() with invalid path should return error")
	}
}
