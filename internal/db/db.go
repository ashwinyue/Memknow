package db

import (
	"fmt"

	"github.com/ashwinyue/Memknow/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens (or creates) the SQLite database at path and runs AutoMigrate.
func Open(path string) (*gorm.DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.AutoMigrate(
		&model.Channel{},
		&model.Session{},
		&model.Message{},
		&model.MessageToolCall{},
		&model.SessionSummary{},
		&model.Schedule{},
		&model.ScheduleLog{},
	); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	if err := migrateFTS5(db); err != nil {
		return nil, fmt.Errorf("migrate fts5: %w", err)
	}

	return db, nil
}

// migrateFTS5 creates the FTS5 virtual table and triggers for message search.
// Note: we use rowid (SQLite's implicit integer rowid) rather than the
// application-level string id because GORM declares id as TEXT PRIMARY KEY.
func migrateFTS5(db *gorm.DB) error {
	sql := `
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	content,
	content=messages,
	content_rowid=rowid
);

CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.rowid, old.content);
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES('delete', old.rowid, old.content);
	INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
END;
`
	if err := db.Exec(sql).Error; err != nil {
		return err
	}

	// Backfill existing rows for upgraded deployments that already have messages.
	var messagesCount int64
	if err := db.Model(&model.Message{}).Count(&messagesCount).Error; err != nil {
		return err
	}
	if messagesCount == 0 {
		return nil
	}
	var ftsCount int64
	if err := db.Raw("SELECT count(*) FROM messages_fts").Scan(&ftsCount).Error; err != nil {
		return err
	}
	if ftsCount == 0 {
		if err := db.Exec("INSERT INTO messages_fts(messages_fts) VALUES('rebuild')").Error; err != nil {
			return err
		}
	}
	return nil
}
