package config

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config is the top-level application configuration.
type Config struct {
	Apps      []AppConfig     `mapstructure:"apps"`
	Server    ServerConfig    `mapstructure:"server"`
	Claude    ClaudeConfig    `mapstructure:"claude"`
	Session   SessionConfig   `mapstructure:"session"`
	Cleanup   CleanupConfig   `mapstructure:"cleanup"`
	Heartbeat HeartbeatConfig `mapstructure:"heartbeat"`
	WebSearch WebSearchConfig `mapstructure:"web_search"`
	// Language selects the workspace template language. Supported: "zh", "en". Defaults to "zh".
	Language string `mapstructure:"language"`
	// DBPath is the absolute path to bot.db, set at runtime after db.Open().
	// Not read from YAML.
	DBPath string `mapstructure:"-"`
	// ConfigPath is the loaded config.yaml path, set at runtime after Load().
	// Not read from YAML.
	ConfigPath string `mapstructure:"-"`
}

// AppConfig represents one Feishu application + its workspace.
type AppConfig struct {
	ID                      string `mapstructure:"id"`
	FeishuAppID             string `mapstructure:"feishu_app_id"`
	FeishuAppSecret         string `mapstructure:"feishu_app_secret"`
	FeishuVerificationToken string `mapstructure:"feishu_verification_token"`
	FeishuEncryptKey        string `mapstructure:"feishu_encrypt_key"`
	WorkspaceDir            string `mapstructure:"workspace_dir"`
	// WorkspaceMode controls reply presentation.
	// "work" (default): show progress/thinking cards when the chat type allows it.
	// "companion": hide visible progress and reply directly with the final text.
	WorkspaceMode string `mapstructure:"workspace_mode"`
	// WorkspaceTemplate selects the initial embedded workspace template.
	// "default" is the generic assistant template.
	// "product-assistant" is a seeded demo template for product workflow demos.
	WorkspaceTemplate string          `mapstructure:"workspace_template"`
	AllowedChats      []string        `mapstructure:"allowed_chats"`
	Claude            AppClaudeConfig `mapstructure:"claude"`
}

func (a *AppConfig) NormalizedWorkspaceMode() string {
	switch a.WorkspaceMode {
	case "companion":
		return "companion"
	default:
		return "work"
	}
}

func NormalizeWorkspaceMode(mode string) (string, bool) {
	switch mode {
	case "work":
		return "work", true
	case "comp", "companion":
		return "companion", true
	default:
		return "", false
	}
}

func (a *AppConfig) NormalizedWorkspaceTemplate() string {
	switch a.WorkspaceTemplate {
	case "product-assistant":
		return "product-assistant"
	case "code-review":
		return "code-review"
	default:
		return "default"
	}
}

// AllowedChat returns true if the given chat ID is allowed (empty list = all allowed).
func (a *AppConfig) AllowedChat(chatID string) bool {
	if len(a.AllowedChats) == 0 {
		return true
	}
	for _, id := range a.AllowedChats {
		if id == chatID {
			return true
		}
	}
	return false
}

// AppClaudeConfig holds per-app Claude CLI settings.
type AppClaudeConfig struct {
	PermissionMode string   `mapstructure:"permission_mode"`
	AllowedTools   []string `mapstructure:"allowed_tools"`
	// Provider selects which provider from claude.providers to use for this app.
	// Empty means use claude.default_provider.
	Provider string `mapstructure:"provider"`
	// Model overrides the provider's default model for this app.
	// Accepts aliases (sonnet, opus, haiku), full model IDs (claude-sonnet-4-6),
	// or third-party model names (qwen-plus, kimi-k2.5).
	// Empty means use the provider's configured default model.
	Model string `mapstructure:"model"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int `mapstructure:"port"`
}

// ClaudeConfig holds global Claude CLI settings.
type ClaudeConfig struct {
	TimeoutMinutes int `mapstructure:"timeout_minutes"`
	MaxTurns       int `mapstructure:"max_turns"`
	// DefaultProvider selects which provider to use when an app doesn't specify one.
	// Must match a key in Providers. Defaults to "anthropic" if empty.
	DefaultProvider string `mapstructure:"default_provider"`
	// Providers defines available model providers, keyed by name.
	// Example keys: "anthropic", "bailian".
	Providers map[string]ProviderConfig `mapstructure:"providers"`
}

// ProviderConfig holds connection details for a single model provider.
type ProviderConfig struct {
	// BaseURL is the API endpoint. For "anthropic", leave empty to use claude CLI default.
	// For "bailian", defaults to https://coding.dashscope.aliyuncs.com/apps/anthropic if empty.
	BaseURL string `mapstructure:"base_url"`
	// AuthToken is the API key. For "anthropic", leave empty to use claude CLI's own auth.
	AuthToken string `mapstructure:"auth_token"`
	// Model is the default model for this provider.
	// Accepts aliases (sonnet, opus, haiku), full IDs, or third-party names.
	Model string `mapstructure:"model"`
}

// SessionConfig holds session worker settings.
type SessionConfig struct {
	WorkerIdleTimeoutMinutes int                `mapstructure:"worker_idle_timeout_minutes"`
	Probe                    SessionProbeConfig `mapstructure:"probe"`
}

type SessionProbeConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// CleanupConfig holds attachment cleanup settings.
type CleanupConfig struct {
	AttachmentsRetentionDays int    `mapstructure:"attachments_retention_days"`
	AttachmentsMaxDays       int    `mapstructure:"attachments_max_days"`
	Schedule                 string `mapstructure:"schedule"`
}

type HeartbeatConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	IntervalMinutes  int    `mapstructure:"interval_minutes"`
	PromptFile       string `mapstructure:"prompt_file"`
	NotifyTargetType string `mapstructure:"notify_target_type"` // p2p / group
	NotifyTargetID   string `mapstructure:"notify_target_id"`   // open_id or chat_id
}

type WebSearchConfig struct {
	TavilyAPIKey   string `mapstructure:"tavily_api_key"`
	TavilyBaseURL  string `mapstructure:"tavily_base_url"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

// Validate checks that all required fields are populated.
// Call this immediately after Load to catch misconfiguration at startup.
func (c *Config) Validate() error {
	if len(c.Apps) == 0 {
		return fmt.Errorf("config: at least one app must be defined")
	}
	for _, app := range c.Apps {
		if app.ID == "" {
			return fmt.Errorf("config: app is missing 'id'")
		}
		if app.FeishuAppID == "" {
			return fmt.Errorf("config: app %q is missing 'feishu_app_id'", app.ID)
		}
		if app.FeishuAppSecret == "" {
			return fmt.Errorf("config: app %q is missing 'feishu_app_secret'", app.ID)
		}
		if app.WorkspaceDir == "" {
			return fmt.Errorf("config: app %q is missing 'workspace_dir'", app.ID)
		}
	}
	return nil
}

var (
	heartbeatChangeCbs []func(HeartbeatConfig)
	heartbeatMu        sync.RWMutex
)

// RegisterHeartbeatChangeCallback registers a callback invoked when the
// heartbeat section of the config file changes at runtime.
func RegisterHeartbeatChangeCallback(fn func(HeartbeatConfig)) {
	heartbeatMu.Lock()
	defer heartbeatMu.Unlock()
	heartbeatChangeCbs = append(heartbeatChangeCbs, fn)
}

func notifyHeartbeatChange(cfg HeartbeatConfig) {
	heartbeatMu.RLock()
	cbs := make([]func(HeartbeatConfig), len(heartbeatChangeCbs))
	copy(cbs, heartbeatChangeCbs)
	heartbeatMu.RUnlock()
	for _, fn := range cbs {
		fn(cfg)
	}
}

// Load reads the YAML config file at the given path and optionally watches it.
func Load(path string, watch bool) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("claude.timeout_minutes", 5)
	v.SetDefault("claude.max_turns", 20)
	v.SetDefault("session.worker_idle_timeout_minutes", 30)
	v.SetDefault("session.probe.enabled", false)
	v.SetDefault("cleanup.attachments_retention_days", 7)
	v.SetDefault("cleanup.attachments_max_days", 30)
	v.SetDefault("cleanup.schedule", "0 2 * * *")
	v.SetDefault("heartbeat.enabled", false)
	v.SetDefault("heartbeat.interval_minutes", 30)
	v.SetDefault("heartbeat.prompt_file", "HEARTBEAT.md")
	v.SetDefault("heartbeat.notify_target_type", "")
	v.SetDefault("heartbeat.notify_target_id", "")
	v.SetDefault("web_search.tavily_api_key", "")
	v.SetDefault("web_search.tavily_base_url", "https://api.tavily.com/search")
	v.SetDefault("web_search.timeout_seconds", 15)
	v.SetDefault("language", "zh")
	v.SetDefault("apps", []map[string]any{})

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.ConfigPath = path
	for i := range cfg.Apps {
		if cfg.Apps[i].WorkspaceMode == "" {
			cfg.Apps[i].WorkspaceMode = "work"
		}
		if cfg.Apps[i].WorkspaceTemplate == "" {
			cfg.Apps[i].WorkspaceTemplate = "default"
		}
	}

	if watch {
		v.WatchConfig()
		v.OnConfigChange(func(in fsnotify.Event) {
			slog.Info("config file changed", "path", in.Name)
			var newCfg Config
			if err := v.Unmarshal(&newCfg); err != nil {
				slog.Error("reload config failed", "err", err)
				return
			}
			if newCfg.Heartbeat != cfg.Heartbeat {
				slog.Info("heartbeat config changed",
					"old_enabled", cfg.Heartbeat.Enabled,
					"old_interval", cfg.Heartbeat.IntervalMinutes,
					"new_enabled", newCfg.Heartbeat.Enabled,
					"new_interval", newCfg.Heartbeat.IntervalMinutes)
				cfg.Heartbeat = newCfg.Heartbeat
				notifyHeartbeatChange(cfg.Heartbeat)
			}
		})
	}

	return &cfg, nil
}

func (c *Config) SetAppWorkspaceMode(appID, mode string) error {
	normalized, ok := NormalizeWorkspaceMode(mode)
	if !ok {
		return fmt.Errorf("invalid workspace mode %q", mode)
	}
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.ConfigPath == "" {
		return fmt.Errorf("config path is empty")
	}
	if err := updateAppWorkspaceModeInFile(c.ConfigPath, appID, normalized); err != nil {
		return err
	}
	for i := range c.Apps {
		if c.Apps[i].ID == appID {
			c.Apps[i].WorkspaceMode = normalized
			return nil
		}
	}
	return fmt.Errorf("app %q not found", appID)
}

func updateAppWorkspaceModeInFile(path, appID, mode string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse config yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return fmt.Errorf("config yaml is empty")
	}

	appNode, err := findAppNodeByID(root.Content[0], appID)
	if err != nil {
		return err
	}
	setMappingValue(appNode, "workspace_mode", mode)

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshal config yaml: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func findAppNodeByID(root *yaml.Node, appID string) (*yaml.Node, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config yaml root must be a mapping")
	}
	appsNode := mappingValue(root, "apps")
	if appsNode == nil || appsNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("config yaml missing apps sequence")
	}
	for _, item := range appsNode.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if idNode := mappingValue(item, "id"); idNode != nil && idNode.Value == appID {
			return item, nil
		}
	}
	return nil, fmt.Errorf("app %q not found in config file", appID)
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Kind = yaml.ScalarNode
			node.Content[i+1].Tag = "!!str"
			node.Content[i+1].Value = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}
