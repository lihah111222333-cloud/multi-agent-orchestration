package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

var architectureMu sync.Mutex

type GatewayConfig struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Capabilities []string      `json:"capabilities,omitempty"`
	Agents       []AgentConfig `json:"agents,omitempty"`
}

type AgentConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Module       string   `json:"module,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Plugins      []string `json:"plugins,omitempty"`
}

type ArchitectureRaw struct {
	Gateways []GatewayConfig `json:"gateways"`
}

type ArchitectureSnapshot struct {
	Raw       *ArchitectureRaw `json:"raw"`
	Hash      string           `json:"hash"`
	CreatedAt string           `json:"created_at"`
}

func LoadArchitectureRaw(configPath string) (*ArchitectureRaw, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ArchitectureRaw{}, nil
		}
		return nil, err
	}
	raw := &ArchitectureRaw{}
	if err := json.Unmarshal(data, raw); err != nil {
		logger.Warn("config.json parse failed", logger.FieldError, err)
		return &ArchitectureRaw{}, nil
	}
	return raw, nil
}

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

func LoadArchitectureSnapshot(configPath string) (*ArchitectureSnapshot, error) {
	raw, err := LoadArchitectureRaw(configPath)
	if err != nil {
		return nil, err
	}
	normalized, _ := json.Marshal(raw)
	return &ArchitectureSnapshot{
		Raw:       raw,
		Hash:      fmt.Sprintf("sha256:%x", sha256.Sum256(normalized)),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
