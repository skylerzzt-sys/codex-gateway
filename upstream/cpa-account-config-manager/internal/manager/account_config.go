package manager

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"cpa-account-config-manager/internal/cpaapi"
)

const maxAccountConfigIDLength = 512

var (
	ErrAccountConfigNotFound = errors.New("account configuration was not found")
	ErrAccountConfigReadOnly = errors.New("account configuration is read-only")
)

type AccountConfigRequest struct {
	AccountID string `json:"account_id"`
}

type AccountConfigUpdateRequest struct {
	AccountID        string            `json:"account_id"`
	Disabled         *bool             `json:"disabled,omitempty"`
	ConcurrencyLimit *int              `json:"concurrency_limit,omitempty"`
	Note             *string           `json:"note,omitempty"`
	Prefix           *string           `json:"prefix,omitempty"`
	ProxyURL         *string           `json:"proxy_url,omitempty"`
	Websockets       *bool             `json:"websockets,omitempty"`
	Headers          *HeaderPatch      `json:"headers,omitempty"`
	ModelPolicy      *ModelPolicyPatch `json:"model_policy,omitempty"`
}

func (a *App) handleAccountStatusUpdate(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request AccountConfigUpdateRequest
	if err := decodeJSONRequest(req.Body, &request); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	account, err := a.accounts.EditableConfig(ctx, request.AccountID)
	if err != nil {
		if errors.Is(err, ErrAccountConfigNotFound) {
			return jsonResponse(http.StatusNotFound, map[string]any{"error": err.Error()})
		}
		if errors.Is(err, ErrAccountConfigReadOnly) {
			return jsonResponse(http.StatusConflict, map[string]any{"error": err.Error()})
		}
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to load account configuration"})
	}
	key := resolveManagementKey(req.Headers)
	if key == "" {
		return jsonResponse(http.StatusUnauthorized, map[string]any{"error": "management key is unavailable"})
	}
	client, err := newManagementClient(resolveManagementBaseURL(a.configSnapshot().ManagementBaseURL), key, a.managementDoer)
	key = ""
	if err != nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "account status update is unavailable"})
	}
	defer client.clearSecrets()
	patch := AccountPatch{Disabled: request.Disabled, Note: request.Note, Prefix: request.Prefix, ProxyURL: request.ProxyURL, Websockets: request.Websockets, Headers: request.Headers, ModelPolicy: request.ModelPolicy, ConcurrencyLimit: request.ConcurrencyLimit}
	validated, err := patch.Validate()
	if err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
	if validated.Disabled != nil {
		if err = client.PatchDisabled(ctx, account.AccountID, *validated.Disabled); err != nil {
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to update account status"})
		}
	}
	if validated.HasFieldUpdates() {
		if err = client.PatchFields(ctx, account.AccountID, validated); err != nil {
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to update account configuration"})
		}
	}
	if validated.HasPluginUpdates() && a.concurrency != nil {
		resolved, errResolve := a.accounts.ResolveTargets(ctx, TargetScope{Mode: "selected", IDs: []string{account.AccountID}})
		if errResolve != nil || len(resolved.Accounts) != 1 {
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to resolve account concurrency target"})
		}
		if err = a.concurrency.SetLimit(resolved.Accounts[0], *validated.ConcurrencyLimit); err != nil {
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to update account concurrency"})
		}
	}
	return jsonResponse(http.StatusOK, map[string]any{"account_id": account.AccountID, "disabled": validated.Disabled})
}

type AccountEditableConfig struct {
	AccountID               string                         `json:"account_id"`
	Disabled                bool                           `json:"disabled"`
	Note                    string                         `json:"note"`
	Prefix                  string                         `json:"prefix"`
	Proxy                   string                         `json:"proxy"`
	ProxyConfigured         bool                           `json:"proxy_configured"`
	Websockets              *bool                          `json:"websockets"`
	HeaderNames             []string                       `json:"header_names"`
	ModelPolicy             *AccountModelPolicySummary     `json:"model_policy"`
	Concurrency             AccountConcurrencySummary      `json:"concurrency"`
	ConcurrencyAvailability AccountConcurrencyAvailability `json:"account_concurrency"`
}

func (s *AccountService) EditableConfig(ctx context.Context, rawAccountID string) (AccountEditableConfig, error) {
	accountID := strings.TrimSpace(rawAccountID)
	if accountID == "" || len(accountID) > maxAccountConfigIDLength {
		return AccountEditableConfig{}, ErrAccountConfigNotFound
	}
	scope, errScope := (TargetScope{Mode: "selected", IDs: []string{accountID}}).Validate()
	if errScope != nil {
		return AccountEditableConfig{}, ErrAccountConfigNotFound
	}
	resolved, errResolve := s.ResolveTargets(ctx, scope)
	if errResolve != nil {
		return AccountEditableConfig{}, errResolve
	}
	if len(resolved.Accounts) != 1 || len(resolved.MissingIDs) != 0 {
		return AccountEditableConfig{}, ErrAccountConfigNotFound
	}
	account := resolved.Accounts[0]
	if !account.Editable {
		return AccountEditableConfig{}, ErrAccountConfigReadOnly
	}
	return AccountEditableConfig{
		AccountID:               account.ID,
		Disabled:                account.Disabled,
		Note:                    account.Note,
		Prefix:                  account.Prefix,
		Proxy:                   account.Proxy,
		ProxyConfigured:         account.ProxyConfigured,
		Websockets:              cloneBoolPointer(account.Websockets),
		HeaderNames:             append([]string{}, account.HeaderNames...),
		ModelPolicy:             cloneAccountModelPolicySummary(account.ModelPolicy),
		Concurrency:             account.Concurrency,
		ConcurrencyAvailability: s.accountConcurrencyAvailability(),
	}, nil
}

func (a *App) handleAccountConfig(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request AccountConfigRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	config, errConfig := a.accounts.EditableConfig(ctx, request.AccountID)
	if errConfig != nil {
		switch {
		case errors.Is(errConfig, ErrAccountConfigNotFound):
			return jsonResponse(http.StatusNotFound, map[string]any{"error": ErrAccountConfigNotFound.Error()})
		case errors.Is(errConfig, ErrAccountConfigReadOnly):
			return jsonResponse(http.StatusConflict, map[string]any{"error": ErrAccountConfigReadOnly.Error()})
		default:
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to load account configuration"})
		}
	}
	response := jsonResponse(http.StatusOK, config)
	response.Headers.Set("Cache-Control", "no-store")
	return response
}
