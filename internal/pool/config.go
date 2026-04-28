package pool

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// PoolConfig is the static agent pool definition.
type PoolConfig struct {
	Pool struct {
		Name                     string  `yaml:"name"`
		MaxAgents                int     `yaml:"max_agents"`
		MinAgents                int     `yaml:"min_agents"`
		HeartbeatIntervalSeconds int     `yaml:"heartbeat_interval_seconds"`
		HeartbeatTimeoutFactor   int     `yaml:"heartbeat_timeout_factor"`
		BackpressureThreshold    float64 `yaml:"backpressure_threshold"`
		WarmupIdleSeconds        int     `yaml:"warmup_idle_seconds"`
	} `yaml:"pool"`
	Souls []struct {
		ID       string `yaml:"id"`
		Replicas int    `yaml:"replicas"`
	} `yaml:"souls"`
}

// LoadConfig reads and validates a pool YAML file.
func LoadConfig(path string) (*PoolConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pool config: read %s: %w", path, err)
	}
	var cfg PoolConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("pool config: parse: %w", err)
	}
	if cfg.Pool.HeartbeatIntervalSeconds <= 0 {
		cfg.Pool.HeartbeatIntervalSeconds = 5
	}
	if cfg.Pool.HeartbeatTimeoutFactor <= 0 {
		cfg.Pool.HeartbeatTimeoutFactor = 3
	}
	return &cfg, nil
}

// DeadAgentTimeout returns the duration after which an agent is declared dead.
func (c *PoolConfig) DeadAgentTimeout() time.Duration {
	return time.Duration(c.Pool.HeartbeatIntervalSeconds*c.Pool.HeartbeatTimeoutFactor) * time.Second
}

// MaxConcurrent returns the total replicas across all soul types.
func (c *PoolConfig) MaxConcurrent() int {
	n := 0
	for _, s := range c.Souls {
		n += s.Replicas
	}
	return n
}
