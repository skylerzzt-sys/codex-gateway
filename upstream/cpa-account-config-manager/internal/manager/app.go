package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

const (
	PluginID                = "cpa-account-config-manager"
	PluginName              = "CPA Account Config Manager"
	DefaultPluginRepository = "https://github.com/Mxucc/cpa-account-config-manager"

	managementRoutePrefix = "/plugins/" + PluginID
	resourceRoutePrefix   = "/v0/resource/plugins/" + PluginID
)

var (
	// PluginVersion is replaced by release builds through the Go linker.
	PluginVersion    = "0.0.0-dev"
	PluginRepository = DefaultPluginRepository
)

type Registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      cpaapi.Metadata          `json:"metadata"`
	Capabilities  RegistrationCapabilities `json:"capabilities"`
}

type RegistrationCapabilities struct {
	ManagementAPI          bool     `json:"management_api"`
	Scheduler              bool     `json:"scheduler"`
	UsagePlugin            bool     `json:"usage_plugin"`
	RequestInterceptor     bool     `json:"request_interceptor"`
	RequestLifecyclePlugin bool     `json:"request_lifecycle_plugin"`
	AuthProvider           bool     `json:"auth_provider"`
	ModelProvider          bool     `json:"model_provider"`
	Executor               bool     `json:"executor"`
	ExecutorModelScope     string   `json:"executor_model_scope,omitempty"`
	ExecutorInputFormats   []string `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats  []string `json:"executor_output_formats,omitempty"`
}

type App struct {
	mu                sync.RWMutex
	config            Config
	accounts          *AccountService
	deduplication     *AccountDeduplicationService
	deletions         *AccountDeleteService
	tokenRefresh      *AccountTokenRefreshService
	updates           *UpdateChecker
	imports           *ImportService
	usage             *UsageTracker
	creditUsage       *Sub2APICreditUsage
	operations        *OperationJournal
	modelTests        *ModelTestService
	quotaBootstrap    *accountQuotaMetadataBootstrap
	gatewayQuota      *personalGatewayQuotaRefreshWorker
	quotaTransport    quotaHTTPTransport
	managementDoer    HTTPDoer
	requestHooks      *RequestHook
	concurrency       *AccountConcurrencyService
	mutations         *MutationCoordinator
	personalOverdraft *PersonalOverdraftTracker
	hostSchema        uint32
	runtime           *RuntimeOwnership
	experiments       *ExperimentalSettingsService
	agentIdentity     *AgentIdentityExperiment
	indexHTML         []byte
	quiesceOnce       sync.Once
	quotaResetLocks   [64]sync.Mutex
}

func NewApp(host AuthHost, indexHTML []byte) *App {
	usage := NewUsageTracker()
	creditUsage := NewSub2APICreditUsage()
	usage.SetCreditCalculator(creditUsage)
	accounts := NewAccountService(host, usage)
	concurrency := NewAccountConcurrencyService()
	accounts.SetAccountConcurrency(concurrency)
	mutations := NewMutationCoordinator()
	modelTests := NewModelTestService(accounts, usage)
	deletions := NewAccountDeleteService(accounts, mutations)
	operations := NewOperationJournal()
	experiments := NewExperimentalSettingsService()
	quotaBootstrap := NewAccountQuotaMetadataBootstrap()
	gatewayQuota := newPersonalGatewayQuotaRefreshWorker()
	var quotaTransport quotaHTTPTransport
	if transport, ok := host.(quotaHTTPTransport); ok {
		quotaTransport = transport
	}
	var identityTransport AgentIdentityTransport
	if transport, ok := host.(AgentIdentityTransport); ok {
		identityTransport = transport
	}
	agentIdentity := NewAgentIdentityExperiment(experiments.AgentIdentityEnabled, identityTransport)
	modelTests.SetAgentIdentityExperiment(agentIdentity)
	imports := NewImportService(host, mutations)
	imports.SetAgentIdentityExperiment(agentIdentity)
	personalOverdraft := NewPersonalOverdraftTracker()
	weeklyOverdraft := NewWeeklyOverdraftExperiment(func() bool {
		return experiments.WeeklyOverdraftEnabled() && !personalOverdraft.Enabled()
	})
	requestHooks := NewRequestHook(concurrency, weeklyOverdraft, personalOverdraft)
	runtimeMarker := ""
	if provider, ok := host.(interface{ RuntimeProcessMarker() string }); ok {
		runtimeMarker = provider.RuntimeProcessMarker()
	}
	runtime := NewRuntimeOwnershipWithMarker(PluginVersion, runtimeMarker)
	updates := NewUpdateChecker(PluginVersion)
	updates.SetRuntimeOwnership(runtime)
	modelTests.SetExperimentalTransformer(weeklyOverdraft)
	app := &App{
		config:            normalizeConfig(Config{}),
		accounts:          accounts,
		deduplication:     NewAccountDeduplicationService(accounts),
		deletions:         deletions,
		tokenRefresh:      NewAccountTokenRefreshService(accounts, host),
		updates:           updates,
		imports:           imports,
		usage:             usage,
		creditUsage:       creditUsage,
		operations:        operations,
		modelTests:        modelTests,
		quotaBootstrap:    quotaBootstrap,
		gatewayQuota:      gatewayQuota,
		quotaTransport:    quotaTransport,
		requestHooks:      requestHooks,
		concurrency:       concurrency,
		mutations:         mutations,
		personalOverdraft: personalOverdraft,
		hostSchema:        cpaapi.SchemaVersion,
		runtime:           runtime,
		experiments:       experiments,
		agentIdentity:     agentIdentity,
		indexHTML:         append([]byte(nil), indexHTML...),
	}
	accounts.SetObserver(accountObserverGroup{quotaBootstrap})
	quotaBootstrap.SetHandler(app.runNewAccountQuotaMetadata)
	gatewayQuota.SetHandler(app.refreshPersonalGatewayQuotas)
	runtime.SetOnSuperseded(app.quiesceRetiredInstance)
	return app
}

func (a *App) Configure(raw []byte) {
	a.ConfigureHost(raw, cpaapi.SchemaVersion)
}

func (a *App) ConfigureHost(raw []byte, hostSchema uint32) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.config = ParseConfig(raw)
	if errValidate := a.config.PersonalGateway.Validate(); errValidate != nil && a.config.PersonalGateway.AccountAID == "" && a.config.PersonalGateway.AccountBID == "" {
		fmt.Printf("configuration error: personal gateway scheduler enabled but A/B accounts are not configured: %v\n", errValidate)
	}
	config := a.config
	a.hostSchema = normalizeHostSchemaVersion(hostSchema)
	hostSchema = a.hostSchema
	a.mu.Unlock()
	a.concurrency.Configure(config, hostSchema)
	a.runtime.Configure(config)
	if a.runtime.Snapshot().Superseded {
		a.quiesceRetiredInstance()
		return
	}
	a.quotaBootstrap.SetBackgroundWorkOwner(a.runtime)
	a.gatewayQuota.SetBackgroundWorkOwner(a.runtime)
	a.operations.Configure(config)
	a.experiments.Configure(config)
	a.personalOverdraft.Configure(config.PersonalGateway.OverdraftEnabled && hostSchema >= cpaapi.SchemaVersion, config.DataDir)
	a.creditUsage.Configure(config, a.experiments.Sub2APICreditUsageEnabled())
	a.quotaBootstrap.Start()
	a.gatewayQuota.Start()
	a.updates.Configure(config)
	a.usage.Configure(config)
	a.reconcileOperationSources()
}

func (a *App) configSnapshot() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

func (a *App) HandleUsage(record cpaapi.UsageRecord) {
	if a == nil || a.usage == nil {
		return
	}
	if a.runtime != nil && a.runtime.Snapshot().Superseded {
		return
	}
	a.usage.Observe(record)
	a.personalOverdraft.ObserveUsage(record)
}

func (a *App) Close() {
	if a == nil {
		return
	}
	a.quiesceRetiredInstance()
	a.reconcileOperationSources()
	a.runtime.Shutdown()
}

func (a *App) quiesceRetiredInstance() {
	if a == nil {
		return
	}
	a.quiesceOnce.Do(func() {
		superseded := a.runtime != nil && a.runtime.Snapshot().Superseded
		a.quotaBootstrap.Shutdown()
		a.gatewayQuota.Shutdown()
		a.updates.Shutdown()
		a.deletions.Clear()
		a.imports.Shutdown()
		a.agentIdentity.Clear()
		a.concurrency.Shutdown()
		a.creditUsage.Close()
		a.usage.Close()
		if superseded {
			debug.FreeOSMemory()
		}
	})
}

func (a *App) Registration() Registration {
	a.mu.RLock()
	hostSchema := normalizeHostSchemaVersion(a.hostSchema)
	a.mu.RUnlock()
	registrationSchema := cpaapi.LegacySchemaVersion
	requestLifecycle := false
	if hostSchema >= cpaapi.SchemaVersion {
		registrationSchema = cpaapi.SchemaVersion
		requestLifecycle = true
	}
	return Registration{
		SchemaVersion: registrationSchema,
		Metadata: cpaapi.Metadata{
			Name:             PluginName,
			Version:          PluginVersion,
			Author:           "cpa-account-config-manager contributors",
			GitHubRepository: PluginRepository,
			ConfigFields: []cpaapi.ConfigField{
				{Name: "workers", Type: cpaapi.ConfigFieldTypeInteger, Description: "Optional maximum concurrent account mutations (default 6, range 1-16)."},
				{Name: "data_dir", Type: cpaapi.ConfigFieldTypeString, Description: "Optional writable directory for sanitized job, policy, usage, update, and operation-journal state."},
				{Name: "management_base_url", Type: cpaapi.ConfigFieldTypeString, Description: "Optional loopback CLIProxyAPI Management API base URL; defaults to http://127.0.0.1:8317."},
				{Name: "gateway_account_a_id", Type: cpaapi.ConfigFieldTypeString, Description: "Stable Auth ID for personal gateway account A; never a token or Auth JSON."},
				{Name: "gateway_account_b_id", Type: cpaapi.ConfigFieldTypeString, Description: "Stable Auth ID for personal gateway account B; never a token or Auth JSON."},
				{Name: "gateway_role_a", Type: cpaapi.ConfigFieldTypeEnum, EnumValues: []string{"primary", "backup", "disabled"}, Description: "Routing role for account A."},
				{Name: "gateway_role_b", Type: cpaapi.ConfigFieldTypeEnum, EnumValues: []string{"primary", "backup", "disabled"}, Description: "Routing role for account B."},
				{Name: "gateway_mode", Type: cpaapi.ConfigFieldTypeEnum, EnumValues: []string{"auto", "force_a", "force_b"}, Description: "Auto drains Primary first; Force modes fail hard when the target is unavailable."},
				{Name: "gateway_overdraft_enabled", Type: cpaapi.ConfigFieldTypeBoolean, Description: "Enable one real-business overdraft probe per 5h or 7d quota cycle."},
			},
		},
		Capabilities: RegistrationCapabilities{
			ManagementAPI: true, Scheduler: true, UsagePlugin: true, RequestInterceptor: true, RequestLifecyclePlugin: requestLifecycle,
			AuthProvider: true, ModelProvider: true, Executor: true,
			ExecutorModelScope:    "oauth",
			ExecutorInputFormats:  []string{"codex"},
			ExecutorOutputFormats: []string{"codex"},
		},
	}
}

func (a *App) HandleScheduler(ctx context.Context, request cpaapi.SchedulerPickRequest) (cpaapi.SchedulerPickResponse, error) {
	if a == nil {
		return cpaapi.SchedulerPickResponse{}, newPersonalGatewayError(personalGatewayRuntimeUnavailableCode, "personal gateway runtime is unavailable")
	}
	candidates, handlesCodex := personalGatewayCodexCandidates(request)
	if !handlesCodex {
		return cpaapi.SchedulerPickResponse{Handled: false}, nil
	}
	return pickPersonalGatewayAuth(ctx, a.configSnapshot().PersonalGateway, candidates)
}

func personalGatewayCodexCandidates(request cpaapi.SchedulerPickRequest) ([]cpaapi.SchedulerAuthCandidate, bool) {
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	if provider != "" && provider != "codex" {
		return nil, false
	}
	if provider == "" {
		hasCodex := false
		for _, candidateProvider := range request.Providers {
			if strings.EqualFold(strings.TrimSpace(candidateProvider), "codex") {
				hasCodex = true
				break
			}
		}
		if !hasCodex {
			return nil, false
		}
	}

	candidates := make([]cpaapi.SchedulerAuthCandidate, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		candidateProvider := strings.TrimSpace(candidate.Provider)
		if strings.EqualFold(candidateProvider, "codex") || provider == "codex" && candidateProvider == "" {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	return candidates, true
}

func (a *App) HandleRequestBefore(request cpaapi.RequestInterceptRequest) cpaapi.RequestInterceptResponse {
	if a == nil || a.requestHooks == nil {
		return cpaapi.RequestInterceptResponse{}
	}
	return a.requestHooks.InterceptBefore(request)
}

func (a *App) RequestInterceptionActive() bool {
	return a != nil && a.requestHooks != nil && a.requestHooks.Active()
}

func (a *App) RequestInterceptionAcceptsFormat(format string) bool {
	return a != nil && a.requestHooks != nil && a.requestHooks.AcceptsFormat(format)
}

func (a *App) HandleRequestAfter(request cpaapi.RequestInterceptRequest) cpaapi.RequestInterceptResponse {
	if a == nil || a.requestHooks == nil {
		return cpaapi.RequestInterceptResponse{}
	}
	return a.requestHooks.InterceptAfter(request)
}

func (a *App) HandleRequestComplete(completion cpaapi.RequestCompletion) {
	if a == nil {
		return
	}
	if a.concurrency != nil {
		a.concurrency.Complete(completion)
	}
	if a.personalOverdraft != nil {
		a.personalOverdraft.Complete(completion)
	}
}

func (a *App) RequestCompletionActive() bool {
	return a != nil && (a.concurrency != nil && a.concurrency.RequestInterceptionActive() ||
		a.personalOverdraft != nil && a.personalOverdraft.Enabled())
}

func (a *App) HandleAgentIdentityAuthParse(request cpaapi.AuthParseRequest) (cpaapi.AuthParseResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.AuthParseResponse{}, nil
	}
	return a.agentIdentity.ParseAuth(request.RawJSON)
}

func (a *App) HandleAgentIdentityAuthRefresh(request cpaapi.AuthRefreshRequest) (cpaapi.AuthRefreshResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.AuthRefreshResponse{}, fmt.Errorf("Agent Identity experiment is unavailable")
	}
	return a.agentIdentity.RefreshAuth(request)
}

func (a *App) HandleAgentIdentityLoginStart(request cpaapi.AuthLoginStartRequest) (cpaapi.AuthLoginStartResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.AuthLoginStartResponse{}, fmt.Errorf("Agent Identity experiment is unavailable")
	}
	return a.agentIdentity.StartLogin(request)
}

func (a *App) HandleAgentIdentityLoginPoll(request cpaapi.AuthLoginPollRequest) (cpaapi.AuthLoginPollResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.AuthLoginPollResponse{Status: "error", Message: "Agent Identity experiment is unavailable"}, nil
	}
	return a.agentIdentity.PollLogin(request), nil
}

func (a *App) HandleAgentIdentityModels(request cpaapi.AuthModelRequest) (cpaapi.ModelResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.ModelResponse{Provider: agentIdentityProvider}, nil
	}
	return a.agentIdentity.ModelsForAuth(request)
}

func (a *App) HandleAgentIdentityExecute(ctx context.Context, request cpaapi.ExecutorRequest) (cpaapi.ExecutorResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.ExecutorResponse{}, fmt.Errorf("Agent Identity experiment is unavailable")
	}
	return a.agentIdentity.Execute(ctx, request)
}

func (a *App) HandleAgentIdentityExecuteStream(ctx context.Context, request cpaapi.ExecutorRequest) (cpaapi.ExecutorStreamResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.ExecutorStreamResponse{}, fmt.Errorf("Agent Identity experiment is unavailable")
	}
	return a.agentIdentity.ExecuteStream(ctx, request)
}

func (a *App) HandleAgentIdentityHTTPRequest(ctx context.Context, request cpaapi.ExecutorHTTPRequest) (cpaapi.ExecutorHTTPResponse, error) {
	if a == nil || a.agentIdentity == nil {
		return cpaapi.ExecutorHTTPResponse{}, fmt.Errorf("Agent Identity experiment is unavailable")
	}
	return a.agentIdentity.HTTPRequest(ctx, request)
}

func (a *App) ManagementRegistration() cpaapi.ManagementRegistrationResponse {
	return cpaapi.ManagementRegistrationResponse{
		Routes: []cpaapi.ManagementRoute{
			{Method: http.MethodGet, Path: managementRoutePrefix + "/accounts", Description: "List redacted CLIProxyAPI accounts."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/config", Description: "Read one editable account's current allow-listed configuration."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/status", Description: "Update one editable account's enabled status."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/quota-metadata/refresh", Description: "Refresh one Codex account's CPA-native plan and active reset metadata."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/quota-metadata/reset", Description: "Consume one explicitly confirmed Codex active reset credit and refresh quota metadata."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/models", Description: "Load the common effective model catalog for an editable account scope."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/deduplicate/preview", Description: "Find duplicate upstream accounts and return a redacted review plan."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/model-test", Description: "Run one bounded account-specific model availability probe through CLIProxyAPI."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/token/refresh", Description: "Refresh one editable account through CPA's native credential refresh coordinator."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/delete/preview", Description: "Preview deletion of one editable physical Auth file."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/accounts/delete/start", Description: "Delete one confirmed unchanged physical Auth file."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/export/accounts", Description: "Export filtered account credentials for an explicitly selected target format."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/import/preview", Description: "Preview JSON or ZIP conversion into CPA Auth files."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/import/start", Description: "Start importing a confirmed converted Auth-file preview in the background."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/import/status", Description: "Read current or last background import progress."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/updates", Description: "Read plugin release and update-check status."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/updates/check", Description: "Record an immediate CPA plugin-store update check."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/experiments", Description: "Read removable experimental feature settings."},
			{Method: http.MethodPut, Path: managementRoutePrefix + "/experiments", Description: "Persist removable experimental feature settings."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/experiments/agent-identity/session-login", Description: "Convert one explicitly submitted ChatGPT Session JSON into a pending Agent Identity login credential."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/operations", Description: "List the persistent sanitized account-manager operation journal."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/operations/export", Description: "Export the sanitized operation journal as JSON, CSV, or JSON Lines."},
			{Method: http.MethodGet, Path: managementRoutePrefix + "/operations/settings", Description: "Read operation-journal retention settings."},
			{Method: http.MethodPut, Path: managementRoutePrefix + "/operations/settings", Description: "Persist operation-journal retention settings."},
			{Method: http.MethodDelete, Path: managementRoutePrefix + "/operations", Description: "Clear the operation journal while retaining a clear audit event."},
			{Method: http.MethodPost, Path: managementRoutePrefix + "/operations/record", Description: "Record a strict browser-owned plugin-store update outcome."},
		},
		Resources: []cpaapi.ResourceRoute{{
			Path:        "/index.html",
			Menu:        "CPA-A Manager",
			Description: "List, filter, and safely batch-edit CLIProxyAPI account configuration.",
		}},
	}
}

func (a *App) HandleManagement(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	if a == nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "plugin runtime is unavailable"})
	}
	if a.runtime != nil && a.runtime.Snapshot().Superseded {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{
			"error": "plugin runtime has been superseded; restart CPA and retry",
		})
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := normalizedRequestPath(req.Path)

	switch {
	case method == http.MethodGet && path == resourceRoutePrefix+"/index.html":
		return cpaapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       append([]byte(nil), a.indexHTML...),
		}
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/accounts":
		return a.handleListAccounts(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/config":
		return a.handleAccountConfig(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/status":
		return a.handleAccountStatusUpdate(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/quota-metadata/refresh":
		return a.handleAccountQuotaMetadata(ctx, req, false)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/quota-metadata/reset":
		return a.handleAccountQuotaMetadata(ctx, req, true)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/models":
		return a.handleAccountModels(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/deduplicate/preview":
		return a.handleAccountDeduplicationPreview(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/model-test":
		return a.handleAccountModelTest(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/token/refresh":
		return a.handleAccountTokenRefresh(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/delete/preview":
		return a.handleAccountDeletePreview(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/accounts/delete/start":
		return a.handleAccountDeleteStart(ctx, req)
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/export/accounts":
		return a.handleExportAccounts(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/import/preview":
		return a.handleImportPreview(ctx, req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/import/start":
		return a.handleImportStart(ctx, req)
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/import/status":
		return jsonResponse(http.StatusOK, a.imports.Status())
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/updates":
		return jsonResponse(http.StatusOK, a.updates.Snapshot())
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/updates/check":
		return jsonResponse(http.StatusAccepted, a.updates.RequestCheck())
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/experiments":
		return jsonResponse(http.StatusOK, a.experiments.Snapshot())
	case method == http.MethodPut && path == "/v0/management"+managementRoutePrefix+"/experiments":
		return a.handlePutExperimentalSettings(req)
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/experiments/agent-identity/session-login":
		return a.handleAgentIdentitySessionLogin(ctx, req)
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/operations":
		return a.handleListOperations(req)
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/operations/export":
		return a.handleExportOperations(req)
	case method == http.MethodGet && path == "/v0/management"+managementRoutePrefix+"/operations/settings":
		return jsonResponse(http.StatusOK, a.operations.RetentionSettings())
	case method == http.MethodPut && path == "/v0/management"+managementRoutePrefix+"/operations/settings":
		return a.handlePutOperationRetentionSettings(req)
	case method == http.MethodDelete && path == "/v0/management"+managementRoutePrefix+"/operations":
		return a.handleClearOperations()
	case method == http.MethodPost && path == "/v0/management"+managementRoutePrefix+"/operations/record":
		return a.handleRecordOperation(req)
	default:
		return jsonResponse(http.StatusNotFound, map[string]any{
			"error":  "not found",
			"method": method,
			"path":   path,
		})
	}
}

func (a *App) handleAccountDeduplicationPreview(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var options AccountDeduplicationOptions
	if errDecode := decodeJSONRequest(req.Body, &options); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	preview, errPreview := a.deduplication.Preview(ctx, options)
	if errPreview != nil {
		switch {
		case errors.Is(errPreview, ErrDeduplicationTooLarge):
			return jsonResponse(http.StatusRequestEntityTooLarge, map[string]any{"error": errPreview.Error()})
		default:
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to analyze account identities"})
		}
	}
	return jsonResponse(http.StatusOK, preview)
}

func (a *App) handleListOperations(req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	a.reconcileOperationSources()
	query := operationQueryFromRequest(req, operationPageSize)
	return jsonResponse(http.StatusOK, a.operations.List(query))
}

func (a *App) handleExportOperations(req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	a.reconcileOperationSources()
	format := firstQuery(req.Query, "format")
	if format == "" {
		format = "json"
	}
	query := operationQueryFromRequest(req, operationPageSize)
	query.Page = 1
	query.PageSize = operationPageSize
	entries, errSnapshot := a.operations.ExportSnapshot(query)
	if errSnapshot != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]any{"error": "operation journal could not be exported"})
	}
	download, errRender := renderOperationExport(format, entries, time.Now().UTC())
	if errRender != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errRender.Error()})
	}
	return cpaapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type":          []string{download.ContentType},
			"Content-Disposition":   []string{fmt.Sprintf(`attachment; filename="%s"`, download.Filename)},
			"X-Exported-Operations": []string{strconv.Itoa(download.Count)},
		},
		Body: download.Body,
	}
}

func (a *App) handlePutOperationRetentionSettings(req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request OperationRetentionUpdateRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	if request.ExtendedHistory == nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "extended_history is required"})
	}
	settings, errUpdate := a.operations.UpdateRetentionSettings(*request.ExtendedHistory)
	if errUpdate != nil {
		return jsonResponse(http.StatusInternalServerError, map[string]any{"error": "operation journal settings could not be persisted"})
	}
	return jsonResponse(http.StatusOK, settings)
}

func operationQueryFromRequest(req cpaapi.ManagementRequest, pageSize int) OperationQuery {
	return OperationQuery{
		Page:     intQuery(req.Query, "page", 1),
		PageSize: intQuery(req.Query, "page_size", pageSize),
		Category: firstQuery(req.Query, "category"),
		Status:   firstQuery(req.Query, "status"),
		Source:   firstQuery(req.Query, "source"),
		Search:   firstQuery(req.Query, "search"),
	}
}

func (a *App) handleClearOperations() cpaapi.ManagementResponse {
	entry := a.operations.Clear()
	return jsonResponse(http.StatusOK, map[string]any{"operation": entry, "retained": 1})
}

func (a *App) handleRecordOperation(req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request OperationRecordRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	entry, errValidate := validateBrowserOperationRecord(request)
	if errValidate != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errValidate.Error()})
	}
	now := time.Now().UTC()
	entry.StartedAt = now
	entry.FinishedAt = now
	recorded := a.operations.Record(entry)
	return jsonResponse(http.StatusCreated, recorded)
}

func (a *App) handleImportPreview(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	uploads, multipartUpload, errUploads := importUploadsFromRequest(req, a.imports.limits)
	if errUploads != nil {
		status := http.StatusBadRequest
		if strings.Contains(errUploads.Error(), "exceeds") || strings.Contains(errUploads.Error(), "more than") {
			status = http.StatusRequestEntityTooLarge
		}
		return jsonResponse(status, map[string]any{"error": errUploads.Error()})
	}
	var preview ImportPreview
	var errPreview error
	if multipartUpload {
		preview, errPreview = a.imports.PreviewMany(ctx, uploads)
	} else {
		preview, errPreview = a.imports.Preview(ctx, uploads[0])
	}
	if errPreview != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(errPreview, ErrImportNoAccounts):
			status = http.StatusUnprocessableEntity
		case errors.Is(errPreview, ErrImportAuthUnavailable):
			status = http.StatusBadGateway
		case strings.Contains(errPreview.Error(), "exceeds") || strings.Contains(errPreview.Error(), "more than"):
			status = http.StatusRequestEntityTooLarge
		}
		return jsonResponse(status, map[string]any{"error": errPreview.Error()})
	}
	return jsonResponse(http.StatusOK, preview)
}

func (a *App) handleImportStart(_ context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	startedAt := time.Now().UTC()
	var request ImportStartRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	managementKey := resolveManagementKey(req.Headers)
	backgroundKey := managementKey
	result, errStart := a.imports.StartAsync(request.PreviewID, func(started ImportResult) {
		a.operations.Upsert("import:"+started.ID, OperationEntry{
			Category: OperationCategoryImport, Action: OperationActionImport, Status: OperationStatusRunning,
			Source: OperationSourceImport, Scope: OperationScopeAll, TargetCount: started.Total, StartedAt: started.StartedAt,
			ReasonCode: "running",
		})
	}, func(ctx context.Context, completed ImportResult, errRun error) ImportResult {
		defer func() { backgroundKey = "" }()
		return a.completeImport(ctx, completed, errRun, backgroundKey)
	})
	managementKey = ""
	if errStart != nil {
		backgroundKey = ""
		a.operations.Record(OperationEntry{
			Category: OperationCategoryImport, Action: OperationActionImport, Status: OperationStatusFailed,
			Source: OperationSourceImport, Scope: OperationScopeAll, Failed: 1, StartedAt: startedAt,
			FinishedAt: time.Now().UTC(), ReasonCode: "operation_failed",
		})
		status := http.StatusInternalServerError
		switch {
		case errors.Is(errStart, ErrImportPreviewExpired):
			status = http.StatusGone
		case errors.Is(errStart, ErrImportPreviewNotFound):
			status = http.StatusNotFound
		case errors.Is(errStart, ErrJobBusy):
			status = http.StatusConflict
		case errors.Is(errStart, ErrImportAuthUnavailable):
			status = http.StatusBadGateway
		}
		return jsonResponse(status, map[string]any{"error": errStart.Error()})
	}
	return jsonResponse(http.StatusAccepted, result)
}

func (a *App) completeImport(_ context.Context, result ImportResult, _ error, _ string) ImportResult {
	a.operations.Upsert("import:"+result.ID, OperationEntry{
		Category: OperationCategoryImport, Action: OperationActionImport, Status: operationStatusFromJobState(result.State),
		Source: OperationSourceImport, Scope: OperationScopeAll, TargetCount: result.Total, Succeeded: result.Imported,
		Failed: result.Failed, Skipped: result.Skipped, StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
		ReasonCode: operationReasonFromJobState(result.State),
	})
	return result
}

func (a *App) handleListAccounts(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	managementKey := resolveManagementKey(req.Headers)
	a.quotaBootstrap.Arm(managementKey)
	managementKey = ""
	query, errQuery := listQueryFromValues(req.Query)
	if errQuery != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errQuery.Error()})
	}
	response, errList := a.accounts.List(ctx, query)
	if errList != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to load accounts"})
	}
	return jsonResponse(http.StatusOK, response)
}

func (a *App) handleAccountModels(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request AccountModelCatalogRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	scope, errScope := request.Scope.Validate()
	if errScope != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errScope.Error()})
	}
	resolved, errResolve := a.accounts.ResolveTargets(ctx, scope)
	if errResolve != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to resolve target accounts"})
	}
	if len(resolved.Accounts)+len(resolved.MissingIDs) == 0 {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "scope matched no accounts"})
	}
	if len(resolved.Accounts) > maxModelCatalogTargets {
		return jsonResponse(http.StatusRequestEntityTooLarge, map[string]any{"error": fmt.Sprintf("model catalog scope exceeds %d accounts", maxModelCatalogTargets)})
	}
	managementKey := resolveManagementKey(req.Headers)
	if managementKey == "" {
		return jsonResponse(http.StatusUnauthorized, map[string]any{"error": "management key is unavailable"})
	}
	config := a.configSnapshot()
	client, errClient := newManagementClient(resolveManagementBaseURL(config.ManagementBaseURL), managementKey, a.managementDoer)
	managementKey = ""
	if errClient != nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "account model catalog is unavailable"})
	}
	defer client.clearSecrets()

	eligible := make([]Account, 0, len(resolved.Accounts))
	for _, account := range resolved.Accounts {
		if account.Editable {
			eligible = append(eligible, account)
		}
	}
	readOnly := len(resolved.Accounts) - len(eligible)
	response := AccountModelCatalogResponse{
		Models:   []AccountModelOption{},
		Total:    len(resolved.Accounts) + len(resolved.MissingIDs),
		Eligible: len(eligible),
		ReadOnly: readOnly,
		Missing:  len(resolved.MissingIDs),
	}
	if len(eligible) == 1 && len(resolved.Accounts) == 1 && len(resolved.MissingIDs) == 0 {
		response.CurrentPolicy = eligible[0].ModelPolicy
		if response.CurrentPolicy == nil {
			response.CurrentPolicy = &AccountModelPolicySummary{Mode: ModelPolicyModeAll}
		}
	}
	if readOnly > 0 {
		response.Warnings = append(response.Warnings, fmt.Sprintf("%d read-only target(s) were skipped", readOnly))
	}
	if len(resolved.MissingIDs) > 0 {
		response.Warnings = append(response.Warnings, fmt.Sprintf("%d missing target(s) were skipped", len(resolved.MissingIDs)))
	}
	if len(eligible) == 0 {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "scope contains no editable accounts"})
	}
	catalogs, failed := loadCommonAccountModels(ctx, a.accounts, eligible, client, config.Workers)
	response.Loaded = len(catalogs)
	response.Failed = failed
	if failed > 0 {
		response.Warnings = append(response.Warnings, fmt.Sprintf("%d account model catalog(s) could not be loaded", failed))
	}
	if len(catalogs) == 0 {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to load account model catalogs"})
	}
	response.Models = commonAccountModels(catalogs)
	if len(response.Models) == 0 {
		response.Warnings = append(response.Warnings, "the loaded accounts have no common models")
	}
	return jsonResponse(http.StatusOK, response)
}

func (a *App) handleAccountDeletePreview(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request AccountDeletePreviewRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	if strings.TrimSpace(request.ID) == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "account id is required"})
	}
	preview, errPreview := a.deletions.Preview(ctx, request)
	if errPreview != nil {
		switch {
		case errors.Is(errPreview, ErrAccountDeleteTargetNotFound):
			return jsonResponse(http.StatusNotFound, map[string]any{"error": ErrAccountDeleteTargetNotFound.Error()})
		case errors.Is(errPreview, ErrAccountDeleteTargetReadOnly):
			return jsonResponse(http.StatusBadRequest, map[string]any{"error": ErrAccountDeleteTargetReadOnly.Error()})
		case strings.Contains(errPreview.Error(), "resolve account for deletion"):
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": "failed to resolve account for deletion"})
		default:
			return jsonResponse(http.StatusInternalServerError, map[string]any{"error": "failed to create delete preview"})
		}
	}
	return jsonResponse(http.StatusOK, preview)
}

func (a *App) handleAccountDeleteStart(ctx context.Context, req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var request AccountDeleteStartRequest
	if errDecode := decodeJSONRequest(req.Body, &request); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	if strings.TrimSpace(request.PreviewID) == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "preview_id is required"})
	}
	managementKey := resolveManagementKey(req.Headers)
	if managementKey == "" {
		return jsonResponse(http.StatusUnauthorized, map[string]any{"error": "management key is unavailable"})
	}
	config := a.configSnapshot()
	result, errDelete := a.deletions.Start(ctx, request.PreviewID, config.ManagementBaseURL, managementKey)
	managementKey = ""
	if errDelete != nil {
		now := time.Now().UTC()
		a.operations.Record(OperationEntry{
			Category: OperationCategoryAccount, Action: OperationActionDelete, Status: OperationStatusFailed,
			Source: OperationSourceManual, Scope: OperationScopeSingle, TargetCount: 1, Failed: 1,
			StartedAt: now, FinishedAt: now, ReasonCode: "operation_failed",
		})
		switch {
		case errors.Is(errDelete, ErrAccountDeletePreviewExpired):
			return jsonResponse(http.StatusGone, map[string]any{"error": "delete preview expired; create a new preview"})
		case errors.Is(errDelete, ErrAccountDeletePreviewNotFound):
			return jsonResponse(http.StatusNotFound, map[string]any{"error": ErrAccountDeletePreviewNotFound.Error()})
		case errors.Is(errDelete, ErrAccountDeletePreviewStale), errors.Is(errDelete, ErrAccountDeleteBusy):
			return jsonResponse(http.StatusConflict, map[string]any{"error": errDelete.Error()})
		case errors.Is(errDelete, ErrManagementBaseURLInvalid):
			return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": ErrManagementBaseURLInvalid.Error()})
		case errors.Is(errDelete, ErrAccountDeleteFailed):
			return jsonResponse(http.StatusBadGateway, map[string]any{"error": ErrAccountDeleteFailed.Error()})
		default:
			return jsonResponse(http.StatusInternalServerError, map[string]any{"error": "failed to delete account"})
		}
	}
	a.operations.Record(OperationEntry{
		Category: OperationCategoryAccount, Action: OperationActionDelete, Status: OperationStatusSucceeded,
		Source: OperationSourceManual, Scope: OperationScopeSingle, TargetID: result.Account.ID, TargetCount: 1,
		Succeeded: 1, StartedAt: result.DeletedAt, FinishedAt: result.DeletedAt, ReasonCode: "completed",
	})
	return jsonResponse(http.StatusOK, result)
}

func (a *App) handlePutExperimentalSettings(req cpaapi.ManagementRequest) cpaapi.ManagementResponse {
	var settings ExperimentalSettings
	if errDecode := decodeJSONRequest(req.Body, &settings); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errDecode.Error()})
	}
	snapshot, errSave := a.experiments.Set(settings)
	if errSave != nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "experimental settings could not be persisted"})
	}
	a.creditUsage.SetEnabled(snapshot.Settings.Sub2APICreditUsageEnabled)
	return jsonResponse(http.StatusOK, snapshot)
}

func listQueryFromValues(values map[string][]string) (ListQuery, error) {
	query := ListQuery{}
	query.Page = intQuery(values, "page", 1)
	query.PageSize = intQuery(values, "page_size", defaultPageSize)
	query.Filters.Provider = firstQuery(values, "provider")
	query.Filters.Type = firstQuery(values, "type")
	query.Filters.Status = firstQuery(values, "status")
	query.Filters.Editability = firstQuery(values, "editability")
	query.Filters.Source = firstQuery(values, "source")
	query.Filters.Search = firstQuery(values, "search")
	query.SortBy = AccountSortField(strings.ToLower(firstQuery(values, "sort_by")))
	if query.SortBy == "" {
		query.SortBy = AccountSortAccount
	}
	if !validAccountSortField(query.SortBy) {
		return ListQuery{}, fmt.Errorf("unsupported account sort field")
	}
	query.SortOrder = AccountSortOrder(strings.ToLower(firstQuery(values, "sort_order")))
	if query.SortOrder == "" {
		query.SortOrder = AccountSortAscending
	}
	if query.SortOrder != AccountSortAscending && query.SortOrder != AccountSortDescending {
		return ListQuery{}, fmt.Errorf("sort_order must be asc or desc")
	}
	if rawDisabled := firstQuery(values, "disabled"); rawDisabled != "" {
		disabled, errParse := strconv.ParseBool(rawDisabled)
		if errParse != nil {
			return ListQuery{}, fmt.Errorf("disabled must be true or false")
		}
		query.Filters.Disabled = &disabled
	}
	return query, nil
}

func firstQuery(values map[string][]string, key string) string {
	items := values[key]
	if len(items) == 0 {
		return ""
	}
	return strings.TrimSpace(items[0])
}

func intQuery(values map[string][]string, key string, fallback int) int {
	raw := firstQuery(values, key)
	if raw == "" {
		return fallback
	}
	parsed, errParse := strconv.Atoi(raw)
	if errParse != nil {
		return fallback
	}
	return parsed
}

func normalizedRequestPath(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	if index := strings.IndexByte(path, '?'); index >= 0 {
		path = path[:index]
	}
	if strings.HasPrefix(path, managementRoutePrefix+"/") {
		return "/v0/management" + path
	}
	return path
}

func jsonResponse(statusCode int, payload any) cpaapi.ManagementResponse {
	raw, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		statusCode = http.StatusInternalServerError
		raw = []byte(`{"error":"failed to encode response"}`)
	}
	return cpaapi.ManagementResponse{
		StatusCode: statusCode,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       raw,
	}
}

func decodeJSONRequest(raw []byte, destination any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("request body is required")
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("request body exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if errDecode := decoder.Decode(destination); errDecode != nil {
		return fmt.Errorf("invalid request body: %w", errDecode)
	}
	var trailing any
	if errTrailing := decoder.Decode(&trailing); !errors.Is(errTrailing, io.EOF) {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}
