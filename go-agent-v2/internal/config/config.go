// Package config loads process-wide settings from environment variables.
package config

import (
	"github.com/kelseyhightower/envconfig"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// Config 应用全局配置，字段名与 .env 变量一一对应。
type Config struct {
	// LLM
	LLMModel       string  `envconfig:"LLM_MODEL" default:"gpt-4o"`
	LLMTemperature float64 `envconfig:"LLM_TEMPERATURE" default:"0.7"`
	OpenAIAPIKey   string  `envconfig:"OPENAI_API_KEY"`
	OpenAIBaseURL  string  `envconfig:"OPENAI_BASE_URL"`
	LLMTimeout     int     `envconfig:"LLM_TIMEOUT" default:"120"`
	LLMMaxRetries  int     `envconfig:"LLM_MAX_RETRIES" default:"3"`

	// Dynamic Tool Routing (V2)
	DynToolRoutingMode               string  `envconfig:"DYN_TOOL_ROUTING_MODE" default:"legacy"`
	DynToolRouterModel               string  `envconfig:"DYN_TOOL_ROUTER_MODEL"`
	DynToolRouterProvider            string  `envconfig:"DYN_TOOL_ROUTER_PROVIDER" default:"openai_compatible"`
	DynToolRouterBaseURL             string  `envconfig:"DYN_TOOL_ROUTER_BASE_URL"`
	DynToolRouterAPIKey              string  `envconfig:"DYN_TOOL_ROUTER_API_KEY"`
	DynToolRouterConfidenceThreshold float64 `envconfig:"DYN_TOOL_ROUTER_CONFIDENCE_THRESHOLD" default:"0.65"`
	DynToolRouterTimeoutSec          int     `envconfig:"DYN_TOOL_ROUTER_TIMEOUT_SEC" default:"8"`

	// Gateway
	GatewayTimeout         int `envconfig:"GATEWAY_TIMEOUT" default:"240"`
	GatewayMaxAttempts     int `envconfig:"GATEWAY_MAX_ATTEMPTS" default:"2"`
	CommandCardTimeoutSec  int `envconfig:"COMMAND_CARD_TIMEOUT_SEC" default:"240"`
	GatewayMinQualityScore int `envconfig:"GATEWAY_MIN_QUALITY_SCORE" default:"25"`

	// PostgreSQL
	PostgresConnStr        string `envconfig:"POSTGRES_CONNECTION_STRING"`
	PostgresSchema         string `envconfig:"POSTGRES_SCHEMA" default:"public"`
	PostgresPoolMinSize    int    `envconfig:"POSTGRES_POOL_MIN_SIZE" default:"1"`
	PostgresPoolMaxSize    int    `envconfig:"POSTGRES_POOL_MAX_SIZE" default:"10"`
	PostgresPoolTimeoutSec int    `envconfig:"POSTGRES_POOL_TIMEOUT_SEC" default:"10"`

	// Dashboard
	DashboardSSESyncSec int `envconfig:"DASHBOARD_SSE_SYNC_SEC" default:"5"`
	AuditLogLimit       int `envconfig:"AUDIT_LOG_LIMIT" default:"100"`
	SystemLogLimit      int `envconfig:"SYSTEM_LOG_LIMIT" default:"100"`

	// Telegram
	TGBotToken string `envconfig:"TG_BOT_TOKEN"`
	TGChatID   string `envconfig:"TG_CHAT_ID"`

	// 拓扑
	TopologyProposalEnabled bool `envconfig:"TOPOLOGY_PROPOSAL_ENABLED" default:"true"`
	TopologyApprovalTTLSec  int  `envconfig:"TOPOLOGY_APPROVAL_TTL_SEC" default:"120"`

	// 日志
	LogLevel string `envconfig:"LOG_LEVEL" default:"INFO"`

	// HTTP 服务
	GinMode        string `envconfig:"GIN_MODE" default:"release"`          // release / debug / test
	TrustedProxies string `envconfig:"TRUSTED_PROXIES" default:"127.0.0.1"` // 逗号分隔 IP 列表

	// 运行时
	ACPBusSingletonEnabled bool `envconfig:"ACP_BUS_SINGLETON_ENABLED" default:"false"`
	AgentDBExecuteEnabled  bool `envconfig:"AGENT_DB_EXECUTE_ENABLED" default:"true"`
	MigrationNonFatal      bool `envconfig:"MIGRATION_NON_FATAL" default:"false"`
	// 52个高置信未触发 JSON-RPC 方法下线开关（true=下线，false=回滚恢复）
	DisableOffline52Methods bool `envconfig:"DISABLE_OFFLINE_52_METHODS" default:"true"`

	// Turn Tracker (stall 检测)
	StallThresholdSec int `envconfig:"STALL_THRESHOLD_SEC" default:"480"` // 无事件多久(秒)触发 stall 自动中断
	StallHeartbeatSec int `envconfig:"STALL_HEARTBEAT_SEC" default:"300"` // dynamic tool call / 审批等待时的保活心跳间隔(秒)

	// 编排工作区 (双通道: 虚拟目录 + PG 状态)
	OrchestrationWorkspaceRoot          string `envconfig:"ORCHESTRATION_WORKSPACE_ROOT" default:".agent/workspaces"`
	OrchestrationWorkspaceMaxFiles      int    `envconfig:"ORCHESTRATION_WORKSPACE_MAX_FILES" default:"5000"`
	OrchestrationWorkspaceMaxFileBytes  int    `envconfig:"ORCHESTRATION_WORKSPACE_MAX_FILE_BYTES" default:"8388608"`    // 8MB
	OrchestrationWorkspaceMaxTotalBytes int    `envconfig:"ORCHESTRATION_WORKSPACE_MAX_TOTAL_BYTES" default:"268435456"` // 256MB

	// 数据库迁移
	MigrationsDir string `envconfig:"MIGRATIONS_DIR"` // 为空时使用默认路径探测逻辑
}

// Load 从环境变量加载配置 (通过 envconfig 读取 struct tag)。
func Load() *Config {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		logger.Fatal("config: failed to load from env", logger.FieldError, err)
	}
	clampMinimums(&cfg)
	return &cfg
}

// clampMinimums 对 int/float64 字段应用最小值约束（envconfig 不支持 min tag）。
func clampMinimums(c *Config) {
	c.LLMTimeout = max(c.LLMTimeout, 1)
	c.LLMMaxRetries = max(c.LLMMaxRetries, 0)
	c.LLMTemperature = max(c.LLMTemperature, 0.0)
	c.DynToolRouterConfidenceThreshold = max(c.DynToolRouterConfidenceThreshold, 0.0)
	c.DynToolRouterTimeoutSec = max(c.DynToolRouterTimeoutSec, 1)
	c.GatewayTimeout = max(c.GatewayTimeout, 1)
	c.GatewayMaxAttempts = max(c.GatewayMaxAttempts, 1)
	c.CommandCardTimeoutSec = max(c.CommandCardTimeoutSec, 1)
	c.GatewayMinQualityScore = max(c.GatewayMinQualityScore, 0)
	c.PostgresPoolMinSize = max(c.PostgresPoolMinSize, 1)
	c.PostgresPoolMaxSize = max(c.PostgresPoolMaxSize, 1)
	c.PostgresPoolTimeoutSec = max(c.PostgresPoolTimeoutSec, 1)
	c.DashboardSSESyncSec = max(c.DashboardSSESyncSec, 1)
	c.AuditLogLimit = max(c.AuditLogLimit, 1)
	c.SystemLogLimit = max(c.SystemLogLimit, 1)
	c.TopologyApprovalTTLSec = max(c.TopologyApprovalTTLSec, 1)
	c.StallThresholdSec = max(c.StallThresholdSec, 30)
	c.StallHeartbeatSec = max(c.StallHeartbeatSec, 10)
	c.OrchestrationWorkspaceMaxFiles = max(c.OrchestrationWorkspaceMaxFiles, 1)
	c.OrchestrationWorkspaceMaxFileBytes = max(c.OrchestrationWorkspaceMaxFileBytes, 1024)
	c.OrchestrationWorkspaceMaxTotalBytes = max(c.OrchestrationWorkspaceMaxTotalBytes, 10240)
}
