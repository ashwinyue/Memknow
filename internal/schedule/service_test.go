package schedule

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/db"
	"github.com/ashwinyue/Memknow/internal/feishu"
	"github.com/ashwinyue/Memknow/internal/model"
)

type fakeExecutor struct {
	lastReq *claude.ExecuteRequest
	result  *claude.ExecuteResult
}

func (f *fakeExecutor) Execute(_ context.Context, req *claude.ExecuteRequest) (*claude.ExecuteResult, error) {
	f.lastReq = req
	if f.result != nil {
		return f.result, nil
	}
	return &claude.ExecuteResult{Text: "done"}, nil
}

type fakeSender struct {
	lastReceiveID     string
	lastReceiveIDType string
	lastText          string
}

func (f *fakeSender) SendText(_ context.Context, receiveID string, receiveIDType string, text string) (string, error) {
	f.lastReceiveID = receiveID
	f.lastReceiveIDType = receiveIDType
	f.lastText = text
	return "msg-1", nil
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return database
}

func TestResolveDefaults(t *testing.T) {
	p2p := ResolveDefaults(&feishu.IncomingMessage{
		AppID:    "app1",
		ChatType: "p2p",
		ChatID:   "oc_chat",
		SenderID: "ou_user",
	})
	if p2p.TargetType != "p2p" || p2p.TargetID != "ou_user" || p2p.CreatedBy != "ou_user" {
		t.Fatalf("unexpected p2p defaults: %+v", p2p)
	}

	group := ResolveDefaults(&feishu.IncomingMessage{
		AppID:    "app1",
		ChatType: "group",
		ChatID:   "oc_group",
		SenderID: "ou_user",
	})
	if group.TargetType != "group" || group.TargetID != "oc_group" || group.CreatedBy != "ou_user" {
		t.Fatalf("unexpected group defaults: %+v", group)
	}
}

func TestService_CreateAndRun(t *testing.T) {
	database := openTestDB(t)
	exec := &fakeExecutor{result: &claude.ExecuteResult{Text: "记得喝水"}}
	sender := &fakeSender{}
	cfg := &config.Config{
		Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}},
	}
	svc, err := NewService(cfg, database, exec, map[string]Sender{"app1": sender})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()

	created, err := svc.Create(context.Background(), CreateInput{
		AppID:       "app1",
		Name:        "每小时喝水提醒",
		Description: "提醒喝水",
		CronExpr:    "0 * * * *",
		TargetType: "p2p",
		TargetID:    "ou_user",
		Command:     "提醒我喝水",
		CreatedBy:   "ou_user",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.runNow(context.Background(), created); err != nil {
		t.Fatalf("runNow() error = %v", err)
	}

	if exec.lastReq == nil || exec.lastReq.SessionType != model.SessionTypeSchedule {
		t.Fatalf("executor request = %+v, want schedule session", exec.lastReq)
	}
	if sender.lastText != "记得喝水" || sender.lastReceiveID != "ou_user" || sender.lastReceiveIDType != "open_id" {
		t.Fatalf("unexpected send result: %#v", sender)
	}

	var sess model.Session
	if err := database.Where("type = ?", model.SessionTypeSchedule).First(&sess).Error; err != nil {
		t.Fatalf("expected schedule session: %v", err)
	}
	if sess.Status != "archived" {
		t.Fatalf("session status = %q, want archived", sess.Status)
	}

	var log model.ScheduleLog
	if err := database.Where("schedule_id = ?", created.ID).First(&log).Error; err != nil {
		t.Fatalf("expected schedule log: %v", err)
	}
	if log.Status != "ok" {
		t.Fatalf("log status = %q, want ok", log.Status)
	}
}

func TestService_CreateFromMessage(t *testing.T) {
	database := openTestDB(t)
	cfg := &config.Config{Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}}}
	svc, err := NewService(cfg, database, &fakeExecutor{
		result: &claude.ExecuteResult{
			Text: `{"should_create":true,"name":"每小时喝水提醒","description":"喝水","cron_expr":"0 * * * *","command":"喝水"}`,
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()

	sched, ok, err := svc.CreateFromMessage(context.Background(), &cfg.Apps[0], &feishu.IncomingMessage{
		AppID:    "app1",
		ChatType: "p2p",
		ChatID:   "oc_chat",
		SenderID: "ou_user",
		Prompt:   "每小时提醒我喝水",
	})
	if err != nil {
		t.Fatalf("CreateFromMessage() error = %v", err)
	}
	if !ok {
		t.Fatal("CreateFromMessage() ok = false, want true")
	}
	if sched.TargetType != "p2p" || sched.TargetID != "ou_user" {
		t.Fatalf("unexpected schedule target: %+v", sched)
	}
	if sched.CronExpr != "0 * * * *" {
		t.Fatalf("CronExpr = %q, want hourly", sched.CronExpr)
	}
}

func TestService_CreateFromMessage_LLMComplexTime(t *testing.T) {
	database := openTestDB(t)
	cfg := &config.Config{Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}}}
	svc, err := NewService(cfg, database, &fakeExecutor{
		result: &claude.ExecuteResult{
			Text: `{"should_create":true,"name":"每天09:30喝水提醒","description":"喝水","cron_expr":"30 9 * * *","command":"提醒我喝水"}`,
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()

	sched, ok, err := svc.CreateFromMessage(context.Background(), &cfg.Apps[0], &feishu.IncomingMessage{
		AppID:    "app1",
		ChatType: "p2p",
		ChatID:   "oc_chat",
		SenderID: "ou_user",
		Prompt:   "每天早上9点半提醒我喝水",
	})
	if err != nil {
		t.Fatalf("CreateFromMessage() error = %v", err)
	}
	if !ok {
		t.Fatal("CreateFromMessage() ok = false, want true")
	}
	if sched.CronExpr != "30 9 * * *" {
		t.Fatalf("CronExpr = %q, want 30 9 * * *", sched.CronExpr)
	}
}

func TestService_CreateFromMessage_NoFallbackWhenLLMInvalid(t *testing.T) {
	database := openTestDB(t)
	cfg := &config.Config{Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}}}
	svc, err := NewService(cfg, database, &fakeExecutor{
		result: &claude.ExecuteResult{Text: "not json"},
	}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()

	sched, ok, err := svc.CreateFromMessage(context.Background(), &cfg.Apps[0], &feishu.IncomingMessage{
		AppID:    "app1",
		ChatType: "p2p",
		ChatID:   "oc_chat",
		SenderID: "ou_user",
		Prompt:   "每小时提醒我喝水",
	})
	if err != nil {
		t.Fatalf("CreateFromMessage() error = %v", err)
	}
	if ok {
		t.Fatal("CreateFromMessage() ok = true, want false")
	}
	if sched != nil {
		t.Fatalf("schedule should be nil when llm parse invalid, got: %+v", sched)
	}
}

func TestService_ManageFromMessage_DeleteSingleScheduleViaLLM(t *testing.T) {
	database := openTestDB(t)
	now := time.Now()
	if err := database.Create(&model.Schedule{
		ID:        "sched-1",
		AppID:     "app1",
		Name:      "喝水提醒",
		CronExpr:  "* * * * *",
		TargetType:"p2p",
		TargetID:  "ou_user",
		Command:   "提醒我喝水",
		Enabled:   true,
		CreatedBy: "ou_user",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	cfg := &config.Config{Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}}}
	svc, err := NewService(cfg, database, &fakeExecutor{
		result: &claude.ExecuteResult{
			Text: `{"action":"delete","keyword":"","new_prompt":""}`,
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()

	reply, ok, err := svc.ManageFromMessage(context.Background(), &cfg.Apps[0], &feishu.IncomingMessage{
		AppID:    "app1",
		SenderID: "ou_user",
		Prompt:   "关掉定时任务",
	})
	if err != nil {
		t.Fatalf("ManageFromMessage() error = %v", err)
	}
	if !ok {
		t.Fatal("ManageFromMessage() ok = false, want true")
	}
	if !strings.Contains(reply, "已删除") || !strings.Contains(reply, "喝水提醒") {
		t.Fatalf("reply = %q, want delete confirmation", reply)
	}

	var count int64
	if err := database.Model(&model.Schedule{}).Where("app_id = ?", "app1").Count(&count).Error; err != nil {
		t.Fatalf("count schedules: %v", err)
	}
	if count != 0 {
		t.Fatalf("schedule count = %d, want 0", count)
	}
}

func TestService_ManageFromMessage_DeleteWithoutKeywordDisambiguates(t *testing.T) {
	database := openTestDB(t)
	now := time.Now()
	for i, name := range []string{"喝水提醒", "开会提醒"} {
		if err := database.Create(&model.Schedule{
			ID:        fmt.Sprintf("sched-%d", i+1),
			AppID:     "app1",
			Name:      name,
			CronExpr:  "* * * * *",
			TargetType:"p2p",
			TargetID:  "ou_user",
			Command:   name,
			Enabled:   true,
			CreatedBy: "ou_user",
			CreatedAt: now,
			UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("create schedule %s: %v", name, err)
		}
	}
	cfg := &config.Config{Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}}}
	svc, err := NewService(cfg, database, &fakeExecutor{
		result: &claude.ExecuteResult{
			Text: `{"action":"delete","keyword":"","new_prompt":""}`,
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()

	reply, ok, err := svc.ManageFromMessage(context.Background(), &cfg.Apps[0], &feishu.IncomingMessage{
		AppID:    "app1",
		SenderID: "ou_user",
		Prompt:   "关掉定时任务",
	})
	if err != nil {
		t.Fatalf("ManageFromMessage() error = %v", err)
	}
	if !ok {
		t.Fatal("ManageFromMessage() ok = false, want true")
	}
	if !strings.Contains(reply, "我找到多条提醒") || !strings.Contains(reply, "喝水提醒") || !strings.Contains(reply, "开会提醒") {
		t.Fatalf("reply = %q, want disambiguation list", reply)
	}
}

func TestService_BootstrapRegistersEnabledSchedules(t *testing.T) {
	database := openTestDB(t)
	now := time.Now()
	if err := database.Create(&model.Schedule{
		ID:        "sched-1",
		AppID:     "app1",
		Name:      "test",
		CronExpr:  "0 * * * *",
		TargetType:"p2p",
		TargetID:  "ou_user",
		Command:   "提醒我喝水",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	cfg := &config.Config{Apps: []config.AppConfig{{ID: "app1", WorkspaceDir: t.TempDir()}}}
	svc, err := NewService(cfg, database, &fakeExecutor{}, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	defer svc.Stop()
	if err := svc.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(svc.jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(svc.jobs))
	}
}
