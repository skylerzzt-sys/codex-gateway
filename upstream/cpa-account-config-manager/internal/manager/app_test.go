package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

type runtimeMarkerHost struct {
	*fakeAuthHost
	marker string
}

func (h *runtimeMarkerHost) RuntimeProcessMarker() string { return h.marker }

func TestNewAppUsesTheHostProcessMarkerForRuntimeOwnership(t *testing.T) {
	marker := "0123456789abcdef0123456789abcdef"
	app := NewApp(&runtimeMarkerHost{fakeAuthHost: &fakeAuthHost{}, marker: marker}, []byte("index"))
	defer app.Close()
	if app.runtime.processIncarnation != runtimeProcessIncarnation(marker) {
		t.Fatalf("runtime incarnation = %q", app.runtime.processIncarnation)
	}
}

func TestManagementRegistrationUsesExactFixedRoutes(t *testing.T) {
	app := NewApp(&fakeAuthHost{}, []byte("index"))
	defer app.Close()
	registration := app.ManagementRegistration()
	expected := map[string]struct{}{
		http.MethodGet + " /plugins/cpa-account-config-manager/accounts":                                  {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/config":                          {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/status":                          {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/quota-metadata/refresh":          {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/quota-metadata/reset":            {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/models":                          {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/deduplicate/preview":             {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/model-test":                      {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/token/refresh":                   {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/delete/preview":                  {},
		http.MethodPost + " /plugins/cpa-account-config-manager/accounts/delete/start":                    {},
		http.MethodGet + " /plugins/cpa-account-config-manager/export/accounts":                           {},
		http.MethodPost + " /plugins/cpa-account-config-manager/import/preview":                           {},
		http.MethodPost + " /plugins/cpa-account-config-manager/import/start":                             {},
		http.MethodGet + " /plugins/cpa-account-config-manager/import/status":                             {},
		http.MethodGet + " /plugins/cpa-account-config-manager/updates":                                   {},
		http.MethodPost + " /plugins/cpa-account-config-manager/updates/check":                            {},
		http.MethodGet + " /plugins/cpa-account-config-manager/experiments":                               {},
		http.MethodPut + " /plugins/cpa-account-config-manager/experiments":                               {},
		http.MethodPost + " /plugins/cpa-account-config-manager/experiments/agent-identity/session-login": {},
		http.MethodGet + " /plugins/cpa-account-config-manager/operations":                                {},
		http.MethodGet + " /plugins/cpa-account-config-manager/operations/export":                         {},
		http.MethodGet + " /plugins/cpa-account-config-manager/operations/settings":                       {},
		http.MethodPut + " /plugins/cpa-account-config-manager/operations/settings":                       {},
		http.MethodDelete + " /plugins/cpa-account-config-manager/operations":                             {},
		http.MethodPost + " /plugins/cpa-account-config-manager/operations/record":                        {},
	}
	if len(registration.Routes) != len(expected) {
		t.Fatalf("routes len = %d, want %d", len(registration.Routes), len(expected))
	}
	for _, route := range registration.Routes {
		key := route.Method + " " + route.Path
		if _, exists := expected[key]; !exists {
			t.Fatalf("unexpected route %q", key)
		}
		delete(expected, key)
		if route.Path == "" || route.Path[0] != '/' {
			t.Fatalf("invalid route path %q", route.Path)
		}
		for _, forbidden := range []string{"*", ":", "{"} {
			if strings.Contains(route.Path, forbidden) {
				t.Fatalf("route %q contains dynamic marker %q", route.Path, forbidden)
			}
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing routes = %#v", expected)
	}
	if len(registration.Resources) != 1 || registration.Resources[0].Path != "/index.html" || registration.Resources[0].Menu != "CPA-A Manager" {
		t.Fatalf("resources = %#v", registration.Resources)
	}
}

func TestRegistrationUsesInjectedReleaseMetadata(t *testing.T) {
	originalVersion := PluginVersion
	originalRepository := PluginRepository
	PluginVersion = "1.2.3"
	PluginRepository = "https://github.com/example/cpa-account-config-manager"
	defer func() {
		PluginVersion = originalVersion
		PluginRepository = originalRepository
	}()

	app := NewApp(&fakeAuthHost{}, []byte("index"))
	defer app.Close()
	registration := app.Registration()
	if registration.Metadata.Version != "1.2.3" || registration.Metadata.GitHubRepository != PluginRepository {
		t.Fatalf("metadata = %#v", registration.Metadata)
	}
	if !registration.Capabilities.ManagementAPI || !registration.Capabilities.UsagePlugin || !registration.Capabilities.RequestInterceptor {
		t.Fatalf("capabilities = %#v", registration.Capabilities)
	}
}

func TestNewerAppInstanceQuiescesSupersededBackgroundServices(t *testing.T) {
	originalVersion := PluginVersion
	defer func() { PluginVersion = originalVersion }()
	dataDir := t.TempDir()
	config := []byte("data_dir: " + dataDir + "\n")

	PluginVersion = "0.3.1202"
	older := NewApp(&fakeAuthHost{}, []byte("old"))
	older.runtime.bootstrapEnabled = false
	older.runtime.heartbeat = 10 * time.Millisecond
	older.Configure(config)
	t.Cleanup(older.Close)

	PluginVersion = "0.3.1203"
	newer := NewApp(&fakeAuthHost{}, []byte("new"))
	newer.runtime.bootstrapEnabled = false
	newer.runtime.heartbeat = 10 * time.Millisecond
	newer.runtime.takeover = 20 * time.Millisecond
	newer.Configure(config)
	t.Cleanup(newer.Close)

	deadline := time.Now().Add(2 * time.Second)
	for {
		older.updates.mu.RLock()
		updatesClosed := older.updates.closed
		older.updates.mu.RUnlock()
		if older.runtime.Snapshot().Superseded && updatesClosed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("old app did not quiesce: runtime=%#v updates=%t", older.runtime.Snapshot(), updatesClosed)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !newer.runtime.AllowsBackgroundWork() {
		deadline = time.Now().Add(time.Second)
		for !newer.runtime.AllowsBackgroundWork() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !newer.runtime.AllowsBackgroundWork() {
		t.Fatalf("new app did not take ownership: %#v", newer.runtime.Snapshot())
	}
	retiredResponse := older.HandleManagement(context.Background(), cpaapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/management/plugins/cpa-account-config-manager/accounts",
	})
	if retiredResponse.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(retiredResponse.Body), "superseded") {
		t.Fatalf("retired response = %d %s", retiredResponse.StatusCode, retiredResponse.Body)
	}
}

func TestHandleManagementListsRedactedAccounts(t *testing.T) {
	host := &fakeAuthHost{
		entries: []cpaapi.HostAuthFileEntry{{
			AuthIndex: "auth-1",
			Name:      "account.json",
			Provider:  "codex",
			Source:    "file",
			Path:      "/auths/account.json",
		}},
		details: map[string]cpaapi.HostAuthGetResponse{
			"auth-1": {
				AuthIndex: "auth-1",
				Name:      "account.json",
				Path:      "/auths/account.json",
				JSON:      json.RawMessage(`{"type":"codex","access_token":"secret"}`),
			},
		},
	}
	app := NewApp(host, []byte("index"))
	defer app.Close()
	response := app.HandleManagement(context.Background(), cpaapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/management/plugins/cpa-account-config-manager/accounts",
		Query:  url.Values{"page_size": []string{"20"}},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode, response.Body)
	}
	if strings.Contains(string(response.Body), "secret") {
		t.Fatalf("response leaked secret: %s", response.Body)
	}
}

func TestHandleManagementRejectsInvalidAccountSortQuery(t *testing.T) {
	app := NewApp(&fakeAuthHost{}, []byte("index"))
	defer app.Close()
	path := "/v0/management/plugins/cpa-account-config-manager/accounts"
	for name, query := range map[string]url.Values{
		"field": {"sort_by": []string{"credential"}},
		"order": {"sort_order": []string{"sideways"}},
	} {
		t.Run(name, func(t *testing.T) {
			response := app.HandleManagement(t.Context(), cpaapi.ManagementRequest{
				Method: http.MethodGet,
				Path:   path,
				Query:  query,
			})
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.StatusCode, response.Body)
			}
		})
	}
}

func TestAppReplacementRestoresPersistedUsageIntoAccountList(t *testing.T) {
	dataDir := t.TempDir()
	config := []byte(fmt.Sprintf("data_dir: %q\n", dataDir))
	newHost := func() *fakeAuthHost {
		return &fakeAuthHost{
			entries: []cpaapi.HostAuthFileEntry{{
				ID: "runtime-instance-id", AuthIndex: "stable-auth-index", Name: "persisted.json",
				Provider: "codex", Type: "oauth", Email: "persisted@example.com", Source: "file", Path: "/auths/persisted.json",
			}},
			details: map[string]cpaapi.HostAuthGetResponse{
				"stable-auth-index": {
					AuthIndex: "stable-auth-index", Name: "persisted.json", Path: "/auths/persisted.json",
					JSON: json.RawMessage(`{"type":"codex","email":"persisted@example.com","access_token":"must-not-be-persisted"}`),
				},
			},
		}
	}

	observedAt := time.Now().UTC().Truncate(time.Second)
	beforeUpdate := NewApp(newHost(), []byte("old-index"))
	beforeUpdate.Configure(config)
	beforeUpdate.HandleUsage(cpaapi.UsageRecord{
		AuthIndex: "stable-auth-index", AuthID: "must-not-be-persisted", APIKey: "sk-must-not-be-persisted",
		RequestedAt: observedAt,
		Detail:      cpaapi.UsageDetail{InputTokens: 80, OutputTokens: 20, TotalTokens: 100},
		ResponseHeaders: http.Header{
			"Authorization":                         []string{"Bearer must-not-be-persisted"},
			"X-Codex-Primary-Used-Percent":          []string{"64"},
			"X-Codex-Primary-Reset-After-Seconds":   []string{"604800"},
			"X-Codex-Primary-Window-Minutes":        []string{"10080"},
			"X-Codex-Secondary-Used-Percent":        []string{"18"},
			"X-Codex-Secondary-Reset-After-Seconds": []string{"18000"},
			"X-Codex-Secondary-Window-Minutes":      []string{"300"},
		},
	})
	beforeUpdate.Close()

	afterUpdate := NewApp(newHost(), []byte("replacement-index"))
	afterUpdate.Configure(config)
	defer afterUpdate.Close()
	response := afterUpdate.HandleManagement(context.Background(), cpaapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/management/plugins/cpa-account-config-manager/accounts",
		Query:  url.Values{"page_size": []string{"20"}},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("replacement account list status = %d body=%s", response.StatusCode, response.Body)
	}
	var listed ListResponse
	if errDecode := json.Unmarshal(response.Body, &listed); errDecode != nil {
		t.Fatalf("decode replacement account list: %v", errDecode)
	}
	if len(listed.Accounts) != 1 {
		t.Fatalf("replacement account count = %d, want 1", len(listed.Accounts))
	}
	usage := listed.Accounts[0].Usage
	if usage == nil || usage.TotalTokens != 100 || usage.InputTokens != 80 || usage.OutputTokens != 20 {
		t.Fatalf("replacement account usage = %#v", usage)
	}
	if usage.Codex == nil || usage.Codex.FiveHour == nil || usage.Codex.SevenDay == nil ||
		usage.Codex.FiveHour.UsedPercent != 18 || usage.Codex.SevenDay.UsedPercent != 64 {
		t.Fatalf("replacement account quota = %#v", usage.Codex)
	}
	for _, secret := range []string{"must-not-be-persisted", "sk-must-not-be-persisted", "Authorization", "Bearer"} {
		if bytes.Contains(response.Body, []byte(secret)) {
			t.Fatalf("replacement response leaked %q: %s", secret, response.Body)
		}
	}
}

func TestHandleManagementServesResourceOnlyAtResourcePath(t *testing.T) {
	app := NewApp(&fakeAuthHost{}, []byte("<!doctype html><title>manager</title>"))
	defer app.Close()
	response := app.HandleManagement(context.Background(), cpaapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   "/v0/resource/plugins/cpa-account-config-manager/index.html",
	})
	if response.StatusCode != http.StatusOK || string(response.Body) != "<!doctype html><title>manager</title>" {
		t.Fatalf("response = %d %q", response.StatusCode, response.Body)
	}
}
