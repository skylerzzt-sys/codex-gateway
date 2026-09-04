package manager

import (
	"context"
	"errors"
	"testing"

	"cpa-account-config-manager/internal/cpaapi"
)

func gatewayCandidate(id, status string) cpaapi.SchedulerAuthCandidate {
	return cpaapi.SchedulerAuthCandidate{ID: id, Provider: "codex", Status: status}
}

func gatewayConfig(mode string) PersonalGatewayConfig {
	return PersonalGatewayConfig{
		AccountAID: "auth-a",
		AccountBID: "auth-b",
		RoleA:      "Primary",
		RoleB:      "Backup",
		Mode:       mode,
	}
}

func TestNormalizePersonalGatewayConfig(t *testing.T) {
	got := normalizePersonalGatewayConfig(PersonalGatewayConfig{
		AccountAID: " auth-a ",
		AccountBID: " auth-b ",
		RoleA:      "Force?",
		RoleB:      "Backup",
		Mode:       "Force A",
	})
	if got.AccountAID != "auth-a" || got.AccountBID != "auth-b" || got.RoleA != "force?" || got.RoleB != personalGatewayRoleBackup || got.Mode != personalGatewayModeForceA {
		t.Fatalf("normalized config = %#v", got)
	}

	defaults := normalizePersonalGatewayConfig(PersonalGatewayConfig{AccountAID: "auth-a"})
	if defaults.RoleA != personalGatewayRolePrimary || defaults.RoleB != personalGatewayRoleDisabled || defaults.Mode != personalGatewayModeAuto {
		t.Fatalf("defaulted config = %#v", defaults)
	}
}

func TestParseConfigAcceptsNativeControlPanelAliases(t *testing.T) {
	enabled := true
	config := ParseConfig([]byte("gateway_account_a_id: auth-a\ngateway_account_b_id: auth-b\ngateway_role_a: backup\ngateway_role_b: primary\ngateway_mode: force_b\ngateway_overdraft_enabled: true\n"))
	if config.PersonalGateway.AccountAID != "auth-a" || config.PersonalGateway.AccountBID != "auth-b" ||
		config.PersonalGateway.RoleA != personalGatewayRoleBackup || config.PersonalGateway.RoleB != personalGatewayRolePrimary ||
		config.PersonalGateway.Mode != personalGatewayModeForceB || config.PersonalGateway.OverdraftEnabled != enabled {
		t.Fatalf("personal gateway aliases = %#v", config.PersonalGateway)
	}
}

func TestPersonalGatewayConfigValidateUnconfiguredIsStable(t *testing.T) {
	errValidate := (PersonalGatewayConfig{}).Validate()
	if errValidate == nil {
		t.Fatal("Validate() error = nil")
	}
	var coded interface{ ErrorCode() string }
	if !errors.As(errValidate, &coded) || coded.ErrorCode() != personalGatewayUnconfiguredCode {
		t.Fatalf("Validate() error = %v, code=%v", errValidate, coded)
	}
}

func TestPickPersonalGatewayAutoPrefersPrimaryThenBackup(t *testing.T) {
	cfg := gatewayConfig("AUTO")
	resp, errPick := pickPersonalGatewayAuth(context.Background(), cfg, []cpaapi.SchedulerAuthCandidate{
		gatewayCandidate("auth-a", "active"), gatewayCandidate("auth-b", "active"),
	})
	if errPick != nil || !resp.Handled || resp.AuthID != "auth-a" {
		t.Fatalf("Auto both = %#v, %v", resp, errPick)
	}

	resp, errPick = pickPersonalGatewayAuth(context.Background(), cfg, []cpaapi.SchedulerAuthCandidate{gatewayCandidate("auth-b", "active")})
	if errPick != nil || !resp.Handled || resp.AuthID != "auth-b" {
		t.Fatalf("Auto backup = %#v, %v", resp, errPick)
	}
}

func TestPickPersonalGatewayAutoSkipsDisabledAndRejectsUnknown(t *testing.T) {
	cfg := gatewayConfig("auto")
	resp, errPick := pickPersonalGatewayAuth(context.Background(), cfg, []cpaapi.SchedulerAuthCandidate{
		gatewayCandidate("auth-a", "disabled"), gatewayCandidate("auth-b", "active"),
	})
	if errPick != nil || resp.AuthID != "auth-b" {
		t.Fatalf("Auto disabled primary = %#v, %v", resp, errPick)
	}

	_, errPick = pickPersonalGatewayAuth(context.Background(), cfg, []cpaapi.SchedulerAuthCandidate{
		gatewayCandidate("auth-a", "active"), gatewayCandidate("third-account", "active"),
	})
	var coded interface{ ErrorCode() string }
	if !errors.As(errPick, &coded) || coded.ErrorCode() != personalGatewayUnknownCandidateCode {
		t.Fatalf("unknown candidate error = %v, code=%v", errPick, coded)
	}
}

func TestPickPersonalGatewayForceFailsHard(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   string
		target string
		other  string
	}{
		{name: "force a", mode: "force_a", target: "auth-a", other: "auth-b"},
		{name: "force b", mode: "force-b", target: "auth-b", other: "auth-a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := gatewayConfig(test.mode)
			resp, errPick := pickPersonalGatewayAuth(context.Background(), cfg, []cpaapi.SchedulerAuthCandidate{
				gatewayCandidate(test.target, "active"), gatewayCandidate(test.other, "active"),
			})
			if errPick != nil || !resp.Handled || resp.AuthID != test.target {
				t.Fatalf("forced target = %#v, %v", resp, errPick)
			}

			_, errPick = pickPersonalGatewayAuth(context.Background(), cfg, []cpaapi.SchedulerAuthCandidate{gatewayCandidate(test.other, "active")})
			var coded interface{ ErrorCode() string }
			if !errors.As(errPick, &coded) || coded.ErrorCode() != personalGatewayForceTargetErrorCode {
				t.Fatalf("forced spill error = %v, code=%v", errPick, coded)
			}
		})
	}
}

func TestPickPersonalGatewayReturnsStableErrorsForMissingBindingAndPool(t *testing.T) {
	_, errPick := pickPersonalGatewayAuth(context.Background(), PersonalGatewayConfig{}, nil)
	var coded interface{ ErrorCode() string }
	if !errors.As(errPick, &coded) || coded.ErrorCode() != personalGatewayUnconfiguredCode {
		t.Fatalf("unconfigured error = %v, code=%v", errPick, coded)
	}

	_, errPick = pickPersonalGatewayAuth(context.Background(), gatewayConfig("auto"), nil)
	if !errors.As(errPick, &coded) || coded.ErrorCode() != personalGatewayNoCandidateCode {
		t.Fatalf("empty pool error = %v, code=%v", errPick, coded)
	}
}

func TestAppSchedulerDeclinesNonCodexProviders(t *testing.T) {
	app := NewApp(nil, nil)
	defer app.Close()
	app.Configure([]byte("personal_gateway:\n  account_a_id: auth-a\n  role_a: primary\n"))
	response, errPick := app.HandleScheduler(context.Background(), cpaapi.SchedulerPickRequest{
		Provider: "gemini", Candidates: []cpaapi.SchedulerAuthCandidate{gatewayCandidate("auth-a", "active")},
	})
	if errPick != nil || response.Handled || response.AuthID != "" {
		t.Fatalf("non-Codex scheduler response = %#v, %v", response, errPick)
	}
}

func TestAppSchedulerHandlesCodexCandidatesOnMixedRoutes(t *testing.T) {
	app := NewApp(nil, nil)
	defer app.Close()
	app.Configure([]byte("personal_gateway:\n  account_a_id: auth-a\n  account_b_id: auth-b\n  role_a: primary\n  role_b: backup\n  mode: force_a\n"))
	response, errPick := app.HandleScheduler(context.Background(), cpaapi.SchedulerPickRequest{
		Providers: []string{"codex", "claude"},
		Candidates: []cpaapi.SchedulerAuthCandidate{
			gatewayCandidate("auth-a", "active"),
			gatewayCandidate("auth-b", "active"),
			{ID: "claude-auth", Provider: "claude", Status: "active"},
		},
	})
	if errPick != nil || !response.Handled || response.AuthID != "auth-a" {
		t.Fatalf("mixed Codex scheduler response = %#v, %v", response, errPick)
	}
}

func TestAppSchedulerDeclinesMixedRoutesWithoutCodex(t *testing.T) {
	app := NewApp(nil, nil)
	defer app.Close()
	app.Configure([]byte("personal_gateway:\n  account_a_id: auth-a\n  role_a: primary\n"))
	response, errPick := app.HandleScheduler(context.Background(), cpaapi.SchedulerPickRequest{
		Providers:  []string{"gemini", "claude"},
		Candidates: []cpaapi.SchedulerAuthCandidate{{ID: "gemini-auth", Provider: "gemini", Status: "active"}},
	})
	if errPick != nil || response.Handled || response.AuthID != "" {
		t.Fatalf("mixed non-Codex scheduler response = %#v, %v", response, errPick)
	}
}
