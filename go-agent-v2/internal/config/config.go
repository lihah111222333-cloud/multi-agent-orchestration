package config

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type Config struct {
	LLMModel                            string  `envconfig:"LLM_MODEL" default:"gpt-4o"`
	LLMTemperature                      float64 `envconfig:"LLM_TEMPERATURE" default:"0.7"`
	OpenAIAPIKey                        string  `envconfig:"OPENAI_API_KEY"`
	OpenAIBaseURL                       string  `envconfig:"OPENAI_BASE_URL"`
	LLMTimeout                          int     `envconfig:"LLM_TIMEOUT" default:"120"`
	LLMMaxRetries                       int     `envconfig:"LLM_MAX_RETRIES" default:"3"`
	DynToolRoutingMode                  string  `envconfig:"DYN_TOOL_ROUTING_MODE" default:"legacy"`
	DynToolRouterModel                  string  `envconfig:"DYN_TOOL_ROUTER_MODEL"`
	DynToolRouterProvider               string  `envconfig:"DYN_TOOL_ROUTER_PROVIDER" default:"openai_compatible"`
	DynToolRouterBaseURL                string  `envconfig:"DYN_TOOL_ROUTER_BASE_URL"`
	DynToolRouterAPIKey                 string  `envconfig:"DYN_TOOL_ROUTER_API_KEY"`
	DynToolRouterConfidenceThreshold    float64 `envconfig:"DYN_TOOL_ROUTER_CONFIDENCE_THRESHOLD" default:"0.65"`
	DynToolRouterTimeoutSec             int     `envconfig:"DYN_TOOL_ROUTER_TIMEOUT_SEC" default:"8"`
	GatewayTimeout                      int     `envconfig:"GATEWAY_TIMEOUT" default:"240"`
	GatewayMaxAttempts                  int     `envconfig:"GATEWAY_MAX_ATTEMPTS" default:"2"`
	CommandCardTimeoutSec               int     `envconfig:"COMMAND_CARD_TIMEOUT_SEC" default:"240"`
	GatewayMinQualityScore              int     `envconfig:"GATEWAY_MIN_QUALITY_SCORE" default:"25"`
	PostgresConnStr                     string  `envconfig:"POSTGRES_CONNECTION_STRING"`
	PostgresSchema                      string  `envconfig:"POSTGRES_SCHEMA" default:"public"`
	PostgresPoolMinSize                 int     `envconfig:"POSTGRES_POOL_MIN_SIZE" default:"1"`
	PostgresPoolMaxSize                 int     `envconfig:"POSTGRES_POOL_MAX_SIZE" default:"10"`
	PostgresPoolTimeoutSec              int     `envconfig:"POSTGRES_POOL_TIMEOUT_SEC" default:"10"`
	DashboardSSESyncSec                 int     `envconfig:"DASHBOARD_SSE_SYNC_SEC" default:"5"`
	AuditLogLimit                       int     `envconfig:"AUDIT_LOG_LIMIT" default:"100"`
	SystemLogLimit                      int     `envconfig:"SYSTEM_LOG_LIMIT" default:"100"`
	TGBotToken                          string  `envconfig:"TG_BOT_TOKEN"`
	TGChatID                            string  `envconfig:"TG_CHAT_ID"`
	TopologyProposalEnabled             bool    `envconfig:"TOPOLOGY_PROPOSAL_ENABLED" default:"true"`
	TopologyApprovalTTLSec              int     `envconfig:"TOPOLOGY_APPROVAL_TTL_SEC" default:"120"`
	LogLevel                            string  `envconfig:"LOG_LEVEL" default:"INFO"`
	GinMode                             string  `envconfig:"GIN_MODE" default:"release"`
	TrustedProxies                      string  `envconfig:"TRUSTED_PROXIES" default:"127.0.0.1"`
	ACPBusSingletonEnabled              bool    `envconfig:"ACP_BUS_SINGLETON_ENABLED" default:"false"`
	AgentDBExecuteEnabled               bool    `envconfig:"AGENT_DB_EXECUTE_ENABLED" default:"true"`
	MigrationNonFatal                   bool    `envconfig:"MIGRATION_NON_FATAL" default:"false"`
	DisableOffline52Methods             bool    `envconfig:"DISABLE_OFFLINE_52_METHODS" default:"true"`
	StallThresholdSec                   int     `envconfig:"STALL_THRESHOLD_SEC" default:"480"`
	StallHeartbeatSec                   int     `envconfig:"STALL_HEARTBEAT_SEC" default:"300"`
	OrchestrationWorkspaceRoot          string  `envconfig:"ORCHESTRATION_WORKSPACE_ROOT" default:".agent/workspaces"`
	OrchestrationWorkspaceMaxFiles      int     `envconfig:"ORCHESTRATION_WORKSPACE_MAX_FILES" default:"5000"`
	OrchestrationWorkspaceMaxFileBytes  int     `envconfig:"ORCHESTRATION_WORKSPACE_MAX_FILE_BYTES" default:"8388608"`
	OrchestrationWorkspaceMaxTotalBytes int     `envconfig:"ORCHESTRATION_WORKSPACE_MAX_TOTAL_BYTES" default:"268435456"`
	MigrationsDir                       string  `envconfig:"MIGRATIONS_DIR"`
}

func Load() *Config {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		logger.Fatal("config: failed to load from env", logger.FieldError, err)
	}
	clampMinimums(&cfg)
	return &cfg
}

func clampMinimums(c *Config) {
	c.LLMTimeout = max(c.LLMTimeout, 1)
	c.LLMMaxRetries = max(c.LLMMaxRetries, 0)
	c.LLMTemperature = max(c.LLMTemperature, 0.0)
	c.DynToolRouterConfidenceThreshold = min(max(c.DynToolRouterConfidenceThreshold, 0.0), 1.0)
	c.DynToolRouterTimeoutSec = max(c.DynToolRouterTimeoutSec, 1)
	c.GatewayTimeout = max(c.GatewayTimeout, 1)
	c.GatewayMaxAttempts = max(c.GatewayMaxAttempts, 1)
	c.CommandCardTimeoutSec = max(c.CommandCardTimeoutSec, 1)
	c.GatewayMinQualityScore = max(c.GatewayMinQualityScore, 0)
	c.PostgresPoolMinSize = max(c.PostgresPoolMinSize, 1)
	c.PostgresPoolMaxSize = max(c.PostgresPoolMaxSize, 1)
	if c.PostgresPoolMaxSize < c.PostgresPoolMinSize { c.PostgresPoolMaxSize = c.PostgresPoolMinSize }
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
	if c.OrchestrationWorkspaceMaxTotalBytes < c.OrchestrationWorkspaceMaxFileBytes { c.OrchestrationWorkspaceMaxTotalBytes = c.OrchestrationWorkspaceMaxFileBytes }
}
