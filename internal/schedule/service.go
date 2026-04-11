package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/feishu"
	"github.com/ashwinyue/Memknow/internal/model"
)

type Sender interface {
	SendText(ctx context.Context, receiveID string, receiveIDType string, text string) (string, error)
}

type Defaults struct {
	AppID      string
	TargetType string
	TargetID   string
	CreatedBy  string
}

type Intent struct {
	Name        string
	Description string
	CronExpr    string
	Command     string
}

type CreateInput struct {
	AppID       string
	Name        string
	Description string
	CronExpr    string
	TargetType  string
	TargetID    string
	Command     string
	CreatedBy   string
}

type Service struct {
	cfg        *config.Config
	db         *gorm.DB
	executor   claude.ExecutorInterface
	senders    map[string]Sender
	apps       map[string]*config.AppConfig
	scheduler  gocron.Scheduler
	mu         sync.Mutex
	jobs       map[string]gocron.Job
	systemJobs map[string]gocron.Job
}

func NewService(cfg *config.Config, db *gorm.DB, executor claude.ExecutorInterface, senders map[string]Sender) (*Service, error) {
	inner, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}
	apps := make(map[string]*config.AppConfig, len(cfg.Apps))
	for i := range cfg.Apps {
		apps[cfg.Apps[i].ID] = &cfg.Apps[i]
	}
	inner.Start()
	return &Service{
		cfg:       cfg,
		db:        db,
		executor:  executor,
		senders:   senders,
		apps:      apps,
		scheduler: inner,
		jobs:      make(map[string]gocron.Job),
		systemJobs: make(map[string]gocron.Job),
	}, nil
}

func (s *Service) Stop() {
	if s.scheduler != nil {
		_ = s.scheduler.Shutdown()
	}
}

// AddSystemJob registers a cron job not tied to a persisted business schedule.
func (s *Service) AddSystemJob(name, cronExpr string, fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if old, ok := s.systemJobs[name]; ok {
		_ = s.scheduler.RemoveJob(old.ID())
		delete(s.systemJobs, name)
	}
	job, err := s.scheduler.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(fn),
		gocron.WithName(name),
	)
	if err != nil {
		return fmt.Errorf("register system job %s: %w", name, err)
	}
	s.systemJobs[name] = job
	return nil
}

func (s *Service) Bootstrap(ctx context.Context) error {
	var schedules []model.Schedule
	if err := s.db.Where("enabled = ?", true).Find(&schedules).Error; err != nil {
		return err
	}
	for i := range schedules {
		if err := s.scheduleOne(ctx, &schedules[i]); err != nil {
			return err
		}
	}
	return nil
}

func ResolveDefaults(msg *feishu.IncomingMessage) Defaults {
	d := Defaults{
		AppID:     msg.AppID,
		CreatedBy: msg.SenderID,
	}
	if msg.ChatType == "p2p" {
		d.TargetType = "p2p"
		d.TargetID = msg.SenderID
		return d
	}
	d.TargetType = "group"
	d.TargetID = msg.ChatID
	return d
}

func (s *Service) CreateFromMessage(ctx context.Context, appCfg *config.AppConfig, msg *feishu.IncomingMessage) (*model.Schedule, bool, error) {
	prompt := strings.TrimSpace(msg.Prompt)
	if !looksLikeScheduleIntent(prompt) {
		return nil, false, nil
	}

	intent, ok, err := s.parseIntentWithLLM(ctx, appCfg, prompt)
	if err != nil {
		slog.Warn("schedule: llm intent parse failed", "err", err)
		return nil, false, nil
	}
	if !ok {
		return nil, false, nil
	}
	def := ResolveDefaults(msg)
	sched, err := s.Create(ctx, CreateInput{
		AppID:       appCfg.ID,
		Name:        intent.Name,
		Description: intent.Description,
		CronExpr:    intent.CronExpr,
		TargetType:  def.TargetType,
		TargetID:    def.TargetID,
		Command:     intent.Command,
		CreatedBy:   def.CreatedBy,
	})
	if err != nil {
		return nil, true, err
	}
	return sched, true, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (*model.Schedule, error) {
	now := time.Now()
	sched := &model.Schedule{
		ID:          uuid.New().String(),
		AppID:       input.AppID,
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		CronExpr:    strings.TrimSpace(input.CronExpr),
		TargetType:  strings.TrimSpace(input.TargetType),
		TargetID:    strings.TrimSpace(input.TargetID),
		Command:     strings.TrimSpace(input.Command),
		Enabled:     true,
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.Create(sched).Error; err != nil {
		return nil, err
	}
	if err := s.scheduleOne(ctx, sched); err != nil {
		return nil, err
	}
	return sched, nil
}

func (s *Service) scheduleOne(ctx context.Context, sched *model.Schedule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.jobs[sched.ID]; ok {
		_ = s.scheduler.RemoveJob(old.ID())
		delete(s.jobs, sched.ID)
	}
	job, err := s.scheduler.NewJob(
		gocron.CronJob(sched.CronExpr, false),
		gocron.NewTask(func() {
			if err := s.runNow(context.WithoutCancel(ctx), sched); err != nil {
				slog.Error("schedule run failed", "schedule_id", sched.ID, "err", err)
			}
		}),
		gocron.WithName(sched.Name),
	)
	if err != nil {
		return err
	}
	s.jobs[sched.ID] = job
	return nil
}

func (s *Service) runNow(ctx context.Context, sched *model.Schedule) error {
	appCfg, ok := s.apps[sched.AppID]
	if !ok {
		return fmt.Errorf("unknown app: %s", sched.AppID)
	}
	channelKey := feishu.BuildChannelKey(sched.TargetType, sched.TargetID, "", appCfg.ID)
	s.ensureChannel(channelKey, sched, appCfg.ID)

	sessionID := uuid.New().String()
	now := time.Now()
	s.createSession(sessionID, channelKey, sched.CreatedBy, now)

	logRow := &model.ScheduleLog{
		ID:         uuid.New().String(),
		ScheduleID: sched.ID,
		SessionID:  sessionID,
		Status:     "ok",
		StartedAt:  now,
	}
	if err := s.db.Create(logRow).Error; err != nil {
		slog.Warn("schedule: create log row failed", "schedule_id", sched.ID, "session_id", sessionID, "err", err)
	}

	result, err := s.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:       sched.Command,
		SessionID:    sessionID,
		SessionType:  model.SessionTypeSchedule,
		AppConfig:    appCfg,
		WorkspaceDir: appCfg.WorkspaceDir,
		ChannelKey:   channelKey,
		SenderID:     sched.CreatedBy,
	})
	if err != nil {
		s.finishLog(logRow, "error", "", err.Error())
		s.archiveSession(sessionID)
		return err
	}
	if ie, ok := s.executor.(*claude.InteractiveExecutor); ok {
		ie.RemoveSession(sessionID)
	}
	if result.Text != "" {
		if sender, ok := s.senders[sched.AppID]; ok && sender != nil {
			receiveType := feishu.ReceiveIDType(sched.TargetType, sched.TargetID)
			if _, sendErr := sender.SendText(ctx, sched.TargetID, receiveType, result.Text); sendErr != nil {
				slog.Error("schedule send failed", "schedule_id", sched.ID, "err", sendErr)
			}
		}
	}
	if err := s.db.Model(&model.Schedule{}).Where("id = ?", sched.ID).Updates(map[string]any{
		"last_run_at": now,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		slog.Warn("schedule: update schedule last_run_at failed", "schedule_id", sched.ID, "err", err)
	}
	s.finishLog(logRow, "ok", result.Text, "")
	s.archiveSession(sessionID)
	return nil
}

func (s *Service) finishLog(logRow *model.ScheduleLog, status, resultText, errText string) {
	completed := time.Now()
	if err := s.db.Model(&model.ScheduleLog{}).Where("id = ?", logRow.ID).Updates(map[string]any{
		"status":        status,
		"result_text":   resultText,
		"error_message": errText,
		"completed_at":  &completed,
	}).Error; err != nil {
		slog.Warn("schedule: finish log update failed", "log_id", logRow.ID, "status", status, "err", err)
	}
}

func (s *Service) ensureChannel(channelKey string, sched *model.Schedule, appID string) {
	ch := model.Channel{
		ChannelKey: channelKey,
		AppID:      appID,
		ChatType:   sched.TargetType,
		ChatID:     sched.TargetID,
	}
	if err := s.db.Where("channel_key = ?", channelKey).FirstOrCreate(&ch).Error; err != nil {
		slog.Warn("schedule: ensure channel failed", "channel_key", channelKey, "err", err)
	}
}

func (s *Service) createSession(sessionID, channelKey, createdBy string, now time.Time) {
	if err := s.db.Create(&model.Session{
		ID:         sessionID,
		ChannelKey: channelKey,
		Type:       model.SessionTypeSchedule,
		Status:     "active",
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}).Error; err != nil {
		slog.Warn("schedule: create session failed", "session_id", sessionID, "err", err)
	}
}

func (s *Service) archiveSession(sessionID string) {
	if err := s.db.Model(&model.Session{}).Where("id = ?", sessionID).Updates(map[string]any{
		"status":     "archived",
		"updated_at": time.Now(),
	}).Error; err != nil {
		slog.Warn("schedule: archive session failed", "session_id", sessionID, "err", err)
	}
}

func looksLikeScheduleIntent(prompt string) bool {
	p := strings.ToLower(prompt)
	// 明确提到 heartbeat/心跳 的请求应交给 agent 处理（修改 HEARTBEAT.md），
	// 而不是走 schedule 自动创建。
	if strings.Contains(p, "heartbeat") || strings.Contains(p, "心跳") {
		return false
	}
	keywords := []string{"提醒", "定时", "每小时", "每天", "每周", "每月"}
	for _, kw := range keywords {
		if strings.Contains(p, kw) {
			return true
		}
	}
	return false
}

func summarizeCommand(command string) string {
	command = strings.TrimSpace(command)
	command = strings.TrimSuffix(command, "。")
	command = strings.TrimSuffix(command, "！")
	command = strings.TrimSuffix(command, "!")
	if command == "" {
		return "事项"
	}
	return command
}

// ListByApp returns enabled schedules for an app.
func (s *Service) ListByApp(ctx context.Context, appID string) ([]model.Schedule, error) {
	var items []model.Schedule
	if err := s.db.Where("app_id = ? AND enabled = ?", appID, true).Order("created_at desc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListByCreator returns enabled schedules created by a specific user.
func (s *Service) ListByCreator(ctx context.Context, appID, createdBy string) ([]model.Schedule, error) {
	var items []model.Schedule
	if err := s.db.Where("app_id = ? AND created_by = ? AND enabled = ?", appID, createdBy, true).Order("created_at desc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// Delete removes a schedule and its registered job.
func (s *Service) Delete(ctx context.Context, scheduleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[scheduleID]; ok {
		_ = s.scheduler.RemoveJob(job.ID())
		delete(s.jobs, scheduleID)
	}
	return s.db.Where("id = ?", scheduleID).Delete(&model.Schedule{}).Error
}

// Update updates mutable fields of a schedule and reschedules the job.
func (s *Service) Update(ctx context.Context, scheduleID string, updates map[string]any) error {
	if err := s.db.Model(&model.Schedule{}).Where("id = ?", scheduleID).Updates(updates).Error; err != nil {
		return err
	}
	var sched model.Schedule
	if err := s.db.Where("id = ?", scheduleID).First(&sched).Error; err != nil {
		return err
	}
	if !sched.Enabled {
		s.mu.Lock()
		if job, ok := s.jobs[scheduleID]; ok {
			_ = s.scheduler.RemoveJob(job.ID())
			delete(s.jobs, scheduleID)
		}
		s.mu.Unlock()
		return nil
	}
	return s.scheduleOne(ctx, &sched)
}

// ManageFromMessage handles natural-language schedule management (list, delete, etc.).
func (s *Service) ManageFromMessage(ctx context.Context, appCfg *config.AppConfig, msg *feishu.IncomingMessage) (string, bool, error) {
	mi, ok, err := s.parseManageIntentWithLLM(ctx, appCfg, msg.Prompt)
	if err != nil {
		slog.Warn("schedule: llm manage parse failed", "err", err)
		mi, ok = ParseManageIntent(msg.Prompt)
	}
	if !ok {
		return "", false, nil
	}
	switch mi.Action {
	case "list":
		items, err := s.ListByCreator(ctx, appCfg.ID, msg.SenderID)
		if err != nil {
			return "", true, err
		}
		if len(items) == 0 {
			return "📭 你还没有创建定时提醒", true, nil
		}
		var sb strings.Builder
		sb.WriteString("📋 你的定时提醒：\n")
		for i, it := range items {
			sb.WriteString(fmt.Sprintf("%d. %s (`%s`)\n", i+1, it.Name, it.CronExpr))
		}
		return sb.String(), true, nil
	case "delete":
		items, err := s.ListByCreator(ctx, appCfg.ID, msg.SenderID)
		if err != nil {
			return "", true, err
		}
		if len(items) == 0 {
			return "📭 你还没有创建定时提醒", true, nil
		}
		matched, disambiguation := resolveScheduleForManagement(items, mi.Keyword, "delete")
		if disambiguation != "" {
			return disambiguation, true, nil
		}
		if matched == nil {
			return fmt.Sprintf("❌ 没找到包含「%s」的提醒，请检查名称", mi.Keyword), true, nil
		}
		if err := s.Delete(ctx, matched.ID); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("✅ 已删除「%s」", matched.Name), true, nil
	case "update":
		items, err := s.ListByCreator(ctx, appCfg.ID, msg.SenderID)
		if err != nil {
			return "", true, err
		}
		if len(items) == 0 {
			return "📭 你还没有创建定时提醒", true, nil
		}
		matched, disambiguation := resolveScheduleForManagement(items, mi.Keyword, "update")
		if disambiguation != "" {
			return disambiguation, true, nil
		}
		if matched == nil {
			return fmt.Sprintf("❌ 没找到包含「%s」的提醒，请检查名称", mi.Keyword), true, nil
		}
		intent, ok, err := s.parseIntentWithLLM(ctx, appCfg, mi.NewPrompt)
		if err != nil || !ok {
			return "❌ 没听懂新的时间设定，请用类似「每天10点」或「每小时」这样的表达", true, nil
		}
		updates := map[string]any{
			"cron_expr":  intent.CronExpr,
			"command":    intent.Command,
			"updated_at": time.Now(),
		}
		if err := s.Update(ctx, matched.ID, updates); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("✅ 已将「%s」更新为 `%s`", matched.Name, intent.CronExpr), true, nil
	default:
		return "", false, nil
	}
}

func matchScheduleByKeyword(items []model.Schedule, keyword string) *model.Schedule {
	lowerKW := strings.ToLower(keyword)
	// exact substring match first
	for i := range items {
		if strings.Contains(strings.ToLower(items[i].Name), lowerKW) {
			return &items[i]
		}
	}
	// fallback: keyword containment
	for i := range items {
		if strings.Contains(lowerKW, strings.ToLower(items[i].Name)) {
			return &items[i]
		}
	}
	return nil
}

func resolveScheduleForManagement(items []model.Schedule, keyword, action string) (*model.Schedule, string) {
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		return matchScheduleByKeyword(items, keyword), ""
	}
	if len(items) == 1 {
		return &items[0], ""
	}
	var sb strings.Builder
	sb.WriteString("我找到多条提醒，请明确说要")
	if action == "delete" {
		sb.WriteString("删除")
	} else {
		sb.WriteString("修改")
	}
	sb.WriteString("哪一条：\n")
	for i, it := range items {
		sb.WriteString(fmt.Sprintf("%d. %s (`%s`)\n", i+1, it.Name, it.CronExpr))
	}
	return nil, sb.String()
}

// ManageIntent represents a natural-language management command.
type ManageIntent struct {
	Action    string // list | delete | update
	Keyword   string // for delete/update matching
	NewPrompt string // for update: the new time description
}

// ParseManageIntent detects schedule-management intent in user messages.
func ParseManageIntent(prompt string) (ManageIntent, bool) {
	p := strings.ToLower(strings.TrimSpace(prompt))
	listPatterns := []string{"我的提醒", "我的定时", "查看提醒", "查看定时", "有哪些提醒", "有哪些定时", "列出提醒", "列出定时"}
	for _, pat := range listPatterns {
		if strings.Contains(p, pat) {
			return ManageIntent{Action: "list"}, true
		}
	}
	updateTriggers := []string{"改成", "改为", "修改成", "更新为", "调整为", "变成"}
	for _, pat := range updateTriggers {
		if strings.Contains(p, pat) {
			parts := strings.SplitN(p, pat, 2)
			before := strings.TrimSpace(parts[0])
			after := strings.TrimSpace(parts[1])
			kw := extractKeywordBeforeUpdate(before)
			if kw != "" && looksLikeScheduleIntent(after) {
				return ManageIntent{Action: "update", Keyword: kw, NewPrompt: after}, true
			}
		}
	}
	deletePatterns := []string{"删除", "删掉", "移除", "取消"}
	for _, pat := range deletePatterns {
		if strings.Contains(p, pat) {
			// extract the noun phrase after the delete keyword as keyword
			kw := extractKeywordAfter(p, pat)
			if kw != "" {
				return ManageIntent{Action: "delete", Keyword: kw}, true
			}
		}
	}
	return ManageIntent{}, false
}

func extractKeywordAfter(prompt, trigger string) string {
	idx := strings.Index(prompt, trigger)
	if idx == -1 {
		return ""
	}
	after := strings.TrimSpace(prompt[idx+len(trigger):])
	// strip common prefixes
	after = strings.TrimPrefix(after, "我的")
	after = strings.TrimPrefix(after, "这个")
	after = strings.TrimPrefix(after, "那条")
	after = strings.TrimPrefix(after, "该")
	after = strings.TrimSpace(after)
	// remove trailing punctuation
	after = strings.TrimRight(after, "。！?？")
	return after
}

func extractKeywordBeforeUpdate(before string) string {
	before = strings.TrimSpace(before)
	prefixes := []string{"把", "将", "修改", "调整", "更新", "我的", "这个", "那条", "该"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(before, prefix) {
			before = strings.TrimPrefix(before, prefix)
			before = strings.TrimSpace(before)
		}
	}
	before = strings.TrimSuffix(before, "的")
	before = strings.TrimSuffix(before, "时间")
	before = strings.TrimSuffix(before, "内容")
	before = strings.TrimSuffix(before, "命令")
	before = strings.TrimSpace(before)
	return before
}
