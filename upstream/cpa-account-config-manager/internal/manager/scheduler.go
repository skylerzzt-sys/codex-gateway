package manager

import (
	"context"
	"fmt"
	"strings"

	"cpa-account-config-manager/internal/cpaapi"
)

const (
	personalGatewayRolePrimary  = "primary"
	personalGatewayRoleBackup   = "backup"
	personalGatewayRoleDisabled = "disabled"

	personalGatewayModeAuto   = "auto"
	personalGatewayModeForceA = "forcea"
	personalGatewayModeForceB = "forceb"

	personalGatewayUnconfiguredCode       = "personal_gateway_unconfigured"
	personalGatewayInvalidConfigCode      = "personal_gateway_invalid_config"
	personalGatewayUnknownCandidateCode   = "personal_gateway_unknown_candidate"
	personalGatewayNoCandidateCode        = "personal_gateway_no_candidate"
	personalGatewayForceTargetConfigCode  = "personal_gateway_force_target_unconfigured"
	personalGatewayForceTargetErrorCode   = "personal_gateway_force_target_unavailable"
	personalGatewayRuntimeUnavailableCode = "personal_gateway_runtime_unavailable"
)

// personalGatewayError is intentionally small and serializes as a stable ABI
// error code through the outer plugin envelope.
type personalGatewayError struct {
	code    string
	message string
}

func newPersonalGatewayError(code, message string) error {
	return &personalGatewayError{code: code, message: message}
}

func (e *personalGatewayError) Error() string {
	if e == nil {
		return "personal gateway error"
	}
	return e.message
}

func (e *personalGatewayError) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.code
}

func normalizePersonalGatewayConfig(cfg PersonalGatewayConfig) PersonalGatewayConfig {
	cfg.AccountAID = strings.TrimSpace(cfg.AccountAID)
	cfg.AccountBID = strings.TrimSpace(cfg.AccountBID)
	cfg.RoleA = normalizePersonalGatewayRole(cfg.RoleA)
	cfg.RoleB = normalizePersonalGatewayRole(cfg.RoleB)
	cfg.Mode = normalizePersonalGatewayMode(cfg.Mode)
	if cfg.AccountAID != "" && cfg.RoleA == "" {
		cfg.RoleA = personalGatewayRolePrimary
	}
	if cfg.AccountBID != "" && cfg.RoleB == "" {
		cfg.RoleB = personalGatewayRoleBackup
	}
	if cfg.AccountAID == "" {
		cfg.RoleA = personalGatewayRoleDisabled
	}
	if cfg.AccountBID == "" {
		cfg.RoleB = personalGatewayRoleDisabled
	}
	if cfg.Mode == "" {
		cfg.Mode = personalGatewayModeAuto
	}
	return cfg
}

func normalizePersonalGatewayRole(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)
	return value
}

func normalizePersonalGatewayMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "", "-", "", " ", "").Replace(value)
	return value
}

func (cfg PersonalGatewayConfig) Validate() error {
	for _, item := range []struct {
		slot string
		role string
	}{
		{slot: "A", role: cfg.RoleA},
		{slot: "B", role: cfg.RoleB},
	} {
		normalizedRole := normalizePersonalGatewayRole(item.role)
		if normalizedRole == "" {
			continue
		}
		switch normalizedRole {
		case personalGatewayRolePrimary, personalGatewayRoleBackup, personalGatewayRoleDisabled:
		default:
			return newPersonalGatewayError(personalGatewayInvalidConfigCode, fmt.Sprintf("personal gateway role_%s is invalid", strings.ToLower(item.slot)))
		}
	}
	cfg = normalizePersonalGatewayConfig(cfg)
	if cfg.AccountAID == "" && cfg.AccountBID == "" {
		return newPersonalGatewayError(personalGatewayUnconfiguredCode, "personal gateway accounts are not configured")
	}
	if cfg.AccountAID != "" && cfg.AccountAID == cfg.AccountBID {
		return newPersonalGatewayError(personalGatewayInvalidConfigCode, "personal gateway account_a_id and account_b_id must differ")
	}
	for _, item := range []struct {
		slot      string
		accountID string
		role      string
	}{
		{slot: "A", accountID: cfg.AccountAID, role: cfg.RoleA},
		{slot: "B", accountID: cfg.AccountBID, role: cfg.RoleB},
	} {
		if item.accountID == "" {
			continue
		}
		switch item.role {
		case personalGatewayRolePrimary, personalGatewayRoleBackup, personalGatewayRoleDisabled:
		default:
			return newPersonalGatewayError(personalGatewayInvalidConfigCode, fmt.Sprintf("personal gateway role_%s is invalid", strings.ToLower(item.slot)))
		}
	}
	switch cfg.Mode {
	case personalGatewayModeAuto, personalGatewayModeForceA, personalGatewayModeForceB:
	default:
		return newPersonalGatewayError(personalGatewayInvalidConfigCode, "personal gateway mode is invalid")
	}
	return nil
}

func pickPersonalGatewayAuth(ctx context.Context, cfg PersonalGatewayConfig, candidates []cpaapi.SchedulerAuthCandidate) (cpaapi.SchedulerPickResponse, error) {
	if ctx != nil {
		if errContext := ctx.Err(); errContext != nil {
			return cpaapi.SchedulerPickResponse{}, errContext
		}
	}
	cfg = normalizePersonalGatewayConfig(cfg)
	if errValidate := cfg.Validate(); errValidate != nil {
		return cpaapi.SchedulerPickResponse{}, errValidate
	}

	byID := make(map[string]cpaapi.SchedulerAuthCandidate, len(candidates))
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.ID)
		if id == "" {
			return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayUnknownCandidateCode, "scheduler candidate has no auth ID")
		}
		if id != cfg.AccountAID && id != cfg.AccountBID {
			return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayUnknownCandidateCode, "scheduler candidate is not bound to personal gateway")
		}
		if _, exists := byID[id]; exists {
			return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayUnknownCandidateCode, "scheduler candidate list contains a duplicate auth ID")
		}
		byID[id] = candidate
	}

	if cfg.Mode == personalGatewayModeForceA || cfg.Mode == personalGatewayModeForceB {
		targetID := cfg.AccountAID
		if cfg.Mode == personalGatewayModeForceB {
			targetID = cfg.AccountBID
		}
		if targetID == "" {
			return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayForceTargetConfigCode, "forced personal gateway account is not configured")
		}
		candidate, exists := byID[targetID]
		if !exists || !personalGatewayCandidateAvailable(candidate, cfg, targetID) {
			return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayForceTargetErrorCode, "forced personal gateway account is unavailable")
		}
		return cpaapi.SchedulerPickResponse{AuthID: targetID, Handled: true}, nil
	}

	for _, target := range []struct {
		id   string
		role string
	}{
		{cfg.AccountAID, cfg.RoleA},
		{cfg.AccountBID, cfg.RoleB},
	} {
		if target.id == "" || target.role == personalGatewayRoleDisabled {
			continue
		}
		candidate, exists := byID[target.id]
		if exists && personalGatewayCandidateAvailable(candidate, cfg, target.id) {
			if target.role == personalGatewayRolePrimary {
				return cpaapi.SchedulerPickResponse{AuthID: target.id, Handled: true}, nil
			}
		}
	}
	for _, target := range []struct {
		id   string
		role string
	}{
		{cfg.AccountAID, cfg.RoleA},
		{cfg.AccountBID, cfg.RoleB},
	} {
		if target.id == "" || target.role != personalGatewayRoleBackup {
			continue
		}
		candidate, exists := byID[target.id]
		if exists && personalGatewayCandidateAvailable(candidate, cfg, target.id) {
			return cpaapi.SchedulerPickResponse{AuthID: target.id, Handled: true}, nil
		}
	}
	return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayNoCandidateCode, "no configured personal gateway account is available")
}

func personalGatewayCandidateAvailable(candidate cpaapi.SchedulerAuthCandidate, cfg PersonalGatewayConfig, id string) bool {
	if id == cfg.AccountAID && cfg.RoleA == personalGatewayRoleDisabled || id == cfg.AccountBID && cfg.RoleB == personalGatewayRoleDisabled {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(candidate.Status), personalGatewayRoleDisabled)
}
