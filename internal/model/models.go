package model

import "time"

// Channel represents a stable Feishu channel (p2p / group / thread).
type Channel struct {
	ChannelKey string `gorm:"primaryKey"`
	AppID      string `gorm:"index;not null"`
	ChatType   string `gorm:"not null"` // p2p / group / topic_group
	ChatID     string `gorm:"not null"`
	ThreadID   string // only set for topic_group
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Session represents one conversation session within a channel.
type Session struct {
	ID               string `gorm:"primaryKey"`
	ChannelKey       string `gorm:"index;not null"`
	Type             string `gorm:"not null;default:'chat';index"`
	ClaudeSessionID  string // --resume parameter; empty = new context
	Status           string `gorm:"not null;default:'active'"` // active / archived
	CreatedBy        string // open_id of user who created the session
	Title            string `gorm:"index"` // auto-generated from first user message
	ParentSessionID  string // for session lineage after /new or compression
	InputTokens      int    `gorm:"default:0"`
	OutputTokens     int    `gorm:"default:0"`
	CacheReadTokens  int    `gorm:"default:0"`
	CacheWriteTokens int    `gorm:"default:0"`
	EstimatedCostUSD float64
	Model            string // actual model used for this session
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Message records a single message in a session.
type Message struct {
	ID           string `gorm:"primaryKey"`
	SessionID    string `gorm:"index;not null"`
	SenderID     string // open_id of sender (empty for assistant)
	Role         string `gorm:"not null"` // user / assistant / tool
	Content      string `gorm:"type:text"`
	FeishuMsgID  string // original Feishu message_id
	ToolCallID   string // for tool result messages
	ToolName     string // for tool_use / tool_result
	Reasoning    string `gorm:"type:text"` // assistant reasoning text
	TokenCount   int
	FinishReason string
	CreatedAt    time.Time
}

// MessageToolCall stores structured tool-call records associated with an assistant message.
type MessageToolCall struct {
	ID         string `gorm:"primaryKey"`
	SessionID  string `gorm:"index;not null"`
	MessageID  string `gorm:"index;not null"` // assistant message id
	CallID     string `gorm:"index;not null"` // stable within one assistant message
	Name       string `gorm:"not null"`
	Input      string `gorm:"type:text"`
	Output     string `gorm:"type:text"`
	OrderIndex int    `gorm:"not null;default:0"`
	CreatedAt  time.Time
}

// SessionSummary stores an auto-generated summary for an archived session.
type SessionSummary struct {
	ID           string `gorm:"primaryKey"`
	SessionID    string `gorm:"index;not null"`
	ChannelKey   string `gorm:"index;not null"`
	Content      string `gorm:"type:text;not null"`
	MessageCount int    `gorm:"default:0"`
	GeneratedBy  string
	CreatedAt    time.Time
}

// Schedule represents a built-in business scheduled task.
type Schedule struct {
	ID          string `gorm:"primaryKey"`
	AppID       string `gorm:"index;not null"`
	Name        string `gorm:"not null"`
	Description string
	CronExpr    string `gorm:"not null"`
	TargetType  string `gorm:"not null"` // p2p / group
	TargetID    string `gorm:"not null"` // open_id or chat_id
	Command     string `gorm:"type:text;not null"`
	Enabled     bool   `gorm:"not null;default:true"`
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastRunAt   *time.Time
}

// ScheduleLog stores one execution record for a schedule run.
type ScheduleLog struct {
	ID           string `gorm:"primaryKey"`
	ScheduleID   string `gorm:"index;not null"`
	SessionID    string `gorm:"index"`
	Status       string `gorm:"not null;default:'ok'"` // ok / error
	ResultText   string `gorm:"type:text"`
	ErrorMessage string `gorm:"type:text"`
	StartedAt    time.Time
	CompletedAt  *time.Time
}
