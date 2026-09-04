package manager

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultWorkers = 6
	maxWorkers     = 16
)

type Config struct {
	Workers              int                      `yaml:"workers"`
	DataDir              string                   `yaml:"data_dir"`
	ManagementBaseURL    string                   `yaml:"management_base_url"`
	PersonalGateway      PersonalGatewayConfig    `yaml:"personal_gateway"`
	GatewayAccountAID    string                   `yaml:"gateway_account_a_id,omitempty"`
	GatewayAccountBID    string                   `yaml:"gateway_account_b_id,omitempty"`
	GatewayRoleA         string                   `yaml:"gateway_role_a,omitempty"`
	GatewayRoleB         string                   `yaml:"gateway_role_b,omitempty"`
	GatewayMode          string                   `yaml:"gateway_mode,omitempty"`
	GatewayOverdraft     *bool                    `yaml:"gateway_overdraft_enabled,omitempty"`
	UpdatePolicy         *UpdatePolicy            `yaml:"update_policy,omitempty"`
	OperationSettings    *OperationSettingsConfig `yaml:"operation_settings,omitempty"`
	ExperimentalSettings *ExperimentalSettings    `yaml:"experimental_settings,omitempty"`
	implicitDataDir      bool
}

// PersonalGatewayConfig contains only the two stable Auth IDs and their
// routing roles. It deliberately contains no credential material.
type PersonalGatewayConfig struct {
	AccountAID       string `yaml:"account_a_id" json:"account_a_id"`
	AccountBID       string `yaml:"account_b_id" json:"account_b_id"`
	RoleA            string `yaml:"role_a" json:"role_a"`
	RoleB            string `yaml:"role_b" json:"role_b"`
	Mode             string `yaml:"mode" json:"mode"`
	OverdraftEnabled bool   `yaml:"overdraft_enabled" json:"overdraft_enabled"`
}

type OperationSettingsConfig struct {
	ExtendedHistory bool `json:"extended_history" yaml:"extended_history"`
}

func ParseConfig(raw []byte) Config {
	cfg := Config{}
	if len(raw) > 0 {
		_ = yaml.Unmarshal(raw, &cfg)
	}
	return normalizeConfig(cfg)
}

func normalizeConfig(cfg Config) Config {
	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers
	}
	if cfg.Workers > maxWorkers {
		cfg.Workers = maxWorkers
	}
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	if !cfg.implicitDataDir {
		if cfg.DataDir == "" {
			cfg.DataDir = strings.TrimSpace(os.Getenv("CPA_ACCOUNT_CONFIG_MANAGER_DATA_DIR"))
		}
		if cfg.DataDir == "" {
			cfg.DataDir = "data/cpa-account-config-manager"
			cfg.implicitDataDir = true
		}
	}
	cfg.ManagementBaseURL = strings.TrimRight(strings.TrimSpace(cfg.ManagementBaseURL), "/")
	personalGateway := cfg.PersonalGateway
	if value := strings.TrimSpace(cfg.GatewayAccountAID); value != "" {
		personalGateway.AccountAID = value
	}
	if value := strings.TrimSpace(cfg.GatewayAccountBID); value != "" {
		personalGateway.AccountBID = value
	}
	if value := strings.TrimSpace(cfg.GatewayRoleA); value != "" {
		personalGateway.RoleA = value
	}
	if value := strings.TrimSpace(cfg.GatewayRoleB); value != "" {
		personalGateway.RoleB = value
	}
	if value := strings.TrimSpace(cfg.GatewayMode); value != "" {
		personalGateway.Mode = value
	}
	if cfg.GatewayOverdraft != nil {
		personalGateway.OverdraftEnabled = *cfg.GatewayOverdraft
	}
	cfg.PersonalGateway = normalizePersonalGatewayConfig(personalGateway)
	if cfg.UpdatePolicy != nil {
		policy := *cfg.UpdatePolicy
		cfg.UpdatePolicy = &policy
	}
	if cfg.OperationSettings != nil {
		settings := *cfg.OperationSettings
		cfg.OperationSettings = &settings
	}
	if cfg.ExperimentalSettings != nil {
		settings := *cfg.ExperimentalSettings
		cfg.ExperimentalSettings = &settings
	}
	return cfg
}
