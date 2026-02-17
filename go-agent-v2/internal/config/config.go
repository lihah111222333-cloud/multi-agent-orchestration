// Package config 全局配置加载与管理。
//
// 所有字段通过 struct tag 声明环境变量映射:
//
//	`env:"VAR_NAME" default:"value" min:"0"`
//
// Load() 使用反射自动填充，无需手动逐行赋值。
package config

import (
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// Config 应用全局配置，字段名与 .env 变量一一对应。
type Config struct {
	// LLM
	LLMModel       string  `env:"LLM_MODEL" default:"gpt-4o"`
	LLMTemperature float64 `env:"LLM_TEMPERATURE" default:"0.7" min:"0"`
	OpenAIAPIKey   string  `env:"OPENAI_API_KEY"`
	OpenAIBaseURL  string  `env:"OPENAI_BASE_URL"`
	LLMTimeout     int     `env:"LLM_TIMEOUT" default:"120" min:"1"`
	LLMMaxRetries  int     `env:"LLM_MAX_RETRIES" default:"3" min:"0"`

	// Gateway
	GatewayTimeout         int `env:"GATEWAY_TIMEOUT" default:"240" min:"1"`
	GatewayMaxAttempts     int `env:"GATEWAY_MAX_ATTEMPTS" default:"2" min:"1"`
	CommandCardTimeoutSec  int `env:"COMMAND_CARD_TIMEOUT_SEC" default:"240" min:"1"`
	GatewayMinQualityScore int `env:"GATEWAY_MIN_QUALITY_SCORE" default:"25" min:"0"`

	// PostgreSQL
	PostgresConnStr        string `env:"POSTGRES_CONNECTION_STRING"`
	PostgresSchema         string `env:"POSTGRES_SCHEMA" default:"public"`
	PostgresPoolMinSize    int    `env:"POSTGRES_POOL_MIN_SIZE" default:"1" min:"1"`
	PostgresPoolMaxSize    int    `env:"POSTGRES_POOL_MAX_SIZE" default:"10" min:"1"`
	PostgresPoolTimeoutSec int    `env:"POSTGRES_POOL_TIMEOUT_SEC" default:"10" min:"1"`

	// Dashboard
	DashboardSSESyncSec int `env:"DASHBOARD_SSE_SYNC_SEC" default:"5" min:"1"`
	AuditLogLimit       int `env:"AUDIT_LOG_LIMIT" default:"100" min:"1"`
	SystemLogLimit      int `env:"SYSTEM_LOG_LIMIT" default:"100" min:"1"`

	// Telegram
	TGBotToken string `env:"TG_BOT_TOKEN"`
	TGChatID   string `env:"TG_CHAT_ID"`

	// 拓扑
	TopologyProposalEnabled bool `env:"TOPOLOGY_PROPOSAL_ENABLED" default:"true"`
	TopologyApprovalTTLSec  int  `env:"TOPOLOGY_APPROVAL_TTL_SEC" default:"120" min:"1"`

	// 日志
	LogLevel string `env:"LOG_LEVEL" default:"INFO"`

	// 运行时
	ACPBusSingletonEnabled bool `env:"ACP_BUS_SINGLETON_ENABLED" default:"false"`
	AgentDBExecuteEnabled  bool `env:"AGENT_DB_EXECUTE_ENABLED" default:"true"`

	// 编排工作区 (双通道: 虚拟目录 + PG 状态)
	OrchestrationWorkspaceRoot          string `env:"ORCHESTRATION_WORKSPACE_ROOT" default:".agent/workspaces"`
	OrchestrationWorkspaceMaxFiles      int    `env:"ORCHESTRATION_WORKSPACE_MAX_FILES" default:"5000" min:"1"`
	OrchestrationWorkspaceMaxFileBytes  int    `env:"ORCHESTRATION_WORKSPACE_MAX_FILE_BYTES" default:"8388608" min:"1024"`     // 8MB
	OrchestrationWorkspaceMaxTotalBytes int    `env:"ORCHESTRATION_WORKSPACE_MAX_TOTAL_BYTES" default:"268435456" min:"10240"` // 256MB
}

// Load 从环境变量加载配置 (通过反射读取 struct tag)。
func Load() *Config {
	var cfg Config
	util.LoadFromEnv(&cfg)
	return &cfg
}
