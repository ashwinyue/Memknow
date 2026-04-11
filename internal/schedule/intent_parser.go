package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/ashwinyue/Memknow/internal/claude"
	"github.com/ashwinyue/Memknow/internal/config"
	"github.com/ashwinyue/Memknow/internal/model"
)

type llmIntentResponse struct {
	ShouldCreate bool   `json:"should_create"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	CronExpr     string `json:"cron_expr"`
	Command      string `json:"command"`
}

type llmManageIntentResponse struct {
	Action    string `json:"action"`
	Keyword   string `json:"keyword"`
	NewPrompt string `json:"new_prompt"`
}

var firstJSONObjectRe = regexp.MustCompile(`(?s)\{.*\}`)
var cronExprFieldRe = regexp.MustCompile(`^[0-9*/,\-]+$`)

// parseIntentWithLLM asks Claude to convert natural language schedule requests
// into a strict JSON intent. It returns ok=false when the intent should not be
// auto-created.
func (s *Service) parseIntentWithLLM(ctx context.Context, appCfg *config.AppConfig, prompt string) (Intent, bool, error) {
	if s == nil || s.executor == nil || appCfg == nil {
		return Intent{}, false, fmt.Errorf("llm parser unavailable")
	}

	parsePrompt := fmt.Sprintf(`你是一个定时任务解析器。请把用户输入解析成 JSON，不要输出任何额外文字。

输出格式（严格 JSON）：
{
  "should_create": true/false,
  "name": "任务名称",
  "description": "任务描述",
  "cron_expr": "5段 cron 表达式",
  "command": "执行命令文本"
}

规则：
1. 如果用户明确在请求创建提醒/定时任务，should_create=true；否则 false。
2. cron_expr 必须是标准 5 段（分 时 日 月 周），例如 "0 * * * *"。
3. name 和 command 要简洁明确；command 不要为空。
4. 如果无法确定时间表达式，should_create=false，其他字段给空字符串。
5. 只输出 JSON。

用户输入：
%s`, strings.TrimSpace(prompt))

	result, err := s.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:       parsePrompt,
		SessionID:    "schedule-intent-" + uuid.New().String(),
		SessionType:  model.SessionTypeSchedule,
		AppConfig:    appCfg,
		WorkspaceDir: appCfg.WorkspaceDir,
		ChannelKey:   "schedule:intent:" + appCfg.ID,
	})
	if err != nil {
		return Intent{}, false, err
	}

	raw := extractFirstJSONObject(result.Text)
	if raw == "" {
		return Intent{}, false, fmt.Errorf("llm intent parser returned non-json")
	}

	var out llmIntentResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Intent{}, false, fmt.Errorf("decode llm intent json: %w", err)
	}
	if !out.ShouldCreate {
		return Intent{}, false, nil
	}

	intent := Intent{
		Name:        strings.TrimSpace(out.Name),
		Description: strings.TrimSpace(out.Description),
		CronExpr:    strings.TrimSpace(out.CronExpr),
		Command:     strings.TrimSpace(out.Command),
	}
	if intent.Command == "" {
		return Intent{}, false, fmt.Errorf("llm intent missing command")
	}
	if intent.Name == "" {
		intent.Name = fmt.Sprintf("定时%s提醒", summarizeCommand(intent.Command))
	}
	if !isValidCronExpr(intent.CronExpr) {
		return Intent{}, false, fmt.Errorf("invalid cron_expr from llm: %q", intent.CronExpr)
	}
	if intent.Description == "" {
		intent.Description = intent.Command
	}
	return intent, true, nil
}

func (s *Service) parseManageIntentWithLLM(ctx context.Context, appCfg *config.AppConfig, prompt string) (ManageIntent, bool, error) {
	if s == nil || s.executor == nil || appCfg == nil {
		return ManageIntent{}, false, fmt.Errorf("llm parser unavailable")
	}

	parsePrompt := fmt.Sprintf(`你是一个定时任务管理意图解析器。请把用户输入解析成 JSON，不要输出任何额外文字。

输出格式（严格 JSON）：
{
  "action": "list|delete|update|none",
  "keyword": "要操作的提醒关键词，没有就给空字符串",
  "new_prompt": "仅 update 时填写新的时间表达，没有就给空字符串"
}

规则：
1. 查看提醒、列出提醒、看看有哪些提醒 → action=list。
2. 删除、取消、关掉、关闭、停掉、停用提醒 → action=delete。
3. 修改提醒时间/内容 → action=update。
4. 如果不是在管理定时任务，action=none。
5. keyword 尽量提取目标提醒名称；如果用户没有给具体名称，就留空。
6. update 时把新的时间表达放到 new_prompt，其余情况给空字符串。
7. 只输出 JSON。

用户输入：
%s`, strings.TrimSpace(prompt))

	result, err := s.executor.Execute(ctx, &claude.ExecuteRequest{
		Prompt:       parsePrompt,
		SessionID:    "schedule-manage-intent-" + uuid.New().String(),
		SessionType:  model.SessionTypeSchedule,
		AppConfig:    appCfg,
		WorkspaceDir: appCfg.WorkspaceDir,
		ChannelKey:   "schedule:manage-intent:" + appCfg.ID,
	})
	if err != nil {
		return ManageIntent{}, false, err
	}

	raw := extractFirstJSONObject(result.Text)
	if raw == "" {
		return ManageIntent{}, false, fmt.Errorf("llm manage parser returned non-json")
	}

	var out llmManageIntentResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return ManageIntent{}, false, fmt.Errorf("decode llm manage json: %w", err)
	}

	action := strings.ToLower(strings.TrimSpace(out.Action))
	switch action {
	case "list", "delete", "update":
		return ManageIntent{
			Action:    action,
			Keyword:   strings.TrimSpace(out.Keyword),
			NewPrompt: strings.TrimSpace(out.NewPrompt),
		}, true, nil
	case "none", "":
		return ManageIntent{}, false, nil
	default:
		return ManageIntent{}, false, fmt.Errorf("invalid manage action from llm: %q", out.Action)
	}
}

func extractFirstJSONObject(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	match := firstJSONObjectRe.FindString(trimmed)
	return strings.TrimSpace(match)
}

func isValidCronExpr(expr string) bool {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return false
	}
	for _, f := range fields {
		if !cronExprFieldRe.MatchString(f) {
			return false
		}
	}
	return true
}
