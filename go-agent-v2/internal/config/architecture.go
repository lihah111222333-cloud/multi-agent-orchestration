package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// architectureMu 保护 config.json 的并发读写。
var architectureMu sync.Mutex

// GatewayConfig 单个 Gateway 的配置。
type GatewayConfig struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Capabilities []string      `json:"capabilities,omitempty"`
	Agents       []AgentConfig `json:"agents,omitempty"`
}

// AgentConfig 单个 Agent 的配置。
type AgentConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Module       string   `json:"module,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Plugins      []string `json:"plugins,omitempty"`
}

// ArchitectureRaw config.json 的顶层结构。
type ArchitectureRaw struct {
	Gateways []GatewayConfig `json:"gateways"`
}

// ArchitectureSnapshot 架构快照，含哈希和时间戳。
type ArchitectureSnapshot struct {
	Raw       *ArchitectureRaw `json:"raw"`
	Hash      string           `json:"hash"`
	CreatedAt string           `json:"created_at"`
}

// LoadArchitectureRaw 加载原始 config.json。
// 对应 Python load_architecture_raw。
func LoadArchitectureRaw(configPath string) (*ArchitectureRaw, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ArchitectureRaw{}, nil
		}
		return nil, err
	}

	var raw ArchitectureRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		logger.Warn("config.json parse failed", logger.FieldError, err)
		return &ArchitectureRaw{}, nil
	}
	return &raw, nil
}

// SaveArchitecture 原子写入 config.json (对应 Python save_architecture)。
func SaveArchitecture(configPath string, data *ArchitectureRaw) error {
	architectureMu.Lock()
	defer architectureMu.Unlock()

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, configPath)
}

// LoadArchitectureSnapshot 加载架构快照 (对应 Python load_architecture_snapshot)。
func LoadArchitectureSnapshot(configPath string) (*ArchitectureSnapshot, error) {
	raw, err := LoadArchitectureRaw(configPath)
	if err != nil {
		return nil, err
	}

	normalized, _ := json.Marshal(raw)
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(normalized))

	dir := filepath.Dir(configPath)
	_ = dir // 备份目录预留

	return &ArchitectureSnapshot{
		Raw:       raw,
		Hash:      hash,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
