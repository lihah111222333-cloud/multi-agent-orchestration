// config_test.go — 配置加载默认值 + 环境变量覆盖测试。
package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// 确保关键环境变量未设置
	os.Unsetenv("LLM_MODEL")
	os.Unsetenv("GATEWAY_TIMEOUT")
	os.Unsetenv("POSTGRES_SCHEMA")

	cfg := Load()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"LLMModel", cfg.LLMModel, "gpt-4o"},
		{"LLMTemperature", cfg.LLMTemperature, 0.7},
		{"LLMTimeout", cfg.LLMTimeout, 120},
		{"LLMMaxRetries", cfg.LLMMaxRetries, 3},
		{"GatewayTimeout", cfg.GatewayTimeout, 240},
		{"GatewayMaxAttempts", cfg.GatewayMaxAttempts, 2},
		{"CommandCardTimeoutSec", cfg.CommandCardTimeoutSec, 240},
		{"GatewayMinQualityScore", cfg.GatewayMinQualityScore, 25},
		{"PostgresSchema", cfg.PostgresSchema, "public"},
		{"PostgresPoolMinSize", cfg.PostgresPoolMinSize, 1},
		{"PostgresPoolMaxSize", cfg.PostgresPoolMaxSize, 10},
		{"DashboardSSESyncSec", cfg.DashboardSSESyncSec, 5},
		{"AuditLogLimit", cfg.AuditLogLimit, 100},
		{"SystemLogLimit", cfg.SystemLogLimit, 100},
		{"LogLevel", cfg.LogLevel, "INFO"},
		{"TopologyProposalEnabled", cfg.TopologyProposalEnabled, true},
		{"TopologyApprovalTTLSec", cfg.TopologyApprovalTTLSec, 120},
		{"ACPBusSingletonEnabled", cfg.ACPBusSingletonEnabled, false},
		{"AgentDBExecuteEnabled", cfg.AgentDBExecuteEnabled, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("LLM_MODEL", "claude-3")
	t.Setenv("GATEWAY_TIMEOUT", "60")
	t.Setenv("POSTGRES_SCHEMA", "test_schema")
	t.Setenv("LOG_LEVEL", "DEBUG")
	t.Setenv("TOPOLOGY_PROPOSAL_ENABLED", "false")

	cfg := Load()

	if cfg.LLMModel != "claude-3" {
		t.Errorf("LLMModel = %q, want 'claude-3'", cfg.LLMModel)
	}
	if cfg.GatewayTimeout != 60 {
		t.Errorf("GatewayTimeout = %d, want 60", cfg.GatewayTimeout)
	}
	if cfg.PostgresSchema != "test_schema" {
		t.Errorf("PostgresSchema = %q, want 'test_schema'", cfg.PostgresSchema)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q, want 'DEBUG'", cfg.LogLevel)
	}
	if cfg.TopologyProposalEnabled {
		t.Errorf("TopologyProposalEnabled = true, want false")
	}
}

func TestLoadReturnsNonNil(t *testing.T) {
	cfg := Load()
	if cfg == nil {
		t.Fatal("Load() returned nil")
	}
}
