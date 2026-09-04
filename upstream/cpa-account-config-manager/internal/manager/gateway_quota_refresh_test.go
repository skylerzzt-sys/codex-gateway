package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-account-config-manager/internal/cpaapi"
)

type gatewayQuotaTestHost struct {
	*fakeAuthHost
	mu       sync.Mutex
	requests []cpaapi.HostHTTPRequest
}

func (h *gatewayQuotaTestHost) AgentIdentityDo(_ context.Context, _ string, request cpaapi.HostHTTPRequest) (cpaapi.HostHTTPResponse, error) {
	request.Headers = request.Headers.Clone()
	h.mu.Lock()
	h.requests = append(h.requests, request)
	h.mu.Unlock()
	body := []byte(`{"available_count":0,"credits":[]}`)
	if request.URL == codexQuotaUsageURL {
		body = []byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":12,"limit_window_seconds":18000}}}`)
	}
	return cpaapi.HostHTTPResponse{StatusCode: http.StatusOK, Body: body}, nil
}

func TestPersonalGatewayQuotaAccountsMapAuthIDsToAuthIndexes(t *testing.T) {
	if personalGatewayQuotaRefreshInterval != 10*time.Minute {
		t.Fatalf("refresh interval = %s, want 10m", personalGatewayQuotaRefreshInterval)
	}
	accounts := []Account{
		{ID: "index-a", AuthID: "stable-a", Provider: "codex"},
		{ID: "index-b", AuthID: "stable-b", Provider: "codex"},
		{ID: "index-c", AuthID: "unbound", Provider: "codex"},
	}
	selected := personalGatewayQuotaAccounts(PersonalGatewayConfig{AccountAID: "stable-a", AccountBID: "stable-b"}, accounts)
	if len(selected) != 2 || selected[0].ID != "index-a" || selected[1].ID != "index-b" {
		t.Fatalf("selected accounts = %#v", selected)
	}
}

func TestPersonalGatewayQuotaRefreshUsesCurrentAuthDocumentsWithoutManagementKey(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "a.json")
	pathB := filepath.Join(t.TempDir(), "b.json")
	host := &gatewayQuotaTestHost{fakeAuthHost: &fakeAuthHost{
		entries: []cpaapi.HostAuthFileEntry{
			{ID: "stable-a", AuthIndex: "index-a", Name: "a.json", Provider: "codex", Type: "codex", Source: "file", Path: pathA},
			{ID: "stable-b", AuthIndex: "index-b", Name: "b.json", Provider: "codex", Type: "codex", Source: "file", Path: pathB},
		},
		details: map[string]cpaapi.HostAuthGetResponse{
			"index-a": {AuthIndex: "index-a", Name: "a.json", Path: pathA, JSON: json.RawMessage(`{"type":"codex","access_token":"token-a","chatgpt_account_id":"account-a"}`)},
			"index-b": {AuthIndex: "index-b", Name: "b.json", Path: pathB, JSON: json.RawMessage(`{"type":"codex","access_token":"token-b","chatgpt_account_id":"account-b"}`)},
		},
	}}
	app := NewApp(host, []byte("index"))
	defer app.Close()
	app.Configure([]byte(fmt.Sprintf("data_dir: %q\ngateway_account_a_id: stable-a\ngateway_account_b_id: stable-b\n", t.TempDir())))
	app.refreshPersonalGatewayQuotas(context.Background())

	host.mu.Lock()
	requests := append([]cpaapi.HostHTTPRequest(nil), host.requests...)
	host.mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("quota requests = %d, want 4", len(requests))
	}
	want := []struct {
		url       string
		token     string
		accountID string
	}{
		{codexQuotaUsageURL, "Bearer token-a", "account-a"},
		{codexQuotaResetCreditsURL, "Bearer token-a", "account-a"},
		{codexQuotaUsageURL, "Bearer token-b", "account-b"},
		{codexQuotaResetCreditsURL, "Bearer token-b", "account-b"},
	}
	for index, expected := range want {
		request := requests[index]
		if request.URL != expected.url || request.Headers.Get("Authorization") != expected.token || request.Headers.Get("Chatgpt-Account-Id") != expected.accountID {
			t.Fatalf("request %d = %#v", index, request)
		}
	}
}
