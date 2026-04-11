package model

import "strings"

const (
	SessionTypeChat      = "chat"
	SessionTypeHeartbeat = "heartbeat"
	SessionTypeSchedule  = "schedule"
)

func NormalizeSessionType(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case SessionTypeHeartbeat:
		return SessionTypeHeartbeat
	case SessionTypeSchedule:
		return SessionTypeSchedule
	default:
		return SessionTypeChat
	}
}
