package session

import (
	"context"
	"testing"

	"github.com/ashwinyue/Memknow/internal/feishu"
)

func TestManager_HandleImmediateBuiltInCommand_StopWithoutActiveRun(t *testing.T) {
	ts := &testSender{}
	m := &Manager{
		senders: map[string]sender{
			"code": ts,
		},
	}
	msg := &feishu.IncomingMessage{
		AppID:       "code",
		ChannelKey:  "p2p:chat:code",
		Prompt:      "/stop",
		ReceiveID:   "ou_x",
		ReceiveType: "open_id",
	}

	if !m.handleImmediateBuiltInCommand(context.Background(), msg) {
		t.Fatal("expected /stop to be handled immediately")
	}
	if len(ts.texts) != 1 || ts.texts[0] != "当前没有正在执行的任务。" {
		t.Fatalf("texts = %#v, want no-active-run reply", ts.texts)
	}
}

func TestManager_HandleImmediateBuiltInCommand_StopActiveRun(t *testing.T) {
	ts := &testSender{}
	worker := &Worker{}
	runCtx, finish := worker.beginCurrentRun(context.Background())
	defer finish()

	m := &Manager{
		senders: map[string]sender{
			"code": ts,
		},
	}
	m.workers.Store("p2p:chat:code", worker)

	msg := &feishu.IncomingMessage{
		AppID:       "code",
		ChannelKey:  "p2p:chat:code",
		Prompt:      "/stop",
		ReceiveID:   "ou_x",
		ReceiveType: "open_id",
	}

	if !m.handleImmediateBuiltInCommand(context.Background(), msg) {
		t.Fatal("expected /stop to be handled immediately")
	}
	select {
	case <-runCtx.Done():
	default:
		t.Fatal("expected active run to be canceled")
	}
	if len(ts.texts) != 1 || ts.texts[0] != "已停止当前执行。" {
		t.Fatalf("texts = %#v, want stopped reply", ts.texts)
	}
}
